package chat

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/storage"
)

// mockMessageRepo 是 messageRepo 接口的内存 mock，用于脱离 Postgres 单测 chat.Service。
// 只实现被测路径用到的方法，其余返回零值即可（接口化后才可能这样做）。
type mockMessageRepo struct {
	chats    map[uuid.UUID]*storage.ChatMessage
	displays map[uuid.UUID]*storage.DisplayMessage
	lastAsst *storage.ChatMessage
}

func newMockMessageRepo() *mockMessageRepo {
	return &mockMessageRepo{
		chats:    map[uuid.UUID]*storage.ChatMessage{},
		displays: map[uuid.UUID]*storage.DisplayMessage{},
	}
}

func (m *mockMessageRepo) CreateChat(ctx context.Context, msg *storage.ChatMessage) error {
	m.chats[msg.ID] = msg
	if msg.Role == storage.RoleAssistant {
		m.lastAsst = msg
	}
	return nil
}

func (m *mockMessageRepo) GetChat(ctx context.Context, convID, id uuid.UUID) (*storage.ChatMessage, error) {
	if c, ok := m.chats[id]; ok && c.ConversationID == convID {
		return c, nil
	}
	return nil, storage.ErrNotFound
}

func (m *mockMessageRepo) UpdateChat(ctx context.Context, msg *storage.ChatMessage) error {
	m.chats[msg.ID] = msg
	return nil
}

func (m *mockMessageRepo) DeleteChat(ctx context.Context, convID, id uuid.UUID) error {
	delete(m.chats, id)
	return nil
}

func (m *mockMessageRepo) ListChat(ctx context.Context, convID uuid.UUID, includeHidden bool) ([]storage.ChatMessage, error) {
	var out []storage.ChatMessage
	for _, c := range m.chats {
		if c.ConversationID == convID && (includeHidden || !c.Hidden) {
			out = append(out, *c)
		}
	}
	return out, nil
}

func (m *mockMessageRepo) LastAssistant(ctx context.Context, convID uuid.UUID) (*storage.ChatMessage, error) {
	if m.lastAsst == nil {
		return nil, storage.ErrNotFound
	}
	return m.lastAsst, nil
}

func (m *mockMessageRepo) CreateDisplay(ctx context.Context, msg *storage.DisplayMessage) error {
	m.displays[msg.ID] = msg
	return nil
}

func (m *mockMessageRepo) GetDisplay(ctx context.Context, convID, id uuid.UUID) (*storage.DisplayMessage, error) {
	if d, ok := m.displays[id]; ok && d.ConversationID == convID {
		return d, nil
	}
	return nil, storage.ErrNotFound
}

func (m *mockMessageRepo) UpdateDisplay(ctx context.Context, msg *storage.DisplayMessage) error {
	m.displays[msg.ID] = msg
	return nil
}

func (m *mockMessageRepo) DeleteDisplay(ctx context.Context, convID, id uuid.UUID) error {
	delete(m.displays, id)
	return nil
}

func (m *mockMessageRepo) DeleteDisplayByChatID(ctx context.Context, convID, chatID uuid.UUID) error {
	for id, d := range m.displays {
		if d.ChatMessageID != nil && *d.ChatMessageID == chatID {
			delete(m.displays, id)
		}
	}
	return nil
}

func (m *mockMessageRepo) ListDisplay(ctx context.Context, convID uuid.UUID, before *uuid.UUID, limit int) ([]storage.DisplayMessage, error) {
	var out []storage.DisplayMessage
	for _, d := range m.displays {
		if d.ConversationID == convID {
			out = append(out, *d)
		}
	}
	return out, nil
}

// 编译期断言：mock 满足消费端接口。
var _ messageRepo = (*mockMessageRepo)(nil)

// TestAppendUser_WritesChatAndDisplay 验证 AppendUser 同时写入内部消息与展示消息，
// 且展示消息正确回指内部消息 id——全程无需 Postgres（接口化带来的可测性）。
func TestAppendUser_WritesChatAndDisplay(t *testing.T) {
	repo := newMockMessageRepo()
	svc := NewService(repo)
	convID := uuid.New()

	chatMsg, disp, err := svc.AppendUser(context.Background(), convID, "你好", nil)
	if err != nil {
		t.Fatalf("AppendUser 返回错误: %v", err)
	}
	if len(repo.chats) != 1 || len(repo.displays) != 1 {
		t.Fatalf("期望写入 1 条内部消息与 1 条展示消息，实际 chats=%d displays=%d", len(repo.chats), len(repo.displays))
	}
	if chatMsg.Role != storage.RoleUser || chatMsg.Content != "你好" {
		t.Errorf("内部消息角色/内容不符: role=%s content=%q", chatMsg.Role, chatMsg.Content)
	}
	if disp.ChatMessageID == nil || *disp.ChatMessageID != chatMsg.ID {
		t.Errorf("展示消息未正确回指内部消息 id")
	}
}

