package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	authservice "github.com/ialtone/fatecraft/internal/auth"
	"github.com/ialtone/fatecraft/internal/storage"
	"github.com/ialtone/fatecraft/internal/testdb"
	"gorm.io/gorm"
)

type adminUsersHarness struct {
	t      *testing.T
	db     *gorm.DB
	system *storage.SystemSettingsRepo
	router *gin.Engine
	admin  uuid.UUID
	player uuid.UUID
	conv   uuid.UUID
}

func newAdminUsersHarness(t *testing.T) *adminUsersHarness {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := testdb.Open(t, "api_admin_users")
	h := &adminUsersHarness{t: t, db: db, system: storage.NewSystemSettingsRepo(db), admin: uuid.New(), player: uuid.New(), conv: uuid.New()}
	versionID, packID, hash := uuid.New(), uuid.New(), strings.Repeat("0", 64)
	for _, st := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO users (id, username, display_name, role) VALUES (?, 'root', 'Root', 'admin')`, []any{h.admin}},
		{`INSERT INTO users (id, username, display_name) VALUES (?, 'Player1', 'Player One')`, []any{h.player}},
		{`INSERT INTO challenge_packs (id, slug, name) VALUES (?, 'api-admin-users', 'x')`, []any{packID}},
		{`INSERT INTO challenge_versions (id, challenge_pack_id, version, status, manifest, content_hash, published_at) VALUES (?, ?, '1.0.0', 'published', '{}'::jsonb, ?, CURRENT_TIMESTAMP)`, []any{versionID, packID, hash}},
		{`UPDATE challenge_packs SET current_version_id = ?, status = 'active' WHERE id = ?`, []any{versionID, packID}},
		{`INSERT INTO conversations (id, player_id, title, challenge_version_id, challenge_hash, llm_route) VALUES (?, ?, 'story', ?, ?, 'server')`, []any{h.conv, h.player, versionID, hash}},
	} {
		if err := db.Exec(st.query, st.args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	logs := storage.NewLLMRequestLogRepo(db)
	now := time.Now().UTC()
	row := &storage.LLMRequestLog{ID: uuid.New(), UserID: &h.player, ConversationID: &h.conv, Model: "m", BodyStatus: "captured",
		RequestBody: storage.JSONDoc(`{"messages":["hi"]}`), ResponseText: "hello", CreatedAt: now, StartedAt: now}
	if err := logs.Create(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	server := &Server{system: h.system, adminUsers: storage.NewAdminUserRepo(db), llmRequestLogs: logs, ctxLogs: storage.NewContextRepo(db), auth: authservice.NewService(storage.NewAuthRepository(db), authservice.Config{})}
	h.router = gin.New()
	h.router.Use(func(c *gin.Context) {
		var user storage.User
		if err := db.First(&user, "id = ?", h.admin).Error; err != nil {
			t.Fatal(err)
		}
		c.Set(authUserKey, &user)
	})
	h.router.GET("/users", server.listAdminUsers)
	h.router.GET("/users/:id", server.getAdminUser)
	h.router.GET("/users/:id/conversations", server.listAdminUserConversations)
	h.router.GET("/users/:id/conversations/:conversation_id/records", server.listAdminUserConversationRecords)
	h.router.POST("/users/:id/detailed-logging", server.enableAdminUserDetailedLogging)
	h.router.GET("/llm-request-logs", server.listAdminLLMRequestLogs)
	h.router.GET("/audit-logs", server.listAdminAuditLogs)
	return h
}

func (h *adminUsersHarness) do(method, path string) (int, map[string]any) {
	h.t.Helper()
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	body := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return w.Code, body
}

func (h *adminUsersHarness) setLogging(enabled bool, scope string, target *uuid.UUID) {
	h.t.Helper()
	settings, err := h.system.Get(context.Background())
	if err != nil {
		h.t.Fatal(err)
	}
	settings.DetailedLLMLoggingEnabled, settings.DetailedLLMLoggingScope, settings.DetailedLLMLoggingTargetID = enabled, scope, target
	if err := h.system.Save(context.Background(), settings); err != nil {
		h.t.Fatal(err)
	}
}

func (h *adminUsersHarness) auditCount(action string) int64 {
	h.t.Helper()
	var n int64
	if err := h.db.Model(&storage.AdminAuditLog{}).Where("action = ?", action).Count(&n).Error; err != nil {
		h.t.Fatal(err)
	}
	return n
}

func TestPostgresAdminUserConversationsRequireDetailedLogging(t *testing.T) {
	h := newAdminUsersHarness(t)
	convPath := "/users/" + h.player.String() + "/conversations"
	recordsPath := convPath + "/" + h.conv.String() + "/records"
	other := uuid.New()
	h.db.Exec(`INSERT INTO users (id, username, display_name) VALUES (?, 'other', 'Other')`, other)

	// 未开启、范围指向别人、范围为单个对话：一律 403，且不写审计、不泄露内容。
	for _, setup := range []func(){
		func() { h.setLogging(false, "all", nil) },
		func() { h.setLogging(true, "user", &other) },
		func() { h.setLogging(true, "conversation", &h.conv) },
	} {
		setup()
		for _, path := range []string{convPath, recordsPath} {
			code, body := h.do("GET", path)
			if code != http.StatusForbidden || body["code"] != "detailed_logging_required" || body["records"] != nil || body["conversations"] != nil {
				t.Fatalf("%s gate: %d %v", path, code, body)
			}
		}
	}
	if n := h.auditCount("user_conversations_view") + h.auditCount("user_conversation_view"); n != 0 {
		t.Fatalf("被拒绝的请求不应写查看审计: %d", n)
	}
	_, detail := h.do("GET", "/users/"+h.player.String())
	if detail["detailed_logging"].(map[string]any)["covered"] != false {
		t.Fatalf("detail=%v", detail)
	}

	// 一键开启：范围收窄为该用户；重复调用幂等，不再写审计。
	if code, body := h.do("POST", "/users/"+h.player.String()+"/detailed-logging"); code != 200 || body["detailed_logging"].(map[string]any)["covered"] != true {
		t.Fatalf("enable: %d %v", code, body)
	}
	h.do("POST", "/users/"+h.player.String()+"/detailed-logging")
	settings, _ := h.system.Get(context.Background())
	if !settings.DetailedLLMLoggingEnabled || settings.DetailedLLMLoggingScope != "user" || *settings.DetailedLLMLoggingTargetID != h.player {
		t.Fatalf("settings=%+v", settings)
	}
	if n := h.auditCount("user_detailed_logging_enable"); n != 1 {
		t.Fatalf("enable audit=%d", n)
	}
	if code, _ := h.do("POST", "/users/"+uuid.NewString()+"/detailed-logging"); code != 404 {
		t.Fatalf("unknown user enable: %d", code)
	}

	// 已覆盖：能看到对话与采集到的内容，并且每次查看都写审计。
	code, body := h.do("GET", convPath)
	if convs, _ := body["conversations"].([]any); code != 200 || len(convs) != 1 {
		t.Fatalf("conversations: %d %v", code, body)
	}
	code, body = h.do("GET", recordsPath)
	records, _ := body["records"].([]any)
	if code != 200 || len(records) != 1 || records[0].(map[string]any)["response_text"] != "hello" {
		t.Fatalf("records: %d %v", code, body)
	}
	if h.auditCount("user_conversations_view") != 1 || h.auditCount("user_conversation_view") != 1 {
		t.Fatal("每次查看都应写一条审计")
	}
	var audit storage.AdminAuditLog
	if err := h.db.Where("action = 'user_conversation_view'").Take(&audit).Error; err != nil ||
		audit.ActorUserID == nil || *audit.ActorUserID != h.admin || audit.TargetUserID == nil || *audit.TargetUserID != h.player || audit.Metadata["conversation_id"] != h.conv.String() {
		t.Fatalf("audit=%+v err=%v", audit, err)
	}
	// 别人的对话 404，且不写审计。
	if code, _ := h.do("GET", convPath+"/"+uuid.NewString()+"/records"); code != 404 {
		t.Fatalf("foreign conversation: %d", code)
	}
	if h.auditCount("user_conversation_view") != 1 {
		t.Fatal("404 不应写审计")
	}
}

func TestPostgresAdminUserConversationAuditFailureWithholdsContent(t *testing.T) {
	h := newAdminUsersHarness(t)
	h.setLogging(true, "all", nil)
	if err := h.db.Exec(`CREATE FUNCTION reject_view_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action = 'user_conversation_view' THEN RAISE EXCEPTION 'audit down'; END IF; RETURN NEW; END $$`).Error; err != nil {
		t.Fatal(err)
	}
	if err := h.db.Exec(`CREATE TRIGGER reject_view_audit BEFORE INSERT ON admin_audit_logs FOR EACH ROW EXECUTE FUNCTION reject_view_audit()`).Error; err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, httptest.NewRequest("GET", "/users/"+h.player.String()+"/conversations/"+h.conv.String()+"/records", nil))
	if w.Code != 500 || strings.Contains(w.Body.String(), "hello") {
		t.Fatalf("审计失败时不得返回内容: %d %s", w.Code, w.Body)
	}
}

func TestPostgresAdminUserListDetailAndLogFilters(t *testing.T) {
	h := newAdminUsersHarness(t)
	code, body := h.do("GET", "/users?sort=last_login_at&order=asc&status=active&registration=email")
	users, _ := body["users"].([]any)
	if code != 200 || len(users) != 2 || users[0].(map[string]any)["registration_method"] != "email" {
		t.Fatalf("list: %d %v", code, body)
	}
	for _, bad := range []string{"sort=name", "order=up", "status=x", "role=root", "registration=github"} {
		if code, _ := h.do("GET", "/users?"+bad); code != 400 {
			t.Fatalf("%s accepted: %d", bad, code)
		}
	}
	_, body = h.do("GET", "/users/"+h.player.String())
	stats := body["user"].(map[string]any)["stats"].(map[string]any)
	if stats["started"] != float64(1) || stats["completed"] != float64(0) {
		t.Fatalf("detail stats=%v", stats)
	}

	// 用户 ID 过滤：按 username（大小写不敏感）解析；日志条目附带 username 而非只给 UUID。
	_, body = h.do("GET", "/llm-request-logs?user=player1")
	logs, _ := body["logs"].([]any)
	if len(logs) != 1 || logs[0].(map[string]any)["user"].(map[string]any)["username"] != "Player1" {
		t.Fatalf("llm logs by user: %v", body)
	}
	_, body = h.do("GET", "/llm-request-logs?user=root")
	if logs, _ := body["logs"].([]any); len(logs) != 0 {
		t.Fatalf("other user's logs leaked: %v", body)
	}
	if code, _ := h.do("GET", "/llm-request-logs?user=nobody"); code != 404 {
		t.Fatalf("unknown user filter: %d", code)
	}
	h.do("POST", "/users/"+h.player.String()+"/detailed-logging")
	_, body = h.do("GET", "/audit-logs?user=root&user_role=actor&result=success")
	auditLogs, _ := body["logs"].([]any)
	if len(auditLogs) == 0 || auditLogs[0].(map[string]any)["actor"].(map[string]any)["username"] != "root" {
		t.Fatalf("audit by user: %v", body)
	}
	if code, _ := h.do("GET", "/audit-logs?user_role=owner"); code != 400 {
		t.Fatalf("invalid user_role: %d", code)
	}
}
