package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/testdb"
	"gorm.io/gorm"
)

func rewardFixture(t *testing.T) (*gorm.DB, *RewardRepository, User, User, RewardDefinition) {
	t.Helper()
	db := testdb.Open(t, "rewards")
	repo := NewRewardRepository(db)
	actor := User{ID: uuid.New(), Username: "admin", DisplayName: "管理员", Role: UserRoleAdmin, Status: UserStatusActive}
	player := User{ID: uuid.New(), Username: "player", DisplayName: "玩家", Role: UserRoleUser, Status: UserStatusActive}
	for _, user := range []*User{&actor, &player} {
		if err := db.Create(user).Error; err != nil {
			t.Fatal(err)
		}
	}
	def, err := repo.DefinitionByKey(context.Background(), MessageRewriteCreditKey)
	if err != nil {
		t.Fatal(err)
	}
	return db, repo, actor, player, *def
}

func grantRequest(def RewardDefinition, quantity int64, mode string) AdminRewardGrant {
	return AdminRewardGrant{RequestID: uuid.New(), RewardDefinitionID: def.ID, Quantity: quantity, Purpose: "gift", DeliveryMode: mode, Reason: "internal support reason", PublicNote: "感谢参与"}
}

func TestPostgresRewardGrantConcurrencyAndContentIdentity(t *testing.T) {
	db, repo, actor, player, def := rewardFixture(t)
	ctx := context.Background()
	request := grantRequest(def, 24, "direct")
	var group sync.WaitGroup
	results := make(chan error, 12)
	for n := 0; n < 12; n++ {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := repo.AdminGrant(ctx, actor.ID, player.ID, request)
			results <- err
		}()
	}
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	var balance PlayerRewardBalance
	if err := db.First(&balance, "player_id = ? AND reward_definition_id = ?", player.ID, def.ID).Error; err != nil {
		t.Fatal(err)
	}
	if balance.Balance != 24 {
		t.Fatalf("duplicate grant: %d", balance.Balance)
	}
	for _, table := range []string{"reward_records", "reward_ledger", "admin_audit_logs"} {
		var count int64
		if err := db.Table(table).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("%s count=%d err=%v", table, count, err)
		}
	}
	request.Quantity++
	if _, err := repo.AdminGrant(ctx, actor.ID, player.ID, request); !errors.Is(err, ErrRewardRequestConflict) {
		t.Fatalf("changed request: %v", err)
	}
	request.Quantity--
	if err := db.Model(&player).Update("status", UserStatusDisabled).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AdminGrant(ctx, actor.ID, player.ID, request); err != nil {
		t.Fatalf("committed replay after disable: %v", err)
	}
	request.RequestID = uuid.New()
	if _, err := repo.AdminGrant(ctx, actor.ID, player.ID, request); !errors.Is(err, ErrTargetUserDisabled) {
		t.Fatalf("new grant to disabled account: %v", err)
	}
}

