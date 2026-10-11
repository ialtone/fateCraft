package contract

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNarrativeFeedbackKeepsFailureCostsAndCoreWithinBudget(t *testing.T) {
	before := map[string]any{"realm": "凡人", "progress": 100, "health": 100, "stone": 10, "inventory": strings.Repeat("large", 4000)}
	after := map[string]any{"realm": "凡人", "progress": 80, "health": 85, "stone": 9, "inventory": strings.Repeat("other", 4000)}
	feedback, err := NarrativeFeedback("trial.breakthrough", 4, 5, before, after, []string{"realm", "progress", "health"}, map[string]any{"success": false}, false)
	if err != nil {
		t.Fatal(err)
	}
	if feedback["execution"] != "committed" || feedback["goal_succeeded"] != false || feedback["details_partial"] != true {
		t.Fatalf("feedback = %v", feedback)
	}
	facts := feedback["current_facts"].(map[string]any)
	if facts["realm"] != "凡人" || facts["progress"] != json.Number("80") {
		t.Fatalf("facts = %v", facts)
	}
	raw, _ := json.Marshal(feedback)
	if len(raw) > MaxNarrativeFeedbackBytes {
		t.Fatal("feedback exceeded budget")
	}
	changes := feedback["changes"].([]map[string]any)
	seen := map[string]bool{}
	for _, change := range changes {
		seen[change["path"].(string)] = true
	}
	if !seen["health"] || !seen["progress"] || !seen["stone"] || seen["realm"] {
		t.Fatalf("wrong action changes: %v", changes)
	}
}

func TestNarrativeFeedbackSeparatesAudienceAndMissingFromZero(t *testing.T) {
	view, partial, err := SharedNarrativeView(
		map[string]any{"health": 0, "qualified": false, "player_only": "secret", "character": map[string]any{"realm": "凡人"}},
		map[string]any{"health": 0, "qualified": false, "internal": "private", "character": map[string]any{"realm": "凡人", "hidden": "private"}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !partial {
		t.Fatal("omission was not declared")
	}
	raw, _ := json.Marshal(view)
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "private") {
		t.Fatalf("audience leak: %s", raw)
	}
	facts, err := SelectNarrativeFacts(view, []string{"health", "qualified", "character.realm", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if facts["health"] != json.Number("0") || facts["qualified"] != false || facts["missing"].(map[string]any)["unavailable"] != true {
		t.Fatalf("facts = %v", facts)
	}
	if _, err := SelectNarrativeFacts(map[string]any{"large": strings.Repeat("x", MaxNarrativeFactsBytes)}, []string{"large"}); err == nil {
		t.Fatal("oversized mandatory core silently accepted")
	}
}

func TestNarrativeFeedbackUsesKeyedDeltasAndNeverTreatsOmissionAsDeletion(t *testing.T) {
	before := map[string]any{"inventory": []any{map[string]any{"key": "stone", "quantity": 10}, map[string]any{"key": "pill", "quantity": 1}}, "optional_topic": "visible"}
	after := map[string]any{"inventory": []any{map[string]any{"key": "pill", "quantity": 1}, map[string]any{"key": "stone", "quantity": 9}}}
	feedback, err := NarrativeFeedback("trial.practice", 1, 2, before, after, nil, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	changes := feedback["changes"].([]map[string]any)
	if len(changes) != 1 || changes[0]["path"] != `inventory["stone"].quantity` || changes[0]["before"] != json.Number("10") || changes[0]["after"] != json.Number("9") || feedback["details_partial"] != true {
		t.Fatalf("invented deletion or copied entire inventory: %v", feedback)
	}
}
