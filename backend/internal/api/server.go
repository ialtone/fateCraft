package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	authservice "github.com/ialtone/fatecraft/internal/auth"
	"github.com/ialtone/fatecraft/internal/challenge"
	"github.com/ialtone/fatecraft/internal/challengeimport"
	"github.com/ialtone/fatecraft/internal/chat"
	"github.com/ialtone/fatecraft/internal/conversation"
	"github.com/ialtone/fatecraft/internal/identity"
	"github.com/ialtone/fatecraft/internal/llm"
	"github.com/ialtone/fatecraft/internal/memory"
	"github.com/ialtone/fatecraft/internal/realtime"
	"github.com/ialtone/fatecraft/internal/state"
	"github.com/ialtone/fatecraft/internal/storage"
	"github.com/ialtone/fatecraft/internal/tokenizer"
)

type builtinChallengeImporter interface {
	ListBuiltins(context.Context) ([]challengeimport.BuiltinInfo, error)
	ImportBuiltin(context.Context, string) (*challengeimport.ImportResult, error)
}

// Server 聚合全部服务，承载 HTTP 与 WebSocket 处理。
type Server struct {
	conv         *conversation.Service
	chat         *chat.Service
	state        *state.Service
	challenges   *challenge.Service
	builtins     builtinChallengeImporter
	memory       *memory.Service
	settings     *storage.SettingsRepo
	system       *storage.SystemSettingsRepo
	ctxLogs      *storage.ContextRepo
	messages     *storage.MessageRepo
	account      *storage.AccountRepository
	hub          *realtime.Hub
	llmClient    *llm.Client
	counter      tokenizer.Counter
	apiKeyEnv    string
	identity     identity.Provider // 当前玩家身份来源（由认证中间件写入 context）
	auth         *authservice.Service
	cookieSecure bool
}

// Deps 是构造 Server 所需的依赖集合。
type Deps struct {
	Conv         *conversation.Service
	Chat         *chat.Service
	State        *state.Service
	Challenges   *challenge.Service
	Builtins     builtinChallengeImporter
	Memory       *memory.Service
	Settings     *storage.SettingsRepo
	System       *storage.SystemSettingsRepo
	CtxLogs      *storage.ContextRepo
	Messages     *storage.MessageRepo
	Account      *storage.AccountRepository
	Hub          *realtime.Hub
	LLMClient    *llm.Client
	Counter      tokenizer.Counter
	APIKeyEnv    string
	Identity     identity.Provider // 当前玩家身份来源（由认证中间件写入 context）
	Auth         *authservice.Service
	CookieSecure bool
}

func NewServer(d Deps) *Server {
	return &Server{
		conv: d.Conv, chat: d.Chat, state: d.State, challenges: d.Challenges, builtins: d.Builtins,
		memory:   d.Memory,
		settings: d.Settings, system: d.System, ctxLogs: d.CtxLogs, messages: d.Messages, account: d.Account, hub: d.Hub,
		llmClient: d.LLMClient, counter: d.Counter, apiKeyEnv: d.APIKeyEnv, identity: d.Identity,
		auth: d.Auth, cookieSecure: d.CookieSecure,
	}
}

