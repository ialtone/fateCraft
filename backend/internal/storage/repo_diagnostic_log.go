package storage

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// DiagnosticLogCleanupResult reports how many large diagnostic rows were
// removed. Share moderation audit events deliberately use a separate policy.
type DiagnosticLogCleanupResult struct {
	LLMRequests   int64
	ContextBuilds int64
}

type DiagnosticLogRepo struct{ db *gorm.DB }

func NewDiagnosticLogRepo(db *gorm.DB) *DiagnosticLogRepo {
	return &DiagnosticLogRepo{db: db}
}

// CleanupExpired deletes a bounded number of old rows per invocation. Each
// statement is its own short transaction, and SKIP LOCKED keeps multiple app
// instances from waiting on the same rows.
func (r *DiagnosticLogRepo) CleanupExpired(ctx context.Context, cutoff time.Time, batchSize, maxBatches int) (DiagnosticLogCleanupResult, error) {
	if batchSize < 1 || maxBatches < 1 {
		return DiagnosticLogCleanupResult{}, fmt.Errorf("诊断日志清理批次参数无效")
	}
	var total DiagnosticLogCleanupResult
	for batch := 0; batch < maxBatches; batch++ {
		llmDeleted, err := r.deleteLLMRequestBatch(ctx, cutoff, batchSize)
		if err != nil {
			return total, err
		}
		contextDeleted, err := r.deleteContextBuildBatch(ctx, cutoff, batchSize)
		if err != nil {
			return total, err
		}
		total.LLMRequests += llmDeleted
		total.ContextBuilds += contextDeleted
		if llmDeleted < int64(batchSize) && contextDeleted < int64(batchSize) {
			break
		}
	}
	return total, nil
}

func (r *DiagnosticLogRepo) deleteLLMRequestBatch(ctx context.Context, cutoff time.Time, batchSize int) (int64, error) {
	result := r.db.WithContext(ctx).Exec(`
		WITH expired AS (
			SELECT id FROM llm_request_logs
			WHERE created_at < ?
			LIMIT ? FOR UPDATE SKIP LOCKED
		)
		DELETE FROM llm_request_logs AS logs
		USING expired
		WHERE logs.id = expired.id`, cutoff, batchSize)
	return result.RowsAffected, result.Error
}

func (r *DiagnosticLogRepo) deleteContextBuildBatch(ctx context.Context, cutoff time.Time, batchSize int) (int64, error) {
	result := r.db.WithContext(ctx).Exec(`
		WITH expired AS (
			SELECT id FROM context_build_logs
			WHERE created_at < ?
			LIMIT ? FOR UPDATE SKIP LOCKED
		)
		DELETE FROM context_build_logs AS logs
		USING expired
		WHERE logs.id = expired.id`, cutoff, batchSize)
	return result.RowsAffected, result.Error
}
