package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/operation"
	"gorm.io/gorm"
)

// ErrMessageNotUser 表示目标展示消息不是可供玩家改写的用户输入。
var ErrMessageNotUser = errors.New("目标消息不是用户输入")

// RewriteUserMessageInput 描述一次原子剧情改写。奖励消费、消息编辑和后续分支截断
// 必须在同一事务内完成，避免任一步骤失败后留下已扣奖励或半截消息树。
type RewriteUserMessageInput struct {
	ConversationID uuid.UUID
	MessageID      uuid.UUID
	Content        string
	PlayerID       uuid.UUID
	IdempotencyKey string
	InitialState   JSONMap
}

// MessageRepo 负责内部消息（chat_messages）与展示消息（display_messages）。
// 所有查询都强制带 conversation_id，保证对话隔离。
type MessageRepo struct{ db *gorm.DB }

func NewMessageRepo(db *gorm.DB) *MessageRepo { return &MessageRepo{db: db} }

// CreateUserWithDisplay persists the internal and player-facing projections
// together. The generation pipeline uses this boundary so cancellation or a
// display insert failure cannot leave an invisible user trigger in context.
func (r *MessageRepo) CreateUserWithDisplay(ctx context.Context, convID uuid.UUID, content string, tokenCount *int) (*ChatMessage, *DisplayMessage, error) {
	var chat ChatMessage
	var display DisplayMessage
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		chat = ChatMessage{ID: uuid.New(), ConversationID: convID, Role: RoleUser, Kind: "text", Content: content, TokenCount: tokenCount, ContentBlocks: messageTextContentBlocks(content), CreatedAt: now, UpdatedAt: now}
		if metadata, ok := operation.FromContext(ctx); ok {
			chat.TurnID, chat.AttemptID = &metadata.TurnID, &metadata.AttemptID
		}
		if err := r.createChatInTx(ctx, tx, &chat); err != nil {
			return err
		}
		display = DisplayMessage{ID: uuid.New(), ConversationID: convID, ChatMessageID: &chat.ID, Type: "user", Markdown: content, Metadata: JSONMap{}, CreatedAt: now}
		if metadata, ok := operation.FromContext(ctx); ok {
			display.TurnID, display.AttemptID = &metadata.TurnID, &metadata.AttemptID
		}
		return tx.Create(&display).Error
	})
	if err != nil {
		return nil, nil, err
	}
	return &chat, &display, nil
}

// CreateAssistantWithDisplay atomically persists the canonical assistant chat
// row and its display projection, including provider content blocks.
func (r *MessageRepo) CreateAssistantWithDisplay(ctx context.Context, convID, displayID uuid.UUID, content, markdown string, metadata map[string]any, tokenCount *int, blocks JSONDoc) (*ChatMessage, *DisplayMessage, error) {
	if contentBlocksMissing(blocks) {
		blocks = messageTextContentBlocks(content)
	}
	if err := validateContentBlocks(blocks); err != nil {
		return nil, nil, err
	}
	var chat ChatMessage
	var display DisplayMessage
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		chat = ChatMessage{ID: uuid.New(), ConversationID: convID, Role: RoleAssistant, Kind: "text", Content: content, ContentBlocks: blocks, TokenCount: tokenCount, CreatedAt: now, UpdatedAt: now}
		if metadata, ok := operation.FromContext(ctx); ok {
			chat.TurnID, chat.AttemptID = &metadata.TurnID, &metadata.AttemptID
		}
		if err := r.createChatInTx(ctx, tx, &chat); err != nil {
			return err
		}
		if metadata == nil {
			metadata = map[string]any{}
		}
		display = DisplayMessage{ID: displayID, ConversationID: convID, ChatMessageID: &chat.ID, Type: "assistant", Markdown: markdown, Metadata: JSONMap(metadata), Reasoning: ReasoningTextBlocks(blocks), CreatedAt: now}
		if operationMetadata, ok := operation.FromContext(ctx); ok {
			display.TurnID, display.AttemptID = &operationMetadata.TurnID, &operationMetadata.AttemptID
		}
		return tx.Create(&display).Error
	})
	if err != nil {
		return nil, nil, err
	}
	return &chat, &display, nil
}

