package worldbook

import (
	"sort"
	"strings"

	"github.com/ialtone/fatecraft/internal/storage"
	"github.com/ialtone/fatecraft/internal/tokenizer"
)

// Activated 表示一个被激活并准备注入上下文的世界书条目。
type Activated struct {
	Entry  storage.WorldBookEntry
	Tokens int
	Reason string // constant 或 keyword
}

// Scan 在给定预算内激活世界书条目（doc 09 MVP 算法）。
//
//   - 常驻条目（constant）优先尝试注入；
//   - 关键词条目：scanText 命中任一关键词才参与；
//   - 二者合并后按 priority 升序排序（数值小优先）；
//   - 逐个累加 token，超出 budget 的条目跳过并记录在 skipped 中。
func Scan(entries []storage.WorldBookEntry, scanText string, budget int, counter tokenizer.Counter) (activated []Activated, skipped []storage.WorldBookEntry) {
	lowered := strings.ToLower(scanText)

	type candidate struct {
		entry  storage.WorldBookEntry
		tokens int
		reason string
	}
	var candidates []candidate
	for _, e := range entries {
		if !e.Enabled {
			continue
		}
		reason := ""
		if e.Constant {
			reason = "constant"
		} else if matchKeywords(lowered, e.Keywords) {
			reason = "keyword"
		}
		if reason == "" {
			continue
		}
		tokens := e.TokenCount
		var t int
		if tokens != nil {
			t = *tokens
		} else {
			t = counter.Count(e.Title + "\n" + e.Content)
		}
		candidates = append(candidates, candidate{entry: e, tokens: t, reason: reason})
	}

	// 常驻条目优先，其次按 priority 升序，最后按 title 稳定排序。
	sort.SliceStable(candidates, func(i, j int) bool {
		ci, cj := candidates[i], candidates[j]
		if ci.entry.Constant != cj.entry.Constant {
			return ci.entry.Constant
		}
		if ci.entry.Priority != cj.entry.Priority {
			return ci.entry.Priority < cj.entry.Priority
		}
		return ci.entry.Title < cj.entry.Title
	})

	used := 0
	for _, c := range candidates {
		if used+c.tokens <= budget {
			activated = append(activated, Activated{Entry: c.entry, Tokens: c.tokens, Reason: c.reason})
			used += c.tokens
		} else {
			skipped = append(skipped, c.entry)
		}
	}
	return activated, skipped
}

// matchKeywords 判断 scanText（已转小写）是否命中任一关键词。
func matchKeywords(loweredText string, keywords []string) bool {
	for _, kw := range keywords {
		kw = strings.TrimSpace(strings.ToLower(kw))
		if kw == "" {
			continue
		}
		if strings.Contains(loweredText, kw) {
			return true
		}
	}
	return false
}
