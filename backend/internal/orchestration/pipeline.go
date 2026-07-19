package orchestration

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/ialtone/fatecraft/internal/contextbuilder"
	"github.com/ialtone/fatecraft/internal/llm"
	"github.com/ialtone/fatecraft/internal/logging"
	"github.com/ialtone/fatecraft/internal/protocol"
	"github.com/ialtone/fatecraft/internal/realtime"
	"github.com/ialtone/fatecraft/internal/storage"
)

// passOptions 描述一趟生成管线的差异点（普通第一趟 vs 判定第二趟）。
type passOptions struct {
	displayID    uuid.UUID  // 本趟流式增量与最终 assistant 消息共用的展示 id
	triggerMsgID *uuid.UUID // 触发本趟生成的消息 id（仅写入上下文构建日志）
	logBuild     bool       // 是否保存上下文构建日志并推送 context_built（判定第二趟不落）
	startedEarly bool       // generation_started 已由调用方在装配上下文前发出（判定第二趟的事件顺序）
}

// passResult 是一趟管线到「解析信封」为止的产物。
type passResult struct {
	env   protocol.Envelope
	raw   string    // 模型原始输出（narrative 解析为空时的兜底展示）
	logID uuid.UUID // 上下文构建日志 id，logOK 为 true 时有效
	logOK bool      // 构建日志是否成功落库（失败则不把悬空 id 写进消息元数据）
}

// runGeneration 生成主流程：跑一趟管线；检测到判定请求则转入两阶段判定流程，
// 否则直接做单趟收尾（落库/状态/动作/事件）。
func (g *Generator) runGeneration(ctx context.Context, conv *storage.Conversation, triggerMsgID *uuid.UUID, emit realtime.EmitFunc) {
	start := time.Now()
	convID := conv.ID

	settings, err := g.settings.Get(ctx)
	if err != nil {
		emit(errorEvent(convID, "读取 LLM 设置失败: "+err.Error()))
		return
	}

	displayID := uuid.New()
	pass, ok := g.runPass(ctx, conv, settings, passOptions{
		displayID:    displayID,
		triggerMsgID: triggerMsgID,
		logBuild:     true,
	}, emit)
	if !ok {
		return
	}

	// 检测判定请求（两阶段判定协议）。
	// 优先使用对话级配置，如果未设置则使用全局配置。
	rollEnabled := settings.RollSystemEnabled
	if conv.RollSystemEnabled != nil {
		rollEnabled = *conv.RollSystemEnabled
	}
	if rollEnabled && pass.env.RollRequest != nil {
		g.runRollFlow(ctx, conv, settings, pass.env, displayID, emit)
		return
	}

	// 单趟收尾。
	meta := map[string]any{
		"model":       settings.Model,
		"duration_ms": time.Since(start).Milliseconds(),
	}
	if pass.logOK {
		meta["context_build_id"] = pass.logID.String()
	}
	g.finalizePass(ctx, convID, displayID, pass.env, pass.raw, meta, true, emit)
}

// runPass 跑一趟生成管线：装配上下文 →（可选）记录构建日志 → 调用模型 → 解析信封。
// 各阶段的事件顺序与错误文案与旧实现保持一致；失败时自行推送错误事件并返回 ok=false。
func (g *Generator) runPass(ctx context.Context, conv *storage.Conversation, settings *storage.LLMSettings, opts passOptions, emit realtime.EmitFunc) (passResult, bool) {
	convID := conv.ID

	// 装配上下文输入并按预算构建。
	in, err := g.assembleInput(ctx, conv, settings)
	if err != nil {
		emit(errorEvent(convID, "装配上下文失败: "+err.Error()))
		return passResult{}, false
	}
	result, err := contextbuilder.Build(in, g.counter)
	if err != nil {
		emit(errorEvent(convID, humanErr(err)))
		return passResult{}, false
	}

	res := passResult{}
	if opts.logBuild {
		res.logID, res.logOK = g.logContextBuild(ctx, convID, opts.triggerMsgID, settings, result, emit)
	}

	// 请求模型。流式开关打开时走 SSE 并实时下发剧情增量，否则一次性补全。
	if !opts.startedEarly {
		emit(realtime.Event{Type: realtime.TypeGenerationStarted, ConversationID: convID.String(), MessageID: opts.displayID.String()})
	}
	raw, err := g.invokeModel(ctx, settings, convID, opts.displayID, result.Messages, emit)
	if err != nil {
		// 取消与真实失败区分提示；取消时不再落库。
		if ctx.Err() != nil {
			emit(errorEvent(convID, "生成已取消"))
		} else {
			emit(errorEvent(convID, "模型请求失败: "+err.Error()))
		}
		return passResult{}, false
	}

	res.env = protocol.ParseEnvelope(raw)
	res.raw = raw
	return res, true
}

