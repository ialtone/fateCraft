package storage

import "testing"

func TestVisibleInquiryRulesRequiresExplicitPublicMarker(t *testing.T) {
	books := []WorldBook{{Entries: []WorldBookEntry{
		{Title: "公开规则", Content: "玩家可以询问", Enabled: true, Priority: 20, Metadata: map[string]any{"inquiry_visible": true}},
		{Title: "高优先级规则", Content: "应排在前面", Enabled: true, Priority: 10, Metadata: map[string]any{"inquiry_visible": true}},
		{Title: "模型规则", Content: "不能泄露", Enabled: true, Priority: 1, Metadata: map[string]any{}},
		{Title: "已禁用规则", Content: "不能使用", Enabled: false, Priority: 1, Metadata: map[string]any{"inquiry_visible": true}},
	}}}
	rules := VisibleInquiryRules(books)
	if len(rules) != 2 || rules[0].Title != "高优先级规则" || rules[1].Title != "公开规则" {
		t.Fatalf("visible rules = %#v", rules)
	}
}
