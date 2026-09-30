package storage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ialtone/fatecraft/internal/logging"
	"gorm.io/gorm"
)

// DiagnosticLogCleanupResult reports how many large diagnostic rows were
// removed. Share moderation audit events deliberately use a separate policy.
type DiagnosticLogCleanupResult struct {
	HasMore       bool  `json:"has_more"`
	BacklogKnown  bool  `json:"backlog_known"`
	LimitReached  bool  `json:"limit_reached"`
	LLMRequests   int64 `json:"llm_requests"`
	ContextBuilds int64 `json:"context_builds"`
}

type DiagnosticCleanupStatus struct {
	LastRun    *time.Time `json:"last_run,omitempty"`
	DurationMS int64      `json:"duration_ms"`
	Error      string     `json:"error,omitempty"`
	DiagnosticLogCleanupResult
}
type DiagnosticLogRepo struct {
	db     *gorm.DB
	mu     sync.Mutex
	status DiagnosticCleanupStatus
}

func (r *DiagnosticLogRepo) Status() DiagnosticCleanupStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := r.status
	if result.LastRun != nil {
		value := *result.LastRun
		result.LastRun = &value
	}
	return result
}

func NewDiagnosticLogRepo(db *gorm.DB) *DiagnosticLogRepo {
	return &DiagnosticLogRepo{db: db}
}

// CleanupExpired deletes a bounded number of old rows per invocation. Each
// statement is its own short transaction, and SKIP LOCKED keeps multiple app
// instances from waiting on the same rows.
func (r *DiagnosticLogRepo) CleanupExpired(ctx context.Context, cutoff time.Time, batchSize, maxBatches int) (total DiagnosticLogCleanupResult, cleanupErr error) {
	started := time.Now()
	defer func() {
		if errors.Is(cleanupErr, context.DeadlineExceeded) {
			total.LimitReached = true
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		r.status = DiagnosticCleanupStatus{LastRun: &started, DurationMS: time.Since(started).Milliseconds(), Error: logging.ErrorText(cleanupErr), DiagnosticLogCleanupResult: total}
	}()
	if batchSize < 1 || maxBatches < 1 {
		return DiagnosticLogCleanupResult{}, fmt.Errorf("诊断日志清理批次参数无效")
	}
	for batch := 0; batch < maxBatches; batch++ {
		llmDeleted, err := r.deleteLLMRequestBatch(ctx, cutoff, batchSize)
		if err != nil {
			return total, err
		}
		total.LLMRequests += llmDeleted
		contextDeleted, err := r.deleteContextBuildBatch(ctx, cutoff, batchSize)
		if err != nil {
			return total, err
		}
		total.ContextBuilds += contextDeleted
		if llmDeleted < int64(batchSize) && contextDeleted < int64(batchSize) {
			break
		}
	}
	total.LimitReached = total.LLMRequests >= int64(batchSize*maxBatches) || total.ContextBuilds >= int64(batchSize*maxBatches)
	err := r.db.WithContext(ctx).Raw(`SELECT EXISTS (SELECT 1 FROM llm_request_logs WHERE created_at < ?) OR EXISTS (SELECT 1 FROM context_build_logs WHERE created_at < ?)`, cutoff, cutoff).Scan(&total.HasMore).Error
	total.BacklogKnown = err == nil
	return total, err
}

func (r *DiagnosticLogRepo) deleteLLMRequestBatch(ctx context.Context, cutoff time.Time, batchSize int) (int64, error) {
	result := r.db.WithContext(ctx).Exec(`
		WITH expired AS (
			SELECT id FROM llm_request_logs
			WHERE created_at < ?
			ORDER BY created_at, id LIMIT ? FOR UPDATE SKIP LOCKED
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
			ORDER BY created_at, id LIMIT ? FOR UPDATE SKIP LOCKED
		)
		DELETE FROM context_build_logs AS logs
		USING expired
		WHERE logs.id = expired.id`, cutoff, batchSize)
	return result.RowsAffected, result.Error
}
