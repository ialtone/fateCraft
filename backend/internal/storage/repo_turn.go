package storage

import (
	"context"
	"errors"
	"github.com/ialtone/fatecraft/internal/guidance"
	"time"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/platformcapability"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrTurnInProgress           = errors.New("对话已有进行中的回合")
	ErrContinueNotAllowed       = errors.New("当前回合不能继续叙事")
	ErrRegenerateNotAllowed     = errors.New("当前回合不能重新生成")
	ErrCampaignHistoryImmutable = errors.New("该回合包含已提交的长期存档动作，不能重新生成或改写")
)

type TurnRepo struct{ db *gorm.DB }

func NewTurnRepo(db *gorm.DB) *TurnRepo { return &TurnRepo{db: db} }

type BranchPreparation struct {
	TriggerContent        string
	DisplayID             *uuid.UUID
	InvalidatedDisplayIDs []uuid.UUID
}

// RegeneratePreparation is the complete durable setup for a regenerate
// operation.  The branch invalidation, replacement turn/attempt and trigger
// message are committed together so a failure cannot leave the conversation
// with its old reply removed but no retryable turn.
type RegeneratePreparation struct {
	BranchPreparation
	Turn    ConversationTurn
	Attempt GenerationAttempt
	Trigger ChatMessage
	// RewardConsumed records whether this transaction actually applied a
	// rewrite-credit debit. It is used by orchestration to compensate a later
	// generation failure without refunding an idempotent retry.
	RewardConsumed bool
	RewardKey      string
}

// RegenerateInput carries the operation identity and initial state needed by
// the branch transaction. The transaction resolves the platform policy itself
// so callers cannot override the reward or quantity with request data.
type RegenerateInput struct {
	ConversationID uuid.UUID
	PlayerID       uuid.UUID
	IdempotencyKey string
	InitialState   JSONMap
}

// TurnCapabilities is the server-authoritative action projection for the
// current effective turn. The UI must not infer these flags from display
// message ordering because partial/failed turns may have no assistant row.
type TurnCapabilities struct {
	Status                 string
	CanContinue            bool
	CanRegenerate          bool
	RegenerateCostQuantity int64
}

func (r *TurnRepo) CurrentCapabilities(ctx context.Context, conversationID uuid.UUID) (*TurnCapabilities, error) {
	return currentCapabilities(r.db.WithContext(ctx), conversationID)
}

