package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/operation"
	"gorm.io/gorm"
)

// RollHistoryEntry 单次天命判定的流水记录（持久化历史调整所需的最小信息）。
type RollHistoryEntry struct {
	ID                 string     `gorm:"column:id;primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	ConversationID     string     `gorm:"column:conversation_id;type:uuid;not null" json:"conversation_id"`
	RollKey            string     `gorm:"column:roll_key;not null" json:"roll_key"`
	TitleSnapshot      string     `gorm:"column:title_snapshot;not null" json:"title"`
	Category           string     `gorm:"column:category;not null" json:"category"`
	Success            bool       `gorm:"column:success;not null" json:"success"`
	TurnID             *uuid.UUID `gorm:"column:turn_id;type:uuid" json:"turn_id,omitempty"`
	AttemptID          *uuid.UUID `gorm:"column:attempt_id;type:uuid" json:"attempt_id,omitempty"`
	InvalidatedAt      *time.Time `gorm:"column:invalidated_at" json:"invalidated_at,omitempty"`
	InvalidationReason string     `gorm:"column:invalidation_reason" json:"invalidation_reason,omitempty"`
	CreatedAt          time.Time  `gorm:"column:created_at;default:CURRENT_TIMESTAMP" json:"created_at"`
}

func (RollHistoryEntry) TableName() string { return "roll_history" }

// RollHistoryRepo 判定历史数据访问层。
type RollHistoryRepo struct {
	db *gorm.DB
}

func NewRollHistoryRepo(db *gorm.DB) *RollHistoryRepo {
	return &RollHistoryRepo{db: db}
}

// Append 追加一条判定流水。
func (r *RollHistoryRepo) Append(ctx context.Context, convID uuid.UUID, rollKey, title, category string, success bool) error {
	entry := &RollHistoryEntry{
		ConversationID: convID.String(),
		RollKey:        rollKey,
		TitleSnapshot:  title,
		Category:       category,
		Success:        success,
	}
	if metadata, ok := operation.FromContext(ctx); ok {
		entry.TurnID = &metadata.TurnID
		entry.AttemptID = &metadata.AttemptID
	}
	return r.db.WithContext(ctx).Create(entry).Error
}

// Recent 按时间倒序返回某对话最近 limit 条判定流水（最新在前）。
func (r *RollHistoryRepo) Recent(ctx context.Context, convID uuid.UUID, limit int) ([]RollHistoryEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []RollHistoryEntry
	err := r.db.WithContext(ctx).
		Where("conversation_id = ? AND invalidated_at IS NULL", convID.String()).
		Order("created_at DESC, id DESC").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}

// HasSuccess 判断某次挑战是否存在成功的服务端判定流水。
func (r *RollHistoryRepo) HasSuccess(ctx context.Context, convID uuid.UUID) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&RollHistoryEntry{}).
		Where("conversation_id = ? AND success = ? AND invalidated_at IS NULL", convID.String(), true).
		Limit(1).
		Count(&count).Error
	return count > 0, err
}
