package main

import (
	"context"
	"path/filepath"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/ialtone/fatecraft/internal/api"
	"github.com/ialtone/fatecraft/internal/chat"
	"github.com/ialtone/fatecraft/internal/config"
	"github.com/ialtone/fatecraft/internal/conversation"
	"github.com/ialtone/fatecraft/internal/filestore"
	"github.com/ialtone/fatecraft/internal/identity"
	"github.com/ialtone/fatecraft/internal/llm"
	"github.com/ialtone/fatecraft/internal/logging"
	"github.com/ialtone/fatecraft/internal/memory"
	"github.com/ialtone/fatecraft/internal/orchestration"
	"github.com/ialtone/fatecraft/internal/prompt"
	"github.com/ialtone/fatecraft/internal/realtime"
	"github.com/ialtone/fatecraft/internal/roll"
	"github.com/ialtone/fatecraft/internal/state"
	"github.com/ialtone/fatecraft/internal/storage"
	"github.com/ialtone/fatecraft/internal/tokenizer"
	"github.com/ialtone/fatecraft/internal/worldbook"
)

func main() {
	cfg := config.Load()
	gin.SetMode(cfg.GinMode)

	// 初始化全局日志器（尽早，后续所有启动日志都走它）。
	logger := logging.Init(cfg.LogLevel, cfg.LogFormat)
	defer func() { _ = logger.Sync() }()

	// 连接数据库（连接由用户手动接入；AUTO_MIGRATE 默认关闭，权威 schema 见 scripts/sql/init.sql）。
	db, err := storage.Open(cfg.DatabaseURL, cfg.AutoMigrate)
	if err != nil {
		logger.Fatal("数据库连接失败", zap.Error(err))
	}

	// 仓储层（仅运行时数据；模板数据走文件存储）。
	convRepo := storage.NewConversationRepo(db)
	msgRepo := storage.NewMessageRepo(db)
	stateRepo := storage.NewStateRepo(db)
	memRepo := storage.NewMemoryRepo(db)
	ctxRepo := storage.NewContextRepo(db)
	settingsRepo := storage.NewSettingsRepo(db)
	accountRepo := storage.NewAccountRepository(db)

	// 生成锁自愈：进程崩溃/被 kill 后 is_processing 可能残留，启动时统一清除。
	if n, err := convRepo.ClearProcessingLocks(context.Background()); err != nil {
		logger.Warn("清理残留生成锁失败", zap.Error(err))
	} else if n > 0 {
		logger.Info("已清理残留的生成锁", zap.Int64("count", n))
	}

	// 模板文件存储（新架构：支持预设包 + 共享资源）。
	// 从 preset_packs/*/type 和 shared/type 两个位置加载资源；
	// 第三个参数为可写目录：运行时新建/改删只落到 shared/<type>，预设包模板保持只读。
	charStore := filestore.NewMultiSourceStore[storage.Character](cfg.DataDir, []string{
		"preset_packs/*/characters",
		"shared/characters",
	}, "shared/characters")
	personaStore := filestore.NewMultiSourceStore[storage.Persona](cfg.DataDir, []string{
		"preset_packs/*/personas",
		"shared/personas",
	}, "shared/personas")
	scenStore := filestore.NewMultiSourceStore[storage.Scenario](cfg.DataDir, []string{
		"preset_packs/*/scenarios",
		"shared/scenarios",
	}, "shared/scenarios")
	promptStore := filestore.NewMultiSourceStore[storage.PromptPreset](cfg.DataDir, []string{
		"preset_packs/*/prompts",
		"shared/prompts",
	}, "shared/prompts")
	worldStore := filestore.NewMultiSourceStore[storage.WorldBook](cfg.DataDir, []string{
		"preset_packs/*/worldbooks",
		// 注意：世界书不从 shared 加载，必须属于某个预设包；因此无可写目录（整体只读）。
	}, "")

	for name, loader := range map[string]interface{ Load() error }{
		"characters": charStore, "personas": personaStore, "scenarios": scenStore,
		"prompts": promptStore, "worldbooks": worldStore,
	} {
		if err := loader.Load(); err != nil {
			logger.Fatal("加载模板数据失败", zap.String("kind", name), zap.Error(err))
		}
	}

	// 加载预设包元信息
	presetPacks, err := filestore.LoadPresetPacks(cfg.DataDir)
	if err != nil {
		logger.Warn("加载预设包失败", zap.Error(err))
		presetPacks = []filestore.PresetPack{}
	}
	logger.Info("预设包已加载", zap.Int("count", len(presetPacks)))

	// 基础组件。
	counter := tokenizer.NewHeuristic()
	llmClient := llm.NewClient()

	// 玩家身份（单用户模式：固定返回配置里的 dev id）。多用户化时替换此实现即可。
	devIdentity := identity.NewStatic(cfg.DevPlayerID)

	// 领域服务。
	stateSvc := state.NewService(stateRepo)
	convSvc := conversation.NewService(convRepo, stateSvc, msgRepo, charStore, devIdentity)
	chatSvc := chat.NewService(msgRepo)
	promptSvc := prompt.NewService(promptStore)
	worldSvc := worldbook.NewService(worldStore, counter)
	memSvc := memory.NewService(memRepo, counter)

	// 加载判定系统配置并构造判定服务（配置由实例持有，不再使用全局单例；
	// 判定历史持久化到 roll_history 表，连败补偿等跨重启生效）。
	rollConfigPath := filepath.Join(cfg.DataDir, "roll_config.yaml")
	rollCfg, err := roll.LoadConfig(rollConfigPath)
	if err != nil {
		logger.Fatal("加载判定配置失败", zap.Error(err))
	}
	rollSvc := roll.NewService(rollCfg, storage.NewRollHistoryRepo(db))
	logger.Info("判定系统配置已加载")

	// 结构化输出 Schema：路径由组合根注入，加载失败时降级为不启用 response_format。
	// 加载后注入判定词表 enum，使模型输出与 roll_config.yaml 的精确查表保持同源。
	schemaPath := filepath.Join(cfg.DataDir, "schemas", "narrative_response.json")
	respSchema, err := orchestration.LoadResponseSchema(schemaPath)
	if err != nil {
		logger.Warn("加载响应 JSON Schema 失败，response_format 已降级关闭",
			zap.String("path", schemaPath), zap.Error(err))
	} else {
		orchestration.InjectRollVocab(respSchema, rollCfg.Vocab())
	}

	// AI 动作处理器注册表："修仙"玩法的结局记账以处理器形式注册，编排器保持题材中立。
	actionHandlers := map[string]orchestration.ActionHandler{
		"trigger_ending": orchestration.NewCultivationEndingHandler(convSvc, chatSvc, stateSvc, accountRepo, devIdentity),
	}

	// 生成编排 + WebSocket Hub。
	gen := orchestration.NewGenerator(orchestration.GeneratorDeps{
		Conv: convSvc, Chat: chatSvc, State: stateSvc, Memory: memSvc, World: worldSvc,
		Prompts: promptStore, Chars: charStore, Personas: personaStore, Scenarios: scenStore,
		Settings: settingsRepo, CtxLogs: ctxRepo, LLM: llmClient, Counter: counter,
		APIKeyEnv: cfg.LLMAPIKeyEnv, Roll: rollSvc, Actions: actionHandlers, ResponseSchema: respSchema,
	})
	hub := realtime.NewHub(gen, cfg.AllowedOrigins)

	// HTTP 服务。
	server := api.NewServer(api.Deps{
		Conv: convSvc, Chat: chatSvc, State: stateSvc, Prompt: promptSvc,
		World: worldSvc, Memory: memSvc, Char: charStore, Persona: personaStore,
		Scenario: scenStore, Settings: settingsRepo, CtxLogs: ctxRepo, Messages: msgRepo,
		Account: accountRepo, Hub: hub, LLMClient: llmClient, Counter: counter, APIKeyEnv: cfg.LLMAPIKeyEnv,
		Identity: devIdentity, PresetPacks: presetPacks,
	})

	// 用 zap 中间件取代 gin.Default() 自带的文本 logger/recovery，
	// 使访问日志与 panic 堆栈进入结构化日志。
	r := gin.New()
	r.Use(logging.GinLogger(), logging.GinRecovery())
	// CORS 来源从 ALLOWED_ORIGINS 环境变量读取（逗号分隔）；
	// 未配置时全放开，仅供开发使用，生产必须收紧。
	corsOrigins := cfg.AllowedOrigins
	if len(corsOrigins) == 0 {
		corsOrigins = []string{"*"}
	}
	r.Use(cors.New(cors.Config{
		AllowOrigins:     corsOrigins,
		AllowMethods:     []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		AllowCredentials: false,
	}))
	server.Router(r)

	addr := ":" + cfg.Port
	logger.Info("Fatecraft 后端启动", zap.String("addr", addr), zap.String("dataDir", cfg.DataDir))
	if err := r.Run(addr); err != nil {
		logger.Fatal("服务启动失败", zap.Error(err))
	}
}
