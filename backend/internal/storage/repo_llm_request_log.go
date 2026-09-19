package storage

import (
	"context"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"
)

// LLMRequestLogRepo 负责保存和查询管理员可见的模型请求诊断记录。
type LLMRequestLogRepo struct{ db *gorm.DB }

func NewLLMRequestLogRepo(db *gorm.DB) *LLMRequestLogRepo { return &LLMRequestLogRepo{db: db} }

func (r *LLMRequestLogRepo) Create(ctx context.Context, row *LLMRequestLog) error {
	if row.Purpose == "" {
		row.Purpose = "narrative"
	}
	if row.OutputTypes == nil {
		row.OutputTypes = pq.StringArray{}
	}
	return r.db.WithContext(ctx).Create(row).Error
}

func (r *LLMRequestLogRepo) List(ctx context.Context, limit int, conversationID *uuid.UUID) ([]LLMRequestLog, error) {
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
	var rows []LLMRequestLog
	err := query.Select(
		"id", "conversation_id", "message_id", "purpose", "attempt_id", "memory_job_id", "memory_id",
		"inquiry_id", "inquiry_snapshot_id", "inquiry_snapshot_branch_epoch", "inquiry_snapshot_revision", "model", "stream", "response_status",
		"finish_reason", "received_done", "success", "audit_truncated", "error", "duration_ms", "created_at",
		"settings_revision", "output_mode", "contract_name", "contract_version", "contract_fingerprint",
		"transport_success", "structured_success", "structured_error_code", "output_types",
	).Order("created_at DESC, id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *LLMRequestLogRepo) Get(ctx context.Context, id uuid.UUID) (*LLMRequestLog, error) {
	var row LLMRequestLog
	err := r.db.WithContext(ctx).First(&row, "id = ?", id).Error
	if err == gorm.ErrRecordNotFound {
		return nil, ErrNotFound
	}
	return &row, err
}
