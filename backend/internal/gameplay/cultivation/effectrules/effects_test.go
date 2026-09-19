package effectrules

import "testing"

func TestTechniqueEffectsAccumulateOnlyLearnedLayers(t *testing.T) {
	content := map[string]any{"layer_effects": []any{
		map[string]any{"layer": float64(1), "effects": map[string]any{"cultivation_gain": float64(2)}},
		map[string]any{"layer": float64(3), "effects": map[string]any{"max_mana": float64(10)}},
		map[string]any{"layer": float64(5), "effects": map[string]any{"cultivation_gain": float64(4)}},
	}}
	effects, ok := TechniqueEffects(content, 3)
	if !ok || effects["cultivation_gain"] != 2 || effects["max_mana"] != 10 {
		t.Fatalf("layer effects = %#v, %v", effects, ok)
	}
}

func TestParseEffectsRejectsUnknownOrUnboundedValues(t *testing.T) {
	if _, ok := ParseEffects(map[string]any{"luck": float64(1)}); ok {
		t.Fatal("unknown effect was accepted")
	}
	if _, ok := ParseEffects(map[string]any{"max_health": float64(MaxMagnitude + 1)}); ok {
		t.Fatal("unbounded effect was accepted")
	}
}
