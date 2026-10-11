package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

const MaxNarrativeFactsBytes = 4096
const MaxNarrativeFeedbackBytes = 8192
const MaxModuleNarrativeFacts = 20

// NarrativeFactProvider declares a small core of facts that must survive
// context compaction. Paths refer to the module's model projection, not its
// private state. Topic-specific declarations stay with the trusted module.
type NarrativeFactProvider interface {
	NarrativeFactPaths() []string
}

func NarrativeFactPaths(module RuntimeModule) []string {
	if provider, ok := module.(NarrativeFactProvider); ok {
		return provider.NarrativeFactPaths()
	}
	return nil
}

func NormalizeNarrativeView(view map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(view)
	if err != nil {
		return nil, err
	}
	var normalized map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	err = decoder.Decode(&normalized)
	return normalized, err
}

// SelectNarrativeFacts copies only explicitly declared projection paths.
// Missing facts are represented as unavailable, never inferred as zero.
func SelectNarrativeFacts(view map[string]any, paths []string) (map[string]any, error) {
	if len(paths) > 24 {
		return nil, fmt.Errorf("叙事关键事实不能超过 24 项")
	}
	facts := map[string]any{}
	for _, path := range paths {
		if path == "" {
			return nil, fmt.Errorf("叙事关键事实路径不能为空")
		}
		if _, duplicate := facts[path]; duplicate {
			return nil, fmt.Errorf("叙事关键事实路径重复: %s", path)
		}
		value, available := ValueAtPath(view, path)
		if available {
			facts[path] = value
		} else {
			facts[path] = map[string]any{"unavailable": true}
		}
	}
	raw, err := json.Marshal(facts)
	if err != nil || len(raw) > MaxNarrativeFactsBytes {
		return nil, fmt.Errorf("叙事关键事实超过容量上限")
	}
	return NormalizeNarrativeView(facts)
}

// SharedNarrativeView intersects public and model views before a feedback is
// stored in an action result shared with the player. Neither audience gains
// access to facts visible only to the other audience.
func SharedNarrativeView(player, model map[string]any) (map[string]any, bool, error) {
	player, err := NormalizeNarrativeView(player)
	if err != nil {
		return nil, false, err
	}
	model, err = NormalizeNarrativeView(model)
	if err != nil {
		return nil, false, err
	}
	partial := false
	var common func(map[string]any, map[string]any) map[string]any
	common = func(public, visible map[string]any) map[string]any {
		out := map[string]any{}
		for key, value := range visible {
			publicValue, exists := public[key]
			if !exists {
				partial = true
				continue
			}
			left, leftOK := publicValue.(map[string]any)
			right, rightOK := value.(map[string]any)
			if leftOK && rightOK {
				out[key] = common(left, right)
			} else if reflect.DeepEqual(publicValue, value) {
				out[key] = value
			} else {
				partial = true
			}
		}
		return out
	}
	return common(player, model), partial, nil
}

