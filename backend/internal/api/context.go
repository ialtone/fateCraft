package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/storage"
)

// playerContextBuildSummary 只包含玩家能理解的运行摘要。Prompt 分段 key、
// 消息 ID 和世界书内部 ID 仅在管理员诊断接口中返回。
type playerContextBuildSummary struct {
	ID                       uuid.UUID `json:"id"`
	ConversationID           uuid.UUID `json:"conversation_id"`
	Model                    string    `json:"model"`
	MaxContextTokens         int       `json:"max_context_tokens"`
	ReservedResponseTokens   int       `json:"reserved_response_tokens"`
	UsedPromptTokens         int       `json:"used_prompt_tokens"`
	IncludedMessageCount     int       `json:"included_message_count"`
	ActivatedWorldEntryCount int       `json:"activated_world_entry_count"`
	MemoryLag                bool      `json:"memory_lag"`
	OmittedTurnFrom          *int64    `json:"omitted_turn_from,omitempty"`
	OmittedTurnUntil         *int64    `json:"omitted_turn_until,omitempty"`
	CreatedAt                time.Time `json:"created_at"`
}

func summarizePlayerContextBuild(log *storage.ContextBuildLog) playerContextBuildSummary {
	return playerContextBuildSummary{
		ID: log.ID, ConversationID: log.ConversationID, Model: log.Model,
		MaxContextTokens: log.MaxContextTokens, ReservedResponseTokens: log.ReservedResponseTokens,
		UsedPromptTokens: log.UsedPromptTokens, IncludedMessageCount: len(log.IncludedMessageIDs),
		ActivatedWorldEntryCount: len(log.ActivatedWorldEntryIDs), MemoryLag: log.MemoryLag,
		OmittedTurnFrom: log.OmittedTurnFrom, OmittedTurnUntil: log.OmittedTurnUntil, CreatedAt: log.CreatedAt,
	}
}

func (s *Server) lastContext(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	if _, err := s.conv.GetOwned(c.Request.Context(), id); err != nil {
		fail(c, err)
		return
	}
	log, err := s.ctxLogs.Last(c.Request.Context(), id)
	respondLastContext(c, log, err)
}

func respondLastContext(c *gin.Context, log *storage.ContextBuildLog, err error) {
	if errors.Is(err, storage.ErrNotFound) {
		c.Status(http.StatusNoContent)
		return
	}
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, summarizePlayerContextBuild(log))
}

// getContext 按构建日志 id 读取上下文，并做归属校验。
//
// 前端调用的是 GET /context/:cid（见 frontend/src/api/index.ts），保留原路由；
// 归属校验在 handler 内完成：先取日志 → 反查其对话 →
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
	c.JSON(http.StatusOK, summarizePlayerContextBuild(log))
}
