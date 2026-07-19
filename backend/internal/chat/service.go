package chat

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/storage"
)

// messageRepo 是本服务消费端的消息仓储小接口（只声明实际用到的方法）。
// storage.MessageRepo 天然满足该接口，便于用 mock 脱离 Postgres 单测。
type messageRepo interface {
	CreateChat(ctx context.Context, m *storage.ChatMessage) error
	GetChat(ctx context.Context, convID, id uuid.UUID) (*storage.ChatMessage, error)
	UpdateChat(ctx context.Context, m *storage.ChatMessage) error
	DeleteChat(ctx context.Context, convID, id uuid.UUID) error
	ListChat(ctx context.Context, convID uuid.UUID, includeHidden bool) ([]storage.ChatMessage, error)
	LastAssistant(ctx context.Context, convID uuid.UUID) (*storage.ChatMessage, error)
	CreateDisplay(ctx context.Context, m *storage.DisplayMessage) error
	GetDisplay(ctx context.Context, convID, id uuid.UUID) (*storage.DisplayMessage, error)
	UpdateDisplay(ctx context.Context, m *storage.DisplayMessage) error
	DeleteDisplay(ctx context.Context, convID, id uuid.UUID) error
	DeleteDisplayByChatID(ctx context.Context, convID, chatID uuid.UUID) error
	ListDisplay(ctx context.Context, convID uuid.UUID, before *uuid.UUID, limit int) ([]storage.DisplayMessage, error)
}

// Service 负责对话内消息的持久化与编辑操作。所有方法都带 conversation_id。
type Service struct {
	repo messageRepo
}

func NewService(repo messageRepo) *Service { return &Service{repo: repo} }

