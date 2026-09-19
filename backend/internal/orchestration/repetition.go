package orchestration

import (
	"unicode"

	"github.com/ialtone/fatecraft/internal/storage"
)

const (
	// 参与重复检测的最近 assistant 叙事条数。
	repetitionHistoryWindow = 6
	// 归一化（去空白）后短于该 rune 数的文本不检测，避免误伤简短承接语。
	repetitionMinRunes = 60
	// 候选文本 n-gram 被某条历史叙事覆盖的比例达到该值即判为复读。
	repetitionContainmentThreshold = 0.85
	repetitionGramRunes            = 10
)

// isRepetitiveNarrative 判断候选最终叙事是否几乎逐字复读了最近的 assistant
// 叙事。沿用少量句式不会触发；拦截目标是把旧回复整段照抄的退化输出——
// 无论来自哪个模型或网关，这类输出都不应作为新回合落库，与其他协议违规
// 一样一次拒绝，由玩家决定重新生成或调整输入。
func isRepetitiveNarrative(candidate string, history []storage.ChatMessage) bool {
	grams := narrativeGrams(candidate)
	if len(grams) == 0 {
		return false
	}
	seen := 0
	for index := len(history) - 1; index >= 0 && seen < repetitionHistoryWindow; index-- {
		message := history[index]
		if message.Role != storage.RoleAssistant || message.Kind != "text" {
			continue
		}
		seen++
		previous := narrativeGrams(message.Content)
		if len(previous) == 0 {
			continue
		}
		matched := 0
		for gram := range grams {
			if _, ok := previous[gram]; ok {
				matched++
			}
		}
		if float64(matched) >= repetitionContainmentThreshold*float64(len(grams)) {
			return true
		}
	}
	return false
}

// narrativeGrams 把叙事文本去空白归一化后切成定长 rune n-gram 集合；
// 归一化后不足检测下限时返回空集合，表示跳过检测。
func narrativeGrams(text string) map[string]struct{} {
	compact := make([]rune, 0, len(text))
	for _, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		compact = append(compact, r)
	}
	if len(compact) < repetitionMinRunes {
		return nil
	}
	grams := make(map[string]struct{}, len(compact))
	for i := 0; i+repetitionGramRunes <= len(compact); i++ {
		grams[string(compact[i:i+repetitionGramRunes])] = struct{}{}
	}
	return grams
}
