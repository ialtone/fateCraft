package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ialtone/fatecraft/internal/storage"
)

// 管理列表的 offset 上限；limit 的范围见 adminListLimit。
const adminListMaxOffset = 100000

func (s *Server) getSetupStatus(c *gin.Context) {
	complete, err := s.auth.InstallationComplete(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	// installable 与 InitializeOwner 同一判定：未写安装标记但已有用户时不可安装，并给出原因。
	status := gin.H{"initialized": complete, "installable": false}
	switch blocker := s.auth.SetupBlocker(c.Request.Context()); {
	case blocker == nil:
		status["installable"] = true
	case errors.Is(blocker, storage.ErrInstallationHasUsers):
		status["reason"] = blocker.Error() + "。数据库中已有账号但未记录安装完成，请清空数据库后重新安装，或由运维修复安装标记。"
	case !errors.Is(blocker, storage.ErrInstallationComplete):
		fail(c, blocker)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, status)
}

// initializeSetup 是公开接口，但存储层事务只接受完全空的数据库且只能成功一次：
// 它直接创建第一个本地账号并使其成为唯一 owner，不存在“把已登录账号提升为 owner”的路径。
func (s *Server) initializeSetup(c *gin.Context) {
	var body struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Confirm     bool   `json:"confirm"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || !body.Confirm {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请填写账号信息并确认成为平台拥有者"})
		return
	}
	result, err := s.auth.InitializeOwner(c.Request.Context(), body.Email, body.Username, body.Password, body.DisplayName)
	if err != nil {
		switch {
		case errors.Is(err, storage.ErrInstallationComplete), errors.Is(err, storage.ErrInstallationHasUsers):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		default:
			authFail(c, err)
		}
		return
	}
	if s.system != nil {
		s.system.InvalidateCache()
	}
	s.setSessionCookie(c, result.SessionToken)
	c.JSON(http.StatusCreated, gin.H{"user": result.User, "initialized": true})
}

func (s *Server) listAdminUsers(c *gin.Context) {
	limit, ok := adminListLimit(c)
	if !ok {
		return
	}
	offset, ok := adminListOffset(c)
	if !ok {
		return
	}
	if len(c.Query("q")) > 256 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "搜索内容过长"})
		return
	}
	users, err := s.auth.ListUsers(c.Request.Context(), limit+1, offset, c.Query("q"))
	if err != nil {
		fail(c, err)
		return
	}
	hasMore := len(users) > limit
	if hasMore {
		users = users[:limit]
	}
	c.JSON(http.StatusOK, gin.H{"users": users, "has_more": hasMore})
}

func (s *Server) listAdminAuthIdentities(c *gin.Context) {
	limit, ok := adminListLimit(c)
	if !ok {
		return
	}
	offset, ok := adminListOffset(c)
	if !ok {
		return
	}
	identities, err := s.auth.ListAllExternalIdentities(c.Request.Context(), limit, offset)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"identities": identities})
}

func (s *Server) updateAdminUserRole(c *gin.Context) {
	targetID, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var body struct {
		Role string `json:"role"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式无效"})
		return
	}
	actor, _ := c.Get(authUserKey)
	owner, _ := actor.(*storage.User)
	updated, err := s.auth.UpdateUserRole(c.Request.Context(), owner.ID, targetID, body.Role)
	if err != nil {
		s.handleAdminMutationError(c, err)
		return
	}
	c.JSON(http.StatusOK, updated)
}

func (s *Server) updateAdminUserStatus(c *gin.Context) {
	targetID, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式无效"})
		return
	}
	actor, _ := c.Get(authUserKey)
	owner, _ := actor.(*storage.User)
	updated, err := s.auth.UpdateUserStatus(c.Request.Context(), owner.ID, targetID, body.Status)
	if err != nil {
		s.handleAdminMutationError(c, err)
		return
	}
	c.JSON(http.StatusOK, updated)
}

func (s *Server) listAdminAuditLogs(c *gin.Context) {
	filter, ok := logQuery(c, "audit")
	if !ok {
		return
	}
	logs, next, more, err := s.auth.ListAdminAuditLogs(c.Request.Context(), filter)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"logs": logs, "next_cursor": next, "has_more": more})
}

// adminListOffset 读取管理列表的 offset 参数：缺省为 0，必须是 0 到 100000 的整数，否则直接返回 400。
func adminListOffset(c *gin.Context) (int, bool) {
	raw := strings.TrimSpace(c.Query("offset"))
	if raw == "" {
		return 0, true
	}
	offset, err := strconv.Atoi(raw)
	if err != nil || offset < 0 || offset > adminListMaxOffset {
		c.JSON(http.StatusBadRequest, gin.H{"error": "offset 必须是 0 到 100000 的整数"})
		return 0, false
	}
	return offset, true
}

func (s *Server) handleAdminMutationError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, storage.ErrOwnerRequired):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, storage.ErrLastOwner), errors.Is(err, storage.ErrInvalidUserRole), errors.Is(err, storage.ErrInvalidUserStatus),
		errors.Is(err, storage.ErrTargetUserDisabled):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, storage.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
	default:
		fail(c, err)
	}
}
