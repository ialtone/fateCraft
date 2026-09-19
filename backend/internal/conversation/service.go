package conversation

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/ialtone/fatecraft/internal/challenge"
	"github.com/ialtone/fatecraft/internal/gameplay"
	"github.com/ialtone/fatecraft/internal/identity"
	"github.com/ialtone/fatecraft/internal/logging"
	"github.com/ialtone/fatecraft/internal/operation"
	"github.com/ialtone/fatecraft/internal/state"
	"github.com/ialtone/fatecraft/internal/storage"
)

// ErrNotActive 表示对话不处于 active 状态，无法继续生成或编辑。
var ErrNotActive = errors.New("对话未处于 active 状态")

// ErrCampaignRequired 表示该玩法必须先创建长期存档，再从存档创建持续对话。
var ErrCampaignRequired = errors.New("该玩法必须先创建长期存档")

// ErrForkCampaign 表示长期存档对话不能走普通对话的版本分叉流程。
// Campaign 有独立的领域存档和动作提交协议，必须通过 Campaign 入口处理。
var ErrForkCampaign = errors.New("长期存档对话不能通过普通对话分叉")

// ErrGenerationInProgress indicates that another durable generation run still
// owns the conversation. Callers should wait or queue the request rather than
// starting a second writer.
var ErrGenerationInProgress = storage.ErrGenerationInProgress

// conversationRepo 是本服务消费端的对话仓储小接口（只声明实际用到的方法）。
// storage.ConversationRepo 天然满足该接口，便于用 mock 脱离 Postgres 单测。
type conversationRepo interface {
	Create(ctx context.Context, c *storage.Conversation) error
	CreateWithState(ctx context.Context, c *storage.Conversation, state *storage.ConversationState) error
	Get(ctx context.Context, id uuid.UUID) (*storage.Conversation, error)
	List(ctx context.Context, playerID string, statuses []string, search string) ([]storage.Conversation, error)
	ListPage(ctx context.Context, playerID string, statuses []string, search string, limit int) ([]storage.Conversation, error)
	UpdateFields(ctx context.Context, id uuid.UUID, fields map[string]any) error
	StartGeneration(ctx context.Context, id uuid.UUID, request storage.GenerationRequest) (*storage.Conversation, *storage.ConversationGenerationRun, bool, error)
	FinishGeneration(ctx context.Context, runID uuid.UUID, status, errorCode string) error
	HeartbeatGeneration(ctx context.Context, runID uuid.UUID) error
	FailActiveGeneration(ctx context.Context, id uuid.UUID, errorCode string) error
	CancelGeneration(ctx context.Context, id uuid.UUID, reason string) error
	TransitionLifecycle(ctx context.Context, id uuid.UUID, status string, fields map[string]any) error
}

// messageSeeder 是本服务写入开场/触发消息所需的消息仓储小接口。
// storage.MessageRepo 天然满足该接口。
type messageSeeder interface {
	CreateChat(ctx context.Context, m *storage.ChatMessage) error
	CreateDisplay(ctx context.Context, m *storage.DisplayMessage) error
}

// Service 负责对话的生命周期管理。
type Service struct {
	repo      conversationRepo
	stateSvc  *state.Service
	msgRepo   messageSeeder
	challenge *challenge.Service
	identity  identity.Provider
	turns     *storage.TurnRepo
}

func (s *Service) SetTurnRepo(turns *storage.TurnRepo) { s.turns = turns }

func NewService(repo conversationRepo, stateSvc *state.Service, msgRepo messageSeeder, challengeSvc *challenge.Service, ident identity.Provider) *Service {
	return &Service{repo: repo, stateSvc: stateSvc, msgRepo: msgRepo, challenge: challengeSvc, identity: ident}
}

// CreateInput 只暴露挑战和该挑战允许的玩家 Persona。
type CreateInput struct {
	Title         string `json:"title"`
	ChallengeSlug string `json:"challenge_slug"`
	PersonaSlug   string `json:"persona_slug"`
}

