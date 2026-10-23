package storage

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"sync"
	"sync/atomic"
	"testing"
)

func TestPostgresDifferentExchangeRequestsCannotOverspend(t *testing.T) {
	db, repo, actor, player := rewardFixture(t)
	ctx := context.Background()
	addStock(t, db, seedOfferID, 50)
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
	addStock(t, db, seedOfferID, 50)
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
	if _, err := repo.ReverseAdminGrant(ctx, actor.ID, *order.RewardLedgerID, uuid.New(), "不能单边撤回"); !errors.Is(err, ErrRewardReversal) {
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
	addStock(t, db, seedOfferID, 50)
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

func saveExchangeLimit(t *testing.T, repo *RewardRepository, actor User, offer RewardExchangeOffer, limit int64) *RewardExchangeOffer {
	t.Helper()
	offer.MaxRedemptionsPerUser = limit
	saved, err := repo.SaveExchangeOffer(context.Background(), actor.ID, offer.ID, offer)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func TestPostgresExchangePerUserLimitCountsUnitsAcrossRevisions(t *testing.T) {
	db, repo, actor, player := rewardFixture(t)
	ctx := context.Background()
	addStock(t, db, seedOfferID, 50)
	if _, err := repo.AdminGrant(ctx, actor.ID, player.ID, grantRequest(PointsKey, 1000)); err != nil {
		t.Fatal(err)
	}
	offers, err := repo.ListExchangeOffers(ctx, false)
	if err != nil || len(offers) != 1 {
		t.Fatalf("seed offer: %+v %v", offers, err)
	}
	if offers[0].MaxRedemptionsPerUser != 0 {
		t.Fatalf("seed limit = %d, want unlimited", offers[0].MaxRedemptionsPerUser)
	}
	offer := saveExchangeLimit(t, repo, actor, offers[0], 3)
	first := RewardExchangeRequest{RequestID: uuid.New(), OfferID: offer.ID, Revision: offer.Revision, Units: 2}
	order, err := repo.Exchange(ctx, player.ID, first)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Exchange(ctx, player.ID, RewardExchangeRequest{RequestID: uuid.New(), OfferID: offer.ID, Revision: offer.Revision, Units: 2}); !errors.Is(err, ErrExchangeLimitReached) {
		t.Fatalf("over-limit exchange: %v", err)
	}
	checkRewardBalance(t, db, player.ID, PointsKey, 800)
	checkRewardBalance(t, db, player.ID, MessageRewriteCreditKey, 2)
	replay, err := repo.Exchange(ctx, player.ID, first)
	if err != nil || replay.ID != order.ID {
		t.Fatalf("replay after limit: %+v %v", replay, err)
	}
	if _, err := repo.Exchange(ctx, player.ID, RewardExchangeRequest{RequestID: uuid.New(), OfferID: offer.ID, Revision: offer.Revision, Units: 1}); err != nil {
		t.Fatal(err)
	}
	var orders int64
	if err := db.Model(&RewardExchangeOrder{}).Where("player_id = ?", player.ID).Count(&orders).Error; err != nil {
		t.Fatal(err)
	}
	if orders != 2 {
		t.Fatalf("orders=%d, want 2", orders)
	}
	offer = saveExchangeLimit(t, repo, actor, *offer, 2)
	if _, err := repo.Exchange(ctx, player.ID, RewardExchangeRequest{RequestID: uuid.New(), OfferID: offer.ID, Revision: offer.Revision, Units: 1}); !errors.Is(err, ErrExchangeLimitReached) {
		t.Fatalf("lowered limit below redeemed: %v", err)
	}
	offer = saveExchangeLimit(t, repo, actor, *offer, 0)
	if _, err := repo.Exchange(ctx, player.ID, RewardExchangeRequest{RequestID: uuid.New(), OfferID: offer.ID, Revision: offer.Revision, Units: 1}); err != nil {
		t.Fatalf("unlimited after raised config: %v", err)
	}
	usage, err := repo.ExchangeUsage(ctx, player.ID)
	if err != nil {
		t.Fatal(err)
	}
	if usage[offer.ID] != 4 {
		t.Fatalf("redeemed units = %d, want 4", usage[offer.ID])
	}
	checkRewardBalance(t, db, player.ID, PointsKey, 600)
	checkRewardBalance(t, db, player.ID, MessageRewriteCreditKey, 4)
}

func TestPostgresExchangePerUserLimitIsolatedByOfferAndPlayer(t *testing.T) {
	db, repo, actor, player := rewardFixture(t)
	ctx := context.Background()
	addStock(t, db, seedOfferID, 50)
	other := User{ID: uuid.New(), Username: "other", DisplayName: "另一玩家", Role: UserRoleUser, Status: UserStatusActive}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	for _, user := range []User{player, other} {
		if _, err := repo.AdminGrant(ctx, actor.ID, user.ID, grantRequest(PointsKey, 1000)); err != nil {
			t.Fatal(err)
		}
	}
	offers, err := repo.ListExchangeOffers(ctx, false)
	if err != nil || len(offers) != 1 {
		t.Fatalf("seed offer: %+v %v", offers, err)
	}
	first := saveExchangeLimit(t, repo, actor, offers[0], 1)
	second, err := repo.SaveExchangeOffer(ctx, actor.ID, uuid.Nil, RewardExchangeOffer{
		Name: "第二个商品", Description: "", Kind: ExchangeKindReward, CostQuantity: 100, RewardQuantity: 1,
		MaxRedemptionsPerUser: 1, Enabled: true, SortOrder: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	addStock(t, db, second.ID, 5)
	for _, offer := range []RewardExchangeOffer{*first, *second} {
		if _, err := repo.Exchange(ctx, player.ID, RewardExchangeRequest{RequestID: uuid.New(), OfferID: offer.ID, Revision: offer.Revision, Units: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.Exchange(ctx, player.ID, RewardExchangeRequest{RequestID: uuid.New(), OfferID: first.ID, Revision: first.Revision, Units: 1}); !errors.Is(err, ErrExchangeLimitReached) {
		t.Fatalf("same offer exceeded: %v", err)
	}
	if _, err := repo.Exchange(ctx, other.ID, RewardExchangeRequest{RequestID: uuid.New(), OfferID: first.ID, Revision: first.Revision, Units: 1}); err != nil {
		t.Fatalf("other player quota should be independent: %v", err)
	}
	usage, err := repo.ExchangeUsage(ctx, player.ID)
	if err != nil {
		t.Fatal(err)
	}
	if usage[first.ID] != 1 || usage[second.ID] != 1 {
		t.Fatalf("player usage = %+v", usage)
	}
	otherUsage, err := repo.ExchangeUsage(ctx, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if otherUsage[first.ID] != 1 || otherUsage[second.ID] != 0 {
		t.Fatalf("other usage = %+v", otherUsage)
	}
}

func TestPostgresExchangePerUserLimitConcurrentLastUnit(t *testing.T) {
	db, repo, actor, player := rewardFixture(t)
	ctx := context.Background()
	addStock(t, db, seedOfferID, 50)
	if _, err := repo.AdminGrant(ctx, actor.ID, player.ID, grantRequest(PointsKey, 1000)); err != nil {
		t.Fatal(err)
	}
	offers, err := repo.ListExchangeOffers(ctx, false)
	if err != nil || len(offers) != 1 {
		t.Fatalf("seed offer: %+v %v", offers, err)
	}
	offer := saveExchangeLimit(t, repo, actor, offers[0], 1)
	var success, limited atomic.Int64
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.Exchange(ctx, player.ID, RewardExchangeRequest{RequestID: uuid.New(), OfferID: offer.ID, Revision: offer.Revision, Units: 1})
			switch {
			case err == nil:
				success.Add(1)
			case errors.Is(err, ErrExchangeLimitReached):
				limited.Add(1)
			default:
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if success.Load() != 1 || limited.Load() != 1 {
		t.Fatalf("success=%d limited=%d, want 1/1", success.Load(), limited.Load())
	}
	checkRewardBalance(t, db, player.ID, PointsKey, 900)
	checkRewardBalance(t, db, player.ID, MessageRewriteCreditKey, 1)
	var orders int64
	if err := db.Model(&RewardExchangeOrder{}).Where("player_id = ?", player.ID).Count(&orders).Error; err != nil {
		t.Fatal(err)
	}
	if orders != 1 {
		t.Fatalf("orders=%d, want 1", orders)
	}
}

func TestPostgresExchangeOfferLimitValidationAndRevision(t *testing.T) {
	_, repo, actor, _ := rewardFixture(t)
	ctx := context.Background()
	offers, err := repo.ListExchangeOffers(ctx, true)
	if err != nil || len(offers) != 1 {
		t.Fatalf("seed offer: %+v %v", offers, err)
	}
	offer := offers[0]
	for _, invalid := range []int64{-1, MaxRewardQuantity + 1} {
		candidate := offer
		candidate.MaxRedemptionsPerUser = invalid
		if _, err := repo.SaveExchangeOffer(ctx, actor.ID, offer.ID, candidate); !errors.Is(err, ErrRewardQuantity) {
			t.Fatalf("limit %d accepted: %v", invalid, err)
		}
	}
	saved := saveExchangeLimit(t, repo, actor, offer, 5)
	revisions, err := repo.ExchangeRevisions(ctx, saved.ID)
	if err != nil || len(revisions) != 2 {
		t.Fatalf("revisions=%d err=%v", len(revisions), err)
	}
	if got, ok := revisions[0].Snapshot["max_redemptions_per_user"].(float64); !ok || int64(got) != 5 {
		t.Fatalf("saved snapshot limit = %v", revisions[0].Snapshot["max_redemptions_per_user"])
	}
	if got, ok := revisions[1].Snapshot["max_redemptions_per_user"].(float64); !ok || int64(got) != 0 {
		t.Fatalf("seed snapshot limit = %v", revisions[1].Snapshot["max_redemptions_per_user"])
	}
	stale := *saved
	stale.Revision--
	stale.MaxRedemptionsPerUser = 6
	if _, err := repo.SaveExchangeOffer(ctx, actor.ID, saved.ID, stale); !errors.Is(err, ErrExchangeChanged) {
		t.Fatalf("stale revision accepted: %v", err)
	}
}

var seedOfferID = uuid.MustParse("00000000-0000-0000-0000-000000000001")

func addStock(t *testing.T, db *gorm.DB, offer uuid.UUID, n int) {
	t.Helper()
	if err := db.Exec(`INSERT INTO reward_exchange_stock (id, offer_id, code) SELECT gen_random_uuid(), ?, CASE WHEN o.kind = 'cdk' THEN 'CDK-' || g ELSE '' END FROM generate_series(1, ?) g, reward_exchange_offers o WHERE o.id = ?`, offer, n, offer).Error; err != nil {
		t.Fatal(err)
	}
}
