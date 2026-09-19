package storage

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type MemoryRepo struct{ db *gorm.DB }

func NewMemoryRepo(db *gorm.DB) *MemoryRepo { return &MemoryRepo{db: db} }

func (r *MemoryRepo) Latest(ctx context.Context, conversationID uuid.UUID) (*ConversationMemory, error) {
	var memory ConversationMemory
	err := r.db.WithContext(ctx).Table("conversation_memories AS m").
		Select("m.*").
		Joins("JOIN conversations AS c ON c.current_memory_id = m.id AND c.id = m.conversation_id").
		Where("c.id = ? AND m.invalidated_at IS NULL", conversationID).
		First(&memory).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &memory, err
}

func (r *MemoryRepo) Get(ctx context.Context, conversationID, id uuid.UUID) (*ConversationMemory, error) {
	var memory ConversationMemory
	err := r.db.WithContext(ctx).
		First(&memory, "id = ? AND conversation_id = ? AND invalidated_at IS NULL", id, conversationID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &memory, err
}

func (r *MemoryRepo) List(ctx context.Context, conversationID uuid.UUID) ([]ConversationMemory, error) {
	var memories []ConversationMemory
	err := r.db.WithContext(ctx).Where("conversation_id = ?", conversationID).
		Order("version DESC").Find(&memories).Error
	return memories, err
}

type MemoryPlanningState struct {
	BranchEpoch     int64
	CurrentMemoryID *uuid.UUID
	CoveredUntil    int64
	TargetUntil     int64
	UncoveredTokens int
}

const memoryPlanningChunkTokens = 4500

type MemorySource struct {
	Conversation Conversation
	Previous     *ConversationMemory
	Turns        []ConversationTurn
	Messages     []ChatMessage
	Revisions    []StateRevision
	Rolls        []RollHistoryEntry
	Events       []CampaignEvent
}

func (r *MemoryRepo) LoadSource(ctx context.Context, job MemoryJob) (*MemorySource, error) {
	var source MemorySource
	if err := r.db.WithContext(ctx).First(&source.Conversation, "id = ?", job.ConversationID).Error; err != nil {
		return nil, err
	}
	coveredUntil := int64(0)
	if job.PreviousMemoryID != nil {
		previous, err := r.Get(ctx, job.ConversationID, *job.PreviousMemoryID)
		if err != nil {
			return nil, err
		}
		source.Previous = previous
		coveredUntil = previous.CoveredUntilTurnSeq
	}
	turnQuery := r.db.WithContext(ctx).
		Where("conversation_id = ? AND status = ? AND invalidated_at IS NULL AND turn_seq > ? AND turn_seq <= ?",
			job.ConversationID, TurnStatusCompleted, coveredUntil, job.TargetUntilTurnSeq).
		Order("turn_seq ASC")
	if err := turnQuery.Find(&source.Turns).Error; err != nil {
		return nil, err
	}
	if len(source.Turns) == 0 {
		return nil, ErrNotFound
	}
	turnIDs := make([]uuid.UUID, 0, len(source.Turns))
	for _, turn := range source.Turns {
		turnIDs = append(turnIDs, turn.ID)
	}
	if err := r.db.WithContext(ctx).Where("conversation_id = ? AND turn_id IN ? AND invalidated_at IS NULL", job.ConversationID, turnIDs).
		Order("message_seq ASC").Find(&source.Messages).Error; err != nil {
		return nil, err
	}
	if err := r.db.WithContext(ctx).Where("conversation_id = ? AND turn_id IN ? AND invalidated_at IS NULL", job.ConversationID, turnIDs).
		Order("created_at ASC").Find(&source.Revisions).Error; err != nil {
		return nil, err
	}
	if err := r.db.WithContext(ctx).Where("conversation_id = ? AND turn_id IN ? AND invalidated_at IS NULL", job.ConversationID.String(), turnIDs).
		Order("created_at ASC").Find(&source.Rolls).Error; err != nil {
		return nil, err
	}
	if source.Conversation.CampaignID != nil {
		if err := r.db.WithContext(ctx).
			Where("campaign_id = ? AND turn_id IN ? AND visibility IN ?", *source.Conversation.CampaignID, turnIDs,
				[]string{CampaignVisibilityPlayer, CampaignVisibilityModel}).
			Order("sequence ASC").Find(&source.Events).Error; err != nil {
			return nil, err
		}
	}
	return &source, nil
}

func (r *MemoryRepo) PlanningState(ctx context.Context, conversationID uuid.UUID, protectedTurns int) (MemoryPlanningState, error) {
	var state MemoryPlanningState
	var conv Conversation
	if err := r.db.WithContext(ctx).First(&conv, "id = ?", conversationID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return state, ErrNotFound
		}
		return state, err
	}
	state.BranchEpoch = conv.BranchEpoch
	state.CurrentMemoryID = conv.CurrentMemoryID
	if conv.CurrentMemoryID != nil {
		memory, err := r.Get(ctx, conversationID, *conv.CurrentMemoryID)
		if err != nil {
			return state, err
		}
		state.CoveredUntil = memory.CoveredUntilTurnSeq
	}

	var protected []int64
	query := r.db.WithContext(ctx).Model(&ConversationTurn{}).
		Where("conversation_id = ? AND status = ? AND invalidated_at IS NULL AND turn_seq > ?", conversationID, TurnStatusCompleted, state.CoveredUntil).
		Order("turn_seq DESC")
	if protectedTurns > 0 {
		if err := query.Limit(protectedTurns).Pluck("turn_seq", &protected).Error; err != nil {
			return state, err
		}
	}
	if len(protected) > 0 {
		state.TargetUntil = protected[len(protected)-1] - 1
	} else {
		var maxSeq *int64
		if err := query.Select("MAX(turn_seq)").Scan(&maxSeq).Error; err != nil {
			return state, err
		}
		if maxSeq != nil {
			state.TargetUntil = *maxSeq
		}
	}
	if state.TargetUntil <= state.CoveredUntil {
		state.TargetUntil = 0
		return state, nil
	}
	type turnTokens struct {
		TurnSeq int64
		Tokens  int64
	}
	var rows []turnTokens
	err := r.db.WithContext(ctx).Model(&ChatMessage{}).
		Joins("JOIN conversation_turns t ON t.id = chat_messages.turn_id").
		Where("chat_messages.conversation_id = ? AND chat_messages.invalidated_at IS NULL AND t.invalidated_at IS NULL AND t.status = ? AND t.turn_seq > ? AND t.turn_seq <= ?",
			conversationID, TurnStatusCompleted, state.CoveredUntil, state.TargetUntil).
		Select("t.turn_seq, COALESCE(SUM(COALESCE(chat_messages.token_count, 0)), 0) AS tokens").
		Group("t.turn_seq").Order("t.turn_seq ASC").Scan(&rows).Error
	if err != nil {
		return state, err
	}
	chunkTokens := int64(0)
	for _, row := range rows {
		state.UncoveredTokens += int(row.Tokens)
		if chunkTokens > 0 && chunkTokens+row.Tokens > memoryPlanningChunkTokens {
			break
		}
		chunkTokens += row.Tokens
		state.TargetUntil = row.TurnSeq
	}
	return state, nil
}

