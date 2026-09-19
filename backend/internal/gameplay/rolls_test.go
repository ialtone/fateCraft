package gameplay

import (
	"strings"
	"testing"
)

func rollsTestContract() *RuntimeContract {
	return &RuntimeContract{
		Engine: EngineRef{Key: "cultivation", Version: 1},
		StateSchema: StateSchema{SchemaVersion: 1, Fields: []StateField{
			{Path: "profile", Type: "object", Visibility: VisibilityPlayer, UpdatePolicy: UpdateModel, AllowChildren: true},
			{Path: "profile.cultivation_level", Type: "enum", Default: "凡人", Visibility: VisibilityPlayer, UpdatePolicy: UpdateSystem,
				Constraints: map[string]any{"values": []any{"凡人", "练气期", "筑基期"}}},
			{Path: "profile.lifespan", Type: "number", Default: float64(80), Visibility: VisibilityPlayer, UpdatePolicy: UpdateSystem, Constraints: map[string]any{"min": float64(0)}},
			{Path: "profile.progress", Type: "number", Default: float64(0), Visibility: VisibilityPlayer, UpdatePolicy: UpdateModel, Constraints: map[string]any{"min": float64(0), "max": float64(100)}},
			{Path: "locked", Type: "string", Default: "", Visibility: VisibilityInternal, UpdatePolicy: UpdateImmutable},
		}},
		Actions: ActionConfig{
			Model: []string{"update_state", "roll"},
			Rolls: map[string]RollRule{
				"cultivation.breakthrough.foundation": {
					Category: "cultivation", BaseSuccessRate: 75, Sides: 100,
					Requires: map[string]any{"profile.cultivation_level": "练气期"},
					Success:  map[string]any{"profile.cultivation_level": "筑基期", "profile.lifespan": float64(200)},
				},
			},
		},
		Settlement: SettlementConfig{Handler: "core.challenge_completion"},
	}
}

func testRollRule() RollRule {
	return RollRule{Category: "cultivation", BaseSuccessRate: 50, Sides: 100}
}

func TestValidateContractAcceptsRollRules(t *testing.T) {
	if err := ValidateContract(rollsTestContract()); err != nil {
		t.Fatalf("valid roll rules rejected: %v", err)
	}
}

func TestValidateContractRejectsBadRollRules(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(c *RuntimeContract)
		expect string
	}{
		{"unknown require path", func(c *RuntimeContract) {
			rule := testRollRule()
			rule.Requires = map[string]any{"nope.path": "值"}
			c.Actions.Rolls["x"] = rule
		}, "未知路径"},
		{"bad any_of", func(c *RuntimeContract) {
			rule := testRollRule()
			rule.Requires = map[string]any{"profile.cultivation_level": map[string]any{"any_of": []any{}}}
			c.Actions.Rolls["x"] = rule
		}, "any_of"},
		{"effect on undeclared path", func(c *RuntimeContract) {
			rule := testRollRule()
			rule.Success = map[string]any{"nope.path": true}
			c.Actions.Rolls["x"] = rule
		}, "未在 state_schema 声明"},
		{"effect on immutable path", func(c *RuntimeContract) {
			rule := testRollRule()
			rule.Success = map[string]any{"locked": "值"}
			c.Actions.Rolls["x"] = rule
		}, "不允许服务端写入"},
		{"effect value violates enum", func(c *RuntimeContract) {
			rule := testRollRule()
			rule.Success = map[string]any{"profile.cultivation_level": "散仙"}
			c.Actions.Rolls["x"] = rule
		}, "不符合类型或约束"},
		{"missing category", func(c *RuntimeContract) {
			rule := testRollRule()
			rule.Category = ""
			c.Actions.Rolls["x"] = rule
		}, "category 不能为空"},
		{"invalid base rate", func(c *RuntimeContract) {
			rule := testRollRule()
			rule.BaseSuccessRate = 100
			c.Actions.Rolls["x"] = rule
		}, "base_success_rate"},
		{"invalid sides", func(c *RuntimeContract) {
			rule := testRollRule()
			rule.Sides = 1
			c.Actions.Rolls["x"] = rule
		}, "sides"},
		{"negative reward", func(c *RuntimeContract) {
			rule := testRollRule()
			rule.RewardValue = -1
			c.Actions.Rolls["x"] = rule
		}, "reward_value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			contract := rollsTestContract()
			tt.mutate(contract)
			err := ValidateContract(contract)
			if err == nil || !strings.Contains(err.Error(), tt.expect) {
				t.Fatalf("err = %v, want contains %q", err, tt.expect)
			}
		})
	}
}

func TestCheckRollRequires(t *testing.T) {
	rule := RollRule{Requires: map[string]any{"profile.cultivation_level": "练气期"}}
	pass := map[string]any{"profile": map[string]any{"cultivation_level": "练气期"}}
	if err := CheckRollRequires(rule, pass); err != nil {
		t.Fatalf("should pass: %v", err)
	}
	fail := map[string]any{"profile": map[string]any{"cultivation_level": "凡人"}}
	err := CheckRollRequires(rule, fail)
	if err == nil || !strings.Contains(err.Error(), "练气期") || !strings.Contains(err.Error(), "凡人") {
		t.Fatalf("rejection should name current and required values: %v", err)
	}

	anyOf := RollRule{Requires: map[string]any{"profile.cultivation_level": map[string]any{"any_of": []any{"筑基期", "练气期"}}}}
	if err := CheckRollRequires(anyOf, pass); err != nil {
		t.Fatalf("any_of should pass: %v", err)
	}
	if err := CheckRollRequires(anyOf, fail); err == nil {
		t.Fatal("any_of should reject 凡人")
	}

	numeric := RollRule{Requires: map[string]any{"profile.progress": map[string]any{"min": float64(90)}}}
	if err := CheckRollRequires(numeric, map[string]any{"profile": map[string]any{"progress": float64(95)}}); err != nil {
		t.Fatalf("min bound should pass: %v", err)
	}
	if err := CheckRollRequires(numeric, map[string]any{"profile": map[string]any{"progress": float64(10)}}); err == nil {
		t.Fatal("min bound should reject 10")
	}

	missing := RollRule{Requires: map[string]any{"profile.progress": map[string]any{"min": float64(1)}}}
	if err := CheckRollRequires(missing, map[string]any{}); err == nil {
		t.Fatal("missing state should reject")
	}
}
