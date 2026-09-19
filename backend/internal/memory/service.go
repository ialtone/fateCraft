package memory

import (
	"context"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/storage"
	"github.com/ialtone/fatecraft/internal/tokenizer"
)

const (
	DefaultProtectedTurns = 4
	DefaultScheduleTokens = 4000
	TokenizerVersion      = "heuristic-v1"
	ActivityIdle          = "idle"
	ActivityQueued        = "queued"
	ActivityRunning       = "running"
)

type ActivitySink func(conversationID uuid.UUID, status string)

type repository interface {
	Latest(context.Context, uuid.UUID) (*storage.ConversationMemory, error)
	Get(context.Context, uuid.UUID, uuid.UUID) (*storage.ConversationMemory, error)
	List(context.Context, uuid.UUID) ([]storage.ConversationMemory, error)
	PlanningState(context.Context, uuid.UUID, int) (storage.MemoryPlanningState, error)
	Schedule(context.Context, uuid.UUID, storage.MemoryPlanningState) (*storage.MemoryJob, error)
	ListJobs(context.Context, uuid.UUID, int) ([]storage.MemoryJob, error)
}

// Service exposes committed memory snapshots and lightweight scheduling only.
// Model calls are owned by Worker and never happen on the narrative path.
type Service struct {
	repo     repository
	counter  tokenizer.Counter
	wake     func()
	activity ActivitySink
}

func NewService(repo repository, counter tokenizer.Counter) *Service {
	return &Service{repo: repo, counter: counter}
}

func (s *Service) SetWake(wake func()) { s.wake = wake }

func (s *Service) SetActivitySink(sink ActivitySink) { s.activity = sink }

func (s *Service) Latest(ctx context.Context, conversationID uuid.UUID) (*storage.ConversationMemory, error) {
	memory, err := s.repo.Latest(ctx, conversationID)
	if err == storage.ErrNotFound {
		return nil, nil
	}
	return memory, err
}

func (s *Service) Get(ctx context.Context, conversationID, memoryID uuid.UUID) (*storage.ConversationMemory, error) {
	memory, err := s.repo.Get(ctx, conversationID, memoryID)
	if err == storage.ErrNotFound {
		return nil, nil
	}
	return memory, err
}

func (s *Service) List(ctx context.Context, conversationID uuid.UUID) ([]storage.ConversationMemory, error) {
	return s.repo.List(ctx, conversationID)
}

// Schedule performs bounded metadata queries and creates/merges a durable job.
// It returns nil when there is not enough sealed history to summarize.
func (s *Service) Schedule(ctx context.Context, conversationID uuid.UUID, force bool) (*storage.MemoryJob, error) {
	state, err := s.repo.PlanningState(ctx, conversationID, DefaultProtectedTurns)
	if err != nil {
		return nil, err
	}
	if state.TargetUntil <= state.CoveredUntil || state.TargetUntil == 0 {
		return nil, nil
	}
	if !force && state.UncoveredTokens < DefaultScheduleTokens {
		return nil, nil
	}
	job, err := s.repo.Schedule(ctx, conversationID, state)
	if err == nil && job != nil && s.wake != nil {
		s.wake()
	}
	if err == nil && job != nil && s.activity != nil {
		s.activity(conversationID, activityForJob(job.Status))
	}
	return job, err
}

func (s *Service) Jobs(ctx context.Context, conversationID uuid.UUID, limit int) ([]storage.MemoryJob, error) {
	return s.repo.ListJobs(ctx, conversationID, limit)
}

func (s *Service) Count(text string) int { return s.counter.Count(text) }

func activityForJob(status string) string {
	if status == storage.MemoryJobRunning {
		return ActivityRunning
	}
	return ActivityQueued
}
