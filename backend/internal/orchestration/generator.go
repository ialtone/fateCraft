// Package orchestration 是生成编排（application 层）：
// 承担「装配上下文 → LLM 调用 → 解析信封 → 落库 → 应用状态 → 处理动作 → 推送事件」
// 的完整管线，并以 ActionHandler 注册表分发玩法相关的 AI 动作，保持编排器题材中立。
//
// 事件类型与 EmitFunc 定义在 realtime 包（WebSocket 事件协议所在地）；
// realtime.Hub 通过其消费端 Generator 接口反向依赖本包实现，两包之间无环。
package orchestration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/challenge"
	"github.com/ialtone/fatecraft/internal/chat"
	"github.com/ialtone/fatecraft/internal/conversation"
	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/guidance"
	"github.com/ialtone/fatecraft/internal/identity"
	"github.com/ialtone/fatecraft/internal/llm"
	"github.com/ialtone/fatecraft/internal/logging"
	"github.com/ialtone/fatecraft/internal/memory"
	"github.com/ialtone/fatecraft/internal/operation"
	"github.com/ialtone/fatecraft/internal/platformcapability"
	"github.com/ialtone/fatecraft/internal/realtime"
	"github.com/ialtone/fatecraft/internal/roll"
	"github.com/ialtone/fatecraft/internal/state"
	"github.com/ialtone/fatecraft/internal/storage"
	"github.com/ialtone/fatecraft/internal/tokenizer"
)

// Generator 编排一次完整的生成流程（doc 06 §请求流程）。
// 实现 realtime.Generator 接口，供 Hub 与 REST 触发的生成复用同一条管线。
type Generator struct {
	convSvc   *conversation.Service
	chatSvc   *chat.Service
	stateSvc  *state.Service
	memSvc    *memory.Service
	turns     *storage.TurnRepo
	agents    *storage.AgentRepo
	toolTx    *AgentToolTransaction
	challenge *challenge.Service
	campaign  *campaign.Service
	settings  interface {
		Get(context.Context, string) (*storage.LLMSettings, error)
	}
	// routes 解析当前用户的有效路由；为 nil 时默认 server（仅测试路径）。
	routes interface {
		EffectiveRoute(ctx context.Context, playerID string) (string, error)
	}
	// relays 返回该玩家的 Relay 传输；为 nil 时 client 路由无可用传输。
	relays         func(playerID string) llm.RelayTransport
	systemSettings *storage.SystemSettingsRepo
	ctxRepo        *storage.ContextRepo
	llmClient      *llm.Client
	counter        tokenizer.Counter
	roll           *roll.Service  // 判定服务；nil 表示判定系统不可用
	rollCommitter  *RollCommitter // 判定流水、结果消息与状态效果的原子提交器
	completion     *CompletionHandler
	identity       identity.Provider
	rewards        *storage.RewardRepository
}

// GeneratorDeps 是构造 Generator 的依赖集合（具名字段，取代旧的 16 个位置参数）。
type GeneratorDeps struct {
	Conv      *conversation.Service
	Chat      *chat.Service
	State     *state.Service
	Memory    *memory.Service
	Turns     *storage.TurnRepo
	Agents    *storage.AgentRepo
	ToolTx    *AgentToolTransaction
	Challenge *challenge.Service
	Campaign  *campaign.Service
	Settings  interface {
		Get(context.Context, string) (*storage.LLMSettings, error)
	}
	// Routes 解析当前用户的有效路由；为 nil 时默认 server（仅测试路径）。
	Routes interface {
		EffectiveRoute(ctx context.Context, playerID string) (string, error)
	}
	// Relays 返回该玩家的 Relay 传输；client 路由的模型调用经它执行。
	// 为 nil 时 client 路由无可用传输（仅测试路径）。
	Relays  func(playerID string) llm.RelayTransport
	System  *storage.SystemSettingsRepo
	CtxLogs *storage.ContextRepo
	LLM     *llm.Client
	Counter tokenizer.Counter

	// Roll 是判定工具使用的可信计算服务；为 nil 时判定工具会拒绝执行。
	Roll *roll.Service
	// RollCommitter 原子提交判定结果；为 nil 时收到判定请求会拒绝执行。
	RollCommitter *RollCommitter
	Completion    *CompletionHandler
	// Identity 从生成 context 中解析当前用户，以读取其独立 LLM 设置。
	Identity identity.Provider
	// Rewards 用于在已扣除改写额度、但替代生成未提交时执行幂等返还。
	Rewards *storage.RewardRepository
}