func (r *MemoryRepo) Schedule(ctx context.Context, conversationID uuid.UUID, state MemoryPlanningState) (*MemoryJob, error) {
	var job MemoryJob
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing MemoryJob
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("conversation_id = ? AND status IN ?", conversationID, []string{MemoryJobPending, MemoryJobRunning}).
			First(&existing).Error
		switch {
		case err == nil:
			if existing.Status == MemoryJobPending && state.TargetUntil > existing.TargetUntilTurnSeq {
				existing.TargetUntilTurnSeq = state.TargetUntil
				existing.SourceHash = ""
				existing.GenerationFingerprint = ""
				existing.UpdatedAt = time.Now()
				if err := tx.Save(&existing).Error; err != nil {
					return err
				}
			}
			job = existing
			return nil
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return err
		}
		now := time.Now()
		job = MemoryJob{
			ID: uuid.New(), ConversationID: conversationID, PreviousMemoryID: state.CurrentMemoryID,
			SourceBranchEpoch: state.BranchEpoch, TargetUntilTurnSeq: state.TargetUntil,
			Status: MemoryJobPending, MaxExecutions: 4, AvailableAt: now, CreatedAt: now, UpdatedAt: now,
		}
		return tx.Create(&job).Error
	})
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *MemoryRepo) GetJob(ctx context.Context, id uuid.UUID) (*MemoryJob, error) {
	var job MemoryJob
	err := r.db.WithContext(ctx).First(&job, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &job, err
}

func (r *MemoryRepo) ListJobs(ctx context.Context, conversationID uuid.UUID, limit int) ([]MemoryJob, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var jobs []MemoryJob
	err := r.db.WithContext(ctx).Where("conversation_id = ?", conversationID).
		Order("created_at DESC, id DESC").Limit(limit).Find(&jobs).Error
	return jobs, err
}

func (r *MemoryRepo) Claim(ctx context.Context, workerID string, lease time.Duration) (*MemoryJob, error) {
	var job MemoryJob
	claimed := false
	now := time.Now()
	leaseToken := uuid.New()
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Raw(`
			SELECT * FROM memory_jobs
			WHERE (status = 'pending' AND available_at <= ?)
			   OR (status = 'running' AND lease_expires_at < ?)
			ORDER BY available_at, created_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1`, now, now).Scan(&job).Error
		if err != nil {
			return err
		}
		if job.ID == uuid.Nil {
			return ErrNotFound
		}
		if job.ExecutionCount >= job.MaxExecutions {
			if err := tx.Model(&MemoryJob{}).Where("id = ?", job.ID).Updates(map[string]any{
				"status": MemoryJobFailed, "last_error_code": "max_executions", "finished_at": now, "updated_at": now,
			}).Error; err != nil {
				return err
			}
			return ErrNotFound
		}
		if job.Status == MemoryJobRunning {
			job.LastErrorCode = "lease_expired"
			job.LastErrorMessage = "上一执行者租约已过期，任务已被重新领取"
		}
		job.Status = MemoryJobRunning
		job.ExecutionCount++
		job.WorkerID = workerID
		job.LeaseToken = &leaseToken
		expires := now.Add(lease)
		job.LeaseExpiresAt = &expires
		if job.StartedAt == nil {
			job.StartedAt = &now
		}
		job.UpdatedAt = now
		if err := tx.Save(&job).Error; err != nil {
			return err
		}
		claimed = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	if !claimed {
		return nil, ErrNotFound
	}
	return &job, nil
}

func (r *MemoryRepo) SetJobSource(ctx context.Context, jobID uuid.UUID, workerID string, leaseToken uuid.UUID, sourceHash, fingerprint string) error {
	result := r.db.WithContext(ctx).Model(&MemoryJob{}).
		Where("id = ? AND status = ? AND worker_id = ? AND lease_token = ?", jobID, MemoryJobRunning, workerID, leaseToken).
		Updates(map[string]any{"source_hash": sourceHash, "generation_fingerprint": fingerprint, "updated_at": time.Now()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrNotFound
	}
	return nil
}

func (r *MemoryRepo) RenewLease(ctx context.Context, jobID uuid.UUID, workerID string, leaseToken uuid.UUID, lease time.Duration) (bool, error) {
	result := r.db.WithContext(ctx).Model(&MemoryJob{}).
		Where("id = ? AND status = ? AND worker_id = ? AND lease_token = ?", jobID, MemoryJobRunning, workerID, leaseToken).
		Updates(map[string]any{"lease_expires_at": time.Now().Add(lease), "updated_at": time.Now()})
	return result.RowsAffected == 1, result.Error
}

type MemoryCommitInput struct {
	JobID       uuid.UUID
	WorkerID    string
	LeaseToken  uuid.UUID
	Memory      ConversationMemory
	TurnIDs     []uuid.UUID
	MessageIDs  []uuid.UUID
	RevisionIDs []uuid.UUID
	RollIDs     []string
	EventIDs    []uuid.UUID
}

func (r *MemoryRepo) Commit(ctx context.Context, input MemoryCommitInput) error {
	now := time.Now()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job MemoryJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&job, "id = ?", input.JobID).Error; err != nil {
			return err
		}
		if job.Status != MemoryJobRunning || job.WorkerID != input.WorkerID || job.LeaseToken == nil || *job.LeaseToken != input.LeaseToken {
			return errors.New("memory job 租约已失效")
		}
		var conv Conversation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&conv, "id = ?", job.ConversationID).Error; err != nil {
			return err
		}
		if conv.BranchEpoch != job.SourceBranchEpoch || !sameUUID(conv.CurrentMemoryID, job.PreviousMemoryID) {
			return errors.New("memory job 来源分支或链头已变化")
		}
		if input.Memory.SourceHash == "" || input.Memory.SourceHash != job.SourceHash ||
			input.Memory.GenerationFingerprint == "" || input.Memory.GenerationFingerprint != job.GenerationFingerprint ||
			input.Memory.SourceBranchEpoch != job.SourceBranchEpoch ||
			input.Memory.CoveredUntilTurnSeq != job.TargetUntilTurnSeq {
			return errors.New("memory job 候选与固定来源不一致")
		}
		if err := ensureActiveSource(tx, job.ConversationID, input); err != nil {
			return err
		}
		input.Memory.Version = 1
		if job.PreviousMemoryID != nil {
			var previous ConversationMemory
			if err := tx.First(&previous, "id = ? AND conversation_id = ? AND invalidated_at IS NULL", *job.PreviousMemoryID, job.ConversationID).Error; err != nil {
				return err
			}
			input.Memory.Version = previous.Version + 1
		}
		if err := tx.Create(&input.Memory).Error; err != nil {
			return err
		}
		if err := tx.Model(&Conversation{}).Where("id = ? AND branch_epoch = ?", conv.ID, conv.BranchEpoch).
			Update("current_memory_id", input.Memory.ID).Error; err != nil {
			return err
		}
		return tx.Model(&MemoryJob{}).Where("id = ?", job.ID).Updates(map[string]any{
			"status": MemoryJobSucceeded, "result_memory_id": input.Memory.ID,
			"source_hash": input.Memory.SourceHash, "generation_fingerprint": input.Memory.GenerationFingerprint,
			"lease_token": nil, "lease_expires_at": nil, "finished_at": now, "updated_at": now,
		}).Error
	})
}

