package storage

import (
	"context"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/operation"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// StateRepo 负责对话的结构化状态及其修订历史。
type StateRepo struct{ db *gorm.DB }

func NewStateRepo(db *gorm.DB) *StateRepo { return &StateRepo{db: db} }

// Get 读取对话状态，不存在时返回 ErrNotFound。
func (r *StateRepo) Get(ctx context.Context, convID uuid.UUID) (*ConversationState, error) {
	var s ConversationState
	err := r.db.WithContext(ctx).First(&s, "conversation_id = ?", convID).Error
	if err == gorm.ErrRecordNotFound {
		return nil, ErrNotFound
	}
	return &s, err
}

// Transaction 在事务中执行 fn，fn 收到一个绑定事务连接的 StateRepo。
func (r *StateRepo) Transaction(ctx context.Context, fn func(tx *StateRepo) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&StateRepo{db: tx})
	})
}

// GetForUpdate 带行锁（SELECT ... FOR UPDATE）读取对话状态，须在事务内调用。
// 不存在时返回 ErrNotFound（此时无行可锁，由唯一键约束保证并发插入安全）。
func (r *StateRepo) GetForUpdate(ctx context.Context, convID uuid.UUID) (*ConversationState, error) {
	var s ConversationState
	err := r.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&s, "conversation_id = ?", convID).Error
	if err == gorm.ErrRecordNotFound {
		return nil, ErrNotFound
	}
	return &s, err
}

// Upsert 插入或更新对话状态。
func (r *StateRepo) Upsert(ctx context.Context, s *ConversationState) error {
	if err := AssertGenerationActive(ctx, r.db, s.ConversationID); err != nil {
		return err
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "conversation_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"state", "schema_version", "updated_at"}),
	}).Create(s).Error
}

// AddRevision 追加一条状态修订记录。
func (r *StateRepo) AddRevision(ctx context.Context, rev *StateRevision) error {
	if err := AssertGenerationActive(ctx, r.db, rev.ConversationID); err != nil {
		return err
	}
	if metadata, ok := operation.FromContext(ctx); ok {
		rev.TurnID = &metadata.TurnID
		rev.AttemptID = &metadata.AttemptID
	}
	return r.db.WithContext(ctx).Create(rev).Error
}

// ListRevisions 按时间倒序返回状态修订。
func (r *StateRepo) ListRevisions(ctx context.Context, convID uuid.UUID, limit int) ([]StateRevision, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := r.db.WithContext(ctx).Where("conversation_id = ? AND invalidated_at IS NULL", convID).
		Order("created_at DESC, id DESC").Limit(limit)
	var out []StateRevision
	err := q.Find(&out).Error
	return out, err
}
