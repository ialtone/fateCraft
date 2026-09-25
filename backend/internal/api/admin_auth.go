package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	authservice "github.com/ialtone/fatecraft/internal/auth"
	"github.com/ialtone/fatecraft/internal/storage"
)

func (s *Server) getAdminAuthSettings(c *gin.Context) {
	settings, err := s.auth.AdminSettings(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, settings)
}

func (s *Server) updateAdminAuthSettings(c *gin.Context) {
	var body authservice.UpdateAuthSettingsInput
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式无效"})
		return
	}
	// 路由已由 requireOwner 保护；关闭密码登录前需要确认当前 owner 自己仍有其他登录方式。
	value, _ := c.Get(authUserKey)
	owner, ok := value.(*storage.User)
	if !ok || owner == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	settings, err := s.auth.UpdateAdminSettings(c.Request.Context(), owner.ID, body)
	if err != nil {
		if errors.Is(err, authservice.ErrInvalidAuthSettings) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, authservice.ErrEncryptionNotConfigured) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
			return
		}
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, settings)
}

func (s *Server) testAdminEmail(c *gin.Context) {
	var body struct {
		Email string `json:"email"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式无效"})
		return
	}
	if err := s.auth.SendTestEmail(c.Request.Context(), body.Email); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "sent"})
}

func (s *Server) listAdminInvitations(c *gin.Context) {
	invitations, err := s.auth.ListInvitations(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"invitation_codes": invitations})
}

func (s *Server) createAdminInvitation(c *gin.Context) {
	var body authservice.CreateInvitationInput
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式无效"})
		return
	}
	user, _ := c.Get(authUserKey)
	admin, ok := user.(*storage.User)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "仅管理员可以执行此操作"})
		return
	}
	created, err := s.auth.CreateInvitation(c.Request.Context(), body, admin.ID)
	if err != nil {
		if errors.Is(err, authservice.ErrInvalidAuthSettings) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, created)
}

func (s *Server) updateAdminInvitation(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var body authservice.UpdateInvitationInput
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式无效"})
		return
	}
	invitation, err := s.auth.UpdateInvitation(c.Request.Context(), id, body)
	if err != nil {
		if errors.Is(err, authservice.ErrInvalidAuthSettings) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, invitation)
}
