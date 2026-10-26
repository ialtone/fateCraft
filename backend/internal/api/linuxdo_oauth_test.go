package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	authservice "github.com/ialtone/fatecraft/internal/auth"
	"github.com/ialtone/fatecraft/internal/storage"
)

// oauthTestRepo 只实现 LinuxDo 流程用到的存储方法；嵌入接口使其余方法被误调用时直接 panic。
type oauthTestRepo struct {
	authservice.Repository

	mu          sync.Mutex
	flows       map[string]*storage.OAuthLoginFlow
	sessions    map[string]*storage.User
	loginUser   *storage.User
	bindErr     error
	boundUserID *uuid.UUID
	registered  *storage.User
}

func newOAuthTestRepo() *oauthTestRepo {
	return &oauthTestRepo{flows: map[string]*storage.OAuthLoginFlow{}, sessions: map[string]*storage.User{}}
}

func (r *oauthTestRepo) InstallationComplete(context.Context) (bool, error) { return true, nil }

func (r *oauthTestRepo) GetAuthSettings(context.Context) (*storage.AuthSettings, error) {
	return &storage.AuthSettings{LinuxDOLoginEnabled: true}, nil
}

func (r *oauthTestRepo) CreateOAuthFlow(_ context.Context, flow *storage.OAuthLoginFlow) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flows[flow.StateHash] = flow
	return nil
}

func (r *oauthTestRepo) ConsumeOAuthFlow(_ context.Context, provider, stateHash string, now time.Time) (*storage.OAuthLoginFlow, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	flow, ok := r.flows[stateHash]
	if !ok || flow.Provider != provider {
		return nil, storage.ErrNotFound
	}
	delete(r.flows, stateHash)
	if !flow.ExpiresAt.After(now) {
		return nil, storage.ErrNotFound
	}
	return flow, nil
}

func (r *oauthTestRepo) CreateSession(_ context.Context, session *storage.AuthSession) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[session.TokenHash] = &storage.User{ID: session.UserID, Status: storage.UserStatusActive}
	return nil
}

func (r *oauthTestRepo) UserBySession(_ context.Context, tokenHash string, _ time.Time) (*storage.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if user, ok := r.sessions[tokenHash]; ok {
		return user, nil
	}
	return nil, storage.ErrNotFound
}

func (r *oauthTestRepo) ExternalUserByProviderSubject(context.Context, string, string) (*storage.User, error) {
	if r.loginUser == nil {
		return nil, storage.ErrIdentityNotFound
	}
	return r.loginUser, nil
}

func (r *oauthTestRepo) RegisterExternalUser(_ context.Context, user *storage.User, _ storage.ExternalIdentityInput) error {
	r.registered = user
	return nil
}

func (r *oauthTestRepo) BindExternalIdentity(_ context.Context, userID uuid.UUID, in storage.ExternalIdentityInput) (*storage.ExternalIdentitySummary, error) {
	if r.bindErr != nil {
		return nil, r.bindErr
	}
	r.boundUserID = &userID
	return &storage.ExternalIdentitySummary{ID: uuid.New(), Provider: in.Provider, ProviderUserID: in.ProviderUserID}, nil
}

// addSession 为用户登记一个原始会话令牌，返回可直接附加到请求上的 Cookie。
func (r *oauthTestRepo) addSession(user *storage.User, token string) *http.Cookie {
	sum := sha256.Sum256([]byte(token))
	r.mu.Lock()
	r.sessions[hex.EncodeToString(sum[:])] = user
	r.mu.Unlock()
	return &http.Cookie{Name: sessionCookieName, Value: token}
}

type oauthTestEnv struct {
	repo          *oauthTestRepo
	router        *gin.Engine
	providerCalls *int32
}

