package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/chat"
	"github.com/ialtone/fatecraft/internal/conversation"
	"github.com/ialtone/fatecraft/internal/filestore"
	"github.com/ialtone/fatecraft/internal/identity"
	"github.com/ialtone/fatecraft/internal/llm"
	"github.com/ialtone/fatecraft/internal/memory"
	"github.com/ialtone/fatecraft/internal/prompt"
	"github.com/ialtone/fatecraft/internal/realtime"
	"github.com/ialtone/fatecraft/internal/state"
	"github.com/ialtone/fatecraft/internal/storage"
	"github.com/ialtone/fatecraft/internal/tokenizer"
	"github.com/ialtone/fatecraft/internal/worldbook"
)

// Server 聚合全部服务，承载 HTTP 与 WebSocket 处理。
type Server struct {
	conv        *conversation.Service
	chat        *chat.Service
	state       *state.Service
	prompt      *prompt.Service
	world       *worldbook.Service
	memory      *memory.Service
	char        StoreInterface[storage.Character]
	persona     StoreInterface[storage.Persona]
	scenario    StoreInterface[storage.Scenario]
	settings    *storage.SettingsRepo
	ctxLogs     *storage.ContextRepo
	messages    *storage.MessageRepo
	account     *storage.AccountRepository
	hub         *realtime.Hub
	llmClient   *llm.Client
	counter     tokenizer.Counter
	apiKeyEnv   string
	identity    identity.Provider // 当前玩家身份来源（单用户模式下固定返回 dev id）
	presetPacks []filestore.PresetPack
}

// StoreInterface 定义了通用的存储接口
type StoreInterface[T storage.Entity] interface {
	Get(id string) (T, error)
	List() []T
	Load() error
	Exists(id string) bool
	Save(item T) error
	Delete(id string) error
}

// Deps 是构造 Server 所需的依赖集合。
type Deps struct {
	Conv        *conversation.Service
	Chat        *chat.Service
	State       *state.Service
	Prompt      *prompt.Service
	World       *worldbook.Service
	Memory      *memory.Service
	Char        StoreInterface[storage.Character]
	Persona     StoreInterface[storage.Persona]
	Scenario    StoreInterface[storage.Scenario]
	Settings    *storage.SettingsRepo
	CtxLogs     *storage.ContextRepo
	Messages    *storage.MessageRepo
	Account     *storage.AccountRepository
	Hub         *realtime.Hub
	LLMClient   *llm.Client
	Counter     tokenizer.Counter
	APIKeyEnv   string
	Identity    identity.Provider // 当前玩家身份来源（单用户模式下固定返回 dev id）
	PresetPacks []filestore.PresetPack
}

func NewServer(d Deps) *Server {
	return &Server{
		conv: d.Conv, chat: d.Chat, state: d.State, prompt: d.Prompt, world: d.World,
		memory: d.Memory, char: d.Char, persona: d.Persona, scenario: d.Scenario,
		settings: d.Settings, ctxLogs: d.CtxLogs, messages: d.Messages, account: d.Account, hub: d.Hub,
		llmClient: d.LLMClient, counter: d.Counter, apiKeyEnv: d.APIKeyEnv, identity: d.Identity,
		presetPacks: d.PresetPacks,
	}
}