// NarrativeFeedback is constructed inside the action transaction from real
// projections. Committed means the action resolved; it does not imply victory.
func NarrativeFeedback(action string, beforeRevision, afterRevision int64, before, after map[string]any, paths []string, result map[string]any, partial bool) (map[string]any, error) {
	facts, err := SelectNarrativeFacts(after, paths)
	if err != nil {
		return nil, err
	}
	before, err = NormalizeNarrativeView(before)
	if err != nil {
		return nil, err
	}
	after, err = NormalizeNarrativeView(after)
	if err != nil {
		return nil, err
	}
	feedback := map[string]any{
		"version": 1, "action_key": action, "execution": "committed",
		"before_revision": beforeRevision, "revision": afterRevision,
		"current_facts": facts,
		"instruction":   "执行已提交不等于目标成功。仅按实际变化和当前事实叙事；未变化的身份不能提升，省略的数据属于未知。",
	}
	if success, ok := result["success"].(bool); ok {
		feedback["goal_succeeded"] = success
	}
	changes := []map[string]any{}
	feedback["changes"] = changes
	omitted := []string{}
	omittedCount := 0
	omit := func(path string) {
		omittedCount++
		if len(omitted) < 8 {
			runes := []rune(path)
			if len(runes) > 32 {
				path = string(runes[:32]) + "…"
			}
			omitted = append(omitted, path)
		}
		partial = true
	}
	var compare func(string, any, any, int)
	compare = func(path string, previous, current any, depth int) {
		if reflect.DeepEqual(previous, current) {
			return
		}
		// Compact keyed inventories/tasks to changed entries instead of copying
		// the entire collection into every tool result.
		if left, right, ok := narrativeCollectionPair(previous, current); ok && depth < 4 {
			keys := map[string]bool{}
			for key := range left {
				keys[key] = true
			}
			for key := range right {
				keys[key] = true
			}
			ordered := make([]string, 0, len(keys))
			for key := range keys {
				ordered = append(ordered, key)
			}
			sort.Strings(ordered)
			for _, key := range ordered {
				quoted, _ := json.Marshal(key)
				compare(path+"["+string(quoted)+"]", left[key], right[key], depth+1)
			}
			return
		}
		left, leftOK := previous.(map[string]any)
		right, rightOK := current.(map[string]any)
		if leftOK && rightOK && depth < 4 {
			keys := map[string]bool{}
			for key := range left {
				keys[key] = true
			}
			for key := range right {
				keys[key] = true
			}
			ordered := make([]string, 0, len(keys))
			for key := range keys {
				ordered = append(ordered, key)
			}
			// Module-declared critical changes precede optional details.
			rank := func(key string) int {
				child := key
				if path != "" {
					child = path + "." + key
				}
				for index, factPath := range paths {
					if child == factPath || strings.HasPrefix(factPath, child+".") {
						return index
					}
				}
				return len(paths)
			}
			sort.Slice(ordered, func(i, j int) bool {
				if rank(ordered[i]) != rank(ordered[j]) {
					return rank(ordered[i]) < rank(ordered[j])
				}
				return ordered[i] < ordered[j]
			})
			for _, key := range ordered {
				child := key
				if path != "" {
					child = path + "." + key
				}
				_, beforeExists := left[key]
				_, afterExists := right[key]
				if !beforeExists || !afterExists {
					// Projection omission is not proof that a fact was created or
					// removed. Keyed collections above have explicit membership.
					omit(child)
					continue
				}
				compare(child, left[key], right[key], depth+1)
			}
			return
		}
		change := map[string]any{"path": path, "before": previous, "after": current}
		changes = append(changes, change)
		feedback["changes"] = changes
		raw, marshalErr := json.Marshal(feedback)
		// Reserve space for omission metadata and the surrounding tool envelope.
		if len(changes) > 24 || marshalErr != nil || len(raw) > MaxNarrativeFeedbackBytes-1536 {
			changes = changes[:len(changes)-1]
			feedback["changes"] = changes
			omit(path)
		}
	}
	compare("", before, after, 0)
	feedback["details_partial"] = partial
	if len(omitted) > 0 {
		feedback["omitted_changes"] = omitted
		feedback["omitted_change_count"] = omittedCount
	}
	raw, err := json.Marshal(feedback)
	if err != nil || len(raw) > MaxNarrativeFeedbackBytes {
		return nil, fmt.Errorf("动作叙事反馈超过容量上限")
	}
	return feedback, nil
}

func narrativeCollectionPair(previous, current any) (map[string]any, map[string]any, bool) {
	left, leftOK := previous.([]any)
	right, rightOK := current.([]any)
	if !leftOK || !rightOK {
		return nil, nil, false
	}
	for _, identity := range []string{"key", "id", "name"} {
		index := func(items []any) (map[string]any, bool) {
			out := map[string]any{}
			for _, item := range items {
				object, ok := item.(map[string]any)
				if !ok {
					return nil, false
				}
				key, ok := object[identity].(string)
				if !ok || key == "" {
					return nil, false
				}
				if _, exists := out[key]; exists {
					return nil, false
				}
				out[key] = item
			}
			return out, true
		}
		before, beforeOK := index(left)
		after, afterOK := index(right)
		if beforeOK && afterOK {
			return before, after, true
		}
	}
	return nil, nil, false
}
