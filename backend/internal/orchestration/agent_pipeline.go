package orchestration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/ialtone/fatecraft/internal/contextbuilder"
	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/guidance"
	"github.com/ialtone/fatecraft/internal/llm"
	"github.com/ialtone/fatecraft/internal/logging"
	"github.com/ialtone/fatecraft/internal/operation"
	"github.com/ialtone/fatecraft/internal/realtime"
	"github.com/ialtone/fatecraft/internal/storage"
)

const (
	maxAgentModelSteps       = 8
	maxAgentMutatingTools    = 6
	maxAgentAttemptDuration  = 10 * time.Minute
	maxPublicToolResultBytes = 16 * 1024
)

var (
	errAgentToolOutcomeRollback = errors.New("rollback rejected or failed agent tool outcome")
	errAgentToolPersistence     = errors.New("工具记录保存失败")
)

type runtimeEmitter struct {
	conversationID uuid.UUID
	operationID    uuid.UUID
	turnID         uuid.UUID
	attemptID      uuid.UUID
	sequence       int64
	emit           realtime.EmitFunc
	messageID      string
}

func newRuntimeEmitter(ctx context.Context, conversationID uuid.UUID, emit realtime.EmitFunc) *runtimeEmitter {
	metadata, _ := attemptMetadata(ctx)
	generation, _ := operation.GenerationFromContext(ctx)
	return &runtimeEmitter{
		conversationID: conversationID, operationID: generation.ID, turnID: metadata.TurnID,
		attemptID: metadata.AttemptID, emit: emit,
	}
}

func (e *runtimeEmitter) send(kind string, stepNo int, payload any) {
	if e == nil || e.emit == nil {
		return
	}
	e.sequence++
	e.emit(realtime.Event{
		Type: kind, EventID: uuid.NewString(), ConversationID: e.conversationID.String(),
		OperationID: e.operationID.String(),
		TurnID:      e.turnID.String(), AttemptID: e.attemptID.String(), StepNo: stepNo,
		Sequence: e.sequence, OccurredAt: time.Now().Format(time.RFC3339Nano), Payload: payload,
	})
}

