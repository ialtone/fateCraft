// Package orchestration 是生成编排（application 层）：
// 承担「装配上下文 → LLM 调用 → 解析信封 → 落库 → 应用状态 → 处理动作 → 推送事件」
// 的完整管线，并以 ActionHandler 注册表分发玩法相关的 AI 动作，保持编排器题材中立。
//
// 事件类型与 EmitFunc 定义在 realtime 包（WebSocket 事件协议所在地）；
// realtime.Hub 通过其消费端 Generator 接口反向依赖本包实现，两包之间无环。
package orchestration

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/ialtone/fatecraft/internal/challenge"
	"github.com/ialtone/fatecraft/internal/chat"
	"github.com/ialtone/fatecraft/internal/conversation"
	"github.com/ialtone/fatecraft/internal/identity"
	"github.com/ialtone/fatecraft/internal/llm"
	"github.com/ialtone/fatecraft/internal/logging"
	"github.com/ialtone/fatecraft/internal/memory"
	"github.com/ialtone/fatecraft/internal/realtime"
	"github.com/ialtone/fatecraft/internal/roll"
	"github.com/ialtone/fatecraft/internal/state"
	"github.com/ialtone/fatecraft/internal/storage"
	"github.com/ialtone/fatecraft/internal/tokenizer"
)

// Generator 编排一次完整的生成流程（doc 06 §请求流程）。
// 实现 realtime.Generator 接口，供 Hub 与 REST 触发的生成复用同一条管线。
type Generator struct {
	convSvc        *conversation.Service
	chatSvc        *chat.Service
	stateSvc       *state.Service
	memSvc         *memory.Service
	challenge      *challenge.Service
	settings       *storage.SettingsRepo
	systemSettings *storage.SystemSettingsRepo
	ctxRepo        *storage.ContextRepo
	llmClient      *llm.Client
	counter        tokenizer.Counter
	apiKeyEnv      string                   // 数据库中 api_key 为空时使用的兜底密钥
	roll           *roll.Service            // 判定服务；nil 表示判定系统不可用
	actions        map[string]ActionHandler // AI 动作处理器注册表（按 action.type 分发）
	responseSchema *llm.JSONSchema          // 结构化输出 Schema；nil 时不启用 response_format
	identity       identity.Provider
}

// GeneratorDeps 是构造 Generator 的依赖集合（具名字段，取代旧的 16 个位置参数）。
type GeneratorDeps struct {
	Conv      *conversation.Service
	Chat      *chat.Service
	State     *state.Service
	Memory    *memory.Service
	Challenge *challenge.Service
	Settings  *storage.SettingsRepo
	System    *storage.SystemSettingsRepo
	CtxLogs   *storage.ContextRepo
	LLM       *llm.Client
	Counter   tokenizer.Counter
	APIKeyEnv string // 数据库中 api_key 为空时使用的兜底密钥

	// Roll 判定服务（两阶段判定协议）；为 nil 时收到判定请求会报"判定配置未加载"。
	Roll *roll.Service
	// Actions 按 action.type 注册的玩法动作处理器；未注册的动作类型直接忽略。
	Actions map[string]ActionHandler
	// ResponseSchema 结构化输出 Schema，由组合根加载注入；nil 时降级为不启用 response_format。
	ResponseSchema *llm.JSONSchema
	// Identity 从生成 context 中解析当前用户，以读取其独立 LLM 设置。
	Identity identity.Provider
}

// NewGenerator 构造生成编排器。Schema 不再由本包自行探测文件系统，改由组合根注入。
func NewGenerator(d GeneratorDeps) *Generator {
	return &Generator{
		convSvc: d.Conv, chatSvc: d.Chat, stateSvc: d.State, memSvc: d.Memory, challenge: d.Challenge,
		settings: d.Settings, systemSettings: d.System, ctxRepo: d.CtxLogs,
		llmClient: d.LLM, counter: d.Counter, apiKeyEnv: d.APIKeyEnv,
		roll: d.Roll, actions: d.Actions, responseSchema: d.ResponseSchema,
		identity: d.Identity,
	}
}

// Generate 处理一次用户输入：保存用户消息并生成 AI 回复。
func (g *Generator) Generate(ctx context.Context, convID uuid.UUID, userContent string, emit realtime.EmitFunc) {
	conv, err := g.convSvc.EnsureActive(ctx, convID)
	if err != nil {
		emit(domainEvent(convID, err))
		return
	}
	locked, err := g.convSvc.TryLock(ctx, convID)
	if err != nil {
		emit(errorEvent(convID, "加锁失败: "+err.Error()))
		return
	}
	if !locked {
		emit(infoEvent(convID, "当前对话正在生成中，请稍候"))
		return
	}
	defer g.convSvc.Unlock(context.Background(), convID)

	// 保存用户消息并推送。
	uTokens := g.counter.Count(userContent)
	uChat, uDisp, err := g.chatSvc.AppendUser(ctx, convID, userContent, &uTokens)
	if err != nil {
		emit(errorEvent(convID, "保存用户消息失败: "+err.Error()))
		return
	}
	emit(realtime.Event{Type: realtime.TypeMessageCreated, ConversationID: convID.String(), Message: uDisp})
	_ = g.convSvc.Touch(ctx, convID)

	g.runGeneration(ctx, conv, &uChat.ID, emit)
}

