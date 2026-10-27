package storage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/testdb"
	"gorm.io/gorm"
)

type adminUserFixture struct {
	db                 *gorm.DB
	alice, bob, carol  uuid.UUID
	versionID, outcome uuid.UUID
}

func newAdminUserFixture(t *testing.T) *adminUserFixture {
	t.Helper()
	db := testdb.Open(t, "admin_users")
	f := &adminUserFixture{db: db, alice: uuid.New(), bob: uuid.New(), carol: uuid.New(), versionID: uuid.New(), outcome: uuid.New()}
	hash, packID := strings.Repeat("0", 64), uuid.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if err := db.Exec(query, args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	// alice：LinuxDo 注册（无密码）；bob：邮箱注册；carol：已停用管理员。
	exec(`INSERT INTO users (id, username, display_name, created_at) VALUES (?, 'Alice', 'Alice L', now() - interval '3 day')`, f.alice)
	exec(`INSERT INTO users (id, username, display_name, email, password_hash, created_at, last_login_at) VALUES (?, 'bob', 'Bob', 'bob@example.com', 'x', now() - interval '2 day', now() - interval '1 hour')`, f.bob)
	exec(`INSERT INTO users (id, username, display_name, status, role, created_at) VALUES (?, 'carol', 'Carol', 'disabled', 'admin', now() - interval '1 day')`, f.carol)
	exec(`INSERT INTO auth_identities (id, user_id, provider, provider_user_id, username, trust_level) VALUES (?, ?, 'linuxdo', '42', 'alice_ld', 2)`, uuid.New(), f.alice)
	exec(`INSERT INTO challenge_packs (id, slug, name) VALUES (?, 'admin-users', 'Admin Users')`, packID)
	exec(`INSERT INTO challenge_versions (id, challenge_pack_id, version, status, manifest, content_hash, published_at) VALUES (?, ?, '1.0.0', 'published', '{}'::jsonb, ?, CURRENT_TIMESTAMP)`, f.versionID, packID, hash)
	exec(`UPDATE challenge_packs SET current_version_id = ?, status = 'active' WHERE id = ?`, f.versionID, packID)
	exec(`INSERT INTO challenge_outcome_rules (id, challenge_version_id, outcome_key, category, title, verifier_key, verifier_config) VALUES (?, ?, 'win', 'success', 'Win', 'core.always', '{}'::jsonb)`, f.outcome, f.versionID)
	return f
}

func (f *adminUserFixture) conversation(t *testing.T, player uuid.UUID, title string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if err := f.db.Exec(`INSERT INTO conversations (id, player_id, title, challenge_version_id, challenge_hash, llm_route) VALUES (?, ?, ?, ?, ?, 'server')`,
		id, player, title, f.versionID, strings.Repeat("0", 64)).Error; err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *adminUserFixture) run(t *testing.T, player, conversation uuid.UUID, category string) {
	t.Helper()
	if err := f.db.Exec(`INSERT INTO challenge_runs (id, conversation_id, player_id, challenge_version_id, outcome_rule_id, outcome_key, category, title_snapshot, settlement_detail, settled_at)
		VALUES (?, ?, ?, ?, ?, 'win', ?, 'Win', '{}'::jsonb, CURRENT_TIMESTAMP)`, uuid.New(), conversation, player, f.versionID, f.outcome, category).Error; err != nil {
		t.Fatal(err)
	}
}

func TestPostgresAdminUserListFiltersAndSort(t *testing.T) {
	f := newAdminUserFixture(t)
	repo := NewAdminUserRepo(f.db)
	ctx := context.Background()
	names := func(filter AdminUserFilter) string {
		t.Helper()
		filter.Limit = 50
		rows, err := repo.List(ctx, filter)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]string, 0, len(rows))
		for _, row := range rows {
			out = append(out, row.Username)
		}
		return strings.Join(out, ",")
	}
	if got := names(AdminUserFilter{}); got != "carol,bob,Alice" {
		t.Fatalf("默认按注册时间倒序: %s", got)
	}
	if got := names(AdminUserFilter{Asc: true}); got != "Alice,bob,carol" {
		t.Fatalf("注册时间升序: %s", got)
	}
	// 从未登录的账号无论升降序都排在最后。
	if got := names(AdminUserFilter{Sort: "last_login_at"}); !strings.HasPrefix(got, "bob,") {
		t.Fatalf("最后登录倒序: %s", got)
	}
	if got := names(AdminUserFilter{Sort: "last_login_at", Asc: true}); !strings.HasPrefix(got, "bob,") {
		t.Fatalf("最后登录升序 NULL 应在最后: %s", got)
	}
	cases := map[string]AdminUserFilter{
		"Alice": {Registration: RegistrationLinuxDo},
		"bob":   {Registration: RegistrationEmail, Role: UserRoleUser},
		"carol": {Status: UserStatusDisabled, Role: UserRoleAdmin},
	}
	for want, filter := range cases {
		if got := names(filter); got != want {
			t.Fatalf("filter %+v: got %s want %s", filter, got, want)
		}
	}
	// 搜索里的 OR 条件与其他筛选组合时必须正确加括号：carol 已停用，不应被带出。
	if got := names(AdminUserFilter{Search: "a", Status: UserStatusActive}); got != "Alice" {
		t.Fatalf("search+status: %s", got)
	}
	if got := names(AdminUserFilter{Search: "alice"}); got != "Alice" {
		t.Fatalf("search: %s", got)
	}
	// 邮箱注册里 bob 有密码；carol 没有 LinuxDo 绑定也算邮箱。
	if got := names(AdminUserFilter{Registration: RegistrationEmail}); got != "carol,bob" {
		t.Fatalf("email registration: %s", got)
	}
	if id, err := repo.IDByUsername(ctx, " ALICE "); err != nil || id != f.alice {
		t.Fatalf("IDByUsername=%v err=%v", id, err)
	}
	if _, err := repo.IDByUsername(ctx, "nobody"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown username err=%v", err)
	}
}