func currentCapabilities(db *gorm.DB, conversationID uuid.UUID) (*TurnCapabilities, error) {
	var turn ConversationTurn
	err := db.
		Where("conversation_id = ? AND invalidated_at IS NULL", conversationID).
		Order("turn_seq DESC, id DESC").First(&turn).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &TurnCapabilities{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := &TurnCapabilities{Status: turn.Status}
	if turn.Status == TurnStatusPending {
		return result, nil
	}
	// 失败或取消的回合没有提交任何写工具，重试属于恢复操作，免费；已完成回合和
	// partial 回合都已经提交了可信事实（例如判定），替换它们要消耗剧情改写额度，
	// 否则玩家可以用“取消 → 免费重新生成”无限重掷已提交的判定。
	policy := platformcapability.MessageRewritePolicy()
	if policy.Enabled && (turn.Status == TurnStatusCompleted || turn.Status == TurnStatusPartial) {
		result.RegenerateCostQuantity = policy.CostQuantity
	}
	result.CanRegenerate = turn.Status == TurnStatusCompleted || turn.Status == TurnStatusPartial || turn.Status == TurnStatusFailed || turn.Status == TurnStatusCancelled
	var assistantCount int64
	if err := db.Model(&ChatMessage{}).
		Where("conversation_id = ? AND turn_id = ? AND role = ? AND invalidated_at IS NULL", conversationID, turn.ID, RoleAssistant).
		Count(&assistantCount).Error; err != nil {
		return nil, err
	}
	if turn.Status == TurnStatusCompleted {
		result.CanContinue = assistantCount > 0
	}
	var triggerCount int64
	if err := db.Model(&ChatMessage{}).
		Where("conversation_id = ? AND turn_id = ? AND role = ? AND invalidated_at IS NULL", conversationID, turn.ID, RoleUser).
		Count(&triggerCount).Error; err != nil {
		return nil, err
	}
	if turn.Status == TurnStatusPartial {
		var attempt GenerationAttempt
		if err := db.Where("turn_id = ?", turn.ID).Order("attempt_no DESC").First(&attempt).Error; err == nil {
			result.CanContinue = attempt.CommittedMutatingToolCount > 0
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	// A failed/cancelled turn without a user trigger cannot be regenerated;
	// completed opening turns are the exception and intentionally use the
	// server-provided fallback trigger.
	if (turn.Status == TurnStatusFailed || turn.Status == TurnStatusCancelled) && triggerCount == 0 {
		result.CanRegenerate = false
	}
	return result, nil
}

type UserTurnPreparation struct {
	Turn    ConversationTurn
	Attempt GenerationAttempt
	Trigger ChatMessage
	Display DisplayMessage
}

// BeginUserTurn atomically allocates the turn/attempt and persists both user
// message projections. A failure cannot leave a pending turn or invisible
// context-only trigger behind.
func (r *TurnRepo) BeginUserTurn(ctx context.Context, conversationID uuid.UUID, triggerType, content string, tokenCount *int) (*UserTurnPreparation, error) {
	var preparation UserTurnPreparation
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := AssertGenerationActive(ctx, tx, conversationID); err != nil {
			return err
		}
		if selection, ok := guidance.SelectionFromContext(ctx); ok {
			var err error
			content, err = resolveGuidanceSelection(tx, conversationID, selection)
			if err != nil {
				return err
			}
			tokenCount = nil
		}
		if triggerType == "continue" {
			capabilities, err := currentCapabilities(tx, conversationID)
			if err != nil {
				return err
			}
			if !capabilities.CanContinue {
				return ErrContinueNotAllowed
			}
		}
		turn, attempt, err := beginTurnInTx(ctx, tx, conversationID, triggerType)
		if err != nil {
			return err
		}
		now := time.Now()
		trigger := ChatMessage{
			ID: uuid.New(), ConversationID: conversationID, Role: RoleUser, Kind: "text",
			Content: content, ContentBlocks: messageTextContentBlocks(content), TokenCount: tokenCount,
			TurnID: &turn.ID, AttemptID: &attempt.ID, CreatedAt: now, UpdatedAt: now,
		}
		if err := NewMessageRepo(tx).createChatInTx(ctx, tx, &trigger); err != nil {
			return err
		}
		display := DisplayMessage{
			ID: uuid.New(), ConversationID: conversationID, ChatMessageID: &trigger.ID,
			TurnID: &turn.ID, AttemptID: &attempt.ID, Type: "user", Markdown: content,
			Metadata: JSONMap{}, CreatedAt: now,
		}
		if err := tx.Create(&display).Error; err != nil {
			return err
		}
		preparation = UserTurnPreparation{Turn: *turn, Attempt: *attempt, Trigger: trigger, Display: display}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &preparation, nil
}

// BeginRegenerate atomically resolves the current branch target, consumes the
// declared rewrite credit, invalidates the old branch, allocates the
// replacement turn/attempt and writes the hidden trigger message. The caller
// must already own the durable generation lease in ctx.
func (r *TurnRepo) BeginRegenerate(ctx context.Context, in RegenerateInput) (*RegeneratePreparation, error) {
	var preparation RegeneratePreparation
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := AssertGenerationActive(ctx, tx, in.ConversationID); err != nil {
			return err
		}
		// client 路由不扣费：路由取自事务内行锁定的对话行。
		var conv Conversation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&conv, "id = ?", in.ConversationID).Error; err != nil {
			return err
		}
		clientRoute := conv.LLMRoute == LLMRouteClient
		var target ConversationTurn
		if err := tx.Where("conversation_id = ? AND invalidated_at IS NULL", in.ConversationID).
			Order("turn_seq DESC, id DESC").First(&target).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if target.Status == TurnStatusPending {
			return ErrTurnInProgress
		}
		// The UI capability projection is only a convenience. Re-check the same
		// rule while holding the branch transaction lock so a stale client cannot
		// regenerate an opening/failed turn that has no user trigger.
		capabilities, err := currentCapabilities(tx, in.ConversationID)
		if err != nil {
			return err
		}
		if capabilities == nil || !capabilities.CanRegenerate {
			return ErrRegenerateNotAllowed
		}

		preparation.TriggerContent = "继续"
		var trigger ChatMessage
		if err := tx.Where("conversation_id = ? AND turn_id = ? AND role = ? AND invalidated_at IS NULL",
			in.ConversationID, target.ID, RoleUser).Order("message_seq DESC").First(&trigger).Error; err == nil {
			preparation.TriggerContent = trigger.Content
			var display DisplayMessage
			if displayErr := tx.Where("conversation_id = ? AND chat_message_id = ? AND type = ? AND invalidated_at IS NULL",
				in.ConversationID, trigger.ID, "user").First(&display).Error; displayErr == nil {
				preparation.DisplayID = &display.ID
			} else if !errors.Is(displayErr, gorm.ErrRecordNotFound) {
				return displayErr
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if preparation.TriggerContent == "" {
			preparation.TriggerContent = "继续"
		}

		policy := platformcapability.MessageRewritePolicy()
		if !policy.Enabled {
			return errors.New("当前未开启剧情改写")
		}
		costQuantity := int64(0)
		if !clientRoute && (target.Status == TurnStatusCompleted || target.Status == TurnStatusPartial) {
			costQuantity = policy.CostQuantity
		}
		if costQuantity > 0 {
			consumed, err := NewRewardRepository(tx).ConsumeInCurrentTx(ctx, RewardMutation{
				PlayerID: in.PlayerID, RewardKey: policy.CostRewardKey, Quantity: costQuantity,
				SourceType: "message_regenerate", SourceID: in.IdempotencyKey,
				IdempotencyKey: in.IdempotencyKey,
				Metadata: JSONMap{
					"conversation_id": in.ConversationID.String(),
					"operation":       "regenerate",
				},
			})
			if err != nil {
				return err
			}
			preparation.RewardConsumed = consumed != nil && consumed.Applied
			preparation.RewardKey = policy.CostRewardKey
		}

		invalidated, err := prepareBranchInTx(ctx, tx, in.ConversationID, target.ID, preparation.DisplayID, in.InitialState, "regenerate")
		if err != nil {
			return err
		}
		preparation.InvalidatedDisplayIDs = invalidated

		turn, attempt, err := beginTurnInTx(ctx, tx, in.ConversationID, "regenerate")
		if err != nil {
			return err
		}
		preparation.Turn, preparation.Attempt = *turn, *attempt
		triggerID := uuid.New()
		turnID, attemptID := turn.ID, attempt.ID
		chat := &ChatMessage{
			ID: triggerID, ConversationID: in.ConversationID, Role: RoleUser,
			Content: preparation.TriggerContent, ContentBlocks: messageTextContentBlocks(preparation.TriggerContent),
			TurnID: &turnID, AttemptID: &attemptID,
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}
		if err := NewMessageRepo(tx).createChatInTx(ctx, tx, chat); err != nil {
			return err
		}
		preparation.Trigger = *chat
		if preparation.DisplayID != nil {
			result := tx.Model(&DisplayMessage{}).
				Where("id = ? AND conversation_id = ? AND invalidated_at IS NULL", *preparation.DisplayID, in.ConversationID).
				Updates(map[string]any{"chat_message_id": triggerID, "turn_id": turn.ID, "attempt_id": attempt.ID})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrNotFound
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &preparation, nil
}

func RegenerateRequestIdempotencyKey(requestID uuid.UUID) string {
	return "message_regenerate_request:" + requestID.String()
}

type RewritePreparation struct {
	Turn                  ConversationTurn
	Attempt               GenerationAttempt
	Trigger               ChatMessage
	Display               DisplayMessage
	InvalidatedDisplayIDs []uuid.UUID
	Applied               bool
	RewardConsumed        bool
	RewardKey             string
}

// BeginRewrite performs the complete edit-and-regenerate branch mutation
// under the durable generation lease. No unbound display row is exposed
// between editing the old branch and creating its replacement attempt.
func (r *TurnRepo) BeginRewrite(ctx context.Context, in RewriteUserMessageInput) (*RewritePreparation, error) {
	var preparation RewritePreparation
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := AssertGenerationActive(ctx, tx, in.ConversationID); err != nil {
			return err
		}
		// client 路由不扣费：路由取自事务内行锁定的对话行。
		var conv Conversation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&conv, "id = ?", in.ConversationID).Error; err != nil {
			return err
		}
		clientRoute := conv.LLMRoute == LLMRouteClient
		var anchor DisplayMessage
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&anchor,
			"id = ? AND conversation_id = ? AND invalidated_at IS NULL", in.MessageID, in.ConversationID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if anchor.Type != "user" || anchor.ChatMessageID == nil {
			return ErrMessageNotUser
		}
		var original ChatMessage
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&original,
			"id = ? AND conversation_id = ? AND role = ? AND invalidated_at IS NULL", *anchor.ChatMessageID, in.ConversationID, RoleUser).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrMessageNotUser
			}
			return err
		}
		if original.TurnID == nil {
			return errors.New("目标消息缺少回合关联")
		}
		policy := platformcapability.MessageRewritePolicy()
		if !policy.Enabled {
			return errors.New("当前未开启剧情改写")
		}
		rewardConsumed := false
		if policy.CostQuantity > 0 && !clientRoute {
			result, err := NewRewardRepository(tx).ConsumeInCurrentTx(ctx, RewardMutation{
				PlayerID: in.PlayerID, RewardKey: policy.CostRewardKey, Quantity: policy.CostQuantity,
				SourceType: "message_rewrite", SourceID: in.IdempotencyKey, IdempotencyKey: in.IdempotencyKey,
				Metadata: JSONMap{"conversation_id": in.ConversationID.String(), "message_id": in.MessageID.String()},
			})
			if err != nil {
				return err
			}
			rewardConsumed = result != nil && result.Applied
			if result != nil && !result.Applied {
				preparation.Display = anchor
				return nil
			}
		} else if anchor.Markdown == in.Content {
			preparation.Display = anchor
			return nil
		}
		invalidated, err := prepareBranchInTx(ctx, tx, in.ConversationID, *original.TurnID, &anchor.ID, in.InitialState, "rewrite")
		if err != nil {
			return err
		}
		turn, attempt, err := beginTurnInTx(ctx, tx, in.ConversationID, "rewrite")
		if err != nil {
			return err
		}
		now := time.Now()
		trigger := ChatMessage{
			ID: uuid.New(), ConversationID: in.ConversationID, Role: RoleUser, Kind: "text",
			Content: in.Content, ContentBlocks: messageTextContentBlocks(in.Content),
			TurnID: &turn.ID, AttemptID: &attempt.ID, CreatedAt: now, UpdatedAt: now,
		}
		if err := NewMessageRepo(tx).createChatInTx(ctx, tx, &trigger); err != nil {
			return err
		}
		anchor.Markdown = in.Content
		anchor.ChatMessageID = &trigger.ID
		anchor.TurnID = &turn.ID
		anchor.AttemptID = &attempt.ID
		if err := tx.Save(&anchor).Error; err != nil {
			return err
		}
		preparation = RewritePreparation{
			Turn: *turn, Attempt: *attempt, Trigger: trigger, Display: anchor,
			InvalidatedDisplayIDs: invalidated, Applied: true,
			RewardConsumed: rewardConsumed, RewardKey: policy.CostRewardKey,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &preparation, nil
}

func prepareBranchInTx(
	ctx context.Context,
	tx *gorm.DB,
	conversationID, targetTurnID uuid.UUID,
	preserveDisplayID *uuid.UUID,
	initialState JSONMap,
	reason string,
) ([]uuid.UUID, error) {
	var conv Conversation
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&conv, "id = ?", conversationID).Error; err != nil {
		return nil, err
	}
	var target ConversationTurn
	if err := tx.First(&target, "id = ? AND conversation_id = ? AND invalidated_at IS NULL", targetTurnID, conversationID).Error; err != nil {
		return nil, err
	}
	if conv.CampaignID != nil {
		var sideEffects int64
		if err := tx.Model(&CampaignActionCommit{}).
			Joins("JOIN conversation_turns t ON t.id = campaign_action_commits.turn_id").
			Where("campaign_action_commits.campaign_id = ? AND t.conversation_id = ? AND t.turn_seq >= ?", *conv.CampaignID, conversationID, target.TurnSeq).
			Count(&sideEffects).Error; err != nil {
			return nil, err
		}
		if sideEffects == 0 {
			if err := tx.Model(&CampaignEvent{}).
				Joins("JOIN conversation_turns t ON t.id = campaign_events.turn_id").
				Where("campaign_events.campaign_id = ? AND t.conversation_id = ? AND t.turn_seq >= ?", *conv.CampaignID, conversationID, target.TurnSeq).
				Count(&sideEffects).Error; err != nil {
				return nil, err
			}
		}
		if sideEffects > 0 {
			return nil, ErrCampaignHistoryImmutable
		}
	}

	now := time.Now()
	var previous StateRevision
	err := tx.Table("state_revisions AS r").Select("r.*").
		Joins("LEFT JOIN conversation_turns t ON t.id = r.turn_id").
		Where("r.conversation_id = ? AND r.invalidated_at IS NULL AND (t.turn_seq < ? OR r.turn_id IS NULL)", conversationID, target.TurnSeq).
		Order("r.created_at DESC").First(&previous).Error
	restored := initialState
	if err == nil {
		restored = previous.AfterState
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if restored == nil {
		restored = JSONMap{}
	}
	if err := tx.Model(&ConversationState{}).Where("conversation_id = ?", conversationID).
		Updates(map[string]any{"state": restored, "updated_at": now}).Error; err != nil {
		return nil, err
	}

	turnIDs := tx.Model(&ConversationTurn{}).Select("id").
		Where("conversation_id = ? AND turn_seq >= ?", conversationID, target.TurnSeq)
	var invalidatedDisplayIDs []uuid.UUID
	displayIDsQuery := tx.Model(&DisplayMessage{}).Select("id").
		Where("conversation_id = ? AND turn_id IN (?) AND invalidated_at IS NULL", conversationID, turnIDs)
	if preserveDisplayID != nil {
		displayIDsQuery = displayIDsQuery.Where("id <> ?", *preserveDisplayID)
	}
	if err := displayIDsQuery.Pluck("id", &invalidatedDisplayIDs).Error; err != nil {
		return nil, err
	}
	invalid := map[string]any{"invalidated_at": now, "invalidation_reason": reason}
	if err := tx.Model(&ChatMessage{}).Where("conversation_id = ? AND turn_id IN (?) AND invalidated_at IS NULL", conversationID, turnIDs).
		Updates(invalid).Error; err != nil {
		return nil, err
	}
	displayQuery := tx.Model(&DisplayMessage{}).
		Where("conversation_id = ? AND turn_id IN (?) AND invalidated_at IS NULL", conversationID, turnIDs)
	if preserveDisplayID != nil {
		displayQuery = displayQuery.Where("id <> ?", *preserveDisplayID)
	}
	if err := displayQuery.Updates(invalid).Error; err != nil {
		return nil, err
	}
	if err := tx.Model(&StateRevision{}).Where("conversation_id = ? AND turn_id IN (?) AND invalidated_at IS NULL", conversationID, turnIDs).
		Updates(invalid).Error; err != nil {
		return nil, err
	}
	if err := tx.Model(&RollHistoryEntry{}).Where("conversation_id = ? AND turn_id IN (?) AND invalidated_at IS NULL", conversationID.String(), turnIDs).
		Updates(invalid).Error; err != nil {
		return nil, err
	}
	if err := tx.Model(&ConversationTurn{}).Where("conversation_id = ? AND turn_seq >= ? AND invalidated_at IS NULL", conversationID, target.TurnSeq).
		Updates(invalid).Error; err != nil {
		return nil, err
	}
	if err := tx.Model(&GenerationAttempt{}).Where("conversation_id = ? AND turn_id IN (?) AND status = ?", conversationID, turnIDs, AttemptStatusRunning).
		Updates(map[string]any{"status": AttemptStatusCancelled, "failure_stage": reason, "error_code": reason, "finished_at": now}).Error; err != nil {
		return nil, err
	}
	if err := tx.Model(&ConversationMemory{}).
		Where("conversation_id = ? AND covered_until_turn_seq >= ? AND invalidated_at IS NULL", conversationID, target.TurnSeq).
		Updates(invalid).Error; err != nil {
		return nil, err
	}
	if err := tx.Model(&MemoryJob{}).Where("conversation_id = ? AND status IN ?", conversationID, []string{MemoryJobPending, MemoryJobRunning}).
		Updates(map[string]any{
			"status": MemoryJobCancelled, "last_error_code": reason, "worker_id": "", "lease_token": nil,
			"lease_expires_at": nil, "finished_at": now, "updated_at": now,
		}).Error; err != nil {
		return nil, err
	}
	var head ConversationMemory
	var headID *uuid.UUID
	if err := tx.Where("conversation_id = ? AND invalidated_at IS NULL", conversationID).
		Order("version DESC").First(&head).Error; err == nil {
		headID = &head.ID
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err := tx.Model(&Conversation{}).Where("id = ?", conversationID).
		Updates(map[string]any{"branch_epoch": conv.BranchEpoch + 1, "current_memory_id": headID, "updated_at": now}).Error; err != nil {
		return nil, err
	}
	return invalidatedDisplayIDs, nil
}

// Begin atomically allocates a stable turn sequence and pins the current memory.
func (r *TurnRepo) Begin(ctx context.Context, conversationID uuid.UUID, triggerType string) (*ConversationTurn, *GenerationAttempt, error) {
	var turn ConversationTurn
	var attempt GenerationAttempt
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := AssertGenerationActive(ctx, tx, conversationID); err != nil {
			return err
		}
		createdTurn, createdAttempt, err := beginTurnInTx(ctx, tx, conversationID, triggerType)
		if err != nil {
			return err
		}
		turn, attempt = *createdTurn, *createdAttempt
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return &turn, &attempt, nil
}

func beginTurnInTx(ctx context.Context, tx *gorm.DB, conversationID uuid.UUID, triggerType string) (*ConversationTurn, *GenerationAttempt, error) {
	var conv Conversation
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&conv, "id = ?", conversationID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	var active int64
	if err := tx.Model(&ConversationTurn{}).
		Where("conversation_id = ? AND status = ? AND invalidated_at IS NULL", conversationID, TurnStatusPending).
		Count(&active).Error; err != nil {
		return nil, nil, err
	}
	if active > 0 {
		return nil, nil, ErrTurnInProgress
	}

	now := time.Now()
	turn := &ConversationTurn{
		ID: uuid.New(), ConversationID: conversationID, TurnSeq: conv.NextTurnSeq,
		TriggerType: triggerType, Status: TurnStatusPending, CreatedAt: now,
	}
	coveredUntil := int64(0)
	if conv.CurrentMemoryID != nil {
		var memory ConversationMemory
		if err := tx.First(&memory, "id = ? AND conversation_id = ? AND invalidated_at IS NULL", *conv.CurrentMemoryID, conversationID).Error; err != nil {
			return nil, nil, err
		}
		coveredUntil = memory.CoveredUntilTurnSeq
	}
	attempt := &GenerationAttempt{
		ID: uuid.New(), ConversationID: conversationID, TurnID: turn.ID, AttemptNo: 1,
		BranchEpoch: conv.BranchEpoch, MemoryID: conv.CurrentMemoryID,
		MemoryCoveredUntilTurnSeq: coveredUntil, Status: AttemptStatusRunning, StartedAt: now,
	}
	if err := tx.Create(turn).Error; err != nil {
		return nil, nil, err
	}
	if err := tx.Create(attempt).Error; err != nil {
		return nil, nil, err
	}
	if err := tx.Model(&Conversation{}).Where("id = ?", conversationID).
		Update("next_turn_seq", conv.NextTurnSeq+1).Error; err != nil {
		return nil, nil, err
	}
	return turn, attempt, nil
}

func (r *TurnRepo) Pending(ctx context.Context, conversationID uuid.UUID) (*ConversationTurn, *GenerationAttempt, error) {
	var turn ConversationTurn
	err := r.db.WithContext(ctx).
		Where("conversation_id = ? AND status = ? AND invalidated_at IS NULL", conversationID, TurnStatusPending).
		Order("turn_seq DESC").First(&turn).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	var attempt GenerationAttempt
	err = r.db.WithContext(ctx).
		Where("turn_id = ? AND status = ?", turn.ID, AttemptStatusRunning).
		Order("attempt_no DESC").First(&attempt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, ErrNotFound
	}
	return &turn, &attempt, err
}

func (r *TurnRepo) Seal(ctx context.Context, turnID, attemptID uuid.UUID, completed bool, failureStage, errorCode string) error {
	attemptStatus := AttemptStatusFailed
	if completed {
		attemptStatus = AttemptStatusCompleted
	}
	return r.SealStatus(ctx, turnID, attemptID, attemptStatus, failureStage, errorCode)
}

func (r *TurnRepo) SealStatus(ctx context.Context, turnID, attemptID uuid.UUID, attemptStatus, failureStage, errorCode string) error {
	now := time.Now()
	turnStatus := TurnStatusFailed
	switch attemptStatus {
	case AttemptStatusCompleted:
		turnStatus = TurnStatusCompleted
		failureStage, errorCode = "", ""
	case AttemptStatusPartial:
		turnStatus = TurnStatusPartial
	case AttemptStatusCancelled:
		turnStatus = TurnStatusCancelled
	case AttemptStatusFailed:
	default:
		return errors.New("无效的生成 attempt 终态")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var attempt GenerationAttempt
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&attempt, "id = ? AND turn_id = ? AND status = ?", attemptID, turnID, AttemptStatusRunning).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if attemptStatus != AttemptStatusCompleted && attempt.CommittedMutatingToolCount > 0 {
			attemptStatus = AttemptStatusPartial
			turnStatus = TurnStatusPartial
		}
		result := tx.Model(&GenerationAttempt{}).
			Where("id = ? AND turn_id = ? AND status = ?", attemptID, turnID, AttemptStatusRunning).
			Updates(map[string]any{
				"status": attemptStatus, "failure_stage": failureStage, "error_code": errorCode, "finished_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrNotFound
		}
		return tx.Model(&ConversationTurn{}).
			Where("id = ? AND status = ?", turnID, TurnStatusPending).
			Updates(map[string]any{"status": turnStatus, "failure_stage": failureStage, "sealed_at": now}).Error
	})
}

// FailOrphaned 封口所有“仍在 running 但其对话已无活动生成租约”的 attempt、
// step 与 turn。以租约为准而不是一刀切，多实例部署下不会误伤别的实例。
func (r *TurnRepo) FailOrphaned(ctx context.Context) (int64, error) {
	now := time.Now()
	var sealed int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var attempts []GenerationAttempt
		if err := tx.Where("status = ?", AttemptStatusRunning).
			Where("NOT EXISTS (?)", tx.Model(&ConversationGenerationRun{}).
				Select("1").Where("conversation_generation_runs.conversation_id = generation_attempts.conversation_id AND status IN ?", activeGenerationRunStatuses())).
			Find(&attempts).Error; err != nil {
			return err
		}
		for _, attempt := range attempts {
			rows, err := sealLostAttemptInTx(tx, attempt, now)
			if err != nil {
				return err
			}
			sealed += rows
		}
		result := tx.Model(&ConversationTurn{}).
			Where("status = ? AND NOT EXISTS (SELECT 1 FROM generation_attempts a WHERE a.turn_id = conversation_turns.id AND a.status = ?)", TurnStatusPending, AttemptStatusRunning).
			Where("NOT EXISTS (?)", tx.Model(&ConversationGenerationRun{}).
				Select("1").Where("conversation_generation_runs.conversation_id = conversation_turns.conversation_id AND status IN ?", activeGenerationRunStatuses())).
			Updates(map[string]any{"status": TurnStatusFailed, "failure_stage": "generation_lost", "sealed_at": now})
		if result.Error != nil {
			return result.Error
		}
		sealed += result.RowsAffected
		return nil
	})
	return sealed, err
}

