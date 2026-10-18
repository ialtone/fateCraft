package contextbuilder

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/ialtone/fatecraft/internal/tokenizer"
)

var campaignCoreKeys = []string{
	"id", "status", "current_location_key", "game_time", "revision",
	"state_hash", "narrative_facts", "narrative_context",
}

type fittedCampaignProjection struct {
	Content        string
	OriginalTokens int
	Compacted      bool
	OmittedKeys    []string
}

// fitCampaignProjection keeps the platform core, then fits callable action
// contracts and module-owned top-level topics into maxTokens. It operates on
// JSON values only, so it remains independent of any particular gameplay
// theme or provider implementation.
func fitCampaignProjection(
	projection map[string]any,
	priority []string,
	maxTokens int,
	counter tokenizer.Counter,
) (fittedCampaignProjection, error) {
	normalized, err := normalizeCampaignProjection(projection)
	if err != nil {
		return fittedCampaignProjection{}, err
	}
	originalContent := renderCampaignProjection(normalized)
	originalTokens := counter.Count(originalContent)
	if originalTokens <= maxTokens {
		return fittedCampaignProjection{Content: originalContent, OriginalTokens: originalTokens}, nil
	}

	out := make(map[string]any, len(normalized))
	coreSet := make(map[string]bool, len(campaignCoreKeys))
	for _, key := range campaignCoreKeys {
		coreSet[key] = true
		if value, exists := normalized[key]; exists {
			out[key] = value
		}
	}

	extensionOrder := orderedProjectionKeys(normalized, priority, coreSet)
	omittedSections := append([]string(nil), extensionOrder...)
	actions := normalizedActions(normalized["action_capabilities"])
	control := map[string]any{
		"partial":                  true,
		"omitted_sections":         omittedSections,
		"omitted_action_count":     len(actions),
		"action_details_compacted": len(actions) > 0,
		"instruction":              "Only listed action_capabilities are callable this turn; omitted sections are unavailable context, not empty state.",
	}
	out["projection_control"] = control
	if tokensForCampaignProjection(out, counter) > maxTokens {
		return fittedCampaignProjection{}, fmt.Errorf(
			"平台核心需要 %d token，分配预算仅 %d",
			tokensForCampaignProjection(out, counter), maxTokens,
		)
	}

	type includedAction struct {
		projected map[string]any
		original  map[string]any
	}
	included := make([]includedAction, 0, len(actions))
	actionValues := make([]any, 0, len(actions))
	if len(actions) > 0 {
		out["action_capabilities"] = actionValues
	}
	omittedActionCount := len(actions)
	omittedKeys := make([]string, 0)

	// Available actions come first because their closed schemas are the model's
	// only safe way to request a state-changing domain operation.
	sort.SliceStable(actions, func(i, j int) bool {
		return actionAvailable(actions[i]) && !actionAvailable(actions[j])
	})
	for _, action := range actions {
		projected := baseProjectedAction(action)
		actionValues = append(actionValues, projected)
		out["action_capabilities"] = actionValues
		control["omitted_action_count"] = omittedActionCount - 1
		if tokensForCampaignProjection(out, counter) <= maxTokens {
			omittedActionCount--
			included = append(included, includedAction{projected: projected, original: action})
			continue
		}
		actionValues = actionValues[:len(actionValues)-1]
		out["action_capabilities"] = actionValues
		control["omitted_action_count"] = omittedActionCount
		omittedKeys = append(omittedKeys, "action_capabilities["+actionKey(action)+"]")
	}

	for _, key := range extensionOrder {
		out[key] = normalized[key]
		candidateOmissions := withoutString(omittedSections, key)
		control["omitted_sections"] = candidateOmissions
		if tokensForCampaignProjection(out, counter) <= maxTokens {
			omittedSections = candidateOmissions
			continue
		}
		delete(out, key)
		control["omitted_sections"] = omittedSections
		// Module snapshots often group their entire read model under one object.
		// Keep the facts that fit instead of dropping the whole gameplay section
		// when one inventory or task list is large. Every omission stays explicit.
		if object, ok := normalized[key].(map[string]any); ok && fitProjectionObject(out, key, object, control, maxTokens, counter) {
			omittedSections = candidateOmissions
			control["omitted_sections"] = omittedSections
			omittedKeys = append(omittedKeys, key+".*")
			continue
		}
		omittedKeys = append(omittedKeys, key)
	}

	// Use any remaining room to restore dynamic action metadata and options.
	// Options are added in provider order and explicitly marked partial when a
	// suffix does not fit.
	for _, action := range included {
		if metadata, exists := action.original["metadata"]; exists {
			action.projected["metadata"] = metadata
			if tokensForCampaignProjection(out, counter) > maxTokens {
				delete(action.projected, "metadata")
				omittedKeys = append(omittedKeys, "action_capabilities["+actionKey(action.original)+"].metadata")
			}
		}
		options, _ := action.original["options"].([]any)
		if len(options) == 0 {
			continue
		}
		includedOptions := make([]any, 0, len(options))
		for _, option := range options {
			includedOptions = append(includedOptions, option)
			action.projected["options"] = includedOptions
			if tokensForCampaignProjection(out, counter) <= maxTokens {
				continue
			}
			includedOptions = includedOptions[:len(includedOptions)-1]
			if len(includedOptions) == 0 {
				delete(action.projected, "options")
			} else {
				action.projected["options"] = includedOptions
			}
			break
		}
		if len(includedOptions) < len(options) {
			action.projected["projection_options_truncated"] = true
			if tokensForCampaignProjection(out, counter) > maxTokens {
				delete(action.projected, "projection_options_truncated")
			}
			omittedKeys = append(omittedKeys, "action_capabilities["+actionKey(action.original)+"].options")
		}
	}

	if len(actionValues) == 0 {
		delete(out, "action_capabilities")
	}
	content := renderCampaignProjection(out)
	if tokens := counter.Count(content); tokens > maxTokens {
		return fittedCampaignProjection{}, fmt.Errorf("压缩后仍需 %d token，分配预算仅 %d", tokens, maxTokens)
	}
	sort.Strings(omittedKeys)
	return fittedCampaignProjection{
		Content: content, OriginalTokens: originalTokens, Compacted: true, OmittedKeys: omittedKeys,
	}, nil
}

