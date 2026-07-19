package state

import "testing"

func TestAllowedPath(t *testing.T) {
	cases := map[string]bool{
		"profile.title":    true,
		"attributes.hp":    true,
		"inventory.items":  true,
		"inventory.items+": true,
		"location.city":    true,
		"world_flags.met":  true,
		"quests":           true,
		"quests+":          true,
		"quests.foo":       false, // 数组不允许嵌套路径
		"risks":            true,
		"player_id":        false,
		"system_flags.x":   false,
		"":                 false,
	}
	for path, want := range cases {
		if got := AllowedPath(path); got != want {
			t.Errorf("AllowedPath(%q) = %v, 期望 %v", path, got, want)
		}
	}
}

func TestApplyPatch(t *testing.T) {
	base := DefaultState()
	patch := map[string]any{
		"profile.title":    "勇者",
		"inventory.items+": "剑",
		"world_flags.met":  true,
		"player_id":        "hack", // 应被跳过
	}
	result, applied, skipped := ApplyPatch(base, patch)

	profile := result["profile"].(map[string]any)
	if profile["title"] != "勇者" {
		t.Errorf("profile.title = %v, 期望 勇者", profile["title"])
	}
	inv := result["inventory"].(map[string]any)
	items := inv["items"].([]any)
	if len(items) != 1 || items[0] != "剑" {
		t.Errorf("inventory.items = %v, 期望 [剑]", items)
	}
	if _, ok := applied["player_id"]; ok {
		t.Error("player_id 不应被应用")
	}
	if len(skipped) != 1 || skipped[0] != "player_id" {
		t.Errorf("skipped = %v, 期望 [player_id]", skipped)
	}

	// 原始状态不应被修改。
	if items0 := base["inventory"].(map[string]any)["items"].([]any); len(items0) != 0 {
		t.Error("ApplyPatch 不应修改入参 state")
	}
}