// sealLostAttemptInTx 把一个失去生成租约的 running attempt 连同其 running step 和
// pending turn 一起封口：已提交写工具的记为 partial，其余记为 failed。
// 返回被封口的 turn 行数。
func sealLostAttemptInTx(tx *gorm.DB, attempt GenerationAttempt, now time.Time) (int64, error) {
	attemptStatus := AttemptStatusFailed
	turnStatus := TurnStatusFailed
	failureStage := "generation_lost"
	errorCode := "generation_lost"
	if attempt.CommittedMutatingToolCount > 0 {
		attemptStatus = AttemptStatusPartial
		turnStatus = TurnStatusPartial
		failureStage = "generation_lost_after_commit"
		errorCode = "partial_after_generation_lost"
	}
	if err := tx.Model(&GenerationAttempt{}).Where("id = ? AND status = ?", attempt.ID, AttemptStatusRunning).
		Updates(map[string]any{"status": attemptStatus, "failure_stage": failureStage, "error_code": errorCode, "finished_at": now}).Error; err != nil {
		return 0, err
	}
	if err := tx.Model(&AgentStep{}).Where("attempt_id = ? AND status = ?", attempt.ID, AgentStepRunning).
		Updates(map[string]any{"status": AgentStepFailed, "error_code": "generation_lost", "finished_at": now}).Error; err != nil {
		return 0, err
	}
	result := tx.Model(&ConversationTurn{}).Where("id = ? AND status = ?", attempt.TurnID, TurnStatusPending).
		Updates(map[string]any{"status": turnStatus, "failure_stage": failureStage, "sealed_at": now})
	return result.RowsAffected, result.Error
}

