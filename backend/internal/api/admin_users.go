package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/identity"
	"github.com/ialtone/fatecraft/internal/storage"
)

const errDetailedLoggingRequired = "未对该用户开启详细日志，无法查看对话记录"

// detailedLoggingCoverage 描述详细 LLM 日志当前是否覆盖某个用户。
type detailedLoggingCoverage struct {
	Covered bool   `json:"covered"`
	Enabled bool   `json:"enabled"`
	Scope   string `json:"scope"`
}

// userLoggingCoverage 与采集端使用同一判定：已开启，且范围为全部或恰好指向该用户。
func (s *Server) userLoggingCoverage(c *gin.Context, userID uuid.UUID) (detailedLoggingCoverage, bool) {
	if s.system == nil {
		return detailedLoggingCoverage{}, true
	}
	settings, err := s.system.Get(c.Request.Context())
	if err != nil {
		fail(c, err)
		return detailedLoggingCoverage{}, false
	}
	return detailedLoggingCoverage{
		Covered: settings.CaptureDetailedLLMLog(userID.String(), ""),
		Enabled: settings.DetailedLLMLoggingEnabled,
		Scope:   settings.DetailedLLMLoggingScope,
	}, true
}

func (s *Server) getAdminUser(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	detail, err := s.adminUsers.Detail(c.Request.Context(), id)
	if err != nil {
		s.handleAdminMutationError(c, err)
		return
	}
	var rewards []storage.PlayerRewardView
	if s.rewards != nil {
		if rewards, err = s.rewards.ListPlayerRewards(c.Request.Context(), id); err != nil {
			fail(c, err)
			return
		}
	}
	coverage, ok := s.userLoggingCoverage(c, id)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": detail, "rewards": rewards, "detailed_logging": coverage})
}

// requireUserLogging 是查看对话记录的前置条件；未覆盖时返回 403，已覆盖时返回 true。
func (s *Server) requireUserLogging(c *gin.Context, userID uuid.UUID) bool {
	coverage, ok := s.userLoggingCoverage(c, userID)
	if !ok {
		return false
	}
	if !coverage.Covered {
		c.JSON(http.StatusForbidden, gin.H{"error": errDetailedLoggingRequired, "code": "detailed_logging_required"})
		return false
	}
	return true
}

func (s *Server) adminActor(c *gin.Context) uuid.UUID {
	value, _ := c.Get(authUserKey)
	if user, ok := value.(*storage.User); ok {
		return user.ID
	}
	return uuid.Nil
}

// listAdminUserConversations 只列出已被详细日志采集过正文的对话；每次查看都写管理审计。
func (s *Server) listAdminUserConversations(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	limit, ok := adminListLimit(c)
	if !ok {
		return
	}
	offset, ok := adminListOffset(c)
	if !ok {
		return
	}
	if !s.requireUserLogging(c, id) {
		return
	}
	rows, err := s.adminUsers.CapturedConversations(c.Request.Context(), id, limit+1, offset)
	if err != nil {
		fail(c, err)
		return
	}
	if err := s.adminUsers.RecordAudit(c.Request.Context(), s.adminActor(c), id, "user_conversations_view", storage.JSONMap{}); err != nil {
		fail(c, err)
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	c.JSON(http.StatusOK, gin.H{"conversations": rows, "has_more": hasMore})
}

type adminConversationRecord struct {
	ID             uuid.UUID `json:"id"`
	Purpose        string    `json:"purpose"`
	Model          string    `json:"model"`
	Success        bool      `json:"success"`
	Error          string    `json:"error,omitempty"`
	BodyStatus     string    `json:"body_status"`
	AuditTruncated bool      `json:"audit_truncated"`
	RequestBody    any       `json:"request_body"`
	ResponseText   string    `json:"response_text"`
	CreatedAt      string    `json:"created_at"`
}

// listAdminUserConversationRecords 返回单个对话中详细日志采集到的请求/响应正文，不读取游戏表中的聊天内容。
func (s *Server) listAdminUserConversationRecords(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	conversationID, ok := paramUUID(c, "conversation_id")
	if !ok {
		return
	}
	limit, ok := adminListLimit(c)
	if !ok {
		return
	}
	offset, ok := adminListOffset(c)
	if !ok {
		return
	}
	if !s.requireUserLogging(c, id) {
		return
	}
	rows, err := s.adminUsers.CapturedRecords(c.Request.Context(), id, conversationID, limit+1, offset)
	if err != nil {
		fail(c, err)
		return
	}
	// 先落审计再返回正文：审计写入失败时不泄露内容。
	if err := s.adminUsers.RecordAudit(c.Request.Context(), s.adminActor(c), id, "user_conversation_view", storage.JSONMap{"conversation_id": conversationID.String()}); err != nil {
		fail(c, err)
		return
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	out := make([]adminConversationRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, adminConversationRecord{
			ID: row.ID, Purpose: row.Purpose, Model: row.Model, Success: row.Success, Error: row.Error,
			BodyStatus: row.BodyStatus, AuditTruncated: row.AuditTruncated, RequestBody: row.RequestBody,
			ResponseText: row.ResponseText, CreatedAt: row.CreatedAt.Format(time.RFC3339Nano),
		})
	}
	c.JSON(http.StatusOK, gin.H{"records": out, "has_more": hasMore})
}

// enableAdminUserDetailedLogging 把详细日志范围收窄到指定用户并开启。已覆盖该用户时不改动设置，避免把“全部”意外收窄。
func (s *Server) enableAdminUserDetailedLogging(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	if refs, err := s.adminUsers.UserRefs(c.Request.Context(), []uuid.UUID{id}); err != nil {
		fail(c, err)
		return
	} else if _, found := refs[id]; !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return
	}
	actor := s.adminActor(c)
	c.Request = c.Request.WithContext(identity.WithPlayerID(c.Request.Context(), actor.String()))
	settings, err := s.system.Get(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	if !settings.CaptureDetailedLLMLog(id.String(), "") {
		previous := settings.DetailedLLMLoggingScope
		settings.DetailedLLMLoggingEnabled = true
		settings.DetailedLLMLoggingScope = "user"
		settings.DetailedLLMLoggingTargetID = &id
		if err := s.system.Save(c.Request.Context(), settings); err != nil {
			fail(c, err)
			return
		}
		if err := s.adminUsers.RecordAudit(c.Request.Context(), actor, id, "user_detailed_logging_enable", storage.JSONMap{"previous_scope": previous}); err != nil {
			fail(c, err)
			return
		}
	}
	coverage, ok := s.userLoggingCoverage(c, id)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, gin.H{"detailed_logging": coverage})
}

