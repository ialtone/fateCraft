package storage

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func cdkOffer(t *testing.T, repo *RewardRepository, actor User, limit int64) *RewardExchangeOffer {
	t.Helper()
	offer, err := repo.SaveExchangeOffer(context.Background(), actor.ID, uuid.Nil, RewardExchangeOffer{Name: "游戏 CDK", Kind: ExchangeKindCDK, Tutorial: "# 使用教程", CostQuantity: 10, RewardQuantity: 1, MaxRedemptionsPerUser: limit, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return offer
}

func stockCount(t *testing.T, repo *RewardRepository, offer uuid.UUID) ExchangeStockCount {
	t.Helper()
	counts, err := repo.ExchangeStockCounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return counts[offer]
}

func TestPostgresExchangeStockImportRemoveAndCounts(t *testing.T) {
	db, repo, actor, player := rewardFixture(t)
	ctx := context.Background()
	offer := cdkOffer(t, repo, actor, 0)
	if _, err := repo.AddExchangeStock(ctx, actor.ID, offer.ID, []string{"A", "B", "A"}, 0); err == nil {
		t.Fatal("in-paste duplicate accepted")
	}
	if n, err := repo.AddExchangeStock(ctx, actor.ID, offer.ID, []string{" A ", "", "B", "C"}, 0); err != nil || n != 3 {
		t.Fatalf("import n=%d err=%v", n, err)
	}
	var dup *ExchangeDuplicateError
	if _, err := repo.AddExchangeStock(ctx, actor.ID, offer.ID, []string{"D", "B"}, 0); !errors.As(err, &dup) || len(dup.Codes) != 1 || dup.Codes[0] != "B" {
		t.Fatalf("existing duplicate: %v", err)
	}
	if _, err := repo.AddExchangeStock(ctx, player.ID, offer.ID, []string{"E"}, 0); !errors.Is(err, ErrRewardAdminRequired) {
		t.Fatalf("player import: %v", err)
	}
	if _, err := repo.AdminGrant(ctx, actor.ID, player.ID, grantRequest(PointsKey, 100)); err != nil {
		t.Fatal(err)
	}
	order, err := repo.Exchange(ctx, player.ID, RewardExchangeRequest{RequestID: uuid.New(), OfferID: offer.ID, Revision: offer.Revision, Units: 2})
	if err != nil || len(order.Snapshot["codes"].([]string)) != 2 || order.RewardLedgerID != nil {
		t.Fatalf("cdk order %+v %v", order, err)
	}
	checkRewardBalance(t, db, player.ID, MessageRewriteCreditKey, 0)
	rows, err := repo.ListExchangeStock(ctx, offer.ID, "", "", 10, 0)
	if err != nil || len(rows) != 3 {
		t.Fatalf("rows %d %v", len(rows), err)
	}
	ids := []uuid.UUID{rows[0].ID, rows[1].ID, rows[2].ID}
	if n, err := repo.RemoveExchangeStock(ctx, actor.ID, offer.ID, ids); err != nil || n != 1 {
		t.Fatalf("remove n=%d err=%v (redeemed rows must stay)", n, err)
	}
	if got := stockCount(t, repo, offer.ID); got.Total != 2 || got.Remaining != 0 {
		t.Fatalf("counts %+v", got)
	}
	if _, err := repo.Exchange(ctx, player.ID, RewardExchangeRequest{RequestID: uuid.New(), OfferID: offer.ID, Revision: offer.Revision, Units: 1}); !errors.Is(err, ErrExchangeStock) {
		t.Fatalf("sold out: %v", err)
	}
	checkRewardBalance(t, db, player.ID, PointsKey, 80)
}

func TestPostgresExchangeStockConcurrentNoOversell(t *testing.T) {
	db, repo, actor, player := rewardFixture(t)
	ctx := context.Background()
	if _, err := repo.AdminGrant(ctx, actor.ID, player.ID, grantRequest(PointsKey, 1000)); err != nil {
		t.Fatal(err)
	}
	offer := cdkOffer(t, repo, actor, 0)
	if _, err := repo.AddExchangeStock(ctx, actor.ID, offer.ID, []string{"X1", "X2", "X3"}, 0); err != nil {
		t.Fatal(err)
	}
	var done atomic.Int64
	concurrentRewards(t, 8, func() error {
		_, err := repo.Exchange(ctx, player.ID, RewardExchangeRequest{RequestID: uuid.New(), OfferID: offer.ID, Revision: offer.Revision, Units: 1})
		if errors.Is(err, ErrExchangeStock) {
			return nil
		}
		if err == nil {
			done.Add(1)
		}
		return err
	})
	if done.Load() != 3 {
		t.Fatalf("sold %d of 3", done.Load())
	}
	checkRewardBalance(t, db, player.ID, PointsKey, 970)
	// 整单拒绝：剩余不足时不部分成交。
	if _, err := repo.AddExchangeStock(ctx, actor.ID, offer.ID, []string{"Y1"}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Exchange(ctx, player.ID, RewardExchangeRequest{RequestID: uuid.New(), OfferID: offer.ID, Revision: offer.Revision, Units: 2}); !errors.Is(err, ErrExchangeStock) {
		t.Fatalf("partial fill: %v", err)
	}
	if got := stockCount(t, repo, offer.ID); got.Total != 4 || got.Remaining != 1 {
		t.Fatalf("counts %+v", got)
	}
}

func TestPostgresExchangeTicketRefundRestoresQuota(t *testing.T) {
	db, repo, actor, player := rewardFixture(t)
	ctx := context.Background()
	if _, err := repo.AdminGrant(ctx, actor.ID, player.ID, grantRequest(PointsKey, 100)); err != nil {
		t.Fatal(err)
	}
	offer := cdkOffer(t, repo, actor, 1)
	if _, err := repo.AddExchangeStock(ctx, actor.ID, offer.ID, []string{"K1", "K2"}, 0); err != nil {
		t.Fatal(err)
	}
	order, err := repo.Exchange(ctx, player.ID, RewardExchangeRequest{RequestID: uuid.New(), OfferID: offer.ID, Revision: offer.Revision, Units: 1})
	if err != nil {
		t.Fatal(err)
	}
	var stock RewardExchangeStock
	if err := db.First(&stock, "order_id = ?", order.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stock.Status != "redeemed" || stock.RedeemedAt == nil || stock.PlayerID == nil || *stock.PlayerID != player.ID {
		t.Fatalf("redeemed stock = %+v", stock)
	}
	redeemedAt := *stock.RedeemedAt
	traced, err := repo.ExchangeOrder(ctx, order.ID)
	if err != nil || traced.ID != order.ID || traced.PlayerID != player.ID || traced.Username != player.Username {
		t.Fatalf("traced order = %+v, err = %v", traced, err)
	}
	if _, err := repo.ExchangeOrder(ctx, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing order: %v", err)
	}
	if _, err := repo.OpenTicket(ctx, actor.ID, order.ID, "不是我的订单"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign ticket: %v", err)
	}
	ticket, err := repo.OpenTicket(ctx, player.ID, order.ID, "CDK 无效")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.OpenTicket(ctx, player.ID, order.ID, "重复"); !errors.Is(err, ErrTicketState) {
		t.Fatalf("open twice: %v", err)
	}
	if _, err := repo.ResolveTicket(ctx, actor.ID, ticket.ID, false, ""); !errors.Is(err, ErrRewardQuantity) {
		t.Fatalf("reject without reason: %v", err)
	}
	if _, err := repo.ResolveTicket(ctx, actor.ID, ticket.ID, false, "请提供截图"); err != nil {
		t.Fatal(err)
	}
	if err := repo.SetTicketsEnabled(ctx, actor.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.OpenTicket(ctx, player.ID, order.ID, "截图"); !errors.Is(err, ErrTicketsDisabled) {
		t.Fatalf("disabled: %v", err)
	}
	if err := repo.SetTicketsEnabled(ctx, actor.ID, true); err != nil {
		t.Fatal(err)
	}
	again, err := repo.OpenTicket(ctx, player.ID, order.ID, "截图")
	if err != nil || again.ID != ticket.ID || len(again.Entries) != 3 {
		t.Fatalf("reopen %+v %v", again, err)
	}
	if err := repo.SetTicketsEnabled(ctx, actor.ID, false); err != nil {
		t.Fatal(err)
	}
	done, err := repo.ResolveTicket(ctx, actor.ID, ticket.ID, true, "")
	if err != nil || done.Status != "refunded" {
		t.Fatalf("approve %+v %v", done, err)
	}
	if _, err := repo.ResolveTicket(ctx, actor.ID, ticket.ID, true, ""); !errors.Is(err, ErrTicketState) {
		t.Fatalf("double approve: %v", err)
	}
	if err := db.First(&stock, "id = ?", stock.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stock.Status != "voided" || stock.OrderID == nil || *stock.OrderID != order.ID || stock.RedeemedAt == nil || !stock.RedeemedAt.Equal(redeemedAt) {
		t.Fatalf("voided stock lost trace = %+v", stock)
	}
	checkRewardBalance(t, db, player.ID, PointsKey, 100)
	if got := stockCount(t, repo, offer.ID); got.Total != 1 || got.Remaining != 1 {
		t.Fatalf("counts %+v", got)
	}
	if _, err := repo.Exchange(ctx, player.ID, RewardExchangeRequest{RequestID: uuid.New(), OfferID: offer.ID, Revision: offer.Revision, Units: 1}); err != nil {
		t.Fatalf("quota not restored: %v", err)
	}
}

func TestPostgresPointsCDK(t *testing.T) {
	db, repo, actor, player := rewardFixture(t)
	ctx := context.Background()
	if _, _, err := repo.GeneratePointsCDKs(ctx, actor.ID, "", 50, 2, 2); !errors.Is(err, ErrRewardQuantity) {
		t.Fatalf("bad expiry: %v", err)
	}
	batch, codes, err := repo.GeneratePointsCDKs(ctx, actor.ID, "活动", 50, 2, 7)
	if err != nil || len(codes) != 2 || len(codes[0].Code) != 32 || batch.ExpiresAt.Sub(batch.CreatedAt) != 7*24*time.Hour {
		t.Fatalf("generate %+v %+v %v", batch, codes, err)
	}
	if _, err := repo.RedeemPointsCDK(ctx, player.ID, "0123456789abcdef0123456789abcdef"); !errors.Is(err, ErrPointsCDKUnknown) {
		t.Fatalf("unknown: %v", err)
	}
	var success atomic.Int64
	concurrentRewards(t, 4, func() error {
		_, err := repo.RedeemPointsCDK(ctx, player.ID, codes[0].Code)
		if err == nil {
			success.Add(1)
			return nil
		}
		if errors.Is(err, ErrPointsCDKUsed) {
			return nil
		}
		return err
	})
	if success.Load() != 1 {
		t.Fatalf("redeemed %d times", success.Load())
	}
	if _, err := repo.RedeemPointsCDK(ctx, player.ID, codes[1].Code); !errors.Is(err, ErrPointsCDKSameBatch) {
		t.Fatalf("same batch: %v", err)
	}
	if _, err := repo.RedeemPointsCDK(ctx, actor.ID, codes[0].Code); !errors.Is(err, ErrPointsCDKUsed) {
		t.Fatalf("used: %v", err)
	}
	if err := db.Exec("UPDATE points_cdk_batches SET expires_at = now() - interval '1 second'").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RedeemPointsCDK(ctx, actor.ID, codes[1].Code); !errors.Is(err, ErrPointsCDKExpired) {
		t.Fatalf("expired: %v", err)
	}
	checkRewardBalance(t, db, player.ID, PointsKey, 50)
	batches, err := repo.PointsCDKBatches(ctx, 10, 0)
	if err != nil || len(batches) != 1 || batches[0].Redeemed != 1 {
		t.Fatalf("batches %+v %v", batches, err)
	}
	rows, err := repo.PointsCDKs(ctx, batch.ID)
	if err != nil || len(rows) != 2 || rows[0].Username != "player" {
		t.Fatalf("codes %+v %v", rows, err)
	}
}
