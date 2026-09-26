// Package auth implements the server-side account and OAuth session flows.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
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
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/ialtone/fatecraft/internal/storage"
)

const linuxDOProvider = "linuxdo"

// LinuxDOFlowTTL 是一次 LinuxDo 授权流程及其浏览器绑定 Cookie 的有效期。
const LinuxDOFlowTTL = 10 * time.Minute

// 用户名规则：去除首尾空白后 2-32 个字符，不能包含 @、空白、控制或格式字符。
const (
	usernameMinRunes    = 2
	usernameMaxRunes    = 32
	displayNameMaxRunes = 64
	returnToMaxBytes    = 2048
)

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
	ErrLinuxDOLoginDisabled      = errors.New("LinuxDo 登录未启用")
	ErrInstallationRequired      = errors.New("请先完成首次安装")
	ErrInvalidCredentials        = errors.New("用户名、邮箱或密码错误")
	// ErrEmailInUse 与存储层共用同一个哨兵，唯一约束冲突和注册前检查可以统一判断。
	ErrEmailInUse                 = storage.ErrEmailInUse
	ErrInvalidUsername            = errors.New("用户名无效")
	ErrInvalidDisplayName         = errors.New("昵称无效")
	ErrInvalidOAuthFlow           = errors.New("OAuth 登录流程已失效，请重试")
	ErrProviderRejected           = errors.New("LinuxDo 账号不可用")
	ErrLinuxDOUnbound             = errors.New("该 LinuxDo 账户尚未绑定站内账号")
	ErrLinuxDOCancelled           = errors.New("已取消 LinuxDo 授权")
	ErrLinuxDOBindSessionMismatch = errors.New("绑定 LinuxDo 需要在发起绑定的账号保持登录时完成")
)

type Repository interface {
	CreateOAuthFlow(context.Context, *storage.OAuthLoginFlow) error
	ConsumeOAuthFlow(context.Context, string, string, time.Time) (*storage.OAuthLoginFlow, error)
	CreateSession(context.Context, *storage.AuthSession) error
	DeleteSession(context.Context, string) error
	UserBySession(context.Context, string, time.Time) (*storage.User, error)
	UserByEmail(context.Context, string) (*storage.User, error)
	UserByIdentifier(context.Context, string) (*storage.User, error)
	RegisterEmailUser(context.Context, *storage.User, *uuid.UUID, *string, time.Time) error
	ExternalUserByProviderSubject(context.Context, string, string) (*storage.User, error)
	BindExternalIdentity(context.Context, uuid.UUID, storage.ExternalIdentityInput) (*storage.ExternalIdentitySummary, error)
	ListExternalIdentities(context.Context, uuid.UUID) ([]storage.ExternalIdentitySummary, error)
	ListAllExternalIdentities(context.Context, int, int) ([]storage.AdminExternalIdentitySummary, error)
	UnbindExternalIdentity(context.Context, uuid.UUID, uuid.UUID, bool) error
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
	InstallationComplete(context.Context) (bool, error)
	InitializeOwner(context.Context, *storage.User) (*storage.User, error)
	ListUsers(context.Context, int, int) ([]storage.User, error)
	UpdateUserRole(context.Context, uuid.UUID, uuid.UUID, string) (*storage.User, error)
	UpdateUserStatus(context.Context, uuid.UUID, uuid.UUID, string) (*storage.User, error)
	ListAdminAuditLogs(context.Context, int) ([]storage.AdminAuditLog, error)
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
	cipher     credentialCipher
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
	initialized, err := s.repo.InstallationComplete(ctx)
	if err != nil {
		return PublicConfig{}, err
	}
	if !initialized {
		settings.EmailRegistrationEnabled = false
		// LinuxDo is a binding/login method after installation, never a way to
		// create an unowned account before the first local owner exists.
		settings.EmailAuthEnabled = false
	}
	return PublicConfig{
		EmailAuthEnabled: settings.EmailAuthEnabled, EmailRegistrationEnabled: settings.EmailRegistrationEnabled,
		EmailVerificationEnabled: settings.EmailVerificationEnabled, InvitationRequired: settings.InvitationRequired,
		VerificationResendSeconds: settings.VerificationResendSeconds,
		LinuxDOEnabled:            initialized && s.linuxDOLoginEnabled(settings),
	}, nil
}

