package storage

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/testdb"
	"gorm.io/gorm"
	"testing"
)

func TestPostgresModuleActionReplayRewindAndAtomicFailure(t *testing.T) {
	db := testdb.Open(t, "module_action_restore")
	id := seedGenerationTestConversation(t, db)
	var conv Conversation
	if err := db.First(&conv, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	playerID := uuid.MustParse(conv.PlayerID)
	campaignID := uuid.New()
	row := Campaign{ID: campaignID, PlayerID: playerID, Title: "Module test", ChallengeVersionID: conv.ChallengeVersionID, ChallengeHash: conv.ChallengeHash, Status: CampaignStatusActive, CurrentLocationKey: "start", RandomSeed: 1, LLMRoute: LLMRouteServer}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&Conversation{}).Where("id = ?", id).Update("campaign_id", campaignID).Error; err != nil {
		t.Fatal(err)
	}
	initial := JSONMap{"schema_version": 1, "revision": 0, "game_time": 0, "status": "active", "data": map[string]any{"stock": 3}}
	if err := InitializeCampaignRuntimeState(db, campaignID, initial); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&ConversationState{ConversationID: id, State: JSONMap{}, SchemaVersion: 1}).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewChallengeRepository(db)
	calls := 0
	input := CommitModuleActionInput{ConversationID: id, PlayerID: playerID, RequestID: uuid.New(), ActionKey: "test.spend", ArgumentsHash: "a", Label: "消耗物资"}
	execute := func(ctx context.Context, _ *StateRepo, bound *ChallengeRepository, locked *Conversation) (JSONMap, error) {
		calls++
		result, err := bound.CampaignActions().CommitAction(ctx, CommitCampaignActionInput{CampaignID: campaignID, PlayerID: playerID, ExpectedRevision: 0, IdempotencyKey: "first", ActionKey: input.ActionKey, ActorType: "player"}, func(store *CampaignActionStore, row Campaign) (CampaignActionOutcome, error) {
			next := JSONMap{"schema_version": 1, "revision": 1, "game_time": 60, "status": "active", "data": map[string]any{"stock": 2}}
			if err := store.SaveRuntimeState(next); err != nil {
				return CampaignActionOutcome{}, err
			}
			if _, _, err := store.GrantCampaignStageReward(ctx, row, "early", "阶段", 5); err != nil {
				return CampaignActionOutcome{}, err
			}
			_, err := NewRewardRepository(bound.db).GrantInCurrentTx(ctx, RewardMutation{PlayerID: playerID, RewardKey: PointsKey, Quantity: 12, SourceType: "admin_grant", SourceID: campaignID.String(), IdempotencyKey: "checkpoint-earned", Metadata: JSONMap{}})
			return CampaignActionOutcome{GameTimeDelta: 60, Events: []CampaignEventDraft{{EventType: "test.spent", Visibility: CampaignVisibilityPlayer}}, Result: JSONMap{"stock": 2}}, err
		})
		if err != nil {
			return nil, err
		}
		return JSONMap{"revision": result.Campaign.Revision}, nil
	}
	if _, err := repo.CommitModuleAction(context.Background(), input, execute); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CommitModuleAction(context.Background(), input, execute); err != nil || calls != 1 {
		t.Fatalf("replay executed action %d times: %v", calls, err)
	}
	conflicting := input
	conflicting.ArgumentsHash = "b"
	if _, err := repo.CommitModuleAction(context.Background(), conflicting, execute); !errors.Is(err, ErrModuleActionConflict) {
		t.Fatalf("mismatched retry accepted: %v", err)
	}
	var commit ModuleActionCommit
	if err := db.First(&commit, "conversation_id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, err := prepareBranchInTx(context.Background(), tx, id, commit.TurnID, nil, JSONMap{}, "rewrite")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&row, "id = ?", campaignID).Error; err != nil {
		t.Fatal(err)
	}
	var runtime CampaignRuntimeState
	if err := db.First(&runtime, "campaign_id = ?", campaignID).Error; err != nil {
		t.Fatal(err)
	}
	if row.GameTime != 0 || row.Revision != 2 || runtime.State["revision"] != float64(2) || runtime.State["data"].(map[string]any)["stock"] != float64(3) {
		t.Fatalf("checkpoint failed to restore state with new revision: %+v %+v", row, runtime)
	}
	events, err := NewCampaignRepository(db).RecentEvents(context.Background(), campaignID, nil, 100)
	if err != nil || len(events) != 0 {
		t.Fatalf("invalidated events leaked: %+v %v", events, err)
	}
	var historyCount int64
	db.Model(&CampaignEvent{}).Where("campaign_id = ?", campaignID).Count(&historyCount)
	if historyCount != 1 {
		t.Fatal("append-only history deleted")
	}
	var quantity int64
	if err := db.Model(&RewardLedgerEntry{}).Where("player_id = ? AND reward_key = ?", playerID, PointsKey).Select("COALESCE(SUM(quantity_delta),0)").Scan(&quantity).Error; err != nil {
		t.Fatal(err)
	}
	if quantity != 17 {
		t.Fatal("rewind changed account ledger")
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		store := &CampaignActionStore{tx: tx}
		claim, applied, err := store.GrantCampaignStageReward(context.Background(), row, "early", "阶段", 5)
		if err != nil {
			return err
		}
		if applied || claim.GrantedPoints != 5 {
			return errors.New("rewind recreated stage entitlement")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CommitModuleAction(context.Background(), input, execute); !errors.Is(err, ErrModuleActionConflict) {
		t.Fatalf("old branch retry accepted: %v", err)
	}
	var turnCount int64
	db.Model(&ConversationTurn{}).Where("conversation_id = ?", id).Count(&turnCount)
	failure := input
	failure.BranchEpoch = 1
	failure.RequestID = uuid.New()
	injected := errors.New("injected failure")
	_, err = repo.CommitModuleAction(context.Background(), failure, func(_ context.Context, _ *StateRepo, bound *ChallengeRepository, _ *Conversation) (JSONMap, error) {
		if err := bound.db.Model(&CampaignRuntimeState{}).Where("campaign_id = ?", campaignID).Update("state", JSONMap{}).Error; err != nil {
			return nil, err
		}
		return nil, injected
	})
	if !errors.Is(err, injected) {
		t.Fatal(err)
	}
	var afterCount int64
	db.Model(&ConversationTurn{}).Where("conversation_id = ?", id).Count(&afterCount)
	if afterCount != turnCount {
		t.Fatal("failed action left a turn")
	}
	if err := db.First(&runtime, "campaign_id = ?", campaignID).Error; err != nil {
		t.Fatal(err)
	}
	if runtime.State["revision"] != float64(2) {
		t.Fatal("failed action mutated state")
	}
}
