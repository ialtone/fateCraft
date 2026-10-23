package api

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	authservice "github.com/ialtone/fatecraft/internal/auth"
	"github.com/ialtone/fatecraft/internal/identity"
	"github.com/ialtone/fatecraft/internal/logging"
	"github.com/ialtone/fatecraft/internal/storage"
)

const (
	sessionCookieName = "fatecraft_session"
	authUserKey       = "auth_user"

	// linuxDOBindingCookieName 保存发起 LinuxDo 授权的浏览器 nonce，只在 LinuxDo 授权路径下发送。
	linuxDOBindingCookieName = "fatecraft_oauth_binding"
	linuxDOBindingCookiePath = "/api/auth/linuxdo"

	// 绑定流程结束后回跳到发起页面时携带的查询参数。
	linuxDOBindQuery      = "linuxdo_bind"
	linuxDOBindErrorQuery = "linuxdo_bind_error"
)

// errLinuxDOProviderFailed 表示 LinuxDo 以 access_denied 以外的 error 参数结束授权。
var errLinuxDOProviderFailed = errors.New("LinuxDo 授权未完成")

func (s *Server) authConfig(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	config, err := s.auth.PublicConfig(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, config)
}

func (s *Server) startLinuxDO(c *gin.Context) {
	start, err := s.auth.StartLinuxDO(c.Request.Context(), c.Query("return_to"))
	if err != nil {
		authFail(c, err)
		return
	}
	s.setLinuxDOBindingCookie(c, start.BrowserBinding)
	c.Redirect(http.StatusFound, start.RedirectURL)
}

func (s *Server) startLinuxDOBind(c *gin.Context) {
	user, _ := c.Get(authUserKey)
	account, ok := user.(*storage.User)
	if !ok || account == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	start, err := s.auth.StartLinuxDOBind(c.Request.Context(), account.ID, c.Query("return_to"))
	if err != nil {
		authFail(c, err)
		return
	}
	s.setLinuxDOBindingCookie(c, start.BrowserBinding)
	c.Redirect(http.StatusFound, start.RedirectURL)
}

// completeLinuxDO 是公开的 LinuxDo 授权回调。它要求浏览器带回发起授权时下发的 nonce Cookie；
// 绑定流程还会读取当前会话，确认回调时登录的正是发起绑定的用户。
func (s *Server) completeLinuxDO(c *gin.Context) {
	// nonce Cookie 只服务这一次回调，无论成败都立即清除。
	binding, _ := c.Cookie(linuxDOBindingCookieName)
	s.clearLinuxDOBindingCookie(c)
	ctx := c.Request.Context()
	if providerError := c.Query("error"); providerError != "" {
		logging.Named("auth").Info("LinuxDo 授权未完成", zap.String("provider_error", providerError))
		cause := authservice.ErrLinuxDOCancelled
		if providerError != "access_denied" {
			cause = errLinuxDOProviderFailed
		}
		s.redirectLinuxDOFailure(c, s.auth.AbortLinuxDO(ctx, c.Query("state"), binding, cause))
		return
	}
	result, err := s.auth.CompleteLinuxDO(ctx, authservice.LinuxDOCallback{
		Code: c.Query("code"), State: c.Query("state"), BrowserBinding: binding,
		SessionUserID: s.sessionUserID(c),
	})
	if err != nil {
		logging.Named("auth").Warn("LinuxDo 回调失败", zap.Error(err))
		s.redirectLinuxDOFailure(c, err)
		return
	}
	if result.Purpose == storage.OAuthPurposeBind {
		c.Redirect(http.StatusFound, returnToWithQuery(result.ReturnTo, linuxDOBindQuery, "ok"))
		return
	}
	if result.SessionToken != "" {
		s.setSessionCookie(c, result.SessionToken)
	}
	c.Redirect(http.StatusFound, result.ReturnTo)
}

// sessionUserID 认证当前请求的会话 Cookie，返回对应用户 ID；没有有效会话时返回 nil。
// 公开的 OAuth 回调路由不经过 requireAuth，绑定流程靠它确认当前登录用户。
func (s *Server) sessionUserID(c *gin.Context) *uuid.UUID {
	raw, err := c.Cookie(sessionCookieName)
	if err != nil || raw == "" {
		return nil
	}
	user, err := s.auth.Authenticate(c.Request.Context(), raw)
	if err != nil || user == nil {
		return nil
	}
	id := user.ID
	return &id
}

