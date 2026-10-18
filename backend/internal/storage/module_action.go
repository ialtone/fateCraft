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
