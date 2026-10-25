package state

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
)

// PatchValidationError reports every invalid key in one atomic state patch.
type PatchValidationError struct {
	Keys []string
}

func (e *PatchValidationError) Error() string {
	return fmt.Sprintf("状态补丁包含非法字段: %s", strings.Join(e.Keys, ", "))
}

type PatchIssueCode string

const (
	PatchMalformedPath   PatchIssueCode = "malformed_path"
	PatchUnknownPath     PatchIssueCode = "unknown_path"
	PatchForbiddenSource PatchIssueCode = "forbidden_source"
	PatchUnsafeObject    PatchIssueCode = "unsafe_object_replace"
	PatchInvalidAppend   PatchIssueCode = "invalid_append"
	PatchInvalidValue    PatchIssueCode = "invalid_value"
)

type PatchIssue struct {
	Key  string
	Code PatchIssueCode
}

// ValidatePatch validates the whole patch before any value is applied. A plus
// suffix appends one value to a declared list field, or to a dynamic child of
// an allow_children object (dynamic children already accept arbitrary values
// via absolute assignment, so appending grants no additional authority).
func ValidatePatch(schema gameplay.StateSchema, source string, patch map[string]any) error {
	invalid := invalidPatchKeys(schema, source, patch)
	if len(invalid) > 0 {
		return &PatchValidationError{Keys: invalid}
	}
	return nil
}

// SplitPatch separates a patch into the subset that passes schema validation
// and the keys that do not. It never fails the whole patch: callers apply the
// valid subset and report the skipped keys, so one drifted path cannot erase
// an otherwise valid model turn.
func SplitPatch(schema gameplay.StateSchema, source string, patch map[string]any) (map[string]any, []string) {
	skipped := invalidPatchKeys(schema, source, patch)
	if len(skipped) == 0 {
		return patch, nil
	}
	skipSet := make(map[string]struct{}, len(skipped))
	for _, key := range skipped {
		skipSet[key] = struct{}{}
	}
	valid := make(map[string]any, len(patch)-len(skipped))
	for key, value := range patch {
		if _, skip := skipSet[key]; !skip {
			valid[key] = value
		}
	}
	return valid, skipped
}

// ApplyPatch validates update paths against the published gameplay schema.
// The patch is atomic: if any key is invalid, no key is applied.
func ApplyPatch(schema gameplay.StateSchema, source string, current map[string]any, patch map[string]any) (result map[string]any, applied map[string]any, skipped []string) {
	result = deepCopy(current)
	if result == nil {
		result = gameplay.InitialState(schema)
	}
	applied = map[string]any{}
	if skipped = invalidPatchKeys(schema, source, patch); len(skipped) > 0 {
		return result, applied, skipped
	}
	keys := make([]string, 0, len(patch))
	for key := range patch {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := patch[key]
		isAppend := strings.HasSuffix(key, "+")
		path := strings.TrimSuffix(key, "+")
		setAtPath(result, strings.Split(path, "."), value, isAppend)
		applied[key] = value
	}
	return result, applied, skipped
}

// InspectPatch classifies every invalid entry so orchestration can safely
// ignore authority/unknown-path drift while treating malformed model-owned
// values atomically.
func InspectPatch(schema gameplay.StateSchema, source string, patch map[string]any) []PatchIssue {
	issues := make([]PatchIssue, 0)
	for key, value := range patch {
		isAppend := strings.HasSuffix(key, "+")
		path := strings.TrimSuffix(key, "+")
		if path == PlatformStateKey || strings.HasPrefix(path, PlatformStateKey+".") {
			issues = append(issues, PatchIssue{Key: key, Code: PatchForbiddenSource})
			continue
		}
		if key == "" || strings.TrimSpace(key) != key || path == "" ||
			strings.Contains(path, "+") || strings.HasPrefix(path, ".") ||
			strings.HasSuffix(path, ".") || strings.HasSuffix(path, "-") || strings.Contains(path, "..") {
			issues = append(issues, PatchIssue{Key: key, Code: PatchMalformedPath})
			continue
		}
		field, ok := gameplay.FieldForPath(schema, path)
		if !ok {
			issues = append(issues, PatchIssue{Key: key, Code: PatchUnknownPath})
			continue
		}
		if !gameplay.CanUpdate(schema, path, source) {
			issues = append(issues, PatchIssue{Key: key, Code: PatchForbiddenSource})
			continue
		}
		appendOK := !isAppend ||
			(field.Path == path && field.Type == "list") ||
			(field.AllowChildren && field.Path != path)
		if !appendOK {
			issues = append(issues, PatchIssue{Key: key, Code: PatchInvalidAppend})
			continue
		}
		if !isAppend && field.Path == path && field.Type == "object" && !gameplay.CanReplaceObject(schema, path) {
			issues = append(issues, PatchIssue{Key: key, Code: PatchUnsafeObject})
			continue
		}
		if !gameplay.ValidUpdateValue(field, path, value, isAppend) {
			issues = append(issues, PatchIssue{Key: key, Code: PatchInvalidValue})
		}
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].Key < issues[j].Key })
	return issues
}

func invalidPatchKeys(schema gameplay.StateSchema, source string, patch map[string]any) []string {
	issues := InspectPatch(schema, source, patch)
	invalid := make([]string, len(issues))
	for index, issue := range issues {
		invalid[index] = issue.Key
	}
	return invalid
}

// FilterState keeps only values declared by the gameplay schema plus the
// platform-owned internal region. It is used for administrator replacement and
// never exposes undeclared pack state keys.
func FilterState(schema gameplay.StateSchema, input map[string]any) map[string]any {
	filtered := map[string]any{}
	for _, field := range schema.Fields {
		if value, ok := gameplay.ValueAtPath(input, field.Path); ok {
			setAtPath(filtered, strings.Split(field.Path, "."), deepCopyValue(value), false)
		}
	}
	if platform := ReservedPlatformState(input); platform != nil {
		filtered[PlatformStateKey] = platform
	}
	return filtered
}

func setAtPath(root map[string]any, parts []string, value any, appendValue bool) {
	current := root
	for index := 0; index < len(parts)-1; index++ {
		next, ok := current[parts[index]].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[parts[index]] = next
		}
		current = next
	}
	last := parts[len(parts)-1]
	if appendValue {
		values, _ := current[last].([]any)
		current[last] = append(values, deepCopyValue(value))
		return
	}
	current[last] = deepCopyValue(value)
}

func deepCopy(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	copy, _ := deepCopyValue(in).(map[string]any)
	return copy
}

func deepCopyValue(value any) any {
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var result any
	if err := json.Unmarshal(raw, &result); err != nil {
		return value
	}
	return result
}
