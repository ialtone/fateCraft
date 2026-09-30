package storage

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"sync/atomic"
	"testing"
)

func TestPostgresDifferentExchangeRequestsCannotOverspend(t *testing.T) {
	db, repo, actor, player := rewardFixture(t)
	ctx := context.Background()
	if _, err := repo.AdminGrant(ctx, actor.ID, player.ID, grantRequest(PointsKey, 100)); err != nil {
		t.Fatal(err)
	}
	offers, err := repo.ListExchangeOffers(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	var completed atomic.Int64
	concurrentRewards(t, 8, func() error {
		_, err := repo.Exchange(ctx, player.ID, RewardExchangeRequest{RequestID: uuid.New(), OfferID: offers[0].ID, Revision: offers[0].Revision, Units: 1})
		if errors.Is(err, ErrInsufficientReward) {
			return nil
		}
		if err == nil {
			completed.Add(1)
		}
		return err
	})
	if completed.Load() != 1 {
		t.Fatalf("completed %d exchanges with funds for one", completed.Load())
	}
	checkRewardBalance(t, db, player.ID, PointsKey, 0)
	checkRewardBalance(t, db, player.ID, MessageRewriteCreditKey, 1)
}

func TestPostgresExchangeAtomicIdentityAndPrice(t *testing.T) {
	db, repo, actor, player := rewardFixture(t)
	ctx := context.Background()
	offers, err := repo.ListExchangeOffers(ctx, false)
	if err != nil || len(offers) != 1 {
		t.Fatalf("seed offer: %+v %v", offers, err)
	}
	offer := offers[0]
	if _, err := repo.AdminGrant(ctx, actor.ID, player.ID, grantRequest(PointsKey, 500)); err != nil {
		t.Fatal(err)
	}
	in := RewardExchangeRequest{RequestID: uuid.New(), OfferID: offer.ID, Revision: offer.Revision, Units: 2}
	concurrentRewards(t, 8, func() error { _, err := repo.Exchange(ctx, player.ID, in); return err })
	checkRewardBalance(t, db, player.ID, PointsKey, 300)
	checkRewardBalance(t, db, player.ID, MessageRewriteCreditKey, 2)
	order, err := repo.ExchangeByRequest(ctx, player.ID, in.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ExchangeByRequest(ctx, actor.ID, in.RequestID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user request query: %v", err)
	}
	changed := in
	changed.Units++
	if _, err := repo.Exchange(ctx, player.ID, changed); !errors.Is(err, ErrRewardRequestConflict) {
		t.Fatalf("changed request: %v", err)
	}
	offer.CostQuantity = 200
	updated, err := repo.SaveExchangeOffer(ctx, actor.ID, offer.ID, offer)
	if err != nil {
		t.Fatal(err)
	}
	changed = in
	changed.RequestID = uuid.New()
	if _, err := repo.Exchange(ctx, player.ID, changed); !errors.Is(err, ErrExchangeChanged) {
		t.Fatalf("stale price: %v", err)
	}
	changed.Revision = updated.Revision
	if _, err := repo.Exchange(ctx, player.ID, changed); !errors.Is(err, ErrInsufficientReward) {
		t.Fatalf("insufficient funds: %v", err)
	}
	changed.Units = MaxRewardQuantity
	if _, err := repo.Exchange(ctx, player.ID, changed); !errors.Is(err, ErrRewardQuantity) {
		t.Fatalf("overflow: %v", err)
	}
	updated.Enabled = false
	if _, err := repo.SaveExchangeOffer(ctx, actor.ID, updated.ID, *updated); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Exchange(ctx, player.ID, in); err != nil {
		t.Fatalf("original replay: %v", err)
	}
	if _, err := repo.ReverseAdminGrant(ctx, actor.ID, order.RewardLedgerID, uuid.New(), "不能单边撤回"); !errors.Is(err, ErrRewardReversal) {
		t.Fatal(err)
	}
	var payment RewardLedgerEntry
	if err := db.First(&payment, "id = ?", order.CostLedgerID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RefundConsumed(ctx, RewardRefund{PlayerID: player.ID, RewardKey: PointsKey, ConsumeIdempotencyKey: payment.IdempotencyKey}); !errors.Is(err, ErrRewardReversal) {
		t.Fatal(err)
	}
	checkRewardBalance(t, db, player.ID, PointsKey, 300)
	checkRewardBalance(t, db, player.ID, MessageRewriteCreditKey, 2)
	var count int64
	if err := db.Model(&RewardExchangeOrder{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("orders=%d", count)
	}
}
func TestPostgresExchangeOrderFailureRollsBackBothBalances(t *testing.T) {
	db, repo, actor, player := rewardFixture(t)
	ctx := context.Background()
	if _, err := repo.AdminGrant(ctx, actor.ID, player.ID, grantRequest(PointsKey, 100)); err != nil {
		t.Fatal(err)
	}
	offers, err := repo.ListExchangeOffers(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE FUNCTION reject_test_order() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test order failure'; END; $$; CREATE TRIGGER reject_test_order BEFORE INSERT ON reward_exchange_orders FOR EACH ROW EXECUTE FUNCTION reject_test_order();`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Exchange(ctx, player.ID, RewardExchangeRequest{RequestID: uuid.New(), OfferID: offers[0].ID, Revision: 1, Units: 1}); err == nil {
		t.Fatal("order failure ignored")
	}
	checkRewardBalance(t, db, player.ID, PointsKey, 100)
	checkRewardBalance(t, db, player.ID, MessageRewriteCreditKey, 0)
}