// runAgentGeneration is the only production narrative path. One logical turn
// may contain several model steps, but a failed model call is never resent.
func (g *Generator) runAgentGeneration(ctx context.Context, conv *storage.Conversation, triggerMsgID *uuid.UUID, emit realtime.EmitFunc) {
	convID := conv.ID
	if g.agents == nil {
		markGenerationFailure(ctx, "agent_setup", "agent_repository_missing")
		emit(errorEvent(convID, "Agent 存储服务未加载"))
		return
	}
	settings, err := g.settings.Get(ctx, g.identity.CurrentPlayerID(ctx))
	if err != nil {
		markGenerationFailure(ctx, "settings", "settings_unavailable")
		logging.FromContext(ctx, "orchestration").Error("读取 LLM 设置失败", zap.Error(err))
		emit(errorEvent(convID, "模型设置暂时不可用（关联 ID："+logging.TraceID(ctx)+"）"))
		return
	}
	apiKey := settings.APIKey
	metadata, ok := attemptMetadata(ctx)
	if !ok {
		markGenerationFailure(ctx, "agent_setup", "attempt_metadata_missing")
		emit(errorEvent(convID, "生成 attempt 元数据缺失"))
		return
	}

	attemptCtx, cancelAttempt := context.WithTimeout(ctx, maxAgentAttemptDuration)
	defer cancelAttempt()
	// 租约心跳：只要本轮仍在进行就持续续期；租约被回收（实例判死、被抢占）
	// 时立即取消 attempt，避免两个持有者同时写同一个对话。
	go g.runLeaseHeartbeat(attemptCtx, cancelAttempt)
	runtimeEvents := newRuntimeEmitter(ctx, convID, emit)
	displayID := uuid.New()
	turnStartedAt := time.Now()
	runtimeEvents.messageID = displayID.String()
	runtimeEvents.send(realtime.TypeAgentTurnStarted, 0, map[string]any{"message_id": displayID.String()})
	// 路由二次校验：StartGeneration 与本轮第一步之间用户改了设置时拒绝继续，
	// 错误码 llm_route_mismatch。此时 run/turn/attempt 已创建但无任何模型调用与扣费。
	if routeErr := storage.CheckLLMRoute(conv.LLMRoute, settings.LLMRoute); routeErr != nil {
		markGenerationFailure(ctx, "llm_route", "llm_route_mismatch")
		g.finishAgentFailure(ctx, runtimeEvents, convID, 1, false,
			fmt.Errorf("%w：%s", routeErr, storage.LLMRouteMismatchMessage(conv.LLMRoute, settings.LLMRoute)), emit)
		return
	}

	// 平台设置在回合开始读取一次并整回合固定：工具目录的判定开关与判定
	// 工具执行的难度模式使用同一份读数，管理员修改从下一回合生效。
	platformSettings := storage.DefaultSystemSettings()
	if g.systemSettings != nil {
		loaded, readErr := g.systemSettings.Get(attemptCtx)
		if readErr != nil {
			markGenerationFailure(ctx, "settings", "system_settings_unavailable")
			g.finishAgentFailure(ctx, runtimeEvents, convID, 1, false, readErr, emit)
			return
		}
		platformSettings = loaded
	}

	mutatingCount := 0
	// settled 表示本 attempt 已提交挑战结算：对话已结束，后续步骤只允许写收尾叙事。
	settled := false
	pinned := false
	assembleCache := &attemptAssembleCache{}
	// 回合级耗时累计：成功时随“Agent 回合完成”一次输出，用于拆分
	// LLM / DB（含 assemble 与工具事务）/ 上下文构建（CPU+分词）占比。
	var turnAssembleMS, turnBuildMS, turnStepPersistMS, turnModelMS, turnToolMS, turnPersistMS int64
	var turnPromptTokens, turnCompletionTokens int
	var visibleReasoning strings.Builder
	var pendingGuidance *guidance.Proposal
	for stepNo := 1; stepNo <= maxAgentModelSteps; stepNo++ {
		stepStartedAt := time.Now()
		if err := attemptCtx.Err(); err != nil {
			g.finishAgentFailure(ctx, runtimeEvents, convID, stepNo, mutatingCount > 0, err, emit)
			return
		}

		in, contract, outcomeKeys, err := g.assembleInputWithCache(attemptCtx, conv, settings, assembleCache)
		if err != nil {
			markGenerationFailure(ctx, "context", "context_assembly_failed")
			g.finishAgentFailure(ctx, runtimeEvents, convID, stepNo, mutatingCount > 0, err, emit)
			return
		}
		if stepNo == 1 {
			assessment := assessCurrentPromptInjection(in.History)
			if assessment.IsRisky() {
				logging.Named("security").Warn("检测到疑似提示注入表达",
					zap.String("conversation_id", convID.String()),
					zap.String("attempt_id", metadata.AttemptID.String()),
					zap.String("level", string(assessment.Level)),
					zap.Strings("categories", assessment.Categories),
					zap.Int("matched_rule_count", assessment.MatchedRuleCount),
					zap.String("text_hash", assessment.NormalizedTextHash),
				)
			}
		}
		snapshot, err := buildAgentToolSnapshot(contract, in.CampaignProjection, outcomeKeys, platformSettings.RollSystemEnabled, metadata.TextOnly || settled)
		if err != nil {
			markGenerationFailure(ctx, "tool_snapshot", "tool_snapshot_invalid")
			g.finishAgentFailure(ctx, runtimeEvents, convID, stepNo, mutatingCount > 0, err, emit)
			return
		}
		snapshot.CatalogVersion = fmt.Sprintf("%s:%s@%d", agentToolCatalogVersion, contract.Engine.Key, contract.Engine.Version)
		if pendingGuidance != nil || stepNo == maxAgentModelSteps || settled {
			delete(snapshot.Bindings, guidance.ToolName)
			snapshot.Definitions = nil
			snapshot, err = finalizeAgentToolSnapshot(snapshot)
			if err != nil {
				g.finishAgentFailure(ctx, runtimeEvents, convID, stepNo, mutatingCount > 0, err, emit)
				return
			}
			if settled {
				in.OutputContract += "\n挑战已经完成结算，本步骤不提供任何工具；请依据工具结果写出结局收尾叙事，不要再提出行动选项。"
			} else {
				in.OutputContract += "\n本步骤不提供行动提示工具；请完成普通叙事，不要在正文中输出选项 JSON。"
			}
		}
		if !pinned {
			if err := g.agents.PinAttempt(attemptCtx, metadata.AttemptID, snapshot.CatalogVersion, conv.ChallengeHash); err != nil {
				markGenerationFailure(ctx, "agent_setup", "attempt_pin_failed")
				g.finishAgentFailure(ctx, runtimeEvents, convID, stepNo, mutatingCount > 0, err, emit)
				return
			}
			pinned = true
		}
		// assemble 段止于此：输入装配 + 工具快照 + attempt 钉选，以 DB 读取为主。
		assembleDoneAt := time.Now()

		definitionsJSON, _ := json.Marshal(snapshot.Definitions)
		toolTokens := g.counter.Count(string(definitionsJSON))
		in.RuntimeDirective += "\n" + actionRestrictionNotice(snapshot, in.History) + "\n" + endingNotice(snapshot)
		in.CampaignProjection = narrativeProjection(in.CampaignProjection)
		in.ReservedResponseTokens += toolTokens
		built, err := contextbuilder.Build(in, g.counter)
		if err != nil {
			markGenerationFailure(ctx, "context", "context_budget_failed")
			g.finishAgentFailure(ctx, runtimeEvents, convID, stepNo, mutatingCount > 0, err, emit)
			return
		}
		// build 段止于此：纯 CPU 的预算裁剪与分词计数，不碰 DB。
		buildDoneAt := time.Now()
		built.Segments = append(built.Segments, contextbuilder.SegmentInfo{
			Key: "tool_definitions", Role: "system", Tokens: toolTokens,
			Required: len(snapshot.Definitions) > 0, Included: true, BudgetKey: "tools",
		})
		built.UsedPromptTokens += toolTokens
		built.PromptBudget += toolTokens
		if stepNo == 1 {
			g.logContextBuild(attemptCtx, conv, triggerMsgID, settings, built, emit)
		}

		step := &storage.AgentStep{
			ID: uuid.New(), ConversationID: convID, StepNo: stepNo,
			Status: storage.AgentStepRunning, OutputLane: storage.AgentLaneUndecided,
			Model: settings.Model, Protocol: string(llm.NormalizeProtocol(settings.Protocol)),
			ToolSnapshot: storage.JSONDoc(snapshot.JSON), ToolSnapshotHash: snapshot.Hash,
			CampaignRevision: conversationCampaignRevision(conv, snapshot), PromptTokens: built.UsedPromptTokens,
			TokenUsageSource: "estimated", FirstEventMS: -1, FirstTextMS: -1,
			StartedAt: time.Now(),
		}
		if err := g.agents.CreateStep(attemptCtx, step); err != nil {
			markGenerationFailure(ctx, "agent_step", "step_persist_failed")
			g.finishAgentFailure(ctx, runtimeEvents, convID, stepNo, mutatingCount > 0, err, emit)
			return
		}
		runtimeEvents.send(realtime.TypeAgentStepStarted, stepNo, map[string]any{"tool_count": len(snapshot.Definitions)})

		lane := storage.AgentLaneUndecided
		textPreview := newAgentTextPreview()
		modelStartedAt := time.Now()
		firstEventMS, firstTextMS, firstToolMS := int64(-1), int64(-1), int64(-1)
		params := llm.Params{
			PlayerID: conv.PlayerID,
			BaseURL:  settings.BaseURL, APIKey: apiKey, Model: settings.Model,
			Protocol: llm.NormalizeProtocol(settings.Protocol), Temperature: settings.Temperature,
			TopP: settings.TopP, MaxTokens: settings.MaxResponseTokens, ReasoningEffort: settings.ReasoningEffort,
			ConversationID: convID.String(), MessageID: displayID.String(), Purpose: "narrative",
			AttemptID: metadata.AttemptID.String(), SettingsRevision: settings.SettingsRevision, SettingsSource: settings.Source,
			Relay: g.relayForRoute(attemptCtx, settings),
		}
		if metadata.MemoryID != nil {
			params.MemoryID = metadata.MemoryID.String()
		}
		result, callErr := g.llmClient.AgentStep(attemptCtx, params, built.Messages, snapshot.Definitions, settings.StreamEnabled, func(event llm.AgentEvent) {
			eventMS := time.Since(modelStartedAt).Milliseconds()
			if firstEventMS < 0 && event.Kind != llm.AgentEventStepCompleted {
				firstEventMS = eventMS
			}
			switch event.Kind {
			case llm.AgentEventReasoningDelta:
				if event.Text != "" {
					visibleReasoning.WriteString(event.Text)
					runtimeEvents.send(realtime.TypeAgentReasoningDelta, stepNo, map[string]any{"message_id": displayID.String(), "delta": event.Text})
				}
			case llm.AgentEventTextDelta:
				if firstTextMS < 0 {
					firstTextMS = eventMS
				}
				textPreview.append(event.Text, settings.StreamEnabled, func(delta string) {
					// A stream can be displayed before the complete result tells us
					// whether this step is text or tool lane. The preview is
					// deliberately not persisted until the lane is reconciled below.
					runtimeEvents.send(realtime.TypeAgentTextDelta, stepNo, map[string]any{
						"message_id": displayID.String(), "delta": delta, "preview": true,
					})
				})
			case llm.AgentEventRefusal:
				if firstTextMS < 0 {
					firstTextMS = eventMS
				}
				textPreview.append(event.Text, settings.StreamEnabled, func(delta string) {
					runtimeEvents.send(realtime.TypeAgentTextDelta, stepNo, map[string]any{
						"message_id": displayID.String(), "delta": delta, "preview": true,
					})
				})
			case llm.AgentEventToolCallStarted:
				if firstToolMS < 0 {
					firstToolMS = eventMS
				}
				textPreview.markTool(func() {
					runtimeEvents.send(realtime.TypeAgentTextReset, stepNo, map[string]any{
						"message_id": displayID.String(), "reason": "tool_call",
					})
				})
			}
		})
		modelDurationMS := time.Since(modelStartedAt).Milliseconds()
		toolCalls := result.ToolCallBlocks()
		contentBlocks := agentContentBlocksDoc(result.CanonicalBlocks())
		completionTokens := g.counter.Count(string(contentBlocks))
		if len(contentBlocks) == 0 || string(contentBlocks) == "[]" {
			completionTokens = g.counter.Count(result.TextProjection())
		}
		promptTokens := built.UsedPromptTokens
		usageSource := "estimated"
		if result.Usage.OutputTokens > 0 {
			completionTokens = result.Usage.OutputTokens
		}
		if result.Usage.InputTokens > 0 {
			promptTokens = result.Usage.InputTokens
		}
		if result.Usage.InputTokens > 0 && result.Usage.OutputTokens > 0 {
			usageSource = "provider"
		}
		generationMS := modelDurationMS
		if settings.StreamEnabled && firstEventMS >= 0 && modelDurationMS > firstEventMS {
			generationMS = modelDurationMS - firstEventMS
		}
		metrics := storage.AgentStepMetrics{
			PromptTokens: promptTokens, CompletionTokens: completionTokens, TokenUsageSource: usageSource,
			FirstEventMS: firstEventMS, FirstTextMS: firstTextMS,
			ModelDurationMS: modelDurationMS, GenerationMS: generationMS,
		}
		if callErr != nil {
			logAgentStepTiming(ctx, convID, metadata.AttemptID, stepNo, stepStartedAt, assembleDoneAt, buildDoneAt, modelStartedAt,
				firstEventMS, firstTextMS, firstToolMS, lane, built.UsedPromptTokens, completionTokens,
				len(toolCalls), callErr)
			status := storage.AgentStepFailed
			if isCancellation(callErr) || errors.Is(callErr, context.DeadlineExceeded) {
				status = storage.AgentStepCancelled
			}
			_ = g.agents.FinishStep(context.WithoutCancel(ctx), step.ID, status, lane, result.FinishReason, agentErrorCode(callErr), metrics)
			markGenerationFailure(ctx, "model_step", agentErrorCode(callErr))
			g.finishAgentFailure(ctx, runtimeEvents, convID, stepNo, mutatingCount > 0, callErr, emit)
			return
		}
		lane = resolveAgentLane(result)
		logAgentStepTiming(ctx, convID, metadata.AttemptID, stepNo, stepStartedAt, assembleDoneAt, buildDoneAt, modelStartedAt,
			firstEventMS, firstTextMS, firstToolMS, lane, built.UsedPromptTokens, completionTokens,
			len(toolCalls), nil)
		turnAssembleMS += assembleDoneAt.Sub(stepStartedAt).Milliseconds()
		turnBuildMS += buildDoneAt.Sub(assembleDoneAt).Milliseconds()
		// build 之后到模型调用前：CreateStep（首步另含 logContextBuild），以 DB 写为主。
		turnStepPersistMS += modelStartedAt.Sub(buildDoneAt).Milliseconds()
		turnModelMS += modelDurationMS
		turnPromptTokens += promptTokens
		turnCompletionTokens += completionTokens
		finishStepStartedAt := time.Now()
		finishStepErr := g.agents.FinishStep(attemptCtx, step.ID, storage.AgentStepCompleted, lane, result.FinishReason, "", metrics)
		turnStepPersistMS += time.Since(finishStepStartedAt).Milliseconds()
		if err := finishStepErr; err != nil {
			markGenerationFailure(ctx, "agent_step", "step_finish_failed")
			g.finishAgentFailure(ctx, runtimeEvents, convID, stepNo, mutatingCount > 0, err, emit)
			return
		}
		switch lane {
		case storage.AgentLaneText:
			text := strings.TrimSpace(textPreview.text())
			if text == "" {
				text = strings.TrimSpace(result.TextProjection())
			}
			if text == "" || !utf8.ValidString(text) {
				err := errors.New("模型最终文本为空或编码无效")
				markGenerationFailure(ctx, "final_text", "invalid_final_text")
				g.finishAgentFailure(ctx, runtimeEvents, convID, stepNo, mutatingCount > 0, err, emit)
				return
			}
			meta := map[string]any{"model": settings.Model, "agent_steps": stepNo, "tool_calls": mutatingCount}
			if pendingGuidance != nil {
				proposal := guidance.Snapshot{Proposal: *pendingGuidance, StateFingerprint: in.StateFingerprint}
				if snapshot.CampaignRevision != nil {
					proposal.CampaignRevision = *snapshot.CampaignRevision
				}
				meta[guidance.MetadataKey] = proposal
			}
			if !settings.StreamEnabled || !textPreview.sent() {
				runtimeEvents.send(realtime.TypeAgentTextDelta, stepNo, map[string]any{
					"message_id": displayID.String(), "delta": text,
					"preview": settings.StreamEnabled,
				})
			}
			// 复读守卫：无论上游模型或网关如何退化，几乎逐字照抄最近
			// 叙事的输出一律拒绝落库，由玩家选择重新生成或调整输入。
			if isRepetitiveNarrative(text, in.History) {
				err := errors.New("模型叙事与最近回复几乎完全重复")
				markGenerationFailure(ctx, "final_text", "repetitive_final_text")
				g.finishAgentFailure(ctx, runtimeEvents, convID, stepNo, mutatingCount > 0, err, emit)
				return
			}
			tokens := g.counter.Count(text)
			persistStartedAt := time.Now()
			_, display, err := g.chatSvc.AppendAssistantWithIDAndBlocks(attemptCtx, convID, displayID, text, text, meta, &tokens, contentBlocks)
			turnPersistMS += time.Since(persistStartedAt).Milliseconds()
			if err != nil {
				markGenerationFailure(ctx, "finalize", "assistant_persist_failed")
				g.finishAgentFailure(ctx, runtimeEvents, convID, stepNo, mutatingCount > 0, err, emit)
				return
			}
			// The durable assistant row is the billing boundary: once it is
			// persisted, the rewrite has succeeded and its debit must be kept even
			// if a later realtime notification is interrupted.
			markGenerationCommitted(ctx)
			// A turn can contain several tool-loop model steps. The final
			// assistant chat row keeps the last provider reasoning payload for
			// protocol replay, while the display projection shows the readable
			// reasoning accumulated across the whole turn.
			display.Reasoning = strings.TrimSpace(visibleReasoning.String())
			emit(realtime.Event{Type: realtime.TypeMessageCreated, ConversationID: convID.String(), MessageID: displayID.String(), Message: display})
			var activeGuidance *guidance.Active
			if pendingGuidance != nil {
				value := (guidance.Snapshot{Proposal: *pendingGuidance}).Public(displayID.String())
				activeGuidance = &value
			}
			runtimeEvents.send(realtime.TypeGuidanceUpdated, stepNo, map[string]any{"guidance": activeGuidance})
			runtimeEvents.send(realtime.TypeAgentTurnCompleted, stepNo, map[string]any{"message_id": displayID.String()})
			totalMS := time.Since(turnStartedAt).Milliseconds()
			logging.FromContext(ctx, "performance").Info("Agent 回合完成",
				zap.String("conversation_id", convID.String()),
				zap.String("attempt_id", metadata.AttemptID.String()),
				zap.Int("steps", stepNo),
				zap.Int("mutating_tools", mutatingCount),
				zap.Int64("total_ms", totalMS),
				// 分段累计：assemble（DB 读）、build（CPU/分词）、step_persist（步骤
				// 记录 DB 写）、model（LLM）、tool（含工具事务 DB 写）、persist（收尾落库）；
				// other 为 total 减去各段之和的残差（事件推送、guidance 等）。
				zap.Int64("assemble_ms", turnAssembleMS),
				zap.Int64("build_ms", turnBuildMS),
				zap.Int64("step_persist_ms", turnStepPersistMS),
				zap.Int64("model_ms", turnModelMS),
				zap.Int64("tool_ms", turnToolMS),
				zap.Int64("persist_ms", turnPersistMS),
				zap.Int64("other_ms", totalMS-turnAssembleMS-turnBuildMS-turnStepPersistMS-turnModelMS-turnToolMS-turnPersistMS),
				// 每步都会重发完整上下文，这里是各步输入 token 之和（即计费输入量），
				// 不是单次上下文大小。
				zap.Int("prompt_tokens_sum", turnPromptTokens),
				zap.Int("completion_tokens", turnCompletionTokens),
			)
			_ = g.convSvc.Touch(attemptCtx, convID)
			return

		case storage.AgentLaneTool:
			// Some OpenAI-compatible Responses providers ignore
			// parallel_tool_calls=false. Accept such a response, but execute
			// authoritative tools in the provider's stable order through the
			// existing idempotent transaction path.
			if err := validateAgentToolCallBatch(toolCalls); err != nil {
				markGenerationFailure(ctx, "tool_protocol", "invalid_tool_call_batch")
				g.finishAgentFailure(ctx, runtimeEvents, convID, stepNo, mutatingCount > 0, err, emit)
				return
			}
			for index, call := range toolCalls {
				toolStartedAt := time.Now()
				// One model step is one assistant declaration. Later invocations
				// append only their result, referencing that same stored batch.
				var blocks storage.JSONDoc
				if index == 0 {
					blocks = contentBlocks
				}
				// 同一批次里排在结算之后的写工具不再执行：对话已经结束。
				outcome, err := g.processAgentToolCall(
					attemptCtx, conv, contract, platformSettings, step, snapshot, call,
					mutatingCount < maxAgentMutatingTools && !settled, blocks, runtimeEvents, emit,
				)
				if err != nil {
					logAgentToolTiming(ctx, convID, metadata.AttemptID, stepNo, call.Name, toolStartedAt, err)
					markGenerationFailure(ctx, "tool_execute", agentErrorCode(err))
					g.finishAgentFailure(ctx, runtimeEvents, convID, stepNo, mutatingCount > 0, err, emit)
					return
				}
				logAgentToolTiming(ctx, convID, metadata.AttemptID, stepNo, call.Name, toolStartedAt, nil,
					zap.String("status", outcome.Status), zap.String("code", outcome.Code))
				turnToolMS += time.Since(toolStartedAt).Milliseconds()
				if outcome.Mutating && outcome.Status == storage.ToolInvocationCommitted {
					mutatingCount++
					if revision := outcomeRevision(outcome); revision != nil {
						snapshot.ObservedRevision = revision
					}
					pendingGuidance = nil
				}
				if outcome.Status == storage.ToolInvocationCommitted && (snapshot.Bindings[call.Name].Kind == agentToolCompletion || outcome.Code == "module_settled") {
					settled = true
				}
				if call.Name == guidance.ToolName && outcome.Status == storage.ToolInvocationCommitted {
					raw, marshalErr := json.Marshal(outcome.Result["data"])
					proposal, decodeErr := guidance.Decode(raw)
					if marshalErr != nil || decodeErr != nil {
						g.finishAgentFailure(ctx, runtimeEvents, convID, stepNo, mutatingCount > 0, errors.New("行动提示结果校验失败"), emit)
						return
					}
					pendingGuidance = &proposal
				}
				if outcome.Fatal {
					err := errors.New("工具内部执行失败")
					markGenerationFailure(ctx, "tool_execute", outcome.Code)
					g.finishAgentFailure(ctx, runtimeEvents, convID, stepNo, mutatingCount > 0, err, emit)
					return
				}
			}

		default:
			err := errors.New("模型没有返回文本或工具调用")
			markGenerationFailure(ctx, "model_step", "empty_model_step")
			g.finishAgentFailure(ctx, runtimeEvents, convID, stepNo, mutatingCount > 0, err, emit)
			return
		}
	}
	err = errors.New("主叙事 Agent 已达到 8 个模型步骤上限")
	markGenerationFailure(ctx, "step_limit", "agent_step_limit")
	g.finishAgentFailure(ctx, runtimeEvents, convID, maxAgentModelSteps, mutatingCount > 0, err, emit)
}

