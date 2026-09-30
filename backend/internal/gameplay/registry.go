package gameplay

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/ialtone/fatecraft/internal/storage"
)

type Evidence struct {
	State              map[string]any
	SuccessfulRollKeys map[string]bool
	TurnCount          int
	Facts              map[string]any
}

type VerifierFunc func(Evidence, storage.JSONMap) error
type ConfigValidator func(storage.JSONMap) error

type verifierRegistration struct {
	validate ConfigValidator
	verify   VerifierFunc
}

type ModuleDescriptor struct {
	Key                        string   `json:"key"`
	Version                    int      `json:"version"`
	Name                       string   `json:"name"`
	Description                string   `json:"description"`
	Modes                      []string `json:"modes"`
	Actions                    []string `json:"actions"`
	Verifiers                  []string `json:"verifiers"`
	SettlementHandlers         []string `json:"settlement_handlers"`
	CampaignLifecycleHandlers  []string `json:"campaign_lifecycle_handlers,omitempty"`
	CampaignProjectionHandlers []string `json:"campaign_projection_handlers,omitempty"`
	CampaignContextPolicies    []string `json:"campaign_context_policies,omitempty"`
	CampaignActions            []string `json:"campaign_actions,omitempty"`
	// CampaignActionWrites/Events are immutable ownership metadata published by
	// the module. They let runtime validation prove that every authoritative
	// field has a registered writer and event contract.
	CampaignActionWrites  map[string][]string `json:"campaign_action_writes,omitempty"`
	CampaignActionEvents  map[string][]string `json:"campaign_action_events,omitempty"`
	CampaignRequiresRealm bool                `json:"campaign_requires_realm,omitempty"`
}

type Registry struct {
	modules   map[string]ModuleDescriptor
	verifiers map[string]verifierRegistration
}

func NewRegistry() *Registry {
	return &Registry{
		modules: map[string]ModuleDescriptor{}, verifiers: map[string]verifierRegistration{},
	}
}

func NewCoreRegistry() *Registry {
	registry := NewRegistry()
	registry.RegisterVerifier("core.always", validateEmptyConfig, func(Evidence, storage.JSONMap) error { return nil })
	registry.RegisterVerifier("core.conditions", validateConditionsConfig, verifyConditions)
	registry.RegisterModule(ModuleDescriptor{
		Key: "core.generic", Version: 1, Name: "通用状态玩法",
		Description:        "使用受限状态条件、服务端判定证据和固定通关积分。",
		Modes:              []string{RuntimeModeChallenge},
		Actions:            []string{"update_state", "roll", "complete_challenge"},
		Verifiers:          []string{"core.always", "core.conditions"},
		SettlementHandlers: []string{"core.challenge_completion"},
	})
	return registry
}

func (r *Registry) RegisterModule(module ModuleDescriptor) {
	if r == nil {
		panic("gameplay registry is nil")
	}
	if module.Key == "" || module.Version <= 0 {
		panic("invalid gameplay module")
	}
	key := moduleRegistryKey(module.Key, module.Version)
	if _, exists := r.modules[key]; exists {
		panic("duplicate gameplay module: " + key)
	}
	r.modules[key] = module
}

func (r *Registry) RegisterVerifier(key string, validate ConfigValidator, verify VerifierFunc) {
	if key == "" || validate == nil || verify == nil {
		panic("invalid gameplay verifier")
	}
	if _, exists := r.verifiers[key]; exists {
		panic("duplicate gameplay verifier: " + key)
	}
	r.verifiers[key] = verifierRegistration{validate: validate, verify: verify}
}

