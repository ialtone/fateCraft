package state

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/storage"
)

// stateRepo 是本服务消费端的状态仓储小接口（只声明实际用到的方法）。
// storage.StateRepo 天然满足该接口，便于用 mock 脱离 Postgres 单测。
// 注意 Transaction 的回调仍收具体 *storage.StateRepo：事务内的
// GetForUpdate/Upsert/AddRevision 直接在该具体连接上调用（Go 函数类型不变，
// 无法把回调参数也抽象成接口，否则具体 repo 反而不满足本接口）。
type stateRepo interface {
	Get(ctx context.Context, convID uuid.UUID) (*storage.ConversationState, error)
	Upsert(ctx context.Context, s *storage.ConversationState) error
	ListRevisions(ctx context.Context, convID uuid.UUID, limit int) ([]storage.StateRevision, error)
	Transaction(ctx context.Context, fn func(tx *storage.StateRepo) error) error
}

// Service 负责对话结构化状态的读取、补丁应用与修订记录。
type Service struct {
	repo stateRepo
}

func NewService(repo stateRepo) *Service { return &Service{repo: repo} }

// EnsureInit 在对话创建时初始化默认状态（已存在则跳过）。
func (s *Service) EnsureInit(ctx context.Context, convID uuid.UUID) error {
	if _, err := s.repo.Get(ctx, convID); err == nil {
		return nil
	} else if err != storage.ErrNotFound {
		return err
	}
	return s.repo.Upsert(ctx, &storage.ConversationState{
		ConversationID: convID,
		State:          storage.JSONMap(DefaultState()),
		SchemaVersion:  1,
		UpdatedAt:      time.Now(),
	})
}

// Get 返回对话状态；不存在时返回默认状态（不落库）。
func (s *Service) Get(ctx context.Context, convID uuid.UUID) (map[string]any, error) {
	cs, err := s.repo.Get(ctx, convID)
	if err == storage.ErrNotFound {
		return DefaultState(), nil
	}
	if err != nil {
		return nil, err
	}
	return cs.State, nil
}

// Apply 校验并应用一个 state_update 补丁，落库并记录修订。
// 在事务中带行锁读改写，避免用户 PATCH 与 AI 生成并发时互相覆盖。
// 返回应用后的新状态与被跳过的非法路径。
func (s *Service) Apply(ctx context.Context, convID uuid.UUID, patch map[string]any, source string, relatedMsgID *uuid.UUID) (map[string]any, []string, error) {
	var after map[string]any
	var skipped []string

	err := s.repo.Transaction(ctx, func(tx *storage.StateRepo) error {
		before, err := getForUpdateOrDefault(ctx, tx, convID)
		if err != nil {
			return err
		}
		if len(patch) == 0 {
			after = before
			return nil
		}

		var applied map[string]any
		after, applied, skipped = ApplyPatch(before, patch)
		now := time.Now()

		if err := tx.Upsert(ctx, &storage.ConversationState{
			ConversationID: convID,
			State:          storage.JSONMap(after),
			SchemaVersion:  1,
			UpdatedAt:      now,
		}); err != nil {
			return err
		}

		if len(applied) > 0 {
			_ = tx.AddRevision(ctx, &storage.StateRevision{
				ID:               uuid.New(),
				ConversationID:   convID,
				Source:           source,
				RelatedMessageID: relatedMsgID,
				Patch:            storage.JSONMap(applied),
				BeforeState:      storage.JSONMap(before),
				AfterState:       storage.JSONMap(after),
				CreatedAt:        now,
			})
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return after, skipped, nil
}

// Replace 用整份状态覆盖当前状态（用户手动编辑），并记录修订。
// 非法顶层键会被过滤掉，与 Apply 走同一套白名单校验。
func (s *Service) Replace(ctx context.Context, convID uuid.UUID, newState map[string]any, source string) (map[string]any, error) {
	// 过滤不在白名单内的顶层键。
	filtered := map[string]any{}
	for k, v := range newState {
		if _, ok := allowedTopLevels[k]; ok {
			filtered[k] = v
		}
	}

	err := s.repo.Transaction(ctx, func(tx *storage.StateRepo) error {
		before, err := getForUpdateOrDefault(ctx, tx, convID)
		if err != nil {
			return err
		}
		now := time.Now()
		if err := tx.Upsert(ctx, &storage.ConversationState{
			ConversationID: convID,
			State:          storage.JSONMap(filtered),
			SchemaVersion:  1,
			UpdatedAt:      now,
		}); err != nil {
			return err
		}
		_ = tx.AddRevision(ctx, &storage.StateRevision{
			ID:             uuid.New(),
			ConversationID: convID,
			Source:         source,
			Patch:          storage.JSONMap{"_replace": true},
			BeforeState:    storage.JSONMap(before),
			AfterState:     storage.JSONMap(filtered),
			CreatedAt:      now,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return filtered, nil
}

// getForUpdateOrDefault 在事务内带行锁读取状态；不存在时返回默认状态。
func getForUpdateOrDefault(ctx context.Context, tx *storage.StateRepo, convID uuid.UUID) (map[string]any, error) {
	cs, err := tx.GetForUpdate(ctx, convID)
	if err == storage.ErrNotFound {
		return DefaultState(), nil
	}
	if err != nil {
		return nil, err
	}
	return cs.State, nil
}

// Revisions 返回状态修订历史。
func (s *Service) Revisions(ctx context.Context, convID uuid.UUID, limit int) ([]storage.StateRevision, error) {
	return s.repo.ListRevisions(ctx, convID, limit)
}