// Router 注册全部路由。
func (s *Server) Router(r *gin.Engine) {
	api := r.Group("/api")

	api.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	api.GET("/auth/config", s.authConfig)
	api.GET("/auth/linuxdo/start", s.startLinuxDO)
	api.GET("/auth/linuxdo/callback", s.completeLinuxDO)
	api.POST("/auth/email-code", s.sendRegistrationCode)
	api.POST("/auth/register", s.registerEmail)
	api.POST("/auth/login", s.loginEmail)
	api.POST("/auth/logout", s.logout)
	api.GET("/auth/me", s.requireAuth, s.me)

	protected := api.Group("")
	protected.Use(s.requireAuth)
	protected.GET("/challenges", s.listChallenges)
	protected.GET("/challenges/:slug", s.getChallenge)

	// 对话
	protected.POST("/conversations", s.createConversation)
	protected.GET("/conversations", s.listConversations)
	protected.GET("/conversations/:id", s.getConversation)
	protected.PATCH("/conversations/:id", s.updateConversation)
	protected.DELETE("/conversations/:id", s.deleteConversation)
	protected.POST("/conversations/:id/end", s.endConversation)
	protected.POST("/conversations/:id/archive", s.archiveConversation)
	protected.POST("/conversations/:id/restore", s.restoreConversation)

	// 消息
	protected.GET("/conversations/:id/messages", s.listMessages)
	protected.POST("/conversations/:id/messages", s.postMessage)
	protected.POST("/conversations/:id/regenerate", s.regenerate)
	protected.POST("/conversations/:id/continue", s.continueGen)
	protected.PATCH("/messages/:id", s.editMessage)
	protected.POST("/messages/:id/edit-and-regenerate", s.editAndRegenerate)
	protected.DELETE("/messages/:id", s.deleteMessage)

	// 状态
	protected.GET("/conversations/:id/state", s.getState)
	protected.PATCH("/conversations/:id/state", s.requireAdmin, s.patchState)
	protected.GET("/conversations/:id/state/revisions", s.stateRevisions)

	// 上下文
	protected.GET("/conversations/:id/context/last", s.lastContext)
	protected.GET("/context/:cid", s.getContext)

	// 摘要
	protected.GET("/conversations/:id/summaries", s.listSummaries)
	protected.POST("/conversations/:id/summaries", s.generateSummary)
	protected.PATCH("/summaries/:id", s.editSummary)

	// 账号（天道回溯）
	protected.GET("/account", s.getAccount)
	protected.POST("/account/rewind/use", s.useRewind)

	// 设置
	protected.GET("/settings/llm", s.getLLMSettings)
	protected.PATCH("/settings/llm", s.updateLLMSettings)
	protected.POST("/settings/fetch-models", s.fetchModels)

	// 管理员全局玩法设置
	protected.GET("/admin/roll-settings", s.requireAdmin, s.getAdminRollSettings)
	protected.PATCH("/admin/roll-settings", s.requireAdmin, s.updateAdminRollSettings)
	protected.GET("/admin/auth-settings", s.requireAdmin, s.getAdminAuthSettings)
	protected.PATCH("/admin/auth-settings", s.requireAdmin, s.updateAdminAuthSettings)
	protected.POST("/admin/auth-settings/test-email", s.requireAdmin, s.testAdminEmail)
	protected.GET("/admin/invitation-codes", s.requireAdmin, s.listAdminInvitations)
	protected.POST("/admin/invitation-codes", s.requireAdmin, s.createAdminInvitation)
	protected.PATCH("/admin/invitation-codes/:id", s.requireAdmin, s.updateAdminInvitation)
	protected.GET("/admin/builtin-challenges", s.requireAdmin, s.listAdminBuiltins)
	protected.POST("/admin/builtin-challenges/:slug/import", s.requireAdmin, s.importAdminBuiltin)
	protected.GET("/admin/challenges", s.requireAdmin, s.listAdminChallenges)
	protected.POST("/admin/challenges", s.requireAdmin, s.createAdminChallenge)
	protected.POST("/admin/challenges/:id/versions", s.requireAdmin, s.createAdminChallengeVersion)
	protected.GET("/admin/challenge-versions/:id", s.requireAdmin, s.getAdminChallengeVersion)
	protected.PUT("/admin/challenge-versions/:id", s.requireAdmin, s.saveAdminChallengeVersion)
	protected.POST("/admin/challenge-versions/:id/publish", s.requireAdmin, s.publishAdminChallengeVersion)

	// WebSocket
	r.GET("/ws/conversations/:id", s.requireAuth, s.serveWS)
}

// --- 通用辅助 ---

func paramUUID(c *gin.Context, key string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(key))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 id: " + key})
		return uuid.Nil, false
	}
	return id, true
}

// paramSlug 读取一个模板 slug 形式的路径参数（非空即可）。
func paramSlug(c *gin.Context, key string) (string, bool) {
	id := c.Param(key)
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 id: " + key})
		return "", false
	}
	return id, true
}

// paramUUIDFromString 解析一个字符串形式的 uuid（用于请求体字段）。
func paramUUIDFromString(c *gin.Context, raw string) (uuid.UUID, bool) {
	id, err := uuid.Parse(raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 conversation_id"})
		return uuid.Nil, false
	}
	return id, true
}

// fail 按错误类型返回合适的 HTTP 状态码。
func fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, storage.ErrNotFound), errors.Is(err, challenge.ErrNotPublished), errors.Is(err, challengeimport.ErrBuiltinNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "资源不存在"})
	case challengeFailureStatus(err):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, conversation.ErrNotActive), errors.Is(err, challenge.ErrConversationEnded):
		c.JSON(http.StatusConflict, gin.H{"error": "对话未处于 active 状态"})
	case errors.Is(err, storage.ErrAlreadySettled), errors.Is(err, storage.ErrPublishedVersion), errors.Is(err, challengeimport.ErrVersionConflict):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}
