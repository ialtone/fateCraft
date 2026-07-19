package protocol

import (
	"strings"
	"testing"
)

// feedAll 模拟流式：按给定切分逐步累积输入，校验输出单调递增，返回最终结果。
func feedAll(t *testing.T, chunks []string) string {
	t.Helper()
	var raw strings.Builder
	last := ""
	for _, ch := range chunks {
		raw.WriteString(ch)
		cur := ExtractPartialNarrative(raw.String())
		if !strings.HasPrefix(cur, last) {
			t.Fatalf("输出不单调：上一次 %q，本次 %q", last, cur)
		}
		last = cur
	}
	return last
}

// 在每个字节边界切分，逐字节喂入，验证最终结果与单调性。
func feedByteWise(t *testing.T, full string) string {
	t.Helper()
	chunks := make([]string, 0, len(full))
	for i := 0; i < len(full); i++ {
		chunks = append(chunks, full[i:i+1])
	}
	return feedAll(t, chunks)
}

// 标准信封：narrative 在前，state_update 在后。
func TestPartial_BasicEnvelope(t *testing.T) {
	full := `{"narrative": "你推开木门，茶香扑面。", "state_update": {"location.name": "命运茶馆"}}`
	got := feedByteWise(t, full)
	want := "你推开木门，茶香扑面。"
	if got != want {
		t.Fatalf("期望 %q，得到 %q", want, got)
	}
}

// 转义字符被切在增量边界上（\n、\"、\\ 逐字节喂入）。
func TestPartial_EscapesAcrossChunks(t *testing.T) {
	full := `{"narrative":"第一行\n他说：\"留步\\停\""}`
	got := feedByteWise(t, full)
	want := "第一行\n他说：\"留步\\停\""
	if got != want {
		t.Fatalf("期望 %q，得到 %q", want, got)
	}
}

// JSON 的 \uXXXX 转义与代理对（emoji）被切在增量边界上。
func TestPartial_UnicodeEscape(t *testing.T) {
	// 构造 {"narrative":"A茶🍵B"}（茶 + 🍵 的转义形式）
	esc := "\\u8336" + "\\ud83c" + "\\udf75"
	full := `{"narrative":"A` + esc + `B"}`
	got := feedByteWise(t, full)
	want := "A茶🍵B"
	if got != want {
		t.Fatalf("期望 %q，得到 %q", want, got)
	}
}

// narrative 不是第一个键：前面有嵌套对象也不能误判。
func TestPartial_NarrativeNotFirst(t *testing.T) {
	full := `{"state_update":{"profile.title":"narrative"},"notes":"x","narrative":"正文在此"}`
	got := feedByteWise(t, full)
	if got != "正文在此" {
		t.Fatalf("期望 %q，得到 %q", "正文在此", got)
	}
}

// "narrative" 作为其它键的值出现时不能触发。
func TestPartial_NarrativeAsValue(t *testing.T) {
	full := `{"notes":"narrative","narrative":"真正的剧情"}`
	got := feedByteWise(t, full)
	if got != "真正的剧情" {
		t.Fatalf("期望 %q，得到 %q", "真正的剧情", got)
	}
}

// ```json 围栏包裹：围栏行未写完时输出为空，写完后正常提取。
func TestPartial_CodeFence(t *testing.T) {
	full := "```json\n{\"narrative\":\"围栏内剧情\"}\n```"
	got := feedByteWise(t, full)
	if got != "围栏内剧情" {
		t.Fatalf("期望 %q，得到 %q", "围栏内剧情", got)
	}
}

// 模型没按约定输出 JSON：整段纯文本原样透传（与 ParseEnvelope 降级一致）。
func TestPartial_PlainTextFallback(t *testing.T) {
	full := "茶馆的木门吱呀一声开了。"
	got := feedByteWise(t, full)
	if got != full {
		t.Fatalf("期望透传 %q，得到 %q", full, got)
	}
}

// JSON 未闭合（仍在生成中）：返回 narrative 已生成前缀。
func TestPartial_UnclosedJSON(t *testing.T) {
	got := ExtractPartialNarrative(`{"narrative":"故事讲到一半`)
	if got != "故事讲到一半" {
		t.Fatalf("期望 %q，得到 %q", "故事讲到一半", got)
	}
}

// narrative 键还没出现时输出为空。
func TestPartial_KeyNotYetSeen(t *testing.T) {
	if got := ExtractPartialNarrative(`{"state_up`); got != "" {
		t.Fatalf("键未出现时应为空，得到 %q", got)
	}
	if got := ExtractPartialNarrative(`{"narrat`); got != "" {
		t.Fatalf("键名未写完时应为空，得到 %q", got)
	}
}
