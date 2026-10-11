package campaign

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/ialtone/fatecraft/internal/storage"
)

const (
	defaultCapabilityProviderTimeout  = 200 * time.Millisecond
	defaultCapabilityTotalBytes       = 96 * 1024
	defaultCapabilityOptionsPerAction = 64
)

// ActionDefinition is the state-independent contract of a trusted action.
// Dynamic availability and argument bounds belong to ActionCapability.
type ActionDefinition struct {
	Ending         bool            `json:"ending,omitempty"`
	Key            string          `json:"key"`
	Description    string          `json:"description"`
	Actors         []string        `json:"actors"`
	ArgumentSchema storage.JSONMap `json:"argument_schema"`
	// Writes and Events make an action's authoritative side effects explicit
	// for runtime audits and ownership validation. They are immutable metadata;
	// the handler remains the final authority for business rules.
	Writes []string `json:"writes,omitempty"`
	Events []string `json:"events,omitempty"`
}

type ActionViolation struct {
	Code    string          `json:"code"`
	Path    string          `json:"path,omitempty"`
	Message string          `json:"message,omitempty"`
	Public  bool            `json:"-"`
	Details storage.JSONMap `json:"details,omitempty"`
}

type ActionCapability struct {
	Key            string            `json:"key"`
	Description    string            `json:"description"`
	Available      bool              `json:"available"`
	Revision       int64             `json:"revision"`
	ArgumentSchema storage.JSONMap   `json:"argument_schema"`
	Options        []storage.JSONMap `json:"options,omitempty"`
	Violations     []ActionViolation `json:"violations,omitempty"`
	Metadata       storage.JSONMap   `json:"metadata,omitempty"`
	Truncated      bool              `json:"truncated,omitempty"`
}

type CapabilityBudget struct {
	MaxOptions int
	MaxBytes   int
}

type CapabilityContext struct {
	Campaign  storage.Campaign
	Domain    *storage.CampaignDomainState
	ActorType string
	Query     string
	Budget    CapabilityBudget
}

type ActionCapabilityProvider interface {
	Project(context.Context, CapabilityContext, ActionDefinition) (ActionCapability, error)
}

type ActionCapabilityProviderFunc func(context.Context, CapabilityContext, ActionDefinition) (ActionCapability, error)

func (fn ActionCapabilityProviderFunc) Project(ctx context.Context, input CapabilityContext, definition ActionDefinition) (ActionCapability, error) {
	return fn(ctx, input, definition)
}

// StaticCapabilityProvider is only appropriate for actions whose availability
// and argument bounds do not depend on campaign state.
type StaticCapabilityProvider struct{}

func (StaticCapabilityProvider) Project(_ context.Context, input CapabilityContext, definition ActionDefinition) (ActionCapability, error) {
	return ActionCapability{
		Key: definition.Key, Description: definition.Description, Available: true,
		Revision: input.Campaign.Revision, ArgumentSchema: cloneJSONMap(definition.ArgumentSchema),
	}, nil
}

type RegisteredAction struct {
	Definition ActionDefinition
	Capability ActionCapabilityProvider
	Handler    ActionHandler
}

type ProjectionContext struct {
	Campaign      storage.Campaign
	Domain        *storage.CampaignDomainState
	Query         string
	ContextPolicy string
}

// ProjectionView is the model-facing, bounded read model produced by a
// gameplay module. Priority is a list of top-level Data keys ordered from
// most to least important when the context builder must fit a small model
// window. Keeping this policy with the module prevents the platform from
// hard-coding topic-specific knowledge.
type ProjectionView struct {
	Data     map[string]any
	Priority []string
	// FactPaths is a module-declared core that must survive model compaction.
	FactPaths []string
}

// ProjectionProvider owns all topic-specific Campaign read models. The core
// only selects a provider using the pinned Runtime contract.
type ProjectionProvider interface {
	Snapshot(context.Context, ProjectionContext) (map[string]any, error)
	Inquiry(context.Context, ProjectionContext) (map[string]any, error)
	Context(context.Context, ProjectionContext) (ProjectionView, error)
}

type ActionViolationError struct {
	Violation ActionViolation
}

func (e *ActionViolationError) Error() string {
	if e == nil {
		return ErrActionRejected.Error()
	}
	if message := strings.TrimSpace(e.Violation.Message); message != "" {
		return ErrActionRejected.Error() + ": " + message
	}
	return ErrActionRejected.Error() + ": " + e.Violation.Code
}

func (e *ActionViolationError) Unwrap() error { return ErrActionRejected }

func RejectAction(violation ActionViolation) error {
	if !validStableKey(violation.Code) {
		violation.Code = "action.rejected"
	}
	violation.Message = strings.TrimSpace(violation.Message)
	return &ActionViolationError{Violation: violation}
}

