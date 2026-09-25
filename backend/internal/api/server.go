package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	authservice "github.com/ialtone/fatecraft/internal/auth"
	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/challenge"
	"github.com/ialtone/fatecraft/internal/challengeimport"
	"github.com/ialtone/fatecraft/internal/chat"
	"github.com/ialtone/fatecraft/internal/conversation"
	"github.com/ialtone/fatecraft/internal/identity"
	"github.com/ialtone/fatecraft/internal/inquiry"
	"github.com/ialtone/fatecraft/internal/llm"
	"github.com/ialtone/fatecraft/internal/llmsettings"
	"github.com/ialtone/fatecraft/internal/memory"
	"github.com/ialtone/fatecraft/internal/modelcapability"
	"github.com/ialtone/fatecraft/internal/realtime"
	shareapp "github.com/ialtone/fatecraft/internal/share"
	"github.com/ialtone/fatecraft/internal/state"
	"github.com/ialtone/fatecraft/internal/storage"
	"github.com/ialtone/fatecraft/internal/tokenizer"
)

type builtinChallengeImporter interface {
	ListBuiltins(context.Context) ([]challengeimport.BuiltinInfo, error)
	ImportBuiltin(context.Context, string) (*challengeimport.ImportResult, error)
	OverwriteBuiltin(context.Context, string) (*challengeimport.ImportResult, error)
}

// Server 聚合全部服务，承载 HTTP 与 WebSocket 处理。
type Server struct {
	conv           *conversation.Service
	campaigns      *campaign.Service
	chat           *chat.Service
	state          *state.Service
	challenges     *challenge.Service
	builtins       builtinChallengeImporter
	memory         *memory.Service
	inquiries      *inquiry.Service
	settings       *storage.SettingsRepo
	llmSettings    *llmsettings.Service
	system         *storage.SystemSettingsRepo
	ctxLogs        *storage.ContextRepo
	messages       *storage.MessageRepo
	agents         *storage.AgentRepo
	turns          *storage.TurnRepo
	rewards        *storage.RewardRepository
	llmRequestLogs *storage.LLMRequestLogRepo
	hub            *realtime.Hub
	llmClient      *llm.Client
	capabilities   *modelcapability.Service
	shares         *shareapp.Service
	counter        tokenizer.Counter
	identity       identity.Provider // 当前玩家身份来源（由认证中间件写入 context）
	auth           *authservice.Service
	cookieSecure   bool
}

// Deps 是构造 Server 所需的依赖集合。
type Deps struct {
	Conv           *conversation.Service
	Campaigns      *campaign.Service
	Chat           *chat.Service
	State          *state.Service
	Challenges     *challenge.Service
	Builtins       builtinChallengeImporter
	Memory         *memory.Service
	Inquiries      *inquiry.Service
	Settings       *storage.SettingsRepo
	LLMSettings    *llmsettings.Service
	System         *storage.SystemSettingsRepo
	CtxLogs        *storage.ContextRepo
	Messages       *storage.MessageRepo
	Agents         *storage.AgentRepo
	Turns          *storage.TurnRepo
	Rewards        *storage.RewardRepository
	LLMRequestLogs *storage.LLMRequestLogRepo
	Hub            *realtime.Hub
	LLMClient      *llm.Client
	Capabilities   *modelcapability.Service
	Shares         *shareapp.Service
	Counter        tokenizer.Counter
	Identity       identity.Provider // 当前玩家身份来源（由认证中间件写入 context）
	Auth           *authservice.Service
	CookieSecure   bool
}

type BodyLimitOverride struct {
	Method   string
	Path     string
	MaxBytes int64
}

