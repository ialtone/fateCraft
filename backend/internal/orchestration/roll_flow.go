package orchestration

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/ialtone/fatecraft/internal/logging"
	"github.com/ialtone/fatecraft/internal/protocol"
	"github.com/ialtone/fatecraft/internal/realtime"
	"github.com/ialtone/fatecraft/internal/roll"
	"github.com/ialtone/fatecraft/internal/storage"
)

// runRollFlow 处理判定请求（两阶段判定协议，使用组合判定系统）。
//
// 与旧实现的等价关系：第一阶段落库判定请求叙事 + 执行判定 + 落库判定结果，
// 第二阶段把「判定结果」作为新上下文再跑一趟管线（复用 runPass），
// 消灭对第一趟「装配→调用→解析→落库→应用→推事件」的整段复制。
func (g *Generator) runRollFlow(
	ctx context.Context,
	conv *storage.Conversation,
	settings *storage.LLMSettings,
	difficultyMode string,
	env protocol.Envelope,
	displayID uuid.UUID,
	emit realtime.EmitFunc,
) {
	convID := conv.ID
	req := env.RollRequest

	// 客户端已取消则不落库，直接结束。
	if ctx.Err() != nil {
		emit(infoEvent(convID, "生成已取消"))
		return
	}

	// 1. 保存第一阶段的叙事（包含判定请求）。
	narrative := strings.TrimSpace(env.Narrative)
	if narrative == "" {
		narrative = "(判定请求)"
	}
	meta := map[string]any{"roll_request": req}
	firstChat, disp, err := g.chatSvc.AppendAssistantWithID(ctx, convID, displayID, narrative, narrative, meta, nil)
	if err != nil {
		emit(errorEvent(convID, "保存判定请求失败: "+err.Error()))
		return
	}

	// 第一阶段若带 state_update（strict schema 下必然输出，可能含实际消耗如"用掉一张雷符"），
	// 在此应用，避免叙述的消耗未记账。动作（action）留到第二阶段收尾统一处理。
	if len(env.StateUpdate) > 0 {
		if after, skipped, aerr := g.stateSvc.Apply(ctx, convID, env.StateUpdate, storage.SourceAI, &firstChat.ID); aerr == nil {
			meta["applied_state"] = env.StateUpdate
			if len(skipped) > 0 {
				meta["skipped_state"] = skipped
			}
			disp.Metadata = storage.JSONMap(meta)
			if uerr := g.chatSvc.UpdateDisplayMetadata(ctx, convID, disp.ID, meta); uerr != nil {
				logging.Named("orchestration").Warn("回写第一阶段 applied_state 失败",
					zap.String("conversation_id", convID.String()), zap.Error(uerr))
			}
			emit(realtime.Event{Type: realtime.TypeStateUpdate, ConversationID: convID.String(), State: after})
		} else {
			logging.Named("orchestration").Warn("第一阶段状态应用失败",
				zap.String("conversation_id", convID.String()), zap.Error(aerr))
		}
	}
	emit(realtime.Event{Type: realtime.TypeMessageCreated, ConversationID: convID.String(), MessageID: displayID.String(), Message: disp})
	// 闭合第一阶段的流式协议：displayID 的生成到此结束，
	// 第二阶段的增量与最终事件统一使用新的 finalID。
	emit(realtime.Event{Type: realtime.TypeGenerationFinished, ConversationID: convID.String(), MessageID: displayID.String()})

	// 2. 获取玩家状态。
	playerState, err := g.stateSvc.Get(ctx, convID)
	if err != nil {
		emit(errorEvent(convID, "获取玩家状态失败: "+err.Error()))
		return
	}

	// 3. 计算判定结果（判定服务由组合根注入，取代旧的全局单例）。
	if g.roll == nil {
		emit(errorEvent(convID, "判定配置未加载"))
		return
	}

	detailedResult := g.roll.CalculateResult(ctx, req, playerState, convID, difficultyMode)
	outcome := roll.ExecuteRoll(detailedResult, int(req.Sides))

	// 4. 记录历史。
	g.roll.Record(ctx, convID, req.Type, outcome.Success)

	// 5. 保存判定结果：进上下文（system 内部消息）+ 前端展示，两者同时落库。
	// 第二阶段 runPass 装配历史时会带上这条 system 消息，模型据此知道判定输赢；
	// 上下文文本额外附一句续写指令，防止模型忽略结果或再次发起判定。
	resultText := roll.FormatResult(req.Type, req.Description, outcome)
	resultContext := resultText + "\n（这是系统判定结果，请严格依据该结果继续叙事，不要再次发起判定。）"
	resultMeta := map[string]any{
		"roll_outcome": outcome,
		"roll_type":    req.Type,
	}
	_, resultDisp, err := g.chatSvc.AppendSystemChatEvent(ctx, convID, "roll_result", resultContext, resultText, resultMeta)
	if err != nil {
		emit(errorEvent(convID, "保存判定结果失败: "+err.Error()))
		return
	}
	emit(realtime.Event{Type: realtime.TypeMessageCreated, ConversationID: convID.String(), MessageID: resultDisp.ID.String(), Message: resultDisp})

	// 客户端已取消则不再发起第二阶段。
	if ctx.Err() != nil {
		emit(infoEvent(convID, "生成已取消"))
		return
	}

	// 6. 再次调用 AI，让它根据判定结果继续叙事。
	// 第二阶段使用独立的 finalID 推送增量与最终事件，保证事件协议闭合；
	// generation_started 先于装配上下文发出，与旧实现的事件顺序一致。
	finalID := uuid.New()
	emit(realtime.Event{Type: realtime.TypeGenerationStarted, ConversationID: convID.String(), MessageID: finalID.String()})

	pass, ok := g.runPass(ctx, conv, settings, passOptions{
		displayID:    finalID,
		logBuild:     false,
		startedEarly: true,
	}, emit)
	if !ok {
		return
	}
	// runPass 内部已在真实失败/取消时推送错误事件；这里再补一次取消检查，
	// 覆盖"流式读取正常结束但 ctx 期间被取消"的情形，取消时不落库。
	if ctx.Err() != nil {
		emit(infoEvent(convID, "生成已取消"))
		return
	}

	env2 := pass.env
	// 二阶段协议不允许再发起判定：若模型再次输出 roll_request，忽略并记录日志，
	// 且避免把裸 JSON 信封当作剧情落库。
	if env2.RollRequest != nil {
		logging.Named("orchestration").Warn("判定第二阶段再次收到 roll_request，已忽略",
			zap.String("conversation_id", convID.String()))
		if strings.TrimSpace(env2.Narrative) == "" {
			env2.Narrative = "（天机紊乱，本次判定后的叙事生成异常，请重试或继续。）"
		}
	}

	// 第二阶段收尾：动作仍须在落库前预校验；无效结局会丢弃并重试一次。
	finalMeta := map[string]any{
		"model":          settings.Model,
		"after_roll":     true,
		"roll_result_id": resultDisp.ID.String(),
	}
	pass.env = env2
	g.finalizeCandidate(ctx, conv, settings, pass, finalID, nil, finalMeta, false, false, emit)
}