func PublicActionViolation(err error) (ActionViolation, bool) {
	var violationErr *ActionViolationError
	if !errors.As(err, &violationErr) || violationErr == nil || !violationErr.Violation.Public {
		return ActionViolation{}, false
	}
	violation := violationErr.Violation
	violation.Details = nil
	return violation, strings.TrimSpace(violation.Message) != ""
}

func validateActionDefinition(definition ActionDefinition) error {
	if !validStableKey(definition.Key) || strings.TrimSpace(definition.Description) == "" || len(definition.Actors) == 0 {
		return errors.New("invalid action definition")
	}
	seenActors := map[string]bool{}
	for _, actor := range definition.Actors {
		if actor != "player" && actor != "model" && actor != "system" || seenActors[actor] {
			return fmt.Errorf("invalid or duplicate action actor %q", actor)
		}
		seenActors[actor] = true
	}
	if err := validateArgumentSchemaDefinition(definition.ArgumentSchema); err != nil {
		return err
	}
	seenWrites := map[string]bool{}
	for _, path := range definition.Writes {
		if !validActionPath(path) || seenWrites[path] {
			return fmt.Errorf("invalid or duplicate action write path %q", path)
		}
		seenWrites[path] = true
	}
	seenEvents := map[string]bool{}
	for _, eventType := range definition.Events {
		if !validStableKey(eventType) || seenEvents[eventType] {
			return fmt.Errorf("invalid or duplicate action event %q", eventType)
		}
		seenEvents[eventType] = true
	}
	return nil
}

func validActionPath(path string) bool {
	if !validStableKey(path) || strings.HasPrefix(path, "fc_") {
		return false
	}
	for _, part := range strings.Split(path, ".") {
		if !validStableKey(part) {
			return false
		}
	}
	return true
}

func validateArgumentSchemaDefinition(schema storage.JSONMap) error {
	if schema == nil || schema["type"] != "object" || schema["additionalProperties"] != false {
		return errors.New("action argument_schema must be a closed object schema")
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		if typed, typedOK := schema["properties"].(storage.JSONMap); typedOK {
			properties, ok = map[string]any(typed), true
		}
	}
	if !ok {
		return errors.New("action argument_schema properties must be an object")
	}
	for key, raw := range properties {
		if !validArgumentName(key) {
			return fmt.Errorf("invalid action argument name %q", key)
		}
		property, ok := raw.(map[string]any)
		if !ok {
			if typed, typedOK := raw.(storage.JSONMap); typedOK {
				property, ok = map[string]any(typed), true
			}
		}
		if !ok || !supportedArgumentType(property["type"]) {
			return fmt.Errorf("unsupported schema for action argument %s", key)
		}
	}
	for _, name := range stringSlice(schema["required"]) {
		if _, exists := properties[name]; !exists {
			return fmt.Errorf("required action argument %s is not declared", name)
		}
	}
	return nil
}

func validateActionArguments(schema storage.JSONMap, arguments map[string]any) error {
	properties, _ := schema["properties"].(map[string]any)
	if properties == nil {
		if typed, ok := schema["properties"].(storage.JSONMap); ok {
			properties = map[string]any(typed)
		}
	}
	for _, name := range stringSlice(schema["required"]) {
		if value, exists := arguments[name]; !exists || value == nil {
			return fmt.Errorf("%w: 缺少参数 %s", ErrInvalidAction, name)
		}
	}
	for name, value := range arguments {
		rawSchema, exists := properties[name]
		if !exists {
			return fmt.Errorf("%w: 包含未知参数 %s", ErrInvalidAction, name)
		}
		property, _ := rawSchema.(map[string]any)
		if property == nil {
			if typed, ok := rawSchema.(storage.JSONMap); ok {
				property = map[string]any(typed)
			}
		}
		if err := validateArgumentValue(name, value, property); err != nil {
			return err
		}
	}
	return nil
}

