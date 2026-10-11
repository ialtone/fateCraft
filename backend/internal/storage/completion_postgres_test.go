package storage

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/testdb"
	"testing"
)

func TestPostgresCompletionAtomicReplayAndBranchIsolation(t *testing.T) {
	db := testdb.Open(t, "completion_receipt")
	ctx := context.Background()
	id := seedGenerationTestConversation(t, db)
	var conv Conversation
	if err := db.First(&conv, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewChallengeRepository(db)
	input := CommitModuleActionInput{ConversationID: id, PlayerID: uuid.MustParse(conv.PlayerID), RequestID: uuid.New(), BranchEpoch: conv.BranchEpoch, ActionKey: "test.exit", ArgumentsHash: "exit", Label: "主动退出"}
	view := NewCompletion(conv, 1, "test", ChallengeOutcomeRule{OutcomeKey: "abandoned", Category: "neutral", Title: "中止"})
	calls := 0
	execute := func(ctx context.Context, _ *StateRepo, bound *ChallengeRepository, locked *Conversation) (JSONMap, error) {
		calls++
		if err := bound.EndModuleConversation(ctx, locked.ID); err != nil {
			return nil, err
		}
		return JSONMap{"completion": view}, nil
	}
	failed := input
	failed.RequestID = uuid.New()
	_, err := repo.CommitModuleAction(ctx, failed, func(ctx context.Context, _ *StateRepo, bound *ChallengeRepository, locked *Conversation) (JSONMap, error) {
		if err := bound.EndModuleConversation(ctx, locked.ID); err != nil {
			return nil, err
		}
		return nil, errors.New("injected failure")
	})
	if err == nil {
		t.Fatal("failure accepted")
	}
	if err := db.First(&conv, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	if conv.Status != StatusActive {
		t.Fatal("failed transaction closed conversation")
	}
	for i := 0; i < 2; i++ {
		if _, err := repo.CommitModuleAction(ctx, input, execute); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("executed %d times", calls)
	}
	if err := db.First(&conv, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	restored, err := repo.Completion(ctx, conv)
	if err != nil || restored == nil || restored.ID != view.ID {
		t.Fatalf("receipt not restored: %+v %v", restored, err)
	}
	conv.BranchEpoch++
	restored, err = repo.Completion(ctx, conv)
	if err != nil || restored != nil {
		t.Fatalf("stale branch receipt: %+v %v", restored, err)
	}
}
