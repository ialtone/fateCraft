package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/ialtone/fatecraft/internal/storage"
)

type fakeRepository struct {
	flow           *storage.OAuthLoginFlow
	user           *storage.User
	session        *storage.AuthSession
	settings       *storage.AuthSettings
	verification   *storage.EmailVerificationCode
	invitations    []storage.InvitationCode
	verificationID *uuid.UUID
	invitationHash *string
	identities     []storage.ExternalIdentitySummary
	// registerErr 模拟注册事务失败（例如唯一索引冲突）。
	registerErr error
	// bindErr 模拟绑定失败；boundUserID 记录实际绑定到的用户。
	bindErr     error
	boundUserID *uuid.UUID
	// lastIdentifier 记录登录时传给存储层的标识。
	lastIdentifier string
	// unbindPasswordLoginEnabled 记录解绑时传入的全站密码登录开关。
	unbindPasswordLoginEnabled *bool
}

func (f *fakeRepository) CreateOAuthFlow(_ context.Context, flow *storage.OAuthLoginFlow) error {
	f.flow = flow
	return nil
}
func (f *fakeRepository) ConsumeOAuthFlow(_ context.Context, provider, hash string, now time.Time) (*storage.OAuthLoginFlow, error) {
	if f.flow == nil || f.flow.Provider != provider || f.flow.StateHash != hash || !f.flow.ExpiresAt.After(now) {
		return nil, storage.ErrNotFound
	}
	flow := f.flow
	f.flow = nil
	return flow, nil
}
func (f *fakeRepository) CreateSession(_ context.Context, session *storage.AuthSession) error {
	f.session = session
	return nil
}
func (f *fakeRepository) DeleteSession(context.Context, string) error { return nil }
func (f *fakeRepository) UserBySession(context.Context, string, time.Time) (*storage.User, error) {
	return f.user, nil
}
func (f *fakeRepository) UserByEmail(context.Context, string) (*storage.User, error) {
	return nil, storage.ErrNotFound
}