// resolveAgentLane 根据完整的模型结果决定步骤类型。工具调用优先于文本：
// 模型可能先输出一小段中间文本再发起工具调用，这不是协议错误。中间文本
// 可以作为尚未确认的实时预览，但不作为最终叙事落库；工具结果写回后由下一步重新叙事。
func resolveAgentLane(result llm.AgentStepResult) string {
	if len(result.ToolCallBlocks()) > 0 {
		return storage.AgentLaneTool
	}
	if strings.TrimSpace(result.TextProjection()) != "" {
		return storage.AgentLaneText
	}
	return storage.AgentLaneUndecided
}

func (g *Generator) processAgentToolCall(
	ctx context.Context,
	conv *storage.Conversation,
	contract *gameplay.RuntimeContract,
	platform *storage.SystemSettings,
	step *storage.AgentStep,
	snapshot agentToolSnapshot,
	call llm.ToolCall,
	mutatingAllowed bool,
	contentBlocks storage.JSONDoc,
	runtimeEvents *runtimeEmitter,
	emit realtime.EmitFunc,
) (agentToolOutcome, error) {
	if contract == nil {
		return agentToolOutcome{}, errors.New("工具执行缺少 Runtime")
	}
	if err := stableToolCallID(call); err != nil {
		return agentToolOutcome{}, err
	}
	arguments, argumentsErr := decodeToolArguments(call.Arguments)
	if arguments == nil {
		arguments = map[string]any{}
	}
	binding, known := snapshot.Bindings[call.Name]
	if !known {
		binding = unknownToolBinding(call.Name)
	}
	invocation := &storage.AgentToolInvocation{
		ID: uuid.New(), ConversationID: conv.ID, StepID: step.ID,
		StepToolSnapshotHash: snapshot.Hash, CallID: call.ID, ToolName: call.Name,
		Arguments: storage.JSONMap(arguments), ArgumentsHash: hashAgentArguments(call.Arguments),
	}
	claimed, replayed, err := g.agents.ClaimInvocation(ctx, invocation)
	if err != nil {
		return agentToolOutcome{}, err
	}
	if replayed {
		if claimed.ToolName != call.Name || claimed.ArgumentsHash != invocation.ArgumentsHash {
			return agentToolOutcome{}, errors.New("相同 call_id 被用于不同工具参数")
		}
		if claimed.Status == storage.ToolInvocationReceived {
			return agentToolOutcome{}, errors.New("工具调用仍处于未完成状态")
		}
		return agentToolOutcome{
			Result: claimed.Result, Status: claimed.Status, Code: claimed.ResultCode,
			Mutating: claimed.IsMutating, Fatal: claimed.Status == storage.ToolInvocationFailed,
		}, nil
	}
	runtimeEvents.send(realtime.TypeAgentToolStarted, step.StepNo, map[string]any{"name": call.Name, "category": agentToolPublicCategory(binding.Kind)})

	var outcome agentToolOutcome
	var transactionErr error
	finishedInTx := false
	switch {
	case argumentsErr != nil:
		outcome = agentToolOutcome{
			Status: storage.ToolInvocationRejected, Code: "invalid_arguments",
			Result: storage.JSONMap{"ok": false, "code": "invalid_arguments", "message": argumentsErr.Error(), "data": map[string]any{}},
		}
	case !known:
		outcome = unavailableToolOutcome(snapshot, call.Name)
	case binding.Mutating && !mutatingAllowed:
		outcome = agentToolOutcome{
			Status: storage.ToolInvocationRejected, Code: "mutating_tool_limit",
			Result: storage.JSONMap{
				"ok": false, "code": "mutating_tool_limit",
				"message": "本回合写工具数量已达到上限，请根据现有结果完成叙事", "data": map[string]any{},
			},
		}
	default:
		if g.toolTx == nil {
			return agentToolOutcome{}, errors.New("Agent 工具事务未配置")
		}
		var resultText string
		if binding.Kind == agentToolModule || binding.Kind == agentToolEnding {
			content, err := g.challenge.ModuleContentForVersion(ctx, conv.ChallengeVersionID, conv.ChallengeHash)
			if err != nil {
				return agentToolOutcome{}, err
			}
			binding.ModuleContent = &content
		}
		err := g.toolTx.Run(ctx, func(tx *agentToolTx) error {
			outcome = g.executeAgentToolInTx(ctx, conv, contract, platform, binding, arguments, call.ID, tx)
			if _, err := json.Marshal(outcome.Result); err != nil {
				return fmt.Errorf("工具结果无法序列化: %w", err)
			}
			resultText, outcome.Result = boundedToolResult(outcome.Result)
			if outcome.Status != storage.ToolInvocationCommitted {
				return errAgentToolOutcomeRollback
			}
			return tx.agents.FinishInvocationInCurrentTxWithBlocks(
				context.WithoutCancel(ctx), tx.db, claimed.ID, outcome.Status, outcome.Code,
				outcome.Result, outcome.Mutating, contentBlocks, resultText,
			)
		})
		switch {
		case errors.Is(err, errAgentToolOutcomeRollback):
			// The handler produced a public rejection/failure after touching domain
			// repositories. The sentinel has rolled those writes back; persist only
			// the provider-visible call/result pair below.
		case err != nil:
			transactionErr = err
			logging.FromContext(ctx, "orchestration").Error("Agent 工具事务失败", zap.String("tool", call.Name), zap.String("error_code", "tool_transaction_failed"), zap.Error(err))
			outcome = agentToolOutcome{
				Status: storage.ToolInvocationFailed, Code: "tool_transaction_failed", Fatal: true,
				Result: storage.JSONMap{
					"ok": false, "code": "tool_transaction_failed",
					"message": "工具事务提交失败", "data": map[string]any{},
				},
			}
		default:
			finishedInTx = true
		}
		if finishedInTx && outcome.Mutating {
			markMutatingToolCommitted(ctx)
		}
		if finishedInTx {
			for _, event := range outcome.Events {
				emit(decorateGenerationEvent(ctx, event))
			}
			if toolMayChangeConversationState(binding.Kind) {
				if playerState, stateErr := g.stateSvc.GetPlayer(context.WithoutCancel(ctx), conv.ID); stateErr == nil {
					emit(decorateGenerationEvent(ctx, realtime.Event{Type: realtime.TypeStateUpdate, ConversationID: conv.ID.String(), State: playerState}))
				}
			}
		}
	}
	if !finishedInTx {
		resultText, normalizedResult := boundedToolResult(outcome.Result)
		outcome.Result = normalizedResult
		if err := g.agents.FinishInvocationWithBlocks(
			context.WithoutCancel(ctx), claimed.ID, outcome.Status, outcome.Code, outcome.Result,
			outcome.Mutating, contentBlocks, resultText,
		); err != nil {
			return outcome, fmt.Errorf("%w: %w", errAgentToolPersistence, errors.Join(transactionErr, err))
		}
	}
	if outcome.Result == nil {
		return outcome, errors.New("工具未生成结果")
	}
	runtimeEvents.send(realtime.TypeAgentToolFinished, step.StepNo, map[string]any{
		"name": call.Name, "status": outcome.Status, "code": outcome.Code,
	})
	return outcome, nil
}