// NewGenerator 构造主叙事 Agent 编排器。
func NewGenerator(d GeneratorDeps) *Generator {
	return &Generator{
		convSvc: d.Conv, chatSvc: d.Chat, stateSvc: d.State, memSvc: d.Memory,
		turns: d.Turns, agents: d.Agents, toolTx: d.ToolTx,
		challenge: d.Challenge, campaign: d.Campaign,
		settings: d.Settings, systemSettings: d.System, ctxRepo: d.CtxLogs,
		routes: d.Routes, relays: d.Relays,
		llmClient: d.LLM, counter: d.Counter,
		roll: d.Roll, rollCommitter: d.RollCommitter, completion: d.Completion,
		identity: d.Identity,
		rewards:  d.Rewards,
	}
}

// RequestCancel persists cancellation before the Hub cancels the in-memory
// context. Generation write boundaries reject cancel_requested runs, closing
// the small window between a user stop request and provider cancellation.
func (g *Generator) RequestCancel(ctx context.Context, convID uuid.UUID, reason string) error {
	return g.convSvc.CancelGeneration(ctx, convID, reason)
}

func (g *Generator) finishGenerationRun(runID uuid.UUID, ctx context.Context) {
	status := storage.GenerationRunCompleted
	errorCode := ""
	committed := false
	mutated := false
	if tracker, ok := ctx.Value(trackerKey{}).(*generationTracker); ok {
		tracker.mu.Lock()
		stage, code := tracker.stage, tracker.code
		committed = tracker.committed
		mutated = tracker.mutatingCommitted
		tracker.mu.Unlock()
		switch {
		case committed:
			status = storage.GenerationRunCompleted
		case ctx.Err() != nil:
			status = storage.GenerationRunCancelled
			errorCode = "context_cancelled"
		case stage != "" || code != "":
			status = storage.GenerationRunFailed
			errorCode = code
		}
	} else {
		// A run is not considered successful merely because no attempt tracker
		// was installed.  Preflight failures (runtime lookup, branch preparation
		// or attempt allocation) happen before beginAttempt and previously left a
		// misleading completed lease in the audit trail.
		if ctx.Err() != nil {
			status = storage.GenerationRunCancelled
			errorCode = "context_cancelled"
		} else {
			status = storage.GenerationRunFailed
			errorCode = "generation_setup_failed"
		}
	}
	// 已提交写工具（partial）时不返还：付费改写已经替换了可信事实，
	// 返还会让“取消 → 重新生成”变成免费重掷。
	if !committed && !mutated {
		g.refundRewriteReservation(ctx, runID.String())
	}
	if err := g.convSvc.FinishGeneration(context.WithoutCancel(ctx), runID, status, errorCode); err != nil {
		logging.Named("orchestration").Warn("释放生成运行实例失败",
			zap.String("run_id", runID.String()), zap.Error(err))
	}
}

// refundRewriteReservation 在替换叙事没有到达提交边界、且没有提交任何写工具时
// 返还已扣除的改写额度。可重复调用：补偿发放使用确定性的幂等键。在发送终止
// WebSocket 事件之前调用，客户端刷新余额时能与失败提示保持一致；
// finishGenerationRun 会再调用一次，作为崩溃或提前返回时的兜底。
func (g *Generator) refundRewriteReservation(ctx context.Context, source string) {
	reservation, ok := rewriteReservationFromContext(ctx)
	if !ok || g.rewards == nil {
		return
	}
	if _, err := g.rewards.RefundConsumed(context.WithoutCancel(ctx), storage.RewardRefund{
		PlayerID: reservation.PlayerID, RewardKey: reservation.RewardKey,
		ConsumeIdempotencyKey: reservation.ConsumeIdempotencyKey,
		Metadata: storage.JSONMap{
			"operation": "message_rewrite_refund", "source": source,
		},
	}); err != nil {
		logging.Named("orchestration").Error("返还剧情改写额度失败",
			zap.String("source", source), zap.String("consume_idempotency_key", reservation.ConsumeIdempotencyKey), zap.Error(err))
	}
}

