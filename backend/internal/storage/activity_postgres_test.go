package storage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/testdb"
	"gorm.io/gorm"
)

func TestActivityRevisionIsTransactionalAndIgnoresPhaseAndLeaseHeartbeats(t *testing.T) {
	db := testdb.Open(t, "activity")
	id := seedGenerationTestConversation(t, db)
	repo := NewActivityRepo(db)
	ctx := context.Background()
	initial, err := repo.Snapshot(ctx, id, "")
	if err != nil {
		t.Fatal(err)
	}
	var owner string
	if err := db.Model(&Conversation{}).Select("player_id").Where("id=?", id).Scan(&owner).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Snapshot(ctx, id, uuid.NewString()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign owner: %v", err)
	}
	if _, err := repo.Snapshot(ctx, id, owner); err != nil {
		t.Fatal(err)
	}
	turn := ConversationTurn{ID: uuid.New(), ConversationID: id, TurnSeq: 1, TriggerType: "user_input", Status: TurnStatusPending, CreatedAt: time.Now()}
	attempt := GenerationAttempt{ID: uuid.New(), ConversationID: id, TurnID: turn.ID, AttemptNo: 1, Status: AttemptStatusRunning, StartedAt: time.Now()}
	job := MemoryJob{ID: uuid.New(), ConversationID: id, SourceBranchEpoch: 0, TargetUntilTurnSeq: 1, Status: MemoryJobPending, MaxExecutions: 4, CreatedAt: time.Now(), UpdatedAt: time.Now(), AvailableAt: time.Now()}
	if err := db.Transaction(func(tx *gorm.DB) error {
		for _, row := range []any{&turn, &attempt, &job} {
			if err := tx.Create(row).Error; err != nil {
				return err
			}
		}
		return tx.Model(&Conversation{}).Where("id=?", id).Update("is_processing", true).Error
	}); err != nil {
		t.Fatal(err)
	}
	active, err := repo.Snapshot(ctx, id, "")
	if err != nil {
		t.Fatal(err)
	}
	if active.ActivityVersion != initial.ActivityVersion+1 || active.AttemptID == nil || *active.AttemptID != attempt.ID || active.Memory != "queued" || !active.IsProcessing {
		t.Fatalf("transaction projection: %+v, initial %d", active, initial.ActivityVersion)
	}
	if err := db.Model(&GenerationAttempt{}).Where("id=?", attempt.ID).Update("step_count", 2).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&MemoryJob{}).Where("id=?", job.ID).Updates(map[string]any{"lease_expires_at": time.Now().Add(time.Minute), "updated_at": time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	unchanged, err := repo.Snapshot(ctx, id, "")
	if err != nil || unchanged.ActivityVersion != active.ActivityVersion {
		t.Fatalf("heartbeat changed stable version: %+v %v", unchanged, err)
	}
	rollback := errors.New("rollback")
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&MemoryJob{}).Where("id=?", job.ID).Update("status", MemoryJobRunning).Error; err != nil {
			return err
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	unchanged, err = repo.Snapshot(ctx, id, "")
	if err != nil || unchanged.ActivityVersion != active.ActivityVersion || unchanged.Memory != "queued" {
		t.Fatalf("rollback escaped: %+v %v", unchanged, err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&GenerationAttempt{}).Where("id=?", attempt.ID).Update("status", AttemptStatusFailed).Error; err != nil {
			return err
		}
		if err := tx.Model(&ConversationTurn{}).Where("id=?", turn.ID).Update("status", TurnStatusFailed).Error; err != nil {
			return err
		}
		return tx.Model(&MemoryJob{}).Where("id=?", job.ID).Update("status", MemoryJobRunning).Error
	}); err != nil {
		t.Fatal(err)
	}
	failed, err := repo.Snapshot(ctx, id, "")
	if err != nil || failed.ActivityVersion != active.ActivityVersion+1 || failed.CanRegenerate || failed.TurnStatus != TurnStatusFailed || failed.Memory != "running" {
		t.Fatalf("failed capabilities without trigger: %+v %v", failed, err)
	}
	trigger := ChatMessage{ID: uuid.New(), ConversationID: id, TurnID: &turn.ID, Role: RoleUser, Content: "trigger", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := NewMessageRepo(db).CreateChat(ctx, &trigger); err != nil {
		t.Fatal(err)
	}
	recoverable, err := repo.Snapshot(ctx, id, "")
	if err != nil || !recoverable.CanRegenerate || recoverable.RegenerateCostQuantity != 0 {
		t.Fatalf("recoverable turn: %+v %v", recoverable, err)
	}
	if err := db.Model(&Conversation{}).Where("id=?", id).Update("is_processing", false).Error; err != nil {
		t.Fatal(err)
	}
	idle, err := repo.Snapshot(ctx, id, "")
	if err != nil || idle.IsProcessing || idle.Narrative != "idle" || idle.ActivityVersion <= recoverable.ActivityVersion {
		t.Fatalf("release: %+v %v", idle, err)
	}
	for _, status := range []string{MemoryJobSucceeded, MemoryJobFailed, MemoryJobCancelled} {
		if err := db.Model(&MemoryJob{}).Where("id=?", job.ID).Update("status", status).Error; err != nil {
			t.Fatal(err)
		}
		terminal, err := repo.Snapshot(ctx, id, "")
		if err != nil || terminal.Memory != "idle" || terminal.MemoryJob.Status != status || terminal.ActivityVersion <= idle.ActivityVersion {
			t.Fatalf("memory terminal %s: %+v %v", status, terminal, err)
		}
		idle = terminal
	}
}

func TestActivitySnapshotMatchesCurrentCapabilities(t *testing.T) {
	db := testdb.Open(t, "activity_capabilities")
	id := seedGenerationTestConversation(t, db)
	ctx := context.Background()
	for i, status := range []string{TurnStatusCompleted, TurnStatusPartial, TurnStatusFailed, TurnStatusCancelled} {
		turn := ConversationTurn{ID: uuid.New(), ConversationID: id, TurnSeq: int64(i + 1), TriggerType: "user_input", Status: status, CreatedAt: time.Now()}
		attempt := GenerationAttempt{ID: uuid.New(), ConversationID: id, TurnID: turn.ID, AttemptNo: 1, Status: status, CommittedMutatingToolCount: 1, StartedAt: time.Now()}
		if err := db.Create(&turn).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&attempt).Error; err != nil {
			t.Fatal(err)
		}
		for _, role := range []string{RoleUser, RoleAssistant} {
			message := ChatMessage{ID: uuid.New(), ConversationID: id, TurnID: &turn.ID, Role: role, Content: "message", CreatedAt: time.Now(), UpdatedAt: time.Now()}
			if err := NewMessageRepo(db).CreateChat(ctx, &message); err != nil {
				t.Fatal(err)
			}
		}
		snapshot, err := NewActivityRepo(db).Snapshot(ctx, id, "")
		if err != nil {
			t.Fatal(err)
		}
		capabilities, err := NewTurnRepo(db).CurrentCapabilities(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.CanContinue != capabilities.CanContinue || snapshot.CanRegenerate != capabilities.CanRegenerate || snapshot.RegenerateCostQuantity != capabilities.RegenerateCostQuantity {
			t.Fatalf("%s: %+v != %+v", status, snapshot, capabilities)
		}
	}
}

func TestActivityMessageUpdatesOnlyInvalidateStableFields(t *testing.T) {
	db := testdb.Open(t, "activity_message_update")
	id := seedGenerationTestConversation(t, db)
	ctx := context.Background()
	repo := NewActivityRepo(db)
	message := ChatMessage{ID: uuid.New(), ConversationID: id, Role: RoleAssistant, Content: "original", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := NewMessageRepo(db).CreateChat(ctx, &message); err != nil {
		t.Fatal(err)
	}
	read := func() *ActivitySnapshot {
		t.Helper()
		snapshot, err := repo.Snapshot(ctx, id, "")
		if err != nil {
			t.Fatal(err)
		}
		return snapshot
	}
	previous := read().ActivityVersion
	// Content-only edits and unchanged stable columns must not invalidate activity,
	// even with a large body. This is a semantic regression test, not a benchmark.
	if err := db.Model(&ChatMessage{}).Where("id=?", message.ID).Updates(map[string]any{
		"content": strings.Repeat("正文", 16384), "role": RoleAssistant, "invalidated_at": nil,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if got := read().ActivityVersion; got != previous {
		t.Fatalf("content-only update changed version: %d -> %d", previous, got)
	}
	for _, update := range []map[string]any{
		{"role": RoleUser},
		{"invalidated_at": time.Now()},
		{"invalidated_at": nil},
	} {
		if err := db.Model(&ChatMessage{}).Where("id=?", message.ID).Updates(update).Error; err != nil {
			t.Fatal(err)
		}
		got := read().ActivityVersion
		if got != previous+1 {
			t.Fatalf("stable update %v: version %d -> %d", update, previous, got)
		}
		previous = got
	}
}
