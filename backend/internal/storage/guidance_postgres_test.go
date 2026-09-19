package storage

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/guidance"
	"github.com/ialtone/fatecraft/internal/operation"
)

func TestPostgresGuidanceLifecycleAndAtomicSelection(t *testing.T) {
	db := openGenerationTestDB(t)
	ctx := context.Background()
	id := seedGenerationTestConversation(t, db)
	conversations, turns, messages := NewConversationRepo(db), NewTurnRepo(db), NewMessageRepo(db)
	request := GenerationRequest{RequestID:uuid.New(), OperationType:"user_input", PayloadHash:"opening"}
	_, run, _, err := conversations.StartGeneration(ctx, id, request)
	if err != nil { t.Fatal(err) }
	runCtx := operation.WithGeneration(ctx, id, run.ID)
	prepared, err := turns.BeginUserTurn(runCtx, id, "user_input", "Show directions", nil)
	if err != nil { t.Fatal(err) }
	attemptCtx := operation.WithMetadata(runCtx, operation.Metadata{ConversationID:id, TurnID:prepared.Turn.ID, AttemptID:prepared.Attempt.ID})
	proposal := guidance.Proposal{Title:"Choose", Options:[]guidance.Option{{Label:"Ask",Message:"Ask the guide"},{Label:"Wait",Message:"Wait here"}}}
	snapshot := guidance.Snapshot{Proposal:proposal, StateFingerprint:guidance.Fingerprint(JSONMap{})}
	displayID := uuid.New()
	_, _, err = messages.CreateAssistantWithDisplay(attemptCtx, id, displayID, "A crossroads.", "A crossroads.", map[string]any{guidance.MetadataKey:snapshot}, nil, nil)
	if err != nil { t.Fatal(err) }
	if err := turns.Seal(attemptCtx, prepared.Turn.ID, prepared.Attempt.ID, true, "", ""); err != nil { t.Fatal(err) }
	if err := conversations.FinishGeneration(ctx, run.ID, GenerationRunCompleted, ""); err != nil { t.Fatal(err) }
	active, err := messages.CurrentGuidance(ctx, id)
	if err != nil || active == nil || active.ID != displayID.String() { t.Fatalf("guidance not recovered: %#v %v", active, err) }
	if other, err := messages.CurrentGuidance(ctx, uuid.New()); err != nil || other != nil { t.Fatalf("cross-conversation leak: %#v %v", other, err) }

	// A state change invalidates suggestions without touching message history.
	if err := db.Create(&ConversationState{ConversationID:id, State:JSONMap{"changed":true}, SchemaVersion:1}).Error; err != nil { t.Fatal(err) }
	if active, err := messages.CurrentGuidance(ctx, id); err != nil || active != nil { t.Fatalf("state-stale guidance: %#v %v", active, err) }
	if err := db.Model(&ConversationState{}).Where("conversation_id = ?", id).Update("state", JSONMap{}).Error; err != nil { t.Fatal(err) }

	selection := guidance.Selection{ID:displayID.String(), Key:"option-2"}
	selectRequest := GenerationRequest{RequestID:uuid.New(), OperationType:"user_input", TargetMessageID:&displayID, PayloadHash:guidance.Fingerprint(selection)}
	_, selectRun, _, err := conversations.StartGeneration(ctx, id, selectRequest)
	if err != nil { t.Fatal(err) }
	selectCtx := operation.WithGeneration(ctx, id, selectRun.ID)
	badCtx := guidance.WithSelection(selectCtx, guidance.Selection{ID:selection.ID, Key:"forged"})
	if _, err := turns.BeginUserTurn(badCtx, id, "user_input", "Injected content", nil); !errors.Is(err, ErrGuidanceExpired) { t.Fatalf("forged key accepted: %v", err) }
	var count int64
	db.Model(&ConversationTurn{}).Where("conversation_id = ?", id).Count(&count)
	if count != 1 { t.Fatalf("failed selection created a turn: %d", count) }
	selectCtx = guidance.WithSelection(selectCtx, selection)
	selected, err := turns.BeginUserTurn(selectCtx, id, "user_input", "Injected content", nil)
	if err != nil { t.Fatal(err) }
	if selected.Trigger.Content != "Wait here" || selected.Display.Markdown != "Wait here" { t.Fatalf("client content used: %#v", selected) }
	if _, err := turns.BeginUserTurn(selectCtx, id, "user_input", "Again", nil); !errors.Is(err, ErrGuidanceExpired) { t.Fatalf("double selection accepted: %v", err) }
	if active, err := messages.CurrentGuidance(ctx, id); err != nil || active != nil { t.Fatalf("consumed guidance restored: %#v %v", active, err) }
	if err := turns.Seal(selectCtx, selected.Turn.ID, selected.Attempt.ID, false, "test", "test"); err != nil { t.Fatal(err) }
	if err := conversations.FinishGeneration(ctx, selectRun.ID, GenerationRunFailed, "test"); err != nil { t.Fatal(err) }
	if active, err := messages.CurrentGuidance(ctx, id); err != nil || active != nil { t.Fatalf("failed turn revived older guidance: %#v %v", active, err) }
	if _, replay, replayed, err := conversations.StartGeneration(ctx, id, selectRequest); err != nil || !replayed || replay.ID != selectRun.ID { t.Fatalf("request retry not idempotent: %#v %v", replay, err) }
}