// validateCapabilityArguments is called again inside the locked transaction.
// It validates dynamic schemas and dependent option limits without changing
// the submitted arguments.
func validateCapabilityArguments(capability ActionCapability, arguments map[string]any) error {
	if !capability.Available {
		for _, violation := range capability.Violations {
			if violation.Public {
				return RejectAction(violation)
			}
		}
		return RejectAction(ActionViolation{Code: "action.unavailable", Message: "动作当前不可执行", Public: false})
	}
	if err := validateActionArguments(capability.ArgumentSchema, arguments); err != nil {
		return err
	}
	if len(capability.Options) == 0 || capability.Truncated {
		return nil
	}
	properties, _ := capability.ArgumentSchema["properties"].(map[string]any)
	selectorFields := map[string]bool{}
	for _, option := range capability.Options {
		for field := range properties {
			if _, constrained := option[field]; constrained {
				selectorFields[field] = true
			}
		}
	}
	matchedSelector := false
	suppliedSelector := false
	for _, option := range capability.Options {
		selectorCount := 0
		matches := true
		for field := range selectorFields {
			optionValue, constrained := option[field]
			argumentValue, supplied := arguments[field]
			if !supplied {
				continue
			}
			suppliedSelector = true
			selectorCount++
			if !constrained || !jsonValuesEqual(optionValue, argumentValue) {
				matches = false
				break
			}
		}
		if selectorCount == 0 || !matches {
			continue
		}
		matchedSelector = true
		if optionLimitsAllow(option["limits"], arguments) {
			return nil
		}
	}
	if !suppliedSelector {
		return nil
	}
	message := "参数组合不在当前动作能力允许范围内"
	code := "action.option_unavailable"
	if matchedSelector {
		message = "动作参数超出当前资源或数量上限"
		code = "action.limit_exceeded"
	}
	return RejectAction(ActionViolation{Code: code, Path: "arguments", Message: message, Public: true})
}

func optionLimitsAllow(raw any, arguments map[string]any) bool {
	limits, _ := raw.(storage.JSONMap)
	if limits == nil {
		if generic, ok := raw.(map[string]any); ok {
			limits = storage.JSONMap(generic)
		}
	}
	for field, rawLimit := range limits {
		value, supplied := arguments[field]
		if !supplied {
			continue
		}
		number, numeric := exactInteger(value)
		limit, _ := rawLimit.(storage.JSONMap)
		if limit == nil {
			if generic, ok := rawLimit.(map[string]any); ok {
				limit = storage.JSONMap(generic)
			}
		}
		if !numeric || limit == nil {
			return false
		}
		if minimum, ok := schemaNumber(limit["minimum"]); ok && float64(number) < minimum {
			return false
		}
		if maximum, ok := schemaNumber(limit["maximum"]); ok && float64(number) > maximum {
			return false
		}
	}
	return true
}

func jsonValuesEqual(left, right any) bool {
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return string(leftJSON) == string(rightJSON)
}

func validateArgumentValue(name string, value any, schema map[string]any) error {
	typeName, _ := schema["type"].(string)
	switch typeName {
	case "string":
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("%w: 参数 %s 必须是字符串", ErrInvalidAction, name)
		}
		if minimum, ok := schemaNumber(schema["minLength"]); ok && len([]rune(text)) < int(minimum) {
			return fmt.Errorf("%w: 参数 %s 太短", ErrInvalidAction, name)
		}
		if maximum, ok := schemaNumber(schema["maxLength"]); ok && len([]rune(text)) > int(maximum) {
			return fmt.Errorf("%w: 参数 %s 太长", ErrInvalidAction, name)
		}
	case "integer":
		number, ok := exactInteger(value)
		if !ok {
			return fmt.Errorf("%w: 参数 %s 必须是整数", ErrInvalidAction, name)
		}
		if minimum, ok := schemaNumber(schema["minimum"]); ok && float64(number) < minimum {
			return fmt.Errorf("%w: 参数 %s 小于允许范围", ErrInvalidAction, name)
		}
		if maximum, ok := schemaNumber(schema["maximum"]); ok && float64(number) > maximum {
			return fmt.Errorf("%w: 参数 %s 超出允许范围", ErrInvalidAction, name)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%w: 参数 %s 必须是布尔值", ErrInvalidAction, name)
		}
	default:
		return fmt.Errorf("%w: 参数 %s 的结构不受支持", ErrInvalidAction, name)
	}
	if enum, exists := schema["enum"].([]any); exists && !enumContains(enum, value) {
		return fmt.Errorf("%w: 参数 %s 不在允许选项中", ErrInvalidAction, name)
	}
	return nil
}

func validateProjectedCapability(capability ActionCapability, definition ActionDefinition, revision int64, budget CapabilityBudget) (ActionCapability, error) {
	capability.Key = definition.Key
	capability.Description = definition.Description
	capability.Revision = revision
	if capability.ArgumentSchema == nil {
		capability.ArgumentSchema = cloneJSONMap(definition.ArgumentSchema)
	}
	if err := validateArgumentSchemaDefinition(capability.ArgumentSchema); err != nil {
		return ActionCapability{}, fmt.Errorf("capability %s: %w", definition.Key, err)
	}
	if err := validateSchemaNarrowing(definition.ArgumentSchema, capability.ArgumentSchema); err != nil {
		return ActionCapability{}, fmt.Errorf("capability %s broadens its action definition: %w", definition.Key, err)
	}
	if budget.MaxOptions > 0 && len(capability.Options) > budget.MaxOptions {
		capability.Options = capability.Options[:budget.MaxOptions]
		capability.Truncated = true
	}
	publicViolations := make([]ActionViolation, 0, len(capability.Violations))
	for _, violation := range capability.Violations {
		if !violation.Public || !validStableKey(violation.Code) {
			continue
		}
		violation.Details = nil
		publicViolations = append(publicViolations, violation)
	}
	capability.Violations = publicViolations
	return capability, nil
}