// linuxDOLoginEnabled 是 LinuxDo 登录的实际可用性：部署配置齐全（环境变量）
// 且管理员未在管理后台关闭该开关。绑定流程只看部署配置，不受开关影响。
func (s *Service) linuxDOLoginEnabled(settings *storage.AuthSettings) bool {
	return s.cfg.LinuxDOEnabled() && settings.LinuxDOLoginEnabled
}

// LinuxDOStart 是发起 LinuxDo 授权后需要交给浏览器的数据。
type LinuxDOStart struct {
	// RedirectURL 是 LinuxDo 授权页地址。
	RedirectURL string
	// BrowserBinding 是需要写入短期 HttpOnly Cookie 的随机 nonce；数据库只保存它的 SHA-256。
	BrowserBinding string
}

// StartLinuxDO 发起 LinuxDo 登录流程，只能在首次安装完成后使用。
func (s *Service) StartLinuxDO(ctx context.Context, returnTo string) (*LinuxDOStart, error) {
	if !s.cfg.LinuxDOEnabled() {
		return nil, ErrLinuxDONotConfigured
	}
	initialized, err := s.repo.InstallationComplete(ctx)
	if err != nil {
		return nil, err
	}
	if !initialized {
		return nil, ErrInstallationRequired
	}
	settings, err := s.repo.GetAuthSettings(ctx)
	if err != nil {
		return nil, err
	}
	if !settings.LinuxDOLoginEnabled {
		return nil, ErrLinuxDOLoginDisabled
	}
	return s.startLinuxDOFlow(ctx, storage.OAuthPurposeLogin, nil, returnTo)
}

// StartLinuxDOBind 为当前已登录用户发起 LinuxDo 绑定流程。
func (s *Service) StartLinuxDOBind(ctx context.Context, userID uuid.UUID, returnTo string) (*LinuxDOStart, error) {
	if !s.cfg.LinuxDOEnabled() {
		return nil, ErrLinuxDONotConfigured
	}
	return s.startLinuxDOFlow(ctx, storage.OAuthPurposeBind, &userID, returnTo)
}

