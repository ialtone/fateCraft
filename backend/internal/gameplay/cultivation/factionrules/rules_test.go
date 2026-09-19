package factionrules

import "testing"

func TestDerivePermissionsGrantsAndRevokesAtThresholds(t *testing.T) {
	content := map[string]any{"permission_thresholds": map[string]any{
		"guest_access":      float64(-50),
		"outer_gate_access": float64(100),
	}}

	permissions := DerivePermissions(content, 99)
	if !IsGranted(permissions, "guest_access") || IsGranted(permissions, "outer_gate_access") {
		t.Fatalf("permissions below threshold = %#v", permissions)
	}
	permissions = DerivePermissions(content, 100)
	if !IsGranted(permissions, "outer_gate_access") {
		t.Fatalf("permission was not granted at threshold: %#v", permissions)
	}
	permissions = DerivePermissions(content, -100)
	if len(permissions) != 0 {
		t.Fatalf("permissions were not revoked after reputation loss: %#v", permissions)
	}
}

func TestEntryRequirementsRejectsUnboundedOrMalformedDefinitions(t *testing.T) {
	valid := map[string]any{"entry_requirements": []any{map[string]any{
		"faction_key": "green-mist-sect", "permission_key": "outer_gate_access",
	}}}
	requirements, ok := EntryRequirements(valid)
	if !ok || len(requirements) != 1 || requirements[0].FactionKey != "green-mist-sect" {
		t.Fatalf("valid requirements = %#v, %v", requirements, ok)
	}
	requirements, ok = EntryRequirements(map[string]any{"entry_requirements": []any{map[string]any{"min_realm_rank": int64(3)}}})
	if !ok || len(requirements) != 1 || requirements[0].MinRealmRank != 3 {
		t.Fatalf("valid realm requirement = %#v, %v", requirements, ok)
	}
	if _, ok := EntryRequirements(map[string]any{"entry_requirements": []any{map[string]any{"min_realm_rank": int64(0)}}}); ok {
		t.Fatal("zero realm requirement was accepted")
	}

	tooMany := make([]any, MaxEntryRequirements+1)
	for index := range tooMany {
		tooMany[index] = map[string]any{
			"faction_key": "faction", "permission_key": "access_" + string(rune('a'+index)),
		}
	}
	if _, ok := EntryRequirements(map[string]any{"entry_requirements": tooMany}); ok {
		t.Fatal("unbounded requirements were accepted")
	}
}

func TestPositionUsesContributionIndependentlyFromReputation(t *testing.T) {
	content := map[string]any{"position_thresholds": map[string]any{
		"outer_disciple": float64(1000), "inner_disciple": float64(5000),
	}}
	if got := Position(content, 999); got != "outsider" {
		t.Fatalf("position below threshold = %s", got)
	}
	if got := Position(content, 5000); got != "inner_disciple" {
		t.Fatalf("position at threshold = %s", got)
	}
	if got := ReputationLevel(-1000); got != "hostile" {
		t.Fatalf("reputation level = %s", got)
	}
}