// RequestBodyLimit applies a default upper bound before handlers bind JSON.
// Exact method/full-path overrides support intentionally larger documents
// without weakening the limit for every ordinary API request.
func RequestBodyLimit(maxBytes int64, overrides ...BodyLimitOverride) gin.HandlerFunc {
	if maxBytes <= 0 {
		maxBytes = 4 << 20
	}
	return func(c *gin.Context) {
		limit := maxBytes
		for _, override := range overrides {
			if c.Request.Method == override.Method && c.FullPath() == override.Path && override.MaxBytes > 0 {
				limit = override.MaxBytes
				break
			}
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}

func NewServer(d Deps) *Server {
	return &Server{
		conv: d.Conv, campaigns: d.Campaigns, chat: d.Chat, state: d.State, challenges: d.Challenges, builtins: d.Builtins,
		memory: d.Memory, inquiries: d.Inquiries,
		settings: d.Settings, llmSettings: d.LLMSettings, system: d.System, ctxLogs: d.CtxLogs, messages: d.Messages, agents: d.Agents, rewards: d.Rewards,
		llmRequestLogs: d.LLMRequestLogs, hub: d.Hub, turns: d.Turns,
		llmClient: d.LLMClient, capabilities: d.Capabilities, shares: d.Shares, counter: d.Counter, identity: d.Identity,
		auth: d.Auth, cookieSecure: d.CookieSecure,
	}
}

// Router 注册全部路由。
func (s *Server) Router(r *gin.Engine) {
	r.GET("/s/:token", s.getConversationShareHTML)
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
	api.GET("/setup/status", s.getSetupStatus)
	api.POST("/setup/initialize", s.initializeSetup)
	api.GET("/public/conversation-shares/:token", s.getPublicConversationShare)
	api.POST("/public/conversation-shares/:token/reports", s.reportPublicConversationShare)

	protected := api.Group("")
	protected.Use(s.requireAuth)
	protected.GET("/auth/identities", s.listAuthIdentities)
	protected.GET("/auth/linuxdo/bind/start", s.startLinuxDOBind)
	protected.DELETE("/auth/identities/:id", s.unbindAuthIdentity)
	protected.GET("/challenges", s.listChallenges)
	protected.GET("/challenges/:slug", s.getChallenge)

	// 长期存档与持续对话
	protected.POST("/campaigns", s.createCampaign)
	protected.GET("/campaigns", s.listCampaigns)
	protected.GET("/campaigns/:id", s.getCampaign)
	protected.GET("/campaigns/:id/snapshot", s.getCampaignSnapshot)
	protected.GET("/campaigns/:id/events", s.listCampaignEvents)
	protected.GET("/campaigns/:id/conversations", s.listCampaignConversations)
	protected.POST("/campaigns/:id/conversations", s.createCampaignConversation)
	protected.POST("/campaigns/:id/actions", s.executeCampaignAction)
	protected.POST("/campaigns/:id/abandon", s.abandonCampaign)

	// 对话
	protected.POST("/conversations", s.createConversation)
	protected.GET("/conversations", s.listConversations)
	protected.GET("/conversations/:id", s.getConversation)
	protected.POST("/conversations/:id/fork", s.forkConversation)
	protected.PATCH("/conversations/:id", s.updateConversation)
	protected.DELETE("/conversations/:id", s.deleteConversation)
	protected.POST("/conversations/:id/end", s.endConversation)
	protected.POST("/conversations/:id/archive", s.archiveConversation)
	protected.POST("/conversations/:id/restore", s.restoreConversation)
	protected.GET("/conversations/:id/inquiries", s.listInquiries)
	protected.POST("/conversations/:id/inquiries", s.askInquiry)
	protected.POST("/conversations/:id/inquiries/cancel", s.cancelInquiry)
	protected.DELETE("/conversations/:id/inquiries", s.clearInquiries)
	protected.GET("/conversations/:id/activity", s.getConversationActivity)
	protected.GET("/conversations/:id/runtime-inspector", s.getRuntimeInspector)
	protected.POST("/conversations/:id/shares", s.createConversationShare)
	protected.GET("/conversation-shares", s.listOwnedConversationShares)
	protected.DELETE("/conversation-shares/:id", s.revokeConversationShare)
	protected.POST("/conversation-shares/:id/replace", s.replaceConversationShare)

	// 消息
	protected.GET("/conversations/:id/messages", s.listMessages)
	protected.POST("/conversations/:id/messages", s.postMessage)
	protected.GET("/conversations/:id/guidance", s.getGuidance)
	protected.POST("/conversations/:id/guidance/select", s.selectGuidance)
	protected.POST("/conversations/:id/regenerate", s.regenerate)
	protected.POST("/conversations/:id/continue", s.continueGen)
	protected.POST("/messages/:id/edit-and-regenerate", s.editAndRegenerate)

	// 状态
	protected.GET("/conversations/:id/state", s.getState)
	protected.GET("/conversations/:id/runtime", s.getConversationRuntime)
	protected.PATCH("/conversations/:id/state", s.requireAdmin, s.patchState)
	protected.GET("/conversations/:id/state/revisions", s.stateRevisions)

	// 上下文
	protected.GET("/conversations/:id/context/last", s.lastContext)
	protected.GET("/context/:cid", s.getContext)

	// 玩家奖励与不可变账本
	protected.GET("/me/rewards", s.listRewards)
	protected.GET("/me/reward-ledger", s.listRewardLedger)

	// 设置
	protected.GET("/settings/llm", s.getLLMSettings)
	protected.GET("/settings/llm/providers", s.listLLMProviderProfiles)
	protected.PATCH("/settings/llm", s.updateLLMSettings)
	protected.POST("/settings/fetch-models", s.fetchModels)
	protected.POST("/settings/llm/capabilities/check", s.checkLLMCapabilities)

	// 管理员全局玩法设置
	protected.GET("/admin/system-settings", s.requireAdmin, s.getAdminSystemSettings)
	protected.PATCH("/admin/system-settings", s.requireAdmin, s.updateAdminSystemSettings)
	protected.POST("/admin/system-settings/fetch-models", s.requireOwner, s.fetchAdminPublicModels)
	protected.GET("/admin/llm-request-logs", s.requireAdmin, s.listAdminLLMRequestLogs)
	protected.GET("/admin/llm-request-logs/:id", s.requireAdmin, s.getAdminLLMRequestLog)
	protected.GET("/admin/context-build-logs", s.requireAdmin, s.listAdminContextBuildLogs)
	protected.GET("/admin/context-build-logs/:id", s.requireAdmin, s.getAdminContextBuildLog)
	protected.GET("/admin/conversation-shares", s.requireAdmin, s.listAdminConversationShares)
	protected.POST("/admin/conversation-shares/:id/moderate", s.requireAdmin, s.moderateAdminConversationShare)
	protected.GET("/admin/share-reports", s.requireAdmin, s.listAdminShareReports)
	protected.POST("/admin/share-reports/:id/resolve", s.requireAdmin, s.resolveAdminShareReport)
	protected.GET("/admin/auth-settings", s.requireAdmin, s.getAdminAuthSettings)
	protected.PATCH("/admin/auth-settings", s.requireOwner, s.updateAdminAuthSettings)
	protected.POST("/admin/auth-settings/test-email", s.requireOwner, s.testAdminEmail)
	protected.GET("/admin/invitation-codes", s.requireAdmin, s.listAdminInvitations)
	protected.POST("/admin/invitation-codes", s.requireAdmin, s.createAdminInvitation)
	protected.PATCH("/admin/invitation-codes/:id", s.requireAdmin, s.updateAdminInvitation)
	protected.GET("/admin/users", s.requireAdmin, s.listAdminUsers)
	protected.GET("/admin/auth-identities", s.requireAdmin, s.listAdminAuthIdentities)
	protected.PATCH("/admin/users/:id/role", s.requireOwner, s.updateAdminUserRole)
	protected.PATCH("/admin/users/:id/status", s.requireOwner, s.updateAdminUserStatus)
	protected.GET("/admin/audit-logs", s.requireAdmin, s.listAdminAuditLogs)
	protected.GET("/admin/builtin-challenges", s.requireAdmin, s.listAdminBuiltins)
	protected.POST("/admin/builtin-challenges/:slug/import", s.requireAdmin, s.importAdminBuiltin)
	protected.GET("/admin/gameplay-modules", s.requireAdmin, s.listAdminGameplayModules)
	protected.GET("/admin/reward-definitions", s.requireAdmin, s.listAdminRewardDefinitions)
	protected.POST("/admin/reward-definitions", s.requireAdmin, s.createAdminRewardDefinition)
	protected.PATCH("/admin/reward-definitions/:id", s.requireAdmin, s.updateAdminRewardDefinition)
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
	case errors.Is(err, conversation.ErrNotActive), errors.Is(err, storage.ErrConversationNotActive), errors.Is(err, challenge.ErrConversationEnded):
		c.JSON(http.StatusConflict, gin.H{"error": "对话未处于 active 状态"})
	case errors.Is(err, storage.ErrGenerationInProgress):
		c.JSON(http.StatusConflict, gin.H{"error": "当前对话正在生成中，请稍候"})
	case errors.Is(err, storage.ErrShareBusy), errors.Is(err, storage.ErrShareLimit), errors.Is(err, storage.ErrShareIdempotencyConflict):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, storage.ErrShareEmpty), errors.Is(err, storage.ErrShareRange), errors.Is(err, shareapp.ErrInvalidInput), errors.Is(err, shareapp.ErrTooLarge), errors.Is(err, shareapp.ErrUnsupportedMessage):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, shareapp.ErrSigningUnavailable):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
	case errors.Is(err, storage.ErrTurnInProgress), errors.Is(err, storage.ErrContinueNotAllowed), errors.Is(err, storage.ErrRegenerateNotAllowed):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, storage.ErrGenerationRequestConflict):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, conversation.ErrForkCampaign):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, inquiry.ErrBusy):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, inquiry.ErrRateLimited), errors.Is(err, shareapp.ErrRateLimited):
		c.JSON(http.StatusTooManyRequests, gin.H{"error": err.Error()})
	case errors.Is(err, inquiry.ErrInvalidQuestion), errors.Is(err, inquiry.ErrInvalidResponse):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, llmsettings.ErrUserSettingsDisabled):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, llmsettings.ErrPlatformIncomplete), errors.Is(err, llmsettings.ErrMissingAPIKey), errors.Is(err, llmsettings.ErrInvalidAccessMode):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, llmsettings.ErrEncryptionUnavailable):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
	case errors.Is(err, context.Canceled):
		c.JSON(http.StatusRequestTimeout, gin.H{"error": "请求已取消"})
	case errors.Is(err, context.DeadlineExceeded):
		c.JSON(http.StatusGatewayTimeout, gin.H{"error": "上游请求超时"})
	case errors.Is(err, storage.ErrCampaignNotActive), errors.Is(err, storage.ErrCampaignRevisionConflict),
		errors.Is(err, storage.ErrCampaignIdempotencyConflict), errors.Is(err, storage.ErrCampaignHasConversation),
		errors.Is(err, storage.ErrCampaignHistoryImmutable),
		errors.Is(err, campaign.ErrActionRejected):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, conversation.ErrCampaignRequired), errors.Is(err, campaign.ErrCampaignModeRequired),
		errors.Is(err, campaign.ErrInvalidInput), errors.Is(err, campaign.ErrInvalidAction), errors.Is(err, campaign.ErrUnknownAction):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, challenge.ErrEntryRequirementNotMet):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, storage.ErrAlreadySettled), errors.Is(err, storage.ErrPublishedVersion),
		errors.Is(err, storage.ErrVersionInUse), errors.Is(err, challengeimport.ErrOverwriteRequired):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, storage.ErrRewardDefinitionConflict), errors.Is(err, storage.ErrRewardDefinitionInUse):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}