// startLinuxDOFlow 创建一次性授权流程：state、PKCE verifier 和浏览器绑定 nonce 都是随机值，
// 数据库只保存 state 与 nonce 的 SHA-256。
func (s *Service) startLinuxDOFlow(ctx context.Context, purpose string, userID *uuid.UUID, returnTo string) (*LinuxDOStart, error) {
	state, err := randomToken(32)
	if err != nil {
		return nil, fmt.Errorf("生成 OAuth state 失败: %w", err)
	}
	verifier, err := randomToken(48)
	if err != nil {
		return nil, fmt.Errorf("生成 PKCE verifier 失败: %w", err)
	}
	binding, err := randomToken(32)
	if err != nil {
		return nil, fmt.Errorf("生成浏览器绑定 nonce 失败: %w", err)
	}
	now := time.Now()
	flow := &storage.OAuthLoginFlow{
		StateHash: sha256Hex(state), Provider: linuxDOProvider, Purpose: purpose, UserID: userID,
		CodeVerifier: verifier, BrowserBindingHash: sha256Hex(binding), ReturnTo: safeReturnTo(returnTo),
		ExpiresAt: now.Add(LinuxDOFlowTTL), CreatedAt: now,
	}
	if err := s.repo.CreateOAuthFlow(ctx, flow); err != nil {
		return nil, err
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
	return &LinuxDOStart{RedirectURL: s.cfg.AuthorizationURL + "?" + params.Encode(), BrowserBinding: binding}, nil
}

type LoginResult struct {
	// Purpose 只在 LinuxDo 回调结果中设置：login 或 bind。
	Purpose      string
	User         *storage.User
	SessionToken string
	ReturnTo     string
}

// LinuxDOCallback 是 LinuxDo 授权回调携带的参数和发起回调的浏览器上下文。
type LinuxDOCallback struct {
	Code  string
	State string
	// BrowserBinding 是回调请求携带的浏览器绑定 Cookie 原值。
	BrowserBinding string
	// SessionUserID 是回调请求当前有效会话对应的用户；没有有效会话时为 nil。
	SessionUserID *uuid.UUID
}

// LinuxDOFlowError 表示 state 已经匹配到本浏览器发起的授权流程之后发生的失败。
// 它携带流程用途和回跳地址，调用方可以把绑定失败的原因带回发起页面。
type LinuxDOFlowError struct {
	Purpose  string
	ReturnTo string
	Err      error
}

func (e *LinuxDOFlowError) Error() string { return e.Err.Error() }

func (e *LinuxDOFlowError) Unwrap() error { return e.Err }

// CompleteLinuxDO 完成 LinuxDo 授权回调。流程必须由同一浏览器发起（浏览器绑定 nonce 匹配）；
// 绑定流程还要求回调时的当前会话正是发起绑定的用户。state 匹配之后的失败都包装为 *LinuxDOFlowError。
func (s *Service) CompleteLinuxDO(ctx context.Context, in LinuxDOCallback) (*LoginResult, error) {
	if !s.cfg.LinuxDOEnabled() {
		return nil, ErrInvalidOAuthFlow
	}
	flow, err := s.consumeLinuxDOFlow(ctx, in.State, in.BrowserBinding)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*LoginResult, error) {
		return nil, &LinuxDOFlowError{Purpose: flow.Purpose, ReturnTo: flow.ReturnTo, Err: err}
	}
	if flow.Purpose == storage.OAuthPurposeLogin {
		// 管理员关闭 LinuxDo 登录后，已发起的登录流程也不得完成；绑定流程不受影响。
		settings, err := s.repo.GetAuthSettings(ctx)
		if err != nil {
			return fail(err)
		}
		if !settings.LinuxDOLoginEnabled {
			return fail(ErrLinuxDOLoginDisabled)
		}
	}
	if in.Code == "" {
		return fail(ErrInvalidOAuthFlow)
	}
	if flow.Purpose == storage.OAuthPurposeBind {
		// 先校验会话再交换 code：授权链接被转交给其他浏览器或账号时不会产生任何外部调用和绑定。
		if flow.UserID == nil || in.SessionUserID == nil || *flow.UserID != *in.SessionUserID {
			return fail(ErrLinuxDOBindSessionMismatch)
		}
	}
	accessToken, err := s.exchangeCode(ctx, in.Code, flow.CodeVerifier)
	if err != nil {
		return fail(err)
	}
	profile, err := s.fetchLinuxDOProfile(ctx, accessToken)
	if err != nil {
		return fail(err)
	}
	if profile.Active != nil && !*profile.Active {
		return fail(ErrProviderRejected)
	}
	if profile.Sub == "" {
		return fail(errors.New("LinuxDo 用户信息缺少 sub"))
	}
	identityInput := storage.ExternalIdentityInput{
		Provider: linuxDOProvider, ProviderUserID: profile.Sub, Username: firstNonEmpty(profile.Username, profile.Login),
		Email: normalizeOptionalEmail(profile.Email), DisplayName: firstNonEmpty(profile.Name, profile.Username, profile.Login, "LinuxDo 用户"),
		AvatarURL: normalizeOptionalString(profile.AvatarURL), TrustLevel: profile.TrustLevel, Profile: profile.Raw,
	}
	if flow.Purpose == storage.OAuthPurposeBind {
		if _, err := s.repo.BindExternalIdentity(ctx, *flow.UserID, identityInput); err != nil {
			return fail(err)
		}
		return &LoginResult{Purpose: flow.Purpose, ReturnTo: flow.ReturnTo}, nil
	}
	user, err := s.repo.ExternalUserByProviderSubject(ctx, linuxDOProvider, profile.Sub)
	if err != nil {
		if errors.Is(err, storage.ErrIdentityNotFound) {
			return fail(ErrLinuxDOUnbound)
		}
		return fail(err)
	}
	token, err := s.createSession(ctx, user)
	if err != nil {
		return fail(err)
	}
	return &LoginResult{Purpose: flow.Purpose, User: user, SessionToken: token, ReturnTo: flow.ReturnTo}, nil
}

