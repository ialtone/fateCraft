package api

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"

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
		bid, err := uuid.Parse(b)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "before 必须是有效 UUID"})
			return
		}
		before = &bid
	}
	limit := 50
	if l := c.Query("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n < 1 || n > maxMessageListLimit {
			c.JSON(http.StatusBadRequest, gin.H{"error": "limit 必须是 1 到 500 的整数"})
			return
		}
		limit = n
	}
	if _, err := s.conv.GetOwned(c.Request.Context(), id); err != nil {
		fail(c, err)
		return
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
	Content   string `json:"content"`
	RequestID string `json:"request_id"`
}

type generationActionBody struct {
	RequestID string `json:"request_id"`
}

func requestIDFromString(raw string) (uuid.UUID, error) {
	if strings.TrimSpace(raw) == "" {
		return uuid.New(), nil
	}
	return uuid.Parse(raw)
}

// respondGenerationBusy 在 Hub 判忙时区分两种情况：同一 request_id 的传输层重试
// 命中已登记的 durable 生成，按回放返回 202，结果仍经 WebSocket 推送；
// 只有真正的新请求才返回 409。
func (s *Server) respondGenerationBusy(c *gin.Context, convID, requestID uuid.UUID) {
	exists, err := s.conv.GenerationRequestExists(c.Request.Context(), convID, requestID)
	if err != nil {
		fail(c, err)
		return
	}
	if exists {
		c.JSON(http.StatusAccepted, gin.H{"status": "replayed"})
		return
	}
	c.JSON(http.StatusConflict, gin.H{"error": "当前对话正在生成中，请稍候"})
}

// requireLLMRoute 校验对话固化路由与当前有效路由：不匹配时返回 409，
// 不创建 run/turn/attempt、不扣额度。通过返回 true。
func (s *Server) requireLLMRoute(c *gin.Context, convID uuid.UUID) bool {
	conv, err := s.conv.GetOwned(c.Request.Context(), convID)
	if err != nil {
		fail(c, err)
		return false
	}
	effective := storage.LLMRouteServer
	if s.llmSettings != nil {
		effective, err = s.llmSettings.EffectiveRoute(c.Request.Context(), conv.PlayerID)
		if err != nil {
			fail(c, err)
			return false
		}
	}
	if err := storage.CheckLLMRoute(conv.LLMRoute, effective); err != nil {
		c.JSON(http.StatusConflict, gin.H{
			"error":           storage.LLMRouteMismatchMessage(conv.LLMRoute, effective),
			"code":            "llm_route_mismatch",
			"llm_route":       conv.LLMRoute,
			"effective_route": effective,
		})
		return false
	}
	return true
}

// postMessage 通过 REST 触发一次生成；结果通过该对话的 WebSocket 广播。
func (s *Server) requireGenerationActive(c *gin.Context, id, requestID uuid.UUID) bool {
	if _, err := s.conv.GetOwned(c.Request.Context(), id); err != nil {
		fail(c, err)
		return false
	}
	exists, err := s.conv.GenerationRequestExists(c.Request.Context(), id, requestID)
	if err != nil {
		fail(c, err)
		return false
	}
	if exists {
		return true
	}
	if _, err := s.conv.EnsureActive(c.Request.Context(), id); err != nil {
		fail(c, err)
		return false
	}
	return true
}

func (s *Server) postMessage(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var body postMessageBody
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.Content) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "content 不能为空"})
		return
	}
	if !s.requireLLMRoute(c, id) {
		return
	}
	requestID, err := requestIDFromString(body.RequestID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request_id 必须是有效 UUID"})
		return
	}
	if !s.requireGenerationActive(c, id, requestID) {
		return
	}
	gen := s.hub.Gen()
	// REST 入口无法判断队列状态，直接尝试启动；对话忙时按 request_id 区分回放与冲突。
	if !s.hub.RunDetached(c.Request.Context(), id, func(ctx context.Context, emit realtime.EmitFunc) {
		gen.Generate(ctx, id, requestID, body.Content, emit)
	}) {
		s.respondGenerationBusy(c, id, requestID)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "accepted"})
}

func (s *Server) regenerate(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	if !s.requireLLMRoute(c, id) {
		return
	}
	var body generationActionBody
	if err := c.ShouldBindJSON(&body); err != nil && err != io.EOF {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	requestID, err := requestIDFromString(body.RequestID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request_id 必须是有效 UUID"})
		return
	}
	if !s.requireGenerationActive(c, id, requestID) {
		return
	}
	gen := s.hub.Gen()
	if !s.hub.RunDetached(c.Request.Context(), id, func(ctx context.Context, emit realtime.EmitFunc) {
		gen.Regenerate(ctx, id, requestID, emit)
	}) {
		s.respondGenerationBusy(c, id, requestID)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "accepted"})
}

func (s *Server) continueGen(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	if !s.requireLLMRoute(c, id) {
		return
	}
	var body generationActionBody
	if err := c.ShouldBindJSON(&body); err != nil && err != io.EOF {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	requestID, err := requestIDFromString(body.RequestID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request_id 必须是有效 UUID"})
		return
	}
	if !s.requireGenerationActive(c, id, requestID) {
		return
	}
	gen := s.hub.Gen()
	if !s.hub.RunDetached(c.Request.Context(), id, func(ctx context.Context, emit realtime.EmitFunc) {
		gen.Continue(ctx, id, requestID, emit)
	}) {
		s.respondGenerationBusy(c, id, requestID)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "accepted"})
}

// resolveMessageConv 按展示消息 id 定位其对话，便于做归属校验。
func (s *Server) resolveMessageConv(c *gin.Context, msgID uuid.UUID) (uuid.UUID, bool) {
	disp, err := s.messages.GetDisplayByID(c.Request.Context(), msgID)
	if err != nil {
		fail(c, err)
		return uuid.Nil, false
	}
	if _, err := s.conv.GetOwned(c.Request.Context(), disp.ConversationID); err != nil {
		fail(c, err)
		return uuid.Nil, false
	}
	return disp.ConversationID, true
}

// editAndRegenerate 编辑用户消息后，删除所有后续消息并重新生成。
func (s *Server) editAndRegenerate(c *gin.Context) {
	msgID, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	convID, ok := s.resolveMessageConv(c, msgID)
	if !ok {
		return
	}
	target, err := s.messages.GetDisplayByID(c.Request.Context(), msgID)
	if err != nil {
		fail(c, err)
		return
	}
	if target.Type != "user" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "只能改写自己的输入消息"})
		return
	}

	// 确保对话处于活跃状态
	if _, err := s.conv.EnsureActive(c.Request.Context(), convID); err != nil {
		fail(c, err)
		return
	}
	if !s.requireLLMRoute(c, convID) {
		return
	}
	var in chat.EditInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if in.Content == nil || strings.TrimSpace(*in.Content) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "content 不能为空"})
		return
	}
	requestID, err := requestIDFromString(in.RequestID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request_id 必须是有效 UUID"})
		return
	}
	// 奖励扣除、分支替换、替换 attempt 创建和消息绑定由后台协调器作为一个操作完成。
	gen := s.hub.Gen()
	if !s.hub.RunDetached(c.Request.Context(), convID, func(ctx context.Context, emit realtime.EmitFunc) {
		gen.Rewrite(ctx, convID, requestID, msgID, strings.TrimSpace(*in.Content), emit)
	}) {
		s.respondGenerationBusy(c, convID, requestID)
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"status": "accepted"})
}
