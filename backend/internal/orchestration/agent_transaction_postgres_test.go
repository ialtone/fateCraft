package orchestration

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/ialtone/fatecraft/internal/operation"
	"github.com/ialtone/fatecraft/internal/storage"
	"github.com/ialtone/fatecraft/internal/testdb"
)

func TestPostgresAgentToolTransactionAtomicity(t *testing.T) {
	db := openAgentTransactionTestDB(t)
	ctx, ids := seedAgentTransactionTest(t, db)
	agents := storage.NewAgentRepo(db)
	toolTx := NewAgentToolTransaction(db)
	states := storage.NewStateRepo(db)

	rejected := claimAgentTransactionInvocation(t, ctx, agents, ids, "rejected-call")
	err := toolTx.Run(ctx, func(tx *agentToolTx) error {
		if err := tx.states.Upsert(ctx, &storage.ConversationState{
			ConversationID: ids.conversationID, State: storage.JSONMap{"marker": "must_rollback"},
			SchemaVersion: 1, UpdatedAt: time.Now(),
		}); err != nil {
			return err
		}
		return errAgentToolOutcomeRollback
	})
	if !errors.Is(err, errAgentToolOutcomeRollback) {
		t.Fatalf("rejected transaction error = %v", err)
	}
	if _, err := states.Get(ctx, ids.conversationID); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("rejected transaction left domain state: %v", err)
	}
	if err := agents.FinishInvocation(
		ctx, rejected.ID, storage.ToolInvocationRejected, "state_rejected",
		storage.JSONMap{"ok": false, "code": "state_rejected", "message": "rejected"}, false,
		storage.JSONDoc(`[{"id":"rejected-call","name":"fc_test","arguments":"{}"},{"id":"committed-call","name":"fc_test","arguments":"{}"}]`),
		`{"ok":false,"code":"state_rejected","message":"rejected"}`,
	); err != nil {
		t.Fatal(err)
	}
	assertAgentTransactionState(t, db, ids, rejected.ID, storage.ToolInvocationRejected, 0, 2)

	committed := claimAgentTransactionInvocation(t, ctx, agents, ids, "committed-call")
	err = toolTx.Run(ctx, func(tx *agentToolTx) error {
		if err := tx.states.Upsert(ctx, &storage.ConversationState{
			ConversationID: ids.conversationID, State: storage.JSONMap{"marker": "committed"},
			SchemaVersion: 1, UpdatedAt: time.Now(),
		}); err != nil {
			return err
		}
		return tx.agents.FinishInvocationInCurrentTxWithBlocks(
			ctx, tx.db, committed.ID, storage.ToolInvocationCommitted, "state_committed",
			storage.JSONMap{"ok": true, "code": "state_committed", "message": "committed"}, true,
			nil,
			`{"ok":true,"code":"state_committed","message":"committed"}`,
		)
	})
	if err != nil {
		t.Fatal(err)
	}
	state, err := states.Get(ctx, ids.conversationID)
	if err != nil || state.State["marker"] != "committed" {
		t.Fatalf("committed domain state = %+v, err=%v", state, err)
	}
	assertAgentTransactionState(t, db, ids, committed.ID, storage.ToolInvocationCommitted, 1, 3)
}

type agentTransactionIDs struct {
	conversationID uuid.UUID
	turnID         uuid.UUID
	attemptID      uuid.UUID
	stepID         uuid.UUID
}

func openAgentTransactionTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testdb.Open(t, "agent_verify")
}

