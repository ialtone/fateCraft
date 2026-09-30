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
	if row.BodyMetadata == nil {
		row.BodyMetadata = JSONMap{}
	}
	if len(row.RequestBody) == 0 {
		row.RequestBody = JSONDoc(`{}`)
	}
	return r.db.WithContext(ctx).Create(row).Error
}

func (r *LLMRequestLogRepo) List(ctx context.Context, filter LogQuery) ([]LLMRequestLog, string, bool, error) {
	query, err := filter.query(ctx, r.db, "llm")
	if err != nil {
		return nil, "", false, err
	}
	var rows []LLMRequestLog
	err = query.Select(
		"id", "trace_id", "protocol", "started_at", "transport_attempts", "input_tokens", "output_tokens", "error_code", "body_status", "body_metadata", "settings_source", "conversation_id", "message_id", "purpose", "attempt_id", "memory_job_id", "memory_id",
		"inquiry_id", "inquiry_snapshot_id", "inquiry_snapshot_branch_epoch", "inquiry_snapshot_revision", "model", "stream", "response_status",
		"finish_reason", "received_done", "success", "audit_truncated", "error", "duration_ms", "created_at",
		"settings_revision", "output_mode", "contract_name", "contract_version", "contract_fingerprint",
		"transport_success", "structured_success", "structured_error_code", "output_types",
	).Find(&rows).Error
	if err != nil {
		return nil, "", false, err
	}
	more := len(rows) > filter.Limit
	if more {
		rows = rows[:filter.Limit]
	}
	next := ""
	if more {
		last := rows[len(rows)-1]
		next = filter.next("llm", last.CreatedAt, last.ID)
	}
	return rows, next, more, nil
}

func (r *LLMRequestLogRepo) Get(ctx context.Context, id uuid.UUID) (*LLMRequestLog, error) {
	var row LLMRequestLog
	err := r.db.WithContext(ctx).First(&row, "id = ?", id).Error
	if err == gorm.ErrRecordNotFound {
		return nil, ErrNotFound
	}
	return &row, err
}