func TestPostgresRewardClaimCancellationAndPrivateFields(t *testing.T) {
	db, repo, actor, player, def := rewardFixture(t)
	ctx := context.Background()
	for n := 0; n < 8; n++ {
		row, err := repo.AdminGrant(ctx, actor.ID, player.ID, grantRequest(def, 3, "claim"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repo.Record(ctx, &actor.ID, row.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("cross-user read: %v", err)
		}
		if _, err := repo.ClaimReward(ctx, actor.ID, row.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("cross-user claim: %v", err)
		}
		encoded, _ := json.Marshal(row)
		for _, secret := range []string{"internal support reason", "request_hash", "operator_id", "actor_id"} {
			if strings.Contains(string(encoded), secret) {
				t.Fatalf("private field leaked: %s", secret)
			}
		}
		var group sync.WaitGroup
		results := make(chan error, 2)
		group.Add(2)
		go func() { defer group.Done(); _, err := repo.ClaimReward(ctx, player.ID, row.ID); results <- err }()
		go func() {
			defer group.Done()
			_, err := repo.ManageReward(ctx, actor.ID, row.ID, "cancel", "撤销误发", uuid.New())
			results <- err
		}()
		group.Wait()
		close(results)
		for err := range results {
			if err != nil && !errors.Is(err, ErrRewardState) {
				t.Fatal(err)
			}
		}
		current, err := repo.Record(ctx, &player.ID, row.ID)
		if err != nil {
			t.Fatal(err)
		}
		var count int64
		db.Model(&RewardLedgerEntry{}).Where("source_id = ?", row.SourceID).Count(&count)
		if current.Status == "cancelled" && count != 0 || current.Status == "granted" && count != 1 {
			t.Fatalf("inconsistent terminal result: %s ledgers=%d", current.Status, count)
		}
		if current.Status != "cancelled" && current.Status != "granted" {
			t.Fatalf("nonterminal record: %s", current.Status)
		}
	}
}

func TestPostgresRewardRefundAndReversalRelations(t *testing.T) {
	db, repo, actor, player, def := rewardFixture(t)
	ctx := context.Background()
	row, err := repo.AdminGrant(ctx, actor.ID, player.ID, grantRequest(def, 10, "direct"))
	if err != nil {
		t.Fatal(err)
	}
	consume := RewardMutation{PlayerID: player.ID, RewardKey: def.Key, Quantity: 7, SourceType: "message_rewrite", SourceID: "turn", IdempotencyKey: "consume:turn"}
	if _, err := repo.Consume(ctx, consume); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ManageReward(ctx, actor.ID, row.ID, "reverse", "wrong grant", uuid.New()); !errors.Is(err, ErrInsufficientReward) {
		t.Fatalf("reversal with insufficient balance: %v", err)
	}
	refund := RewardRefund{PlayerID: player.ID, RewardKey: def.Key, ConsumeIdempotencyKey: consume.IdempotencyKey, RefundIdempotencyKey: "refund-one", SourceID: "turn"}
	if _, err := repo.RefundConsumed(ctx, refund); err != nil {
		t.Fatal(err)
	}
	refund.RefundIdempotencyKey = "another-refund-request"
	if _, err := repo.RefundConsumed(ctx, refund); err != nil {
		t.Fatal(err)
	}
	reversalID := uuid.New()
	if _, err := repo.ManageReward(ctx, actor.ID, row.ID, "reverse", "wrong grant", reversalID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ManageReward(ctx, actor.ID, row.ID, "reverse", "wrong grant", reversalID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ManageReward(ctx, actor.ID, row.ID, "reverse", "wrong grant", uuid.New()); !errors.Is(err, ErrRewardState) {
		t.Fatalf("duplicate reversal: %v", err)
	}
	var balance PlayerRewardBalance
	db.First(&balance, "player_id = ? AND reward_definition_id = ?", player.ID, def.ID)
	if balance.Balance != 0 || balance.GrantedTotal != 10 || balance.ConsumedTotal != 7 || balance.RefundedTotal != 7 || balance.ReversedTotal != 10 {
		t.Fatalf("incorrect projections: %+v", balance)
	}
	var counts []struct {
		Operation string
		Count     int
	}
	db.Model(&RewardLedgerEntry{}).Select("operation,count(*) AS count").Group("operation").Scan(&counts)
	for _, count := range counts {
		if count.Count != 1 {
			t.Fatalf("duplicate %s: %d", count.Operation, count.Count)
		}
	}
}

func TestPostgresRewardAuditFailureRollsBackEverything(t *testing.T) {
	db, repo, actor, player, def := rewardFixture(t)
	ctx := context.Background()
	if err := db.Exec(`CREATE FUNCTION fail_reward_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test audit failure'; END $$;
	CREATE TRIGGER fail_reward_audit BEFORE INSERT ON admin_audit_logs FOR EACH ROW EXECUTE FUNCTION fail_reward_audit();`).Error; err != nil {
		t.Fatal(err)
	}
	request := grantRequest(def, 10, "direct")
	if _, err := repo.AdminGrant(ctx, actor.ID, player.ID, request); err == nil {
		t.Fatal("audit failure did not fail grant")
	}
	for _, table := range []string{"reward_records", "reward_ledger", "player_reward_balances"} {
		var count int64
		db.Table(table).Count(&count)
		if count != 0 {
			t.Fatalf("transaction leaked %s: %d", table, count)
		}
	}
	if err := db.Exec("DROP TRIGGER fail_reward_audit ON admin_audit_logs").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.AdminGrant(ctx, actor.ID, player.ID, request); err != nil {
		t.Fatalf("same request after rollback: %v", err)
	}
}

func TestPostgresRewardSnapshotsSkipsAndConfiguration(t *testing.T) {
	db, repo, actor, player, def := rewardFixture(t)
	ctx := context.Background()
	row, err := repo.AdminGrant(ctx, actor.ID, player.ID, grantRequest(def, 2, "claim"))
	if err != nil {
		t.Fatal(err)
	}
	oldName := def.Name
	def.Name = "新版额度名称"
	if _, err := repo.SaveDefinition(ctx, actor.ID, def.ID, def, false); err != nil {
		t.Fatal(err)
	}
	def.Disabled = true
	if _, err := repo.SaveDefinition(ctx, actor.ID, def.ID, def, false); !errors.Is(err, ErrRewardState) {
		t.Fatalf("registration rule was silently disabled: %v", err)
	}
	if _, err := repo.SaveDefinition(ctx, actor.ID, def.ID, def, true); err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimReward(ctx, player.ID, row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.Snapshot["name"] != oldName {
		t.Fatal("existing snapshot changed")
	}
	rule, err := repo.RegistrationRule(ctx)
	if err != nil || rule.Enabled {
		t.Fatalf("registration disable: %+v %v", rule, err)
	}
	if err := repo.WithTx(ctx, func(tx *RewardRepository) error { return tx.GrantRegistrationInCurrentTx(ctx, player.ID) }); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&RewardRecord{}).Where("source_type = 'user_registration'").Count(&count)
	if count != 0 {
		t.Fatal("disabled registration rule granted rewards")
	}
	badge := RewardDefinition{Key: "test.badge", Kind: "badge", Name: "纪念章", Metadata: JSONMap{}}
	if err := repo.CreateDefinition(ctx, &badge); err != nil {
		t.Fatal(err)
	}
	first, err := repo.AdminGrant(ctx, actor.ID, player.ID, grantRequest(badge, 1, "direct"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.AdminGrant(ctx, actor.ID, player.ID, grantRequest(badge, 1, "direct"))
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != "granted" || second.Status != "skipped" || second.SkipReason != "already_owned" || second.LedgerID != nil {
		t.Fatalf("bad badge results: %+v %+v", first, second)
	}
	zero, err := repo.Grant(ctx, RewardMutation{PlayerID: player.ID, RewardKey: def.Key, Quantity: 0, SourceType: "challenge_run", SourceID: "zero-run", IdempotencyKey: "zero-run"})
	if err != nil || zero.Ledger != nil {
		t.Fatalf("zero award: %+v %v", zero, err)
	}
	var skipped RewardRecord
	if err := db.First(&skipped, "source_key = 'zero-run'").Error; err != nil || skipped.SkipReason != "zero_quantity" {
		t.Fatalf("zero record: %+v %v", skipped, err)
	}
}

func TestPostgresAdminUserSearchBeyondFirstPage(t *testing.T) {
	db, _, _, player, _ := rewardFixture(t)
	ctx := context.Background()
	repo := NewAuthRepository(db)
	for n := 0; n < 205; n++ {
		user := User{ID: uuid.New(), Username: fmt.Sprintf("user_%03d", n), DisplayName: "同名用户", Role: UserRoleUser, Status: UserStatusActive}
		if err := db.Create(&user).Error; err != nil {
			t.Fatal(err)
		}
	}
	users, err := repo.ListUsers(ctx, 30, 0, "PLAYER")
	if err != nil || len(users) != 1 || users[0].ID != player.ID {
		t.Fatalf("old account search: %+v %v", users, err)
	}
	users, err = repo.ListUsers(ctx, 30, 0, "user_001")
	if err != nil || len(users) != 1 || users[0].Username != "user_001" {
		t.Fatalf("literal underscore search: %+v %v", users, err)
	}
	users, err = repo.ListUsers(ctx, 30, 200, "同名用户")
	if err != nil || len(users) != 5 {
		t.Fatalf("search pagination: %d %v", len(users), err)
	}
	users, err = repo.ListUsers(ctx, 30, 0, "%")
	if err != nil || len(users) != 0 {
		t.Fatalf("wildcard escaped: %d %v", len(users), err)
	}
}
