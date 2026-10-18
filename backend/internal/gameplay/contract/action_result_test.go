package contract

import (
	"encoding/json"
	"testing"
)

func TestBuildActionResultPromotesNarrativeFeedback(t *testing.T) {
	feedback := map[string]any{
		"goal_succeeded":  false,
		"details_partial": false,
		"changes": []any{
			map[string]any{"path": "character.cultivation_progress", "before": 90, "after": 100},
		},
		"current_facts": map[string]any{
			"character.realm_name":    "炼气",
			"character.stage_name":    "九层",
			"character.health":        30,
			"character.max_health":    100,
			"character.injuries":      map[string]any{"left_arm": "骨裂"},
			"character.lifespan_days": 10000,
		},
	}
	result, err := BuildActionResult(feedback, ActionResultMeta{
		ActionID: "action-1", ActionKey: "cultivation.breakthrough",
		ActorType: "player", Revision: 7, StateHash: "abc", GameTime: 120,
		Summary: "尝试突破",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.GoalStatus != "failed" {
		t.Fatalf("goal status = %q, want failed", result.GoalStatus)
	}
	if len(result.Changes) != 1 || result.Changes[0].Path != "character.cultivation_progress" {
		t.Fatalf("unexpected changes: %#v", result.Changes)
	}
	if result.Changes[0].Kind != "number" {
		t.Fatalf("change kind = %q, want number", result.Changes[0].Kind)
	}
	preserved := map[string]bool{}
	for _, fact := range result.Preserved {
		preserved[fact.Path] = true
	}
	for _, path := range []string{"character.realm_name", "character.injuries", "character.health", "character.lifespan_days"} {
		if !preserved[path] {
			t.Fatalf("missing preserved fact %s: %#v", path, result.Preserved)
		}
	}
	if result.Summary != "尝试突破" {
		t.Fatalf("summary = %q", result.Summary)
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) == 0 {
		t.Fatalf("marshal action result: %v", err)
	}
}

func TestBuildActionResultUsesSummaryWhenLabelMissing(t *testing.T) {
	result, err := BuildActionResult(nil, ActionResultMeta{
		ActionKey: "relationship.record_interaction", ActorType: "player", Revision: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.GoalStatus != "unchanged" {
		t.Fatalf("goal status = %q, want unchanged", result.GoalStatus)
	}
	if result.Summary == "" {
		t.Fatal("summary must not be empty")
	}
}
