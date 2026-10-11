package orchestration

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"

	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/storage"
)

type rollCommitTxStub struct {
	calls      []string
	state      *storage.ConversationState
	stateErr   error
	failAt     string
	history    *storage.RollHistoryEntry
	chat       *storage.ChatMessage
	display    *storage.DisplayMessage
	savedState *storage.ConversationState
	revision   *storage.StateRevision
}

func (s *rollCommitTxStub) call(name string) error {
	s.calls = append(s.calls, name)
	if s.failAt == name {
		return errors.New("forced " + name + " failure")
	}
	return nil
}

func (s *rollCommitTxStub) LockConversation(context.Context, uuid.UUID) error {
	return s.call("lock")
}

func (s *rollCommitTxStub) GetStateForUpdate(context.Context, uuid.UUID) (*storage.ConversationState, error) {
	if err := s.call("get_state"); err != nil {
		return nil, err
	}
	return s.state, s.stateErr
}

func (s *rollCommitTxStub) CreateRollHistory(_ context.Context, entry *storage.RollHistoryEntry) error {
	s.history = entry
	return s.call("history")
}

func (s *rollCommitTxStub) CreateChat(_ context.Context, message *storage.ChatMessage) error {
	s.chat = message
	return s.call("chat")
}

func (s *rollCommitTxStub) CreateDisplay(_ context.Context, message *storage.DisplayMessage) error {
	s.display = message
	return s.call("display")
}

func (s *rollCommitTxStub) UpsertState(_ context.Context, value *storage.ConversationState) error {
	s.savedState = value
	return s.call("state")
}

func (s *rollCommitTxStub) CreateRevision(_ context.Context, revision *storage.StateRevision) error {
	s.revision = revision
	return s.call("revision")
}

func rollCommitSchema() gameplay.StateSchema {
	return gameplay.StateSchema{SchemaVersion: 1, Fields: []gameplay.StateField{
		{Path: "profile.level", Type: "enum", Default: "凡人", UpdatePolicy: gameplay.UpdateSystem,
			Constraints: map[string]any{"values": []any{"凡人", "练气期"}}},
	}}
}

func TestCommitRollInTxPersistsOneAtomicFactSet(t *testing.T) {
	convID := uuid.New()
	tx := &rollCommitTxStub{stateErr: storage.ErrNotFound}
	result, err := commitRollInTx(context.Background(), tx, RollCommitInput{
		ConversationID: convID,
		Schema:         rollCommitSchema(),
		Record:         true,
		RollKey:        "cultivation.breakthrough",
		Title:          "突破判定",
		Category:       "cultivation",
		Success:        true,
		Content:        "system result",
		Markdown:       "visible result",
		Metadata:       map[string]any{"roll_key": "cultivation.breakthrough"},
		EffectPatch:    map[string]any{"profile.level": "练气期"},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantCalls := []string{"lock", "get_state", "history", "chat", "display", "state", "revision"}
	if !reflect.DeepEqual(tx.calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", tx.calls, wantCalls)
	}
	if tx.history == nil || !tx.history.Success || tx.history.RollKey != "cultivation.breakthrough" {
		t.Fatalf("history = %+v", tx.history)
	}
	if tx.display == nil || tx.chat == nil || tx.display.ChatMessageID == nil || *tx.display.ChatMessageID != tx.chat.ID {
		t.Fatalf("chat/display relation invalid: chat=%+v display=%+v", tx.chat, tx.display)
	}
	if tx.savedState.State["profile"].(map[string]any)["level"] != "练气期" {
		t.Fatalf("saved state = %+v", tx.savedState.State)
	}
	if tx.revision == nil || tx.revision.RelatedMessageID == nil || *tx.revision.RelatedMessageID != tx.chat.ID {
		t.Fatalf("revision = %+v", tx.revision)
	}
	if result.Display != tx.display || !result.StateChanged {
		t.Fatalf("result = %+v", result)
	}
}

func TestCommitRollInTxStopsAtFirstFailure(t *testing.T) {
	tx := &rollCommitTxStub{failAt: "display", stateErr: storage.ErrNotFound}
	_, err := commitRollInTx(context.Background(), tx, RollCommitInput{
		ConversationID: uuid.New(),
		Schema:         rollCommitSchema(),
		Record:         true,
		EffectPatch:    map[string]any{"profile.level": "练气期"},
	})
	if err == nil {
		t.Fatal("display failure should abort transaction callback")
	}
	wantCalls := []string{"lock", "get_state", "history", "chat", "display"}
	if !reflect.DeepEqual(tx.calls, wantCalls) {
		t.Fatalf("calls after failure = %v, want %v", tx.calls, wantCalls)
	}
	if tx.savedState != nil || tx.revision != nil {
		t.Fatalf("state writes must not run after message failure: state=%+v revision=%+v", tx.savedState, tx.revision)
	}
}

func TestCommitRollInTxGateRejectionDoesNotCreateEvidenceOrState(t *testing.T) {
	tx := &rollCommitTxStub{stateErr: storage.ErrNotFound}
	result, err := commitRollInTx(context.Background(), tx, RollCommitInput{
		ConversationID: uuid.New(),
		Schema:         rollCommitSchema(),
		Content:        "gate rejection",
		Markdown:       "gate rejection",
	})
	if err != nil {
		t.Fatal(err)
	}
	wantCalls := []string{"lock", "get_state", "chat", "display"}
	if !reflect.DeepEqual(tx.calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", tx.calls, wantCalls)
	}
	if tx.history != nil || result.StateChanged {
		t.Fatalf("rejected gate created evidence or state: history=%+v result=%+v", tx.history, result)
	}
}

func TestCommitRollInTxRejectsStateChangedAfterCalculation(t *testing.T) {
	convID := uuid.New()
	tx := &rollCommitTxStub{state: &storage.ConversationState{
		ConversationID: convID,
		State:          storage.JSONMap{"profile": map[string]any{"level": "练气期"}},
	}}
	_, err := commitRollInTx(context.Background(), tx, RollCommitInput{
		ConversationID: convID,
		Schema:         rollCommitSchema(),
		ExpectedState:  map[string]any{"profile": map[string]any{"level": "凡人"}},
		Record:         true,
	})
	if err == nil {
		t.Fatal("stale calculated state must reject the roll commit")
	}
	wantCalls := []string{"lock", "get_state"}
	if !reflect.DeepEqual(tx.calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", tx.calls, wantCalls)
	}
	if tx.history != nil || tx.chat != nil || tx.display != nil {
		t.Fatalf("stale result wrote facts: history=%+v chat=%+v display=%+v", tx.history, tx.chat, tx.display)
	}
}