func seedAgentTransactionTest(t *testing.T, db *gorm.DB) (context.Context, agentTransactionIDs) {
	t.Helper()
	playerID, packID, versionID := uuid.New(), uuid.New(), uuid.New()
	ids := agentTransactionIDs{conversationID: uuid.New(), turnID: uuid.New(), attemptID: uuid.New(), stepID: uuid.New()}
	hash := strings.Repeat("0", 64)
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO users (id, username, display_name) VALUES (?, 'agent-test', 'Agent Test')`, []any{playerID}},
		{`INSERT INTO challenge_packs (id, slug, name) VALUES (?, 'agent-test', 'Agent Test')`, []any{packID}},
		{`INSERT INTO challenge_versions (id, challenge_pack_id, version, status, manifest, content_hash, published_at) VALUES (?, ?, '1.0.0', 'published', '{}'::jsonb, ?, CURRENT_TIMESTAMP)`, []any{versionID, packID, hash}},
		{`UPDATE challenge_packs SET current_version_id = ?, status = 'active' WHERE id = ?`, []any{versionID, packID}},
		{`INSERT INTO conversations (id, player_id, title, challenge_version_id, challenge_hash, llm_route) VALUES (?, ?, 'Agent Test', ?, ?, 'server')`, []any{ids.conversationID, playerID, versionID, hash}},
		{`INSERT INTO conversation_turns (id, conversation_id, turn_seq, trigger_type, status) VALUES (?, ?, 1, 'user_input', 'pending')`, []any{ids.turnID, ids.conversationID}},
		{`INSERT INTO generation_attempts (id, conversation_id, turn_id, attempt_no, branch_epoch, status) VALUES (?, ?, ?, 1, 0, 'running')`, []any{ids.attemptID, ids.conversationID, ids.turnID}},
		{`INSERT INTO agent_steps (id, conversation_id, turn_id, attempt_id, step_no, status, output_lane, model, protocol, tool_snapshot_json, tool_snapshot_hash) VALUES (?, ?, ?, ?, 1, 'completed', 'tool', 'test', 'chat_completions', '[]'::jsonb, ?)`, []any{ids.stepID, ids.conversationID, ids.turnID, ids.attemptID, hash}},
	}
	for _, statement := range statements {
		if err := db.Exec(statement.query, statement.args...).Error; err != nil {
			t.Fatalf("seed agent transaction test: %v", err)
		}
	}
	ctx := operation.WithMetadata(context.Background(), operation.Metadata{TurnID: ids.turnID, AttemptID: ids.attemptID})
	return ctx, ids
}

func claimAgentTransactionInvocation(t *testing.T, ctx context.Context, agents *storage.AgentRepo, ids agentTransactionIDs, callID string) *storage.AgentToolInvocation {
	t.Helper()
	invocation, replayed, err := agents.ClaimInvocation(ctx, &storage.AgentToolInvocation{
		ID: uuid.New(), ConversationID: ids.conversationID, StepID: ids.stepID,
		StepToolSnapshotHash: strings.Repeat("0", 64), CallID: callID, ToolName: "fc_test",
		Arguments: storage.JSONMap{}, ArgumentsHash: hashAgentArguments("{}"),
	})
	if err != nil || replayed {
		t.Fatalf("claim invocation: replayed=%v err=%v", replayed, err)
	}
	return invocation
}

func assertAgentTransactionState(t *testing.T, db *gorm.DB, ids agentTransactionIDs, invocationID uuid.UUID, status string, committedCount, messageCount int) {
	t.Helper()
	var invocation storage.AgentToolInvocation
	if err := db.First(&invocation, "id = ?", invocationID).Error; err != nil || invocation.Status != status {
		t.Fatalf("invocation = %+v, err=%v", invocation, err)
	}
	var attempt storage.GenerationAttempt
	if err := db.First(&attempt, "id = ?", ids.attemptID).Error; err != nil || attempt.CommittedMutatingToolCount != committedCount {
		t.Fatalf("attempt = %+v, err=%v", attempt, err)
	}
	var count int64
	if err := db.Model(&storage.ChatMessage{}).Where("attempt_id = ?", ids.attemptID).Count(&count).Error; err != nil || count != int64(messageCount) {
		t.Fatalf("tool message count = %d, want %d, err=%v", count, messageCount, err)
	}
	if status == storage.ToolInvocationCommitted && invocation.CommittedAt == nil {
		t.Fatal("committed invocation has no committed_at")
	}
	if status != storage.ToolInvocationCommitted && invocation.CommittedAt != nil {
		t.Fatalf("%s invocation unexpectedly has committed_at", status)
	}
}