// Create 新建对话并固定已发布挑战版本及其内容哈希。
func (s *Service) Create(ctx context.Context, in CreateInput) (*storage.Conversation, error) {
	playerID, err := uuid.Parse(s.identity.CurrentPlayerID(ctx))
	if err != nil {
		return nil, storage.ErrNotFound
	}
	selection, err := s.challenge.ResolveSelection(ctx, in.ChallengeSlug, in.PersonaSlug, playerID)
	if err != nil {
		return nil, err
	}
	if gameplay.NormalizeRuntimeMode(selection.Contract.Mode) == gameplay.RuntimeModeCampaign {
		return nil, ErrCampaignRequired
	}
	now := time.Now()
	title := in.Title
	if title == "" {
		title = "新的对话"
	}
	conv := &storage.Conversation{
		ID:                 uuid.New(),
		PlayerID:           s.identity.CurrentPlayerID(ctx),
		Title:              title,
		Status:             storage.StatusActive,
		ChallengeVersionID: selection.Version.ID,
		ChallengeHash:      selection.Version.ContentHash,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if selection.Character != nil {
		conv.CharacterAssetID = &selection.Character.ID
	}
	if selection.Persona != nil {
		conv.PersonaAssetID = &selection.Persona.ID
	}
	if selection.Scenario != nil {
		conv.ScenarioAssetID = &selection.Scenario.ID
	}
	initialState := &storage.ConversationState{
		ConversationID: conv.ID,
		State:          storage.JSONMap(gameplay.InitialState(selection.Contract.StateSchema)),
		SchemaVersion:  selection.Contract.StateSchema.SchemaVersion,
		UpdatedAt:      now,
	}
	if err := s.repo.CreateWithState(ctx, conv, initialState); err != nil {
		return nil, err
	}
	s.SeedOpening(ctx, conv, now)
	return conv, nil
}

// Fork creates a fresh conversation on the pack's current version using the
// source conversation's persona when it still exists. Historical messages and
// state remain untouched; this deliberately starts from the new version's
// validated initial state instead of attempting unsafe arbitrary state
// migration. If a persona was removed in the new version, Create falls back
// to that version's recommended persona.
func (s *Service) Fork(ctx context.Context, sourceID uuid.UUID, title string) (*storage.Conversation, error) {
	source, err := s.GetOwned(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	if source.Status == storage.StatusDeleted {
		return nil, ErrNotActive
	}
	if source.CampaignID != nil {
		return nil, ErrForkCampaign
	}
	slug, err := s.challenge.SlugForVersion(ctx, source.ChallengeVersionID)
	if err != nil {
		return nil, err
	}
	personaSlug := ""
	if source.PersonaAssetID != nil {
		if asset, assetErr := s.challenge.Asset(ctx, *source.PersonaAssetID); assetErr == nil && asset != nil {
			personaSlug = asset.Slug
		}
	}
	if title == "" {
		title = source.Title + "（新版本）"
	}
	if titleRunes := []rune(title); len(titleRunes) > 256 {
		title = string(titleRunes[:256])
	}
	created, err := s.Create(ctx, CreateInput{Title: title, ChallengeSlug: slug, PersonaSlug: personaSlug})
	if err == nil || !errors.Is(err, challenge.ErrInvalidSelection) || personaSlug == "" {
		return created, err
	}
	// The old persona may have been intentionally removed or renamed. Let the
	// current version choose its recommended persona rather than blocking the
	// player's upgrade path.
	return s.Create(ctx, CreateInput{Title: title, ChallengeSlug: slug})
}

// SeedOpening 为已经原子创建完成的 Conversation 写入非关键开场消息。
// 消息失败只记录日志，不会让调用方误以为对话或初始状态没有创建成功。
func (s *Service) SeedOpening(ctx context.Context, conv *storage.Conversation, now time.Time) {
	if conv == nil || conv.CharacterAssetID == nil {
		return
	}
	bundle, err := s.challenge.Bundle(ctx, conv)
	if err != nil || bundle.Character == nil || bundle.Character.FirstMessage == "" {
		return
	}
	var turnID, attemptID uuid.UUID
	if s.turns != nil {
		turn, attempt, beginErr := s.turns.Begin(ctx, conv.ID, "opening")
		if beginErr != nil {
			logging.Named("conversation").Warn("创建开场回合失败", zap.String("conversation_id", conv.ID.String()), zap.Error(beginErr))
			return
		}
		turnID, attemptID = turn.ID, attempt.ID
		ctx = operation.WithMetadata(ctx, operation.Metadata{
			TurnID: turn.ID, AttemptID: attempt.ID, MemoryID: attempt.MemoryID, BranchEpoch: attempt.BranchEpoch,
		})
	}
	succeeded := false
	if bundle.Character.FirstMessage == "__AUTO_GENERATE__" {
		succeeded = s.seedAutoTrigger(ctx, conv.ID, now)
	} else {
		succeeded = s.seedFirstMessage(ctx, conv.ID, bundle.Character.FirstMessage, now)
	}
	if s.turns != nil && turnID != uuid.Nil {
		_ = s.turns.Seal(context.WithoutCancel(ctx), turnID, attemptID, succeeded, "opening", "opening_seed_failed")
	}
}

func (s *Service) seedAutoTrigger(ctx context.Context, convID uuid.UUID, now time.Time) bool {
	chatID := uuid.New()
	if err := s.msgRepo.CreateChat(ctx, &storage.ChatMessage{
		ID:             chatID,
		ConversationID: convID,
		Role:           storage.RoleUser,
		Content:        "开始",
		CreatedAt:      now,
		UpdatedAt:      now,
	}); err != nil {
		// 种子消息失败不阻断建会话，但必须留痕（否则首次生成缺触发语，无从排查）。
		logging.Named("conversation").Warn("写入自动触发种子消息失败",
			zap.String("conversation_id", convID.String()), zap.Error(err))
		return false
	}
	// 不创建 display 消息，这条消息对用户不可见
	return true
}

func (s *Service) seedFirstMessage(ctx context.Context, convID uuid.UUID, text string, now time.Time) bool {
	log := logging.Named("conversation").With(zap.String("conversation_id", convID.String()))
	chatID := uuid.New()
	if err := s.msgRepo.CreateChat(ctx, &storage.ChatMessage{
		ID:             chatID,
		ConversationID: convID,
		Role:           storage.RoleAssistant,
		Content:        text,
		CreatedAt:      now,
		UpdatedAt:      now,
	}); err != nil {
		log.Warn("写入开场白内部消息失败", zap.Error(err))
		return false // chat 消息失败则不再挂 display，避免悬空引用
	}
	if err := s.msgRepo.CreateDisplay(ctx, &storage.DisplayMessage{
		ID:             uuid.New(),
		ConversationID: convID,
		ChatMessageID:  &chatID,
		Type:           "assistant",
		Markdown:       text,
		Metadata:       storage.JSONMap{"first_message": true},
		CreatedAt:      now,
	}); err != nil {
		log.Warn("写入开场白展示消息失败", zap.Error(err))
		return false
	}
	return true
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*storage.Conversation, error) {
	return s.GetOwned(ctx, id)
}

// GetOwned 读取对话并校验归属当前玩家；归属不符返回 ErrNotFound（不泄漏存在性）。
// 与 EnsureActive 的区别在于不校验 active 状态，适用于对已结束/归档对话的只读访问
// （如上下文构建日志查询）。
func (s *Service) GetOwned(ctx context.Context, id uuid.UUID) (*storage.Conversation, error) {
	conv, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if conv.PlayerID != s.identity.CurrentPlayerID(ctx) {
		return nil, storage.ErrNotFound
	}
	return conv, nil
}

func (s *Service) List(ctx context.Context, statuses []string, search string, limits ...int) ([]storage.Conversation, error) {
	limit := 50
	if len(limits) > 0 {
		limit = limits[0]
	}
	return s.repo.ListPage(ctx, s.identity.CurrentPlayerID(ctx), statuses, search, limit)
}

// EnsureActive 返回对话并校验其归属当前玩家且处于 active 状态。
// 归属不符时返回 ErrNotFound（而非专门错误），避免泄漏对话是否存在。
func (s *Service) EnsureActive(ctx context.Context, id uuid.UUID) (*storage.Conversation, error) {
	conv, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if conv.PlayerID != s.identity.CurrentPlayerID(ctx) {
		return nil, storage.ErrNotFound
	}
	if conv.Status != storage.StatusActive {
		return nil, ErrNotActive
	}
	// Enforce the version freeze at the write boundary as well. This also
	// repairs rows created before automatic archiving was introduced.
	if conv.CampaignID == nil && s.challenge != nil {
		current, versionErr := s.challenge.IsCurrentVersion(ctx, conv.ChallengeVersionID)
		if versionErr != nil {
			return nil, versionErr
		}
		if !current {
			now := time.Now()
			_ = s.repo.UpdateFields(ctx, id, map[string]any{
				"status": storage.StatusArchived, "archived_at": now, "updated_at": now,
			})
			return nil, ErrNotActive
		}
	}
	return conv, nil
}

// UpdateTitle 更新普通用户唯一可编辑的对话元数据。
func (s *Service) UpdateTitle(ctx context.Context, id uuid.UUID, title string) (*storage.Conversation, error) {
	if _, err := s.GetOwned(ctx, id); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateFields(ctx, id, map[string]any{"title": title, "updated_at": time.Now()}); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, id)
}

// Touch 更新对话的 updated_at，用于消息活动后排序。
func (s *Service) Touch(ctx context.Context, id uuid.UUID) error {
	return s.repo.UpdateFields(ctx, id, map[string]any{"updated_at": time.Now()})
}

// End 结束对话：置为 ended 并解除生成锁。
func (s *Service) End(ctx context.Context, id uuid.UUID) (*storage.Conversation, error) {
	if _, err := s.GetOwned(ctx, id); err != nil {
		return nil, err
	}
	now := time.Now()
	if err := s.repo.TransitionLifecycle(ctx, id, storage.StatusEnded, map[string]any{
		"ended_at": now, "updated_at": now,
	}); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, id)
}

