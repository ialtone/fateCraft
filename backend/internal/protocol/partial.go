package protocol

import (
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// ExtractPartialNarrative 从可能不完整的模型累积输出中，尽力提取 narrative 字段已生成的部分。
//
// 设计参照 SillyTavern 的流式处理（StreamingProcessor.onProgressStreaming）：
// 每个增量到达后都对「累积全文」重新清洗一遍，而不是直接转发原始增量——
// 这样转义符、键名被切在增量边界上的所有情况都自然消解。
// 返回值随输入增长单调递增（只会在尾部追加），调用方据此计算可见增量。
//
// 规则：
//   - 整体不是 JSON（忽略 ``` 围栏后首个非空白字符不是 '{'）→ 原样透传（与
//     ParseEnvelope 的降级行为一致：整段文本当作剧情）；
//   - 是 JSON → 只返回顶层 "narrative" 字符串值的已解码前缀；
//   - 尚无法判定（围栏行未写完 / narrative 键还没出现）→ 返回空串，等待更多输入。
func ExtractPartialNarrative(raw string) string {
	s := strings.TrimLeft(raw, " \t\r\n")
	if s == "" {
		return ""
	}
	// ``` 围栏：等围栏行结束再判定内部内容。
	if s[0] == '`' {
		if !strings.HasPrefix(s, "```") {
			if len(s) < 3 {
				return "" // 可能是围栏前缀（` 或 ``），等待
			}
			return raw // 反引号开头但不是围栏，按纯文本透传
		}
		nl := strings.IndexByte(s, '\n')
		if nl < 0 {
			return "" // 围栏行（```json）还没换行，等待
		}
		s = strings.TrimLeft(s[nl+1:], " \t\r\n")
		if s == "" {
			return ""
		}
	}
	if s[0] != '{' {
		return raw // 纯文本降级：原样透传
	}
	return scanNarrative(s)
}

// scanNarrative 在（可能不完整的）JSON 文本中定位顶层 "narrative" 键，
// 返回其字符串值的已解码前缀；narrative 值已闭合时返回完整值。
func scanNarrative(s string) string {
	var (
		out        strings.Builder // narrative 的解码结果
		curStr     strings.Builder // 当前非 narrative 字符串的解码结果（用于识别键名）
		inString   bool
		inNarr     bool   // 当前字符串就是 narrative 的值
		pendingKey string // depth==1 下刚结束的字符串（候选键名）
		afterColon bool   // pendingKey 之后已出现冒号，下一个值即键的值
		depth      int
	)

	writeByte := func(c byte) {
		if inNarr {
			out.WriteByte(c)
		} else {
			curStr.WriteByte(c)
		}
	}
	writeRune := func(r rune) {
		if inNarr {
			out.WriteRune(r)
		} else {
			curStr.WriteRune(r)
		}
	}

	i := 0
	for i < len(s) {
		c := s[i]

		if inString {
			switch c {
			case '\\':
				// 解码转义；尾部不完整时直接返回已有前缀，等待更多输入。
				if i+1 >= len(s) {
					return out.String()
				}
				switch e := s[i+1]; e {
				case '"', '\\', '/':
					writeByte(e)
					i += 2
				case 'n':
					writeByte('\n')
					i += 2
				case 't':
					writeByte('\t')
					i += 2
				case 'r':
					writeByte('\r')
					i += 2
				case 'b':
					writeByte('\b')
					i += 2
				case 'f':
					writeByte('\f')
					i += 2
				case 'u':
					r, size, ok := decodeUnicodeEscape(s[i:])
					if !ok {
						return out.String() // \uXXXX（或代理对）未写完，等待
					}
					writeRune(r)
					i += size
				default:
					// 非法转义，原样保留两个字符，尽量不丢内容。
					writeByte('\\')
					writeByte(e)
					i += 2
				}
			case '"':
				if inNarr {
					return out.String() // narrative 值闭合，完整返回
				}
				inString = false
				if depth == 1 {
					pendingKey = curStr.String()
					afterColon = false
				}
				i++
			default:
				writeByte(c)
				i++
			}
			continue
		}

		switch c {
		case '"':
			inString = true
			curStr.Reset()
			inNarr = depth == 1 && afterColon && pendingKey == "narrative"
			i++
		case ':':
			if depth == 1 && pendingKey != "" {
				afterColon = true
			}
			i++
		case '{', '[':
			depth++
			pendingKey = ""
			afterColon = false
			i++
		case '}', ']':
			depth--
			i++
		case ',':
			if depth == 1 {
				pendingKey = ""
				afterColon = false
			}
			i++
		default:
			i++
		}
	}
	// 输入耗尽（JSON 未闭合）：返回 narrative 已解码前缀。
	return out.String()
}

// decodeUnicodeEscape 解码 s 开头的 \uXXXX（含代理对 \uXXXX\uXXXX）。
// 返回解码出的 rune 与消费的字节数；序列不完整时 ok=false。
func decodeUnicodeEscape(s string) (r rune, size int, ok bool) {
	if len(s) < 6 {
		return 0, 0, false
	}
	hi, valid := parseHex4(s[2:6])
	if !valid {
		return utf8.RuneError, 6, true // 非法十六进制，按替换符消费
	}
	if !utf16.IsSurrogate(rune(hi)) {
		return rune(hi), 6, true
	}
	// 高代理：需要后续 \uXXXX 组成代理对。
	if len(s) < 12 {
		return 0, 0, false // 代理对未写完，等待
	}
	if s[6] != '\\' || s[7] != 'u' {
		return utf8.RuneError, 6, true
	}
	lo, valid := parseHex4(s[8:12])
	if !valid {
		return utf8.RuneError, 6, true
	}
	combined := utf16.DecodeRune(rune(hi), rune(lo))
	return combined, 12, true
}

func parseHex4(s string) (int, bool) {
	v := 0
	for k := 0; k < 4; k++ {
		c := s[k]
		v <<= 4
		switch {
		case c >= '0' && c <= '9':
			v |= int(c - '0')
		case c >= 'a' && c <= 'f':
			v |= int(c-'a') + 10
		case c >= 'A' && c <= 'F':
			v |= int(c-'A') + 10
		default:
			return 0, false
		}
	}
	return v, true
}
