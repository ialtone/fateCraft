package contract

import (
	"encoding/json"
	"strings"
	"testing"
)

func progressRewardsFixture(definition Definition) Content {
	return Content{
		Runtime:             &RuntimeContract{Engine: EngineRef{Key: "core.generic", Version: 1}, Mode: RuntimeModeChallenge},
		PlatformDefinitions: []Definition{definition},
	}
}

func progressRewardsDefinition(stages []any, outcomeKeys []any, cap int64) Definition {
	return Definition{
		Kind:       DefinitionKindChallengeProgressRewards,
		Key:        "progress-rewards",
		Name:       "挑战阶段积分",
		Visibility: VisibilityPlayer,
		Content: map[string]any{
			"stages":       stages,
			"outcome_keys": outcomeKeys,
			"stage_cap":    cap,
		},
	}
}

func progressStage(key string, order int, points int64) map[string]any {
	return map[string]any{
		"key": key, "name": key, "order": order, "points": points,
		"verifier_key":    "core.conditions",
		"verifier_config": map[string]any{"conditions": []any{map[string]any{"type": "state_in", "path": "score", "values": []any{"done"}}}},
	}
}

func progressSchemas(t *testing.T) Content {
	content := progressRewardsFixture(progressRewardsDefinition(
		[]any{progressStage("stage-a", 10, 5)},
		[]any{"success"}, 5,
	))
	content.Outcomes = []Outcome{{Key: "success", Enabled: true, Category: "success", RewardPoints: 10}}
	content.Runtime.StateSchema = mustStateSchema(t, `{"schema_version":1,"fields":[{"path":"score","type":"string","default":"","visibility":"player","update_policy":"module"}]}`)
	return content
}

func mustStateSchema(t *testing.T, raw string) StateSchema {
	t.Helper()
	var schema StateSchema
	if err := json.Unmarshal([]byte(raw), &schema); err != nil {
		t.Fatal(err)
	}
	return schema
}

func TestProgressRewardsSchemaDeclaresCapBudget(t *testing.T) {
	schema := ProgressRewardsSchema()
	budget, ok := schema["x-reward-budget"].(map[string]any)
	if !ok {
		t.Fatal("missing x-reward-budget")
	}
	for _, key := range []string{"stages", "points", "outcomes", "cap"} {
		if _, ok := budget[key].(string); !ok {
			t.Fatalf("missing budget key %s", key)
		}
	}
	if _, ok := budget["total"]; ok {
		t.Fatal("single-run stage budget must not declare a fixed total")
	}
}