func TestPostgresAdminUserDetailStats(t *testing.T) {
	f := newAdminUserFixture(t)
	repo := NewAdminUserRepo(f.db)
	ctx := context.Background()
	c1, c2, _ := f.conversation(t, f.alice, "a1"), f.conversation(t, f.alice, "a2"), f.conversation(t, f.alice, "a3")
	f.run(t, f.alice, c1, "success")
	f.run(t, f.alice, c2, "failure")
	f.conversation(t, f.bob, "b1")

	detail, err := repo.Detail(ctx, f.alice)
	if err != nil {
		t.Fatal(err)
	}
	if detail.RegistrationMethod != RegistrationLinuxDo || detail.LinuxDo == nil || detail.LinuxDo.Username != "alice_ld" || detail.LinuxDo.TrustLevel == nil || *detail.LinuxDo.TrustLevel != 2 {
		t.Fatalf("detail=%+v linuxdo=%+v", detail.AdminUserRow, detail.LinuxDo)
	}
	if want := (AdminUserPlayStats{Started: 3, Completed: 2, Success: 1, Failure: 1}); detail.Stats != want {
		t.Fatalf("stats=%+v want %+v", detail.Stats, want)
	}
	bob, err := repo.Detail(ctx, f.bob)
	if err != nil || bob.LinuxDo != nil || bob.RegistrationMethod != RegistrationEmail || bob.Stats != (AdminUserPlayStats{Started: 1}) {
		t.Fatalf("bob=%+v err=%v", bob, err)
	}
	if _, err := repo.Detail(ctx, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing user err=%v", err)
	}
}

func TestPostgresAdminUserCapturedConversationsAndLogFilters(t *testing.T) {
	f := newAdminUserFixture(t)
	repo := NewAdminUserRepo(f.db)
	logs := NewLLMRequestLogRepo(f.db)
	ctx := context.Background()
	aliceConv, bobConv := f.conversation(t, f.alice, "alice story"), f.conversation(t, f.bob, "bob story")
	now := time.Now().UTC().Truncate(time.Microsecond)
	create := func(user, conv uuid.UUID, model, status string, at time.Time) {
		t.Helper()
		row := &LLMRequestLog{ID: uuid.New(), UserID: &user, ConversationID: &conv, Model: model, BodyStatus: status, ResponseText: "reply-" + model,
			RequestBody: JSONDoc(`{"messages":[]}`), CreatedAt: at, StartedAt: at}
		if err := logs.Create(ctx, row); err != nil {
			t.Fatal(err)
		}
	}
	create(f.alice, aliceConv, "m1", "captured", now.Add(-2*time.Minute))
	create(f.alice, aliceConv, "m2", "truncated", now.Add(-1*time.Minute))
	create(f.alice, aliceConv, "m3", "disabled", now)
	create(f.bob, bobConv, "m1", "captured", now)

	convs, err := repo.CapturedConversations(ctx, f.alice, 10, 0)
	if err != nil || len(convs) != 1 || convs[0].ID != aliceConv || convs[0].RecordCount != 2 {
		t.Fatalf("convs=%+v err=%v", convs, err)
	}
	records, err := repo.CapturedRecords(ctx, f.alice, aliceConv, 10, 0)
	if err != nil || len(records) != 2 || records[0].Model != "m1" || records[1].Model != "m2" || records[0].ResponseText != "reply-m1" {
		t.Fatalf("records=%+v err=%v", records, err)
	}
	// 别人的对话、不存在的对话都不可读。
	if _, err := repo.CapturedRecords(ctx, f.alice, bobConv, 10, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user records err=%v", err)
	}

	// 模型调用日志：按用户筛选，并能与模型过滤叠加。
	rows, _, _, err := logs.List(ctx, LogQuery{Limit: 10, UserID: &f.alice})
	if err != nil || len(rows) != 3 {
		t.Fatalf("llm by user rows=%d err=%v", len(rows), err)
	}
	rows, _, _, err = logs.List(ctx, LogQuery{Limit: 10, UserID: &f.alice, Model: "m1"})
	if err != nil || len(rows) != 1 || rows[0].UserID == nil || *rows[0].UserID != f.alice {
		t.Fatalf("llm by user+model rows=%+v err=%v", rows, err)
	}

	// 上下文日志：经对话的玩家过滤。
	ctxRepo := NewContextRepo(f.db)
	for _, conv := range []uuid.UUID{aliceConv, bobConv} {
		if err := f.db.Exec(`INSERT INTO context_build_logs (id, conversation_id, challenge_version_id, challenge_hash, model, max_context_tokens, reserved_response_tokens, used_prompt_tokens, segments)
			VALUES (?, ?, ?, ?, 'ctx-model', 1, 1, 1, '[]'::jsonb)`, uuid.New(), conv, f.versionID, strings.Repeat("0", 64)).Error; err != nil {
			t.Fatal(err)
		}
	}
	ctxRows, _, _, err := ctxRepo.List(ctx, LogQuery{Limit: 10, UserID: &f.bob, Model: "ctx-model"})
	if err != nil || len(ctxRows) != 1 || ctxRows[0].ConversationID != bobConv {
		t.Fatalf("context rows=%+v err=%v", ctxRows, err)
	}
	owners, err := repo.ConversationUsers(ctx, []uuid.UUID{aliceConv, bobConv})
	if err != nil || owners[aliceConv].Username != "Alice" || owners[bobConv].Username != "bob" {
		t.Fatalf("owners=%+v err=%v", owners, err)
	}

	// 删除用户后日志保留但 user_id 置空。
	if err := f.db.Exec(`DELETE FROM conversations WHERE player_id = ?`, f.alice).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.db.Exec(`DELETE FROM users WHERE id = ?`, f.alice).Error; err != nil {
		t.Fatal(err)
	}
	var orphaned int64
	if err := f.db.Model(&LLMRequestLog{}).Where("user_id IS NULL AND model IN ('m2','m3')").Count(&orphaned).Error; err != nil || orphaned != 2 {
		t.Fatalf("orphaned=%d err=%v", orphaned, err)
	}
}