func ensureActiveSource(tx *gorm.DB, conversationID uuid.UUID, input MemoryCommitInput) error {
	checks := []struct {
		model any
		ids   any
		where string
		want  int
	}{
		{&ConversationTurn{}, input.TurnIDs, "conversation_id = ? AND id IN ? AND status = 'completed' AND invalidated_at IS NULL", len(input.TurnIDs)},
		{&ChatMessage{}, input.MessageIDs, "conversation_id = ? AND id IN ? AND invalidated_at IS NULL", len(input.MessageIDs)},
		{&StateRevision{}, input.RevisionIDs, "conversation_id = ? AND id IN ? AND invalidated_at IS NULL", len(input.RevisionIDs)},
		{&RollHistoryEntry{}, input.RollIDs, "conversation_id = ? AND id IN ? AND invalidated_at IS NULL", len(input.RollIDs)},
	}
	for _, check := range checks {
		if check.want == 0 {
			continue
		}
		var count int64
		if err := tx.Model(check.model).Where(check.where, conversationID, check.ids).Count(&count).Error; err != nil {
			return err
		}
		if count != int64(check.want) {
			return errors.New("memory job 来源在生成期间发生变化")
		}
	}
	if len(input.EventIDs) > 0 {
		var count int64
		if err := tx.Model(&CampaignEvent{}).
			Where("id IN ? AND visibility IN ?", input.EventIDs, []string{CampaignVisibilityPlayer, CampaignVisibilityModel}).
			Count(&count).Error; err != nil {
			return err
		}
		if count != int64(len(input.EventIDs)) {
			return errors.New("memory job Campaign 来源在生成期间发生变化")
		}
	}
	return nil
}

