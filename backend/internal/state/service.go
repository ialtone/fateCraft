package state

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"

	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/storage"
)

type stateRepo interface {
	Get(ctx context.Context, convID uuid.UUID) (*storage.ConversationState, error)
	Upsert(ctx context.Context, state *storage.ConversationState) error
	ListRevisions(ctx context.Context, convID uuid.UUID, limit int) ([]storage.StateRevision, error)
	Transaction(ctx context.Context, fn func(tx *storage.StateRepo) error) error
}

type runtimeProvider interface {
	RuntimeForConversation(ctx context.Context, convID uuid.UUID) (*gameplay.RuntimeContract, error)
}

// Service applies the immutable runtime contract bound to each conversation.
// PostgreSQL stores state values, while the challenge version owns their schema.
type Service struct {
	repo     stateRepo
	runtimes runtimeProvider
}

func NewService(repo stateRepo, providers ...runtimeProvider) *Service {
	var runtimes runtimeProvider
	if len(providers) > 0 {
		runtimes = providers[0]
	}
	return &Service{repo: repo, runtimes: runtimes}
}

func (s *Service) contract(ctx context.Context, convID uuid.UUID) (*gameplay.RuntimeContract, error) {
	if s.runtimes == nil {
		return nil, errors.New("状态服务缺少玩法运行时提供者")
	}
	return s.runtimes.RuntimeForConversation(ctx, convID)
}

func (s *Service) EnsureInit(ctx context.Context, convID uuid.UUID) error {
	if _, err := s.repo.Get(ctx, convID); err == nil {
		return nil
	} else if !errors.Is(err, storage.ErrNotFound) {
		return err
	}
	contract, err := s.contract(ctx, convID)
	if err != nil {
		return err
	}
	return s.repo.Upsert(ctx, &storage.ConversationState{
		ConversationID: convID,
		State:          storage.JSONMap(gameplay.InitialState(contract.StateSchema)),
		SchemaVersion:  contract.StateSchema.SchemaVersion,
		UpdatedAt:      time.Now(),
	})
}

// Get returns the trusted full state for internal gameplay services.
func (s *Service) Get(ctx context.Context, convID uuid.UUID) (map[string]any, error) {
	stored, err := s.repo.Get(ctx, convID)
	if errors.Is(err, storage.ErrNotFound) {
		contract, runtimeErr := s.contract(ctx, convID)
		if runtimeErr != nil {
			return nil, runtimeErr
		}
		return gameplay.InitialState(contract.StateSchema), nil
	}
	if err != nil {
		return nil, err
	}
	return stored.State, nil
}

func (s *Service) GetPlayer(ctx context.Context, convID uuid.UUID) (map[string]any, error) {
	trusted, err := s.Get(ctx, convID)
	if err != nil {
		return nil, err
	}
	return s.ProjectPlayer(ctx, convID, trusted)
}

// ProjectPlayer applies the immutable runtime visibility contract to a trusted
// state snapshot. Callers that already fixed a revision can project that exact
// snapshot without rereading a newer conversation_state row.
func (s *Service) ProjectPlayer(ctx context.Context, convID uuid.UUID, trusted map[string]any) (map[string]any, error) {
	contract, err := s.contract(ctx, convID)
	if err != nil {
		return nil, err
	}
	return gameplay.PlayerState(*contract, trusted), nil
}

func (s *Service) Apply(ctx context.Context, convID uuid.UUID, patch map[string]any, source string, relatedMsgID *uuid.UUID) (map[string]any, []string, error) {
	contract, err := s.contract(ctx, convID)
	if err != nil {
		return nil, nil, err
	}
	if err := ValidatePatch(contract.StateSchema, source, patch); err != nil {
		var validationErr *PatchValidationError
		if errors.As(err, &validationErr) {
			return nil, validationErr.Keys, err
		}
		return nil, nil, err
	}
	var after map[string]any
	var skipped []string
	err = s.repo.Transaction(ctx, func(tx *storage.StateRepo) error {
		return applyWithRepo(ctx, tx, convID, contract.StateSchema, patch, source, relatedMsgID, &after, &skipped)
	})
	if err != nil {
		return nil, nil, err
	}
	return after, skipped, nil
}

// GetInTx 在调用方已打开的事务内读取可信状态（带行锁）。schema 由调用方传入，
// 事务内不再回查玩法版本，避免在持锁期间向连接池申请第二个连接。
func GetInTx(ctx context.Context, repo *storage.StateRepo, convID uuid.UUID, schema gameplay.StateSchema) (map[string]any, error) {
	if repo == nil {
		return nil, errors.New("状态事务仓库未配置")
	}
	return getForUpdateOrInitial(ctx, repo, convID, schema)
}

// ApplyInTx 在调用方事务内应用状态补丁。与 GetInTx 同理，schema 由调用方提供，
// 使可信状态、工具审计和 attempt 计数在同一事务内一起提交或回滚。
func ApplyInTx(ctx context.Context, repo *storage.StateRepo, convID uuid.UUID, schema gameplay.StateSchema, patch map[string]any, source string, relatedMsgID *uuid.UUID) (map[string]any, []string, error) {
	if repo == nil {
		return nil, nil, errors.New("状态事务仓库未配置")
	}
	if err := ValidatePatch(schema, source, patch); err != nil {
		var validationErr *PatchValidationError
		if errors.As(err, &validationErr) {
			return nil, validationErr.Keys, err
		}
		return nil, nil, err
	}
	var after map[string]any
	var skipped []string
	err := applyWithRepo(ctx, repo, convID, schema, patch, source, relatedMsgID, &after, &skipped)
	if err != nil {
		return nil, nil, err
	}
	return after, skipped, nil
}

