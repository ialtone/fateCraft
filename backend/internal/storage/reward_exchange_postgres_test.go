package storage

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func TestPostgresRewardExchangeAtomicReplayAndRevision(t *testing.T) {
	db, repo, actor, player, cost := rewardFixture(t)
	ctx := context.Background()
	target := RewardDefinition{Key: "test.tokens", Name: "账号点数", Kind: "currency", Stackable: true, Consumable: true, Metadata: JSONMap{}}
	if err := repo.CreateDefinition(ctx, &target); err != nil {
		t.Fatal(err)
	}
	offer, err := repo.SaveExchangeOffer(ctx, actor.ID, uuid.Nil, RewardExchangeOffer{Name: "点数兑换", CostDefinitionID: cost.ID, CostQuantity: 3, RewardDefinitionID: target.ID, RewardQuantity: 2, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AdminGrant(ctx, actor.ID, player.ID, grantRequest(cost, 10, "direct")); err != nil {
		t.Fatal(err)
	}
	request := RewardExchangeRequest{RequestID: uuid.New(), OfferID: offer.ID, Revision: offer.Revision, Units: 2}
	var group sync.WaitGroup
	results := make(chan *RewardExchangeOrder, 10)
	errs := make(chan error, 10)
	for n := 0; n < 10; n++ {
		group.Add(1)
		go func() {
			defer group.Done()
			row, err := repo.Exchange(ctx, player.ID, request)
			results <- row
			errs <- err
		}()
	}
	group.Wait()
	close(errs)
	close(results)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var order *RewardExchangeOrder
	for row := range results {
		if order != nil && row.ID != order.ID {
			t.Fatal("replay created new order")
		}
		order = row
	}
	assertBalance := func(id uuid.UUID, want int64) {
		t.Helper()
		var b PlayerRewardBalance
		if err := db.First(&b, "player_id = ? AND reward_definition_id = ?", player.ID, id).Error; err != nil || b.Balance != want {
			t.Fatalf("balance: %+v %v, want %d", b, err, want)
		}
	}
	assertBalance(cost.ID, 4)
	assertBalance(target.ID, 4)
	request.Units = 1
	if _, err := repo.Exchange(ctx, player.ID, request); !errors.Is(err, ErrRewardRequestConflict) {
		t.Fatalf("changed request: %v", err)
	}
	request.Units = 2
	offer.CostQuantity = 4
	offer.Name = "新价格"
	edited, err := repo.SaveExchangeOffer(ctx, actor.ID, offer.ID, *offer)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SaveExchangeOffer(ctx, actor.ID, offer.ID, *offer); !errors.Is(err, ErrExchangeChanged) {
		t.Fatalf("stale admin edit: %v", err)
	}
	stale := request
	stale.RequestID = uuid.New()
	if _, err := repo.Exchange(ctx, player.ID, stale); !errors.Is(err, ErrExchangeChanged) {
		t.Fatalf("stale price: %v", err)
	}
	expensive := stale
	expensive.Revision = edited.Revision
	if _, err := repo.Exchange(ctx, player.ID, expensive); !errors.Is(err, ErrInsufficientReward) {
		t.Fatalf("insufficient points: %v", err)
	}
	assertBalance(cost.ID, 4)
	assertBalance(target.ID, 4)
	edited.Enabled = false
	if _, err := repo.SaveExchangeOffer(ctx, actor.ID, edited.ID, *edited); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Exchange(ctx, player.ID, request); err != nil {
		t.Fatalf("replay after unlisting: %v", err)
	}
	replay, err := repo.ExchangeByRequest(ctx, player.ID, request.RequestID)
	if err != nil || replay.Snapshot["name"] != "点数兑换" {
		t.Fatalf("snapshot changed: %+v %v", replay, err)
	}
	if _, err := repo.ExchangeByRequest(ctx, actor.ID, request.RequestID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user order read: %v", err)
	}
	if _, err := repo.ManageReward(ctx, actor.ID, order.RewardRecordID, "reverse", "不能单边冲正", uuid.New()); !errors.Is(err, ErrRewardReversal) {
		t.Fatalf("one-sided reversal: %v", err)
	}
	if _, err := repo.RefundConsumed(ctx, RewardRefund{PlayerID: player.ID, RewardKey: cost.Key, ConsumeIdempotencyKey: "exchange:cost:" + order.ID.String(), RefundIdempotencyKey: "invalid", SourceID: "test"}); !errors.Is(err, ErrRewardReversal) {
		t.Fatalf("one-sided refund: %v", err)
	}
	var count int64
	if err := db.Model(&RewardExchangeOrder{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("orders=%d %v", count, err)
	}
	if err := db.Model(&RewardExchangeOrder{}).Where("id = ?", order.ID).Update("units", 3).Error; err == nil {
		t.Fatal("immutable order updated")
	}
	target.Stackable = false
	target.Consumable = false
	if _, err := repo.SaveDefinition(ctx, actor.ID, target.ID, target, false); !errors.Is(err, ErrRewardDefinitionInUse) {
		t.Fatalf("exchange asset semantics changed: %v", err)
	}
	overview, err := repo.Overview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, asset := range overview {
		if asset.ID == cost.ID && (asset.Balance != "4" || asset.ExchangeCost != "6") {
			t.Fatalf("bad overview: %+v", asset)
		}
	}
}

func TestPostgresRewardExchangeOwnedRollbackAndConcurrentSpend(t *testing.T) {
	db, repo, actor, player, cost := rewardFixture(t)
	ctx := context.Background()
	badge := RewardDefinition{Key: "test.exchange-badge", Name: "通关徽章", Kind: "badge", Metadata: JSONMap{}}
	if err := repo.CreateDefinition(ctx, &badge); err != nil {
		t.Fatal(err)
	}
	offer, err := repo.SaveExchangeOffer(ctx, actor.ID, uuid.Nil, RewardExchangeOffer{Name: "徽章", CostDefinitionID: cost.ID, CostQuantity: 3, RewardDefinitionID: badge.ID, RewardQuantity: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AdminGrant(ctx, actor.ID, player.ID, grantRequest(cost, 5, "direct")); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE FUNCTION fail_exchange_order() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test order failure'; END $$;
 CREATE TRIGGER fail_exchange_order BEFORE INSERT ON reward_exchange_orders FOR EACH ROW EXECUTE FUNCTION fail_exchange_order();`).Error; err != nil {
		t.Fatal(err)
	}
	request := RewardExchangeRequest{RequestID: uuid.New(), OfferID: offer.ID, Revision: offer.Revision, Units: 1}
	if _, err := repo.Exchange(ctx, player.ID, request); err == nil {
		t.Fatal("order failure did not roll back")
	}
	for _, table := range []string{"reward_ledger", "reward_records"} {
		var count int64
		if err := db.Table(table).Where("source_type = 'reward_exchange'").Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("leaked %s: %d %v", table, count, err)
		}
	}
	if err := db.Exec("DROP TRIGGER fail_exchange_order ON reward_exchange_orders").Error; err != nil {
		t.Fatal(err)
	}
	var b PlayerRewardBalance
	if err := db.First(&b, "player_id = ? AND reward_definition_id = ?", player.ID, cost.ID).Error; err != nil || b.Balance != 5 {
		t.Fatalf("payment leaked: %+v %v", b, err)
	}
	var group sync.WaitGroup
	results := make(chan error, 2)
	group.Add(2)
	go func() { defer group.Done(); _, err := repo.Exchange(ctx, player.ID, request); results <- err }()
	go func() {
		defer group.Done()
		_, err := repo.Consume(ctx, RewardMutation{PlayerID: player.ID, RewardKey: cost.Key, Quantity: 3, SourceType: "message_rewrite", SourceID: "concurrent", IdempotencyKey: "concurrent-spend"})
		results <- err
	}()
	group.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, ErrInsufficientReward) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("successful spends=%d, want 1", success)
	}
	if err := db.First(&b, "player_id = ? AND reward_definition_id = ?", player.ID, cost.ID).Error; err != nil || b.Balance != 2 {
		t.Fatalf("overspent: %+v %v", b, err)
	}
	if _, err := repo.AdminGrant(ctx, actor.ID, player.ID, grantRequest(badge, 1, "direct")); err != nil {
		t.Fatal(err)
	}
	request.RequestID = uuid.New()
	if _, err := repo.Exchange(ctx, player.ID, request); !errors.Is(err, ErrExchangeOwned) {
		t.Fatalf("already-owned reward: %v", err)
	}
	if err := db.First(&b, "player_id = ? AND reward_definition_id = ?", player.ID, cost.ID).Error; err != nil || b.Balance != 2 {
		t.Fatalf("already-owned charge: %+v %v", b, err)
	}
}

func TestPostgresAwardSourceIdentityAndPendingUserDisable(t *testing.T) {
	db, repo, actor, player, def := rewardFixture(t)
	ctx := context.Background()
	award := RewardMutation{PlayerID: player.ID, RewardKey: def.Key, Quantity: 2, SourceType: "challenge_run", SourceID: "fixed-source", IdempotencyKey: "source-key-a"}
	first, err := repo.Grant(ctx, award)
	if err != nil {
		t.Fatal(err)
	}
	award.IdempotencyKey = "source-key-b"
	second, err := repo.Grant(ctx, award)
	if err != nil || first.Ledger.ID != second.Ledger.ID {
		t.Fatalf("source identity: %+v %v", second, err)
	}
	award.Quantity++
	if _, err := repo.Grant(ctx, award); !errors.Is(err, ErrRewardRequestConflict) {
		t.Fatalf("source conflict: %v", err)
	}
	pending, err := repo.AdminGrant(ctx, actor.ID, player.ID, grantRequest(def, 2, "claim"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&player).Update("status", UserStatusDisabled).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimReward(ctx, player.ID, pending.ID); !errors.Is(err, ErrTargetUserDisabled) {
		t.Fatalf("disabled claimant: %v", err)
	}
}
