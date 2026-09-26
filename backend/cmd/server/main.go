package main

import (
	"context"
	"errors"
	"net/http"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"go.uber.org/zap"

	"github.com/ialtone/fatecraft/internal/api"
	authservice "github.com/ialtone/fatecraft/internal/auth"
	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/challenge"
	"github.com/ialtone/fatecraft/internal/challengeimport"
	"github.com/ialtone/fatecraft/internal/chat"
	"github.com/ialtone/fatecraft/internal/config"
	"github.com/ialtone/fatecraft/internal/conversation"
	"github.com/ialtone/fatecraft/internal/gameplay"
	"github.com/ialtone/fatecraft/internal/gameplay/campaigngeneric"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/campaignactions"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/campaignview"
	"github.com/ialtone/fatecraft/internal/identity"
	"github.com/ialtone/fatecraft/internal/inquiry"
	"github.com/ialtone/fatecraft/internal/llm"
	"github.com/ialtone/fatecraft/internal/llmrelay"
	"github.com/ialtone/fatecraft/internal/llmsettings"
	"github.com/ialtone/fatecraft/internal/logging"
	"github.com/ialtone/fatecraft/internal/memory"
	"github.com/ialtone/fatecraft/internal/modelcapability"
	"github.com/ialtone/fatecraft/internal/orchestration"
	"github.com/ialtone/fatecraft/internal/realtime"
	"github.com/ialtone/fatecraft/internal/roll"
	shareapp "github.com/ialtone/fatecraft/internal/share"
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
	db, err := storage.Open(cfg.DatabaseURL, storage.DBPoolConfig{
		MaxOpenConns: cfg.DBMaxOpenConns, MaxIdleConns: cfg.DBMaxIdleConns,
		ConnMaxLifetime: cfg.DBConnMaxLifetime, ConnMaxIdleTime: cfg.DBConnMaxIdleTime,
	})
	if err != nil {
		logger.Fatal("数据库连接失败", zap.Error(err))
	}

	// 仓储层。挑战内容和运行时数据都以 PostgreSQL 为权威来源。
	convRepo := storage.NewConversationRepo(db)
	msgRepo := storage.NewMessageRepo(db)
	stateRepo := storage.NewStateRepo(db)
	memRepo := storage.NewMemoryRepo(db)
	turnRepo := storage.NewTurnRepo(db)
	agentRepo := storage.NewAgentRepo(db)
	ctxRepo := storage.NewContextRepo(db)
	settingsRepo := storage.NewSettingsRepo(db)
	systemSettingsRepo := storage.NewSystemSettingsRepo(db)
	llmRequestLogRepo := storage.NewLLMRequestLogRepo(db)
	diagnosticLogRepo := storage.NewDiagnosticLogRepo(db)
	rewardRepo := storage.NewRewardRepository(db)
	authRepo := storage.NewAuthRepository(db)
	challengeRepo := storage.NewChallengeRepository(db)
	shareRepo := storage.NewShareRepository(db)
	gameplayRegistry := gameplay.NewCoreRegistry()
	campaigngeneric.Register(gameplayRegistry)
	cultivation.Register(gameplayRegistry)
	challengeSvc := challenge.NewServiceWithCache(challengeRepo, challenge.CacheConfig{
		MaxVersions: cfg.ChallengeCacheMaxVersions,
		MaxBytes:    cfg.ChallengeCacheMaxBytes,
	}, gameplayRegistry)
	inquiryRepo := storage.NewInquiryRepo(db, challengeSvc)
	campaignRepo := storage.NewCampaignRepository(db, challengeSvc)
	builtinImporter := challengeimport.New(challengeRepo, cfg.DataDir, gameplayRegistry)
	bootstrap, err := builtinImporter.BootstrapBuiltins(context.Background())
	if err != nil {
		logger.Fatal("初始化内置挑战失败", zap.String("dataDir", cfg.DataDir), zap.Error(err))
	}
	if bootstrap.Initialized {
		logger.Info("空数据库已初始化内置挑战", zap.Int("count", len(bootstrap.Imports)))
	}

	// 生成租约自愈：启动时回收本实例上次进程遗留的租约；周期任务只能回收
	// 真正过期的租约，不能按 owner 回收，否则会取消本进程仍在运行的生成。
	convRepo.SetInstanceID(cfg.InstanceID)
	reapStaleGenerations := func(ctx context.Context, recoverOwned bool) {
		if recoverOwned {
			if n, err := convRepo.ReapOwnedGenerations(ctx); err != nil {
				logger.Warn("回收重启前生成租约失败", zap.Error(err))
			} else if n > 0 {
				logger.Info("已回收重启前生成租约", zap.Int64("count", n))
			}
		}
		if n, err := convRepo.ReapStaleGenerations(ctx); err != nil {
			logger.Warn("回收残留生成租约失败", zap.Error(err))
		} else if n > 0 {
			logger.Info("已回收残留的生成租约", zap.Int64("count", n))
		}
		if n, err := agentRepo.MarkOrphanedInvocations(ctx); err != nil {
			logger.Warn("封口残留工具调用失败", zap.Error(err))
		} else if n > 0 {
			logger.Info("已封口残留工具调用", zap.Int64("count", n))
		}
		if n, err := turnRepo.FailOrphaned(ctx); err != nil {
			logger.Warn("封口残留生成回合失败", zap.Error(err))
		} else if n > 0 {
			logger.Info("已封口残留生成回合", zap.Int64("count", n))
		}
	}
	reapStaleGenerations(context.Background(), true)

	// 基础组件。
	counter := tokenizer.NewHeuristic()
	llmClient := llm.NewClient()
	llmSettingsSvc := llmsettings.New(settingsRepo, systemSettingsRepo, cfg.AuthDataEncryptionKey)
	capabilitySvc := modelcapability.New(llmClient)
	// Relay 注册表：client 路由的模型请求经浏览器执行；准入要求有效路由为 client。
	relayRegistry := llmrelay.NewRegistry(llmSettingsSvc.EffectiveRoute, cfg.AllowedOrigins)
	capabilitySvc.SetRelayTransport(relayRegistry.For)
	auditWriter := llm.NewAsyncRequestAuditWriter(func(ctx context.Context, audit llm.RequestAudit) error {
		row := &storage.LLMRequestLog{
			ID: uuid.New(), Model: audit.Model, BaseURL: audit.BaseURL, Stream: audit.Stream,
			Purpose:     audit.Purpose,
			RequestBody: storage.JSONDoc(audit.RequestBody), RawResponse: audit.RawResponse,
			ResponseText: audit.ResponseText, AuditTruncated: audit.AuditTruncated, FinishReason: audit.FinishReason,
			ReceivedDone: audit.ReceivedDone, Success: audit.Success, Error: audit.Error,
			DurationMS:       audit.DurationMS,
			SettingsRevision: audit.SettingsRevision, SettingsSource: audit.SettingsSource, OutputMode: string(audit.OutputMode),
			ContractName: audit.ContractName, ContractVersion: audit.ContractVersion,
			ContractFingerprint: audit.ContractFingerprint, TransportSuccess: audit.TransportSuccess,
			StructuredSuccess: audit.StructuredSuccess, StructuredErrorCode: audit.StructuredErrorCode,
			OutputTypes: pq.StringArray(audit.OutputTypes),
		}
		if audit.ResponseStatus > 0 {
			row.ResponseStatus = &audit.ResponseStatus
		}
		if id, err := uuid.Parse(audit.ConversationID); err == nil {
			row.ConversationID = &id
		}
		if id, err := uuid.Parse(audit.MessageID); err == nil {
			row.MessageID = &id
		}
		if row.Purpose == "" {
			row.Purpose = "narrative"
		}
		if row.SettingsSource == "" {
			row.SettingsSource = "user"
		}
		if id, err := uuid.Parse(audit.AttemptID); err == nil {
			row.AttemptID = &id
		}
		if id, err := uuid.Parse(audit.MemoryJobID); err == nil {
			row.MemoryJobID = &id
		}
		if id, err := uuid.Parse(audit.MemoryID); err == nil {
			row.MemoryID = &id
		}
		if id, err := uuid.Parse(audit.InquiryID); err == nil {
			row.InquiryID = &id
		}
		if id, err := uuid.Parse(audit.InquirySnapshotID); err == nil {
			row.InquirySnapshotID = &id
		}
		row.InquirySnapshotBranchEpoch = audit.InquirySnapshotBranchEpoch
		row.InquirySnapshotRevision = audit.InquirySnapshotRevision
		return llmRequestLogRepo.Create(ctx, row)
	}, 256)
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := auditWriter.Close(closeCtx); err != nil {
			logger.Warn("关闭异步模型请求日志写入器失败", zap.Error(err))
		}
	}()
	llmClient.SetRequestAudit(auditWriter.Write, func(ctx context.Context) bool {
		settings, err := systemSettingsRepo.Get(ctx)
		return err == nil && settings.DetailedLLMLoggingEnabled
	})

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
	stateSvc := state.NewService(stateRepo, challengeSvc)
	convSvc := conversation.NewService(convRepo, stateSvc, msgRepo, challengeSvc, requestIdentity)
	convSvc.SetTurnRepo(turnRepo)
	convSvc.SetRouteResolver(llmSettingsSvc)
	chatSvc := chat.NewService(msgRepo)
	memSvc := memory.NewService(memRepo, counter)
	memoryWorker := memory.NewWorker(memRepo, llmSettingsSvc, llmClient, counter, memory.WorkerConfig{Concurrency: cfg.MemoryWorkerConcurrency})
	memoryWorker.SetRelayAvailability(relayRegistry.HasActive)
	memoryWorker.SetRelayTransport(relayRegistry.For)
	memSvc.SetWake(memoryWorker.Wake)
	memoryWorker.SetScheduler(func(ctx context.Context, conversationID uuid.UUID) {
		if _, err := memSvc.Schedule(ctx, conversationID, false); err != nil {
			logging.Named("memory").Warn("记忆提交后继续调度失败", zap.String("conversation_id", conversationID.String()), zap.Error(err))
		}
	})
	// 加载判定系统配置并构造判定服务（配置由实例持有，不再使用全局单例；
	// 判定历史持久化到 roll_history 表，连败补偿等跨重启生效）。
	rollConfigPath := filepath.Join(cfg.DataDir, "roll_config.yaml")
	rollCfg, err := roll.LoadConfig(rollConfigPath)
	if err != nil {
		logger.Fatal("加载判定配置失败", zap.Error(err))
	}
	rollSvc := roll.NewService(rollCfg, storage.NewRollHistoryRepo(db))
	logger.Info("判定系统配置已加载")
	campaignSvc := campaign.NewService(campaignRepo, challengeSvc, convSvc, requestIdentity, gameplayRegistry)
	campaignSvc.SetRollServices(rollSvc, systemSettingsRepo)
	campaignSvc.SetRouteResolver(llmSettingsSvc)
	campaignactions.Register(campaignSvc)
	campaignSvc.RegisterProjection(campaigngeneric.ProjectionHandler, campaignview.Provider{})
	campaignSvc.RegisterProjection(cultivation.CampaignProjectionHandler, campaignview.Provider{})
	inquirySvc := inquiry.NewService(inquiryRepo, campaignSvc, stateSvc, llmSettingsSvc, requestIdentity, llmClient, counter)
	inquirySvc.SetRelayTransport(relayRegistry.For)
	shareSvc := shareapp.New(shareRepo, cfg.ShareLinkSigningKey)

	// 生成编排 + WebSocket Hub。
	gen := orchestration.NewGenerator(orchestration.GeneratorDeps{
		Conv: convSvc, Chat: chatSvc, State: stateSvc, Memory: memSvc,
		Turns: turnRepo, Agents: agentRepo,
		ToolTx:    orchestration.NewAgentToolTransaction(db, challengeSvc),
		Challenge: challengeSvc, Campaign: campaignSvc,
		Settings: llmSettingsSvc, System: systemSettingsRepo, CtxLogs: ctxRepo, LLM: llmClient, Counter: counter,
		Routes: llmSettingsSvc, Relays: relayRegistry.For,
		Roll: rollSvc, RollCommitter: orchestration.NewRollCommitter(db),
		Completion: orchestration.NewCompletionHandler(chatSvc, stateSvc, challengeSvc, requestIdentity),
		Identity:   requestIdentity,
		Rewards:    rewardRepo,
	})
	hub := realtime.NewHub(gen, cfg.AllowedOrigins)
	memoryActivitySink := memory.ActivitySink(func(conversationID uuid.UUID, status string) {
		hub.Broadcast(conversationID, realtime.Event{
			Type: realtime.TypeActivityUpdated, ConversationID: conversationID.String(),
			Activity: "memory", Status: status,
		})
	})
	memSvc.SetActivitySink(memoryActivitySink)
	memoryWorker.SetActivitySink(memoryActivitySink)

	// HTTP 服务。
	server := api.NewServer(api.Deps{
		Conv: convSvc, Campaigns: campaignSvc, Chat: chatSvc, State: stateSvc, Challenges: challengeSvc, Builtins: builtinImporter,
		Memory: memSvc, Inquiries: inquirySvc, Settings: settingsRepo, LLMSettings: llmSettingsSvc, System: systemSettingsRepo, CtxLogs: ctxRepo, Messages: msgRepo, Agents: agentRepo,
		Rewards: rewardRepo, LLMRequestLogs: llmRequestLogRepo, Turns: turnRepo,
		Hub: hub, LLMClient: llmClient, Capabilities: capabilitySvc, Shares: shareSvc, Relays: relayRegistry, Counter: counter,
		Identity: requestIdentity, Auth: authSvc, CookieSecure: cfg.AuthCookieSecure,
	})

	// 用 zap 中间件取代 gin.Default() 自带的文本 logger/recovery，
	// 使访问日志与 panic 堆栈进入结构化日志。
	r := gin.New()
	// 只信任 TRUSTED_PROXIES 中的反向代理设置的 X-Forwarded-For / X-Real-IP；未配置时传 nil，
	// 不信任任何代理，ClientIP 取直连对端地址，避免客户端伪造来源 IP 绕过限流。
	if err := r.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		logger.Fatal("TRUSTED_PROXIES 配置无效", zap.Strings("trusted_proxies", cfg.TrustedProxies), zap.Error(err))
	}
	r.Use(logging.GinLogger(), logging.GinRecovery())
	// Bound all request bodies before any handler calls ShouldBindJSON. A full
	// challenge version document has a separate, explicit larger boundary;
	// WebSocket frames have their own limit in the Hub.
	r.Use(api.RequestBodyLimit(4<<20, api.BodyLimitOverride{
		Method: http.MethodPut, Path: "/api/admin/challenge-versions/:id", MaxBytes: 32 << 20,
	}))
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
	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	workerCtx, stopWorker := context.WithCancel(rootCtx)
	go memoryWorker.Run(workerCtx)
	memoryWorker.Wake()
	// 诊断日志清理受管理员开关控制。每小时只处理有限批次，避免与在线
	// 请求争抢数据库；分享治理审计不属于这项短期诊断数据。
	go func() {
		cleanup := func() {
			cleanupCtx, cancel := context.WithTimeout(workerCtx, 45*time.Second)
			defer cancel()
			settings, err := systemSettingsRepo.Get(cleanupCtx)
			if err != nil {
				logger.Warn("读取诊断日志清理设置失败", zap.Error(err))
				return
			}
			if !settings.DiagnosticLogCleanupEnabled {
				return
			}
			cutoff := time.Now().UTC().AddDate(0, 0, -settings.DiagnosticLogRetentionDays)
			result, err := diagnosticLogRepo.CleanupExpired(cleanupCtx, cutoff, 500, 20)
			if err != nil {
				logger.Warn("清理过期诊断日志失败", zap.Error(err))
				return
			}
			if result.LLMRequests+result.ContextBuilds > 0 {
				logger.Info("已清理过期诊断日志", zap.Int64("llm_request_logs", result.LLMRequests), zap.Int64("context_build_logs", result.ContextBuilds))
			}
		}
		cleanup()
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-workerCtx.Done():
				return
			case <-ticker.C:
				cleanup()
			}
		}
	}()
	// 租约回收器：周期略短于租约时长，保证死实例持有的对话最多在一个租约周期内解锁。
	go func() {
		ticker := time.NewTicker(storage.GenerationHeartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-workerCtx.Done():
				return
			case <-ticker.C:
				reapCtx, cancel := context.WithTimeout(workerCtx, 20*time.Second)
				reapStaleGenerations(reapCtx, false)
				cancel()
			}
		}
	}()

	logger.Info("Fatecraft 后端启动", zap.String("addr", addr), zap.String("dataDir", cfg.DataDir))
	// Keep header/request idle timeouts explicit. WriteTimeout is intentionally
	// omitted because the same server hosts long-lived WebSocket connections.
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() { errCh <- httpServer.ListenAndServe() }()
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			stopWorker()
			memoryWorker.Wait()
			logger.Fatal("服务启动失败", zap.Error(err))
		}
	case <-rootCtx.Done():
	}
	stopWorker()
	memoryWorker.Wait()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Warn("HTTP 服务关闭未完成", zap.Error(err))
	}
}