// sealLostGenerationInTx 在抢占过期生成租约的同一事务里，封口该对话遗留的
// running attempt、pending turn 和仍为 received 的工具调用。调用方必须已持有对话行锁。
func sealLostGenerationInTx(tx *gorm.DB, conversationID uuid.UUID, now time.Time) error {
	var attempts []GenerationAttempt
	if err := tx.Where("conversation_id = ? AND status = ?", conversationID, AttemptStatusRunning).
		Find(&attempts).Error; err != nil {
		return err
	}
	for _, attempt := range attempts {
		if _, err := sealLostAttemptInTx(tx, attempt, now); err != nil {
			return err
		}
	}
	if err := tx.Model(&ConversationTurn{}).
		Where("conversation_id = ? AND status = ?", conversationID, TurnStatusPending).
		Updates(map[string]any{"status": TurnStatusFailed, "failure_stage": "generation_lost", "sealed_at": now}).Error; err != nil {
		return err
	}
	return tx.Model(&AgentToolInvocation{}).
		Where("conversation_id = ? AND status = ?", conversationID, ToolInvocationReceived).
		Updates(map[string]any{
			"status": ToolInvocationAbandoned, "result_code": "generation_lost",
			"result_json": JSONMap{"ok": false, "code": "generation_lost", "message": "工具调用所属的生成已经失效，未提交"},
			"updated_at":  now,
		}).Error
}