// RecoverPanic 由 Hub 在生成 goroutine panic 后调用：封存该对话残留的生成租约
// 与未完成回合，避免对话永久停留在“正在生成中”。领域事务已随 panic 回滚。
func (g *Generator) RecoverPanic(ctx context.Context, convID uuid.UUID, recovered any) {
	log := logging.Named("orchestration").With(zap.String("conversation_id", convID.String()), zap.Any("panic", recovered))
	if err := g.convSvc.FailActiveGeneration(ctx, convID, "panic"); err != nil {
		log.Error("封存 panic 生成租约失败", zap.Error(err))
	}
	if g.turns != nil {
		if _, err := g.turns.FailOrphaned(ctx); err != nil {
			log.Error("封口 panic 生成回合失败", zap.Error(err))
		}
	}
}

// heartbeatGeneration 续期当前生成租约。返回错误说明租约已被回收或抢占，
// 调用方必须立即停止本轮生成。
func (g *Generator) heartbeatGeneration(ctx context.Context) error {
	generation, ok := operation.GenerationFromContext(ctx)
	if !ok {
		return nil
	}
	return g.convSvc.HeartbeatGeneration(ctx, generation.ID)
}

// runLeaseHeartbeat 在后台按固定间隔续期租约，直到 ctx 结束。续期失败时
// 取消 attempt，让正在进行的模型请求和后续写入尽快停止。
func (g *Generator) runLeaseHeartbeat(ctx context.Context, cancel context.CancelFunc) {
	ticker := time.NewTicker(storage.GenerationHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := g.heartbeatGeneration(ctx); err != nil {
				logging.Named("orchestration").Warn("生成租约续期失败，中止本轮生成", zap.Error(err))
				cancel()
				return
			}
		}
	}
}

// relayForRoute 返回 client 路由设置对应的 Relay 传输，否则返回 nil。
// 无可用传输时返回 nil，doRaw 将报 llm_relay_unavailable。
func (g *Generator) relayForRoute(ctx context.Context, settings *storage.LLMSettings) llm.RelayTransport {
	if settings.LLMRoute != storage.LLMRouteClient || g.relays == nil {
		return nil
	}
	return g.relays(g.identity.CurrentPlayerID(ctx))
}

// checkLLMRouteGate 在创建任何生成记录之前校验对话路由：对象固化路由必须
// 等于当前有效路由。返回对话、对象路由与有效路由；不匹配时 err 为 ErrLLMRouteMismatch。
func (g *Generator) checkLLMRouteGate(ctx context.Context, convID uuid.UUID) (conv *storage.Conversation, objectRoute, effectiveRoute string, err error) {
	conv, err = g.convSvc.GetOwned(ctx, convID)
	if err != nil {
		return nil, "", "", err
	}
	objectRoute = conv.LLMRoute
	effectiveRoute = storage.LLMRouteServer
	if g.routes != nil {
		effectiveRoute, err = g.routes.EffectiveRoute(ctx, g.identity.CurrentPlayerID(ctx))
		if err != nil {
			return nil, "", "", err
		}
	}
	if err = storage.CheckLLMRoute(objectRoute, effectiveRoute); err != nil {
		return nil, objectRoute, effectiveRoute, err
	}
	return conv, objectRoute, effectiveRoute, nil
}

