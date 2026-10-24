package orchestration

import (
	"context"
	"sync"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/ialtone/fatecraft/internal/logging"
	"github.com/ialtone/fatecraft/internal/operation"
	"github.com/ialtone/fatecraft/internal/storage"
)

type generationTracker struct {
	mu                sync.Mutex
	committed         bool
	mutatingCommitted bool
	stage             string
	code              string
}

// rewriteReservation ties a platform-credit debit to the generation run that
// must eventually commit the replacement narrative. The reservation lives in
// context so every early-return path (preflight, model, protocol, persistence
// or cancellation) reaches the same compensation logic.
type rewriteReservation struct {
	PlayerID              uuid.UUID
	RewardKey             string
	ConsumeIdempotencyKey string
}

type rewriteReservationKey struct{}

func withRewriteReservation(ctx context.Context, reservation rewriteReservation) context.Context {
	return context.WithValue(ctx, rewriteReservationKey{}, reservation)
}

func rewriteReservationFromContext(ctx context.Context) (rewriteReservation, bool) {
	reservation, ok := ctx.Value(rewriteReservationKey{}).(rewriteReservation)
	return reservation, ok && reservation.PlayerID != uuid.Nil && reservation.RewardKey != "" && reservation.ConsumeIdempotencyKey != ""
}

func markMutatingToolCommitted(ctx context.Context) {
	if tracker, ok := ctx.Value(trackerKey{}).(*generationTracker); ok {
		tracker.mu.Lock()
		tracker.mutatingCommitted = true
		tracker.mu.Unlock()
	}
}

type trackerKey struct{}

func withTracker(ctx context.Context, tracker *generationTracker) context.Context {
	return context.WithValue(ctx, trackerKey{}, tracker)
}

func markGenerationCommitted(ctx context.Context) {
	if tracker, ok := ctx.Value(trackerKey{}).(*generationTracker); ok {
		tracker.mu.Lock()
		tracker.committed = true
		tracker.stage = ""
		tracker.code = ""
		tracker.mu.Unlock()
	}
}

func markGenerationFailure(ctx context.Context, stage, code string) {
	if tracker, ok := ctx.Value(trackerKey{}).(*generationTracker); ok {
		tracker.mu.Lock()
		if !tracker.committed {
			tracker.stage = stage
			tracker.code = code
		}
		tracker.mu.Unlock()
	}
}

func (g *Generator) beginAttempt(ctx context.Context, conversationID uuid.UUID, triggerType string) (context.Context, func(), error) {
	turn, attempt, err := g.turns.Begin(ctx, conversationID, triggerType)
	if err != nil {
		return ctx, func() {}, err
	}
	return g.beginAttemptFromRecords(ctx, conversationID, triggerType, turn, attempt)
}

func (g *Generator) beginAttemptFromRecords(
	ctx context.Context,
	conversationID uuid.UUID,
	triggerType string,
	turn *storage.ConversationTurn,
	attempt *storage.GenerationAttempt,
) (context.Context, func(), error) {
	if turn == nil || attempt == nil {
		return ctx, func() {}, storage.ErrNotFound
	}
	metadata := operation.Metadata{
		ConversationID: conversationID, TurnID: turn.ID, AttemptID: attempt.ID, MemoryID: attempt.MemoryID, BranchEpoch: attempt.BranchEpoch,
		TriggerType: triggerType,
		// 面板动作叙事与续写一样只做叙事收尾：事实已在动作事务中结算，
		// 本回合不再提供新的玩法动作工具。
		TextOnly: triggerType == "continue" || triggerType == "module_action",
	}
	tracker := &generationTracker{stage: "generation", code: "generation_failed"}
	attemptCtx := withTracker(operation.WithMetadata(ctx, metadata), tracker)
	finish := func() {
		tracker.mu.Lock()
		completed, mutated, stage, code := tracker.committed, tracker.mutatingCommitted, tracker.stage, tracker.code
		tracker.mu.Unlock()
		status := storage.AttemptStatusFailed
		if completed {
			status = storage.AttemptStatusCompleted
			stage, code = "", ""
		} else if mutated {
			status = storage.AttemptStatusPartial
		} else if ctx.Err() != nil {
			status = storage.AttemptStatusCancelled
		}
		sealCtx := context.WithoutCancel(ctx)
		if err := g.turns.SealStatus(sealCtx, turn.ID, attempt.ID, status, stage, code); err != nil {
			logging.Named("orchestration").Error("封口生成回合失败",
				zap.String("conversation_id", conversationID.String()),
				zap.String("turn_id", turn.ID.String()), zap.String("attempt_id", attempt.ID.String()), zap.Error(err))
		}
		if g.memSvc != nil && status == storage.AttemptStatusCompleted {
			if _, err := g.memSvc.Schedule(sealCtx, conversationID, false); err != nil {
				logging.Named("memory").Warn("回合封口后调度记忆失败",
					zap.String("conversation_id", conversationID.String()), zap.Error(err))
			}
		}
	}
	return attemptCtx, finish, nil
}

func attemptMetadata(ctx context.Context) (operation.Metadata, bool) {
	return operation.FromContext(ctx)
}
