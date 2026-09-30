package api

import (
	"net/http"
	"strconv"
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
	} else {
		ids["conversation_id"] = &q.ConversationID
		ids["attempt_id"] = &q.AttemptID
	}
	if kind == "llm" {
		ids["trace_id"] = &q.TraceID
		q.Model = c.Query("model")
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

func (s *Server) diagnosticStatus(c *gin.Context) {
	if s.diagnostics == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "诊断状态未配置"})
		return
	}
	c.JSON(http.StatusOK, s.diagnostics())
}
