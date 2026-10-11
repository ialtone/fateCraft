package contract

import (
	"testing"
)

func genericTestContract() RuntimeContract {
	return RuntimeContract{
		Engine: EngineRef{Key: "core.generic", Version: 1},
		StateSchema: StateSchema{SchemaVersion: 1, Fields: []StateField{
			{Path: "investigation", Type: "object", Default: map[string]any{}, Visibility: VisibilityPlayer, UpdatePolicy: UpdateModel},
			{Path: "investigation.clue_integrity", Type: "integer", Default: float64(0), Visibility: VisibilityPlayer, UpdatePolicy: UpdateModel},
			{Path: "investigation.status", Type: "enum", Default: "open", Visibility: VisibilityPlayer, UpdatePolicy: UpdateSystem, Constraints: map[string]any{"values": []any{"open", "solved", "closed"}}},
		}},
		Actions:      ActionConfig{Model: []string{"update_state", "roll", "complete_challenge"}},
		Settlement:   SettlementConfig{Handler: "core.challenge_completion"},
		Presentation: Presentation{StatePanelTitle: "调查进展"},
	}
}

func TestCoreConditionsSupportBoundedAllAnyTree(t *testing.T) {
	registry := NewCoreRegistry()
	contract := genericTestContract()
	config := map[string]any{"conditions": []any{
		map[string]any{"type": "all", "conditions": []any{
			map[string]any{"type": "state_number", "path": "investigation.clue_integrity", "operator": "gte", "value": float64(80)},
			map[string]any{"type": "any", "conditions": []any{
				map[string]any{"type": "successful_roll", "key": "investigation.accuse"},
				map[string]any{"type": "minimum_turns", "value": float64(12)},
			}},
		}},
	}}
	if err := registry.ValidateVerifier(&contract, "core.conditions", config); err != nil {
		t.Fatalf("ValidateVerifier() error = %v", err)
	}
	evidence := Evidence{
		State:              map[string]any{"investigation": map[string]any{"clue_integrity": float64(90)}},
		SuccessfulRollKeys: map[string]bool{"investigation.accuse": true},
	}
	if err := registry.Verify("core.conditions", evidence, config); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}

func TestPublishedRulesRejectUnknownOrUntypedNumericStatePaths(t *testing.T) {
	registry := NewCoreRegistry()
	contract := genericTestContract()
	unknown := map[string]any{"conditions": []any{
		map[string]any{"type": "state_number", "path": "investigation.missing", "operator": "gte", "value": float64(1)},
	}}
	if err := registry.ValidateVerifier(&contract, "core.conditions", unknown); err == nil {
		t.Fatal("unknown numeric condition path was accepted")
	}
}

func TestCoreConditionsSupportStateIn(t *testing.T) {
	registry := NewCoreRegistry()
	contract := genericTestContract()
	config := map[string]any{"conditions": []any{
		map[string]any{"type": "state_in", "path": "investigation.status", "values": []any{"solved", "closed"}},
	}}
	if err := registry.ValidateVerifier(&contract, "core.conditions", config); err != nil {
		t.Fatalf("ValidateVerifier() error = %v", err)
	}
	if err := registry.Verify("core.conditions", Evidence{
		State: map[string]any{"investigation": map[string]any{"status": "solved"}},
	}, config); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if err := registry.Verify("core.conditions", Evidence{
		State: map[string]any{"investigation": map[string]any{"status": "open"}},
	}, config); err == nil {
		t.Fatal("state_in accepted a value outside its configured set")
	}
}

func TestCoreConditionsRejectInvalidStateIn(t *testing.T) {
	registry := NewCoreRegistry()
	contract := genericTestContract()
	tests := []map[string]any{
		{"conditions": []any{map[string]any{"type": "state_in", "path": "investigation.missing", "values": []any{"solved"}}}},
		{"conditions": []any{map[string]any{"type": "state_in", "path": "investigation.status", "values": []any{}}}},
		{"conditions": []any{map[string]any{"type": "state_in", "path": "investigation.status", "values": []any{"solved", "solved"}}}},
		{"conditions": []any{map[string]any{"type": "state_in", "path": "investigation.status", "values": []any{"unknown"}}}},
		{"conditions": []any{map[string]any{"type": "state_in", "path": "investigation.status", "values": []any{map[string]any{"unexpected": true}}}}},
	}
	for index, config := range tests {
		if err := registry.ValidateVerifier(&contract, "core.conditions", config); err == nil {
			t.Fatalf("invalid state_in config %d was accepted", index)
		}
	}
}