func logAgentStepTiming(
	ctx context.Context,
	conversationID, attemptID uuid.UUID,
	stepNo int,
	stepStartedAt, assembleDoneAt, buildDoneAt, modelStartedAt time.Time,
	firstEventMS, firstTextMS, firstToolMS int64,
	lane string,
	promptTokens, completionTokens, toolCalls int,
	callErr error,
) {
	fields := []zap.Field{
		zap.String("conversation_id", conversationID.String()),
		zap.String("attempt_id", attemptID.String()),
		zap.Int("step_no", stepNo),
		zap.Int64("step_ms", time.Since(stepStartedAt).Milliseconds()),
		zap.Int64("context_ms", modelStartedAt.Sub(stepStartedAt).Milliseconds()),
		// 上下文段拆分：assemble 以 DB 读取为主，build 为纯 CPU（预算裁剪+分词）。
		zap.Int64("assemble_ms", assembleDoneAt.Sub(stepStartedAt).Milliseconds()),
		zap.Int64("build_ms", buildDoneAt.Sub(assembleDoneAt).Milliseconds()),
		zap.Int64("model_ms", time.Since(modelStartedAt).Milliseconds()),
		zap.Int64("first_event_ms", firstEventMS),
		zap.Int64("first_text_ms", firstTextMS),
		zap.Int64("first_tool_ms", firstToolMS),
		zap.String("output_lane", lane),
		zap.Int("prompt_tokens", promptTokens),
		zap.Int("completion_tokens", completionTokens),
		zap.Int("tool_calls", toolCalls),
	}
	if callErr != nil {
		fields = append(fields, zap.String("error_code", agentErrorCode(callErr)))
	}
	log := logging.FromContext(ctx, "performance")
	if callErr != nil {
		log.Error("Agent 模型步骤失败", fields...)
	} else if time.Since(modelStartedAt) >= 60*time.Second {
		log.Warn("Agent 模型步骤较慢", fields...)
	} else {
		log.Debug("Agent 模型步骤耗时", fields...)
	}
}