func TestPostgresAdminUserAuditFilters(t *testing.T) {
	f := newAdminUserFixture(t)
	repo := NewAdminUserRepo(f.db)
	auth := NewAuthRepository(f.db)
	ctx := context.Background()
	if err := repo.RecordAudit(ctx, f.carol, f.alice, "user_conversation_view", JSONMap{"conversation_id": "x"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordAudit(ctx, f.alice, f.bob, "user_conversation_view", JSONMap{}); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Exec(`INSERT INTO admin_audit_logs(id, actor_user_id, action, result, created_at) VALUES (?, ?, 'x', 'rejected', now())`, uuid.New(), f.carol).Error; err != nil {
		t.Fatal(err)
	}
	count := func(q LogQuery) int {
		t.Helper()
		q.Limit = 50
		rows, _, _, err := auth.ListAdminAuditLogs(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		return len(rows)
	}
	if n := count(LogQuery{UserID: &f.alice}); n != 2 {
		t.Fatalf("alice either: %d", n)
	}
	if n := count(LogQuery{UserID: &f.alice, UserRole: "target"}); n != 1 {
		t.Fatalf("alice target: %d", n)
	}
	if n := count(LogQuery{UserID: &f.alice, UserRole: "actor"}); n != 1 {
		t.Fatalf("alice actor: %d", n)
	}
	if n := count(LogQuery{UserID: &f.carol, Result: "rejected"}); n != 1 {
		t.Fatalf("carol rejected: %d", n)
	}
	if _, _, _, err := auth.ListAdminAuditLogs(ctx, LogQuery{Limit: 1, UserRole: "owner"}); !errors.Is(err, ErrLogQuery) {
		t.Fatalf("invalid role err=%v", err)
	}
}

func TestPostgresCreateSessionRecordsLastLogin(t *testing.T) {
	f := newAdminUserFixture(t)
	auth := NewAuthRepository(f.db)
	ctx := context.Background()
	before := time.Now().UTC().Truncate(time.Microsecond)
	var updatedBefore time.Time
	if err := f.db.Model(&User{}).Select("updated_at").Where("id = ?", f.alice).Scan(&updatedBefore).Error; err != nil {
		t.Fatal(err)
	}
	session := &AuthSession{TokenHash: strings.Repeat("a", 64), UserID: f.alice, ExpiresAt: before.Add(time.Hour), CreatedAt: before}
	if err := auth.CreateSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	var user User
	if err := f.db.First(&user, "id = ?", f.alice).Error; err != nil {
		t.Fatal(err)
	}
	if user.LastLoginAt == nil || !user.LastLoginAt.Equal(before) || !user.UpdatedAt.Equal(updatedBefore) {
		t.Fatalf("last_login_at=%v updated_at=%v (was %v)", user.LastLoginAt, user.UpdatedAt, updatedBefore)
	}
}