// CreateSystemWithDisplay atomically persists a system context message and its
// player-facing event projection.
func (r *MessageRepo) CreateSystemWithDisplay(ctx context.Context, convID uuid.UUID, dtype, content, markdown string, metadata map[string]any) (*ChatMessage, *DisplayMessage, error) {
	var chat ChatMessage
	var display DisplayMessage
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		chat = ChatMessage{ID: uuid.New(), ConversationID: convID, Role: RoleSystem, Kind: "text", Content: content, ContentBlocks: messageTextContentBlocks(content), CreatedAt: now, UpdatedAt: now}
		if metadata, ok := operation.FromContext(ctx); ok {
			chat.TurnID, chat.AttemptID = &metadata.TurnID, &metadata.AttemptID
		}
		if err := r.createChatInTx(ctx, tx, &chat); err != nil {
			return err
		}
		if metadata == nil {
			metadata = map[string]any{}
		}
		display = DisplayMessage{ID: uuid.New(), ConversationID: convID, ChatMessageID: &chat.ID, TurnID: chat.TurnID, AttemptID: chat.AttemptID, Type: dtype, Markdown: markdown, Metadata: JSONMap(metadata), CreatedAt: now}
		return tx.Create(&display).Error
	})
	if err != nil {
		return nil, nil, err
	}
	return &chat, &display, nil
}

// --- 内部消息 ---

func (r *MessageRepo) CreateChat(ctx context.Context, m *ChatMessage) error {
	if m == nil {
		return errors.New("消息不能为空")
	}
	if metadata, ok := operation.FromContext(ctx); ok {
		m.TurnID = &metadata.TurnID
		m.AttemptID = &metadata.AttemptID
	}
	if err := normalizeChatMessage(m); err != nil {
		return err
	}
	if _, ok := operation.GenerationFromContext(ctx); ok {
		return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return r.createChatInTx(ctx, tx, m)
		})
	}
	return r.createChatInTx(ctx, r.db, m)
}

func (r *MessageRepo) createChatInTx(ctx context.Context, db *gorm.DB, m *ChatMessage) error {
	// Keep this invariant at the lowest persistence boundary as well. Some
	// compound operations (for example regenerate) intentionally use this
	// helper directly inside their transaction and must not be able to bypass
	// the content_blocks normalization performed by CreateChat.
	if m == nil {
		return errors.New("消息不能为空")
	}
	if err := normalizeChatMessage(m); err != nil {
		return err
	}
	if err := AssertGenerationActive(ctx, db, m.ConversationID); err != nil {
		return err
	}
	if m.MessageSeq == 0 {
		var row struct{ Sequence int64 }
		if err := db.WithContext(ctx).Raw(`
			UPDATE conversations
			SET next_message_seq = next_message_seq + 1
			WHERE id = ?
			RETURNING next_message_seq - 1 AS sequence`, m.ConversationID).Scan(&row).Error; err != nil {
			return err
		}
		if row.Sequence == 0 {
			return ErrNotFound
		}
		m.MessageSeq = row.Sequence
	}
	return db.WithContext(ctx).Create(m).Error
}

// normalizeChatMessage enforces the database invariant for every chat message
// write path. PostgreSQL has a NOT NULL/default constraint on content_blocks,
// but GORM serializes a nil JSONDoc as an explicit NULL when a struct is
// inserted, so relying on the SQL default is insufficient.
func normalizeChatMessage(m *ChatMessage) error {
	if m.TokenizerVersion == "" {
		m.TokenizerVersion = ChatTokenizerVersion
	}
	if m.Kind == "" {
		m.Kind = "text"
	}
	if contentBlocksMissing(m.ContentBlocks) {
		// Tool-call messages carry their payload in the tool_call block supplied
		// by the caller. Do not synthesize a text block from the diagnostic
		// Content field for that kind.
		if m.Kind == "tool_call" {
			m.ContentBlocks = JSONDoc("[]")
		} else {
			m.ContentBlocks = messageTextContentBlocks(m.Content)
		}
		return nil
	}
	return validateContentBlocks(m.ContentBlocks)
}