// redirectLinuxDOFailure 按流程用途回跳：绑定流程带着具体原因回到发起页面，
// 登录流程或无法确认来源的流程回到登录页。
func (s *Server) redirectLinuxDOFailure(c *gin.Context, err error) {
	var flowErr *authservice.LinuxDOFlowError
	if errors.As(err, &flowErr) && flowErr.Purpose == storage.OAuthPurposeBind {
		c.Redirect(http.StatusFound, returnToWithQuery(flowErr.ReturnTo, linuxDOBindErrorQuery, linuxDOBindErrorMessage(err)))
		return
	}
	s.redirectLoginError(c, linuxDOLoginErrorMessage(err))
}

// linuxDOBindErrorMessage 把绑定失败原因转换为展示给用户的提示。
func linuxDOBindErrorMessage(err error) string {
	switch {
	case errors.Is(err, storage.ErrIdentityAlreadyBound):
		return "该 LinuxDo 账户已绑定到其他站内账号，需要先在原账号中解除绑定"
	case errors.Is(err, authservice.ErrLinuxDOCancelled):
		return "已取消 LinuxDo 授权，未绑定任何账户"
	case errors.Is(err, authservice.ErrLinuxDOBindSessionMismatch):
		return "绑定需要在发起绑定的账号保持登录时完成，请重新发起绑定"
	default:
		return "LinuxDo 绑定失败，请稍后重试"
	}
}

// linuxDOLoginErrorMessage 把登录失败原因转换为展示给用户的提示。
func linuxDOLoginErrorMessage(err error) string {
	switch {
	case errors.Is(err, authservice.ErrLinuxDOUnbound):
		return "该 LinuxDo 账户尚未绑定站内账号，请先用用户名或邮箱登录，再到“设置 → 第三方账户”中绑定"
	case errors.Is(err, authservice.ErrLinuxDOCancelled):
		return "已取消 LinuxDo 授权"
	case errors.Is(err, authservice.ErrLinuxDOLoginDisabled):
		return "LinuxDo 登录未启用"
	case errors.Is(err, authservice.ErrProviderRejected):
		return "该 LinuxDo 账号当前不可用"
	case errors.Is(err, authservice.ErrInvalidOAuthFlow):
		return "LinuxDo 登录流程已失效，请重新发起登录"
	default:
		return "LinuxDo 登录失败，请重试"
	}
}

// returnToWithQuery 在站内回跳地址上设置一个查询参数，并移除旧的绑定结果参数。
// returnTo 在发起授权时已经过 safeReturnTo 校验；这里再做一次防御，异常时回落到 "/"。
func returnToWithQuery(returnTo, key, value string) string {
	target, err := url.Parse(returnTo)
	if err != nil || target.Scheme != "" || target.Host != "" || target.User != nil ||
		!strings.HasPrefix(target.Path, "/") || strings.HasPrefix(target.Path, "//") {
		target = &url.URL{Path: "/"}
	}
	query := target.Query()
	query.Del(linuxDOBindQuery)
	query.Del(linuxDOBindErrorQuery)
	query.Set(key, value)
	target.RawQuery = query.Encode()
	return target.String()
}

func (s *Server) listAuthIdentities(c *gin.Context) {
	user, _ := c.Get(authUserKey)
	account, ok := user.(*storage.User)
	if !ok || account == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	identities, err := s.auth.ListExternalIdentities(c.Request.Context(), account.ID)
	if err != nil {
		fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"identities": identities})
}

func (s *Server) unbindAuthIdentity(c *gin.Context) {
	identityID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "第三方账户 ID 无效"})
		return
	}
	user, _ := c.Get(authUserKey)
	account, ok := user.(*storage.User)
	if !ok || account == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	if err := s.auth.UnbindExternalIdentity(c.Request.Context(), account.ID, identityID); err != nil {
		if errors.Is(err, storage.ErrLastAuthMethod) || errors.Is(err, storage.ErrIdentityNotFound) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "unbound"})
}

type emailAuthBody struct {
	Identifier       string `json:"identifier"`
	Email            string `json:"email"`
	Username         string `json:"username"`
	Password         string `json:"password"`
	DisplayName      string `json:"display_name"`
	VerificationCode string `json:"verification_code"`
	InvitationCode   string `json:"invitation_code"`
}