// emitRouteGateError 按入口输出路由闸门失败：WS 入口发 notice，不创建 run/turn/attempt、不扣额度。
func emitRouteGateError(convID uuid.UUID, objectRoute, effectiveRoute string, err error, emit realtime.EmitFunc) {
	if errors.Is(err, storage.ErrLLMRouteMismatch) {
		emit(infoEvent(convID, storage.LLMRouteMismatchMessage(objectRoute, effectiveRoute)))
		return
	}
	emit(domainEvent(convID, err))
}

// Generate 处理一次用户输入：保存用户消息并生成 AI 回复。
func (g *Generator) Generate(ctx context.Context, convID, requestID uuid.UUID, userContent string, emit realtime.EmitFunc) {
	request := generationRequest(requestID, "user_input", nil, userContent)
	if selection, ok := guidance.SelectionFromContext(ctx); ok {
		target, err := uuid.Parse(selection.ID)
		if err != nil {
			emit(errorEvent(convID, "无效的行动提示 ID"))
			return
		}
		request = generationRequest(requestID, "user_input", &target, selection.ID+":"+selection.Key)
	}
	// 路由闸门：StartGeneration 之前校验，不匹配时只发 notice，不创建任何记录、不扣额度。
	if _, objectRoute, effectiveRoute, gateErr := g.checkLLMRouteGate(ctx, convID); gateErr != nil {
		emitRouteGateError(convID, objectRoute, effectiveRoute, gateErr, emit)
		return
	}
	conv, run, replayed, err := g.convSvc.StartGeneration(ctx, convID, request)
	if err != nil {
		emit(domainEvent(convID, err))
		return
	}
	if run == nil {
		emit(infoEvent(convID, "当前对话正在生成中，请稍候"))
		return
	}
	if replayed {
		emit(realtime.Event{Type: realtime.TypeGenerationReplayed, ConversationID: convID.String(), OperationID: run.ID.String()})
		return
	}
	ctx = operation.WithGeneration(ctx, convID, run.ID)
	defer func() { g.finishGenerationRun(run.ID, ctx) }()
	uTokens := g.counter.Count(userContent)
	preparation, err := g.turns.BeginUserTurn(ctx, convID, "user_input", userContent, &uTokens)
	if err != nil {
		emit(errorEvent(convID, "创建生成回合失败: "+err.Error()))
		return
	}
	ctx, finish, err := g.beginAttemptFromRecords(ctx, convID, "user_input", &preparation.Turn, &preparation.Attempt)
	if err != nil {
		emit(errorEvent(convID, "创建生成回合失败: "+err.Error()))
		return
	}
	defer finish()
	emit(decorateGenerationEvent(ctx, realtime.Event{Type: realtime.TypeMessageCreated, ConversationID: convID.String(), Message: &preparation.Display}))
	_ = g.convSvc.Touch(ctx, convID)

	g.runGeneration(ctx, conv, &preparation.Trigger.ID, emit)
}