func contentBlocksMissing(raw JSONDoc) bool {
	trimmed := strings.TrimSpace(string(raw))
	return trimmed == "" || trimmed == "null"
}

func validateContentBlocks(raw JSONDoc) error {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed[0] != '[' {
		return errors.New("消息内容块必须是 JSON 数组")
	}
	var blocks []struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return fmt.Errorf("消息内容块必须是 JSON 数组: %w", err)
	}
	for _, block := range blocks {
		switch block.Kind {
		case "text", "thinking", "refusal", "tool_call":
		default:
			return fmt.Errorf("消息内容块类型无效: %s", block.Kind)
		}
	}
	return nil
}

func messageTextContentBlocks(content string) JSONDoc {
	if content == "" {
		return JSONDoc("[]")
	}
	raw, _ := json.Marshal([]map[string]any{{"kind": "text", "text": content}})
	return JSONDoc(raw)
}

func (r *MessageRepo) GetChat(ctx context.Context, convID, id uuid.UUID) (*ChatMessage, error) {
	var m ChatMessage
	err := r.db.WithContext(ctx).First(&m, "id = ? AND conversation_id = ? AND invalidated_at IS NULL", id, convID).Error
	if err == gorm.ErrRecordNotFound {
		return nil, ErrNotFound
	}
	return &m, err
}

func (r *MessageRepo) UpdateChat(ctx context.Context, m *ChatMessage) error {
	if m == nil {
		return errors.New("消息不能为空")
	}
	if err := normalizeChatMessage(m); err != nil {
		return err
	}
	if _, ok := operation.GenerationFromContext(ctx); ok {
		return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := AssertGenerationActive(ctx, tx, m.ConversationID); err != nil {
				return err
			}
			return tx.Save(m).Error
		})
	}
	return r.db.WithContext(ctx).Save(m).Error
}

func (r *MessageRepo) DeleteChat(ctx context.Context, convID, id uuid.UUID) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&ChatMessage{}).
		Where("id = ? AND conversation_id = ?", id, convID).
		Updates(map[string]any{"invalidated_at": now, "invalidation_reason": "deleted"}).Error
}

// ListChat 按时间正序返回对话的内部消息，可选是否包含 hidden。
func (r *MessageRepo) ListChat(ctx context.Context, convID uuid.UUID, includeHidden bool) ([]ChatMessage, error) {
	q := r.db.WithContext(ctx).Table("chat_messages").
		Select("chat_messages.*, COALESCE(conversation_turns.turn_seq, 0) AS turn_seq").
		Joins("LEFT JOIN conversation_turns ON conversation_turns.id = chat_messages.turn_id").
		Where("chat_messages.conversation_id = ? AND chat_messages.invalidated_at IS NULL", convID)
	if !includeHidden {
		q = q.Where("chat_messages.hidden = ?", false)
	}
	var out []ChatMessage
	err := q.Order("chat_messages.message_seq ASC").Find(&out).Error
	return out, err
}

// LastAssistant 返回最近一条 assistant 内部消息。
func (r *MessageRepo) LastAssistant(ctx context.Context, convID uuid.UUID) (*ChatMessage, error) {
	var m ChatMessage
	err := r.db.WithContext(ctx).
		Where("conversation_id = ? AND role = ? AND invalidated_at IS NULL", convID, RoleAssistant).
		Order("message_seq DESC").First(&m).Error
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
	now := time.Now()
	return r.db.WithContext(ctx).Model(&ChatMessage{}).
		Where("conversation_id = ? AND message_seq >= ? AND invalidated_at IS NULL", convID, anchor.MessageSeq).
		Updates(map[string]any{"invalidated_at": now, "invalidation_reason": "branch_rewrite"}).Error
}

