package orchestration

import (
	"strings"
	"testing"

	"github.com/ialtone/fatecraft/internal/storage"
)

func narrativeHistory(contents ...string) []storage.ChatMessage {
	out := make([]storage.ChatMessage, 0, len(contents))
	for _, content := range contents {
		out = append(out, storage.ChatMessage{Role: storage.RoleAssistant, Kind: "text", Content: content})
	}
	return out
}

func TestIsRepetitiveNarrative(t *testing.T) {
	previous := "老僧不慌不忙，将一枚古朴的玉简递到汝掌心：剑气入体，初窥门径。玉简温润如玉，剑气初醒，似有万剑归宗之势，助汝凡人初成。老僧拂袖而去，渐行渐远。你可继续本世修行，或寻觅灵石，或进寺拜见长辈，抑或闭关自省。"

	t.Run("verbatim copy of a recent reply is rejected", func(t *testing.T) {
		if !isRepetitiveNarrative(previous, narrativeHistory("开场叙事，与后文无关。", previous)) {
			t.Fatal("identical narrative should be flagged")
		}
	})

	t.Run("copy with minor suffix is rejected", func(t *testing.T) {
		candidate := previous + "路口仍有方向待抉择。"
		if !isRepetitiveNarrative(candidate, narrativeHistory(previous)) {
			t.Fatal("near-identical narrative should be flagged")
		}
	})

	t.Run("fresh narrative reusing one closing sentence passes", func(t *testing.T) {
		candidate := "山道尽头忽起雷声，木剑在鞘中轻鸣。你循声而上，见崖壁裂开一线幽光，其中悬着半卷残破剑谱，笔意凌厉如未出鞘之锋。守谱的石俑缓缓转头，眼窝中燃起两点青火。你可继续本世修行，或寻觅灵石，或进寺拜见长辈，抑或闭关自省。"
		if isRepetitiveNarrative(candidate, narrativeHistory(previous)) {
			t.Fatal("mostly new narrative must not be flagged")
		}
	})

	t.Run("short acknowledgement is skipped", func(t *testing.T) {
		if isRepetitiveNarrative("你稍作歇息。", narrativeHistory("你稍作歇息。")) {
			t.Fatal("short text must skip the guard")
		}
	})

	t.Run("duplicate outside the window passes", func(t *testing.T) {
		history := narrativeHistory(previous)
		for i := 0; i < repetitionHistoryWindow; i++ {
			history = append(history, narrativeHistory("第"+strings.Repeat("很", 70)+"回合的全新叙事内容"+strings.Repeat("啊", i+1))...)
		}
		if isRepetitiveNarrative(previous, history) {
			t.Fatal("duplicates older than the window must not be flagged")
		}
	})

	t.Run("non-assistant rows are ignored", func(t *testing.T) {
		history := []storage.ChatMessage{{Role: storage.RoleUser, Kind: "text", Content: previous}}
		if isRepetitiveNarrative(previous, history) {
			t.Fatal("user rows must not participate in the guard")
		}
	})
}
