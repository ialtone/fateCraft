package storage

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/operation"
	"gorm.io/gorm"
)

var ErrModuleActionConflict = errors.New("玩法动作所属分支已变化，或请求标识已用于其他操作")

type ModuleActionCommit struct {
	ID             uuid.UUID `gorm:"type:uuid;primaryKey"`
	ConversationID uuid.UUID `gorm:"type:uuid"`
	BranchEpoch    int64
	RequestID      uuid.UUID `gorm:"type:uuid"`
	ActionKey      string
	ArgumentsHash  string
	TurnID         uuid.UUID `gorm:"type:uuid"`
	Result         JSONMap   `gorm:"type:jsonb"`
	Narrative      JSONMap   `gorm:"type:jsonb"`
	SchemaVersion  int16
	NarratedTurnID *uuid.UUID `gorm:"type:uuid"`
	NarratedAt     *time.Time
	CreatedAt      time.Time
}

type CommitModuleActionInput struct {
	ConversationID, PlayerID, RequestID uuid.UUID
	BranchEpoch                         int64
	ActionKey, ArgumentsHash, Label     string
}

func (r *ChallengeRepository) CommitModuleAction(ctx context.Context, in CommitModuleActionInput, execute func(context.Context, *StateRepo, *ChallengeRepository, *Conversation) (JSONMap, error)) (JSONMap, error) {
	var result JSONMap
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := NewChallengeRepository(tx)
		conv, err := repo.LockConversation(ctx, in.ConversationID)
		if err != nil {
			return err
		}
		if conv.PlayerID != in.PlayerID.String() {
			return ErrNotFound
		}
		if conv.BranchEpoch != in.BranchEpoch {
			return ErrModuleActionConflict
		}
		var existing ModuleActionCommit
		err = tx.Where("conversation_id = ? AND branch_epoch = ? AND request_id = ?", conv.ID, in.BranchEpoch, in.RequestID).First(&existing).Error
		if err == nil {
			if existing.ActionKey != in.ActionKey || existing.ArgumentsHash != in.ArgumentsHash {
				return ErrModuleActionConflict
			}
			result = existing.Result
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if conv.Status != StatusActive {
			return ErrConversationNotActive
		}
		if conv.IsProcessing {
			return ErrGenerationInProgress
		}
		preparation, err := NewTurnRepo(tx).BeginUserTurn(ctx, conv.ID, "user_input", in.Label, nil)
		if err != nil {
			return err
		}
		bound := operation.WithMetadata(ctx, operation.Metadata{ConversationID: conv.ID, TurnID: preparation.Turn.ID, AttemptID: preparation.Attempt.ID, BranchEpoch: conv.BranchEpoch, TriggerType: "user_input"})
		result, err = execute(bound, NewStateRepo(tx), repo, conv)
		if err != nil {
			return err
		}
		now := time.Now()
		message := ChatMessage{ID: uuid.New(), ConversationID: conv.ID, Role: RoleSystem, Kind: "text", Content: "已执行操作：" + in.Label, TurnID: &preparation.Turn.ID, AttemptID: &preparation.Attempt.ID, CreatedAt: now, UpdatedAt: now}
		if err := NewMessageRepo(tx).createChatInTx(bound, tx, &message); err != nil {
			return err
		}
		if err := tx.Create(&DisplayMessage{ID: uuid.New(), ConversationID: conv.ID, ChatMessageID: &message.ID, TurnID: &preparation.Turn.ID, AttemptID: &preparation.Attempt.ID, Type: "system", Markdown: message.Content, Metadata: JSONMap{"module_action": in.ActionKey}, CreatedAt: now}).Error; err != nil {
			return err
		}
		if err := tx.Model(&GenerationAttempt{}).Where("id = ?", preparation.Attempt.ID).Update("committed_mutating_tool_count", 1).Error; err != nil {
			return err
		}
		if err := NewTurnRepo(tx).SealStatus(bound, preparation.Turn.ID, preparation.Attempt.ID, AttemptStatusCompleted, "", ""); err != nil {
			return err
		}
		narrative := JSONMap{}
		if raw, ok := result["narrative"]; ok && raw != nil {
			encoded, marshalErr := json.Marshal(raw)
			if marshalErr != nil {
				return marshalErr
			}
			if unmarshalErr := json.Unmarshal(encoded, &narrative); unmarshalErr != nil {
				return unmarshalErr
			}
		}
		return tx.Create(&ModuleActionCommit{ID: uuid.New(), ConversationID: conv.ID, BranchEpoch: conv.BranchEpoch, RequestID: in.RequestID, ActionKey: in.ActionKey, ArgumentsHash: in.ArgumentsHash, TurnID: preparation.Turn.ID, Result: result, Narrative: narrative, SchemaVersion: 1, CreatedAt: now}).Error
	})
	return result, err
}

// UnnarratedModuleActions 返回当前有效回合中尚未被叙事消费的动作提交。
// 以源回合是否有效判断分支归属：回退点之前的动作仍有效，不能仅比较 epoch。
// 这些提交的 ActionResult 会在下一次叙事回合注入上下文（最多 limit 条）。
func (r *ChallengeRepository) UnnarratedModuleActions(ctx context.Context, conversationID uuid.UUID, limit int) ([]ModuleActionCommit, error) {
	if limit <= 0 {
		limit = 4
	}
	commits := []ModuleActionCommit{}
	validTurns := r.db.Model(&ConversationTurn{}).Select("id").
		Where("conversation_id = ? AND invalidated_at IS NULL", conversationID)
	err := r.db.WithContext(ctx).
		Where("conversation_id = ? AND narrated_turn_id IS NULL AND turn_id IN (?)", conversationID, validTurns).
		Order("created_at ASC, id ASC").Limit(limit).Find(&commits).Error
	return commits, err
}

// MarkModuleActionsNarrated 把注入过叙事的动作提交标记为已消费。幂等：
// 已标记的行保持其首次消费的回合。与展示消息落库分属两个事务，写入失败时
// 这些提交会在下一次叙事回合重注入（最坏情况是结果被复述一次，不损失事实）。
func (r *ChallengeRepository) MarkModuleActionsNarrated(ctx context.Context, conversationID, turnID uuid.UUID, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now()
	return r.db.WithContext(ctx).Model(&ModuleActionCommit{}).
		Where("conversation_id = ? AND id IN ? AND narrated_turn_id IS NULL", conversationID, ids).
		Updates(map[string]any{"narrated_turn_id": turnID, "narrated_at": now}).Error
}
