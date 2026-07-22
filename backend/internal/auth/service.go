// Package auth implements the server-side account and OAuth session flows.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"

	"github.com/ialtone/fatecraft/internal/storage"
)

const linuxDOProvider = "linuxdo"

var (
	ErrUnauthenticated           = errors.New("未登录")
	ErrEmailAuthDisabled         = errors.New("邮箱登录未启用")
	ErrEmailRegistrationDisabled = errors.New("邮箱注册未启用")
	ErrEmailVerificationDisabled = errors.New("邮箱验证码未启用")
	ErrInvalidVerificationCode   = errors.New("邮箱验证码无效或已失效")
	ErrInvalidInvitationCode     = errors.New("邀请码无效或已失效")
	ErrEmailSendFailed           = errors.New("验证码邮件发送失败，请稍后重试")
	ErrEncryptionNotConfigured   = errors.New("服务端尚未配置 AUTH_DATA_ENCRYPTION_KEY")
	ErrVerificationCooldown      = errors.New("验证码发送过于频繁")
	ErrInvalidAuthSettings       = errors.New("认证设置无效")
	ErrLinuxDONotConfigured      = errors.New("LinuxDo 登录尚未配置")
	ErrInvalidCredentials        = errors.New("邮箱或密码错误")
	ErrEmailInUse                = errors.New("邮箱已被注册")
	ErrInvalidOAuthFlow          = errors.New("OAuth 登录流程已失效，请重试")
	ErrProviderRejected          = errors.New("LinuxDo 账号不可用")
)

type Repository interface {
	CreateOAuthFlow(context.Context, *storage.OAuthLoginFlow) error
	ConsumeOAuthFlow(context.Context, string, string, time.Time) (*storage.OAuthLoginFlow, error)
	CreateSession(context.Context, *storage.AuthSession) error
	DeleteSession(context.Context, string) error
	UserBySession(context.Context, string, time.Time) (*storage.User, error)
	UserByEmail(context.Context, string) (*storage.User, error)
	RegisterEmailUser(context.Context, *storage.User, *uuid.UUID, *string, time.Time) error
	UpsertExternalUser(context.Context, storage.ExternalIdentityInput) (*storage.User, error)
	GetAuthSettings(context.Context) (*storage.AuthSettings, error)
	SaveAuthSettings(context.Context, *storage.AuthSettings) error
	LatestEmailVerification(context.Context, string, string) (*storage.EmailVerificationCode, error)
	CreateEmailVerification(context.Context, *storage.EmailVerificationCode) error
	DeleteEmailVerification(context.Context, uuid.UUID) error
	IncrementEmailVerificationAttempts(context.Context, uuid.UUID) error
	ListInvitationCodes(context.Context) ([]storage.InvitationCode, error)
	InvitationCodeByID(context.Context, uuid.UUID) (*storage.InvitationCode, error)
	CreateInvitationCode(context.Context, *storage.InvitationCode) error
	UpdateInvitationCode(context.Context, *storage.InvitationCode) error
}

type Config struct {
	LinuxDOClientID     string
	LinuxDOClientSecret string
	LinuxDORedirectURI  string
	DataEncryptionKey   string
	SessionTTL          time.Duration
	AuthorizationURL    string
	TokenURL            string
	UserInfoURL         string
}

func (c Config) LinuxDOEnabled() bool {
	return c.LinuxDOClientID != "" && c.LinuxDOClientSecret != "" && c.LinuxDORedirectURI != ""
}

type Service struct {
	repo       Repository
	cfg        Config
	httpClient *http.Client
	cipher     *credentialCipher
	cipherErr  error
	mailer     Mailer
}

func NewService(repo Repository, cfg Config) *Service {
	if cfg.SessionTTL <= 0 {
		cfg.SessionTTL = 14 * 24 * time.Hour
	}
	if cfg.AuthorizationURL == "" {
		cfg.AuthorizationURL = "https://connect.linux.do/oauth2/authorize"
	}
	if cfg.TokenURL == "" {
		cfg.TokenURL = "https://connect.linux.do/oauth2/token"
	}
	if cfg.UserInfoURL == "" {
		cfg.UserInfoURL = "https://connect.linux.do/api/user"
	}
	cipher, cipherErr := newCredentialCipher(cfg.DataEncryptionKey)
	return &Service{
		repo: repo, cfg: cfg, httpClient: &http.Client{Timeout: 15 * time.Second},
		cipher: cipher, cipherErr: cipherErr, mailer: smtpMailer{},
	}
}

type PublicConfig struct {
	EmailAuthEnabled          bool `json:"email_auth_enabled"`
	EmailRegistrationEnabled  bool `json:"email_registration_enabled"`
	EmailVerificationEnabled  bool `json:"email_verification_enabled"`
	InvitationRequired        bool `json:"invitation_required"`
	VerificationResendSeconds int  `json:"verification_resend_seconds"`
	LinuxDOEnabled            bool `json:"linuxdo_enabled"`
}

