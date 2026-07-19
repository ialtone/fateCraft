package conversation

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/ialtone/fatecraft/internal/identity"
	"github.com/ialtone/fatecraft/internal/logging"
	"github.com/ialtone/fatecraft/internal/state"
	"github.com/ialtone/fatecraft/internal/storage"
)

// ErrNotActive 表示对话不处于 active 状态，无法继续生成或编辑。
var ErrNotActive = errors.New("对话未处于 active 状态")

// conversationRepo 是本服务消费端的对话仓储小接口（只声明实际用到的方法）。
// storage.ConversationRepo 天然满足该接口，便于用 mock 脱离 Postgres 单测。
type conversationRepo interface {
	Create(ctx context.Context, c *storage.Conversation) error
	Get(ctx context.Context, id uuid.UUID) (*storage.Conversation, error)
	List(ctx context.Context, playerID string, statuses []string, search string) ([]storage.Conversation, error)
	UpdateFields(ctx context.Context, id uuid.UUID, fields map[string]any) error
	IncrementRewindsUsed(ctx context.Context, id uuid.UUID) error
	SetWorldBooks(ctx context.Context, convID uuid.UUID, bookIDs []string) error
	AddWorldBook(ctx context.Context, convID uuid.UUID, bookID string) error
	RemoveWorldBook(ctx context.Context, convID uuid.UUID, bookID string) error
	WorldBookIDs(ctx context.Context, convID uuid.UUID) ([]string, error)
	TryLock(ctx context.Context, id uuid.UUID) (bool, error)
	Unlock(ctx context.Context, id uuid.UUID) error
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
	charStore CharacterStore
	identity  identity.Provider
}

// CharacterStore 角色存储接口
type CharacterStore interface {
	Get(id string) (storage.Character, error)
}

func NewService(repo conversationRepo, stateSvc *state.Service, msgRepo messageSeeder, charStore CharacterStore, ident identity.Provider) *Service {
	return &Service{repo: repo, stateSvc: stateSvc, msgRepo: msgRepo, charStore: charStore, identity: ident}
}

// CreateInput 新建对话的入参（各模板 id 为文件 slug）。
type CreateInput struct {
	Title              string   `json:"title"`
	CharacterID        *string  `json:"character_id"`
	PersonaID          *string  `json:"persona_id"`
	ScenarioID         *string  `json:"scenario_id"`
	PromptPresetID     *string  `json:"prompt_preset_id"`
	WorldBookIDs       []string `json:"world_book_ids"`
	RollSystemEnabled  *bool    `json:"roll_system_enabled"`
	RollDifficultyMode *string  `json:"roll_difficulty_mode"`
}

// Create 新建对话：初始化独立状态、绑定世界书模板，并按角色卡开场白生成首条消息。
func (s *Service) Create(ctx context.Context, in CreateInput) (*storage.Conversation, error) {
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
		CharacterID:        in.CharacterID,
		PersonaID:          in.PersonaID,
		ScenarioID:         in.ScenarioID,
		PromptPresetID:     in.PromptPresetID,
		RollSystemEnabled:  in.RollSystemEnabled,
		RollDifficultyMode: in.RollDifficultyMode,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if err := s.repo.Create(ctx, conv); err != nil {
		return nil, err
	}
	if err := s.stateSvc.EnsureInit(ctx, conv.ID); err != nil {
		return nil, err
	}
	if len(in.WorldBookIDs) > 0 {
		if err := s.repo.SetWorldBooks(ctx, conv.ID, in.WorldBookIDs); err != nil {
			return nil, err
		}
	}
	// 角色卡开场白作为首条 assistant 消息。
	// 如果是 __AUTO_GENERATE__ 标记，则插入一条触发消息让 AI 自动生成开场。
	if in.CharacterID != nil {
		if ch, err := s.charStore.Get(*in.CharacterID); err == nil && ch.FirstMessage != "" {
			if ch.FirstMessage == "__AUTO_GENERATE__" {
				// 插入一条隐藏的触发消息，让 AI 自动生成开场
				s.seedAutoTrigger(ctx, conv.ID, now)
			} else {
				s.seedFirstMessage(ctx, conv.ID, ch.FirstMessage, now)
			}
		}
	}
	return conv, nil
}

