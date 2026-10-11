package contract

import "testing"

func TestBuildRollConditionSnapshotsProjectsPublicValues(t *testing.T) {
	contract := &RuntimeContract{
		StateSchema: StateSchema{Fields: []StateField{
			{Path: "profile.realm", Type: "enum", Visibility: VisibilityPlayer},
			{Path: "profile.progress", Type: "number", Visibility: VisibilityPlayer},
			{Path: "secret.score", Type: "number", Visibility: VisibilityInternal},
		}},
		Presentation: Presentation{
			StateSections: []StateSection{{Fields: []StateViewField{
				{Path: "profile.realm", Label: "境界"},
				{Path: "profile.progress", Label: "修为进度"},
			}}},
			ValueLabels: map[string]map[string]string{
				"state.profile.realm": {"qi-refining": "练气期"},
			},
		},
	}
	rule := RollRule{Requires: map[string]any{
		"secret.score":     map[string]any{"min": 1},
		"profile.realm":    "qi-refining",
		"profile.progress": map[string]any{"min": 100},
	}}
	state := map[string]any{
		"profile": map[string]any{"realm": "qi-refining", "progress": float64(100)},
		"secret":  map[string]any{"score": float64(7)},
	}

	conditions := BuildRollConditionSnapshots(contract, rule, state)
	if len(conditions) != 3 {
		t.Fatalf("got %d conditions, want 3", len(conditions))
	}
	if conditions[0].Key != "profile.progress" || conditions[0].DisplayExpected != "至少 100" || conditions[0].DisplayActual != "100" {
		t.Fatalf("unexpected first condition: %#v", conditions[0])
	}
	if conditions[1].DisplayActual != "练气期" || conditions[1].Label != "境界" || conditions[1].Expected != "qi-refining" {
		t.Fatalf("unexpected realm condition: %#v", conditions[1])
	}
	hidden := conditions[2]
	if hidden.Public || hidden.Key != "" || hidden.Actual != nil || hidden.Expected != nil || !hidden.Met {
		t.Fatalf("hidden condition leaked data: %#v", hidden)
	}
}

func TestBuildRollConditionSnapshotsDoesNotUseTechnicalPathAsLabel(t *testing.T) {
	contract := &RuntimeContract{
		StateSchema: StateSchema{Fields: []StateField{{Path: "profile.realm_key", Type: "string", Visibility: VisibilityPlayer}}},
		Presentation: Presentation{ValueLabels: map[string]map[string]string{
			"state.profile.realm_key": {"foundation": "筑基期"},
		}},
	}
	conditions := BuildRollConditionSnapshots(contract, RollRule{Requires: map[string]any{"profile.realm_key": "foundation"}}, map[string]any{
		"profile": map[string]any{"realm_key": "foundation"},
	})
	if len(conditions) != 1 || conditions[0].Label != "前置条件" || conditions[0].Key != "" || conditions[0].DisplayActual != "筑基期" {
		t.Fatalf("technical path was exposed as label: %#v", conditions)
	}
}

func TestBuildRollConditionSnapshotsFormatsAnyOfAndIsStable(t *testing.T) {
	contract := &RuntimeContract{
		StateSchema: StateSchema{Fields: []StateField{{Path: "profile.realm", Type: "enum", Visibility: VisibilityPlayer}}},
		Presentation: Presentation{ValueLabels: map[string]map[string]string{
			"state.profile.realm": {"foundation": "筑基期", "golden-core": "结丹期"},
		}},
	}
	rule := RollRule{Requires: map[string]any{
		"profile.realm": map[string]any{"any_of": []any{"golden-core", "foundation"}},
	}}
	conditions := BuildRollConditionSnapshots(contract, rule, map[string]any{"profile": map[string]any{"realm": "foundation"}})
	if len(conditions) != 1 || conditions[0].DisplayExpected != "以下之一：结丹期、筑基期" || !conditions[0].Met {
		t.Fatalf("unexpected any_of condition: %#v", conditions)
	}
}