// AbortLinuxDO 处理授权回调携带 error 参数（例如用户拒绝授权）的情况。
// state 能匹配到本浏览器发起的流程时，消耗该流程并返回带用途和回跳地址的 *LinuxDOFlowError；
// 否则原样返回 cause，由调用方回落到登录页。
func (s *Service) AbortLinuxDO(ctx context.Context, state, browserBinding string, cause error) error {
	flow, err := s.consumeLinuxDOFlow(ctx, state, browserBinding)
	if err != nil {
		return cause
	}
	return &LinuxDOFlowError{Purpose: flow.Purpose, ReturnTo: flow.ReturnTo, Err: cause}
}

// consumeLinuxDOFlow 一次性消耗 state 对应的流程，并以常量时间比较浏览器绑定 nonce 的摘要。
// 缺少 state 或 nonce、流程不存在或已过期、nonce 不匹配时统一返回 ErrInvalidOAuthFlow；
// 流程无论是否匹配都会被删除，不能重放。
func (s *Service) consumeLinuxDOFlow(ctx context.Context, state, browserBinding string) (*storage.OAuthLoginFlow, error) {
	if state == "" || browserBinding == "" {
		return nil, ErrInvalidOAuthFlow
	}
	flow, err := s.repo.ConsumeOAuthFlow(ctx, linuxDOProvider, sha256Hex(state), time.Now())
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, ErrInvalidOAuthFlow
		}
		return nil, err
	}
	if flow == nil {
		return nil, ErrInvalidOAuthFlow
	}
	if subtle.ConstantTimeCompare([]byte(flow.BrowserBindingHash), []byte(sha256Hex(browserBinding))) != 1 {
		return nil, ErrInvalidOAuthFlow
	}
	return flow, nil
}

// RegisterEmail 按当前服务端策略注册邮箱账号。username 是独立的登录用户名，
// displayName 为空时默认使用用户名。
func (s *Service) RegisterEmail(ctx context.Context, email, username, password, displayName, verificationCode, invitationCode string) (*LoginResult, error) {
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
	username, err = normalizeUsername(username)
	if err != nil {
		return nil, err
	}
	displayName, err = normalizeDisplayName(displayName, username)
	if err != nil {
		return nil, err
	}
	if err := validatePassword(password); err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	hashString := string(hash)
	user := &storage.User{ID: uuid.New(), Username: username, Email: &email, PasswordHash: &hashString, DisplayName: displayName, Status: storage.UserStatusActive, CreatedAt: time.Now(), UpdatedAt: time.Now()}
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
		// 用户名/邮箱冲突已由存储层按唯一索引名称转换为哨兵错误，这里原样返回。
		switch {
		case errors.Is(err, storage.ErrInvalidVerificationCode):
			return nil, ErrInvalidVerificationCode
		case errors.Is(err, storage.ErrInvalidInvitationCode):
			return nil, ErrInvalidInvitationCode
		default:
			return nil, err
		}
	}
	token, err := s.createSession(ctx, user)
	if err != nil {
		return nil, err
	}
	return &LoginResult{User: user, SessionToken: token}, nil
}