func logAgentToolTiming(
	ctx context.Context,
	conversationID, attemptID uuid.UUID,
	stepNo int,
	toolName string,
	startedAt time.Time,
	callErr error,
	extra ...zap.Field,
) {
	fields := []zap.Field{
		zap.String("conversation_id", conversationID.String()),
		zap.String("attempt_id", attemptID.String()),
		zap.Int("step_no", stepNo),
		zap.String("tool", toolName),
		zap.Int64("duration_ms", time.Since(startedAt).Milliseconds()),
	}
	if callErr != nil {
		fields = append(fields, zap.String("error_code", agentErrorCode(callErr)))
	}
	fields = append(fields, extra...)
	log := logging.FromContext(ctx, "performance")
	if callErr != nil {
		log.Warn("Agent 工具执行未成功", fields...)
	} else if time.Since(startedAt) >= time.Second {
		log.Warn("Agent 工具执行较慢", fields...)
	} else {
		log.Debug("Agent 工具执行耗时", fields...)
	}
}

func toolMayChangeConversationState(kind agentToolKind) bool {
	switch kind {
	case agentToolStateText, agentToolStateNumber, agentToolStateBoolean,
		agentToolStateReplace, agentToolStateAppend, agentToolStateDynamic, agentToolRoll, agentToolModule:
		return true
	default:
		return false
	}
}

