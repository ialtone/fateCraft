package craftingrules

import "testing"

func TestParseAndChooseQualityAreBoundedAndDeterministic(t *testing.T) {
	recipe, ok := Parse(map[string]any{
		"craft_type": "alchemy", "starting_known": true,
		"inputs": []any{map[string]any{
			"quantity": int64(2), "options": []any{map[string]any{"item_key": "herb", "quality_key": "common"}},
		}},
		"craft_minutes": int64(60), "success_chance_bps": int64(7000),
		"mastery_bonus_bps": int64(2000), "technique_key": "alchemy",
		"failure_input_loss_bps": int64(5000),
		"outputs": []any{map[string]any{
			"kind": "item", "key": "pill", "quantity": int64(1),
			"quality_weights": map[string]any{"common": int64(8), "fine": int64(2)},
		}},
	})
	if !ok || SuccessChance(recipe, 5000) != 8000 {
		t.Fatalf("recipe = %#v ok=%v", recipe, ok)
	}
	first, firstOK := ChooseQuality(recipe.Outputs[0].QualityWeights, 9)
	second, secondOK := ChooseQuality(recipe.Outputs[0].QualityWeights, 9)
	if !firstOK || !secondOK || first != second || first != "fine" {
		t.Fatalf("quality was not deterministic: %q %q", first, second)
	}
	invalid := map[string]any{
		"craft_type": "alchemy", "inputs": []any{}, "craft_minutes": int64(1),
		"success_chance_bps": int64(1), "failure_input_loss_bps": int64(0),
		"outputs": []any{}, "untrusted": true,
	}
	if _, ok := Parse(invalid); ok {
		t.Fatal("invalid recipe was accepted")
	}
}