// TestDropLastAssistant 验证删除最近一条 assistant 消息会同时清除其内部消息与展示消息。
func TestDropLastAssistant(t *testing.T) {
	repo := newMockMessageRepo()
	svc := NewService(repo)
	convID := uuid.New()

	if _, _, err := svc.AppendAssistant(context.Background(), convID, "内容", "markdown", nil, nil); err != nil {
		t.Fatalf("AppendAssistant 返回错误: %v", err)
	}
	if err := svc.DropLastAssistant(context.Background(), convID); err != nil {
		t.Fatalf("DropLastAssistant 返回错误: %v", err)
	}
	if len(repo.chats) != 0 || len(repo.displays) != 0 {
		t.Fatalf("期望删除后无残留，实际 chats=%d displays=%d", len(repo.chats), len(repo.displays))
	}
}

// TestDropLastAssistant_NotFound 验证无 assistant 消息时返回 storage.ErrNotFound。
func TestDropLastAssistant_NotFound(t *testing.T) {
	repo := newMockMessageRepo()
	svc := NewService(repo)
	if err := svc.DropLastAssistant(context.Background(), uuid.New()); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("期望 ErrNotFound，实际 %v", err)
	}
}

// TestAppendSystemChatEvent 验证系统事件同时落 chat 消息（进上下文）与展示消息。
// 这是判定结果能进入第二阶段模型上下文的关键保证（此前只落展示消息导致模型看不到判定输赢）。
func TestAppendSystemChatEvent(t *testing.T) {
	repo := newMockMessageRepo()
	svc := NewService(repo)
	convID := uuid.New()

	chatMsg, disp, err := svc.AppendSystemChatEvent(context.Background(), convID,
		"roll_result", "判定成功（上下文版）", "判定成功", map[string]any{"roll_type": "破阵"})
	if err != nil {
		t.Fatalf("AppendSystemChatEvent: %v", err)
	}

	// chat 消息：system 角色、内容为上下文文本，History 能取到（进入下一轮上下文）。
	if chatMsg.Role != storage.RoleSystem || chatMsg.Content != "判定成功（上下文版）" {
		t.Fatalf("chat 消息不符: %+v", chatMsg)
	}
	hist, err := svc.History(context.Background(), convID, false)
	if err != nil || len(hist) != 1 {
		t.Fatalf("History 应含 1 条判定结果, got %d, err=%v", len(hist), err)
	}

	// 展示消息：类型 roll_result、关联 chat 消息、markdown 为展示文本。
	if disp.Type != "roll_result" || disp.Markdown != "判定成功" {
		t.Fatalf("展示消息不符: %+v", disp)
	}
	if disp.ChatMessageID == nil || *disp.ChatMessageID != chatMsg.ID {
		t.Fatalf("展示消息应关联 chat 消息: %+v", disp.ChatMessageID)
	}
}

// TestUpdateDisplayMetadata 验证 metadata 回写落库（applied_state 刷新后不再丢失）。
func TestUpdateDisplayMetadata(t *testing.T) {
	repo := newMockMessageRepo()
	svc := NewService(repo)
	convID := uuid.New()

	_, disp, err := svc.AppendAssistant(context.Background(), convID, "剧情", "剧情", nil, nil)
	if err != nil {
		t.Fatalf("AppendAssistant: %v", err)
	}
	if err := svc.UpdateDisplayMetadata(context.Background(), convID, disp.ID,
		map[string]any{"applied_state": map[string]any{"hp": 1}}); err != nil {
		t.Fatalf("UpdateDisplayMetadata: %v", err)
	}
	got := repo.displays[disp.ID]
	if got == nil || got.Metadata["applied_state"] == nil {
		t.Fatalf("metadata 未落库: %+v", got)
	}
}
