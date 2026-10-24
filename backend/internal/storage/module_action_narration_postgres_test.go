package storage

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/testdb"
	"gorm.io/gorm"
)

func TestPostgresModuleNarrationFollowsValidBranch(t *testing.T) {
	db := testdb.Open(t, "module_narration")
	id := seedGenerationTestConversation(t, db)
	ctx := context.Background()
	var conv Conversation
	if err := db.First(&conv, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewChallengeRepository(db)
	commit := func() ModuleActionCommit {
		t.Helper()
		input := CommitModuleActionInput{ConversationID: id, PlayerID: uuid.MustParse(conv.PlayerID), RequestID: uuid.New(), ActionKey: "test.action", ArgumentsHash: "hash", Label: "动作"}
		if _, err := repo.CommitModuleAction(ctx, input, func(context.Context, *StateRepo, *ChallengeRepository, *Conversation) (JSONMap, error) {
			return JSONMap{}, nil
		}); err != nil {
			t.Fatal(err)
		}
		var row ModuleActionCommit
		if err := db.First(&row, "request_id = ?", input.RequestID).Error; err != nil {
			t.Fatal(err)
		}
		return row
	}
	a, b := commit(), commit()
	turns := NewTurnRepo(db)
	narration, attempt, err := turns.Begin(ctx, id, "module_action")
	if err != nil {
		t.Fatal(err)
	}
	if err := turns.SealStatus(ctx, narration.ID, attempt.ID, AttemptStatusCompleted, "", ""); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkModuleActionsNarrated(ctx, id, narration.ID, []uuid.UUID{a.ID}); err != nil {
		t.Fatal(err)
	}
	c := commit()
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, err := prepareBranchInTx(ctx, tx, id, narration.ID, nil, JSONMap{}, "rewrite")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.UnnarratedModuleActions(ctx, id, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != a.ID || rows[1].ID != b.ID {
		t.Fatalf("want retained actions %s and %s, excluding invalidated %s; got %#v", a.ID, b.ID, c.ID, rows)
	}
	if rows[0].NarratedTurnID != nil || rows[0].NarratedAt != nil {
		t.Fatal("invalidated narration did not release consumed action")
	}
}