// Regenerate 删除最近一条 AI 回复后重新生成。
func (g *Generator) Regenerate(ctx context.Context, convID, requestID uuid.UUID, emit realtime.EmitFunc) {
	if _, objectRoute, effectiveRoute, gateErr := g.checkLLMRouteGate(ctx, convID); gateErr != nil {
		emitRouteGateError(convID, objectRoute, effectiveRoute, gateErr, emit)
		return
	}
	conv, run, replayed, err := g.convSvc.StartGeneration(ctx, convID, generationRequest(requestID, "regenerate", nil, "regenerate"))
	if err != nil {
		emit(domainEvent(convID, err))
		return
	}
	if run == nil {
		emit(infoEvent(convID, "当前对话正在生成中，请稍候"))
		return
	}
	if replayed {
		emit(realtime.Event{Type: realtime.TypeGenerationReplayed, ConversationID: convID.String(), OperationID: run.ID.String()})
		return
	}
	ctx = operation.WithGeneration(ctx, convID, run.ID)
	defer func() { g.finishGenerationRun(run.ID, ctx) }()
	contract, err := g.challenge.RuntimeForConversation(ctx, convID)
	if err != nil {
		emit(errorEvent(convID, "读取玩法合约失败: "+err.Error()))
		return
	}
	// Regeneration is the no-edit form of the same platform-owned capability as
	// message rewrite. It must not depend on whatever a challenge package puts
	// in its Runtime JSON.
	policy := platformcapability.MessageRewritePolicy()
	if !policy.Enabled {
		emit(infoEvent(convID, "当前未开启剧情改写"))
		return
	}
	playerID, err := uuid.Parse(g.identity.CurrentPlayerID(ctx))
	if err != nil {
		emit(errorEvent(convID, "当前用户身份无效"))
		return
	}
	preparation, err := g.turns.BeginRegenerate(ctx, storage.RegenerateInput{
		ConversationID: convID,
		PlayerID:       playerID,
		IdempotencyKey: storage.RegenerateRequestIdempotencyKey(requestID),
		InitialState:   storage.JSONMap(gameplay.InitialState(contract.StateSchema)),
	})
	if err != nil {
		if errors.Is(err, storage.ErrCampaignCheckpointMissing) || errors.Is(err, storage.ErrRegenerateNotAllowed) || errors.Is(err, storage.ErrInsufficientReward) {
			emit(infoEvent(convID, err.Error()))
		} else {
			emit(errorEvent(convID, "准备重新生成失败: "+err.Error()))
		}
		return
	}
	if preparation.RewardConsumed {
		ctx = withRewriteReservation(ctx, rewriteReservation{
			PlayerID: playerID, RewardKey: preparation.RewardKey,
			ConsumeIdempotencyKey: storage.RegenerateRequestIdempotencyKey(requestID),
		})
	}
	ctx, finish, err := g.beginAttemptFromRecords(ctx, convID, "regenerate", &preparation.Turn, &preparation.Attempt)
	if err != nil {
		g.refundRewriteReservation(ctx, "regenerate_attempt_setup")
		emit(errorEvent(convID, "创建重新生成回合失败: "+err.Error()))
		return
	}
	defer finish()

	emit(branchReplacedEvent(ctx, convID, preparation.InvalidatedDisplayIDs, nil))
	g.runGeneration(ctx, conv, &preparation.Trigger.ID, emit)
}

// Rewrite owns the complete edit-and-regenerate operation. Branch replacement,
// the replacement turn/attempt and edited trigger message are committed by one
// storage transaction after the durable generation lease is acquired.
func (g *Generator) Rewrite(ctx context.Context, convID, requestID, messageID uuid.UUID, content string, emit realtime.EmitFunc) {
	if _, objectRoute, effectiveRoute, gateErr := g.checkLLMRouteGate(ctx, convID); gateErr != nil {
		emitRouteGateError(convID, objectRoute, effectiveRoute, gateErr, emit)
		return
	}
	conv, run, replayed, err := g.convSvc.StartGeneration(ctx, convID, generationRequest(requestID, "rewrite", &messageID, content))
	if err != nil {
		emit(domainEvent(convID, err))
		return
	}
	if run == nil {
		emit(infoEvent(convID, "当前对话正在生成中，请稍候"))
		return
	}
	if replayed {
		emit(realtime.Event{Type: realtime.TypeGenerationReplayed, ConversationID: convID.String(), OperationID: run.ID.String()})
		return
	}
	ctx = operation.WithGeneration(ctx, convID, run.ID)
	defer func() { g.finishGenerationRun(run.ID, ctx) }()
	contract, err := g.challenge.RuntimeForConversation(ctx, convID)
	if err != nil {
		emit(errorEvent(convID, "读取玩法合约失败: "+err.Error()))
		return
	}
	policy := platformcapability.MessageRewritePolicy()
	if !policy.Enabled {
		emit(infoEvent(convID, "当前未开启剧情改写"))
		return
	}
	playerID, err := uuid.Parse(g.identity.CurrentPlayerID(ctx))
	if err != nil {
		emit(errorEvent(convID, "当前用户身份无效"))
		return
	}
	preparation, err := g.turns.BeginRewrite(ctx, storage.RewriteUserMessageInput{
		ConversationID: convID, MessageID: messageID, Content: content, PlayerID: playerID,
		IdempotencyKey: storage.RewriteRequestIdempotencyKey(requestID),
		InitialState:   storage.JSONMap(gameplay.InitialState(contract.StateSchema)),
	})
	if err != nil {
		if errors.Is(err, storage.ErrInsufficientReward) || errors.Is(err, storage.ErrCampaignCheckpointMissing) {
			emit(infoEvent(convID, err.Error()))
		} else {
			emit(errorEvent(convID, "准备剧情改写失败: "+err.Error()))
		}
		return
	}
	if preparation.RewardConsumed {
		ctx = withRewriteReservation(ctx, rewriteReservation{
			PlayerID: playerID, RewardKey: preparation.RewardKey,
			ConsumeIdempotencyKey: storage.RewriteRequestIdempotencyKey(requestID),
		})
	}
	if !preparation.Applied {
		ctx = withTracker(ctx, &generationTracker{committed: true})
		return
	}
	ctx, finish, err := g.beginAttemptFromRecords(ctx, convID, "rewrite", &preparation.Turn, &preparation.Attempt)
	if err != nil {
		g.refundRewriteReservation(ctx, "rewrite_attempt_setup")
		emit(errorEvent(convID, "创建改写回合失败: "+err.Error()))
		return
	}
	defer finish()
	emit(branchReplacedEvent(ctx, convID, preparation.InvalidatedDisplayIDs, &preparation.Display))
	g.runGeneration(ctx, conv, &preparation.Trigger.ID, emit)
}