func (s *Service) PublicConfig(ctx context.Context) (PublicConfig, error) {
	settings, err := s.repo.GetAuthSettings(ctx)
	if err != nil {
		return PublicConfig{}, err
	}
	return PublicConfig{
		EmailAuthEnabled: settings.EmailAuthEnabled, EmailRegistrationEnabled: settings.EmailRegistrationEnabled,
		EmailVerificationEnabled: settings.EmailVerificationEnabled, InvitationRequired: settings.InvitationRequired,
		VerificationResendSeconds: settings.VerificationResendSeconds,
		LinuxDOEnabled:            s.cfg.LinuxDOEnabled(),
	}, nil
}

func (s *Service) StartLinuxDO(ctx context.Context, returnTo string) (string, error) {
	if !s.cfg.LinuxDOEnabled() {
		return "", ErrLinuxDONotConfigured
	}
	state, err := randomToken(32)
	if err != nil {
		return "", fmt.Errorf("生成 OAuth state 失败: %w", err)
	}
	verifier, err := randomToken(48)
	if err != nil {
		return "", fmt.Errorf("生成 PKCE verifier 失败: %w", err)
	}
	flow := &storage.OAuthLoginFlow{
		StateHash: sha256Hex(state), Provider: linuxDOProvider, CodeVerifier: verifier,
		ReturnTo: safeReturnTo(returnTo), ExpiresAt: time.Now().Add(10 * time.Minute), CreatedAt: time.Now(),
	}
	if err := s.repo.CreateOAuthFlow(ctx, flow); err != nil {
		return "", err
	}
	challenge := sha256.Sum256([]byte(verifier))
	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {s.cfg.LinuxDOClientID},
		"redirect_uri":          {s.cfg.LinuxDORedirectURI},
		"scope":                 {"openid profile email"},
		"state":                 {state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(challenge[:])},
		"code_challenge_method": {"S256"},
	}
	return s.cfg.AuthorizationURL + "?" + params.Encode(), nil
}

type LoginResult struct {
	User         *storage.User
	SessionToken string
	ReturnTo     string
}

func (s *Service) CompleteLinuxDO(ctx context.Context, code, state string) (*LoginResult, error) {
	if !s.cfg.LinuxDOEnabled() || code == "" || state == "" {
		return nil, ErrInvalidOAuthFlow
	}
	flow, err := s.repo.ConsumeOAuthFlow(ctx, linuxDOProvider, sha256Hex(state), time.Now())
	if err != nil || flow == nil {
		return nil, ErrInvalidOAuthFlow
	}
	accessToken, err := s.exchangeCode(ctx, code, flow.CodeVerifier)
	if err != nil {
		return nil, err
	}
	profile, err := s.fetchLinuxDOProfile(ctx, accessToken)
	if err != nil {
		return nil, err
	}
	if profile.Active != nil && !*profile.Active {
		return nil, ErrProviderRejected
	}
	if profile.Sub == "" {
		return nil, errors.New("LinuxDo 用户信息缺少 sub")
	}
	user, err := s.repo.UpsertExternalUser(ctx, storage.ExternalIdentityInput{
		Provider: linuxDOProvider, ProviderUserID: profile.Sub, Username: firstNonEmpty(profile.Username, profile.Login),
		Email: normalizeOptionalEmail(profile.Email), DisplayName: firstNonEmpty(profile.Name, profile.Username, profile.Login, "LinuxDo 用户"),
		AvatarURL: normalizeOptionalString(profile.AvatarURL), TrustLevel: profile.TrustLevel, Profile: profile.Raw,
	})
	if err != nil {
		return nil, err
	}
	token, err := s.createSession(ctx, user)
	if err != nil {
		return nil, err
	}
	return &LoginResult{User: user, SessionToken: token, ReturnTo: flow.ReturnTo}, nil
}