func validateSchemaNarrowing(base, projected storage.JSONMap) error {
	baseProperties := schemaProperties(base)
	projectedProperties := schemaProperties(projected)
	baseRequired := sliceSet(stringSlice(base["required"]))
	projectedRequired := sliceSet(stringSlice(projected["required"]))
	for field := range baseRequired {
		if !projectedRequired[field] {
			return fmt.Errorf("required argument %s became optional", field)
		}
	}
	for field, rawProjected := range projectedProperties {
		rawBase, exists := baseProperties[field]
		if !exists {
			return fmt.Errorf("argument %s was added", field)
		}
		baseProperty := propertySchema(rawBase)
		projectedProperty := propertySchema(rawProjected)
		if baseProperty["type"] != projectedProperty["type"] {
			return fmt.Errorf("argument %s changed type", field)
		}
		switch baseProperty["type"] {
		case "string":
			if boundBroadened(baseProperty, projectedProperty, "minLength", true) ||
				boundBroadened(baseProperty, projectedProperty, "maxLength", false) {
				return fmt.Errorf("argument %s broadened its length bounds", field)
			}
		case "integer":
			if boundBroadened(baseProperty, projectedProperty, "minimum", true) ||
				boundBroadened(baseProperty, projectedProperty, "maximum", false) {
				return fmt.Errorf("argument %s broadened its numeric bounds", field)
			}
		}
		baseEnum, baseHasEnum := baseProperty["enum"].([]any)
		if !baseHasEnum {
			continue
		}
		projectedEnum, projectedHasEnum := projectedProperty["enum"].([]any)
		if !projectedHasEnum {
			return fmt.Errorf("argument %s removed its enum", field)
		}
		for _, value := range projectedEnum {
			if !enumContains(baseEnum, value) {
				return fmt.Errorf("argument %s added an enum value", field)
			}
		}
	}
	return nil
}

func schemaProperties(schema storage.JSONMap) map[string]any {
	properties, _ := schema["properties"].(map[string]any)
	if properties == nil {
		if typed, ok := schema["properties"].(storage.JSONMap); ok {
			properties = map[string]any(typed)
		}
	}
	return properties
}

func propertySchema(raw any) map[string]any {
	property, _ := raw.(map[string]any)
	if property == nil {
		if typed, ok := raw.(storage.JSONMap); ok {
			property = map[string]any(typed)
		}
	}
	return property
}

func sliceSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}

func boundBroadened(base, projected map[string]any, field string, lower bool) bool {
	baseValue, baseExists := schemaNumber(base[field])
	if !baseExists {
		return false
	}
	projectedValue, projectedExists := schemaNumber(projected[field])
	if !projectedExists {
		return true
	}
	if lower {
		return projectedValue < baseValue
	}
	return projectedValue > baseValue
}

func exactInteger(value any) (int64, bool) {
	switch number := value.(type) {
	case int:
		return int64(number), true
	case int64:
		return number, true
	case float64:
		if math.IsNaN(number) || math.IsInf(number, 0) || number != math.Trunc(number) || number < math.MinInt64 || number > math.MaxInt64 {
			return 0, false
		}
		return int64(number), true
	default:
		return 0, false
	}
}

func schemaNumber(value any) (float64, bool) {
	switch number := value.(type) {
	case int:
		return float64(number), true
	case int64:
		return float64(number), true
	case float64:
		return number, true
	default:
		return 0, false
	}
}

func enumContains(values []any, wanted any) bool {
	wantedJSON, _ := json.Marshal(wanted)
	for _, value := range values {
		valueJSON, _ := json.Marshal(value)
		if string(valueJSON) == string(wantedJSON) {
			return true
		}
	}
	return false
}

func supportedArgumentType(value any) bool {
	typeName, _ := value.(string)
	return typeName == "string" || typeName == "integer" || typeName == "boolean"
}

func validArgumentName(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for index, char := range value {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || index > 0 && char == '_' {
			continue
		}
		return false
	}
	return true
}

func stringSlice(value any) []string {
	switch values := value.(type) {
	case []string:
		return values
	case []any:
		out := make([]string, 0, len(values))
		for _, raw := range values {
			if text, ok := raw.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func cloneJSONMap(value storage.JSONMap) storage.JSONMap {
	if value == nil {
		return nil
	}
	raw, _ := json.Marshal(value)
	var out storage.JSONMap
	_ = json.Unmarshal(raw, &out)
	return out
}