func agentContentBlocksDoc(blocks []llm.ContentBlock) storage.JSONDoc {
	if len(blocks) == 0 {
		return storage.JSONDoc("[]")
	}
	raw, err := json.Marshal(blocks)
	if err != nil || len(raw) == 0 {
		return storage.JSONDoc("[]")
	}
	return storage.JSONDoc(raw)
}

func agentToolPublicCategory(kind agentToolKind) string {
	switch kind {
	case agentToolStateText, agentToolStateNumber, agentToolStateBoolean,
		agentToolStateReplace, agentToolStateAppend, agentToolStateDynamic:
		return "state"
	case agentToolRoll:
		return "roll"
	case agentToolCompletion:
		return "settlement"
	case agentToolCampaign, agentToolModule:
		return "action"
	case agentToolGuidance:
		return "guidance"
	default:
		return ""
	}
}

func boundedToolResult(result storage.JSONMap) (string, storage.JSONMap) {
	encoded, err := json.Marshal(result)
	if err == nil && len(encoded) <= maxPublicToolResultBytes {
		return string(encoded), result
	}
	ok, _ := result["ok"].(bool)
	code, _ := result["code"].(string)
	message, _ := result["message"].(string)
	if code == "" {
		code = "tool_result_truncated"
	}
	if message == "" {
		message = "工具已完成，但公开结果超过上下文上限；请依据最新可信投影继续。"
	}
	bounded := storage.JSONMap{
		"ok": ok, "code": code, "message": message,
		"data": map[string]any{"truncated": true},
	}
	// Keep the bounded transaction-generated feedback even when events or
	// other optional result details do not fit. Never discard goal/fact truth.
	if err == nil {
		var source struct {
			Data struct {
				Completion        json.RawMessage `json:"completion"`
				Revision          *int64          `json:"revision"`
				NarrativeFeedback json.RawMessage `json:"narrative_feedback"`
			} `json:"data"`
		}
		if json.Unmarshal(encoded, &source) == nil {
			data := bounded["data"].(map[string]any)
			if len(source.Data.Completion) > 0 && len(source.Data.Completion) <= maxPublicToolResultBytes/2 {
				data["completion"] = source.Data.Completion
			}
			if source.Data.Revision != nil {
				data["revision"] = *source.Data.Revision
			}
			if len(source.Data.NarrativeFeedback) > 0 && len(source.Data.NarrativeFeedback) <= gameplay.MaxNarrativeFeedbackBytes {
				data["narrative_feedback"] = source.Data.NarrativeFeedback
			}
		}
	}
	encoded, _ = json.Marshal(bounded)
	return string(encoded), bounded
}