func (s *Service) seedAutoTrigger(ctx context.Context, convID uuid.UUID, now time.Time) {
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
	}
	// 不创建 display 消息，这条消息对用户不可见
}

func (s *Service) seedFirstMessage(ctx context.Context, convID uuid.UUID, text string, now time.Time) {
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
		return // chat 消息失败则不再挂 display，避免悬空引用
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
	}
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*storage.Conversation, error) {
	return s.repo.Get(ctx, id)
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

func (s *Service) List(ctx context.Context, statuses []string, search string) ([]storage.Conversation, error) {
	return s.repo.List(ctx, s.identity.CurrentPlayerID(ctx), statuses, search)
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
	return conv, nil
}

// Update 局部更新对话可编辑字段（目前为标题与各模板绑定）。
func (s *Service) Update(ctx context.Context, id uuid.UUID, fields map[string]any) (*storage.Conversation, error) {
	fields["updated_at"] = time.Now()
	if err := s.repo.UpdateFields(ctx, id, fields); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, id)
}

// Touch 更新对话的 updated_at，用于消息活动后排序。
func (s *Service) Touch(ctx context.Context, id uuid.UUID) error {
	return s.repo.UpdateFields(ctx, id, map[string]any{"updated_at": time.Now()})
}

// IncrementRewindsUsed 将对话本局已用回溯次数 +1（天道回溯成功时调用）。
func (s *Service) IncrementRewindsUsed(ctx context.Context, id uuid.UUID) error {
	return s.repo.IncrementRewindsUsed(ctx, id)
}

// End 结束对话：置为 ended 并解除生成锁。
func (s *Service) End(ctx context.Context, id uuid.UUID) (*storage.Conversation, error) {
	now := time.Now()
	if err := s.repo.UpdateFields(ctx, id, map[string]any{
		"status": storage.StatusEnded, "ended_at": now, "is_processing": false, "updated_at": now,
	}); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, id)
}

// Archive 归档对话。
func (s *Service) Archive(ctx context.Context, id uuid.UUID) (*storage.Conversation, error) {
	now := time.Now()
	if err := s.repo.UpdateFields(ctx, id, map[string]any{
		"status": storage.StatusArchived, "archived_at": now, "updated_at": now,
	}); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, id)
}

// Restore 将归档/结束的对话恢复为 active。
func (s *Service) Restore(ctx context.Context, id uuid.UUID) (*storage.Conversation, error) {
	now := time.Now()
	if err := s.repo.UpdateFields(ctx, id, map[string]any{
		"status": storage.StatusActive, "archived_at": nil, "ended_at": nil, "updated_at": now,
	}); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, id)
}

// Delete 软删除对话（MVP 先软删除）。
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	now := time.Now()
	return s.repo.UpdateFields(ctx, id, map[string]any{
		"status": storage.StatusDeleted, "deleted_at": now, "updated_at": now,
	})
}

// WorldBookIDs 返回对话绑定的世界书 id（模板文件 slug）。
func (s *Service) WorldBookIDs(ctx context.Context, id uuid.UUID) ([]string, error) {
	return s.repo.WorldBookIDs(ctx, id)
}

// SetWorldBooks 覆盖对话的世界书绑定。
func (s *Service) SetWorldBooks(ctx context.Context, id uuid.UUID, bookIDs []string) error {
	return s.repo.SetWorldBooks(ctx, id, bookIDs)
}

func (s *Service) AddWorldBook(ctx context.Context, id uuid.UUID, bookID string) error {
	return s.repo.AddWorldBook(ctx, id, bookID)
}

func (s *Service) RemoveWorldBook(ctx context.Context, id uuid.UUID, bookID string) error {
	return s.repo.RemoveWorldBook(ctx, id, bookID)
}

// TryLock / Unlock 暴露对话级生成锁。
func (s *Service) TryLock(ctx context.Context, id uuid.UUID) (bool, error) {
	return s.repo.TryLock(ctx, id)
}
func (s *Service) Unlock(ctx context.Context, id uuid.UUID) error { return s.repo.Unlock(ctx, id) }