func fitProjectionObject(out map[string]any, key string, object map[string]any, control map[string]any, budget int, counter tokenizer.Counter) bool {
	keys := make([]string, 0, len(object))
	for field := range object {
		keys = append(keys, field)
	}
	isCollection := func(value any) bool {
		switch value.(type) {
		case []any, map[string]any:
			return true
		}
		return false
	}
	sort.Slice(keys, func(i, j int) bool {
		left, right := isCollection(object[keys[i]]), isCollection(object[keys[j]])
		if left != right {
			return !left
		}
		return keys[i] < keys[j]
	})
	previous, hadPrevious := control["omitted_fields"]
	omitted := []string{}
	if values, ok := previous.([]string); ok {
		omitted = append(omitted, values...)
	}
	for _, field := range keys {
		omitted = append(omitted, key+"."+field)
	}
	partial := map[string]any{}
	out[key], control["omitted_fields"] = partial, omitted
	for _, field := range keys {
		partial[field] = object[field]
		next := withoutString(omitted, key+"."+field)
		control["omitted_fields"] = next
		if tokensForCampaignProjection(out, counter) <= budget {
			omitted = next
		} else {
			delete(partial, field)
			control["omitted_fields"] = omitted
		}
	}
	if len(partial) > 0 && tokensForCampaignProjection(out, counter) <= budget {
		return true
	}
	delete(out, key)
	if hadPrevious {
		control["omitted_fields"] = previous
	} else {
		delete(control, "omitted_fields")
	}
	return false
}

func normalizeCampaignProjection(projection map[string]any) (map[string]any, error) {
	if projection == nil {
		projection = map[string]any{}
	}
	raw, err := json.Marshal(projection)
	if err != nil {
		return nil, fmt.Errorf("序列化 Campaign 投影: %w", err)
	}
	var normalized map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&normalized); err != nil {
		return nil, fmt.Errorf("规范化 Campaign 投影: %w", err)
	}
	return normalized, nil
}

func orderedProjectionKeys(projection map[string]any, priority []string, core map[string]bool) []string {
	seen := map[string]bool{"action_capabilities": true, "projection_control": true}
	out := make([]string, 0, len(projection))
	appendKey := func(key string) {
		if key == "" || seen[key] || core[key] {
			return
		}
		if _, exists := projection[key]; !exists {
			return
		}
		seen[key] = true
		out = append(out, key)
	}
	for _, key := range priority {
		appendKey(key)
	}
	remaining := make([]string, 0, len(projection))
	for key := range projection {
		if !seen[key] && !core[key] {
			remaining = append(remaining, key)
		}
	}
	sort.Strings(remaining)
	for _, key := range remaining {
		appendKey(key)
	}
	return out
}

func normalizedActions(raw any) []map[string]any {
	values, _ := raw.([]any)
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		if action, ok := value.(map[string]any); ok {
			out = append(out, action)
		}
	}
	return out
}

func baseProjectedAction(action map[string]any) map[string]any {
	out := map[string]any{
		"key":         action["key"],
		"description": action["description"],
		"available":   actionAvailable(action),
	}
	if violations, exists := action["violations"]; exists {
		out["violations"] = violations
	}
	if !actionAvailable(action) {
		return out
	}
	if schema, exists := action["argument_schema"]; exists {
		out["argument_schema"] = schema
	}
	if truncated, _ := action["truncated"].(bool); truncated {
		out["truncated"] = true
	}
	return out
}

func actionAvailable(action map[string]any) bool {
	available, _ := action["available"].(bool)
	return available
}

func actionKey(action map[string]any) string {
	key, _ := action["key"].(string)
	if key == "" {
		return "unknown"
	}
	return key
}

func tokensForCampaignProjection(projection map[string]any, counter tokenizer.Counter) int {
	return counter.Count(renderCampaignProjection(projection))
}

func withoutString(values []string, wanted string) []string {
	out := make([]string, 0, len(values)-1)
	for _, value := range values {
		if value != wanted {
			out = append(out, value)
		}
	}
	return out
}
