package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/guidance"
	"github.com/ialtone/fatecraft/internal/realtime"
)

func (s *Server) getGuidance(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	if _, err := s.conv.GetOwned(c.Request.Context(), id); err != nil {
		fail(c, err)
		return
	}
	active, err := s.messages.CurrentGuidance(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"guidance": active})
}

func (s *Server) selectGuidance(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var body struct {
		guidance.Selection
		RequestID string `json:"request_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的行动提示选择"})
		return
	}
	_, idErr := uuid.Parse(body.ID)
	requestID, requestErr := uuid.Parse(body.RequestID)
	if idErr != nil || requestErr != nil || strings.TrimSpace(body.Key) == "" || len(body.Key) > 64 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "需要有效的 guidance_id、item_key 和 request_id"})
		return
	}
	if _, err := s.conv.EnsureActive(c.Request.Context(), id); err != nil {
		fail(c, err)
		return
	}
	// 在 BeginUserTurn 取得 durable 生成租约之后再解析选择；同一 request_id 的
	// 重试因此可以回放已消费的选项。
	if !s.hub.RunDetached(c.Request.Context(), id, func(ctx context.Context, emit realtime.EmitFunc) {
		s.hub.Gen().Generate(guidance.WithSelection(ctx, body.Selection), id, requestID, "", emit)
	}) {
		s.respondGenerationBusy(c, id, requestID)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "accepted"})
}
