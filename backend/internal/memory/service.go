package memory

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/storage"
	"github.com/ialtone/fatecraft/internal/tokenizer"
)

// memoryRepo 是本服务消费端的摘要仓储小接口（只声明实际用到的方法）。
// storage.MemoryRepo 天然满足该接口，便于用 mock 脱离 Postgres 单测。
type memoryRepo interface {
	Create(ctx context.Context, s *storage.MemorySummary) error
	Latest(ctx context.Context, convID uuid.UUID) (*storage.MemorySummary, error)
	MaxVersion(ctx context.Context, convID uuid.UUID) (int, error)
	List(ctx context.Context, convID uuid.UUID) ([]storage.MemorySummary, error)
	Get(ctx context.Context, convID, id uuid.UUID) (*storage.MemorySummary, error)
	UpdateContent(ctx context.Context, convID, id uuid.UUID, content string, tokenCount int) error
}

// Service 负责摘要记忆的版本管理（严格按 conversation_id 隔离）。
type Service struct {
	repo    memoryRepo
	counter tokenizer.Counter
}

func NewService(repo memoryRepo, counter tokenizer.Counter) *Service {
	return &Service{repo: repo, counter: counter}
}

// Latest 返回最新摘要；无摘要时返回 (nil, nil)。
func (s *Service) Latest(ctx context.Context, convID uuid.UUID) (*storage.MemorySummary, error) {
	sum, err := s.repo.Latest(ctx, convID)
	if err == storage.ErrNotFound {
		return nil, nil
	}
	return sum, err
}

// List 返回所有摘要版本（版本倒序）。
func (s *Service) List(ctx context.Context, convID uuid.UUID) ([]storage.MemorySummary, error) {
	return s.repo.List(ctx, convID)
}

func (s *Service) Get(ctx context.Context, convID, id uuid.UUID) (*storage.MemorySummary, error) {
	return s.repo.Get(ctx, convID, id)
}

// CreateVersion 在当前对话上追加一个新摘要版本。
func (s *Service) CreateVersion(ctx context.Context, convID uuid.UUID, content string, coveredUntil *uuid.UUID) (*storage.MemorySummary, error) {
	maxV, err := s.repo.MaxVersion(ctx, convID)
	if err != nil {
		return nil, err
	}
	tokens := s.counter.Count(content)
	sum := &storage.MemorySummary{
		ID:                    uuid.New(),
		ConversationID:        convID,
		Version:               maxV + 1,
		Content:               content,
		CoveredUntilMessageID: coveredUntil,
		TokenCount:            &tokens,
		CreatedAt:             time.Now(),
	}
	if err := s.repo.Create(ctx, sum); err != nil {
		return nil, err
	}
	return sum, nil
}

// Edit 编辑某个摘要版本的正文。
func (s *Service) Edit(ctx context.Context, convID, id uuid.UUID, content string) (*storage.MemorySummary, error) {
	if err := s.repo.UpdateContent(ctx, convID, id, content, s.counter.Count(content)); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, convID, id)
}