func hashAgentArguments(raw string) string {
	hash := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(hash[:])
}

func agentErrorCode(err error) string {
	switch {
	case errors.Is(err, errAgentToolPersistence):
		return "tool_persistence_failed"
	case err == nil:
		return ""
	case errors.Is(err, context.Canceled), errors.Is(err, storage.ErrGenerationNotActive):
		// 用户取消 / 对话结束会先把租约置为 cancel_requested，随后的写入
		// 校验返回 ErrGenerationNotActive；对玩家而言这仍是"已取消"。
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, llm.ErrLLMStreamStalled):
		return "stream_stalled"
	case errors.Is(err, llm.ErrTruncated):
		return "output_truncated"
	case errors.Is(err, llm.ErrToolReplayInvalidHistory):
		return "tool_replay_invalid_history"
	case errors.Is(err, llm.ErrToolCallConflict):
		return "tool_call_conflict"
	case errors.Is(err, storage.ErrLLMRouteMismatch):
		return "llm_route_mismatch"
	default:
		return "agent_step_failed"
	}
}

func isCancellation(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, storage.ErrGenerationNotActive)
}

func (g *Generator) finishAgentFailure(
	ctx context.Context,
	runtimeEvents *runtimeEmitter,
	conversationID uuid.UUID,
	stepNo int,
	mutated bool,
	err error,
	emit realtime.EmitFunc,
) {
	// 在发送终止事件之前返还改写额度，界面刷新余额时不会与补偿事务竞争；
	// 外层的生成租约收尾会幂等地再执行一次，覆盖预检失败和崩溃路径。
	// 已提交写工具（partial）时保留扣费，避免用取消来免费重掷已提交的判定。
	if !mutated {
		g.refundRewriteReservation(ctx, "agent_failure")
	}
	kind := realtime.TypeAgentTurnFailed
	message := generationErrorMessage(err)
	if mutated {
		kind = realtime.TypeAgentTurnPartial
		message = partialGenerationErrorMessage(err)
	} else if isCancellation(err) {
		kind = realtime.TypeAgentTurnCancelled
		message = "生成已取消"
	}
	if !isCancellation(err) {
		logging.FromContext(ctx, "performance").Error("Agent 回合未完成", zap.String("conversation_id", conversationID.String()), zap.Bool("partial", mutated), zap.String("error_code", agentErrorCode(err)), zap.Error(err))
		message += "（关联 ID：" + logging.TraceID(ctx) + "）"
	}
	runtimeEvents.send(kind, stepNo, map[string]any{"message_id": runtimeEvents.messageID, "code": agentErrorCode(err), "message": message, "trace_id": logging.TraceID(ctx)})
	if mutated {
		emit(infoEvent(conversationID, message))
	} else if isCancellation(err) {
		emit(infoEvent(conversationID, message))
	} else {
		emit(errorEvent(conversationID, message))
	}
}
