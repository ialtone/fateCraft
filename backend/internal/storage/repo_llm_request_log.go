package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

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
	// Diagnostic writes must never wait for another parent row while holding
	// earlier FK locks: business transactions may need those rows in reverse
	// order. Prelock every FK parent without waiting, rolling back all acquired
	// locks before a bounded retry. Keep the real FK constraints on the insert.
	for attempt := 0; ; attempt++ {
		err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := lockLLMLogParents(tx, row); err != nil {
				return err
			}
			return tx.Create(row).Error
		})
		if err == nil || attempt == 2 || !retryableDiagnosticLock(err) {
			return err
		}
		// The failed transaction has rolled back; do not retain parent locks or
		// a connection while waiting. The async writer also bounds total time.
		timer := time.NewTimer(time.Duration(20<<attempt) * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func lockLLMLogParents(tx *gorm.DB, row *LLMRequestLog) error {
	parents := []struct {
		table string
		id    *uuid.UUID
	}{
		{"conversations", row.ConversationID},
		{"generation_attempts", row.AttemptID},
		{"memory_jobs", row.MemoryJobID},
		{"conversation_memories", row.MemoryID},
	}
	for _, parent := range parents {
		if parent.id == nil {
			continue
		}
		var id uuid.UUID
		// Table names are fixed above, never supplied by a request.
		err := tx.Raw("SELECT id FROM "+parent.table+" WHERE id = ? FOR KEY SHARE NOWAIT", *parent.id).Row().Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("diagnostic parent %s: %w", parent.table, gorm.ErrForeignKeyViolated)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func retryableDiagnosticLock(err error) bool {
	var state interface{ SQLState() string }
	if !errors.As(err, &state) {
		return false
	}
	return state.SQLState() == "55P03" || state.SQLState() == "40P01"
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
