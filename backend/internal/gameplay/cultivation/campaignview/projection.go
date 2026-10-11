package campaignview

import (
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldview"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/ascensionrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/effectrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/encounterrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/practicerules"
	"github.com/ialtone/fatecraft/internal/storage"
	"sort"
)

func publicDomainSnapshot(row storage.Campaign, state *storage.CampaignDomainState) map[string]any {
	if state == nil {
		state = &storage.CampaignDomainState{}
	}
	return map[string]any{
		"character":            characterView(state),
		"realm_system":         realmProgressionView(state),
		"techniques":           techniqueViews(state, 0),
		"inventory":            cultivationInventoryView(state, 0),
		"recipes":              recipeViews(row, state, 0),
		"markets":              worldview.MarketViews(row, state, 0),
		"world":                worldview.WorldView(row, state, false),
		"relationships":        worldview.RelationshipViews(row, state, false, 0),
		"factions":             worldview.FactionViews(state, 0),
		"region_state":         worldview.CurrentRegionView(row, state),
		"quests":               worldview.QuestViews(row, state, "quest", false, 0),
		"obligations":          worldview.QuestViews(row, state, "obligation", false, 0),
		"encounters":           encounterViews(state, false, 20),
		"available_encounters": availableEncounterViews(row, state, 20),
	}
}

func addDomainProjection(projection map[string]any, row storage.Campaign, state *storage.CampaignDomainState) {
	addDomainProjectionForQuery(projection, row, state, "")
}

func addDomainProjectionForQuery(projection map[string]any, row storage.Campaign, state *storage.CampaignDomainState, query string) {
	if state == nil {
		return
	}
	projection["character"] = characterView(state)
	projection["realm_system"] = realmProgressionView(state)
	projection["techniques"] = techniqueViews(state, 16)
	projection["inventory"] = cultivationInventoryView(state, 32)
	projection["recipes"] = recipeViews(row, state, 12)
	projection["markets"] = worldview.MarketViews(row, state, 64)
	projection["world"] = worldview.WorldView(row, state, true)
	projection["relationships"] = worldview.RelationshipViews(row, state, true, 12)
	projection["referenced_relationships"] = worldview.ReferencedRelationshipViews(row, state, query, 4)
	projection["current_npc_private_context"] = worldview.PrivateNPCViews(row, state, 8)
	projection["available_npc_archetypes"] = worldview.SpawnableNPCArchetypeViews(row, state, 12)
	projection["factions"] = worldview.FactionViews(state, 12)
	projection["region_state"] = worldview.CurrentRegionView(row, state)
	projection["quests"] = worldview.QuestViews(row, state, "quest", true, 12)
	projection["obligations"] = worldview.QuestViews(row, state, "obligation", true, 12)
	projection["encounters"] = encounterViews(state, true, 4)
	projection["available_encounters"] = availableEncounterViews(row, state, 8)
}