func TestRegistryPublishesPlatformDefinitionToChallengeModules(t *testing.T) {
	registry := NewCoreRegistry()
	module, err := registry.Module(EngineRef{Key: "core.generic", Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := module.DefinitionSchemas[DefinitionKindChallengeProgressRewards]; !ok {
		t.Fatal("platform definition schema not merged into challenge module")
	}
	if module.DefinitionLabels[DefinitionKindChallengeProgressRewards] != "阶段积分" {
		t.Fatalf("unexpected label %q", module.DefinitionLabels[DefinitionKindChallengeProgressRewards])
	}
	// The platform kind must not be treated as a module-owned definition kind.
	if id := DefinitionKindChallengeProgressRewards; !IsPlatformDefinitionKind(id) {
		t.Fatal("platform kind not recognized")
	}
}

func TestValidateDefinitionsRejectsPlatformKindOutsideTrustedRuntime(t *testing.T) {
	registry := NewCoreRegistry()
	content := progressSchemas(t)
	content.Runtime.Mode = "narrative"
	if err := registry.ValidateDefinitions(content); err == nil {
		t.Fatal("non-gameplay content accepted stage rewards")
	}
}

func TestValidateContentEnforcesStageBudgetAgainstLowestSuccess(t *testing.T) {
	registry := NewCoreRegistry()
	valid := progressSchemas(t)
	if err := registry.ValidateContent(valid); err != nil {
		t.Fatalf("valid content rejected: %v", err)
	}

	tooHigh := progressSchemas(t)
	tooHigh.PlatformDefinitions[0] = progressRewardsDefinition(
		[]any{progressStage("stage-a", 10, 6)}, []any{"success"}, 5,
	)
	if err := registry.ValidateContent(tooHigh); err == nil {
		t.Fatal("stage total above cap accepted")
	}

	wrongCap := progressSchemas(t)
	wrongCap.PlatformDefinitions[0] = progressRewardsDefinition(
		[]any{progressStage("stage-a", 10, 5)}, []any{"success"}, 10,
	)
	if err := registry.ValidateContent(wrongCap); err == nil {
		t.Fatal("stage_cap not derived from the lowest success accepted")
	}

	failureOutcome := progressSchemas(t)
	failureOutcome.Outcomes = append(failureOutcome.Outcomes, Outcome{Key: "broken", Enabled: true, Category: "failure", RewardPoints: 0})
	failureOutcome.PlatformDefinitions[0] = progressRewardsDefinition(
		[]any{progressStage("stage-a", 10, 5)}, []any{"broken"}, 5,
	)
	if err := registry.ValidateContent(failureOutcome); err == nil {
		t.Fatal("stage rewards referencing a failure outcome accepted")
	}

	noPositiveSuccess := progressSchemas(t)
	noPositiveSuccess.Outcomes = []Outcome{{Key: "success", Enabled: true, Category: "success", RewardPoints: 0}}
	if err := registry.ValidateContent(noPositiveSuccess); err == nil {
		t.Fatal("stage rewards without a positive success accepted")
	}
}

func TestParseProgressRewardsRejectsDuplicates(t *testing.T) {
	content := progressSchemas(t)
	content.PlatformDefinitions = append(content.PlatformDefinitions, content.PlatformDefinitions[0])
	if _, _, err := ParseProgressRewards(content); err == nil {
		t.Fatal("duplicate platform definition accepted")
	}
}

func TestValidateDefinitionsRejectsDuplicateStageOrder(t *testing.T) {
	registry := NewCoreRegistry()
	content := progressSchemas(t)
	content.PlatformDefinitions[0] = progressRewardsDefinition(
		[]any{progressStage("stage-a", 10, 2), progressStage("stage-b", 10, 3)},
		[]any{"success"}, 5,
	)
	if err := registry.ValidateDefinitions(content); err == nil {
		t.Fatal("duplicate stage order accepted")
	}
}

func TestProgressRewardsRejectsUntrustedAndTerminalState(t *testing.T) {
	for _, tc := range []struct{ path, policy string }{
		{"score", "module"},
		{"score", "model"},
		{"gameplay.outcome_key", "module"},
		{"gameplay.death_cause", "module"},
		{"module_state.status", "module"},
	} {
		t.Run(tc.path+"/"+tc.policy, func(t *testing.T) {
			content := progressSchemas(t)
			content.Runtime.StateSchema = mustStateSchema(t, `{"schema_version":1,"fields":[{"path":"`+tc.path+`","type":"string","default":"","visibility":"player","update_policy":"`+tc.policy+`"}]}`)
			stage := progressStage("stage-a", 10, 5)
			stage["verifier_config"] = map[string]any{"conditions": []any{map[string]any{
				"type": "all", "conditions": []any{map[string]any{"type": "state_equals", "path": tc.path, "value": "done"}},
			}}}
			content.PlatformDefinitions[0] = progressRewardsDefinition([]any{stage}, []any{"success"}, 5)
			err := NewCoreRegistry().ValidateContent(content)
			if wantValid := tc.path == "score" && tc.policy == "module"; (err == nil) != wantValid {
				t.Fatalf("stage condition validation: %v", err)
			}
		})
	}
}

func TestProgressRewardsRejectsStageKeyBeyondStorageLimit(t *testing.T) {
	content := progressSchemas(t)
	content.PlatformDefinitions[0] = progressRewardsDefinition([]any{progressStage(strings.Repeat("a", 65), 10, 5)}, []any{"success"}, 5)
	if err := NewCoreRegistry().ValidateContent(content); err == nil {
		t.Fatal("stage key exceeding storage limit accepted")
	}
}
