package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// RollHistoryEntry 单次天命判定的流水记录（持久化历史调整所需的最小信息）。
type RollHistoryEntry struct {
	ID             string    `gorm:"column:id;primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	ConversationID string    `gorm:"column:conversation_id;type:uuid;not null" json:"conversation_id"`
	RollType       string    `gorm:"column:roll_type;not null" json:"roll_type"`
	Success        bool      `gorm:"column:success;not null" json:"success"`
	CreatedAt      time.Time `gorm:"column:created_at;default:CURRENT_TIMESTAMP" json:"created_at"`
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
func (r *RollHistoryRepo) Append(ctx context.Context, convID uuid.UUID, rollType string, success bool) error {
	return r.db.WithContext(ctx).Create(&RollHistoryEntry{
		ConversationID: convID.String(),
		RollType:       rollType,
		Success:        success,
	}).Error
}

// Recent 按时间倒序返回某对话最近 limit 条判定流水（最新在前）。
func (r *RollHistoryRepo) Recent(ctx context.Context, convID uuid.UUID, limit int) ([]RollHistoryEntry, error) {
	var rows []RollHistoryEntry
	err := r.db.WithContext(ctx).
		Where("conversation_id = ?", convID.String()).
		Order("created_at DESC").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}
