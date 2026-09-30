package storage

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/testdb"
	"gorm.io/gorm"
	"sync"
	"testing"
)

func rewardFixture(t *testing.T) (*gorm.DB, *RewardRepository, User, User) {
	t.Helper()
	db := testdb.Open(t, "rewards")
	actor := User{ID: uuid.New(), Username: "admin", DisplayName: "管理员", Role: UserRoleAdmin, Status: UserStatusActive}
	player := User{ID: uuid.New(), Username: "player", DisplayName: "玩家", Role: UserRoleUser, Status: UserStatusActive}
	for _, user := range []*User{&actor, &player} {
		if err := db.Create(user).Error; err != nil {
			t.Fatal(err)
		}
	}
	return db, NewRewardRepository(db), actor, player
}
func grantRequest(key string, quantity int64) AdminRewardGrant {
	return AdminRewardGrant{RequestID: uuid.New(), RewardKey: key, Quantity: quantity, Reason: "内部补偿原因", PublicNote: "感谢参与"}
}
func checkRewardBalance(t *testing.T, db *gorm.DB, player uuid.UUID, key string, want int64) {
	t.Helper()
	var row PlayerRewardBalance
	err := db.Where("player_id = ? AND reward_key = ?", player, key).First(&row).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal(err)
	}
	var sum int64
	if err := db.Model(&RewardLedgerEntry{}).Select("COALESCE(sum(quantity_delta),0)").Where("player_id = ? AND reward_key = ?", player, key).Scan(&sum).Error; err != nil {
		t.Fatal(err)
	}
	if row.Balance != want || sum != want {
		t.Fatalf("%s balance=%d ledger=%d want=%d", key, row.Balance, sum, want)
	}
}
func concurrentRewards(t *testing.T, n int, fn func() error) {
	t.Helper()
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- fn() }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestPostgresRewardDirectGrantIdentityAndReversal(t *testing.T) {
	db, repo, actor, player := rewardFixture(t)
	ctx := context.Background()
	in := grantRequest(PointsKey, 200)
	concurrentRewards(t, 8, func() error { _, err := repo.AdminGrant(ctx, actor.ID, player.ID, in); return err })
	checkRewardBalance(t, db, player.ID, PointsKey, 200)
	in.Quantity++
	if _, err := repo.AdminGrant(ctx, actor.ID, player.ID, in); !errors.Is(err, ErrRewardRequestConflict) {
		t.Fatalf("changed request accepted: %v", err)
	}
	in.Quantity--
	if _, err := repo.AdminGrant(ctx, player.ID, player.ID, in); !errors.Is(err, ErrRewardAdminRequired) {
		t.Fatalf("non-admin replay: %v", err)
	}
	row, err := repo.GrantByRequest(ctx, in.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	request := uuid.New()
	concurrentRewards(t, 4, func() error { _, err := repo.ReverseAdminGrant(ctx, actor.ID, row.ID, request, "误发"); return err })
	checkRewardBalance(t, db, player.ID, PointsKey, 0)
	if _, err := repo.ReverseAdminGrant(ctx, actor.ID, row.ID, uuid.New(), "再次撤回"); !errors.Is(err, ErrRewardState) {
		t.Fatalf("double reversal: %v", err)
	}
	if err := db.Model(&player).Update("status", UserStatusDisabled).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AdminGrant(ctx, actor.ID, player.ID, in); err != nil {
		t.Fatalf("committed replay after disable: %v", err)
	}
	in.RequestID = uuid.New()
	if _, err := repo.AdminGrant(ctx, actor.ID, player.ID, in); !errors.Is(err, ErrTargetUserDisabled) {
		t.Fatalf("new grant to disabled user: %v", err)
	}
	var auditCount int64
	if err := db.Model(&AdminAuditLog{}).Count(&auditCount).Error; err != nil || auditCount != 2 {
		t.Fatalf("audit count=%d %v", auditCount, err)
	}
}
func TestPostgresRewardRegistrationRevisionAndRollback(t *testing.T) {
	db, repo, actor, player := rewardFixture(t)
	ctx := context.Background()
	rule, err := repo.RegistrationRule(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stale := *rule
	rule.Points = 300
	rule.RewriteCredits = 4
	if _, err = repo.SaveRegistrationRule(ctx, actor.ID, *rule); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.SaveRegistrationRule(ctx, actor.ID, stale); !errors.Is(err, ErrRewardRequestConflict) {
		t.Fatalf("stale rule: %v", err)
	}
	aborted := errors.New("abort source transaction")
	err = repo.WithTx(ctx, func(tx *RewardRepository) error {
		if err := tx.GrantRegistrationInCurrentTx(ctx, player.ID); err != nil {
			return err
		}
		return aborted
	})
	if !errors.Is(err, aborted) {
		t.Fatal(err)
	}
	checkRewardBalance(t, db, player.ID, PointsKey, 0)
	checkRewardBalance(t, db, player.ID, MessageRewriteCreditKey, 0)
	concurrentRewards(t, 4, func() error {
		return repo.WithTx(ctx, func(tx *RewardRepository) error { return tx.GrantRegistrationInCurrentTx(ctx, player.ID) })
	})
	checkRewardBalance(t, db, player.ID, PointsKey, 300)
	checkRewardBalance(t, db, player.ID, MessageRewriteCreditKey, 4)
	current, err := repo.RegistrationRule(ctx)
	if err != nil {
		t.Fatal(err)
	}
	current.Points = 0
	current.RewriteCredits = 0
	if _, err := repo.SaveRegistrationRule(ctx, actor.ID, *current); err != nil {
		t.Fatal(err)
	}
	other := User{ID: uuid.New(), Username: "zero", Role: UserRoleUser, Status: UserStatusActive}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.WithTx(ctx, func(tx *RewardRepository) error { return tx.GrantRegistrationInCurrentTx(ctx, other.ID) }); err != nil {
		t.Fatal(err)
	}
	checkRewardBalance(t, db, other.ID, PointsKey, 0)
	checkRewardBalance(t, db, other.ID, MessageRewriteCreditKey, 0)
	latest, err := repo.RegistrationRule(ctx)
	if err != nil {
		t.Fatal(err)
	}
	latest.Points, latest.RewriteCredits = 50, 10
	if _, err := repo.SaveRegistrationRule(ctx, actor.ID, *latest); err != nil {
		t.Fatal(err)
	}
	if err := repo.WithTx(ctx, func(tx *RewardRepository) error { return tx.GrantRegistrationInCurrentTx(ctx, other.ID) }); err != nil {
		t.Fatal(err)
	}
	checkRewardBalance(t, db, other.ID, PointsKey, 0)
	checkRewardBalance(t, db, other.ID, MessageRewriteCreditKey, 0)
}

func TestPostgresAdminSelfGrantConcurrency(t *testing.T) {
	db, repo, actor, _ := rewardFixture(t)
	ctx := context.Background()
	concurrentRewards(t, 4, func() error {
		_, err := repo.AdminGrant(ctx, actor.ID, actor.ID, grantRequest(PointsKey, 10))
		return err
	})
	checkRewardBalance(t, db, actor.ID, PointsKey, 40)
}
func TestPostgresRewardConsumptionRefundAndConstraints(t *testing.T) {
	db, repo, actor, player := rewardFixture(t)
	ctx := context.Background()
	grant, err := repo.AdminGrant(ctx, actor.ID, player.ID, grantRequest(MessageRewriteCreditKey, 3))
	if err != nil {
		t.Fatal(err)
	}
	consume := RewardMutation{PlayerID: player.ID, RewardKey: MessageRewriteCreditKey, Quantity: 1, SourceType: "message_rewrite", SourceID: "operation-1", IdempotencyKey: "consume-1"}
	if _, err = repo.Consume(ctx, consume); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.ReverseAdminGrant(ctx, actor.ID, grant.ID, uuid.New(), "余额不足"); !errors.Is(err, ErrInsufficientReward) {
		t.Fatalf("insufficient reversal: %v", err)
	}
	refund := RewardRefund{PlayerID: player.ID, RewardKey: MessageRewriteCreditKey, ConsumeIdempotencyKey: "consume-1"}
	concurrentRewards(t, 6, func() error { _, err := repo.RefundConsumed(ctx, refund); return err })
	checkRewardBalance(t, db, player.ID, MessageRewriteCreditKey, 3)
	if err := db.Model(&RewardLedgerEntry{}).Where("id = ?", grant.ID).Update("quantity_delta", 10).Error; err == nil {
		t.Fatal("mutable ledger")
	}
	if err := db.Exec("INSERT INTO player_reward_balances(player_id,reward_key) VALUES (?, 'custom.coin')", player.ID).Error; err == nil {
		t.Fatal("custom asset accepted")
	}
	bad := consume
	bad.RewardKey = "custom.coin"
	if _, err := repo.Consume(ctx, bad); !errors.Is(err, ErrRewardKey) {
		t.Fatal(err)
	}
	rows, err := repo.ListLedger(ctx, &player.ID, 30, RewardPageCursor{RewardKey: MessageRewriteCreditKey})
	if err != nil || len(rows) != 3 {
		t.Fatalf("ledger filter: %+v %v", rows, err)
	}
}
func TestPostgresRewardAuditFailureRollsBack(t *testing.T) {
	db, repo, actor, player := rewardFixture(t)
	if err := db.Exec(`CREATE FUNCTION reject_test_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test audit failure'; END; $$; CREATE TRIGGER reject_test_audit BEFORE INSERT ON admin_audit_logs FOR EACH ROW EXECUTE FUNCTION reject_test_audit();`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AdminGrant(context.Background(), actor.ID, player.ID, grantRequest(PointsKey, 10)); err == nil {
		t.Fatal("audit failure ignored")
	}
	checkRewardBalance(t, db, player.ID, PointsKey, 0)
}
func TestPostgresRewardGrantAmountBounds(t *testing.T) {
	db, repo, actor, player := rewardFixture(t)
	ctx := context.Background()
	for _, amount := range []int64{0, -1, MaxRewardQuantity + 1} {
		if _, err := repo.AdminGrant(ctx, actor.ID, player.ID, grantRequest(PointsKey, amount)); !errors.Is(err, ErrRewardQuantity) {
			t.Fatalf("amount %d: %v", amount, err)
		}
	}
	if _, err := repo.AdminGrant(ctx, actor.ID, player.ID, grantRequest(PointsKey, MaxRewardQuantity)); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AdminGrant(ctx, actor.ID, player.ID, grantRequest(PointsKey, 1)); !errors.Is(err, ErrRewardQuantity) {
		t.Fatalf("overflow: %v", err)
	}
	checkRewardBalance(t, db, player.ID, PointsKey, MaxRewardQuantity)
}
