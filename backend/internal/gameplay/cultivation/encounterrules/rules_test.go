package encounterrules

import "testing"

func TestParseAcceptsBoundedEncounterAndDefaultsRewardQuality(t *testing.T) {
	rules, ok := Parse(map[string]any{
		"location_key": "mist-wood", "opponent_key": "mist-beast", "opponent_name": "雾兽",
		"opponent_realm_rank": 1, "opponent_max_health": 80, "opponent_attack": 12,
		"opponent_defense": 4, "player_damage_base": 5, "player_damage_scale_bps": 10_000,
		"player_mana_cost": 2, "round_minutes": 5, "max_rounds": 12, "flee_chance_bps": 2_500,
		"rewards": []any{map[string]any{"item_key": "mist-hide", "quantity": 2}},
	})
	if !ok {
		t.Fatal("expected valid encounter")
	}
	if len(rules.Rewards) != 1 || rules.Rewards[0].QualityKey != "common" {
		t.Fatalf("unexpected normalized rewards: %+v", rules.Rewards)
	}
}

func TestParseRejectsUnknownFieldsAndInvalidBounds(t *testing.T) {
	valid := map[string]any{
		"location_key": "mist-wood", "opponent_key": "mist-beast", "opponent_name": "雾兽",
		"opponent_realm_rank": 1, "opponent_max_health": 80, "opponent_attack": 12,
		"opponent_defense": 4, "player_damage_base": 5, "player_damage_scale_bps": 10_000,
		"player_mana_cost": 2, "round_minutes": 5, "max_rounds": 12, "flee_chance_bps": 2_500,
	}
	withUnknown := cloneMap(valid)
	withUnknown["model_decides_victory"] = true
	if _, ok := Parse(withUnknown); ok {
		t.Fatal("unknown fields must be rejected")
	}
	invalidHealth := cloneMap(valid)
	invalidHealth["opponent_max_health"] = 0
	if _, ok := Parse(invalidHealth); ok {
		t.Fatal("zero opponent health must be rejected")
	}
	duplicateRewards := cloneMap(valid)
	duplicateRewards["rewards"] = []any{
		map[string]any{"item_key": "mist-hide", "quantity": 1},
		map[string]any{"item_key": "mist-hide", "quality_key": "common", "quantity": 1},
	}
	if _, ok := Parse(duplicateRewards); ok {
		t.Fatal("duplicate normalized rewards must be rejected")
	}
}

func cloneMap(source map[string]any) map[string]any {
	clone := make(map[string]any, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}
