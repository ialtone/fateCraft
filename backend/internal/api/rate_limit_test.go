package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	authservice "github.com/ialtone/fatecraft/internal/auth"
	"github.com/ialtone/fatecraft/internal/storage"
)

// loginOnlyRepo 满足 LoginEmail 的前置检查；格式错误的标识符随后直接判定凭据无效。
type loginOnlyRepo struct{ authservice.Repository }

func (loginOnlyRepo) InstallationComplete(context.Context) (bool, error) { return true, nil }
func (loginOnlyRepo) GetAuthSettings(context.Context) (*storage.AuthSettings, error) {
	return &storage.AuthSettings{EmailAuthEnabled: true}, nil
}

type fixedPlayer string

func (p fixedPlayer) CurrentPlayerID(context.Context) string { return string(p) }

func TestLoginFailuresAndPlayerActionsAreRateLimited(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{auth: authservice.NewService(loginOnlyRepo{}, authservice.Config{}), limits: newFixedWindow()}
	r := gin.New()
	r.POST("/login", s.loginEmail)
	login := func(identifier, ip string) int {
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"identifier":"`+identifier+`","password":"wrong"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = ip + ":1234"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	for i := 0; i < 5; i++ {
		if code := login("victim@", "10.0.0.1"); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d", i+1, code)
		}
	}
	if code := login("VICTIM@", "10.0.0.2"); code != http.StatusTooManyRequests {
		t.Fatalf("locked account status = %d", code)
	}
	if code := login("other@", "10.0.0.2"); code != http.StatusUnauthorized {
		t.Fatalf("other account status = %d", code)
	}

	action := func(player string) bool {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
		s.identity = fixedPlayer(player)
		return s.allowPlayerAction(c)
	}
	for i := 0; i < 30; i++ {
		if !action("p1") {
			t.Fatalf("action %d rejected", i+1)
		}
	}
	if action("p1") || !action("p2") {
		t.Fatal("31st action must be limited per player only")
	}
}
