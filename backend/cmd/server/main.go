package main

import (
	"context"
	"path/filepath"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/ialtone/fatecraft/internal/api"
	authservice "github.com/ialtone/fatecraft/internal/auth"
	"github.com/ialtone/fatecraft/internal/challenge"
	"github.com/ialtone/fatecraft/internal/challengeimport"
	"github.com/ialtone/fatecraft/internal/chat"
	"github.com/ialtone/fatecraft/internal/config"
	"github.com/ialtone/fatecraft/internal/conversation"
	"github.com/ialtone/fatecraft/internal/identity"
	"github.com/ialtone/fatecraft/internal/llm"
	"github.com/ialtone/fatecraft/internal/logging"
	"github.com/ialtone/fatecraft/internal/memory"
	"github.com/ialtone/fatecraft/internal/orchestration"
	"github.com/ialtone/fatecraft/internal/realtime"
	"github.com/ialtone/fatecraft/internal/roll"
	"github.com/ialtone/fatecraft/internal/state"
	"github.com/ialtone/fatecraft/internal/storage"
	"github.com/ialtone/fatecraft/internal/tokenizer"
)

func main() {
	cfg := config.Load()
	gin.SetMode(cfg.GinMode)

	// 初始化全局日志器（尽早，后续所有启动日志都走它）。
	logger := logging.Init(cfg.LogLevel, cfg.LogFormat)
	defer func() { _ = logger.Sync() }()

	// 只连接数据库；表结构由 scripts/sql/init.sql 唯一维护。
	db, err := storage.Open(cfg.DatabaseURL)
	if err != nil {
		logger.Fatal("数据库连接失败", zap.Error(err))
	}

	// 仓储层。挑战内容和运行时数据都以 PostgreSQL 为权威来源。
	convRepo := storage.NewConversationRepo(db)
	msgRepo := storage.NewMessageRepo(db)
	stateRepo := storage.NewStateRepo(db)
	memRepo := storage.NewMemoryRepo(db)
	ctxRepo := storage.NewContextRepo(db)
	settingsRepo := storage.NewSettingsRepo(db)
	systemSettingsRepo := storage.NewSystemSettingsRepo(db)
	accountRepo := storage.NewAccountRepository(db)
	authRepo := storage.NewAuthRepository(db)
	challengeRepo := storage.NewChallengeRepository(db)
	challengeSvc := challenge.NewService(challengeRepo)
	builtinImporter := challengeimport.New(challengeRepo, cfg.DataDir)
	bootstrap, err := builtinImporter.BootstrapBuiltins(context.Background())
	if err != nil {
		logger.Fatal("初始化内置挑战失败", zap.String("dataDir", cfg.DataDir), zap.Error(err))
	}
	if bootstrap.Initialized {
		logger.Info("空数据库已初始化内置挑战", zap.Int("count", len(bootstrap.Imports)))
	}

	// 生成锁自愈：进程崩溃/被 kill 后 is_processing 可能残留，启动时统一清除。
	if n, err := convRepo.ClearProcessingLocks(context.Background()); err != nil {
		logger.Warn("清理残留生成锁失败", zap.Error(err))
	} else if n > 0 {
		logger.Info("已清理残留的生成锁", zap.Int64("count", n))
	}

	// 基础组件。
	counter := tokenizer.NewHeuristic()
	llmClient := llm.NewClient()

	// 玩家身份由认证中间件写入请求 context；领域服务不依赖具体登录方式。
	requestIdentity := identity.NewContext()
	authSvc := authservice.NewService(authRepo, authservice.Config{
		LinuxDOClientID:     cfg.LinuxDOClientID,
		LinuxDOClientSecret: cfg.LinuxDOClientSecret,
		LinuxDORedirectURI:  cfg.LinuxDORedirectURI,
		DataEncryptionKey:   cfg.AuthDataEncryptionKey,
		SessionTTL:          cfg.AuthSessionTTL,
	})

	// 领域服务。
	stateSvc := state.NewService(stateRepo)
	convSvc := conversation.NewService(convRepo, stateSvc, msgRepo, challengeSvc, requestIdentity)
	chatSvc := chat.NewService(msgRepo)
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
		"trigger_ending": orchestration.NewCultivationEndingHandler(chatSvc, stateSvc, challengeSvc, requestIdentity),
	}

	// 生成编排 + WebSocket Hub。
	gen := orchestration.NewGenerator(orchestration.GeneratorDeps{
		Conv: convSvc, Chat: chatSvc, State: stateSvc, Memory: memSvc, Challenge: challengeSvc,
		Settings: settingsRepo, System: systemSettingsRepo, CtxLogs: ctxRepo, LLM: llmClient, Counter: counter,
		APIKeyEnv: cfg.LLMAPIKeyEnv, Roll: rollSvc, Actions: actionHandlers, ResponseSchema: respSchema,
		Identity: requestIdentity,
	})
	hub := realtime.NewHub(gen, cfg.AllowedOrigins)

	// HTTP 服务。
	server := api.NewServer(api.Deps{
		Conv: convSvc, Chat: chatSvc, State: stateSvc, Challenges: challengeSvc, Builtins: builtinImporter,
		Memory: memSvc, Settings: settingsRepo, System: systemSettingsRepo, CtxLogs: ctxRepo, Messages: msgRepo,
		Account: accountRepo, Hub: hub, LLMClient: llmClient, Counter: counter, APIKeyEnv: cfg.LLMAPIKeyEnv,
		Identity: requestIdentity, Auth: authSvc, CookieSecure: cfg.AuthCookieSecure,
	})

	// 用 zap 中间件取代 gin.Default() 自带的文本 logger/recovery，
	// 使访问日志与 panic 堆栈进入结构化日志。
	r := gin.New()
	r.Use(logging.GinLogger(), logging.GinRecovery())
	// CORS 来源从 ALLOWED_ORIGINS 环境变量读取（逗号分隔）；
	// 未配置时全放开，仅供开发使用，生产必须收紧。
	corsOrigins := cfg.AllowedOrigins
	allowCredentials := len(corsOrigins) > 0
	if len(corsOrigins) == 0 {
		corsOrigins = []string{"*"}
	}
	r.Use(cors.New(cors.Config{
		AllowOrigins:     corsOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		AllowCredentials: allowCredentials,
	}))
	server.Router(r)

	addr := ":" + cfg.Port
	logger.Info("Fatecraft 后端启动", zap.String("addr", addr), zap.String("dataDir", cfg.DataDir))
	if err := r.Run(addr); err != nil {
		logger.Fatal("服务启动失败", zap.Error(err))
	}
}