// userFilterValues 读取并校验用户列表的筛选/排序参数；非法取值直接返回 400。
func userFilterValues(c *gin.Context) (storage.AdminUserFilter, bool) {
	f := storage.AdminUserFilter{Search: c.Query("q"), Status: c.Query("status"), Role: c.Query("role"), Registration: c.Query("registration"), Sort: c.Query("sort")}
	var bad string
	switch {
	case len(f.Search) > 256:
		bad = "搜索内容过长"
	case f.Status != "" && f.Status != storage.UserStatusActive && f.Status != storage.UserStatusDisabled:
		bad = "无效的用户状态"
	case f.Role != "" && f.Role != storage.UserRoleUser && f.Role != storage.UserRoleAdmin && f.Role != storage.UserRoleOwner:
		bad = "无效的用户角色"
	case f.Registration != "" && f.Registration != storage.RegistrationEmail && f.Registration != storage.RegistrationLinuxDo:
		bad = "无效的注册方式"
	case f.Sort != "" && f.Sort != "created_at" && f.Sort != "last_login_at":
		bad = "排序字段必须是 created_at 或 last_login_at"
	case c.Query("order") != "" && c.Query("order") != "asc" && c.Query("order") != "desc":
		bad = "排序方向必须是 asc 或 desc"
	}
	if bad != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": bad})
		return f, false
	}
	f.Asc = c.Query("order") == "asc"
	return f, true
}

// 日志响应里引用用户时附带用户 ID（username），界面不展示 UUID。
func (s *Server) llmLogUsers(c *gin.Context, rows []storage.LLMRequestLog) map[uuid.UUID]storage.UserRef {
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		if row.UserID != nil {
			ids = append(ids, *row.UserID)
		}
	}
	return s.userRefs(c, ids)
}

func (s *Server) userRefs(c *gin.Context, ids []uuid.UUID) map[uuid.UUID]storage.UserRef {
	if s.adminUsers == nil {
		return nil
	}
	refs, err := s.adminUsers.UserRefs(c.Request.Context(), ids)
	if err != nil {
		return nil // 展示信息缺失不应让日志列表失败
	}
	return refs
}

func (s *Server) contextLogUsers(c *gin.Context, rows []storage.ContextBuildLog) map[uuid.UUID]storage.UserRef {
	if s.adminUsers == nil {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ConversationID)
	}
	refs, err := s.adminUsers.ConversationUsers(c.Request.Context(), ids)
	if err != nil {
		return nil
	}
	return refs
}

type adminAuditLogView struct {
	storage.AdminAuditLog
	Actor  *storage.UserRef `json:"actor,omitempty"`
	Target *storage.UserRef `json:"target,omitempty"`
}

func (s *Server) auditLogViews(c *gin.Context, rows []storage.AdminAuditLog) []adminAuditLogView {
	ids := make([]uuid.UUID, 0, 2*len(rows))
	for _, row := range rows {
		if row.ActorUserID != nil {
			ids = append(ids, *row.ActorUserID)
		}
		if row.TargetUserID != nil {
			ids = append(ids, *row.TargetUserID)
		}
	}
	refs := s.userRefs(c, ids)
	out := make([]adminAuditLogView, 0, len(rows))
	for _, row := range rows {
		view := adminAuditLogView{AdminAuditLog: row}
		if row.ActorUserID != nil {
			if ref, ok := refs[*row.ActorUserID]; ok {
				view.Actor = &ref
			}
		}
		if row.TargetUserID != nil {
			if ref, ok := refs[*row.TargetUserID]; ok {
				view.Target = &ref
			}
		}
		out = append(out, view)
	}
	return out
}
