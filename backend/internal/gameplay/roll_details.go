package gameplay

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	// RollDetailsVersion is bumped when the player-facing roll metadata contract
	// changes in an incompatible way. Old messages remain readable as summaries.
	RollDetailsVersion = 1
	maxRollConditions  = 32
	maxConditionText   = 256
)

// RollConditionSnapshot is the safe, immutable projection of one published
// requires entry. Raw values are included only for explicitly player-visible
// state fields; model/internal conditions are represented by their met flag.
type RollConditionSnapshot struct {
	Key             string `json:"key,omitempty"`
	Label           string `json:"label"`
	Expected        any    `json:"expected,omitempty"`
	Actual          any    `json:"actual,omitempty"`
	DisplayExpected string `json:"display_expected,omitempty"`
	DisplayActual   string `json:"display_actual,omitempty"`
	Met             bool   `json:"met"`
	Public          bool   `json:"public"`
}

// BuildRollConditionSnapshots evaluates the same trusted state used by
// CheckRollRequires and returns a bounded, deterministic player-safe snapshot.
// It deliberately does not expose technical paths or values for model/internal
// fields, even though those fields may participate in the requirement.
func BuildRollConditionSnapshots(contract *RuntimeContract, rule RollRule, state map[string]any) []RollConditionSnapshot {
	if contract == nil || len(rule.Requires) == 0 {
		return []RollConditionSnapshot{}
	}
	paths := make([]string, 0, len(rule.Requires))
	for path := range rule.Requires {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	if len(paths) > maxRollConditions {
		paths = paths[:maxRollConditions]
	}

	conditions := make([]RollConditionSnapshot, 0, len(paths))
	for _, path := range paths {
		condition := rule.Requires[path]
		actual, exists := ValueAtPath(state, path)
		met := exists && matchRequire(actual, condition)
		field, exact := exactStateField(contract.StateSchema, path)
		public := exact && field.Visibility == VisibilityPlayer

		snapshot := RollConditionSnapshot{Met: met, Public: public}
		if public {
			snapshot.Label, snapshot.Key = rollConditionLabel(contract, path)
			snapshot.Expected = safeRollDetailValue(condition)
			snapshot.Actual = safeRollDetailValue(actual)
			snapshot.DisplayExpected = limitConditionText(formatRequirement(contract, path, condition))
			snapshot.DisplayActual = limitConditionText(formatValue(contract, path, actual))
		} else {
			snapshot.Label = "隐藏前置条件"
		}
		conditions = append(conditions, snapshot)
	}
	return conditions
}

func exactStateField(schema StateSchema, path string) (StateField, bool) {
	path = strings.TrimSuffix(path, "+")
	for _, field := range schema.Fields {
		if field.Path == path {
			return field, true
		}
	}
	return StateField{}, false
}

func rollConditionLabel(contract *RuntimeContract, path string) (string, string) {
	for _, section := range contract.Presentation.StateSections {
		for _, field := range section.Fields {
			if field.Path == path && strings.TrimSpace(field.Label) != "" {
				return limitConditionText(field.Label), path
			}
		}
	}
	// A public value may have a value_labels mapping without a dedicated state
	// panel field (for example an internal realm key). Keep the value useful but
	// never expose its technical path as a player-facing label or DOM key.
	return "前置条件", ""
}

func formatRequirement(contract *RuntimeContract, path string, condition any) string {
	object, ok := condition.(map[string]any)
	if !ok {
		return formatValue(contract, path, condition)
	}
	if values, ok := object["any_of"].([]any); ok {
		parts := make([]string, 0, len(values))
		for _, value := range values {
			parts = append(parts, formatValue(contract, path, value))
		}
		return "以下之一：" + strings.Join(parts, "、")
	}
	if values, ok := object["any_of"].([]string); ok {
		parts := make([]string, 0, len(values))
		for _, value := range values {
			parts = append(parts, formatValue(contract, path, value))
		}
		return "以下之一：" + strings.Join(parts, "、")
	}
	minimum, hasMin := numericValue(object["min"])
	maximum, hasMax := numericValue(object["max"])
	switch {
	case hasMin && hasMax:
		return formatNumber(minimum) + "～" + formatNumber(maximum)
	case hasMin:
		return "至少 " + formatNumber(minimum)
	case hasMax:
		return "至多 " + formatNumber(maximum)
	default:
		return "要求条件"
	}
}

func formatValue(contract *RuntimeContract, path string, value any) string {
	if value == nil {
		return "未设置"
	}
	if text, ok := value.(string); ok {
		if labels := contract.Presentation.ValueLabels["state."+path]; labels != nil {
			if label, found := labels[text]; found && strings.TrimSpace(label) != "" {
				return limitConditionText(label)
			}
		}
		return limitConditionText(text)
	}
	if number, ok := numericValue(value); ok {
		return formatNumber(number)
	}
	return limitConditionText(fmt.Sprint(value))
}

func formatNumber(number float64) string {
	if number == float64(int64(number)) {
		return strconv.FormatInt(int64(number), 10)
	}
	return strconv.FormatFloat(number, 'f', -1, 64)
}

func limitConditionText(value string) string {
	runes := []rune(value)
	if len(runes) <= maxConditionText {
		return value
	}
	return string(runes[:maxConditionText-1]) + "…"
}

// safeRollDetailValue keeps the raw compatibility fields bounded as well as
// the human-readable fields. The UI uses display_*; these values are retained
// for API consumers that need typed inspection of a public scalar condition.
func safeRollDetailValue(value any) any {
	switch typed := value.(type) {
	case nil, string, bool:
		return typed
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return typed
	case []any:
		if len(typed) > 16 {
			typed = typed[:16]
		}
		result := make([]any, len(typed))
		for i, item := range typed {
			result[i] = safeRollDetailValue(item)
		}
		return result
	case map[string]any:
		result := make(map[string]any)
		for _, key := range []string{"min", "max", "any_of"} {
			if item, ok := typed[key]; ok {
				result[key] = safeRollDetailValue(item)
			}
		}
		return result
	default:
		return fmt.Sprint(typed)
	}
}