func (r *MemoryRepo) FailJob(ctx context.Context, jobID uuid.UUID, workerID string, leaseToken uuid.UUID, code, message string, retryAt *time.Time) error {
	updates := map[string]any{
		"last_error_code": code, "last_error_message": boundedError(message),
		"worker_id": "", "lease_token": nil, "lease_expires_at": nil, "updated_at": time.Now(),
	}
	if retryAt != nil {
		updates["status"] = MemoryJobPending
		updates["available_at"] = *retryAt
	} else {
		updates["status"] = MemoryJobFailed
		updates["finished_at"] = time.Now()
	}
	result := r.db.WithContext(ctx).Model(&MemoryJob{}).
		Where("id = ? AND status = ? AND worker_id = ? AND lease_token = ?", jobID, MemoryJobRunning, workerID, leaseToken).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrNotFound
	}
	return nil
}

func (r *MemoryRepo) CancelActive(ctx context.Context, conversationID uuid.UUID, reason string) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&MemoryJob{}).
		Where("conversation_id = ? AND status IN ?", conversationID, []string{MemoryJobPending, MemoryJobRunning}).
		Updates(map[string]any{
			"status": MemoryJobCancelled, "last_error_code": reason, "worker_id": "",
			"lease_token": nil, "lease_expires_at": nil, "finished_at": now, "updated_at": now,
		}).Error
}

func sameUUID(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func boundedError(value string) string {
	const max = 1024
	if len(value) > max {
		return value[:max]
	}
	return value
}
