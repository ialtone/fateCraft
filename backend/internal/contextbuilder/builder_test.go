package contextbuilder

import (
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/storage"
)

// runeCounter 用每个字符计 1 个 token，便于在测试中做确定性预算计算。
type runeCounter struct{}

func (runeCounter) Count(s string) int { return utf8.RuneCountInString(s) }

func newTestState() map[string]any {
	return map[string]any{"profile": map[string]any{}}
}

// 必选段超预算时应返回 ErrRequiredOverflow 且不构造消息。
func TestBuild_RequiredOverflow(t *testing.T) {
	in := Input{
		MaxContextTokens:       300, // 预算 = 300 - 0 - 256 = 44
		ReservedResponseTokens: 0,
		PresetSegments: []storage.PromptSegment{
			{Key: "system_master", Role: "system", Content: longString(100), Enabled: true, Required: true},
		},
		State: newTestState(),
	}
	_, err := Build(in, runeCounter{})
	if err == nil {
		t.Fatal("期望 ErrRequiredOverflow，得到 nil")
	}
}

// 最近历史应按时间正序出现在消息末尾，且 IncludedMessageIDs 同序。
func TestBuild_HistoryChronologicalOrder(t *testing.T) {
	h1, h2, h3 := uuid.New(), uuid.New(), uuid.New()
	in := Input{
		MaxContextTokens:       10000,
		ReservedResponseTokens: 0,
		PresetSegments: []storage.PromptSegment{
			{Key: "system_master", Role: "system", Content: "sys", Enabled: true, Required: true},
		},
		State: newTestState(),
		History: []storage.ChatMessage{
			{ID: h1, Role: "user", Content: "first"},
			{ID: h2, Role: "assistant", Content: "second"},
			{ID: h3, Role: "user", Content: "third"},
		},
	}
	res, err := Build(in, runeCounter{})
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	// 末三条消息应为 first/second/third。
	got := res.Messages[len(res.Messages)-3:]
	want := []string{"first", "second", "third"}
	for i, m := range got {
		if m.Content != want[i] {
			t.Fatalf("历史顺序错误，位置 %d 期望 %q 得到 %q", i, want[i], m.Content)
		}
	}
	if len(res.IncludedMessageIDs) != 3 || res.IncludedMessageIDs[0] != h1 || res.IncludedMessageIDs[2] != h3 {
		t.Fatalf("IncludedMessageIDs 顺序错误: %v", res.IncludedMessageIDs)
	}
}

// 预算紧张时只应纳入最新的历史消息。
func TestBuild_HistoryBudgetTrim(t *testing.T) {
	h1, h2 := uuid.New(), uuid.New()
	// 必选段固定占 36 token（system_master 1 + current_state 渲染 35）；
	// 预算 50 → 剩余 14，只够装下最新的 "tiny"（4），装不下 40 字符的长消息。
	in := Input{
		MaxContextTokens:       256 + 50, // 预算 = 50
		ReservedResponseTokens: 0,
		PresetSegments: []storage.PromptSegment{
			{Key: "system_master", Role: "system", Content: "s", Enabled: true, Required: true},
		},
		State: map[string]any{}, // 渲染后较短
		History: []storage.ChatMessage{
			{ID: h1, Role: "user", Content: longString(40)}, // 超出剩余预算
			{ID: h2, Role: "user", Content: "tiny"},
		},
	}
	res, err := Build(in, runeCounter{})
	if err != nil {
		t.Fatalf("意外错误: %v", err)
	}
	for _, id := range res.IncludedMessageIDs {
		if id == h1 {
			t.Fatal("预算紧张时不应纳入较大的历史消息 h1")
		}
	}
}

func longString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'x'
	}
	return string(b)
}
