package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (s *Server) lastContext(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	log, err := s.ctxLogs.Last(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, log)
}

// getContext 按构建日志 id 读取上下文，并做归属校验。
//
// 前端调用的是 GET /context/:cid（见 frontend/src/api/index.ts），为不破坏该契约
// 保留原路由与响应格式；归属校验改在 handler 内完成：先取日志 → 反查其对话 →
// 比对当前玩家 id。不属于当前玩家时返回 404（复用 storage.ErrNotFound），
// 避免泄漏日志/对话是否存在。
func (s *Server) getContext(c *gin.Context) {
	id, ok := paramUUID(c, "cid")
	if !ok {
		return
	}
	log, err := s.ctxLogs.Get(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	// 反查该日志所属对话并校验归属（EnsureActive 已内含 playerID 校验，
	// 归属不符返回 ErrNotFound）。日志不绑定活跃状态，故用 conv.Get + 手动比对，
	// 避免对已结束/归档对话的上下文查询被 active 校验误拒。
	if _, err := s.conv.GetOwned(c.Request.Context(), log.ConversationID); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, log)
}
