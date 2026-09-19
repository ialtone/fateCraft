package orchestration

import "math"

func modelActionAllowed(allowed []string, wanted string) bool {
	for _, actionKey := range allowed {
		if actionKey == wanted {
			return true
		}
	}
	return false
}

func getStringParam(params map[string]any, key, fallback string) string {
	if value, ok := params[key].(string); ok {
		return value
	}
	return fallback
}

func exactInt64(value any) (int64, bool) {
	switch number := value.(type) {
	case int:
		return int64(number), true
	case int64:
		return number, true
	case float64:
		if math.IsNaN(number) || math.IsInf(number, 0) || number != math.Trunc(number) ||
			number < math.MinInt64 || number > math.MaxInt64 {
			return 0, false
		}
		return int64(number), true
	default:
		return 0, false
	}
}
