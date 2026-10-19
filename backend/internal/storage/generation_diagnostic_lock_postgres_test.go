package storage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/operation"
	"gorm.io/gorm"
)

func TestPostgresGenerationFenceCompatibleWithDiagnosticForeignKeys(t *testing.T) {
	db := openGenerationTestDB(t)
	id := seedGenerationTestConversation(t, db)
	ctx := context.Background()
	_, run, _, err := NewConversationRepo(db).StartGeneration(ctx, id, GenerationRequest{
		RequestID: uuid.New(), OperationType: "user_input", PayloadHash: "diagnostic-locks",
	})
	if err != nil {
		t.Fatal(err)
	}
	runCtx := operation.WithGeneration(ctx, id, run.ID)
	turn, attempt, err := NewTurnRepo(db).Begin(runCtx, id, "user_input")
	if err != nil {
		t.Fatal(err)
	}
	bound := operation.WithMetadata(runCtx, operation.Metadata{ConversationID: id, TurnID: turn.ID, AttemptID: attempt.ID})
	parents := []struct {
		table string
		id    uuid.UUID
	}{
		{"conversations", id}, {"conversation_generation_runs", run.ID},
		{"generation_attempts", attempt.ID}, {"conversation_turns", turn.ID},
	}
	for _, test := range []struct {
		name string
		ctx  context.Context
	}{
		{"run", runCtx}, {"attempt", bound},
	} {
		t.Run(test.name, func(t *testing.T) {
			// Reproduce the diagnostic side already holding FK locks. The
			// generation fence must still finish while these locks are held.
			foreignKeys := db.Begin()
			if foreignKeys.Error != nil {
				t.Fatal(foreignKeys.Error)
			}
			defer foreignKeys.Rollback()
			for _, parent := range parents {
				if err := foreignKeys.Exec("SELECT id FROM "+parent.table+" WHERE id = ? FOR KEY SHARE", parent.id).Error; err != nil {
					t.Fatal(err)
				}
			}
			business := db.Begin()
			if business.Error != nil {
				t.Fatal(business.Error)
			}
			defer business.Rollback()
			if err := business.Exec("SET LOCAL lock_timeout = '750ms'").Error; err != nil {
				t.Fatal(err)
			}
			if err := AssertGenerationActive(test.ctx, business, id); err != nil {
				t.Fatalf("generation blocked by FK locks: %v", err)
			}
			// The weaker lock must still exclude competing business writes.
			lockedParents := parents
			if test.name == "run" {
				lockedParents = parents[:2]
			}
			for _, parent := range lockedParents {
				probe := db.Begin()
				if probe.Error != nil {
					t.Fatal(probe.Error)
				}
				err := probe.Exec("SELECT id FROM "+parent.table+" WHERE id = ? FOR NO KEY UPDATE NOWAIT", parent.id).Error
				probe.Rollback()
				assertDiagnosticSQLState(t, err, "55P03")
			}
			if test.name == "attempt" {
				step := &AgentStep{ID: uuid.New(), ConversationID: id, StepNo: 1, Model: "test", Protocol: "responses", TokenUsageSource: "estimated"}
				if err := NewAgentRepo(business).CreateStep(bound, step); err != nil {
					t.Fatalf("agent step failed while FK locks held: %v", err)
				}
				if err := NewAgentRepo(business).FinishStep(bound, step.ID, AgentStepCompleted, AgentLaneText, "stop", "", AgentStepMetrics{TokenUsageSource: "estimated"}); err != nil {
					t.Fatalf("agent step finish failed while FK locks held: %v", err)
				}
			}
			// The reverse interleaving also works: persist a real diagnostic
			// record before the generation transaction releases its locks.
			logCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			row := &LLMRequestLog{ID: uuid.New(), Model: "lock-test", ConversationID: &id, AttemptID: &attempt.ID}
			if err := NewLLMRequestLogRepo(db).Create(logCtx, row); err != nil {
				t.Fatalf("diagnostic blocked by generation fence: %v", err)
			}
			if err := business.Commit().Error; err != nil {
				t.Fatal(err)
			}
			if _, err := NewLLMRequestLogRepo(db).Get(ctx, row.ID); err != nil {
				t.Fatalf("diagnostic did not persist: %v", err)
			}
		})
	}
}

