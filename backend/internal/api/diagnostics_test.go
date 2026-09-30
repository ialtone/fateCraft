package api

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/llmsettings"
	"github.com/ialtone/fatecraft/internal/logging"
	"github.com/ialtone/fatecraft/internal/storage"
	"github.com/ialtone/fatecraft/internal/testdb"
)

func TestPostgresConfigFailureDoesNotCloseRelay(t *testing.T) {
	db := testdb.Open(t, "api_config_audit")
	system := storage.NewSystemSettingsRepo(db)
	initial, err := system.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	initial.ClientLLMRouteEnabled = true
	if err := system.Save(context.Background(), initial); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE FUNCTION reject_api_config_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic-secret audit write failed'; END $$`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER reject_api_config_audit BEFORE INSERT ON admin_audit_logs FOR EACH ROW EXECUTE FUNCTION reject_api_config_audit()`).Error; err != nil {
		t.Fatal(err)
	}
	relays := &fakeRelayRegistry{}
	server := &Server{system: system, llmSettings: llmsettings.New(&clientRouteStubUsers{stored: validClientRouteUserSettings()}, system, ""), relays: relays}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("PATCH", "/", strings.NewReader(`{"client_llm_route_enabled":false}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(authUserKey, &storage.User{ID: uuid.New(), Role: storage.UserRoleOwner})
	server.updateAdminSystemSettings(c)
	stored, err := system.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if w.Code != 500 || relays.closeAllCalls != 0 || !stored.ClientLLMRouteEnabled || strings.Contains(w.Body.String(), "synthetic-secret") {
		t.Fatalf("configuration failure leaked side effects: status=%d closes=%d", w.Code, relays.closeAllCalls)
	}
}

func TestInternalFailuresArePrivateAndCorrelated(t *testing.T) {
	for _, handler := range []func(*gin.Context, error){fail, authFail, upstreamFailure} {
		r := gin.New()
		r.Use(logging.GinLogger(), logging.GinRecovery())
		r.GET("/error", func(c *gin.Context) {
			handler(c, errors.New("internal database synthetic-secret"))
			if len(c.Errors) != 1 {
				t.Error("internal failure not attached to access event")
			}
		})
		response := httptest.NewRecorder()
		r.ServeHTTP(response, httptest.NewRequest("GET", "/error", nil))
		if response.Code < 500 || strings.Contains(response.Body.String(), "synthetic-secret") || !strings.Contains(response.Body.String(), response.Header().Get("X-Request-ID")) {
			t.Fatalf("unsafe failure: %d %s", response.Code, response.Body)
		}
	}
}

func TestLogQueryRejectsInvalidFilters(t *testing.T) {
	for _, query := range []string{"limit=201", "from=nope", "from=2026-09-30T00:00:00Z&until=2026-09-29T00:00:00Z", "trace_id=not-uuid", "success=nope"} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/?"+query, nil)
		if _, ok := logQuery(c, "llm"); ok || w.Code != 400 {
			t.Fatalf("invalid filter accepted: %s", query)
		}
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/", nil)
	fail(c, storage.ErrSettingsConflict)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "settings_conflict") {
		t.Fatal("business conflict lost its code")
	}
}