// --- 展示消息 ---

func (r *MessageRepo) CreateDisplay(ctx context.Context, m *DisplayMessage) error {
	if metadata, ok := operation.FromContext(ctx); ok {
		m.TurnID = &metadata.TurnID
		m.AttemptID = &metadata.AttemptID
	}
	if _, ok := operation.GenerationFromContext(ctx); ok {
		return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := AssertGenerationActive(ctx, tx, m.ConversationID); err != nil {
				return err
			}
			return tx.Create(m).Error
		})
	}
	return r.db.WithContext(ctx).Create(m).Error
}

func (r *MessageRepo) GetDisplay(ctx context.Context, convID, id uuid.UUID) (*DisplayMessage, error) {
	var m DisplayMessage
	err := r.db.WithContext(ctx).First(&m, "id = ? AND conversation_id = ? AND invalidated_at IS NULL", id, convID).Error
	if err == gorm.ErrRecordNotFound {
		return nil, ErrNotFound
	}
	if err == nil {
		// hydrateDisplayReasoning works on a slice to avoid an N+1 query in
		// ListDisplay; copy the computed projection back for this single row.
		var hydrated []DisplayMessage
		hydrated = append(hydrated, m)
		if err := r.hydrateDisplayReasoning(ctx, hydrated); err != nil {
			return nil, err
		}
		m = hydrated[0]
	}
	return &m, err
}

// GetDisplayByID 仅按 id 查询展示消息（用于先定位对话再校验归属）。
func (r *MessageRepo) GetDisplayByID(ctx context.Context, id uuid.UUID) (*DisplayMessage, error) {
	var m DisplayMessage
	err := r.db.WithContext(ctx).First(&m, "id = ? AND invalidated_at IS NULL", id).Error
	if err == gorm.ErrRecordNotFound {
		return nil, ErrNotFound
	}
	if err == nil {
		rows := []DisplayMessage{m}
		if err := r.hydrateDisplayReasoning(ctx, rows); err != nil {
			return nil, err
		}
		m = rows[0]
	}
	return &m, err
}

func (r *MessageRepo) UpdateDisplay(ctx context.Context, m *DisplayMessage) error {
	if _, ok := operation.GenerationFromContext(ctx); ok {
		return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := AssertGenerationActive(ctx, tx, m.ConversationID); err != nil {
				return err
			}
			return tx.Save(m).Error
		})
	}
	return r.db.WithContext(ctx).Save(m).Error
}

func (r *MessageRepo) DeleteDisplay(ctx context.Context, convID, id uuid.UUID) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&DisplayMessage{}).
		Where("id = ? AND conversation_id = ?", id, convID).
		Updates(map[string]any{"invalidated_at": now, "invalidation_reason": "deleted"}).Error
}

// DeleteDisplayByChatID 删除关联到某条内部消息的展示消息。
func (r *MessageRepo) DeleteDisplayByChatID(ctx context.Context, convID, chatID uuid.UUID) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&DisplayMessage{}).
		Where("conversation_id = ? AND chat_message_id = ?", convID, chatID).
		Updates(map[string]any{"invalidated_at": now, "invalidation_reason": "deleted"}).Error
}

// ListDisplay 分页返回展示消息。before 为复合游标锚点。
func (r *MessageRepo) ListDisplay(ctx context.Context, convID uuid.UUID, before *uuid.UUID, limit int) ([]DisplayMessage, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	q := r.db.WithContext(ctx).Where("conversation_id = ? AND invalidated_at IS NULL", convID)
	if before != nil {
		var anchor DisplayMessage
		if err := r.db.WithContext(ctx).First(&anchor, "id = ? AND conversation_id = ? AND invalidated_at IS NULL", *before, convID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrNotFound
			}
			return nil, err
		}
		q = q.Where("(created_at < ?) OR (created_at = ? AND id < ?)", anchor.CreatedAt, anchor.CreatedAt, anchor.ID)
	}
	q = q.Order("created_at DESC, id DESC")
	q = q.Limit(limit)
	var out []DisplayMessage
	if err := q.Find(&out).Error; err != nil {
		return nil, err
	}
	// 翻转为时间正序，便于前端直接渲染。
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	if err := r.hydrateDisplayReasoning(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}

