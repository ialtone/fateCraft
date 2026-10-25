package storage

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPostgresFixedPointsSettlementIsAtomicAndOnce(t *testing.T) {
	db, _, _, player := rewardFixture(t)
	ctx := context.Background()
	pack, version, outcome := uuid.New(), uuid.New(), uuid.New()
	hash := strings.Repeat("0", 64)
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO challenge_packs(id,slug,name) VALUES (?, 'points-test', 'Points')`, []any{pack}},
		{`INSERT INTO challenge_versions(id,challenge_pack_id,version,status,manifest,content_hash,published_at) VALUES (?,?,'1.0.0','published','{}',?,CURRENT_TIMESTAMP)`, []any{version, pack, hash}},
		{`UPDATE challenge_packs SET current_version_id = ?, status = 'active' WHERE id = ?`, []any{version, pack}},
		{`INSERT INTO challenge_outcome_rules(id,challenge_version_id,outcome_key,category,title,verifier_key,verifier_config,reward_points) VALUES (?,?,'end','success','End','core.always','{}',100)`, []any{outcome, version}},
	} {
		if err := db.Exec(statement.sql, statement.args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	newConversation := func(route string) uuid.UUID {
		t.Helper()
		id := uuid.New()
		if err := db.Exec(`INSERT INTO conversations(id,player_id,title,challenge_version_id,challenge_hash,llm_route) VALUES (?,?,'Points',?,?,?)`, id, player.ID, version, hash, route).Error; err != nil {
			t.Fatal(err)
		}
		return id
	}
	aborted := errors.New("abort settlement source transaction")
	settle := func(id uuid.UUID, points int64, rollback bool) error {
		return NewChallengeRepository(db).WithTx(ctx, func(tx *ChallengeRepository) error {
			conv, err := tx.LockConversation(ctx, id)
			if err != nil {
				return err
			}
			actual := points
			if conv.LLMRoute == LLMRouteClient {
				actual = 0
			}
			run := ChallengeRun{ID: uuid.New(), ConversationID: id, PlayerID: player.ID, ChallengeVersionID: version, OutcomeRuleID: outcome, OutcomeKey: "end", Category: "success", TitleSnapshot: "End", Summary: "done", SettlementDetail: JSONMap{"reward_points": actual}, SettledAt: time.Now()}
			if _, err := tx.CompleteRun(ctx, conv, &run, points); err != nil {
				return err
			}
			if rollback {
				return aborted
			}
			return nil
		})
	}
	rolledBack := newConversation(LLMRouteServer)
	if err := settle(rolledBack, 100, true); !errors.Is(err, aborted) {
		t.Fatal(err)
	}
	checkRewardBalance(t, db, player.ID, PointsKey, 0)
	var count int64
	if err := db.Model(&ChallengeRun{}).Where("conversation_id = ?", rolledBack).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("rollback retained run: %d %v", count, err)
	}
	var conv Conversation
	if err := db.First(&conv, "id = ?", rolledBack).Error; err != nil || conv.Status != StatusActive {
		t.Fatalf("rollback ended conversation: %s %v", conv.Status, err)
	}
	var applied atomic.Int64
	concurrentRewards(t, 6, func() error {
		err := settle(rolledBack, 100, false)
		if errors.Is(err, ErrAlreadySettled) {
			return nil
		}
		if err == nil {
			applied.Add(1)
		}
		return err
	})
	if applied.Load() != 1 {
		t.Fatalf("settled %d times", applied.Load())
	}
	checkRewardBalance(t, db, player.ID, PointsKey, 100)
	for _, route := range []string{LLMRouteServer, LLMRouteClient} {
		id := newConversation(route)
		points := int64(0)
		if route == LLMRouteClient {
			points = 100
		}
		if err := settle(id, points, false); err != nil {
			t.Fatal(err)
		}
		var run ChallengeRun
		if err := db.First(&run, "conversation_id = ?", id).Error; err != nil {
			t.Fatal(err)
		}
		if run.SettlementDetail["reward_points"] != float64(0) {
			t.Fatalf("zero/client settlement points: %+v", run.SettlementDetail)
		}
		if err := db.Model(&ChallengeRunReward{}).Where("challenge_run_id = ?", run.ID).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("zero/client reward: %d %v", count, err)
		}
	}
	checkRewardBalance(t, db, player.ID, PointsKey, 100)
}

func TestPostgresEndingRewardsAreRepeatableAcrossSettlements(t *testing.T) {
	db, _, _, player := rewardFixture(t)
	ctx := context.Background()
	pack, v1, v2 := uuid.New(), uuid.New(), uuid.New()
	rules := map[string]uuid.UUID{"v1:end": uuid.New(), "v1:other": uuid.New(), "v2:end": uuid.New()}
	hash := strings.Repeat("0", 64)
	for _, statement := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO challenge_packs(id,slug,name) VALUES (?, 'cap-test', 'Cap')`, []any{pack}},
		{`INSERT INTO challenge_versions(id,challenge_pack_id,version,status,manifest,content_hash,published_at) VALUES (?,?,'1.0.0','published','{}',?,CURRENT_TIMESTAMP)`, []any{v1, pack, hash}},
		{`INSERT INTO challenge_versions(id,challenge_pack_id,version,status,manifest,content_hash,published_at) VALUES (?,?,'1.0.1','published','{}',?,CURRENT_TIMESTAMP)`, []any{v2, pack, hash}},
		{`UPDATE challenge_packs SET current_version_id = ?, status = 'active' WHERE id = ?`, []any{v1, pack}},
		{`INSERT INTO challenge_outcome_rules(id,challenge_version_id,outcome_key,category,title,verifier_key,verifier_config,reward_points) VALUES (?,?,'end','success','End','core.always','{}',100)`, []any{rules["v1:end"], v1}},
		{`INSERT INTO challenge_outcome_rules(id,challenge_version_id,outcome_key,category,title,verifier_key,verifier_config,reward_points) VALUES (?,?,'other','success','Other','core.always','{}',100)`, []any{rules["v1:other"], v1}},
		{`INSERT INTO challenge_outcome_rules(id,challenge_version_id,outcome_key,category,title,verifier_key,verifier_config,reward_points) VALUES (?,?,'end','success','End','core.always','{}',100)`, []any{rules["v2:end"], v2}},
	} {
		if err := db.Exec(statement.sql, statement.args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	settle := func(version uuid.UUID, outcomeKey string) []ChallengeRunReward {
		outcome := rules["v1:"+outcomeKey]
		if version == v2 {
			outcome = rules["v2:"+outcomeKey]
		}
		t.Helper()
		id := uuid.New()
		if err := db.Exec(`INSERT INTO conversations(id,player_id,title,challenge_version_id,challenge_hash,llm_route) VALUES (?,?,'Cap',?,?,?)`, id, player.ID, version, hash, LLMRouteServer).Error; err != nil {
			t.Fatal(err)
		}
		var rewards []ChallengeRunReward
		err := NewChallengeRepository(db).WithTx(ctx, func(tx *ChallengeRepository) error {
			conv, err := tx.LockConversation(ctx, id)
			if err != nil {
				return err
			}
			run := ChallengeRun{ID: uuid.New(), ConversationID: id, PlayerID: player.ID, ChallengeVersionID: version, OutcomeRuleID: outcome, OutcomeKey: outcomeKey, Category: "success", TitleSnapshot: "End", Summary: "done", SettlementDetail: JSONMap{"reward_points": 100}, SettledAt: time.Now()}
			rewards, err = tx.CompleteRun(ctx, conv, &run, 100)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return rewards
	}
	if got := settle(v1, "end"); len(got) != 1 {
		t.Fatalf("first clear must be rewarded: %+v", got)
	}
	if got := settle(v1, "end"); len(got) != 1 {
		t.Fatalf("repeat clear must be rewarded again: %+v", got)
	}
	if err := db.Exec(`UPDATE challenge_packs SET current_version_id = ? WHERE id = ?`, v2, pack).Error; err != nil {
		t.Fatal(err)
	}
	if got := settle(v2, "end"); len(got) != 1 {
		t.Fatalf("a new version of the same pack must be rewarded: %+v", got)
	}
	if err := db.Exec(`UPDATE challenge_packs SET current_version_id = ? WHERE id = ?`, v1, pack).Error; err != nil {
		t.Fatal(err)
	}
	if got := settle(v1, "other"); len(got) != 1 {
		t.Fatalf("a different ending must be rewarded: %+v", got)
	}
	checkRewardBalance(t, db, player.ID, PointsKey, 400)
	var zero int64
	if err := db.Model(&ChallengeRun{}).Where("player_id = ? AND settlement_detail->>'reward_points' = '0'", player.ID).Count(&zero).Error; err != nil || zero != 0 {
		t.Fatalf("zero-point runs = %d err=%v", zero, err)
	}
}
