package storage

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ContextRepo 负责上下文构建日志，严格按 conversation_id 隔离。
type ContextRepo struct{ db *gorm.DB }

func NewContextRepo(db *gorm.DB) *ContextRepo { return &ContextRepo{db: db} }

func (r *ContextRepo) Create(ctx context.Context, log *ContextBuildLog) error {
	return r.db.WithContext(ctx).Create(log).Error
}

// Last 返回当前对话最近一次上下文构建日志。
func (r *ContextRepo) Last(ctx context.Context, convID uuid.UUID) (*ContextBuildLog, error) {
	var log ContextBuildLog
	err := r.db.WithContext(ctx).
		Where("conversation_id = ?", convID).
		Order("created_at DESC").First(&log).Error
	if err == gorm.ErrRecordNotFound {
		return nil, ErrNotFound
	}
	return &log, err
}

// Get 按 id 读取上下文构建日志。
func (r *ContextRepo) Get(ctx context.Context, id uuid.UUID) (*ContextBuildLog, error) {
	var log ContextBuildLog
	err := r.db.WithContext(ctx).First(&log, "id = ?", id).Error
	if err == gorm.ErrRecordNotFound {
		return nil, ErrNotFound
	}
	return &log, err
}
