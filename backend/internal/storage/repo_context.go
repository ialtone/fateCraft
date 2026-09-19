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
		Order("created_at DESC, id DESC").First(&log).Error
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

// List 返回管理员诊断列表。Prompt 分段仅在按 ID 读取详情时加载。
func (r *ContextRepo) List(ctx context.Context, limit int, conversationID *uuid.UUID) ([]ContextBuildLog, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	query := r.db.WithContext(ctx)
	if conversationID != nil {
		query = query.Where("conversation_id = ?", *conversationID)
	}
	var rows []ContextBuildLog
	err := query.Select(
		"id", "conversation_id", "trigger_message_id", "model", "max_context_tokens",
		"reserved_response_tokens", "used_prompt_tokens", "included_message_ids", "activated_world_entry_ids",
		"turn_id", "attempt_id", "branch_epoch", "memory_id", "memory_covered_until_turn_seq",
		"memory_lag", "omitted_turn_from", "omitted_turn_until", "memory_job_id", "created_at",
	).Order("created_at DESC, id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}
