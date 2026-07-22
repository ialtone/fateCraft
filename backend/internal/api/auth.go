package api

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	authservice "github.com/ialtone/fatecraft/internal/auth"
	"github.com/ialtone/fatecraft/internal/identity"
	"github.com/ialtone/fatecraft/internal/logging"
	"github.com/ialtone/fatecraft/internal/storage"
)

const (
	sessionCookieName = "fatecraft_session"
	authUserKey       = "auth_user"
)

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
	redirectURL, err := s.auth.StartLinuxDO(c.Request.Context(), c.Query("return_to"))
	if err != nil {
		authFail(c, err)
		return
	}
	c.Redirect(http.StatusFound, redirectURL)
}

func (s *Server) completeLinuxDO(c *gin.Context) {
	if c.Query("error") != "" {
		logging.Named("auth").Info("LinuxDo 用户取消授权", zap.String("provider_error", c.Query("error")))
		s.redirectLoginError(c, "LinuxDo 已取消授权")
		return
	}
	result, err := s.auth.CompleteLinuxDO(c.Request.Context(), c.Query("code"), c.Query("state"))
	if err != nil {
		logging.Named("auth").Warn("LinuxDo 登录回调失败", zap.Error(err))
		s.redirectLoginError(c, "LinuxDo 登录失败，请重试")
		return
	}
	s.setSessionCookie(c, result.SessionToken)
	c.Redirect(http.StatusFound, result.ReturnTo)
}

type emailAuthBody struct {
	Email            string `json:"email"`
	Password         string `json:"password"`
	DisplayName      string `json:"display_name"`
	VerificationCode string `json:"verification_code"`
	InvitationCode   string `json:"invitation_code"`
}

func (s *Server) sendRegistrationCode(c *gin.Context) {
	var body struct {
		Email string `json:"email"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式无效"})
		return
	}
	if err := s.auth.SendRegistrationCode(c.Request.Context(), body.Email); err != nil {
		authFail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "sent"})
}

func (s *Server) registerEmail(c *gin.Context) {
	var body emailAuthBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式无效"})
		return
	}
	result, err := s.auth.RegisterEmail(
		c.Request.Context(), body.Email, body.Password, body.DisplayName,
		body.VerificationCode, body.InvitationCode,
	)
	if err != nil {
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
	result, err := s.auth.LoginEmail(c.Request.Context(), body.Email, body.Password)
	if err != nil {
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

func (s *Server) redirectLoginError(c *gin.Context, message string) {
	c.Redirect(http.StatusFound, "/login?error="+url.QueryEscape(message))
}

func authFail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, authservice.ErrUnauthenticated), errors.Is(err, authservice.ErrInvalidCredentials):
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
	case errors.Is(err, authservice.ErrEmailAuthDisabled), errors.Is(err, authservice.ErrEmailRegistrationDisabled), errors.Is(err, authservice.ErrEmailVerificationDisabled):
		c.JSON(http.StatusNotFound, gin.H{"error": "资源不存在"})
	case errors.Is(err, authservice.ErrEmailInUse):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, authservice.ErrInvalidVerificationCode), errors.Is(err, authservice.ErrInvalidInvitationCode):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, authservice.ErrVerificationCooldown):
		c.JSON(http.StatusTooManyRequests, gin.H{"error": err.Error()})
	case errors.Is(err, authservice.ErrEmailSendFailed):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": authservice.ErrEmailSendFailed.Error()})
	case errors.Is(err, authservice.ErrEncryptionNotConfigured):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "发信服务尚未配置"})
	case errors.Is(err, authservice.ErrLinuxDONotConfigured):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
	case errors.Is(err, authservice.ErrInvalidOAuthFlow), errors.Is(err, authservice.ErrProviderRejected):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, storage.ErrNotFound):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	}
}
