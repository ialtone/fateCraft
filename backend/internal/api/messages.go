package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/chat"
	"github.com/ialtone/fatecraft/internal/realtime"
	"github.com/ialtone/fatecraft/internal/storage"
)

// maxMessageListLimit 是单次拉取消息数量的上限。
const maxMessageListLimit = 500

func (s *Server) listMessages(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var before *uuid.UUID
	if b := c.Query("before"); b != "" {
		if bid, err := uuid.Parse(b); err == nil {
			before = &bid
		}
	}
	limit := 50
	if l := c.Query("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "limit 必须是非负整数"})
			return
		}
		if n > maxMessageListLimit {
			n = maxMessageListLimit
		}
		limit = n
	}
	msgs, err := s.chat.ListDisplay(c.Request.Context(), id, before, limit)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"messages": msgs})
}

// postMessageBody 是 REST 发送消息的入参。
type postMessageBody struct {
	Content string `json:"content"`
}

// postMessage 通过 REST 触发一次生成；结果通过该对话的 WebSocket 广播。
func (s *Server) postMessage(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var body postMessageBody
	if err := c.ShouldBindJSON(&body); err != nil || body.Content == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "content 不能为空"})
		return
	}
	if _, err := s.conv.EnsureActive(c.Request.Context(), id); err != nil {
		fail(c, err)
		return
	}
	gen := s.hub.Gen()
	s.hub.RunDetached(id, func(ctx context.Context, emit realtime.EmitFunc) {
		gen.Generate(ctx, id, body.Content, emit)
	})
	c.JSON(http.StatusAccepted, gin.H{"status": "accepted"})
}

func (s *Server) regenerate(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	if _, err := s.conv.EnsureActive(c.Request.Context(), id); err != nil {
		fail(c, err)
		return
	}
	gen := s.hub.Gen()
	s.hub.RunDetached(id, func(ctx context.Context, emit realtime.EmitFunc) {
		gen.Regenerate(ctx, id, emit)
	})
	c.JSON(http.StatusAccepted, gin.H{"status": "accepted"})
}

func (s *Server) continueGen(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	if _, err := s.conv.EnsureActive(c.Request.Context(), id); err != nil {
		fail(c, err)
		return
	}
	gen := s.hub.Gen()
	s.hub.RunDetached(id, func(ctx context.Context, emit realtime.EmitFunc) {
		gen.Continue(ctx, id, emit)
	})
	c.JSON(http.StatusAccepted, gin.H{"status": "accepted"})
}

// resolveMessageConv 按展示消息 id 定位其对话，便于做归属校验。
func (s *Server) resolveMessageConv(c *gin.Context, msgID uuid.UUID) (uuid.UUID, bool) {
	disp, err := s.messages.GetDisplayByID(c.Request.Context(), msgID)
	if err != nil {
		fail(c, err)
		return uuid.Nil, false
	}
	return disp.ConversationID, true
}

func (s *Server) editMessage(c *gin.Context) {
	msgID, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	convID, ok := s.resolveMessageConv(c, msgID)
	if !ok {
		return
	}
	var in chat.EditInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	disp, err := s.chat.EditDisplay(c.Request.Context(), convID, msgID, in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, disp)
}

// editAndRegenerate 编辑用户消息后，删除所有后续消息并重新生成（天道回溯）。
func (s *Server) editAndRegenerate(c *gin.Context) {
	msgID, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	convID, ok := s.resolveMessageConv(c, msgID)
	if !ok {
		return
	}

	// 确保对话处于活跃状态
	if _, err := s.conv.EnsureActive(c.Request.Context(), convID); err != nil {
		fail(c, err)
		return
	}

	var in chat.EditInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if in.Content == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "content 不能为空"})
		return
	}

	// 天道回溯原子化：先扣减回溯次数，扣减失败（次数不足）直接返回，
	// 成功后再执行编辑 + 重生成；前端无需先调 /account/rewind/use。
	playerID := s.identity.CurrentPlayerID(c.Request.Context())
	if err := s.account.UseRewind(playerID); err != nil {
		if errors.Is(err, storage.ErrNoRewinds) {
			c.JSON(http.StatusConflict, gin.H{"error": "没有可用的天道回溯次数"})
			return
		}
		fail(c, err)
		return
	}

	// 编辑用户消息。失败时补偿性退还已扣减的回溯次数，避免"扣了但没回溯成"。
	disp, err := s.chat.EditDisplay(c.Request.Context(), convID, msgID, in)
	if err != nil {
		_ = s.account.AddRewind(playerID, 1)
		fail(c, err)
		return
	}

	// 删除该消息之后的所有消息。失败同样退还次数。
	if err := s.messages.DeleteDisplayMessagesAfter(c.Request.Context(), convID, msgID); err != nil {
		_ = s.account.AddRewind(playerID, 1)
		fail(c, err)
		return
	}

	// 记录本局已用回溯次数（尽力而为，不阻断主流程）
	_ = s.conv.IncrementRewindsUsed(c.Request.Context(), convID)

	// 触发重新生成：走不再 AppendUser 的路径，避免上下文出现重复用户消息。
	gen := s.hub.Gen()
	s.hub.RunDetached(convID, func(ctx context.Context, emit realtime.EmitFunc) {
		gen.GenerateAfterEdit(ctx, convID, emit)
	})

	c.JSON(http.StatusAccepted, gin.H{"status": "accepted", "message": disp})
}

func (s *Server) deleteMessage(c *gin.Context) {
	msgID, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	convID, ok := s.resolveMessageConv(c, msgID)
	if !ok {
		return
	}
	if err := s.chat.DeleteDisplay(c.Request.Context(), convID, msgID); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}
