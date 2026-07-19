package state

import (
	"encoding/json"
	"strings"
)

// DefaultState 返回一个新对话的默认结构化状态（doc 03）。
func DefaultState() map[string]any {
	return map[string]any{
		"profile": map[string]any{
			"name":                 "",
			"cultivation_level":    "凡人",
			"cultivation_stage":    "",
			"cultivation_progress": 0,
			"age":                  18,
			"lifespan":             80,
			"remaining_lifespan":   80,
			"title":                "",
		},
		"attributes":    map[string]any{},
		"traits":        []any{},
		"inventory":     map[string]any{"items": []any{}},
		"relationships": map[string]any{},
		"location":      map[string]any{},
		"quests":        []any{},
		"risks":         []any{},
		"world_flags":   map[string]any{},
		"cultivation": map[string]any{
			"breakthrough_attempts": 0,
			"failed_breakthroughs":  0,
			"talent_awakened":       false,
			"cultivation_method":    "",
		},
		"ascension": map[string]any{
			"void_rift_discovered":    false,
			"ascension_token_owned":   false,
			"tribulation_preparation": 0,
			"karma_debt":              0,
		},
	}
}

// allowedTopLevels 是允许更新的顶层命名空间（doc 06 §状态更新校验）。
// nested=true 表示允许其下任意嵌套路径；false 表示只能更新该数组本身。
var allowedTopLevels = map[string]bool{
	"profile":       true,
	"attributes":    true,
	"inventory":     true,
	"relationships": true,
	"location":      true,
	"world_flags":   true,
	"cultivation":   true,
	"ascension":     true,
	"quests":        false,
	"risks":         false,
	"traits":        false, // 天赋特质数组，供判定系统 trait 匹配读取；只能整体更新或追加
}

// AllowedPath 校验某条 state_update 路径是否允许更新。
func AllowedPath(path string) bool {
	path = strings.TrimSuffix(path, "+")
	if path == "" {
		return false
	}
	parts := strings.Split(path, ".")
	nested, ok := allowedTopLevels[parts[0]]
	if !ok {
		return false
	}
	if !nested && len(parts) > 1 {
		// quests / risks 只能整体更新或追加，不允许嵌套路径。
		return false
	}
	return true
}

// ApplyPatch 将 AI/用户提交的 state_update 应用到当前状态。
//
// patch 的键为点号路径，以 "+" 结尾表示向该路径的数组追加元素。
// 返回应用后的新状态、实际生效的补丁，以及被跳过（不合法路径）的键。
// 不会修改入参 state。
func ApplyPatch(state map[string]any, patch map[string]any) (result map[string]any, applied map[string]any, skipped []string) {
	result = deepCopy(state)
	if result == nil {
		result = DefaultState()
	}
	applied = map[string]any{}

	for key, value := range patch {
		if !AllowedPath(key) {
			skipped = append(skipped, key)
			continue
		}
		isAppend := strings.HasSuffix(key, "+")
		path := strings.TrimSuffix(key, "+")
		parts := strings.Split(path, ".")
		setAtPath(result, parts, value, isAppend)
		applied[key] = value
	}
	return result, applied, skipped
}

// setAtPath 沿路径定位（必要时创建中间 map），写入或追加值。
func setAtPath(root map[string]any, parts []string, value any, isAppend bool) {
	cur := root
	for i := 0; i < len(parts)-1; i++ {
		next, ok := cur[parts[i]].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[parts[i]] = next
		}
		cur = next
	}
	last := parts[len(parts)-1]
	if isAppend {
		arr, ok := cur[last].([]any)
		if !ok {
			arr = []any{}
		}
		cur[last] = append(arr, value)
		return
	}
	cur[last] = value
}

// deepCopy 通过 JSON 往返对状态做深拷贝。
func deepCopy(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	b, err := json.Marshal(in)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}