func availableEncounterViews(row storage.Campaign, state *storage.CampaignDomainState, limit int) []map[string]any {
	out := make([]map[string]any, 0)
	ascension, _ := ascensionrules.FromState(state)
	for _, definition := range state.Definitions {
		if ascension != nil && definition.Key == ascension.EncounterKey {
			continue
		}
		if definition.Kind != storage.DefinitionEncounter || definition.Visibility == storage.CampaignVisibilityInternal {
			continue
		}
		rules, ok := encounterrules.Parse(map[string]any(definition.Content))
		if !ok || rules.LocationKey != row.CurrentLocationKey {
			continue
		}
		out = append(out, map[string]any{
			"key": definition.Key, "name": definition.Name, "location_key": rules.LocationKey,
			"opponent_key": rules.OpponentKey, "opponent_name": rules.OpponentName,
			"opponent_realm_rank": rules.OpponentRealmRank, "opponent_max_health": rules.OpponentMaxHealth,
			"round_minutes": rules.RoundMinutes, "max_rounds": rules.MaxEncounterRounds,
			"flee_chance_bps": rules.FleeChanceBPS,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i]["key"].(string) < out[j]["key"].(string) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func encounterViews(state *storage.CampaignDomainState, compact bool, limit int) []map[string]any {
	rows := state.Encounters
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		view := map[string]any{
			"id": row.ID, "key": row.EncounterKey, "location_key": row.LocationKey,
			"opponent_key": row.OpponentKey, "opponent_name": row.OpponentName,
			"opponent_realm_rank": row.OpponentRealmRank, "opponent_health": row.OpponentHealth,
			"opponent_max_health": row.OpponentMaxHealth, "round": row.Round,
			"max_rounds": row.MaxRounds, "status": row.Status, "last_move": row.LastMove,
			"started_at_game_time": row.StartedAtGameTime,
		}
		if !compact || row.Status == "active" {
			view["opponent_attack"] = row.OpponentAttack
			view["opponent_defense"] = row.OpponentDefense
		}
		out = append(out, view)
	}
	return out
}

func characterView(state *storage.CampaignDomainState) any {
	if state.Character == nil {
		return nil
	}
	character := *state.Character
	realmName := character.RealmKey
	if definition, ok := state.Definition(storage.DefinitionRealm, character.RealmKey); ok {
		realmName = definition.Name
	}
	activeEffects := activeCharacterEffects(state)
	stageName := character.StageKey
	if stages, labels, ok := realmStages(state, character.RealmKey); ok {
		for index, stage := range stages {
			if stage == character.StageKey && index < len(labels) {
				stageName = labels[index]
				break
			}
		}
	}
	return map[string]any{
		"id": character.ID, "name": character.Name, "origin": character.Origin,
		"public_identity": character.PublicIdentity,
		"realm_key":       character.RealmKey, "realm_name": realmName,
		"stage_key": character.StageKey, "stage_name": stageName, "realm_rank": character.RealmRank,
		"cultivation_progress": character.CultivationProgress,
		"cultivation_required": character.CultivationRequired,
		"foundation_quality":   character.FoundationQuality,
		"age_days":             character.AgeDays, "lifespan_days": character.LifespanDays,
		"health": character.Health, "base_max_health": character.BaseMaxHealth, "max_health": character.MaxHealth,
		"mana": character.Mana, "base_max_mana": character.BaseMaxMana, "max_mana": character.MaxMana,
		"spiritual_sense":          character.SpiritualSense,
		"base_max_spiritual_sense": character.BaseMaxSpiritualSense,
		"max_spiritual_sense":      character.MaxSpiritualSense,
		"injuries":                 character.Injuries, "long_term_effects": character.LongTermEffects,
		"active_effects": activeEffects,
	}
}

func realmStages(state *storage.CampaignDomainState, realmKey string) ([]string, []string, bool) {
	definition, ok := state.Definition(storage.DefinitionRealm, realmKey)
	if !ok {
		return nil, nil, false
	}
	stages := stringSlice(definition.Content["stages"])
	labels := stringSlice(definition.Content["stage_labels"])
	return stages, labels, len(stages) > 0 && len(stages) == len(labels)
}

func stringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		if typed, ok := value.([]string); ok {
			return typed
		}
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

// realmProgressionView exposes the active玩法's authoritative realm catalog.
// The generic platform does not assume any particular cultivation hierarchy;
// each challenge supplies its own ordered realm definitions.
func realmProgressionView(state *storage.CampaignDomainState) []map[string]any {
	if state == nil {
		return []map[string]any{}
	}
	rows := make([]storage.GameplayDefinition, 0)
	for _, definition := range state.Definitions {
		if definition.Kind == storage.DefinitionRealm && definition.Visibility != storage.CampaignVisibilityInternal {
			rows = append(rows, definition)
		}
	}
	sort.Slice(rows, func(left, right int) bool {
		leftRank, _ := worldview.ProjectionInt64(rows[left].Content["rank"])
		rightRank, _ := worldview.ProjectionInt64(rows[right].Content["rank"])
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return rows[left].Key < rows[right].Key
	})
	out := make([]map[string]any, 0, len(rows))
	for _, definition := range rows {
		view := map[string]any{"key": definition.Key, "name": definition.Name}
		for _, field := range []string{
			"rank", "stages", "stage_labels", "cultivation_required", "lifespan_days",
			"next_realm_key", "post_realm_transition",
		} {
			if value, ok := definition.Content[field]; ok {
				view[field] = value
			}
		}
		out = append(out, view)
	}
	return out
}

func techniqueViews(state *storage.CampaignDomainState, limit int) []map[string]any {
	activeEffects, _ := effectrules.Equipped(state)
	rows := state.Techniques
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		name := row.TechniqueKey
		if definition, ok := state.Definition(storage.DefinitionTechnique, row.TechniqueKey); ok {
			name = definition.Name
		}
		view := map[string]any{
			"id": row.ID, "key": row.TechniqueKey, "name": name, "category": row.Category,
			"learned_layer": row.LearnedLayer, "mastery": row.Mastery, "insight": row.Insight,
			"equipped_slot": row.EquippedSlot, "status": row.Status,
		}
		if definition, ok := state.Definition(storage.DefinitionTechnique, row.TechniqueKey); ok {
			effects, _ := effectrules.MasteredEffects(definition.Content, row.Mastery, row.LearnedLayer)
			view["layer_effects"] = effectMapView(effects)
			view["practice"] = techniquePracticeView(state.Character, row, definition, activeEffects)
		}
		out = append(out, view)
	}
	return out
}

func techniquePracticeView(
	character *storage.CampaignCharacter,
	technique storage.CampaignTechnique,
	definition storage.GameplayDefinition,
	effects map[string]int64,
) map[string]any {
	rules := practicerules.Evaluate(practicerules.Facts{
		Character: character, Technique: technique, Definition: definition, EquippedEffects: effects,
	}, nil)
	view := map[string]any{
		"available": rules.Available, "max_sessions": rules.MaximumSessions,
		"mana_cost_per_session":        rules.ManaCostPerSession,
		"practice_minutes_per_session": rules.PracticeMinutesPerSession,
		"cultivation_gain_per_session": rules.CultivationGainPerSession,
		"mastery_gain_per_session":     rules.MasteryGainPerSession,
	}
	if !rules.Available {
		view["unavailable_reason"] = rules.ViolationMessage
	}
	return view
}

func activeCharacterEffects(state *storage.CampaignDomainState) map[string]int64 {
	effects, _ := effectrules.Equipped(state)
	return effects
}

func effectMapView(effects map[string]int64) map[string]int64 {
	if effects == nil {
		return map[string]int64{}
	}
	return effects
}

func cultivationInventoryView(state *storage.CampaignDomainState, limit int) map[string]any {
	result := worldview.InventoryView(state, limit)
	for _, view := range result["instances"].([]map[string]any) {
		key, _ := view["definition_key"].(string)
		if d, ok := state.Definition(storage.DefinitionEquipment, key); ok {
			bonuses, _ := effectrules.EquipmentBonuses(d.Content)
			view["bonuses"] = effectMapView(bonuses)
		}
	}
	return result
}