func (s *Service) RegisterEmail(ctx context.Context, email, password, displayName, verificationCode, invitationCode string) (*LoginResult, error) {
	settings, err := s.repo.GetAuthSettings(ctx)
	if err != nil {
		return nil, err
	}
	if !settings.EmailRegistrationEnabled {
		return nil, ErrEmailRegistrationDisabled
	}
	email, err = normalizeEmail(email)
	if err != nil {
		return nil, err
	}
	if len([]byte(password)) < 8 || len([]byte(password)) > 72 {
		return nil, errors.New("密码长度必须为 8-72 字节")
	}
	if strings.TrimSpace(displayName) == "" {
		displayName = strings.Split(email, "@")[0]
	}
	displayName = strings.TrimSpace(displayName)
	if utf8.RuneCountInString(displayName) > 128 {
		return nil, errors.New("昵称不能超过 128 个字符")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	hashString := string(hash)
	user := &storage.User{ID: uuid.New(), Email: &email, PasswordHash: &hashString, DisplayName: displayName, Status: storage.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	var verificationID *uuid.UUID
	if settings.EmailVerificationEnabled {
		verification, err := s.verifyRegistrationCode(ctx, email, verificationCode, time.Now())
		if err != nil {
			return nil, err
		}
		verificationID = &verification.ID
	}
	var invitationHash *string
	if settings.InvitationRequired {
		normalized := normalizeInvitationCode(invitationCode)
		if normalized == "" {
			return nil, ErrInvalidInvitationCode
		}
		hash := sha256Hex(normalized)
		invitationHash = &hash
	}
	if err := s.repo.RegisterEmailUser(ctx, user, verificationID, invitationHash, time.Now()); err != nil {
		var pqErr *pq.Error
		if (errors.As(err, &pqErr) && pqErr.Code == "23505") || strings.Contains(strings.ToLower(err.Error()), "duplicate") || strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, ErrEmailInUse
		}
		if errors.Is(err, storage.ErrInvalidVerificationCode) {
			return nil, ErrInvalidVerificationCode
		}
		if errors.Is(err, storage.ErrInvalidInvitationCode) {
			return nil, ErrInvalidInvitationCode
		}
		return nil, err
	}
	token, err := s.createSession(ctx, user)
	if err != nil {
		return nil, err
	}
	return &LoginResult{User: user, SessionToken: token}, nil
}

func (s *Service) LoginEmail(ctx context.Context, email, password string) (*LoginResult, error) {
	settings, err := s.repo.GetAuthSettings(ctx)
	if err != nil {
		return nil, err
	}
	if !settings.EmailAuthEnabled {
		return nil, ErrEmailAuthDisabled
	}
	normalized, err := normalizeEmail(email)
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	user, err := s.repo.UserByEmail(ctx, normalized)
	if err != nil || user.PasswordHash == nil || user.Status != storage.UserStatusActive || bcrypt.CompareHashAndPassword([]byte(*user.PasswordHash), []byte(password)) != nil {
		return nil, ErrInvalidCredentials
	}
	now := time.Now()
	user.LastLoginAt = &now
	token, err := s.createSession(ctx, user)
	if err != nil {
		return nil, err
	}
	return &LoginResult{User: user, SessionToken: token}, nil
}

func (s *Service) Authenticate(ctx context.Context, rawToken string) (*storage.User, error) {
	if rawToken == "" {
		return nil, ErrUnauthenticated
	}
	user, err := s.repo.UserBySession(ctx, sha256Hex(rawToken), time.Now())
	if err != nil || user == nil {
		return nil, ErrUnauthenticated
	}
	return user, nil
}

func (s *Service) Logout(ctx context.Context, rawToken string) error {
	if rawToken == "" {
		return nil
	}
	return s.repo.DeleteSession(ctx, sha256Hex(rawToken))
}

func (s *Service) SessionTTL() time.Duration { return s.cfg.SessionTTL }

func (s *Service) createSession(ctx context.Context, user *storage.User) (string, error) {
	raw, err := randomToken(32)
	if err != nil {
		return "", err
	}
	now := time.Now()
	if err := s.repo.CreateSession(ctx, &storage.AuthSession{TokenHash: sha256Hex(raw), UserID: user.ID, ExpiresAt: now.Add(s.cfg.SessionTTL), CreatedAt: now}); err != nil {
		return "", err
	}
	return raw, nil
}

type linuxDOProfile struct {
	Sub        string          `json:"sub"`
	Username   string          `json:"username"`
	Login      string          `json:"login"`
	Name       string          `json:"name"`
	Email      string          `json:"email"`
	AvatarURL  string          `json:"avatar_url"`
	Active     *bool           `json:"active"`
	TrustLevel *int            `json:"trust_level"`
	Raw        storage.JSONMap `json:"-"`
}

func (s *Service) exchangeCode(ctx context.Context, code, verifier string) (string, error) {
	form := url.Values{
		"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {s.cfg.LinuxDORedirectURI},
		"client_id": {s.cfg.LinuxDOClientID}, "client_secret": {s.cfg.LinuxDOClientSecret}, "code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	res, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("LinuxDo token 请求失败: %w", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("LinuxDo token 请求失败（HTTP %d）", res.StatusCode)
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &token); err != nil || token.AccessToken == "" {
		return "", errors.New("LinuxDo token 响应无效")
	}
	return token.AccessToken, nil
}

func (s *Service) fetchLinuxDOProfile(ctx context.Context, accessToken string) (*linuxDOProfile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.UserInfoURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	res, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("读取 LinuxDo 用户信息失败: %w", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("读取 LinuxDo 用户信息失败（HTTP %d）", res.StatusCode)
	}
	var profile linuxDOProfile
	if err := json.Unmarshal(body, &profile); err != nil {
		return nil, errors.New("LinuxDo 用户信息响应无效")
	}
	var raw map[string]any
	if json.Unmarshal(body, &raw) == nil {
		profile.Raw = storage.JSONMap(raw)
	}
	return &profile, nil
}

func normalizeEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || !strings.Contains(email, "@") || len(email) > 320 {
		return "", errors.New("请输入有效邮箱")
	}
	return email, nil
}

func normalizeOptionalEmail(raw string) *string {
	if email, err := normalizeEmail(raw); err == nil {
		return &email
	}
	return nil
}

func normalizeOptionalString(raw string) *string {
	if v := strings.TrimSpace(raw); v != "" {
		return &v
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", sum[:])
}

func safeReturnTo(raw string) string {
	if strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") && !strings.ContainsAny(raw, "\r\n") {
		return raw
	}
	return "/"
}
