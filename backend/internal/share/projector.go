package share

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/ialtone/fatecraft/internal/storage"
)

var ErrUnsupportedMessage = errors.New("消息类型不能公开分享")

var allowedMessageTypes = map[string]struct{}{
	"user": {}, "assistant": {}, "system": {}, "event": {}, "state": {}, "roll_result": {},
}

func projectMessages(messages []storage.DisplayMessage) ([]MessageSnapshot, error) {
	if len(messages) == 0 || len(messages) > MaxMessages {
		return nil, storage.ErrShareRange
	}
	out := make([]MessageSnapshot, 0, len(messages))
	for i, message := range messages {
		if _, ok := allowedMessageTypes[message.Type]; !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnsupportedMessage, message.Type)
		}
		metadata := storage.JSONMap{}
		if message.Type == "roll_result" {
			metadata = projectRollMetadata(message.Metadata)
		}
		out = append(out, MessageSnapshot{
			Position: i + 1, Type: message.Type, Markdown: message.Markdown,
			Metadata: metadata, CreatedAt: message.CreatedAt,
		})
	}
	return out, nil
}

func projectRollMetadata(source storage.JSONMap) storage.JSONMap {
	if integer(source["details_version"]) != 1 {
		return storage.JSONMap{}
	}
	outcome, ok := object(source["roll_outcome"])
	if !ok {
		return storage.JSONMap{}
	}
	projectedOutcome := storage.JSONMap{}
	for _, key := range []string{"rolled", "sides", "success_rate"} {
		if value, ok := finiteNumber(outcome[key]); ok {
			projectedOutcome[key] = value
		}
	}
	if value, ok := outcome["success"].(bool); ok {
		projectedOutcome["success"] = value
	}
	if value := boundedString(outcome["level"], 64); value != "" {
		projectedOutcome["level"] = value
	}
	if value, ok := projectNumberObject(outcome["breakdown"], []string{"base", "attributes", "traits", "context", "reward_cap", "history", "difficulty_mode", "final"}); ok {
		projectedOutcome["breakdown"] = value
	}
	if value, ok := projectRange(outcome["success_range"]); ok {
		projectedOutcome["success_range"] = value
	}
	if rawOutcomes, ok := object(outcome["outcomes"]); ok {
		items := storage.JSONMap{}
		for _, key := range []string{"critical_success", "success", "failure", "critical_failure"} {
			if value, ok := projectRange(rawOutcomes[key]); ok {
				items[key] = value
			}
		}
		if len(items) > 0 {
			projectedOutcome["outcomes"] = items
		}
	}
	if len(projectedOutcome) < 6 {
		return storage.JSONMap{}
	}

	result := storage.JSONMap{"details_version": 1, "roll_outcome": projectedOutcome}
	for _, key := range []string{"roll_key", "roll_title", "roll_category"} {
		if value := boundedString(source[key], 256); value != "" {
			result[key] = value
		}
	}
	conditions := make([]any, 0, 32)
	if raw, ok := source["conditions"].([]any); ok {
		for _, item := range raw {
			condition, ok := object(item)
			if !ok || condition["public"] != true {
				continue
			}
			label := boundedString(condition["label"], 256)
			met, valid := condition["met"].(bool)
			if label == "" || !valid {
				continue
			}
			projected := storage.JSONMap{"label": label, "met": met, "public": true}
			for _, key := range []string{"key", "display_expected", "display_actual"} {
				if value := boundedString(condition[key], 256); value != "" {
					projected[key] = value
				}
			}
			conditions = append(conditions, projected)
			if len(conditions) == 32 {
				break
			}
		}
	}
	result["conditions"] = conditions
	return result
}

func object(value any) (storage.JSONMap, bool) {
	switch typed := value.(type) {
	case storage.JSONMap:
		return typed, true
	case map[string]any:
		return storage.JSONMap(typed), true
	default:
		return nil, false
	}
}

func finiteNumber(value any) (any, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return typed, true
	case float64:
		return typed, !math.IsNaN(typed) && !math.IsInf(typed, 0)
	default:
		return nil, false
	}
}

func integer(value any) int {
	if number, ok := finiteNumber(value); ok {
		switch typed := number.(type) {
		case int:
			return typed
		case int64:
			return int(typed)
		case float64:
			if typed == math.Trunc(typed) {
				return int(typed)
			}
		}
	}
	return 0
}

func projectNumberObject(value any, fields []string) (storage.JSONMap, bool) {
	source, ok := object(value)
	if !ok {
		return nil, false
	}
	result := storage.JSONMap{}
	for _, field := range fields {
		number, ok := finiteNumber(source[field])
		if !ok {
			return nil, false
		}
		result[field] = number
	}
	return result, true
}

func projectRange(value any) (storage.JSONMap, bool) {
	source, ok := object(value)
	if !ok {
		return nil, false
	}
	start, startOK := finiteNumber(source["range_start"])
	end, endOK := finiteNumber(source["range_end"])
	if !startOK || !endOK {
		return nil, false
	}
	result := storage.JSONMap{"range_start": start, "range_end": end}
	if probability, ok := finiteNumber(source["probability"]); ok {
		result["probability"] = probability
	}
	return result, true
}

func boundedString(value any, limit int) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	runes := []rune(strings.TrimSpace(text))
	if len(runes) > limit {
		runes = runes[:limit]
	}
	return string(runes)
}
