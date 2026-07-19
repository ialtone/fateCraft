package storage

import (
	"context"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// MessageRepo 负责内部消息（chat_messages）与展示消息（display_messages）。
// 所有查询都强制带 conversation_id，保证对话隔离。
type MessageRepo struct{ db *gorm.DB }

func NewMessageRepo(db *gorm.DB) *MessageRepo { return &MessageRepo{db: db} }

// --- 内部消息 ---

func (r *MessageRepo) CreateChat(ctx context.Context, m *ChatMessage) error {
	return r.db.WithContext(ctx).Create(m).Error
}

func (r *MessageRepo) GetChat(ctx context.Context, convID, id uuid.UUID) (*ChatMessage, error) {
	var m ChatMessage
	err := r.db.WithContext(ctx).First(&m, "id = ? AND conversation_id = ?", id, convID).Error
	if err == gorm.ErrRecordNotFound {
		return nil, ErrNotFound
	}
	return &m, err
}

func (r *MessageRepo) UpdateChat(ctx context.Context, m *ChatMessage) error {
	return r.db.WithContext(ctx).Save(m).Error
}

func (r *MessageRepo) DeleteChat(ctx context.Context, convID, id uuid.UUID) error {
	return r.db.WithContext(ctx).
		Where("id = ? AND conversation_id = ?", id, convID).
		Delete(&ChatMessage{}).Error
}

// ListChat 按时间正序返回对话的内部消息，可选是否包含 hidden。
func (r *MessageRepo) ListChat(ctx context.Context, convID uuid.UUID, includeHidden bool) ([]ChatMessage, error) {
	q := r.db.WithContext(ctx).Where("conversation_id = ?", convID)
	if !includeHidden {
		q = q.Where("hidden = ?", false)
	}
	var out []ChatMessage
	err := q.Order("created_at ASC").Find(&out).Error
	return out, err
}

// LastAssistant 返回最近一条 assistant 内部消息。
func (r *MessageRepo) LastAssistant(ctx context.Context, convID uuid.UUID) (*ChatMessage, error) {
	var m ChatMessage
	err := r.db.WithContext(ctx).
		Where("conversation_id = ? AND role = ?", convID, RoleAssistant).
		Order("created_at DESC").First(&m).Error
	if err == gorm.ErrRecordNotFound {
		return nil, ErrNotFound
	}
	return &m, err
}

// DeleteChatFrom 删除某条消息（含）之后创建的所有内部消息，用于编辑/重新生成后重建上下文。
func (r *MessageRepo) DeleteChatFrom(ctx context.Context, convID, fromID uuid.UUID) error {
	var anchor ChatMessage
	if err := r.db.WithContext(ctx).First(&anchor, "id = ? AND conversation_id = ?", fromID, convID).Error; err != nil {
		return err
	}
	return r.db.WithContext(ctx).
		Where("conversation_id = ? AND created_at >= ?", convID, anchor.CreatedAt).
		Delete(&ChatMessage{}).Error
}

// --- 展示消息 ---

func (r *MessageRepo) CreateDisplay(ctx context.Context, m *DisplayMessage) error {
	return r.db.WithContext(ctx).Create(m).Error
}

func (r *MessageRepo) GetDisplay(ctx context.Context, convID, id uuid.UUID) (*DisplayMessage, error) {
	var m DisplayMessage
	err := r.db.WithContext(ctx).First(&m, "id = ? AND conversation_id = ?", id, convID).Error
	if err == gorm.ErrRecordNotFound {
		return nil, ErrNotFound
	}
	return &m, err
}

// GetDisplayByID 仅按 id 查询展示消息（用于先定位对话再校验归属）。
func (r *MessageRepo) GetDisplayByID(ctx context.Context, id uuid.UUID) (*DisplayMessage, error) {
	var m DisplayMessage
	err := r.db.WithContext(ctx).First(&m, "id = ?", id).Error
	if err == gorm.ErrRecordNotFound {
		return nil, ErrNotFound
	}
	return &m, err
}

func (r *MessageRepo) UpdateDisplay(ctx context.Context, m *DisplayMessage) error {
	return r.db.WithContext(ctx).Save(m).Error
}

func (r *MessageRepo) DeleteDisplay(ctx context.Context, convID, id uuid.UUID) error {
	return r.db.WithContext(ctx).
		Where("id = ? AND conversation_id = ?", id, convID).
		Delete(&DisplayMessage{}).Error
}

// DeleteDisplayByChatID 删除关联到某条内部消息的展示消息。
func (r *MessageRepo) DeleteDisplayByChatID(ctx context.Context, convID, chatID uuid.UUID) error {
	return r.db.WithContext(ctx).
		Where("conversation_id = ? AND chat_message_id = ?", convID, chatID).
		Delete(&DisplayMessage{}).Error
}

// ListDisplay 分页返回展示消息。before 为游标（返回早于该时间的消息），limit<=0 时取全部。
func (r *MessageRepo) ListDisplay(ctx context.Context, convID uuid.UUID, before *uuid.UUID, limit int) ([]DisplayMessage, error) {
	q := r.db.WithContext(ctx).Where("conversation_id = ?", convID)
	if before != nil {
		var anchor DisplayMessage
		if err := r.db.WithContext(ctx).First(&anchor, "id = ? AND conversation_id = ?", *before, convID).Error; err == nil {
			q = q.Where("created_at < ?", anchor.CreatedAt)
		}
	}
	q = q.Order("created_at DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	var out []DisplayMessage
	if err := q.Find(&out).Error; err != nil {
		return nil, err
	}
	// 翻转为时间正序，便于前端直接渲染。
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// DeleteDisplayMessagesAfter 删除指定消息之后创建的所有展示消息和内部消息（用于天道回溯）。
// 在事务中按锚点 created_at 删除，内部消息不依赖 display 关联，
// 因此"开始"/"继续"等无展示的隐藏消息也会一并清除。
func (r *MessageRepo) DeleteDisplayMessagesAfter(ctx context.Context, convID, afterID uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 获取锚点消息的创建时间
		var anchor DisplayMessage
		if err := tx.First(&anchor, "id = ? AND conversation_id = ?", afterID, convID).Error; err != nil {
			return err
		}

		// 删除锚点之后创建的所有内部消息（含无 display 关联的隐藏消息）
		if err := tx.
			Where("conversation_id = ? AND created_at > ?", convID, anchor.CreatedAt).
			Delete(&ChatMessage{}).Error; err != nil {
			return err
		}

		// 删除锚点之后创建的所有展示消息
		return tx.
			Where("conversation_id = ? AND created_at > ?", convID, anchor.CreatedAt).
			Delete(&DisplayMessage{}).Error
	})
}