func applyWithRepo(ctx context.Context, tx *storage.StateRepo, convID uuid.UUID, schema gameplay.StateSchema, patch map[string]any, source string, relatedMsgID *uuid.UUID, afterOut *map[string]any, skippedOut *[]string) error {
	before, err := getForUpdateOrInitial(ctx, tx, convID, schema)
	if err != nil {
		return err
	}
	if len(patch) == 0 {
		*afterOut = before
		*skippedOut = nil
		return nil
	}
	after, applied, skipped := ApplyPatch(schema, source, before, patch)
	*afterOut = after
	*skippedOut = skipped
	now := time.Now()
	if err := tx.Upsert(ctx, &storage.ConversationState{
		ConversationID: convID, State: storage.JSONMap(after),
		SchemaVersion: schema.SchemaVersion, UpdatedAt: now,
	}); err != nil {
		return err
	}
	if len(applied) > 0 {
		if err := tx.AddRevision(ctx, &storage.StateRevision{
			ID: uuid.New(), ConversationID: convID, Source: source,
			RelatedMessageID: relatedMsgID, Patch: storage.JSONMap(applied),
			BeforeState: storage.JSONMap(before), AfterState: storage.JSONMap(after), CreatedAt: now,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) Replace(ctx context.Context, convID uuid.UUID, newState map[string]any, source string) (map[string]any, error) {
	contract, err := s.contract(ctx, convID)
	if err != nil {
		return nil, err
	}
	filtered := FilterState(contract.StateSchema, newState)
	err = s.repo.Transaction(ctx, func(tx *storage.StateRepo) error {
		before, err := getForUpdateOrInitial(ctx, tx, convID, contract.StateSchema)
		if err != nil {
			return err
		}
		for _, field := range contract.StateSchema.Fields {
			if field.UpdatePolicy != gameplay.UpdateModule {
				continue
			}
			previous, _ := gameplay.ValueAtPath(before, field.Path)
			next, _ := gameplay.ValueAtPath(filtered, field.Path)
			if !reflect.DeepEqual(previous, next) {
				return &PatchValidationError{Keys: []string{field.Path}}
			}
		}
		now := time.Now()
		if err := tx.Upsert(ctx, &storage.ConversationState{
			ConversationID: convID, State: storage.JSONMap(filtered),
			SchemaVersion: contract.StateSchema.SchemaVersion, UpdatedAt: now,
		}); err != nil {
			return err
		}
		return tx.AddRevision(ctx, &storage.StateRevision{
			ID: uuid.New(), ConversationID: convID, Source: source,
			Patch: storage.JSONMap{"_replace": true}, BeforeState: storage.JSONMap(before),
			AfterState: storage.JSONMap(filtered), CreatedAt: now,
		})
	})
	if err != nil {
		return nil, err
	}
	return filtered, nil
}

func getForUpdateOrInitial(ctx context.Context, tx *storage.StateRepo, convID uuid.UUID, schema gameplay.StateSchema) (map[string]any, error) {
	stored, err := tx.GetForUpdate(ctx, convID)
	if errors.Is(err, storage.ErrNotFound) {
		return gameplay.InitialState(schema), nil
	}
	if err != nil {
		return nil, err
	}
	return stored.State, nil
}

func (s *Service) Revisions(ctx context.Context, convID uuid.UUID, limit int) ([]storage.StateRevision, error) {
	return s.repo.ListRevisions(ctx, convID, limit)
}

// PlayerRevisions returns revision metadata with patch and snapshots projected
// through the same visibility contract as the current player state endpoint.
func (s *Service) PlayerRevisions(ctx context.Context, convID uuid.UUID, limit int) ([]storage.StateRevision, error) {
	contract, err := s.contract(ctx, convID)
	if err != nil {
		return nil, err
	}
	revisions, err := s.repo.ListRevisions(ctx, convID, limit)
	if err != nil {
		return nil, err
	}
	for index := range revisions {
		revisions[index] = projectPlayerRevision(*contract, revisions[index])
	}
	return revisions, nil
}

func projectPlayerRevision(contract gameplay.RuntimeContract, revision storage.StateRevision) storage.StateRevision {
	patch := storage.JSONMap{}
	for key, value := range revision.Patch {
		path := strings.TrimSuffix(key, "+")
		field, ok := gameplay.FieldForPath(contract.StateSchema, path)
		if ok && field.Visibility == gameplay.VisibilityPlayer {
			patch[key] = deepCopyValue(value)
		}
	}
	revision.Patch = patch
	revision.BeforeState = storage.JSONMap(gameplay.PlayerState(contract, revision.BeforeState))
	revision.AfterState = storage.JSONMap(gameplay.PlayerState(contract, revision.AfterState))
	return revision
}
