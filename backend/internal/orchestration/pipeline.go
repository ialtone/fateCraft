package orchestration

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/ialtone/fatecraft/internal/contextbuilder"
	"github.com/ialtone/fatecraft/internal/logging"
	"github.com/ialtone/fatecraft/internal/operation"
	"github.com/ialtone/fatecraft/internal/realtime"
	"github.com/ialtone/fatecraft/internal/storage"
)

func (g *Generator) runGeneration(ctx context.Context, conv *storage.Conversation, triggerMsgID *uuid.UUID, emit realtime.EmitFunc) {
	// Attach the durable operation/attempt identity to every event produced by
	// the pipeline, including state, campaign and message facts. This lets a
	// client discard late events from a replaced branch instead of applying them
	// to the current timeline.
	g.runAgentGeneration(ctx, conv, triggerMsgID, func(event realtime.Event) {
		emit(decorateGenerationEvent(ctx, event))
	})
}

func decorateGenerationEvent(ctx context.Context, event realtime.Event) realtime.Event {
	if generation, ok := operation.GenerationFromContext(ctx); ok && event.OperationID == "" {
		event.OperationID = generation.ID.String()
	}
	if metadata, ok := operation.FromContext(ctx); ok {
		if event.ConversationID == "" {
			event.ConversationID = metadata.ConversationID.String()
		}
		if event.TurnID == "" {
			event.TurnID = metadata.TurnID.String()
		}
		if event.AttemptID == "" {
			event.AttemptID = metadata.AttemptID.String()
		}
	}
	return event
}

// logContextBuild persists the user-facing budget summary. Tool definitions
// are represented as their own segment by the Agent coordinator.
func (g *Generator) logContextBuild(ctx context.Context, conv *storage.Conversation, triggerMsgID *uuid.UUID, settings *storage.LLMSettings, result *contextbuilder.Result, emit realtime.EmitFunc) (uuid.UUID, bool) {
	convID := conv.ID
	logID := uuid.New()
	row := &storage.ContextBuildLog{
		ID: logID, ConversationID: convID, TriggerMessageID: triggerMsgID,
		ChallengeVersionID: conv.ChallengeVersionID, ChallengeHash: conv.ChallengeHash,
		Model: settings.Model, MaxContextTokens: settings.MaxContextTokens,
		ReservedResponseTokens: settings.MaxResponseTokens, UsedPromptTokens: result.UsedPromptTokens,
		Segments: toJSONDoc(result.Segments), IncludedMessageIDs: uuidsToStrings(result.IncludedMessageIDs),
		ActivatedWorldEntryIDs: result.ActivatedEntryIDs, MemoryLag: result.MemoryLag,
		OmittedTurnFrom: result.OmittedTurnFrom, OmittedTurnUntil: result.OmittedTurnUntil,
		CreatedAt: time.Now(),
	}
	if metadata, ok := attemptMetadata(ctx); ok {
		row.TurnID = &metadata.TurnID
		row.AttemptID = &metadata.AttemptID
		row.BranchEpoch = metadata.BranchEpoch
		row.MemoryID = metadata.MemoryID
		if metadata.MemoryID != nil && g.memSvc != nil {
			if memory, err := g.memSvc.Get(ctx, convID, *metadata.MemoryID); err == nil && memory != nil {
				row.MemoryCoveredUntilTurnSeq = memory.CoveredUntilTurnSeq
			}
		}
	}
	if result.MemoryLag && g.memSvc != nil {
		if job, err := g.memSvc.Schedule(ctx, convID, true); err != nil {
			logging.Named("memory").Warn("上下文存在未覆盖省略时调度记忆失败", zap.String("conversation_id", convID.String()), zap.Error(err))
		} else if job != nil {
			row.MemoryJobID = &job.ID
		}
	}
	logErr := g.ctxRepo.Create(ctx, row)
	if logErr != nil {
		logging.Named("orchestration").Warn("保存上下文构建日志失败", zap.String("conversation_id", convID.String()), zap.Error(logErr))
		return uuid.Nil, false
	}
	emit(realtime.Event{Type: realtime.TypeContextBuilt, ConversationID: convID.String(), ContextBuildID: logID.String()})
	return logID, true
}
