package tokenizer

import "unicode"

// Counter 用于估算文本的 token 数。可替换为真正的 BPE 实现。
type Counter interface {
	Count(text string) int
}

// Heuristic 是一个轻量的、对中文友好的 token 估算器。
//
// 规则（粗略近似，无需下载 BPE 词表）：
//   - CJK 表意文字按每字约 1 个 token 计；
//   - 其余字符（拉丁字母、数字、标点、空白）按每约 4 个字符 1 个 token 计。
//
// 该估算偏保守，足以做预算控制；需要精确计费时可替换实现。
type Heuristic struct{}

func NewHeuristic() Heuristic { return Heuristic{} }

func (Heuristic) Count(text string) int {
	if text == "" {
		return 0
	}
	cjk := 0
	other := 0
	for _, r := range text {
		if isCJK(r) {
			cjk++
		} else {
			other++
		}
	}
	tokens := cjk + (other+3)/4 // 其余字符向上取整
	if tokens < 1 {
		tokens = 1
	}
	return tokens
}

// isCJK 判断是否为需要按字计费的中日韩表意文字。
func isCJK(r rune) bool {
	switch {
	case unicode.Is(unicode.Han, r):
		return true
	case unicode.Is(unicode.Hiragana, r):
		return true
	case unicode.Is(unicode.Katakana, r):
		return true
	case unicode.Is(unicode.Hangul, r):
		return true
	default:
		return false
	}
}