func (s *Server) sendRegistrationCode(c *gin.Context) {
	if !s.requireInstallationComplete(c) {
		return
	}
	var body struct {
		Email string `json:"email"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式无效"})
		return
	}
	if s.limits.count("code:"+c.ClientIP(), time.Hour, true) > 10 {
		tooManyRequests(c, "验证码请求过于频繁，请稍后再试")
		return
	}
	if err := s.auth.SendRegistrationCode(c.Request.Context(), body.Email); err != nil {
		authFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "sent"})
}

func (s *Server) registerEmail(c *gin.Context) {
	if !s.requireInstallationComplete(c) {
		return
	}
	var body emailAuthBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式无效"})
		return
	}
	verifyKey := "verify:" + strings.ToLower(strings.TrimSpace(body.Email))
	if s.limits.count(verifyKey, 15*time.Minute, false) >= 5 {
		tooManyRequests(c, "验证码错误次数过多，请稍后再试")
		return
	}
	result, err := s.auth.RegisterEmail(
		c.Request.Context(), body.Email, body.Username, body.Password, body.DisplayName,
		body.VerificationCode, body.InvitationCode,
	)
	if err != nil {
		if errors.Is(err, authservice.ErrInvalidVerificationCode) {
			s.limits.count(verifyKey, 15*time.Minute, true)
		}
		authFail(c, err)
		return
	}
	s.setSessionCookie(c, result.SessionToken)
	c.JSON(http.StatusCreated, gin.H{"user": result.User})
}

func (s *Server) loginEmail(c *gin.Context) {
	var body emailAuthBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式无效"})
		return
	}
	identifier := body.Identifier
	if identifier == "" {
		identifier = body.Email
	}
	// 失败计数：同一账号 15 分钟 5 次、同一 IP 15 分钟 20 次，超出返回 429。
	accountKey, ipKey := "login:"+strings.ToLower(strings.TrimSpace(identifier)), "login-ip:"+c.ClientIP()
	if s.limits.count(accountKey, 15*time.Minute, false) >= 5 || s.limits.count(ipKey, 15*time.Minute, false) >= 20 {
		tooManyRequests(c, "登录失败次数过多，请稍后再试")
		return
	}
	result, err := s.auth.LoginEmail(c.Request.Context(), identifier, body.Password)
	if err != nil {
		if errors.Is(err, authservice.ErrInvalidCredentials) {
			s.limits.count(accountKey, 15*time.Minute, true)
			s.limits.count(ipKey, 15*time.Minute, true)
		}
		authFail(c, err)
		return
	}
	s.setSessionCookie(c, result.SessionToken)
	c.JSON(http.StatusOK, gin.H{"user": result.User})
}

func (s *Server) logout(c *gin.Context) {
	raw, _ := c.Cookie(sessionCookieName)
	if err := s.auth.Logout(c.Request.Context(), raw); err != nil {
		fail(c, err)
		return
	}
	s.clearSessionCookie(c)
	c.JSON(http.StatusOK, gin.H{"status": "logged_out"})
}

func (s *Server) me(c *gin.Context) {
	user, ok := c.Get(authUserKey)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"user": user})
}

func (s *Server) requireAuth(c *gin.Context) {
	raw, err := c.Cookie(sessionCookieName)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	user, err := s.auth.Authenticate(c.Request.Context(), raw)
	if err != nil {
		s.clearSessionCookie(c)
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "登录已失效"})
		return
	}
	c.Set(authUserKey, user)
	ctx := identity.WithPlayerID(c.Request.Context(), user.ID.String())
	ctx = logging.WithFields(ctx, zap.String("player_id", user.ID.String()))
	c.Request = c.Request.WithContext(ctx)
	c.Next()
}

// requireAdmin 必须放在 requireAuth 之后，只允许管理员执行平台管理操作。
func (s *Server) requireAdmin(c *gin.Context) {
	value, ok := c.Get(authUserKey)
	user, valid := value.(*storage.User)
	if !ok || !valid || !user.IsAdmin() {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "仅管理员可以执行此操作"})
		return
	}
	c.Next()
}

// requireOwner protects platform identity and security configuration changes.
// It must be used after requireAuth, just like requireAdmin.
func (s *Server) requireOwner(c *gin.Context) {
	value, ok := c.Get(authUserKey)
	user, valid := value.(*storage.User)
	if !ok || !valid || !user.IsOwner() {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "仅平台拥有者可以执行此操作"})
		return
	}
	c.Next()
}

func (s *Server) requireInstallationComplete(c *gin.Context) bool {
	complete, err := s.auth.InstallationComplete(c.Request.Context())
	if err != nil {
		fail(c, err)
		return false
	}
	if !complete {
		c.JSON(http.StatusConflict, gin.H{"error": "请先完成首次安装"})
		return false
	}
	return true
}

func (s *Server) setSessionCookie(c *gin.Context, token string) {
	ttl := s.auth.SessionTTL()
	http.SetCookie(c.Writer, &http.Cookie{
		Name: sessionCookieName, Value: token, Path: "/", HttpOnly: true,
		Secure: s.cookieSecure, SameSite: http.SameSiteLaxMode,
		Expires: time.Now().Add(ttl), MaxAge: int(ttl.Seconds()),
	})
}

func (s *Server) clearSessionCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/", HttpOnly: true,
		Secure: s.cookieSecure, SameSite: http.SameSiteLaxMode,
		Expires: time.Unix(1, 0), MaxAge: -1,
	})
}

// setLinuxDOBindingCookie 下发与本次授权流程绑定的短期 nonce。Cookie 只在 LinuxDo 授权路径下发送，
// SameSite=Lax 保证 LinuxDo 通过顶层跳转回到回调地址时仍会携带。
func (s *Server) setLinuxDOBindingCookie(c *gin.Context, nonce string) {
	ttl := authservice.LinuxDOFlowTTL
	http.SetCookie(c.Writer, &http.Cookie{
		Name: linuxDOBindingCookieName, Value: nonce, Path: linuxDOBindingCookiePath, HttpOnly: true,
		Secure: s.cookieSecure, SameSite: http.SameSiteLaxMode,
		Expires: time.Now().Add(ttl), MaxAge: int(ttl.Seconds()),
	})
}

func (s *Server) clearLinuxDOBindingCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: linuxDOBindingCookieName, Value: "", Path: linuxDOBindingCookiePath, HttpOnly: true,
		Secure: s.cookieSecure, SameSite: http.SameSiteLaxMode,
		Expires: time.Unix(1, 0), MaxAge: -1,
	})
}

func (s *Server) redirectLoginError(c *gin.Context, message string) {
	c.Redirect(http.StatusFound, "/login?error="+url.QueryEscape(message))
}

func authFail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, authservice.ErrUnauthenticated), errors.Is(err, authservice.ErrInvalidCredentials):
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
	case errors.Is(err, authservice.ErrEmailAuthDisabled):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, authservice.ErrEmailRegistrationDisabled), errors.Is(err, authservice.ErrEmailVerificationDisabled):
		c.JSON(http.StatusNotFound, gin.H{"error": "资源不存在"})
	case errors.Is(err, authservice.ErrEmailInUse):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, storage.ErrUsernameInUse):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, authservice.ErrInvalidVerificationCode), errors.Is(err, authservice.ErrInvalidInvitationCode):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, authservice.ErrInvalidAuthSettings), errors.Is(err, authservice.ErrInvalidEmail), errors.Is(err, authservice.ErrInvalidPassword):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, authservice.ErrInvalidUsername), errors.Is(err, authservice.ErrInvalidDisplayName),
		errors.Is(err, storage.ErrUserNotBindable), errors.Is(err, storage.ErrIdentityAlreadyBound):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, authservice.ErrVerificationCooldown):
		c.JSON(http.StatusTooManyRequests, gin.H{"error": err.Error()})
	case errors.Is(err, authservice.ErrEmailSendFailed):
		_ = c.Error(err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": authservice.ErrEmailSendFailed.Error()})
	case errors.Is(err, authservice.ErrEncryptionNotConfigured):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "发信服务尚未配置"})
	case errors.Is(err, authservice.ErrLinuxDONotConfigured), errors.Is(err, authservice.ErrLinuxDOLoginDisabled):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
	case errors.Is(err, authservice.ErrInstallationRequired):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, authservice.ErrInvalidOAuthFlow), errors.Is(err, authservice.ErrProviderRejected), errors.Is(err, authservice.ErrLinuxDOUnbound):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, storage.ErrNotFound):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
	default:
		fail(c, err)
	}
}
