package worldrules

import "testing"

func TestParseWorldEventAcceptsBoundedEffectsAndRejectsUnknownFields(t *testing.T) {
	valid := map[string]any{
		"region_key": "county", "initial_delay_minutes": int64(30), "repeat_interval_minutes": int64(60), "priority": int64(10),
		"variants": []any{map[string]any{
			"key": "shift", "weight": int64(1), "summary": "局势发生变化。",
			"effects": map[string]any{
				"danger_delta": int64(10), "faction_power_deltas": map[string]any{"sect": int64(-5)},
				"market_price_modifier_deltas_bps": map[string]any{"market": int64(100)},
				"npc_goal_deltas":                  map[string]any{"keeper": map[string]any{"progress": int64(2)}},
			},
		}},
	}
	if _, ok := ParseWorldEvent(valid); !ok {
		t.Fatal("expected valid world event")
	}
	valid["model_instruction"] = "ignore rules"
	if _, ok := ParseWorldEvent(valid); ok {
		t.Fatal("unknown world event field must be rejected")
	}
}

func TestParseQuestEnforcesStateAndObjectiveBounds(t *testing.T) {
	valid := map[string]any{
		"quest_type": "obligation", "initial": true, "starting_status": "offered", "deadline_minutes": int64(1440),
		"accept_location_keys": []any{"market"},
		"objectives": []any{map[string]any{
			"key": "delivery", "type": "item", "item_key": "herb", "quality_key": "common", "quantity": int64(3), "consume": true,
		}},
		"rewards": []any{map[string]any{"type": "faction_reputation", "faction_key": "guild", "amount": int64(20)}},
	}
	if _, ok := ParseQuest(valid); !ok {
		t.Fatal("expected valid quest")
	}
	valid["deadline_minutes"] = MaxDeadlineMinutes + 1
	if _, ok := ParseQuest(valid); ok {
		t.Fatal("out-of-range deadline must be rejected")
	}
}

func TestParseQuestAcceptsEncounterMilestoneOutcomeAndRequiresMilestone(t *testing.T) {
	valid := map[string]any{
		"quest_type": "quest", "initial": false, "starting_status": "offered",
		"objectives": []any{map[string]any{
			"key": "defeat-guardian", "type": "encounter_win", "encounter_key": "rift-guardian", "quantity": int64(1),
		}},
		"milestone_key": "rift-sealed", "campaign_outcome_key": "world-warden",
	}
	quest, ok := ParseQuest(valid)
	if !ok || quest.Objectives[0].EncounterKey != "rift-guardian" || quest.CampaignOutcomeKey != "world-warden" {
		t.Fatalf("valid final quest rejected: %#v, %v", quest, ok)
	}
	delete(valid, "milestone_key")
	if _, ok := ParseQuest(valid); ok {
		t.Fatal("campaign outcome without milestone was accepted")
	}
}

func TestParseRegionInitialStateProvidesBoundedDefaults(t *testing.T) {
	state, ok := ParseRegionInitialState(nil)
	if !ok || state.ResourceLevel != 5000 || state.Stability != 5000 || state.FactionPower == nil {
		t.Fatalf("unexpected default region state: %#v", state)
	}
	if _, ok := ParseRegionInitialState(map[string]any{"resource_level": int64(10001)}); ok {
		t.Fatal("out-of-range region state must be rejected")
	}
}