func newOAuthTestEnv(t *testing.T, cookieSecure bool) *oauthTestEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	var calls int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			_, _ = w.Write([]byte(`{"access_token":"access-token"}`))
		case "/user":
			_, _ = w.Write([]byte(`{"sub":"linuxdo-42","username":"fate","active":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(provider.Close)
	repo := newOAuthTestRepo()
	server := &Server{
		auth: authservice.NewService(repo, authservice.Config{
			LinuxDOClientID: "client-id", LinuxDOClientSecret: "secret",
			LinuxDORedirectURI: "http://localhost/api/auth/linuxdo/callback",
			AuthorizationURL:   "https://connect.example/oauth2/authorize",
			TokenURL:           provider.URL + "/token", UserInfoURL: provider.URL + "/user",
		}),
		cookieSecure: cookieSecure,
	}
	router := gin.New()
	router.GET("/api/auth/linuxdo/start", server.startLinuxDO)
	router.GET("/api/auth/linuxdo/bind/start", server.requireAuth, server.startLinuxDOBind)
	router.GET("/api/auth/linuxdo/callback", server.completeLinuxDO)
	return &oauthTestEnv{repo: repo, router: router, providerCalls: &calls}
}

func (e *oauthTestEnv) do(t *testing.T, target string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	e.router.ServeHTTP(recorder, request)
	return recorder
}

// start 发起授权并返回 state 与浏览器绑定 Cookie。
func (e *oauthTestEnv) start(t *testing.T, target string, cookies ...*http.Cookie) (string, *http.Cookie) {
	t.Helper()
	recorder := e.do(t, target, cookies...)
	if recorder.Code != http.StatusFound {
		t.Fatalf("start status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	location, err := url.Parse(recorder.Header().Get("Location"))
	if err != nil || location.Host != "connect.example" {
		t.Fatalf("unexpected authorize redirect: %q", recorder.Header().Get("Location"))
	}
	binding := responseCookie(recorder, linuxDOBindingCookieName)
	if binding == nil || binding.Value == "" {
		t.Fatal("start did not set the browser binding cookie")
	}
	return location.Query().Get("state"), &http.Cookie{Name: binding.Name, Value: binding.Value}
}

func responseCookie(recorder *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func redirectTarget(t *testing.T, recorder *httptest.ResponseRecorder) *url.URL {
	t.Helper()
	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302; body=%s", recorder.Code, recorder.Body.String())
	}
	target, err := url.Parse(recorder.Header().Get("Location"))
	if err != nil || target.Host != "" || target.Scheme != "" {
		t.Fatalf("unexpected Location %q", recorder.Header().Get("Location"))
	}
	return target
}

func assertBindingCookieCleared(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	cookie := responseCookie(recorder, linuxDOBindingCookieName)
	if cookie == nil || cookie.MaxAge >= 0 || cookie.Value != "" || cookie.Path != linuxDOBindingCookiePath {
		t.Fatalf("callback must clear the browser binding cookie: %+v", cookie)
	}
}

func TestLinuxDOStartSetsBrowserBindingCookie(t *testing.T) {
	for _, secure := range []bool{false, true} {
		env := newOAuthTestEnv(t, secure)
		recorder := env.do(t, "/api/auth/linuxdo/start?return_to=/settings")
		if recorder.Code != http.StatusFound {
			t.Fatalf("status = %d", recorder.Code)
		}
		cookie := responseCookie(recorder, linuxDOBindingCookieName)
		if cookie == nil || cookie.Value == "" {
			t.Fatal("binding cookie missing")
		}
		if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/api/auth/linuxdo" ||
			cookie.MaxAge != 600 || cookie.Secure != secure {
			t.Fatalf("unexpected cookie attributes (secure=%v): %+v", secure, cookie)
		}
		// 库里只保存 nonce 摘要。
		sum := sha256.Sum256([]byte(cookie.Value))
		var stored *storage.OAuthLoginFlow
		for _, flow := range env.repo.flows {
			stored = flow
		}
		if stored == nil || stored.BrowserBindingHash != hex.EncodeToString(sum[:]) || stored.ReturnTo != "/settings" {
			t.Fatalf("flow was not bound to the cookie: %+v", stored)
		}
	}
}

func TestLinuxDOLoginCallback(t *testing.T) {
	t.Run("forwarded_callback_without_binding_cookie_is_rejected", func(t *testing.T) {
		env := newOAuthTestEnv(t, false)
		env.repo.loginUser = &storage.User{ID: uuid.New(), Status: storage.UserStatusActive}
		state, _ := env.start(t, "/api/auth/linuxdo/start?return_to=/conversations")
		recorder := env.do(t, "/api/auth/linuxdo/callback?code=auth-code&state="+url.QueryEscape(state))
		target := redirectTarget(t, recorder)
		if target.Path != "/login" || target.Query().Get("error") != "LinuxDo 登录流程已失效，请重新发起登录" {
			t.Fatalf("unexpected redirect: %s", target)
		}
		if responseCookie(recorder, sessionCookieName) != nil || atomic.LoadInt32(env.providerCalls) != 0 {
			t.Fatal("rejected callback must not exchange code or create a session")
		}
		assertBindingCookieCleared(t, recorder)
	})

	t.Run("bound_identity_logs_in", func(t *testing.T) {
		env := newOAuthTestEnv(t, false)
		env.repo.loginUser = &storage.User{ID: uuid.New(), Status: storage.UserStatusActive}
		state, binding := env.start(t, "/api/auth/linuxdo/start?return_to=/conversations")
		recorder := env.do(t, "/api/auth/linuxdo/callback?code=auth-code&state="+url.QueryEscape(state), binding)
		target := redirectTarget(t, recorder)
		if target.String() != "/conversations" {
			t.Fatalf("redirect = %s", target)
		}
		if session := responseCookie(recorder, sessionCookieName); session == nil || session.Value == "" {
			t.Fatal("session cookie was not set")
		}
		assertBindingCookieCleared(t, recorder)
	})

	t.Run("unbound_identity_registers_and_logs_in", func(t *testing.T) {
		env := newOAuthTestEnv(t, false)
		state, binding := env.start(t, "/api/auth/linuxdo/start?return_to=/conversations")
		recorder := env.do(t, "/api/auth/linuxdo/callback?code=auth-code&state="+url.QueryEscape(state), binding)
		target := redirectTarget(t, recorder)
		if target.String() != "/conversations" || env.repo.registered == nil {
			t.Fatalf("redirect = %s registered = %v", target, env.repo.registered)
		}
		if session := responseCookie(recorder, sessionCookieName); session == nil || session.Value == "" {
			t.Fatal("session cookie was not set")
		}
	})

	t.Run("provider_error_without_state_falls_back_to_login", func(t *testing.T) {
		env := newOAuthTestEnv(t, false)
		recorder := env.do(t, "/api/auth/linuxdo/callback?error=access_denied")
		target := redirectTarget(t, recorder)
		if target.Path != "/login" || target.Query().Get("error") != "已取消 LinuxDo 授权" {
			t.Fatalf("unexpected redirect: %s", target)
		}
		assertBindingCookieCleared(t, recorder)
	})
}

func TestLinuxDOBindCallback(t *testing.T) {
	owner := &storage.User{ID: uuid.New(), Role: storage.UserRoleUser, Status: storage.UserStatusActive}
	other := &storage.User{ID: uuid.New(), Role: storage.UserRoleUser, Status: storage.UserStatusActive}

	t.Run("initiating_session_binds_and_returns", func(t *testing.T) {
		env := newOAuthTestEnv(t, false)
		session := env.repo.addSession(owner, "owner-session")
		state, binding := env.start(t, "/api/auth/linuxdo/bind/start?return_to=/settings", session)
		recorder := env.do(t, "/api/auth/linuxdo/callback?code=auth-code&state="+url.QueryEscape(state), binding, session)
		target := redirectTarget(t, recorder)
		if target.Path != "/settings" || target.Query().Get(linuxDOBindQuery) != "ok" || target.Query().Get(linuxDOBindErrorQuery) != "" {
			t.Fatalf("unexpected redirect: %s", target)
		}
		if env.repo.boundUserID == nil || *env.repo.boundUserID != owner.ID {
			t.Fatalf("identity bound to %v, want %v", env.repo.boundUserID, owner.ID)
		}
		if responseCookie(recorder, sessionCookieName) != nil {
			t.Fatal("bind callback must not replace the current session")
		}
		assertBindingCookieCleared(t, recorder)
	})

	for _, test := range []struct {
		name    string
		session func(env *oauthTestEnv) []*http.Cookie
	}{
		{name: "no_session", session: func(*oauthTestEnv) []*http.Cookie { return nil }},
		{name: "other_user_session", session: func(env *oauthTestEnv) []*http.Cookie {
			return []*http.Cookie{env.repo.addSession(other, "other-session")}
		}},
	} {
		t.Run("rejects_"+test.name, func(t *testing.T) {
			env := newOAuthTestEnv(t, false)
			state, binding := env.start(t, "/api/auth/linuxdo/bind/start?return_to=/settings", env.repo.addSession(owner, "owner-session"))
			cookies := append([]*http.Cookie{binding}, test.session(env)...)
			recorder := env.do(t, "/api/auth/linuxdo/callback?code=auth-code&state="+url.QueryEscape(state), cookies...)
			target := redirectTarget(t, recorder)
			if target.Path != "/settings" || target.Query().Get(linuxDOBindErrorQuery) != "绑定需要在发起绑定的账号保持登录时完成，请重新发起绑定" {
				t.Fatalf("unexpected redirect: %s", target)
			}
			if env.repo.boundUserID != nil || atomic.LoadInt32(env.providerCalls) != 0 {
				t.Fatal("mismatched session must not bind or exchange code")
			}
		})
	}

	t.Run("identity_owned_by_other_user", func(t *testing.T) {
		env := newOAuthTestEnv(t, false)
		env.repo.bindErr = storage.ErrIdentityAlreadyBound
		session := env.repo.addSession(owner, "owner-session")
		state, binding := env.start(t, "/api/auth/linuxdo/bind/start?return_to=/settings", session)
		recorder := env.do(t, "/api/auth/linuxdo/callback?code=auth-code&state="+url.QueryEscape(state), binding, session)
		target := redirectTarget(t, recorder)
		if target.Path != "/settings" || target.Query().Get(linuxDOBindErrorQuery) != "该 LinuxDo 账户已绑定到其他站内账号，需要先在原账号中解除绑定" {
			t.Fatalf("unexpected redirect: %s", target)
		}
	})

	t.Run("provider_cancel_returns_to_settings", func(t *testing.T) {
		env := newOAuthTestEnv(t, false)
		session := env.repo.addSession(owner, "owner-session")
		state, binding := env.start(t, "/api/auth/linuxdo/bind/start?return_to=/settings", session)
		recorder := env.do(t, "/api/auth/linuxdo/callback?error=access_denied&state="+url.QueryEscape(state), binding, session)
		target := redirectTarget(t, recorder)
		if target.Path != "/settings" || target.Query().Get(linuxDOBindErrorQuery) != "已取消 LinuxDo 授权，未绑定任何账户" {
			t.Fatalf("unexpected redirect: %s", target)
		}
		if len(env.repo.flows) != 0 {
			t.Fatal("cancelled flow must be consumed")
		}
		assertBindingCookieCleared(t, recorder)
	})
}