// hydrateDisplayReasoning joins the safe readable projection from linked
// assistant messages without exposing provider-owned raw reasoning items.
func (r *MessageRepo) hydrateDisplayReasoning(ctx context.Context, displays []DisplayMessage) error {
	ids := make([]uuid.UUID, 0, len(displays))
	turnIDs := make([]uuid.UUID, 0, len(displays))
	seen := make(map[uuid.UUID]struct{}, len(displays))
	seenTurns := make(map[uuid.UUID]struct{}, len(displays))
	for _, display := range displays {
		if display.ChatMessageID != nil {
			if _, ok := seen[*display.ChatMessageID]; !ok {
				seen[*display.ChatMessageID] = struct{}{}
				ids = append(ids, *display.ChatMessageID)
			}
		}
		if display.TurnID != nil {
			if _, ok := seenTurns[*display.TurnID]; !ok {
				seenTurns[*display.TurnID] = struct{}{}
				turnIDs = append(turnIDs, *display.TurnID)
			}
		}
	}
	if len(ids) == 0 && len(turnIDs) == 0 {
		return nil
	}
	var chats []ChatMessage
	query := r.db.WithContext(ctx).Where("invalidated_at IS NULL")
	switch {
	case len(ids) > 0 && len(turnIDs) > 0:
		query = query.Where("id IN ? OR turn_id IN ?", ids, turnIDs)
	case len(ids) > 0:
		query = query.Where("id IN ?", ids)
	default:
		query = query.Where("turn_id IN ?", turnIDs)
	}
	if err := query.Order("message_seq ASC").Find(&chats).Error; err != nil {
		return err
	}
	byID := make(map[uuid.UUID]ChatMessage, len(chats))
	byTurn := make(map[uuid.UUID][]ChatMessage, len(turnIDs))
	for _, chat := range chats {
		byID[chat.ID] = chat
		if chat.TurnID != nil {
			byTurn[*chat.TurnID] = append(byTurn[*chat.TurnID], chat)
		}
	}
	for index := range displays {
		var linked *ChatMessage
		if displays[index].ChatMessageID != nil {
			if chat, ok := byID[*displays[index].ChatMessageID]; ok {
				linked = &chat
			}
		}
		if linked == nil {
			continue
		}
		if linked.TurnID != nil {
			var parts []string
			for _, chat := range byTurn[*linked.TurnID] {
				if chat.Role != RoleAssistant || (linked.MessageSeq > 0 && chat.MessageSeq > linked.MessageSeq) {
					continue
				}
				appendReasoningTextPart(&parts, ReasoningTextBlocks(chat.ContentBlocks))
			}
			displays[index].Reasoning = strings.Join(parts, "\n")
			if displays[index].Reasoning == "" {
				displays[index].Reasoning = ReasoningTextBlocks(linked.ContentBlocks)
			}
		} else {
			displays[index].Reasoning = ReasoningTextBlocks(linked.ContentBlocks)
		}
	}
	return nil
}

// RewriteIdempotencyKey deterministically identifies one edit intent. A
// different edited body is a new operation; transport retries of the same
// message/body pair reuse the reward ledger entry.
func RewriteIdempotencyKey(messageID uuid.UUID, content string) string {
	sum := sha256.Sum256([]byte(content))
	return "message_rewrite:" + messageID.String() + ":" + hex.EncodeToString(sum[:])
}

func RewriteRequestIdempotencyKey(requestID uuid.UUID) string {
	return "message_rewrite_request:" + requestID.String()
}
