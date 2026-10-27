package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/storage"
)

func logQuery(c *gin.Context, kind string) (storage.LogQuery, bool) {
	q := storage.LogQuery{Limit: 50, Cursor: c.Query("cursor")}
	invalid := func() (storage.LogQuery, bool) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "日志筛选或游标无效"})
		return q, false
	}
	if raw := c.Query("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 200 {
			return invalid()
		}
		q.Limit = value
	}
	for name, target := range map[string]**time.Time{"from": &q.From, "until": &q.Until} {
		if raw := c.Query(name); raw != "" {
			value, err := time.Parse(time.RFC3339Nano, raw)
			if err != nil {
				return invalid()
			}
			value = value.UTC()
			*target = &value
		}
	}
	ids := map[string]**uuid.UUID{}
	if kind == "audit" {
		ids["actor_id"] = &q.ActorID
		q.Action = c.Query("action")
		q.UserRole = c.Query("user_role")
		q.Result = c.Query("result")
	} else {
		ids["conversation_id"] = &q.ConversationID
		ids["attempt_id"] = &q.AttemptID
	}
	if kind != "audit" {
		q.Model = c.Query("model")
	}
	if kind == "llm" {
		ids["trace_id"] = &q.TraceID
		q.Purpose = c.Query("purpose")
		if raw := c.Query("success"); raw != "" {
			value, err := strconv.ParseBool(raw)
			if err != nil {
				return invalid()
			}
			q.Success = &value
		}
	}
	for name, target := range ids {
		if raw := c.Query(name); raw != "" {
			value, err := uuid.Parse(raw)
			if err != nil {
				return invalid()
			}
			*target = &value
		}
	}
	if q.From != nil && q.Until != nil && !q.From.Before(*q.Until) {
		return invalid()
	}
	return q, true
}

// applyLogUser 把 ?user=<用户 ID（username）> 解析为用户主键；用户不存在时已写出 404。
func (s *Server) applyLogUser(c *gin.Context, q *storage.LogQuery) bool {
	name := strings.TrimSpace(c.Query("user"))
	if name == "" {
		return true
	}
	if s.adminUsers == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "用户管理服务未配置"})
		return false
	}
	id, err := s.adminUsers.IDByUsername(c.Request.Context(), name)
	if errors.Is(err, storage.ErrNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
		return false
	}
	if err != nil {
		fail(c, err)
		return false
	}
	q.UserID = &id
	return true
}

func (s *Server) diagnosticStatus(c *gin.Context) {
	if s.diagnostics == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "诊断状态未配置"})
		return
	}
	c.JSON(http.StatusOK, s.diagnostics())
}
