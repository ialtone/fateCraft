package practicerules

import (
	"testing"

	"github.com/ialtone/fatecraft/internal/storage"
)

func TestCapabilityAndRequestedSessionEvaluationShareBoundary(t *testing.T) {
	slot := "primary"
	facts := Facts{
		Character: &storage.CampaignCharacter{Mana: 11},
		Technique: storage.CampaignTechnique{TechniqueKey: "breathing", Status: "active", EquippedSlot: &slot, LearnedLayer: 1},
		Definition: storage.GameplayDefinition{Content: storage.JSONMap{
			"mana_cost": int64(1), "practice_minutes": int64(60), "cultivation_gain": int64(10), "mastery_gain": int64(25),
			"layer_effects": []any{map[string]any{"layer": int64(1), "effects": map[string]any{}}},
		}},
	}
	capability := Evaluate(facts, nil)
	if !capability.Available || capability.MaximumSessions != 11 {
		t.Fatalf("capability = %#v", capability)
	}
	atMaximum := int64(11)
	if result := Evaluate(facts, &atMaximum); !result.Available {
		t.Fatalf("maximum was rejected: %#v", result)
	}
	overMaximum := int64(12)
	if result := Evaluate(facts, &overMaximum); result.Available || result.ViolationCode != "resource.insufficient" {
		t.Fatalf("over maximum result = %#v", result)
	}
}