// Regenerate 删除最近一条 AI 回复后重新生成。
func (g *Generator) Regenerate(ctx context.Context, convID uuid.UUID, emit realtime.EmitFunc) {
	conv, err := g.convSvc.EnsureActive(ctx, convID)
	if err != nil {
		emit(domainEvent(convID, err))
		return
	}
	locked, err := g.convSvc.TryLock(ctx, convID)
	if err != nil {
		emit(errorEvent(convID, "加锁失败: "+err.Error()))
		return
	}
	if !locked {
		emit(infoEvent(convID, "当前对话正在生成中，请稍候"))
		return
	}
	defer g.convSvc.Unlock(context.Background(), convID)

	if err := g.chatSvc.DropLastAssistant(ctx, convID); err != nil && err != storage.ErrNotFound {
		emit(errorEvent(convID, "删除上一条回复失败: "+err.Error()))
		return
	}
	g.runGeneration(ctx, conv, nil, emit)
}

// GenerateAfterEdit 在用户消息已被编辑、后续消息已删除的前提下直接重新生成
// （天道回溯）。不再追加用户消息，避免上下文出现重复内容。
func (g *Generator) GenerateAfterEdit(ctx context.Context, convID uuid.UUID, emit realtime.EmitFunc) {
	conv, err := g.convSvc.EnsureActive(ctx, convID)
	if err != nil {
		emit(domainEvent(convID, err))
		return
	}
	locked, err := g.convSvc.TryLock(ctx, convID)
	if err != nil {
		emit(errorEvent(convID, "加锁失败: "+err.Error()))
		return
	}
	if !locked {
		emit(infoEvent(convID, "当前对话正在生成中，请稍候"))
		return
	}
	defer g.convSvc.Unlock(context.Background(), convID)

	g.runGeneration(ctx, conv, nil, emit)
}

// Continue 在不新增用户输入的情况下追加一段 AI 续写。
// 通过添加仅进入上下文的"继续"消息来触发 AI 接续上文。
func (g *Generator) Continue(ctx context.Context, convID uuid.UUID, emit realtime.EmitFunc) {
	conv, err := g.convSvc.EnsureActive(ctx, convID)
	if err != nil {
		emit(domainEvent(convID, err))
		return
	}
	locked, err := g.convSvc.TryLock(ctx, convID)
	if err != nil {
		emit(errorEvent(convID, "加锁失败: "+err.Error()))
		return
	}
	if !locked {
		emit(infoEvent(convID, "当前对话正在生成中，请稍候"))
		return
	}
	defer g.convSvc.Unlock(context.Background(), convID)

	// ctx 已取消（如客户端立即点了取消）则不落任何消息。
	if ctx.Err() != nil {
		emit(infoEvent(convID, "生成已取消"))
		return
	}

	// 添加只进上下文、不产生展示消息的"继续"触发（仿 seedAutoTrigger），
	// 刷新后 UI 不会出现这条消息。
	continueContent := "继续"
	uTokens := g.counter.Count(continueContent)
	uChat, err := g.chatSvc.AppendUserChatOnly(ctx, convID, continueContent, &uTokens)
	if err != nil {
		emit(errorEvent(convID, "保存继续消息失败: "+err.Error()))
		return
	}
	_ = g.convSvc.Touch(ctx, convID)

	g.runGeneration(ctx, conv, &uChat.ID, emit)
}

// WebSocket 仍使用 error 事件向前端展示无法继续的原因；服务端日志级别由
// 业务语义决定，不能把所有客户端 error 事件都当成系统 ERROR。
func errorEvent(convID uuid.UUID, msg string, fields ...zap.Field) realtime.Event {
	return loggedEvent(convID, msg, zapcore.ErrorLevel, "生成流程故障", fields...)
}

func warningEvent(convID uuid.UUID, msg string, fields ...zap.Field) realtime.Event {
	return loggedEvent(convID, msg, zapcore.WarnLevel, "生成请求未通过", fields...)
}

func infoEvent(convID uuid.UUID, msg string, fields ...zap.Field) realtime.Event {
	return loggedEvent(convID, msg, zapcore.InfoLevel, "生成流程未执行", fields...)
}

func loggedEvent(convID uuid.UUID, msg string, level zapcore.Level, logMessage string, fields ...zap.Field) realtime.Event {
	return loggedEventWith(logging.Named("orchestration"), convID, msg, level, logMessage, fields...)
}

func loggedEventWith(log *zap.Logger, convID uuid.UUID, msg string, level zapcore.Level, logMessage string, fields ...zap.Field) realtime.Event {
	base := []zap.Field{
		zap.String("conversation_id", convID.String()),
		zap.String("detail", msg),
	}
	log.Log(level, logMessage, append(base, fields...)...)
	return realtime.Event{Type: realtime.TypeError, ConversationID: convID.String(), Error: msg}
}

func domainEvent(convID uuid.UUID, err error) realtime.Event {
	msg := humanErr(err)
	switch {
	case errors.Is(err, conversation.ErrNotActive), errors.Is(err, storage.ErrNotFound):
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
	default:
		return err.Error()
	}
}
