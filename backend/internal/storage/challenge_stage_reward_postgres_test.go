package storage

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/operation"
	"github.com/ialtone/fatecraft/internal/testdb"
	"gorm.io/gorm"
)

func seedChallengeStageConversation(t *testing.T, db *gorm.DB, route string) (Conversation, uuid.UUID) {
	t.Helper()
	playerID, packID, versionID, conversationID, turnID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	hash := strings.Repeat("0", 64)
	suffix := uuid.NewString()
	slug := "stage-test-" + suffix
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO users (id, username, display_name) VALUES (?, ?, 'Stage Test')`, []any{playerID, "stage-" + suffix[:8]}},
		{`INSERT INTO challenge_packs (id, slug, name) VALUES (?, ?, 'Stage Test')`, []any{packID, slug}},
		{`INSERT INTO challenge_versions (id, challenge_pack_id, version, status, manifest, content_hash, published_at) VALUES (?, ?, '1.0.0', 'published', '{}'::jsonb, ?, CURRENT_TIMESTAMP)`, []any{versionID, packID, hash}},
		{`UPDATE challenge_packs SET current_version_id = ?, status = 'active' WHERE id = ?`, []any{versionID, packID}},
		{`INSERT INTO conversations (id, player_id, title, challenge_version_id, challenge_hash, llm_route) VALUES (?, ?, ?, ?, ?, ?)`, []any{conversationID, playerID, route, versionID, hash, route}},
		{`INSERT INTO conversation_turns (id, conversation_id, turn_seq, trigger_type, status) VALUES (?, ?, 1, 'module_action', 'completed')`, []any{turnID, conversationID}},
	}
	for _, statement := range statements {
		if err := db.Exec(statement.query, statement.args...).Error; err != nil {
			t.Fatalf("seed challenge stage conversation: %v", err)
		}
	}
	var conv Conversation
	if err := db.First(&conv, "id = ?", conversationID).Error; err != nil {
		t.Fatal(err)
	}
	return conv, turnID
}

func TestPostgresChallengeStageRewardsAreAtomicIdempotentAndClientZero(t *testing.T) {
	db := testdb.Open(t, "challenge_stages")
	ctx := context.Background()
	for _, route := range []string{LLMRouteServer, LLMRouteClient} {
		conv, turnID := seedChallengeStageConversation(t, db, route)
		bound := operation.WithMetadata(ctx, operation.Metadata{ConversationID: conv.ID, TurnID: turnID, BranchEpoch: conv.BranchEpoch})
		repo := NewChallengeRepository(db)
		grant := func(key string, order int, points int64) (bool, error) {
			var created bool
			err := repo.WithTx(bound, func(tx *ChallengeRepository) error {
				var err error
				_, created, err = tx.GrantStageReward(bound, conv, StageRewardGrant{Key: key, Name: key, SortOrder: order, Points: points, ActionKey: "trial.breakthrough"})
				return err
			})
			return created, err
		}

		if err := repo.WithTx(bound, func(tx *ChallengeRepository) error {
			if _, _, err := tx.GrantStageReward(bound, conv, StageRewardGrant{Key: "rolled-back", Name: "rolled-back", Points: 25}); err != nil {
				return err
			}
			return errors.New("injected failure")
		}); err == nil {
			t.Fatal("injected failure did not abort")
		}
		var count int64
		db.Model(&ChallengeStageReward{}).Where("conversation_id = ?", conv.ID).Count(&count)
		if count != 0 {
			t.Fatalf("stage fact survived rollback: %d", count)
		}

		var wg sync.WaitGroup
		results := make(chan error, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := grant("first", 10, 25)
				results <- err
			}()
		}
		wg.Wait()
		close(results)
		for err := range results {
			if err != nil {
				t.Fatalf("concurrent grant: %v", err)
			}
		}
		if ok, err := grant("second", 20, 25); err != nil || !ok {
			t.Fatalf("second stage created=%v err=%v", ok, err)
		}
		if ok, err := grant("first", 10, 25); err != nil || ok {
			t.Fatalf("replayed stage created=%v err=%v", ok, err)
		}

		rows, err := repo.StageRewards(bound, conv.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 2 || rows[0].StageKey != "first" || rows[1].StageKey != "second" {
			t.Fatalf("stage facts: %#v", rows)
		}
		points, err := repo.StageRewardPoints(bound, conv.ID)
		if err != nil {
			t.Fatal(err)
		}
		var ledgerCount int64
		db.Model(&RewardLedgerEntry{}).Where("source_type = 'challenge_stage' AND source_id LIKE ?", conv.ID.String()+":%").Count(&ledgerCount)
		if route == LLMRouteClient {
			if points != 0 || ledgerCount != 0 {
				t.Fatalf("client route points=%d ledger=%d", points, ledgerCount)
			}
			for _, row := range rows {
				if row.GrantedPoints != 0 || row.DisabledReason != RewardsDisabledReasonClientRoute || row.ConfiguredPoints != 25 {
					t.Fatalf("client stage fact: %#v", row)
				}
			}
		} else {
			if points != 50 || ledgerCount != 2 {
				t.Fatalf("server route points=%d ledger=%d", points, ledgerCount)
			}
			checkRewardBalance(t, db, uuid.MustParse(conv.PlayerID), PointsKey, 50)
			var entries []RewardLedgerEntry
			if err := db.Where("source_type = 'challenge_stage' AND source_id LIKE ?", conv.ID.String()+":%").Find(&entries).Error; err != nil {
				t.Fatal(err)
			}
			source := rewardGameplayProjection(entries[0])
			if source == nil || source.StageKey == "" || source.StageName == "" || source.ConversationID == nil || *source.ConversationID != conv.ID || source.VersionID != conv.ChallengeVersionID {
				t.Fatalf("ledger gameplay projection: %#v", source)
			}
		}

		if err := db.Model(&ChallengeStageReward{}).Where("conversation_id = ?", conv.ID).Delete(&ChallengeStageReward{}).Error; err == nil {
			t.Fatal("stage facts are not append-only")
		}
	}

	unboundConv, _ := seedChallengeStageConversation(t, db, LLMRouteServer)
	if err := NewChallengeRepository(db).WithTx(ctx, func(tx *ChallengeRepository) error {
		_, _, err := tx.GrantStageReward(ctx, unboundConv, StageRewardGrant{Key: "first", Name: "first", Points: 25})
		return err
	}); err == nil {
		t.Fatal("stage grant without turn metadata accepted")
	}
}

func TestPostgresChallengeStageRewardRejectsConflictingSnapshot(t *testing.T) {
	db := testdb.Open(t, "challenge_stages")
	conv, turnID := seedChallengeStageConversation(t, db, LLMRouteServer)
	bound := operation.WithMetadata(context.Background(), operation.Metadata{ConversationID: conv.ID, TurnID: turnID, BranchEpoch: conv.BranchEpoch})
	repo := NewChallengeRepository(db)
	if err := repo.WithTx(bound, func(tx *ChallengeRepository) error {
		_, _, err := tx.GrantStageReward(bound, conv, StageRewardGrant{Key: "first", Name: "first", SortOrder: 10, Points: 25})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	err := repo.WithTx(bound, func(tx *ChallengeRepository) error {
		_, _, err := tx.GrantStageReward(bound, conv, StageRewardGrant{Key: "first", Name: "renamed", SortOrder: 10, Points: 25})
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "不一致") {
		t.Fatalf("conflicting snapshot accepted: %v", err)
	}
	var count int64
	db.Model(&ChallengeStageReward{}).Where("conversation_id = ?", conv.ID).Count(&count)
	if count != 1 {
		t.Fatalf("stage facts = %d", count)
	}
	if err := db.Model(&ChallengeStageReward{}).Where("conversation_id = ?", conv.ID).Update("granted_points", 0).Error; err == nil {
		t.Fatal("stage fact update accepted")
	}
	if count, err := repo.UserTurnCount(bound, conv.ID); err != nil || count != 0 {
		t.Fatalf("user turn count = %d err=%v", count, err)
	}
}