// LoginEmail 使用用户名或邮箱加密码登录。含 "@" 的标识只按规范化邮箱匹配，
// 其余按用户名大小写不敏感匹配。
func (s *Service) LoginEmail(ctx context.Context, identifier, password string) (*LoginResult, error) {
	initialized, err := s.repo.InstallationComplete(ctx)
	if err != nil {
		return nil, err
	}
	if !initialized {
		return nil, ErrInstallationRequired
	}
	settings, err := s.repo.GetAuthSettings(ctx)
	if err != nil {
		return nil, err
	}
	if !settings.EmailAuthEnabled {
		return nil, ErrEmailAuthDisabled
	}
	identifier = strings.TrimSpace(identifier)
	if identifier == "" || utf8.RuneCountInString(identifier) > 320 {
		return nil, ErrInvalidCredentials
	}
	if strings.Contains(identifier, "@") {
		normalized, emailErr := normalizeEmail(identifier)
		if emailErr != nil {
			return nil, ErrInvalidCredentials
		}
		identifier = normalized
	}
	user, err := s.repo.UserByIdentifier(ctx, identifier)
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

func (s *Service) InstallationComplete(ctx context.Context) (bool, error) {
	return s.repo.InstallationComplete(ctx)
}

// InitializeOwner 在空库上创建第一个本地账号并使其成为唯一 owner。
// 用户名规则与普通注册相同；displayName 为空时默认使用用户名。
func (s *Service) InitializeOwner(ctx context.Context, email, username, password, displayName string) (*LoginResult, error) {
	username, err := normalizeUsername(username)
	if err != nil {
		return nil, err
	}
	displayName, err = normalizeDisplayName(displayName, username)
	if err != nil {
		return nil, err
	}
	if err := validatePassword(password); err != nil {
		return nil, err
	}
	var emailPtr *string
	if strings.TrimSpace(email) != "" {
		normalizedEmail, emailErr := normalizeEmail(email)
		if emailErr != nil {
			return nil, emailErr
		}
		emailPtr = &normalizedEmail
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	user, err := s.repo.InitializeOwner(ctx, &storage.User{ID: uuid.New(), Username: username, Email: emailPtr, PasswordHash: ptrString(string(hash)), DisplayName: displayName, Status: storage.UserStatusActive, CreatedAt: now, UpdatedAt: now})
	if err != nil {
		// 用户名/邮箱冲突已由存储层按唯一索引名称转换为哨兵错误。
		return nil, err
	}
	token, err := s.createSession(ctx, user)
	if err != nil {
		return nil, err
	}
	return &LoginResult{User: user, SessionToken: token}, nil
}

func (s *Service) ListExternalIdentities(ctx context.Context, userID uuid.UUID) ([]storage.ExternalIdentitySummary, error) {
	return s.repo.ListExternalIdentities(ctx, userID)
}

// ListAllExternalIdentities 分页读取全站第三方身份的脱敏摘要；limit/offset 由调用方校验。
func (s *Service) ListAllExternalIdentities(ctx context.Context, limit, offset int) ([]storage.AdminExternalIdentitySummary, error) {
	return s.repo.ListAllExternalIdentities(ctx, limit, offset)
}

// UnbindExternalIdentity 解除当前用户的一个第三方身份。全站关闭密码登录时，
// 即使账号设置了密码也不能解除最后一个第三方身份，否则账号将无法登录。
func (s *Service) UnbindExternalIdentity(ctx context.Context, userID, identityID uuid.UUID) error {
	settings, err := s.repo.GetAuthSettings(ctx)
	if err != nil {
		return err
	}
	return s.repo.UnbindExternalIdentity(ctx, userID, identityID, settings.EmailAuthEnabled)
}

// hasLinuxDOIdentity 判断用户是否已经绑定至少一个 LinuxDo 身份。
func (s *Service) hasLinuxDOIdentity(ctx context.Context, userID uuid.UUID) (bool, error) {
	identities, err := s.repo.ListExternalIdentities(ctx, userID)
	if err != nil {
		return false, err
	}
	for _, identity := range identities {
		if identity.Provider == linuxDOProvider {
			return true, nil
		}
	}
	return false, nil
}

// ListUsers 分页读取用户列表；limit/offset 由调用方校验。
func (s *Service) ListUsers(ctx context.Context, limit, offset int) ([]storage.User, error) {
	return s.repo.ListUsers(ctx, limit, offset)
}

func (s *Service) UpdateUserRole(ctx context.Context, actorID, targetID uuid.UUID, role string) (*storage.User, error) {
	return s.repo.UpdateUserRole(ctx, actorID, targetID, role)
}

func (s *Service) UpdateUserStatus(ctx context.Context, actorID, targetID uuid.UUID, status string) (*storage.User, error) {
	return s.repo.UpdateUserStatus(ctx, actorID, targetID, status)
}

func (s *Service) ListAdminAuditLogs(ctx context.Context, limit int) ([]storage.AdminAuditLog, error) {
	return s.repo.ListAdminAuditLogs(ctx, limit)
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

// normalizeUsername 校验并规范化登录用户名：去除首尾空白后必须为 2-32 个字符，
// 不能包含 @（避免与邮箱标识混淆）、空白、控制字符或零宽等格式字符。大小写原样保留，
// 唯一性由数据库按 lower(username) 保证。
func normalizeUsername(raw string) (string, error) {
	username := strings.TrimSpace(raw)
	if username == "" {
		return "", fmt.Errorf("%w：用户名不能为空", ErrInvalidUsername)
	}
	if !utf8.ValidString(username) {
		return "", fmt.Errorf("%w：包含无效字符", ErrInvalidUsername)
	}
	if count := utf8.RuneCountInString(username); count < usernameMinRunes || count > usernameMaxRunes {
		return "", fmt.Errorf("%w：长度必须为 %d-%d 个字符", ErrInvalidUsername, usernameMinRunes, usernameMaxRunes)
	}
	for _, r := range username {
		if r == '@' {
			return "", fmt.Errorf("%w：不能包含 @", ErrInvalidUsername)
		}
		if unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return "", fmt.Errorf("%w：不能包含空格或控制字符", ErrInvalidUsername)
		}
	}
	return username, nil
}

// normalizeDisplayName 规范化展示昵称；为空时使用 fallback（通常是用户名）。
func normalizeDisplayName(raw, fallback string) (string, error) {
	displayName := strings.TrimSpace(raw)
	if displayName == "" {
		displayName = fallback
	}
	if !utf8.ValidString(displayName) || utf8.RuneCountInString(displayName) > displayNameMaxRunes {
		return "", fmt.Errorf("%w：不能超过 %d 个字符", ErrInvalidDisplayName, displayNameMaxRunes)
	}
	for _, r := range displayName {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("%w：不能包含控制字符", ErrInvalidDisplayName)
		}
	}
	return displayName, nil
}

// validatePassword 校验密码长度；bcrypt 最多只使用前 72 字节。
func validatePassword(password string) error {
	if len(password) < 8 || len(password) > 72 {
		return errors.New("密码长度必须为 8-72 字节")
	}
	return nil
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

func ptrString(value string) *string { return &value }

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

// safeReturnTo 只接受站内绝对路径（可带查询串和片段），其余一律回落到 "/"。
// 浏览器按 WHATWG URL 规则会把 "\" 当作 "/"，并删除制表符和换行，
// 所以先拒绝反斜杠、空白和控制字符（原文和解码后的路径都检查），
// 再要求解析结果没有 scheme、host、userinfo 和 opaque，且路径以恰好一个 "/" 开头。
func safeReturnTo(raw string) string {
	if raw == "" || len(raw) > returnToMaxBytes || !utf8.ValidString(raw) || hasUnsafeURLRune(raw) {
		return "/"
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil || parsed.Opaque != "" {
		return "/"
	}
	if !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") ||
		!strings.HasPrefix(parsed.Path, "/") || strings.HasPrefix(parsed.Path, "//") || hasUnsafeURLRune(parsed.Path) {
		return "/"
	}
	return raw
}

// hasUnsafeURLRune 报告字符串是否包含反斜杠、空白或控制字符。
func hasUnsafeURLRune(value string) bool {
	for _, r := range value {
		if r == '\\' || unicode.IsSpace(r) || unicode.IsControl(r) {
			return true
		}
	}
	return false
}
