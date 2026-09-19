package orchestration

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ialtone/fatecraft/internal/gameplay"
	"github.com/ialtone/fatecraft/internal/operation"
	"github.com/ialtone/fatecraft/internal/state"
	"github.com/ialtone/fatecraft/internal/storage"
)

// RollCommitInput is the complete authoritative result of one server roll.
// A rejected gate has Record=false and no EffectPatch, but its two result
// messages still use the same atomic persistence path.
type RollCommitInput struct {
	ConversationID uuid.UUID
	Schema         gameplay.StateSchema
	ExpectedState  map[string]any
	Record         bool
	RollKey        string
	Title          string
	Category       string
	Success        bool
	Content        string
	Markdown       string
	Metadata       map[string]any
	EffectPatch    map[string]any
}

type RollCommitResult struct {
	Chat         *storage.ChatMessage
	Display      *storage.DisplayMessage
	State        map[string]any
	StateChanged bool
}

// RollCommitter atomically persists all facts produced by a roll. This avoids
// histories that claim a result whose message or state effect never committed.
type RollCommitter struct {
	db *gorm.DB
}

func NewRollCommitter(db *gorm.DB) *RollCommitter {
	return &RollCommitter{db: db}
}

func (c *RollCommitter) Commit(ctx context.Context, input RollCommitInput) (*RollCommitResult, error) {
	if c == nil || c.db == nil {
		return nil, errors.New("判定提交器未配置")
	}
	if input.ConversationID == uuid.Nil {
		return nil, errors.New("判定提交缺少 conversation_id")
	}
	if err := state.ValidatePatch(input.Schema, storage.SourceSystem, input.EffectPatch); err != nil {
		return nil, fmt.Errorf("判定状态效果无效: %w", err)
	}

	var result *RollCommitResult
	err := c.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		result, err = commitRollInTx(ctx, &gormRollCommitTx{db: tx}, input)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// CommitInCurrentTx commits a roll using the caller's already-open database
// transaction. Agent tool commits use this to keep the roll, state revision,
// tool result and attempt counter atomic.
func (c *RollCommitter) CommitInCurrentTx(ctx context.Context, tx *gorm.DB, input RollCommitInput) (*RollCommitResult, error) {
	if c == nil || tx == nil {
		return nil, errors.New("判定提交事务未配置")
	}
	if input.ConversationID == uuid.Nil {
		return nil, errors.New("判定提交缺少 conversation_id")
	}
	if err := state.ValidatePatch(input.Schema, storage.SourceSystem, input.EffectPatch); err != nil {
		return nil, fmt.Errorf("判定状态效果无效: %w", err)
	}
	return commitRollInTx(ctx, &gormRollCommitTx{db: tx}, input)
}

type rollCommitTx interface {
	LockConversation(context.Context, uuid.UUID) error
	GetStateForUpdate(context.Context, uuid.UUID) (*storage.ConversationState, error)
	CreateRollHistory(context.Context, *storage.RollHistoryEntry) error
	CreateChat(context.Context, *storage.ChatMessage) error
	CreateDisplay(context.Context, *storage.DisplayMessage) error
	UpsertState(context.Context, *storage.ConversationState) error
	CreateRevision(context.Context, *storage.StateRevision) error
}

func commitRollInTx(ctx context.Context, tx rollCommitTx, input RollCommitInput) (*RollCommitResult, error) {
	if err := tx.LockConversation(ctx, input.ConversationID); err != nil {
		return nil, err
	}
	stored, err := tx.GetStateForUpdate(ctx, input.ConversationID)
	var before map[string]any
	switch {
	case errors.Is(err, storage.ErrNotFound):
		before = gameplay.InitialState(input.Schema)
	case err != nil:
		return nil, err
	case stored == nil:
		return nil, errors.New("判定提交读取到空状态")
	default:
		before = stored.State
	}
	if input.ExpectedState != nil && !statesEqual(before, input.ExpectedState) {
		return nil, errors.New("角色状态在判定计算期间已变化，请重新发起判定")
	}

	now := time.Now()
	chatMessage := &storage.ChatMessage{
		ID:             uuid.New(),
		ConversationID: input.ConversationID,
		Role:           storage.RoleSystem,
		Content:        input.Content,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	metadata := input.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	displayMessage := &storage.DisplayMessage{
		ID:             uuid.New(),
		ConversationID: input.ConversationID,
		ChatMessageID:  &chatMessage.ID,
		Type:           "roll_result",
		Markdown:       input.Markdown,
		Metadata:       storage.JSONMap(metadata),
		CreatedAt:      now,
	}

	if input.Record {
		if err := tx.CreateRollHistory(ctx, &storage.RollHistoryEntry{
			ID:             uuid.NewString(),
			ConversationID: input.ConversationID.String(),
			RollKey:        input.RollKey,
			TitleSnapshot:  input.Title,
			Category:       input.Category,
			Success:        input.Success,
			CreatedAt:      now,
		}); err != nil {
			return nil, err
		}
	}
	if err := tx.CreateChat(ctx, chatMessage); err != nil {
		return nil, err
	}
	if err := tx.CreateDisplay(ctx, displayMessage); err != nil {
		return nil, err
	}

	result := &RollCommitResult{Chat: chatMessage, Display: displayMessage}
	if len(input.EffectPatch) == 0 {
		return result, nil
	}

	after, applied, skipped := state.ApplyPatch(input.Schema, storage.SourceSystem, before, input.EffectPatch)
	if len(skipped) > 0 {
		return nil, &state.PatchValidationError{Keys: skipped}
	}
	if len(applied) == 0 {
		result.State = after
		return result, nil
	}
	if err := tx.UpsertState(ctx, &storage.ConversationState{
		ConversationID: input.ConversationID,
		State:          storage.JSONMap(after),
		SchemaVersion:  input.Schema.SchemaVersion,
		UpdatedAt:      now,
	}); err != nil {
		return nil, err
	}
	if err := tx.CreateRevision(ctx, &storage.StateRevision{
		ID:               uuid.New(),
		ConversationID:   input.ConversationID,
		Source:           storage.SourceSystem,
		RelatedMessageID: &chatMessage.ID,
		Patch:            storage.JSONMap(applied),
		BeforeState:      storage.JSONMap(before),
		AfterState:       storage.JSONMap(after),
		CreatedAt:        now,
	}); err != nil {
		return nil, err
	}
	result.State = after
	result.StateChanged = true
	return result, nil
}

func statesEqual(left, right map[string]any) bool {
	return reflect.DeepEqual(left, right)
}

type gormRollCommitTx struct {
	db *gorm.DB
}

func (tx *gormRollCommitTx) LockConversation(ctx context.Context, convID uuid.UUID) error {
	var conversation storage.Conversation
	err := tx.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id").First(&conversation, "id = ?", convID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return storage.ErrNotFound
	}
	return err
}

func (tx *gormRollCommitTx) GetStateForUpdate(ctx context.Context, convID uuid.UUID) (*storage.ConversationState, error) {
	var conversationState storage.ConversationState
	err := tx.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&conversationState, "conversation_id = ?", convID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, storage.ErrNotFound
	}
	return &conversationState, err
}

func (tx *gormRollCommitTx) CreateRollHistory(ctx context.Context, entry *storage.RollHistoryEntry) error {
	if metadata, ok := operation.FromContext(ctx); ok {
		entry.TurnID = &metadata.TurnID
		entry.AttemptID = &metadata.AttemptID
	}
	return tx.db.WithContext(ctx).Create(entry).Error
}

func (tx *gormRollCommitTx) CreateChat(ctx context.Context, message *storage.ChatMessage) error {
	return storage.NewMessageRepo(tx.db).CreateChat(ctx, message)
}

func (tx *gormRollCommitTx) CreateDisplay(ctx context.Context, message *storage.DisplayMessage) error {
	return storage.NewMessageRepo(tx.db).CreateDisplay(ctx, message)
}

func (tx *gormRollCommitTx) UpsertState(ctx context.Context, conversationState *storage.ConversationState) error {
	return tx.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "conversation_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"state", "schema_version", "updated_at"}),
	}).Create(conversationState).Error
}

func (tx *gormRollCommitTx) CreateRevision(ctx context.Context, revision *storage.StateRevision) error {
	return storage.NewStateRepo(tx.db).AddRevision(ctx, revision)
}