func branchReplacedEvent(ctx context.Context, convID uuid.UUID, invalidated []uuid.UUID, updated *storage.DisplayMessage) realtime.Event {
	ids := make([]string, 0, len(invalidated))
	for _, id := range invalidated {
		ids = append(ids, id.String())
	}
	return decorateGenerationEvent(ctx, realtime.Event{
		Type: realtime.TypeBranchReplaced, ConversationID: convID.String(), Message: updated,
		Payload: map[string]any{"invalidated_message_ids": ids},
	})
}

// Continue 追加一条可见的“继续”用户消息，并生成一段 AI 续写。
// 该消息既进入模型上下文，也进入玩家时间线，保证实时页面与刷新后的
// display_messages 保持一致。
func (g *Generator) Continue(ctx context.Context, convID, requestID uuid.UUID, emit realtime.EmitFunc) {
	if _, objectRoute, effectiveRoute, gateErr := g.checkLLMRouteGate(ctx, convID); gateErr != nil {
		emitRouteGateError(convID, objectRoute, effectiveRoute, gateErr, emit)
		return
	}
	conv, run, replayed, err := g.convSvc.StartGeneration(ctx, convID, generationRequest(requestID, "continue", nil, "continue"))
	if err != nil {
		emit(domainEvent(convID, err))
		return
	}
	if run == nil {
		emit(infoEvent(convID, "当前对话正在生成中，请稍候"))
		return
	}
	if replayed {
		emit(realtime.Event{Type: realtime.TypeGenerationReplayed, ConversationID: convID.String(), OperationID: run.ID.String()})
		return
	}
	ctx = operation.WithGeneration(ctx, convID, run.ID)
	defer func() { g.finishGenerationRun(run.ID, ctx) }()
	continueContent := "继续"
	uTokens := g.counter.Count(continueContent)
	preparation, err := g.turns.BeginUserTurn(ctx, convID, "continue", continueContent, &uTokens)
	if err != nil {
		if errors.Is(err, storage.ErrContinueNotAllowed) {
			emit(infoEvent(convID, err.Error()))
		} else {
			emit(errorEvent(convID, "创建续写回合失败: "+err.Error()))
		}
		return
	}
	ctx, finish, err := g.beginAttemptFromRecords(ctx, convID, "continue", &preparation.Turn, &preparation.Attempt)
	if err != nil {
		emit(errorEvent(convID, "创建续写回合失败: "+err.Error()))
		return
	}
	defer finish()

	// ctx 已取消（如客户端立即点了取消）则不落任何消息。
	if ctx.Err() != nil {
		emit(infoEvent(convID, "生成已取消"))
		return
	}

	emit(decorateGenerationEvent(ctx, realtime.Event{Type: realtime.TypeMessageCreated, ConversationID: convID.String(), Message: &preparation.Display}))
	_ = g.convSvc.Touch(ctx, convID)

	g.runGeneration(ctx, conv, &preparation.Trigger.ID, emit)
}