func TestPostgresDiagnosticBusyParentsReleaseAllLocks(t *testing.T) {
	db := openGenerationTestDB(t)
	id := seedGenerationTestConversation(t, db)
	_, attempt, err := NewTurnRepo(db).Begin(context.Background(), id, "user_input")
	if err != nil {
		t.Fatal(err)
	}
	job := MemoryJob{ID: uuid.New(), ConversationID: id, TargetUntilTurnSeq: 1, Status: MemoryJobPending,
		MaxExecutions: 4, CreatedAt: time.Now(), UpdatedAt: time.Now(), AvailableAt: time.Now()}
	memory := ConversationMemory{ID: uuid.New(), ConversationID: id, Version: 1,
		CoveredFromTurnSeq: 1, CoveredUntilTurnSeq: 1, CoveredUntilMessageSeq: 1, SchemaVersion: 1,
		Content: JSONDoc(`{}`), SourceHash: strings.Repeat("0", 64), GenerationFingerprint: strings.Repeat("1", 64)}
	for _, row := range []any{&job, &memory} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, parent := range []struct {
		table string
		id    uuid.UUID
	}{
		{"conversations", id}, {"generation_attempts", attempt.ID},
		{"memory_jobs", job.ID}, {"conversation_memories", memory.ID},
	} {
		t.Run(parent.table, func(t *testing.T) {
			// Hold a real incompatible parent lock until Create returns. A
			// blocking FK insert would time out; NOWAIT must exhaust its bounded
			// retries and release every earlier parent lock instead.
			business := db.Begin()
			if business.Error != nil {
				t.Fatal(business.Error)
			}
			defer business.Rollback()
			if err := business.Exec("SELECT id FROM "+parent.table+" WHERE id = ? FOR UPDATE", parent.id).Error; err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			row := &LLMRequestLog{ID: uuid.New(), Model: "busy-parent", ConversationID: &id,
				AttemptID: &attempt.ID, MemoryJobID: &job.ID, MemoryID: &memory.ID}
			err := NewLLMRequestLogRepo(db).Create(ctx, row)
			assertDiagnosticSQLState(t, err, "55P03")
			if ctx.Err() != nil {
				t.Fatalf("diagnostic waited on the parent: %v", ctx.Err())
			}
			// Escalating these locks would fail if the rejected diagnostic kept
			// any previously acquired KEY SHARE locks or a failed transaction.
			for _, earlier := range []struct {
				table string
				id    uuid.UUID
			}{
				{"conversations", id}, {"generation_attempts", attempt.ID},
				{"memory_jobs", job.ID}, {"conversation_memories", memory.ID},
			} {
				if err := business.Exec("SELECT id FROM "+earlier.table+" WHERE id = ? FOR UPDATE NOWAIT", earlier.id).Error; err != nil {
					t.Fatalf("diagnostic retained a parent lock: %v", err)
				}
			}
			if err := business.Commit().Error; err != nil {
				t.Fatal(err)
			}
			var count int64
			if err := db.Model(&LLMRequestLog{}).Where("id = ?", row.ID).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("failed diagnostic partially persisted: count=%d err=%v", count, err)
			}
			if err := NewLLMRequestLogRepo(db).Create(ctx, row); err != nil {
				t.Fatalf("diagnostic did not recover after parent release: %v", err)
			}
		})
	}
}

func TestPostgresGenerationFenceRechecksCancellationAfterLockWait(t *testing.T) {
	db := openGenerationTestDB(t)
	id := seedGenerationTestConversation(t, db)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, run, _, err := NewConversationRepo(db).StartGeneration(ctx, id, GenerationRequest{
		RequestID: uuid.New(), OperationType: "user_input", PayloadHash: "cancel-lock-order",
	})
	if err != nil {
		t.Fatal(err)
	}
	runCtx := operation.WithGeneration(ctx, id, run.ID)
	turn, attempt, err := NewTurnRepo(db).Begin(runCtx, id, "user_input")
	if err != nil {
		t.Fatal(err)
	}
	bound := operation.WithMetadata(runCtx, operation.Metadata{ConversationID: id, TurnID: turn.ID, AttemptID: attempt.ID})
	lifecycle := db.WithContext(ctx).Begin()
	if lifecycle.Error != nil {
		t.Fatal(lifecycle.Error)
	}
	defer lifecycle.Rollback()
	if err := lifecycle.Exec("SELECT id FROM conversations WHERE id = ? FOR UPDATE", id).Error; err != nil {
		t.Fatal(err)
	}
	business := db.WithContext(ctx).Begin()
	if business.Error != nil {
		t.Fatal(business.Error)
	}
	defer business.Rollback()
	var pid int
	if err := business.Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- AssertGenerationActive(bound, business, id) }()
	// Observe a real lock wait rather than assuming the goroutine has reached
	// its query after an arbitrary sleep. It must wait on the conversation
	// without first locking the run/attempt/turn needed by cancellation.
	for {
		var waiting bool
		if err := db.WithContext(ctx).Raw("SELECT cardinality(pg_blocking_pids(?)) > 0", pid).Scan(&waiting).Error; err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("fence did not wait for the lifecycle transaction: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	for _, parent := range []struct {
		table string
		id    uuid.UUID
	}{
		{"conversation_generation_runs", run.ID}, {"generation_attempts", attempt.ID}, {"conversation_turns", turn.ID},
	} {
		if err := lifecycle.Exec("SELECT id FROM "+parent.table+" WHERE id = ? FOR UPDATE NOWAIT", parent.id).Error; err != nil {
			t.Fatalf("fence locked %s before the conversation: %v", parent.table, err)
		}
	}
	if err := NewConversationRepo(lifecycle).CancelGeneration(ctx, id, "lock-test"); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.Commit().Error; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrGenerationNotActive) {
			t.Fatalf("cancelled generation passed fence after wait: %v", err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestPostgresDiagnosticRejectsMissingParents(t *testing.T) {
	db := openGenerationTestDB(t)
	missing := uuid.New()
	for _, row := range []*LLMRequestLog{
		{ID: uuid.New(), ConversationID: &missing}, {ID: uuid.New(), AttemptID: &missing},
		{ID: uuid.New(), MemoryJobID: &missing}, {ID: uuid.New(), MemoryID: &missing},
	} {
		if err := NewLLMRequestLogRepo(db).Create(context.Background(), row); !errors.Is(err, gorm.ErrForeignKeyViolated) {
			t.Fatalf("missing diagnostic parent accepted: %v", err)
		}
	}
}

func assertDiagnosticSQLState(t *testing.T, err error, want string) {
	t.Helper()
	var state interface{ SQLState() string }
	if !errors.As(err, &state) || state.SQLState() != want {
		t.Fatalf("SQL error = %v, want SQLSTATE %s", err, want)
	}
}