// Archive 归档对话。
func (s *Service) Archive(ctx context.Context, id uuid.UUID) (*storage.Conversation, error) {
	conv, err := s.GetOwned(ctx, id)
	if err != nil {
		return nil, err
	}
	if conv.Status != storage.StatusActive {
		return nil, ErrNotActive
	}
	now := time.Now()
	if err := s.repo.TransitionLifecycle(ctx, id, storage.StatusArchived, map[string]any{
		"archived_at": now, "updated_at": now,
	}); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, id)
}

// Restore 只将未结算的归档对话恢复为 active；已结束对话不可恢复。
func (s *Service) Restore(ctx context.Context, id uuid.UUID) (*storage.Conversation, error) {
	conv, err := s.GetOwned(ctx, id)
	if err != nil {
		return nil, err
	}
	if conv.Status != storage.StatusArchived {
		return nil, ErrNotActive
	}
	if conv.CampaignID == nil && s.challenge != nil {
		current, versionErr := s.challenge.IsCurrentVersion(ctx, conv.ChallengeVersionID)
		if versionErr != nil {
			return nil, versionErr
		}
		if !current {
			return nil, ErrNotActive
		}
	}
	now := time.Now()
	if err := s.repo.TransitionLifecycle(ctx, id, storage.StatusActive, map[string]any{
		"archived_at": nil, "ended_at": nil, "updated_at": now,
	}); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, id)
}