func generationRequest(requestID uuid.UUID, operationType string, targetMessageID *uuid.UUID, payload string) storage.GenerationRequest {
	if requestID == uuid.Nil {
		requestID = uuid.New()
	}
	hash := sha256.Sum256([]byte(operationType + "\x00" + payload))
	return storage.GenerationRequest{
		RequestID: requestID, OperationType: operationType, TargetMessageID: targetMessageID,
		PayloadHash: hex.EncodeToString(hash[:]),
	}
}

// WebSocket 用 error 事件表示“本轮生成已经终止”，用 notice 事件表示“请求未被
// 执行 / 已取消”等非终止提示。前端只对 error 复位流式状态；notice 只展示横幅，
// 因此另一个连接的忙碌提示不会打断正在接收的流式输出。服务端日志级别由
// 业务语义决定，不能把所有客户端提示都当成系统 ERROR。
func errorEvent(convID uuid.UUID, msg string, fields ...zap.Field) realtime.Event {
	return loggedEvent(convID, realtime.TypeError, msg, zapcore.ErrorLevel, "生成流程故障", fields...)
}

func warningEvent(convID uuid.UUID, msg string, fields ...zap.Field) realtime.Event {
	return loggedEvent(convID, realtime.TypeError, msg, zapcore.WarnLevel, "生成请求未通过", fields...)
}

func infoEvent(convID uuid.UUID, msg string, fields ...zap.Field) realtime.Event {
	return loggedEvent(convID, realtime.TypeNotice, msg, zapcore.InfoLevel, "生成流程未执行", fields...)
}

func loggedEvent(convID uuid.UUID, eventType, msg string, level zapcore.Level, logMessage string, fields ...zap.Field) realtime.Event {
	return loggedEventWith(logging.Named("orchestration"), convID, eventType, msg, level, logMessage, fields...)
}

func loggedEventWith(log *zap.Logger, convID uuid.UUID, eventType, msg string, level zapcore.Level, logMessage string, fields ...zap.Field) realtime.Event {
	base := []zap.Field{
		zap.String("conversation_id", convID.String()),
		zap.String("detail", msg),
	}
	log.Log(level, logMessage, append(base, fields...)...)
	return realtime.Event{Type: eventType, ConversationID: convID.String(), Error: msg}
}

func domainEvent(convID uuid.UUID, err error) realtime.Event {
	msg := humanErr(err)
	switch {
	case errors.Is(err, conversation.ErrNotActive), errors.Is(err, storage.ErrNotFound), errors.Is(err, storage.ErrGenerationInProgress),
		errors.Is(err, storage.ErrTurnInProgress), errors.Is(err, storage.ErrContinueNotAllowed), errors.Is(err, storage.ErrRegenerateNotAllowed):
		return infoEvent(convID, msg, zap.Error(err))
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return infoEvent(convID, msg)
	default:
		return errorEvent(convID, msg, zap.Error(err))
	}
}

// humanErr 把常见错误翻译为用户可读文案。
func humanErr(err error) string {
	switch {
	case errors.Is(err, conversation.ErrNotActive):
		return "对话未处于 active 状态，无法生成"
	case errors.Is(err, storage.ErrNotFound):
		return "对话不存在"
	case errors.Is(err, storage.ErrGenerationInProgress):
		return "当前对话正在生成中，请稍候"
	case errors.Is(err, storage.ErrContinueNotAllowed):
		return "当前回合不能继续叙事"
	case errors.Is(err, storage.ErrRegenerateNotAllowed):
		return "当前回合不能重新生成"
	default:
		return err.Error()
	}
}