// UserByIdentifier 模拟存储层规则：含 @ 只匹配邮箱，否则按用户名大小写不敏感匹配。
func (f *fakeRepository) UserByIdentifier(_ context.Context, identifier string) (*storage.User, error) {
	f.lastIdentifier = identifier
	if f.user == nil {
		return nil, storage.ErrNotFound
	}
	if strings.Contains(identifier, "@") {
		if f.user.Email != nil && *f.user.Email == identifier {
			return f.user, nil
		}
		return nil, storage.ErrNotFound
	}
	if strings.EqualFold(f.user.Username, identifier) {
		return f.user, nil
	}
	return nil, storage.ErrNotFound
}
func (f *fakeRepository) RegisterEmailUser(_ context.Context, user *storage.User, verificationID *uuid.UUID, invitationHash *string, _ time.Time) error {
	if f.registerErr != nil {
		return f.registerErr
	}
	f.user = user
	f.verificationID = verificationID
	f.invitationHash = invitationHash
	return nil
}
func (f *fakeRepository) ExternalUserByProviderSubject(_ context.Context, _, _ string) (*storage.User, error) {
	if f.user == nil {
		return nil, storage.ErrIdentityNotFound
	}
	return f.user, nil
}
func (f *fakeRepository) BindExternalIdentity(_ context.Context, userID uuid.UUID, in storage.ExternalIdentityInput) (*storage.ExternalIdentitySummary, error) {
	if f.bindErr != nil {
		return nil, f.bindErr
	}
	f.boundUserID = &userID
	return &storage.ExternalIdentitySummary{ID: uuid.New(), Provider: in.Provider, ProviderUserID: in.ProviderUserID, Username: in.Username}, nil
}
func (f *fakeRepository) ListExternalIdentities(context.Context, uuid.UUID) ([]storage.ExternalIdentitySummary, error) {
	return f.identities, nil
}
func (f *fakeRepository) ListAllExternalIdentities(context.Context, int, int) ([]storage.AdminExternalIdentitySummary, error) {
	return nil, nil
}
func (f *fakeRepository) UnbindExternalIdentity(_ context.Context, _, _ uuid.UUID, passwordLoginEnabled bool) error {
	f.unbindPasswordLoginEnabled = &passwordLoginEnabled
	return nil
}
func (f *fakeRepository) GetAuthSettings(context.Context) (*storage.AuthSettings, error) {
	if f.settings == nil {
		f.settings = storage.DefaultAuthSettings()
	}
	copy := *f.settings
	return &copy, nil
}
func (f *fakeRepository) SaveAuthSettings(_ context.Context, settings *storage.AuthSettings) error {
	copy := *settings
	f.settings = &copy
	return nil
}
func (f *fakeRepository) LatestEmailVerification(context.Context, string, string) (*storage.EmailVerificationCode, error) {
	if f.verification == nil {
		return nil, storage.ErrNotFound
	}
	return f.verification, nil
}
func (f *fakeRepository) CreateEmailVerification(_ context.Context, code *storage.EmailVerificationCode) error {
	f.verification = code
	return nil
}
func (f *fakeRepository) DeleteEmailVerification(context.Context, uuid.UUID) error {
	f.verification = nil
	return nil
}
func (f *fakeRepository) IncrementEmailVerificationAttempts(context.Context, uuid.UUID) error {
	if f.verification != nil {
		f.verification.AttemptCount++
	}
	return nil
}
func (f *fakeRepository) ListInvitationCodes(context.Context) ([]storage.InvitationCode, error) {
	return f.invitations, nil
}
func (f *fakeRepository) InvitationCodeByID(_ context.Context, id uuid.UUID) (*storage.InvitationCode, error) {
	for i := range f.invitations {
		if f.invitations[i].ID == id {
			return &f.invitations[i], nil
		}
	}
	return nil, storage.ErrNotFound
}
func (f *fakeRepository) CreateInvitationCode(_ context.Context, invitation *storage.InvitationCode) error {
	f.invitations = append(f.invitations, *invitation)
	return nil
}
func (f *fakeRepository) UpdateInvitationCode(_ context.Context, invitation *storage.InvitationCode) error {
	for i := range f.invitations {
		if f.invitations[i].ID == invitation.ID {
			f.invitations[i] = *invitation
		}
	}
	return nil
}
func (f *fakeRepository) InstallationComplete(context.Context) (bool, error) { return true, nil }
func (f *fakeRepository) InitializeOwner(_ context.Context, user *storage.User) (*storage.User, error) {
	f.user = user
	f.user.Role = storage.UserRoleOwner
	return f.user, nil
}
func (f *fakeRepository) ListUsers(context.Context, int, int) ([]storage.User, error) {
	if f.user == nil {
		return nil, nil
	}
	return []storage.User{*f.user}, nil
}
func (f *fakeRepository) UpdateUserRole(_ context.Context, _, _ uuid.UUID, role string) (*storage.User, error) {
	if f.user == nil {
		f.user = &storage.User{ID: uuid.New(), Status: storage.UserStatusActive}
	}
	f.user.Role = role
	return f.user, nil
}
func (f *fakeRepository) UpdateUserStatus(_ context.Context, _, _ uuid.UUID, status string) (*storage.User, error) {
	if f.user == nil {
		f.user = &storage.User{ID: uuid.New(), Role: storage.UserRoleUser}
	}
	f.user.Status = status
	return f.user, nil
}
func (f *fakeRepository) ListAdminAuditLogs(context.Context, int) ([]storage.AdminAuditLog, error) {
	return nil, nil
}