func (r *Registry) Modules() []ModuleDescriptor {
	out := make([]ModuleDescriptor, 0, len(r.modules))
	for _, module := range r.modules {
		out = append(out, module)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Key == out[j].Key {
			return out[i].Version < out[j].Version
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// CampaignRequiresRealm reports whether a registered Campaign engine needs
// cultivation realm definitions and the corresponding character domain row.
// Runtime validation should run first so an unknown engine cannot be mistaken
// for a generic one.
func (r *Registry) CampaignRequiresRealm(contract *RuntimeContract) bool {
	if r == nil || contract == nil {
		return true
	}
	module, ok := r.modules[moduleRegistryKey(contract.Engine.Key, contract.Engine.Version)]
	if !ok {
		return true
	}
	return module.CampaignRequiresRealm
}

func (r *Registry) ValidateRuntime(contract *RuntimeContract) error {
	if err := ValidateContract(contract); err != nil {
		return err
	}
	module, ok := r.modules[moduleRegistryKey(contract.Engine.Key, contract.Engine.Version)]
	if !ok {
		return fmt.Errorf("未安装玩法模块 %s@%d", contract.Engine.Key, contract.Engine.Version)
	}
	modes := module.Modes
	if len(modes) == 0 {
		modes = []string{RuntimeModeChallenge}
	}
	if !contains(modes, contract.Mode) {
		return fmt.Errorf("玩法模块不支持 runtime mode %s", contract.Mode)
	}
	if contract.Mode == RuntimeModeChallenge && !contains(module.SettlementHandlers, contract.Settlement.Handler) {
		return fmt.Errorf("玩法模块不允许结算处理器 %s", contract.Settlement.Handler)
	}
	if contract.Mode == RuntimeModeCampaign {
		if !contains(module.CampaignLifecycleHandlers, contract.Campaign.LifecycleHandler) {
			return fmt.Errorf("玩法模块不允许 Campaign 生命周期处理器 %s", contract.Campaign.LifecycleHandler)
		}
		if !contains(module.CampaignProjectionHandlers, contract.Campaign.ProjectionHandler) {
			return fmt.Errorf("玩法模块不允许 Campaign 投影处理器 %s", contract.Campaign.ProjectionHandler)
		}
		if !contains(module.CampaignContextPolicies, contract.Campaign.ContextPolicy) {
			return fmt.Errorf("玩法模块不允许 Campaign 上下文策略 %s", contract.Campaign.ContextPolicy)
		}
		for _, action := range contract.Campaign.ActionAllowlist {
			if !contains(module.CampaignActions, action) {
				return fmt.Errorf("玩法模块不允许 Campaign 动作 %s", action)
			}
		}
		for _, ownership := range contract.Campaign.FieldOwnership {
			if ownership.WriteCapability == "" {
				continue
			}
			writes, registered := module.CampaignActionWrites[ownership.WriteCapability]
			if !registered {
				return fmt.Errorf("Campaign 字段 %s 的写入能力未在玩法模块声明: %s", ownership.Path, ownership.WriteCapability)
			}
			matched := false
			for _, path := range writes {
				if pathsOverlap(path, ownership.Path) {
					matched = true
					break
				}
			}
			if !matched {
				return fmt.Errorf("Campaign 字段 %s 与动作 %s 的写入路径不一致", ownership.Path, ownership.WriteCapability)
			}
			declaredEvents := module.CampaignActionEvents[ownership.WriteCapability]
			for _, eventType := range ownership.EventTypes {
				if !contains(declaredEvents, eventType) {
					return fmt.Errorf("Campaign 字段 %s 的事件 %s 未由动作 %s 声明", ownership.Path, eventType, ownership.WriteCapability)
				}
			}
		}
	}
	for _, action := range contract.Actions.Model {
		if !contains(module.Actions, action) {
			return fmt.Errorf("玩法模块不允许模型动作 %s", action)
		}
	}
	return nil
}

func (r *Registry) ValidateVerifier(contract *RuntimeContract, key string, config storage.JSONMap) error {
	module := r.modules[moduleRegistryKey(contract.Engine.Key, contract.Engine.Version)]
	if !contains(module.Verifiers, key) {
		return fmt.Errorf("玩法模块不允许验证器 %s", key)
	}
	registered, ok := r.verifiers[key]
	if !ok {
		return fmt.Errorf("验证器未注册: %s", key)
	}
	if err := registered.validate(config); err != nil {
		return err
	}
	if key == "core.conditions" {
		return validateConditionPaths(contract.StateSchema, config)
	}
	return nil
}

func (r *Registry) Verify(key string, evidence Evidence, config storage.JSONMap) error {
	registered, ok := r.verifiers[key]
	if !ok {
		return fmt.Errorf("验证器未注册: %s", key)
	}
	return registered.verify(evidence, config)
}

func moduleRegistryKey(key string, version int) string {
	return fmt.Sprintf("%s@%d", key, version)
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func validateEmptyConfig(config storage.JSONMap) error {
	if config == nil {
		return nil
	}
	return nil
}

func validateConditionsConfig(config storage.JSONMap) error {
	conditions, ok := config["conditions"].([]any)
	if !ok || len(conditions) == 0 {
		return fmt.Errorf("core.conditions 需要 1 到 32 条 conditions")
	}
	count := 0
	for _, raw := range conditions {
		condition, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("condition 必须是对象")
		}
		if err := validateCondition(condition, 0, &count); err != nil {
			return err
		}
	}
	return nil
}

func validateCondition(condition map[string]any, depth int, count *int) error {
	*count++
	if depth > 4 || *count > 32 {
		return fmt.Errorf("core.conditions 条件树过于复杂")
	}
	switch stringValue(condition["type"]) {
	case "state_number":
		if stringValue(condition["path"]) == "" || !validNumberOperator(stringValue(condition["operator"])) {
			return fmt.Errorf("state_number 条件无效")
		}
		if _, ok := numberValue(condition["value"]); !ok {
			return fmt.Errorf("state_number.value 必须是数字")
		}
	case "state_equals":
		if stringValue(condition["path"]) == "" {
			return fmt.Errorf("state_equals.path 不能为空")
		}
	case "state_in":
		if stringValue(condition["path"]) == "" {
			return fmt.Errorf("state_in.path 不能为空")
		}
		values, ok := condition["values"].([]any)
		if !ok || len(values) == 0 || len(values) > 64 {
			return fmt.Errorf("state_in.values 必须包含 1 到 64 个值")
		}
		for index, value := range values {
			if !validConditionScalar(value) {
				return fmt.Errorf("state_in.values 只能包含字符串、布尔值或数字")
			}
			for previous := 0; previous < index; previous++ {
				if conditionScalarsEqual(value, values[previous]) {
					return fmt.Errorf("state_in.values 不能包含重复值")
				}
			}
		}
	case "successful_roll":
		if stringValue(condition["key"]) == "" {
			return fmt.Errorf("successful_roll.key 不能为空")
		}
	case "minimum_turns":
		value, ok := numberValue(condition["value"])
		if !ok || value < 0 {
			return fmt.Errorf("minimum_turns.value 无效")
		}
	case "all", "any":
		children, ok := condition["conditions"].([]any)
		if !ok || len(children) == 0 {
			return fmt.Errorf("%s 条件不能为空", stringValue(condition["type"]))
		}
		for _, rawChild := range children {
			child, ok := rawChild.(map[string]any)
			if !ok {
				return fmt.Errorf("condition 必须是对象")
			}
			if err := validateCondition(child, depth+1, count); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("未知通用条件类型 %q", stringValue(condition["type"]))
	}
	return nil
}

func verifyConditions(evidence Evidence, config storage.JSONMap) error {
	if err := validateConditionsConfig(config); err != nil {
		return err
	}
	for _, raw := range config["conditions"].([]any) {
		if err := verifyCondition(evidence, raw.(map[string]any)); err != nil {
			return err
		}
	}
	return nil
}

func verifyCondition(evidence Evidence, condition map[string]any) error {
	switch stringValue(condition["type"]) {
	case "state_number":
		path := stringValue(condition["path"])
		actualRaw, ok := ValueAtPath(evidence.State, path)
		actual, numeric := numberValue(actualRaw)
		expected, _ := numberValue(condition["value"])
		if !ok || !numeric || !compareNumber(actual, expected, stringValue(condition["operator"])) {
			return fmt.Errorf("状态条件未满足: %s", path)
		}
	case "state_equals":
		path := stringValue(condition["path"])
		actual, ok := ValueAtPath(evidence.State, path)
		if !ok || fmt.Sprint(actual) != fmt.Sprint(condition["value"]) {
			return fmt.Errorf("状态条件未满足: %s", path)
		}
	case "state_in":
		path := stringValue(condition["path"])
		actual, ok := ValueAtPath(evidence.State, path)
		if !ok {
			return fmt.Errorf("状态条件未满足: %s", path)
		}
		matched := false
		for _, expected := range condition["values"].([]any) {
			if conditionScalarsEqual(actual, expected) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("状态条件未满足: %s", path)
		}
	case "successful_roll":
		key := stringValue(condition["key"])
		if !evidence.SuccessfulRollKeys[key] {
			return fmt.Errorf("尚未通过服务端判定: %s", key)
		}
	case "minimum_turns":
		expected, _ := numberValue(condition["value"])
		if int64(evidence.TurnCount) < int64(expected) {
			return fmt.Errorf("回合数尚未达到要求")
		}
	case "all":
		for _, rawChild := range condition["conditions"].([]any) {
			if err := verifyCondition(evidence, rawChild.(map[string]any)); err != nil {
				return err
			}
		}
	case "any":
		var lastErr error
		for _, rawChild := range condition["conditions"].([]any) {
			if err := verifyCondition(evidence, rawChild.(map[string]any)); err == nil {
				return nil
			} else {
				lastErr = err
			}
		}
		return fmt.Errorf("任一条件均未满足: %v", lastErr)
	}
	return nil
}

func validateConditionPaths(schema StateSchema, config storage.JSONMap) error {
	var visit func(map[string]any) error
	visit = func(condition map[string]any) error {
		typeName := stringValue(condition["type"])
		if typeName == "state_number" || typeName == "state_equals" || typeName == "state_in" {
			path := stringValue(condition["path"])
			field, ok := FieldForPath(schema, path)
			if !ok {
				return fmt.Errorf("条件引用了未声明的状态路径: %s", path)
			}
			if typeName == "state_number" &&
				(field.Path != path || (field.Type != "integer" && field.Type != "number")) {
				return fmt.Errorf("state_number 必须引用已声明的数值状态: %s", path)
			}
			if typeName == "state_in" {
				for _, value := range condition["values"].([]any) {
					if !ValidUpdateValue(field, path, value, false) {
						return fmt.Errorf("state_in.values 包含不符合状态字段 %s 的值", path)
					}
				}
			}
		}
		if typeName == "all" || typeName == "any" {
			for _, rawChild := range condition["conditions"].([]any) {
				if err := visit(rawChild.(map[string]any)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, raw := range config["conditions"].([]any) {
		if err := visit(raw.(map[string]any)); err != nil {
			return err
		}
	}
	return nil
}

func stringValue(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func numberValue(value any) (float64, bool) {
	switch number := value.(type) {
	case int:
		return float64(number), true
	case int64:
		return float64(number), true
	case float64:
		return number, true
	case float32:
		return float64(number), true
	default:
		return 0, false
	}
}

func validConditionScalar(value any) bool {
	if _, ok := value.(string); ok {
		return true
	}
	if _, ok := value.(bool); ok {
		return true
	}
	number, ok := numberValue(value)
	return ok && !math.IsNaN(number) && !math.IsInf(number, 0)
}

func conditionScalarsEqual(left, right any) bool {
	leftNumber, leftNumeric := numberValue(left)
	rightNumber, rightNumeric := numberValue(right)
	if leftNumeric || rightNumeric {
		return leftNumeric && rightNumeric && leftNumber == rightNumber
	}
	if leftString, ok := left.(string); ok {
		rightString, rightOK := right.(string)
		return rightOK && leftString == rightString
	}
	if leftBool, ok := left.(bool); ok {
		rightBool, rightOK := right.(bool)
		return rightOK && leftBool == rightBool
	}
	return false
}

func validNumberOperator(operator string) bool {
	switch operator {
	case "gte", "gt", "lte", "lt", "eq":
		return true
	default:
		return false
	}
}

func compareNumber(actual, expected float64, operator string) bool {
	switch operator {
	case "gte":
		return actual >= expected
	case "gt":
		return actual > expected
	case "lte":
		return actual <= expected
	case "lt":
		return actual < expected
	case "eq":
		return actual == expected
	default:
		return false
	}
}
