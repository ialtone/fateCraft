package filestore

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

// Slugify 把名称转成文件名友好的 slug：小写、仅保留字母数字，其余折叠为连字符。
// 非 ASCII 名称（如中文）会得到空串，由 GenerateSlug 负责兜底。
func Slugify(name string) string {
	var b strings.Builder
	lastDash := true // 抑制开头的连字符
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// GenerateSlug 由名称生成不与现有冲突的 slug：
//   - 名称可转 slug 时直接使用，冲突则追加 -2、-3…；
//   - 名称无法转 slug（如纯中文）时用 <prefix>-<短随机码> 兜底。
func GenerateSlug(prefix, name string, exists func(string) bool) string {
	base := Slugify(name)
	if base == "" {
		for {
			id := prefix + "-" + randomCode()
			if !exists(id) {
				return id
			}
		}
	}
	if !exists(base) {
		return base
	}
	for i := 2; ; i++ {
		id := fmt.Sprintf("%s-%d", base, i)
		if !exists(id) {
			return id
		}
	}
}

// randomCode 返回 6 位十六进制随机码。
func randomCode() string {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 失败几乎不可能发生，兜底为固定值并靠 exists 循环换码。
		return "000000"
	}
	return hex.EncodeToString(b)
}