// newFakeLinuxDOProvider 启动模拟的 LinuxDo token 与用户信息接口，calls 统计被调用次数。
func newFakeLinuxDOProvider(t *testing.T) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		switch r.URL.Path {
		case "/token":
			if err := r.ParseForm(); err != nil || r.PostForm.Get("code") != "auth-code" || r.PostForm.Get("code_verifier") == "" {
				http.Error(w, "bad token request", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"access-token"}`))
		case "/user":
			if r.Header.Get("Authorization") != "Bearer access-token" {
				http.Error(w, "missing bearer", http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"sub":"linuxdo-42","username":"fate","name":"Fate User","email":"fate@example.com","active":true,"trust_level":2}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(provider.Close)
	return provider, &calls
}

func linuxDOTestConfig(provider *httptest.Server) Config {
	cfg := Config{LinuxDOClientID: "client-id", LinuxDOClientSecret: "secret", LinuxDORedirectURI: "http://localhost/callback"}
	if provider != nil {
		cfg.TokenURL = provider.URL + "/token"
		cfg.UserInfoURL = provider.URL + "/user"
	}
	return cfg
}

func stateOf(t *testing.T, start *LinuxDOStart) string {
	t.Helper()
	return mustParseURL(t, start.RedirectURL).Query().Get("state")
}

func TestStartLinuxDOUsesPKCEAndPersistsFlow(t *testing.T) {
	repo := &fakeRepository{}
	svc := NewService(repo, linuxDOTestConfig(nil))
	start, err := svc.StartLinuxDO(context.Background(), "/conversations/abc")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(start.RedirectURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	for _, key := range []string{"code_challenge", "code_challenge_method", "state", "client_id", "redirect_uri"} {
		if q.Get(key) == "" {
			t.Fatalf("missing OAuth query parameter %q", key)
		}
	}
	if q.Get("code_challenge_method") != "S256" || repo.flow == nil {
		t.Fatalf("PKCE flow was not persisted correctly")
	}
	if repo.flow.ReturnTo != "/conversations/abc" || repo.flow.Purpose != storage.OAuthPurposeLogin {
		t.Fatalf("unexpected flow: return_to=%q purpose=%q", repo.flow.ReturnTo, repo.flow.Purpose)
	}
	// 浏览器绑定 nonce 只以摘要形式落库，原值交给调用方写入 Cookie。
	if start.BrowserBinding == "" || repo.flow.BrowserBindingHash != sha256Hex(start.BrowserBinding) {
		t.Fatalf("browser binding was not persisted as a hash: %+v", repo.flow)
	}
	if ttl := time.Until(repo.flow.ExpiresAt); ttl <= 9*time.Minute || ttl > LinuxDOFlowTTL {
		t.Fatalf("unexpected flow TTL: %v", ttl)
	}
}

// 管理员关闭 LinuxDo 登录开关后，发起与完成登录流程都被拒绝；绑定流程不受影响。
func TestLinuxDOLoginDisabledToggle(t *testing.T) {
	repo := &fakeRepository{settings: &storage.AuthSettings{LinuxDOLoginEnabled: false}}
	svc := NewService(repo, linuxDOTestConfig(nil))
	if _, err := svc.StartLinuxDO(context.Background(), "/"); !errors.Is(err, ErrLinuxDOLoginDisabled) {
		t.Fatalf("StartLinuxDO error = %v, want ErrLinuxDOLoginDisabled", err)
	}

	// 开关开着时发起，关闭后再回调：登录用途拒绝完成。
	repo.settings.LinuxDOLoginEnabled = true
	provider, _ := newFakeLinuxDOProvider(t)
	svc = NewService(repo, linuxDOTestConfig(provider))
	start, err := svc.StartLinuxDO(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	repo.settings.LinuxDOLoginEnabled = false
	_, err = svc.CompleteLinuxDO(context.Background(), LinuxDOCallback{
		Code: "auth-code", State: stateOf(t, start), BrowserBinding: start.BrowserBinding,
	})
	var flowErr *LinuxDOFlowError
	if !errors.As(err, &flowErr) || !errors.Is(err, ErrLinuxDOLoginDisabled) {
		t.Fatalf("CompleteLinuxDO error = %v, want LinuxDOFlowError wrapping ErrLinuxDOLoginDisabled", err)
	}

	// 绑定用途不受开关影响。
	binder := uuid.New()
	bindStart, err := svc.StartLinuxDOBind(context.Background(), binder, "/settings")
	if err != nil {
		t.Fatalf("StartLinuxDOBind with login disabled: %v", err)
	}
	if _, err := svc.CompleteLinuxDO(context.Background(), LinuxDOCallback{
		Code: "auth-code", State: stateOf(t, bindStart), BrowserBinding: bindStart.BrowserBinding,
		SessionUserID: &binder,
	}); err != nil {
		t.Fatalf("bind with login disabled: %v", err)
	}
}

func TestCompleteLinuxDOExchangesCodeAndCreatesSessionForBoundIdentity(t *testing.T) {
	email := "fate@example.com"
	repo := &fakeRepository{user: &storage.User{ID: uuid.New(), DisplayName: "Fate User", Email: &email, Status: storage.UserStatusActive}}
	provider, _ := newFakeLinuxDOProvider(t)
	svc := NewService(repo, linuxDOTestConfig(provider))
	start, err := svc.StartLinuxDO(context.Background(), "/")
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.CompleteLinuxDO(context.Background(), LinuxDOCallback{
		Code: "auth-code", State: stateOf(t, start), BrowserBinding: start.BrowserBinding,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Purpose != storage.OAuthPurposeLogin || result.ReturnTo != "/" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.User.DisplayName != "Fate User" || result.User.Email == nil || *result.User.Email != "fate@example.com" {
		t.Fatalf("unexpected user: %+v", result.User)
	}
	if result.SessionToken == "" || repo.session == nil || repo.session.UserID != result.User.ID {
		t.Fatalf("session was not created")
	}
	if repo.flow != nil {
		t.Fatal("OAuth flow was not consumed")
	}
}

// 登录 CSRF：授权回调链接被转交到没有对应 nonce Cookie 的浏览器时必须拒绝，且不交换 code。
func TestCompleteLinuxDORejectsMissingOrForeignBrowserBinding(t *testing.T) {
	email := "fate@example.com"
	for _, test := range []struct {
		name    string
		binding func(start *LinuxDOStart) string
	}{
		{name: "missing", binding: func(*LinuxDOStart) string { return "" }},
		{name: "foreign", binding: func(*LinuxDOStart) string { return "attacker-or-other-browser-nonce" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &fakeRepository{user: &storage.User{ID: uuid.New(), Email: &email, Status: storage.UserStatusActive}}
			provider, calls := newFakeLinuxDOProvider(t)
			svc := NewService(repo, linuxDOTestConfig(provider))
			start, err := svc.StartLinuxDO(context.Background(), "/")
			if err != nil {
				t.Fatal(err)
			}
			result, err := svc.CompleteLinuxDO(context.Background(), LinuxDOCallback{
				Code: "auth-code", State: stateOf(t, start), BrowserBinding: test.binding(start),
			})
			if !errors.Is(err, ErrInvalidOAuthFlow) || result != nil {
				t.Fatalf("expected ErrInvalidOAuthFlow, got result=%+v err=%v", result, err)
			}
			var flowErr *LinuxDOFlowError
			if errors.As(err, &flowErr) {
				t.Fatal("unbound browser must not learn the flow return_to")
			}
			if atomic.LoadInt32(calls) != 0 || repo.session != nil {
				t.Fatalf("code must not be exchanged: calls=%d session=%v", atomic.LoadInt32(calls), repo.session)
			}
		})
	}
}

// 绑定 CSRF：即使浏览器 nonce 匹配，回调时的当前会话也必须是发起绑定的用户。
func TestCompleteLinuxDOBindRequiresInitiatingSession(t *testing.T) {
	owner := uuid.New()
	other := uuid.New()
	for _, test := range []struct {
		name    string
		session *uuid.UUID
	}{
		{name: "no_session", session: nil},
		{name: "other_user", session: &other},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &fakeRepository{}
			provider, calls := newFakeLinuxDOProvider(t)
			svc := NewService(repo, linuxDOTestConfig(provider))
			start, err := svc.StartLinuxDOBind(context.Background(), owner, "/settings")
			if err != nil {
				t.Fatal(err)
			}
			_, err = svc.CompleteLinuxDO(context.Background(), LinuxDOCallback{
				Code: "auth-code", State: stateOf(t, start), BrowserBinding: start.BrowserBinding, SessionUserID: test.session,
			})
			var flowErr *LinuxDOFlowError
			if !errors.As(err, &flowErr) || !errors.Is(err, ErrLinuxDOBindSessionMismatch) {
				t.Fatalf("expected bind session mismatch flow error, got %v", err)
			}
			if flowErr.Purpose != storage.OAuthPurposeBind || flowErr.ReturnTo != "/settings" {
				t.Fatalf("flow error must carry purpose and return_to: %+v", flowErr)
			}
			if atomic.LoadInt32(calls) != 0 || repo.boundUserID != nil {
				t.Fatalf("bind must stop before exchanging code: calls=%d bound=%v", atomic.LoadInt32(calls), repo.boundUserID)
			}
		})
	}
}

func TestCompleteLinuxDOBindSucceedsForInitiatingSession(t *testing.T) {
	owner := uuid.New()
	repo := &fakeRepository{}
	provider, _ := newFakeLinuxDOProvider(t)
	svc := NewService(repo, linuxDOTestConfig(provider))
	start, err := svc.StartLinuxDOBind(context.Background(), owner, "/settings")
	if err != nil {
		t.Fatal(err)
	}
	if repo.flow.Purpose != storage.OAuthPurposeBind || repo.flow.UserID == nil || *repo.flow.UserID != owner {
		t.Fatalf("bind flow was not persisted correctly: %+v", repo.flow)
	}
	session := owner
	result, err := svc.CompleteLinuxDO(context.Background(), LinuxDOCallback{
		Code: "auth-code", State: stateOf(t, start), BrowserBinding: start.BrowserBinding, SessionUserID: &session,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Purpose != storage.OAuthPurposeBind || result.ReturnTo != "/settings" || result.SessionToken != "" {
		t.Fatalf("unexpected bind result: %+v", result)
	}
	if repo.boundUserID == nil || *repo.boundUserID != owner {
		t.Fatalf("identity bound to %v, want %v", repo.boundUserID, owner)
	}
}

func TestCompleteLinuxDOFailuresCarryFlowPurpose(t *testing.T) {
	owner := uuid.New()
	t.Run("bind_identity_owned_by_other_user", func(t *testing.T) {
		repo := &fakeRepository{bindErr: storage.ErrIdentityAlreadyBound}
		provider, _ := newFakeLinuxDOProvider(t)
		svc := NewService(repo, linuxDOTestConfig(provider))
		start, err := svc.StartLinuxDOBind(context.Background(), owner, "/settings?tab=identity")
		if err != nil {
			t.Fatal(err)
		}
		_, err = svc.CompleteLinuxDO(context.Background(), LinuxDOCallback{
			Code: "auth-code", State: stateOf(t, start), BrowserBinding: start.BrowserBinding, SessionUserID: &owner,
		})
		var flowErr *LinuxDOFlowError
		if !errors.As(err, &flowErr) || !errors.Is(err, storage.ErrIdentityAlreadyBound) {
			t.Fatalf("expected already-bound flow error, got %v", err)
		}
		if flowErr.Purpose != storage.OAuthPurposeBind || flowErr.ReturnTo != "/settings?tab=identity" {
			t.Fatalf("unexpected flow error: %+v", flowErr)
		}
	})
	t.Run("login_unbound_identity", func(t *testing.T) {
		repo := &fakeRepository{}
		provider, _ := newFakeLinuxDOProvider(t)
		svc := NewService(repo, linuxDOTestConfig(provider))
		start, err := svc.StartLinuxDO(context.Background(), "/")
		if err != nil {
			t.Fatal(err)
		}
		_, err = svc.CompleteLinuxDO(context.Background(), LinuxDOCallback{
			Code: "auth-code", State: stateOf(t, start), BrowserBinding: start.BrowserBinding,
		})
		var flowErr *LinuxDOFlowError
		if !errors.As(err, &flowErr) || !errors.Is(err, ErrLinuxDOUnbound) || flowErr.Purpose != storage.OAuthPurposeLogin {
			t.Fatalf("expected unbound login flow error, got %v", err)
		}
		if repo.session != nil {
			t.Fatal("unbound identity must not create a session")
		}
	})
}

func TestAbortLinuxDOConsumesMatchingFlow(t *testing.T) {
	owner := uuid.New()
	repo := &fakeRepository{}
	svc := NewService(repo, linuxDOTestConfig(nil))
	start, err := svc.StartLinuxDOBind(context.Background(), owner, "/settings")
	if err != nil {
		t.Fatal(err)
	}
	err = svc.AbortLinuxDO(context.Background(), stateOf(t, start), start.BrowserBinding, ErrLinuxDOCancelled)
	var flowErr *LinuxDOFlowError
	if !errors.As(err, &flowErr) || !errors.Is(err, ErrLinuxDOCancelled) {
		t.Fatalf("expected cancelled flow error, got %v", err)
	}
	if flowErr.Purpose != storage.OAuthPurposeBind || flowErr.ReturnTo != "/settings" || repo.flow != nil {
		t.Fatalf("flow was not consumed into a bind error: %+v flow=%v", flowErr, repo.flow)
	}

	// 没有可用 state 时原样返回原因，由调用方回落到登录页。
	err = svc.AbortLinuxDO(context.Background(), "unknown-state", start.BrowserBinding, ErrLinuxDOCancelled)
	if !errors.Is(err, ErrLinuxDOCancelled) || errors.As(err, &flowErr) {
		t.Fatalf("unknown state must fall back to the plain cause, got %v", err)
	}
}

func TestSafeReturnTo(t *testing.T) {
	for _, test := range []struct {
		raw  string
		want string
	}{
		{raw: "/settings", want: "/settings"},
		{raw: "/ok?x=1", want: "/ok?x=1"},
		{raw: "/conversations/abc#latest", want: "/conversations/abc#latest"},
		{raw: `/\evil.example`, want: "/"},
		{raw: "/\t/evil.example", want: "/"},
		{raw: "//evil.example", want: "/"},
		{raw: "///evil.example", want: "/"},
		{raw: "https://evil.example", want: "/"},
		{raw: "javascript:alert(1)", want: "/"},
		{raw: "evil.example", want: "/"},
		{raw: "", want: "/"},
		{raw: " /settings", want: "/"},
		{raw: "/settings\r\nSet-Cookie: x=1", want: "/"},
		{raw: "/%5Cevil.example", want: "/"},
		{raw: "/%2F/evil.example", want: "/"},
		{raw: "/" + strings.Repeat("a", returnToMaxBytes), want: "/"},
	} {
		if got := safeReturnTo(test.raw); got != test.want {
			t.Errorf("safeReturnTo(%q) = %q, want %q", test.raw, got, test.want)
		}
	}
}

func TestNormalizeUsername(t *testing.T) {
	for _, test := range []struct {
		raw     string
		want    string
		wantErr bool
	}{
		{raw: "alice", want: "alice"},
		{raw: "  Alice_01  ", want: "Alice_01"},
		{raw: "张三", want: "张三"},
		{raw: strings.Repeat("甲", 32), want: strings.Repeat("甲", 32)},
		{raw: "a", wantErr: true},
		{raw: "   ", wantErr: true},
		{raw: strings.Repeat("a", 33), wantErr: true},
		{raw: "alice@example.com", wantErr: true},
		{raw: "alice smith", wantErr: true},
		{raw: "alice\tsmith", wantErr: true},
		{raw: "alice" + string(rune(0x3000)) + "smith", wantErr: true}, // 全角空格
		{raw: "alice\u0000", wantErr: true},
		{raw: "ali" + string(rune(0x200b)) + "ce", wantErr: true}, // 零宽空格（格式字符）
	} {
		got, err := normalizeUsername(test.raw)
		if test.wantErr {
			if !errors.Is(err, ErrInvalidUsername) {
				t.Errorf("normalizeUsername(%q) err = %v, want ErrInvalidUsername", test.raw, err)
			}
			continue
		}
		if err != nil || got != test.want {
			t.Errorf("normalizeUsername(%q) = %q, %v; want %q", test.raw, got, err, test.want)
		}
	}
}

func TestEmailAuthDisabledByDefault(t *testing.T) {
	svc := NewService(&fakeRepository{}, Config{})
	if _, err := svc.RegisterEmail(context.Background(), "user@example.com", "user", "password123", "User", "", ""); err != ErrEmailRegistrationDisabled {
		t.Fatalf("expected disabled error, got %v", err)
	}
	config, err := svc.PublicConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if config.EmailRegistrationEnabled {
		t.Fatal("email registration must be disabled by default")
	}
}

// 用户名与昵称分开保存：同一邮箱前缀或同名昵称不会互相冲突。
func TestRegisterEmailUsesExplicitUsername(t *testing.T) {
	newRepo := func() *fakeRepository {
		repo := &fakeRepository{settings: storage.DefaultAuthSettings()}
		repo.settings.EmailRegistrationEnabled = true
		repo.settings.EmailVerificationEnabled = false
		return repo
	}

	repo := newRepo()
	svc := NewService(repo, Config{})
	result, err := svc.RegisterEmail(context.Background(), "Alice@QQ.com", " alice_qq ", "password123", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if result.User.Username != "alice_qq" || result.User.DisplayName != "alice_qq" || *result.User.Email != "alice@qq.com" {
		t.Fatalf("display name must default to the username: %+v", result.User)
	}

	repo = newRepo()
	svc = NewService(repo, Config{})
	result, err = svc.RegisterEmail(context.Background(), "zhang@example.com", "zhang01", "password123", " 张三 ", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if result.User.Username != "zhang01" || result.User.DisplayName != "张三" {
		t.Fatalf("username and display name must be stored separately: %+v", result.User)
	}

	for _, username := range []string{"", "a", "alice@example.com", "alice smith"} {
		if _, err := NewService(newRepo(), Config{}).RegisterEmail(context.Background(), "a@example.com", username, "password123", "Alice", "", ""); !errors.Is(err, ErrInvalidUsername) {
			t.Fatalf("username %q: expected ErrInvalidUsername, got %v", username, err)
		}
	}

	repo = newRepo()
	repo.registerErr = storage.ErrUsernameInUse
	if _, err := NewService(repo, Config{}).RegisterEmail(context.Background(), "b@example.com", "taken", "password123", "", "", ""); !errors.Is(err, storage.ErrUsernameInUse) {
		t.Fatalf("expected username conflict, got %v", err)
	}
	repo = newRepo()
	repo.registerErr = storage.ErrEmailInUse
	if _, err := NewService(repo, Config{}).RegisterEmail(context.Background(), "b@example.com", "fresh", "password123", "", "", ""); !errors.Is(err, ErrEmailInUse) {
		t.Fatalf("expected email conflict, got %v", err)
	}
}

// 含 @ 的登录标识只按规范化邮箱查找，其余按用户名查找。
func TestLoginEmailRoutesIdentifierByAtSign(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	hashString := string(hash)
	email := "alice@example.com"
	repo := &fakeRepository{user: &storage.User{
		ID: uuid.New(), Username: "Alice", Email: &email, PasswordHash: &hashString, Status: storage.UserStatusActive,
	}}
	svc := NewService(repo, Config{})

	if _, err := svc.LoginEmail(context.Background(), "  ALICE@Example.com ", "password123"); err != nil {
		t.Fatalf("email login failed: %v", err)
	}
	if repo.lastIdentifier != "alice@example.com" {
		t.Fatalf("email identifier was not normalized: %q", repo.lastIdentifier)
	}
	if _, err := svc.LoginEmail(context.Background(), " alice ", "password123"); err != nil {
		t.Fatalf("username login failed: %v", err)
	}
	if repo.lastIdentifier != "alice" {
		t.Fatalf("username identifier = %q", repo.lastIdentifier)
	}
	repo.lastIdentifier = ""
	if _, err := svc.LoginEmail(context.Background(), "alice@", "password123"); !errors.Is(err, ErrInvalidCredentials) || repo.lastIdentifier != "" {
		t.Fatalf("malformed email must fail before lookup: err=%v identifier=%q", err, repo.lastIdentifier)
	}
	if _, err := svc.LoginEmail(context.Background(), "alice", "wrong-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected invalid credentials, got %v", err)
	}
}

func TestInitializeOwnerUsesUsernameRules(t *testing.T) {
	if _, err := NewService(&fakeRepository{}, Config{}).InitializeOwner(context.Background(), "", "owner@example.com", "password123", ""); !errors.Is(err, ErrInvalidUsername) {
		t.Fatalf("expected ErrInvalidUsername, got %v", err)
	}
	repo := &fakeRepository{}
	result, err := NewService(repo, Config{}).InitializeOwner(context.Background(), "", " 掌柜 ", "password123", "")
	if err != nil {
		t.Fatal(err)
	}
	if result.User.Username != "掌柜" || result.User.DisplayName != "掌柜" || result.User.Role != storage.UserRoleOwner || result.User.Email != nil {
		t.Fatalf("unexpected owner: %+v", result.User)
	}
}

type fakeMailer struct {
	message MailMessage
}

func (m *fakeMailer) Send(_ context.Context, _ SMTPConfig, message MailMessage) error {
	m.message = message
	return nil
}

func TestRegistrationCodeAndInvitationAreRequiredByRuntimeSettings(t *testing.T) {
	repo := &fakeRepository{settings: storage.DefaultAuthSettings()}
	repo.settings.EmailRegistrationEnabled = true
	repo.settings.EmailVerificationEnabled = true
	repo.settings.InvitationRequired = true
	repo.settings.SMTPHost = "smtp.example.com"
	repo.settings.SenderEmail = "noreply@example.com"
	svc := NewService(repo, Config{DataEncryptionKey: strings.Repeat("k", 32)})
	mailer := &fakeMailer{}
	svc.mailer = mailer

	if err := svc.SendRegistrationCode(context.Background(), "user@example.com"); err != nil {
		t.Fatal(err)
	}
	code := regexp.MustCompile(`\d{6}`).FindString(mailer.message.Body)
	if code == "" {
		t.Fatalf("verification code missing from body: %q", mailer.message.Body)
	}
	result, err := svc.RegisterEmail(
		context.Background(), "user@example.com", "user01", "password123", "User", code, "FC-ABCD-EFGH-IJKL-MNOP",
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.User == nil || repo.verificationID == nil || repo.invitationHash == nil {
		t.Fatal("registration proofs were not passed to the atomic repository operation")
	}
	if *repo.invitationHash != sha256Hex(normalizeInvitationCode("FC-ABCD-EFGH-IJKL-MNOP")) {
		t.Fatal("invitation code was not hashed before storage lookup")
	}
}

func TestInvalidVerificationCodeIncrementsAttemptCount(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("123456"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	repo := &fakeRepository{
		settings: &storage.AuthSettings{EmailRegistrationEnabled: true, EmailVerificationEnabled: true},
		verification: &storage.EmailVerificationCode{
			ID: uuid.New(), Email: "user@example.com", Purpose: registrationPurpose,
			CodeHash: string(hash), ExpiresAt: time.Now().Add(time.Minute), CreatedAt: time.Now(),
		},
	}
	svc := NewService(repo, Config{})
	_, err = svc.RegisterEmail(context.Background(), "user@example.com", "user01", "password123", "User", "000000", "")
	if !errors.Is(err, ErrInvalidVerificationCode) {
		t.Fatalf("expected invalid verification error, got %v", err)
	}
	if repo.verification.AttemptCount != 1 {
		t.Fatalf("attempt count = %d, want 1", repo.verification.AttemptCount)
	}
}

func validSettingsInput(emailAuthEnabled bool) UpdateAuthSettingsInput {
	return UpdateAuthSettingsInput{
		EmailAuthEnabled: emailAuthEnabled, LinuxDOLoginEnabled: true, SMTPHost: "smtp.example.com", SMTPPort: 587, SMTPSecurity: "starttls",
		SenderEmail: "noreply@example.com", SenderName: "Fatecraft", VerificationSubject: "验证码",
		VerificationTemplate: "验证码：{{code}}", VerificationCodeTTLMinutes: 10, VerificationResendSeconds: 60,
	}
}

func TestSMTPPasswordIsEncryptedAndNeverReturned(t *testing.T) {
	repo := &fakeRepository{settings: storage.DefaultAuthSettings()}
	svc := NewService(repo, Config{DataEncryptionKey: strings.Repeat("secret", 6)})
	input := validSettingsInput(true)
	input.SMTPUsername = "mailer"
	input.SMTPPassword = "plain-password"
	view, err := svc.UpdateAdminSettings(context.Background(), uuid.New(), input)
	if err != nil {
		t.Fatal(err)
	}
	if !view.HasSMTPPassword || repo.settings.SMTPPasswordEncrypted == "" || strings.Contains(repo.settings.SMTPPasswordEncrypted, "plain-password") {
		t.Fatal("SMTP password was not stored as an opaque ciphertext")
	}
	decrypted, err := svc.decryptSMTPPassword(repo.settings)
	if err != nil || decrypted != "plain-password" {
		t.Fatalf("decrypt password: value=%q err=%v", decrypted, err)
	}
}

// 关闭密码登录前（且 LinuxDo 登录保持开启时），owner 必须已绑定可用的 LinuxDo 身份，
// 否则会把 owner 锁在站外。双关全灭是允许的，不在这里控制。
func TestUpdateAdminSettingsPreventsOwnerLockout(t *testing.T) {
	owner := uuid.New()
	linuxDOIdentity := []storage.ExternalIdentitySummary{{ID: uuid.New(), Provider: linuxDOProvider, ProviderUserID: "linuxdo-42"}}
	for _, test := range []struct {
		name       string
		cfg        Config
		identities []storage.ExternalIdentitySummary
		wantErr    bool
	}{
		{name: "linuxdo_not_configured", cfg: Config{}, identities: linuxDOIdentity, wantErr: true},
		{name: "owner_without_linuxdo", cfg: linuxDOTestConfig(nil), wantErr: true},
		{name: "owner_with_other_provider_only", cfg: linuxDOTestConfig(nil), identities: []storage.ExternalIdentitySummary{{ID: uuid.New(), Provider: "github"}}, wantErr: true},
		{name: "owner_with_linuxdo", cfg: linuxDOTestConfig(nil), identities: linuxDOIdentity},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &fakeRepository{settings: storage.DefaultAuthSettings(), identities: test.identities}
			svc := NewService(repo, test.cfg)
			_, err := svc.UpdateAdminSettings(context.Background(), owner, validSettingsInput(false))
			if test.wantErr {
				if !errors.Is(err, ErrInvalidAuthSettings) {
					t.Fatalf("expected ErrInvalidAuthSettings, got %v", err)
				}
				if !repo.settings.EmailAuthEnabled {
					t.Fatal("rejected update must not disable password login")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if repo.settings.EmailAuthEnabled {
				t.Fatal("password login should be disabled")
			}
		})
	}
}

// 关 LinuxDo 登录时密码登录开着：合法，直接保存；双关全灭是允许的明确选择，不做控制。
func TestUpdateAdminSettingsLinuxDOToggle(t *testing.T) {
	repo := &fakeRepository{settings: storage.DefaultAuthSettings()}
	svc := NewService(repo, linuxDOTestConfig(nil))
	input := validSettingsInput(true)
	input.LinuxDOLoginEnabled = false
	view, err := svc.UpdateAdminSettings(context.Background(), uuid.New(), input)
	if err != nil {
		t.Fatal(err)
	}
	if view.LinuxDOLoginEnabled || !view.EmailAuthEnabled {
		t.Fatalf("unexpected view: %+v", view)
	}
	if repo.settings.LinuxDOLoginEnabled {
		t.Fatal("linuxdo login should be disabled")
	}

	// 双关全灭：允许保存（之后无人能再登录），不检查 owner 绑定。
	input = validSettingsInput(false)
	input.LinuxDOLoginEnabled = false
	if _, err := svc.UpdateAdminSettings(context.Background(), uuid.New(), input); err != nil {
		t.Fatalf("disabling both logins error = %v, want nil", err)
	}
	if repo.settings.EmailAuthEnabled || repo.settings.LinuxDOLoginEnabled {
		t.Fatalf("both logins should be disabled: %+v", repo.settings)
	}
}

// 全站关闭密码登录时，解绑必须把该策略交给存储层，禁止移除最后一个第三方身份。
func TestUnbindExternalIdentityPassesPasswordLoginPolicy(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		repo := &fakeRepository{settings: storage.DefaultAuthSettings()}
		repo.settings.EmailAuthEnabled = enabled
		if err := NewService(repo, Config{}).UnbindExternalIdentity(context.Background(), uuid.New(), uuid.New()); err != nil {
			t.Fatal(err)
		}
		if repo.unbindPasswordLoginEnabled == nil || *repo.unbindPasswordLoginEnabled != enabled {
			t.Fatalf("password login policy = %v, want %v", repo.unbindPasswordLoginEnabled, enabled)
		}
	}
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil || strings.TrimSpace(u.Query().Get("state")) == "" {
		t.Fatalf("invalid OAuth URL: %v", err)
	}
	return u
}