// logContextBuild 保存上下文构建日志并推送 context_built 事件。
// 写失败只记日志（返回 ok=false），后续不把悬空的 context_build_id 写进消息元数据。
func (g *Generator) logContextBuild(ctx context.Context, convID uuid.UUID, triggerMsgID *uuid.UUID, settings *storage.LLMSettings, result *contextbuilder.Result, emit realtime.EmitFunc) (uuid.UUID, bool) {
	logID := uuid.New()
	logErr := g.ctxRepo.Create(ctx, &storage.ContextBuildLog{
		ID:                     logID,
		ConversationID:         convID,
		TriggerMessageID:       triggerMsgID,
		Model:                  settings.Model,
		MaxContextTokens:       settings.MaxContextTokens,
		ReservedResponseTokens: settings.MaxResponseTokens,
		UsedPromptTokens:       result.UsedPromptTokens,
		Segments:               toJSONDoc(result.Segments),
		IncludedMessageIDs:     uuidsToStrings(result.IncludedMessageIDs),
		ActivatedWorldEntryIDs: result.ActivatedEntryIDs,
		CreatedAt:              time.Now(),
	})
	if logErr != nil {
		logging.Named("orchestration").Warn("保存上下文构建日志失败",
			zap.String("conversation_id", convID.String()), zap.Error(logErr))
	}
	emit(realtime.Event{Type: realtime.TypeContextBuilt, ConversationID: convID.String(), ContextBuildID: logID.String()})
	return logID, logErr == nil
}

// invokeModel 调用模型并返回原始输出（按设置走流式或一次性补全）。
// 流式模式参照 SillyTavern 的处理：每个增量到达后对「累积全文」重新提取
// narrative 已生成前缀（处理 JSON 信封/转义/围栏），只把新增的可见部分
// 推给前端，避免玩家看到裸 JSON。
func (g *Generator) invokeModel(ctx context.Context, settings *storage.LLMSettings, convID, msgID uuid.UUID, messages []llm.Message, emit realtime.EmitFunc) (string, error) {
	apiKey := settings.APIKey
	if apiKey == "" {
		apiKey = g.apiKeyEnv
	}

	// 构造 response_format 参数（如果 Schema 已注入）。
	var responseFormat *llm.ResponseFormat
	if g.responseSchema != nil {
		responseFormat = &llm.ResponseFormat{
			Type:       "json_schema",
			JSONSchema: g.responseSchema,
		}
	}

	params := llm.Params{
		BaseURL: settings.BaseURL, APIKey: apiKey, Model: settings.Model,
		Temperature: settings.Temperature, TopP: settings.TopP, MaxTokens: settings.MaxResponseTokens,
		ResponseFormat: responseFormat,
	}

	if !settings.StreamEnabled {
		return g.llmClient.Complete(ctx, params, messages)
	}

	var accumulated strings.Builder
	lastNarrative := ""
	return g.llmClient.Stream(ctx, params, messages, func(delta string) {
		accumulated.WriteString(delta)
		cur := protocol.ExtractPartialNarrative(accumulated.String())
		if len(cur) > len(lastNarrative) {
			visible := cur[len(lastNarrative):]
			lastNarrative = cur
			emit(realtime.Event{Type: realtime.TypeGenerationDelta, ConversationID: convID.String(), MessageID: msgID.String(), Delta: visible})
		}
	})
}

// finalizePass 一趟管线的通用收尾：落库 assistant 消息 → 应用状态更新 →
// 处理 AI 动作 → 推送 message_created / generation_finished → 刷新对话时间。
// countTokens 控制是否统计消息 token（判定两阶段产生的消息与旧实现一致不统计）。
func (g *Generator) finalizePass(ctx context.Context, convID, displayID uuid.UUID, env protocol.Envelope, raw string, meta map[string]any, countTokens bool, emit realtime.EmitFunc) {
	// 内部消息内容用干净的剧情文本；解析不出 narrative 时降级为原始输出。
	narrative := strings.TrimSpace(env.Narrative)
	if narrative == "" {
		narrative = strings.TrimSpace(raw)
	}

	var tokens *int
	if countTokens {
		t := g.counter.Count(narrative)
		tokens = &t
	}
	if env.Notes != "" {
		meta["notes"] = env.Notes
	}

	chatMsg, disp, err := g.chatSvc.AppendAssistantWithID(ctx, convID, displayID, narrative, narrative, meta, tokens)
	if err != nil {
		emit(errorEvent(convID, "保存回复失败: "+err.Error()))
		return
	}

	// 校验并应用状态更新。
	if len(env.StateUpdate) > 0 {
		after, skipped, err := g.stateSvc.Apply(ctx, convID, env.StateUpdate, storage.SourceAI, &chatMsg.ID)
		if err == nil {
			meta["applied_state"] = env.StateUpdate
			if len(skipped) > 0 {
				meta["skipped_state"] = skipped
			}
			disp.Metadata = storage.JSONMap(meta)
			// 回写落库：applied_state 此前只存在于内存与 WS 事件里，刷新后即丢失。
			if uerr := g.chatSvc.UpdateDisplayMetadata(ctx, convID, displayID, meta); uerr != nil {
				logging.Named("orchestration").Warn("回写消息 metadata 失败",
					zap.String("conversation_id", convID.String()), zap.Error(uerr))
			}
			emit(realtime.Event{Type: realtime.TypeStateUpdate, ConversationID: convID.String(), State: after})
		} else {
			// 状态应用失败不再完全静默：留日志便于排障（不中断叙事主流程）。
			logging.Named("orchestration").Warn("状态更新应用失败",
				zap.String("conversation_id", convID.String()), zap.Error(err))
		}
	}

	// 检测并处理 AI 动作（按注册表分发）。
	if env.Action != nil {
		g.handleAction(ctx, convID, env.Action, emit)
	}

	emit(realtime.Event{Type: realtime.TypeMessageCreated, ConversationID: convID.String(), MessageID: displayID.String(), Message: disp})
	emit(realtime.Event{Type: realtime.TypeGenerationFinished, ConversationID: convID.String(), MessageID: displayID.String()})
	_ = g.convSvc.Touch(ctx, convID)
}
