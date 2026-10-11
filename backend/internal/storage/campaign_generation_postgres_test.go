package storage

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/operation"
	"github.com/ialtone/fatecraft/internal/testdb"
)

func TestPostgresCampaignSettlingAttemptKeepsGenerationFence(t *testing.T) {
	db := testdb.Open(t, "campaign_fence")
	ctx := context.Background()
	id := seedGenerationTestConversation(t, db)
	var conv Conversation
	if err := db.First(&conv, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	row := Campaign{ID: uuid.New(), PlayerID: uuid.MustParse(conv.PlayerID), ChallengeVersionID: conv.ChallengeVersionID, ChallengeHash: conv.ChallengeHash, Title: "Campaign fence", Status: CampaignStatusActive, LLMRoute: LLMRouteServer, CurrentLocationKey: "start", RandomSeed: 1}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&Conversation{}).Where("id = ?", id).Update("campaign_id", row.ID).Error; err != nil {
		t.Fatal(err)
	}
	_, run, _, err := NewConversationRepo(db).StartGeneration(ctx, id, GenerationRequest{RequestID: uuid.New(), OperationType: "user_input", PayloadHash: "campaign-settle"})
	if err != nil {
		t.Fatal(err)
	}
	runCtx := operation.WithGeneration(ctx, id, run.ID)
	turn, attempt, err := NewTurnRepo(db).Begin(runCtx, id, "user_input")
	if err != nil {
		t.Fatal(err)
	}
	bound := operation.WithMetadata(runCtx, operation.Metadata{ConversationID: id, TurnID: turn.ID, AttemptID: attempt.ID})
	status, key := CampaignStatusCompleted, "finished"
	_, err = NewCampaignRepository(db).CommitAction(bound, CommitCampaignActionInput{CampaignID: row.ID, PlayerID: row.PlayerID, ExpectedRevision: 0, IdempotencyKey: "finish", ActionKey: "test.finish", ActorType: "model"}, func(*CampaignActionStore, Campaign) (CampaignActionOutcome, error) {
		return CampaignActionOutcome{Status: &status, OutcomeKey: &key, Events: []CampaignEventDraft{{EventType: "test.finished", Visibility: CampaignVisibilityPlayer}}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.First(&conv, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	if conv.Status != StatusEnded || conv.EndedAt == nil {
		t.Fatalf("campaign terminal commit did not close conversation: %+v", conv)
	}
	if err := AssertGenerationActive(bound, db, id); err != nil {
		t.Fatalf("finishing Campaign attempt lost its fence: %v", err)
	}
	if err := AssertGenerationActive(runCtx, db, id); !errors.Is(err, ErrGenerationNotActive) {
		t.Fatalf("run without attempt bypassed ended state: %v", err)
	}
	// A different attempt or an obsolete final revision cannot claim this completion.
	if err := db.Model(&Campaign{}).Where("id = ?", row.ID).Update("revision", 2).Error; err != nil {
		t.Fatal(err)
	}
	if err := AssertGenerationActive(bound, db, id); !errors.Is(err, ErrGenerationNotActive) {
		t.Fatalf("old Campaign commit authorized writing: %v", err)
	}
}
