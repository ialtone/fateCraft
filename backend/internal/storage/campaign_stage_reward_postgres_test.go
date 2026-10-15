package storage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/testdb"
)

func TestPostgresCampaignStagesAreAtomicUniqueAndIndependentOfRewind(t *testing.T) {
	db := testdb.Open(t, "campaign_stages")
	id := seedGenerationTestConversation(t, db)
	var conv Conversation
	if err := db.First(&conv, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	for _, route := range []string{LLMRouteServer, LLMRouteClient} {
		row := Campaign{ID: uuid.New(), PlayerID: uuid.MustParse(conv.PlayerID), Title: route, ChallengeVersionID: conv.ChallengeVersionID, ChallengeHash: conv.ChallengeHash, Status: CampaignStatusActive, CurrentLocationKey: "start", RandomSeed: 1, LLMRoute: route}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		conversation := Conversation{
			ID: uuid.New(), PlayerID: conv.PlayerID, Title: route, Status: StatusActive,
			ChallengeVersionID: row.ChallengeVersionID, ChallengeHash: row.ChallengeHash,
			CampaignID: &row.ID, LLMRoute: route,
		}
		if err := db.Create(&conversation).Error; err != nil {
			t.Fatal(err)
		}
		outcomeKey := "stage-test-" + route
		if err := db.Create(&ChallengeOutcomeRule{ID: uuid.New(), ChallengeVersionID: conv.ChallengeVersionID, OutcomeKey: outcomeKey, Category: "success", Title: outcomeKey, Tone: "success", VerifierKey: "core.always", VerifierConfig: JSONMap{}, RewardPoints: 700, Enabled: true}).Error; err != nil {
			t.Fatal(err)
		}
		repo := NewCampaignRepository(db)
		commit := func(key string, revision int64, fail bool) error {
			_, err := repo.CommitAction(context.Background(), CommitCampaignActionInput{CampaignID: row.ID, PlayerID: row.PlayerID, IdempotencyKey: key, ActionKey: "test.stage", ActorType: "system", ExpectedRevision: revision}, func(s *CampaignActionStore, locked Campaign) (CampaignActionOutcome, error) {
				for _, stage := range []struct {
					key    string
					points int64
				}{{"first", 100}, {"second", 100}, {"third", 100}, {"zero", 0}} {
					if _, _, err := s.GrantCampaignStageReward(context.Background(), locked, stage.key, stage.key, stage.points); err != nil {
						return CampaignActionOutcome{}, err
					}
				}
				if fail {
					return CampaignActionOutcome{}, errors.New("injected failure")
				}
				return CampaignActionOutcome{GameTimeDelta: 1, Events: []CampaignEventDraft{{EventType: "test.stage", Visibility: CampaignVisibilityPlayer}}}, nil
			})
			return err
		}
		if err := commit("rollback", 0, true); err == nil {
			t.Fatal("failure did not abort")
		}
		var count int64
		db.Model(&CampaignStageReward{}).Where("campaign_id = ?", row.ID).Count(&count)
		if count != 0 {
			t.Fatal("claim survived rollback")
		}
		var wg sync.WaitGroup
		results := make(chan error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(i int) { defer wg.Done(); results <- commit(fmt.Sprintf("concurrent-%d", i), 0, false) }(i)
		}
		wg.Wait()
		close(results)
		success := 0
		for err := range results {
			if err == nil {
				success++
			}
		}
		if success != 1 {
			t.Fatalf("concurrent successes=%d", success)
		}
		if err := commit("replayed-facts", 1, false); err != nil {
			t.Fatal(err)
		}
		var claims []CampaignStageReward
		if err := db.Where("campaign_id = ?", row.ID).Find(&claims).Error; err != nil {
			t.Fatal(err)
		}
		if len(claims) != 4 {
			t.Fatalf("claims=%d", len(claims))
		}
		var total int64
		for _, claim := range claims {
			total += claim.GrantedPoints
		}
		want := int64(300)
		if route == LLMRouteClient {
			want = 0
		}
		if total != want {
			t.Fatalf("route %s points=%d", route, total)
		}
		db.Model(&RewardLedgerEntry{}).Where("source_type = 'campaign_stage' AND source_id LIKE ?", row.ID.String()+":%").Count(&count)
		ledgerWant := int64(3)
		if route == LLMRouteClient {
			ledgerWant = 0
		}
		if count != ledgerWant {
			t.Fatalf("route=%s ledger rows=%d", route, count)
		}
		finish := func() (*CampaignActionResult, error) {
			return repo.CommitAction(context.Background(), CommitCampaignActionInput{CampaignID: row.ID, PlayerID: row.PlayerID, ExpectedRevision: 2, IdempotencyKey: "finish", ActionKey: "test.finish", ActorType: "system"}, func(s *CampaignActionStore, locked Campaign) (CampaignActionOutcome, error) {
				if _, err := s.GrantCampaignOutcomeRewards(context.Background(), locked, outcomeKey); err != nil {
					return CampaignActionOutcome{}, err
				}
				status := CampaignStatusCompleted
				return CampaignActionOutcome{Status: &status, OutcomeKey: &outcomeKey, Events: []CampaignEventDraft{{EventType: "test.finished", Visibility: CampaignVisibilityPlayer}}}, nil
			})
		}
		if _, err := finish(); err != nil {
			t.Fatal(err)
		}
		if result, err := finish(); err != nil || !result.Replayed {
			t.Fatalf("finish replay failed: %+v %v", result, err)
		}
		if err := db.First(&conversation, "id = ?", conversation.ID).Error; err != nil {
			t.Fatal(err)
		}
		if conversation.Status != StatusEnded {
			t.Fatal("campaign completion did not end its conversation")
		}
		outcomeRewards, err := loadCampaignOutcomeRewards(context.Background(), db, row)
		if err != nil {
			t.Fatal(err)
		}
		for _, reward := range outcomeRewards {
			total += reward.Quantity
		}
		fullWant := int64(1000)
		if route == LLMRouteClient {
			fullWant = 0
		}
		if total != fullWant {
			t.Fatalf("complete route %s paid %d", route, total)
		}
		if err := db.Model(&CampaignStageReward{}).Where("campaign_id = ?", row.ID).Update("granted_points", 1).Error; err == nil {
			t.Fatal("immutable claim changed")
		}
	}
	for _, table := range campaignCheckpointTables {
		if table == "campaign_stage_rewards" {
			t.Fatal("paid stages must not rewind")
		}
	}
}
