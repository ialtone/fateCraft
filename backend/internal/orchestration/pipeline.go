package orchestration

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/ialtone/fatecraft/internal/challenge"
	"github.com/ialtone/fatecraft/internal/contextbuilder"
	"github.com/ialtone/fatecraft/internal/llm"
	"github.com/ialtone/fatecraft/internal/logging"
	"github.com/ialtone/fatecraft/internal/protocol"
	"github.com/ialtone/fatecraft/internal/realtime"
	"github.com/ialtone/fatecraft/internal/storage"
)

// passOptions 描述一趟生成管线的差异点（普通第一趟 vs 判定第二趟）。
type passOptions struct {
	displayID        uuid.UUID  // 本趟流式增量与最终 assistant 消息共用的展示 id
	triggerMsgID     *uuid.UUID // 触发本趟生成的消息 id（仅写入上下文构建日志）
	logBuild         bool       // 是否保存上下文构建日志并推送 context_built（判定第二趟不落）
	startedEarly     bool       // generation_started 已由调用方在装配上下文前发出（判定第二趟的事件顺序）
	runtimeDirective string     // 仅本趟有效的必选服务端指令
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

	settings, err := g.settings.Get(ctx, g.identity.CurrentPlayerID(ctx))
	if err != nil {
		emit(errorEvent(convID, "读取 LLM 设置失败: "+err.Error()))
		return
	}
	rollSettings := storage.DefaultSystemSettings()
	if g.systemSettings != nil {
		rollSettings, err = g.systemSettings.Get(ctx)
		if err != nil {
			emit(errorEvent(convID, "读取平台判定设置失败: "+err.Error()))
			return
		}
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

	// 判定开关与难度只读取管理员维护的全局设置，不信任用户或对话请求字段。
	if rollSettings.RollSystemEnabled && pass.env.RollRequest != nil {
		g.runRollFlow(ctx, conv, settings, rollSettings.RollDifficultyMode, pass.env, displayID, emit)
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
	g.finalizeCandidate(ctx, conv, settings, pass, displayID, triggerMsgID, meta, true, true, emit)
}

// runPass 跑一趟生成管线：装配上下文 →（可选）记录构建日志 → 调用模型 → 解析信封。
// 各阶段的事件顺序与错误文案与旧实现保持一致；失败时自行推送错误事件并返回 ok=false。
func (g *Generator) runPass(ctx context.Context, conv *storage.Conversation, settings *storage.LLMSettings, opts passOptions, emit realtime.EmitFunc) (passResult, bool) {
	convID := conv.ID

	// 装配上下文输入并按预算构建。
	in, err := g.assembleInput(ctx, conv, settings, opts.runtimeDirective)
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
		res.logID, res.logOK = g.logContextBuild(ctx, conv, opts.triggerMsgID, settings, result, emit)
	}

	// 请求模型。流式开关打开时走 SSE 并实时下发剧情增量，否则一次性补全。
	if !opts.startedEarly {
		emit(realtime.Event{Type: realtime.TypeGenerationStarted, ConversationID: convID.String(), MessageID: opts.displayID.String()})
	}
	raw, err := g.invokeModel(ctx, settings, convID, opts.displayID, result.Messages, emit)
	if err != nil {
		// 取消与真实失败区分提示；取消时不再落库。
		if ctx.Err() != nil {
			emit(infoEvent(convID, "生成已取消"))
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
func (g *Generator) logContextBuild(ctx context.Context, conv *storage.Conversation, triggerMsgID *uuid.UUID, settings *storage.LLMSettings, result *contextbuilder.Result, emit realtime.EmitFunc) (uuid.UUID, bool) {
	convID := conv.ID
	logID := uuid.New()
	logErr := g.ctxRepo.Create(ctx, &storage.ContextBuildLog{
		ID:                     logID,
		ConversationID:         convID,
		TriggerMessageID:       triggerMsgID,
		ChallengeVersionID:     conv.ChallengeVersionID,
		ChallengeHash:          conv.ChallengeHash,
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

// finalizeCandidate 在落库前预校验动作。拒绝时丢弃流式候选并带着服务端原因
// 重试一次；第二次仍被拒绝则落一条无动作、无状态更新的后端兜底叙事。
func (g *Generator) finalizeCandidate(
	ctx context.Context,
	conv *storage.Conversation,
	settings *storage.LLMSettings,
	pass passResult,
	displayID uuid.UUID,
	triggerMsgID *uuid.UUID,
	meta map[string]any,
	countTokens bool,
	logRetryBuild bool,
	emit realtime.EmitFunc,
) {
	prepared, err := g.prepareAction(ctx, conv.ID, pass.env.Action)
	if err == nil {
		g.finalizePass(ctx, conv.ID, displayID, pass.env, pass.raw, meta, countTokens, prepared, emit)
		return
	}

	g.discardCandidate(conv.ID, displayID, err, emit)
	if ctx.Err() != nil {
		return
	}
	if !errors.Is(err, challenge.ErrSettlementDenied) {
		emit(errorEvent(conv.ID, "结局动作预校验失败，请稍后重试", zap.Error(err)))
		return
	}

	retryID := uuid.New()
	emit(realtime.Event{Type: realtime.TypeGenerationStarted, ConversationID: conv.ID.String(), MessageID: retryID.String()})
	retry, ok := g.runPass(ctx, conv, settings, passOptions{
		displayID: retryID, triggerMsgID: triggerMsgID, logBuild: logRetryBuild,
		runtimeDirective: rejectedEndingDirective(err), startedEarly: true,
	}, emit)
	if !ok || ctx.Err() != nil {
		return
	}

	var retryPrepared PreparedAction
	var retryErr error
	if retry.env.RollRequest != nil {
		retryErr = errors.New("结局纠错回复不得同时发起新的判定")
	} else if retry.env.Action != nil {
		retryErr = errors.New("结局纠错回复不得再次触发动作")
	} else {
		retryPrepared, retryErr = g.prepareAction(ctx, conv.ID, nil)
	}
	retryMeta := cloneMetadata(meta)
	retryMeta["security_retry"] = true
	delete(retryMeta, "context_build_id")
	if retry.logOK {
		retryMeta["context_build_id"] = retry.logID.String()
	}
	if retryErr == nil {
		g.finalizePass(ctx, conv.ID, retryID, retry.env, retry.raw, retryMeta, countTokens, retryPrepared, emit)
		return
	}

	g.discardCandidate(conv.ID, retryID, retryErr, emit)
	retryMeta["security_fallback"] = true
	retryMeta["ending_rejection"] = retryErr.Error()
	fallback := protocol.Envelope{
		Narrative:   "天道规则拒绝了这次结局：当前经历尚未满足对应的超脱条件。你仍停留在原处，可以继续修行、探索并通过必要的天命判定。",
		StateUpdate: map[string]any{},
	}
	g.finalizePass(ctx, conv.ID, retryID, fallback, "", retryMeta, countTokens, nil, emit)
}

func (g *Generator) discardCandidate(convID, displayID uuid.UUID, reason error, emit realtime.EmitFunc) {
	logging.Named("orchestration").Warn("模型结局动作预校验未通过，候选回复已丢弃",
		zap.String("conversation_id", convID.String()),
		zap.String("message_id", displayID.String()),
		zap.Error(reason))
	emit(realtime.Event{
		Type: realtime.TypeGenerationDiscarded, ConversationID: convID.String(), MessageID: displayID.String(),
	})
}

func rejectedEndingDirective(err error) string {
	reason := strings.TrimSpace(err.Error())
	if len(reason) > 1000 {
		reason = reason[:1000]
	}
	return "上一份候选回复已被服务端丢弃，不能视为已经发生的剧情或状态。拒绝原因：" + reason +
		"。你必须服从服务端已有状态和判定流水，撤销成功结局，继续尚未完成的普通叙事；不得沿用被拒绝回复中的事件、物品、境界、奖励或结论，也不得再次虚构此前发生过这些内容。本次纠错回复不得触发结局，也不得发起新的判定。"
}

func cloneMetadata(meta map[string]any) map[string]any {
	cloned := make(map[string]any, len(meta)+2)
	for key, value := range meta {
		cloned[key] = value
	}
	return cloned
}

// finalizePass 一趟管线的通用收尾：落库 assistant 消息 → 应用状态更新 →
// 提交已预校验动作 → 推送 message_created / generation_finished → 刷新对话时间。
// countTokens 控制是否统计消息 token（判定两阶段产生的消息与旧实现一致不统计）。
func (g *Generator) finalizePass(ctx context.Context, convID, displayID uuid.UUID, env protocol.Envelope, raw string, meta map[string]any, countTokens bool, prepared PreparedAction, emit realtime.EmitFunc) {
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

	// 动作输入已在本次回复落库前冻结，不能读取刚应用的 AI 状态来抬高资格或奖励。
	if prepared != nil {
		prepared.Commit(ctx, emit)
	}

	emit(realtime.Event{Type: realtime.TypeMessageCreated, ConversationID: convID.String(), MessageID: displayID.String(), Message: disp})
	emit(realtime.Event{Type: realtime.TypeGenerationFinished, ConversationID: convID.String(), MessageID: displayID.String()})
	_ = g.convSvc.Touch(ctx, convID)
}