// Router 注册全部路由。
func (s *Server) Router(r *gin.Engine) {
	api := r.Group("/api")

	api.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })

	// 对话
	api.POST("/conversations", s.createConversation)
	api.GET("/conversations", s.listConversations)
	api.GET("/conversations/:id", s.getConversation)
	api.PATCH("/conversations/:id", s.updateConversation)
	api.DELETE("/conversations/:id", s.deleteConversation)
	api.POST("/conversations/:id/end", s.endConversation)
	api.POST("/conversations/:id/archive", s.archiveConversation)
	api.POST("/conversations/:id/restore", s.restoreConversation)

	// 消息
	api.GET("/conversations/:id/messages", s.listMessages)
	api.POST("/conversations/:id/messages", s.postMessage)
	api.POST("/conversations/:id/regenerate", s.regenerate)
	api.POST("/conversations/:id/continue", s.continueGen)
	api.PATCH("/messages/:id", s.editMessage)
	api.POST("/messages/:id/edit-and-regenerate", s.editAndRegenerate)
	api.DELETE("/messages/:id", s.deleteMessage)

	// 状态
	api.GET("/conversations/:id/state", s.getState)
	api.PATCH("/conversations/:id/state", s.patchState)
	api.GET("/conversations/:id/state/revisions", s.stateRevisions)

	// 上下文
	api.GET("/conversations/:id/context/last", s.lastContext)
	api.GET("/context/:cid", s.getContext)

	// 摘要
	api.GET("/conversations/:id/summaries", s.listSummaries)
	api.POST("/conversations/:id/summaries", s.generateSummary)
	api.PATCH("/summaries/:id", s.editSummary)

	// 角色卡 / Persona / 场景
	api.GET("/characters", s.listCharacters)
	api.POST("/characters", s.createCharacter)
	api.GET("/characters/:id", s.getCharacter)
	api.PATCH("/characters/:id", s.updateCharacter)
	api.DELETE("/characters/:id", s.deleteCharacter)
	api.GET("/personas", s.listPersonas)
	api.POST("/personas", s.createPersona)
	api.GET("/scenarios", s.listScenarios)

	// Prompt 预设
	api.GET("/prompts", s.listPrompts)
	api.POST("/prompts", s.createPrompt)
	api.GET("/prompts/:id", s.getPrompt)
	api.PATCH("/prompts/:id", s.updatePrompt)
	api.DELETE("/prompts/:id", s.deletePrompt)
	api.POST("/prompts/:id/segments", s.addSegment)
	api.PATCH("/prompt-segments/:id", s.updateSegment)
	api.DELETE("/prompt-segments/:id", s.deleteSegment)

	// 世界书
	api.GET("/worldbooks", s.listWorldBooks)
	api.POST("/worldbooks", s.createWorldBook)
	api.GET("/worldbooks/:id", s.getWorldBook)
	api.PATCH("/worldbooks/:id", s.updateWorldBook)
	api.DELETE("/worldbooks/:id", s.deleteWorldBook)
	api.POST("/worldbooks/:id/entries", s.addEntry)
	api.PATCH("/worldbook-entries/:id", s.updateEntry)
	api.DELETE("/worldbook-entries/:id", s.deleteEntry)
	api.POST("/conversations/:id/worldbooks/:world_book_id", s.linkWorldBook)
	api.DELETE("/conversations/:id/worldbooks/:world_book_id", s.unlinkWorldBook)

	// 预设包
	api.GET("/preset-packs", s.listPresetPacks)
	api.GET("/preset-packs/:id", s.getPresetPack)

	// 账号（天道回溯）
	api.GET("/account", s.getAccount)
	api.POST("/account/rewind/use", s.useRewind)

	// 设置
	api.GET("/settings/llm", s.getLLMSettings)
	api.PATCH("/settings/llm", s.updateLLMSettings)
	api.POST("/settings/fetch-models", s.fetchModels)

	// WebSocket
	r.GET("/ws/conversations/:id", s.serveWS)
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
	case errors.Is(err, storage.ErrNotFound), errors.Is(err, filestore.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "资源不存在"})
	case errors.Is(err, conversation.ErrNotActive):
		c.JSON(http.StatusConflict, gin.H{"error": "对话未处于 active 状态"})
	case errors.Is(err, filestore.ErrReadOnly):
		// 预设包模板只读、或该类资源未配置可写目录（如世界书）：语义是「方法不允许」。
		c.JSON(http.StatusMethodNotAllowed, gin.H{"error": "该资源为只读，不支持此操作"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}