// Delete 软删除对话（MVP 先软删除）。
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := s.GetOwned(ctx, id); err != nil {
		return err
	}
	now := time.Now()
	return s.repo.TransitionLifecycle(ctx, id, storage.StatusDeleted, map[string]any{
		"deleted_at": now, "updated_at": now,
	})
}

// StartGeneration atomically acquires the durable writable run after checking
// ownership. The repository repeats the active-state check under a row lock,
// closing the check-then-lock race in the old TryLock flow.
func (s *Service) StartGeneration(ctx context.Context, id uuid.UUID, request storage.GenerationRequest) (*storage.Conversation, *storage.ConversationGenerationRun, bool, error) {
	// Do the normal version/status check first. If it reports an inactive
	// conversation, still call the repository: it can safely replay a durable
	// operation identified by the same request_id without starting new work.
	if _, err := s.EnsureActive(ctx, id); err != nil && !errors.Is(err, ErrNotActive) {
		return nil, nil, false, err
	}
	conv, run, replayed, err := s.repo.StartGeneration(ctx, id, request)
	if errors.Is(err, storage.ErrConversationNotActive) {
		return nil, nil, false, ErrNotActive
	}
	return conv, run, replayed, err
}

// FinishGeneration releases only the run that acquired the lock.
func (s *Service) FinishGeneration(ctx context.Context, runID uuid.UUID, status, errorCode string) error {
	return s.repo.FinishGeneration(ctx, runID, status, errorCode)
}

// HeartbeatGeneration 续期仍在进行的生成租约。
func (s *Service) HeartbeatGeneration(ctx context.Context, runID uuid.UUID) error {
	return s.repo.HeartbeatGeneration(ctx, runID)
}

// FailActiveGeneration 封存对话当前的活动租约（生成 goroutine panic 后的兜底）。
func (s *Service) FailActiveGeneration(ctx context.Context, id uuid.UUID, errorCode string) error {
	return s.repo.FailActiveGeneration(ctx, id, errorCode)
}

// CancelGeneration records a cancellation request while retaining ownership
// until the worker has exited.
func (s *Service) CancelGeneration(ctx context.Context, id uuid.UUID, reason string) error {
	return s.repo.CancelGeneration(ctx, id, reason)
}