// AppendUser 写入一条用户消息（内部消息 + 展示消息）。
func (s *Service) AppendUser(ctx context.Context, convID uuid.UUID, content string, tokenCount *int) (*storage.ChatMessage, *storage.DisplayMessage, error) {
	now := time.Now()
	chatMsg := &storage.ChatMessage{
		ID:             uuid.New(),
		ConversationID: convID,
		Role:           storage.RoleUser,
		Content:        content,
		TokenCount:     tokenCount,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.repo.CreateChat(ctx, chatMsg); err != nil {
		return nil, nil, err
	}
	disp := &storage.DisplayMessage{
		ID:             uuid.New(),
		ConversationID: convID,
		ChatMessageID:  &chatMsg.ID,
		Type:           "user",
		Markdown:       content,
		Metadata:       storage.JSONMap{},
		CreatedAt:      now,
	}
	if err := s.repo.CreateDisplay(ctx, disp); err != nil {
		return nil, nil, err
	}
	return chatMsg, disp, nil
}

// AppendUserChatOnly 写入一条只进入上下文、不产生展示消息的用户消息
// （用于"继续"这类自动触发，参考 conversation.Service 的 seedAutoTrigger）。
func (s *Service) AppendUserChatOnly(ctx context.Context, convID uuid.UUID, content string, tokenCount *int) (*storage.ChatMessage, error) {
	now := time.Now()
	chatMsg := &storage.ChatMessage{
		ID:             uuid.New(),
		ConversationID: convID,
		Role:           storage.RoleUser,
		Content:        content,
		TokenCount:     tokenCount,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.repo.CreateChat(ctx, chatMsg); err != nil {
		return nil, err
	}
	return chatMsg, nil
}

// AppendAssistant 写入一条 assistant 消息（内部消息 + 展示消息）。
func (s *Service) AppendAssistant(ctx context.Context, convID uuid.UUID, content string, markdown string, metadata map[string]any, tokenCount *int) (*storage.ChatMessage, *storage.DisplayMessage, error) {
	return s.AppendAssistantWithID(ctx, convID, uuid.New(), content, markdown, metadata, tokenCount)
}

// AppendAssistantWithID 同 AppendAssistant，但允许指定展示消息 id，
// 以便与流式期间使用的 message_id 保持一致。
func (s *Service) AppendAssistantWithID(ctx context.Context, convID, displayID uuid.UUID, content string, markdown string, metadata map[string]any, tokenCount *int) (*storage.ChatMessage, *storage.DisplayMessage, error) {
	now := time.Now()
	chatMsg := &storage.ChatMessage{
		ID:             uuid.New(),
		ConversationID: convID,
		Role:           storage.RoleAssistant,
		Content:        content,
		TokenCount:     tokenCount,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.repo.CreateChat(ctx, chatMsg); err != nil {
		return nil, nil, err
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	disp := &storage.DisplayMessage{
		ID:             displayID,
		ConversationID: convID,
		ChatMessageID:  &chatMsg.ID,
		Type:           "assistant",
		Markdown:       markdown,
		Metadata:       storage.JSONMap(metadata),
		CreatedAt:      now,
	}
	if err := s.repo.CreateDisplay(ctx, disp); err != nil {
		return nil, nil, err
	}
	return chatMsg, disp, nil
}

// AppendSystemEvent 写入一条系统/事件类展示消息（不进入上下文）。
func (s *Service) AppendSystemEvent(ctx context.Context, convID uuid.UUID, dtype, markdown string, metadata map[string]any) (*storage.DisplayMessage, error) {
	if metadata == nil {
		metadata = map[string]any{}
	}
	disp := &storage.DisplayMessage{
		ID:             uuid.New(),
		ConversationID: convID,
		Type:           dtype,
		Markdown:       markdown,
		Metadata:       storage.JSONMap(metadata),
		CreatedAt:      time.Now(),
	}
	if err := s.repo.CreateDisplay(ctx, disp); err != nil {
		return nil, err
	}
	return disp, nil
}

// AppendSystemChatEvent 写入一条系统事件：既进入上下文（system 角色的内部消息），
// 又产生一条展示消息（关联到该内部消息）。用于判定结果这类"模型后续需要知道、
// 玩家也要看到"的事件——只落展示消息会导致下一轮生成看不到该事件。
// content 是给模型看的文本（进上下文），markdown 是给前端展示的文本，通常相同。
func (s *Service) AppendSystemChatEvent(ctx context.Context, convID uuid.UUID, dtype, content, markdown string, metadata map[string]any) (*storage.ChatMessage, *storage.DisplayMessage, error) {
	now := time.Now()
	chatMsg := &storage.ChatMessage{
		ID:             uuid.New(),
		ConversationID: convID,
		Role:           storage.RoleSystem,
		Content:        content,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := s.repo.CreateChat(ctx, chatMsg); err != nil {
		return nil, nil, err
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	disp := &storage.DisplayMessage{
		ID:             uuid.New(),
		ConversationID: convID,
		ChatMessageID:  &chatMsg.ID,
		Type:           dtype,
		Markdown:       markdown,
		Metadata:       storage.JSONMap(metadata),
		CreatedAt:      now,
	}
	if err := s.repo.CreateDisplay(ctx, disp); err != nil {
		return nil, nil, err
	}
	return chatMsg, disp, nil
}

// History 返回用于上下文构造的内部消息（含或不含 hidden）。
func (s *Service) History(ctx context.Context, convID uuid.UUID, includeHidden bool) ([]storage.ChatMessage, error) {
	return s.repo.ListChat(ctx, convID, includeHidden)
}

// ListDisplay 分页返回展示消息（时间正序）。
func (s *Service) ListDisplay(ctx context.Context, convID uuid.UUID, before *uuid.UUID, limit int) ([]storage.DisplayMessage, error) {
	return s.repo.ListDisplay(ctx, convID, before, limit)
}

// UpdateDisplayMetadata 回写一条展示消息的 metadata（用于状态应用后把 applied_state
// 等信息持久化，否则只存在于内存对象与 WS 事件中，刷新后 GET /messages 会丢失）。
func (s *Service) UpdateDisplayMetadata(ctx context.Context, convID, displayID uuid.UUID, metadata map[string]any) error {
	disp, err := s.repo.GetDisplay(ctx, convID, displayID)
	if err != nil {
		return err
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	disp.Metadata = storage.JSONMap(metadata)
	return s.repo.UpdateDisplay(ctx, disp)
}

// EditInput 编辑消息的入参。
type EditInput struct {
	Content *string `json:"content"`
	Hidden  *bool   `json:"hidden"`
}

// EditDisplay 编辑一条展示消息，并同步其关联的内部消息内容。
func (s *Service) EditDisplay(ctx context.Context, convID, displayID uuid.UUID, in EditInput) (*storage.DisplayMessage, error) {
	disp, err := s.repo.GetDisplay(ctx, convID, displayID)
	if err != nil {
		return nil, err
	}
	if in.Content != nil {
		disp.Markdown = *in.Content
		if err := s.repo.UpdateDisplay(ctx, disp); err != nil {
			return nil, err
		}
		if disp.ChatMessageID != nil {
			if cm, err := s.repo.GetChat(ctx, convID, *disp.ChatMessageID); err == nil {
				cm.Content = *in.Content
				cm.UpdatedAt = time.Now()
				if err := s.repo.UpdateChat(ctx, cm); err != nil {
					return nil, err
				}
			}
		}
	}
	if in.Hidden != nil && disp.ChatMessageID != nil {
		if cm, err := s.repo.GetChat(ctx, convID, *disp.ChatMessageID); err == nil {
			cm.Hidden = *in.Hidden
			cm.UpdatedAt = time.Now()
			if err := s.repo.UpdateChat(ctx, cm); err != nil {
				return nil, err
			}
		}
	}
	return disp, nil
}

// DeleteDisplay 删除一条展示消息及其关联的内部消息。
func (s *Service) DeleteDisplay(ctx context.Context, convID, displayID uuid.UUID) error {
	disp, err := s.repo.GetDisplay(ctx, convID, displayID)
	if err != nil {
		return err
	}
	if disp.ChatMessageID != nil {
		if err := s.repo.DeleteChat(ctx, convID, *disp.ChatMessageID); err != nil {
			return err
		}
	}
	return s.repo.DeleteDisplay(ctx, convID, displayID)
}

// LastAssistant 返回最近一条 assistant 内部消息。
func (s *Service) LastAssistant(ctx context.Context, convID uuid.UUID) (*storage.ChatMessage, error) {
	return s.repo.LastAssistant(ctx, convID)
}

// DropLastAssistant 删除最近一条 assistant 消息及其展示消息，用于重新生成。
func (s *Service) DropLastAssistant(ctx context.Context, convID uuid.UUID) error {
	last, err := s.repo.LastAssistant(ctx, convID)
	if err != nil {
		return err
	}
	if err := s.repo.DeleteDisplayByChatID(ctx, convID, last.ID); err != nil {
		return err
	}
	return s.repo.DeleteChat(ctx, convID, last.ID)
}
