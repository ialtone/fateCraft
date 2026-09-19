package campaignview

import (
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/effectrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/encounterrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/factionrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/gatheringrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/practicerules"
	"github.com/ialtone/fatecraft/internal/storage"
)

func publicDomainSnapshot(row storage.Campaign, state *storage.CampaignDomainState) map[string]any {
	if state == nil {
		state = &storage.CampaignDomainState{}
	}
	return map[string]any{
		"campaign":             row,
		"character":            characterView(state),
		"realm_system":         realmProgressionView(state),
		"techniques":           techniqueViews(state, 0),
		"inventory":            inventoryView(state, 0),
		"recipes":              recipeViews(row, state, 0),
		"markets":              marketViews(row, state, 0),
		"world":                worldView(row, state, false),
		"relationships":        relationshipViews(row, state, false, 0),
		"factions":             factionViews(state, 0),
		"region_state":         currentRegionView(row, state),
		"quests":               questViews(row, state, "quest", false, 0),
		"obligations":          questViews(row, state, "obligation", false, 0),
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
	projection["inventory"] = inventoryView(state, 32)
	projection["recipes"] = recipeViews(row, state, 12)
	projection["markets"] = marketViews(row, state, 64)
	projection["world"] = worldView(row, state, true)
	projection["relationships"] = relationshipViews(row, state, true, 12)
	projection["referenced_relationships"] = referencedRelationshipViews(row, state, query, 4)
	projection["current_npc_private_context"] = privateNPCViews(row, state, 8)
	projection["available_npc_archetypes"] = spawnableNPCArchetypeViews(row, state, 12)
	projection["factions"] = factionViews(state, 12)
	projection["region_state"] = currentRegionView(row, state)
	projection["quests"] = questViews(row, state, "quest", true, 12)
	projection["obligations"] = questViews(row, state, "obligation", true, 12)
	projection["encounters"] = encounterViews(state, true, 4)
	projection["available_encounters"] = availableEncounterViews(row, state, 8)
}

func availableEncounterViews(row storage.Campaign, state *storage.CampaignDomainState, limit int) []map[string]any {
	out := make([]map[string]any, 0)
	for _, definition := range state.Definitions {
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

func referencedRelationshipViews(
	row storage.Campaign,
	state *storage.CampaignDomainState,
	query string,
	limit int,
) []map[string]any {
	query = strings.TrimSpace(query)
	if query == "" {
		return []map[string]any{}
	}
	relations := map[uuid.UUID]storage.CampaignRelationship{}
	for _, relation := range state.Relationships {
		relations[relation.NPCID] = relation
	}
	out := make([]map[string]any, 0)
	for _, npc := range state.NPCs {
		if npc.CurrentLocationKey == row.CurrentLocationKey ||
			!strings.Contains(query, npc.StableName) && !strings.Contains(query, npc.InstanceKey) {
			continue
		}
		relation, known := relations[npc.ID]
		if !known {
			continue
		}
		out = append(out, relationshipView(npc, &relation))
		if limit > 0 && len(out) >= limit {
			break
		}
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
		leftRank, _ := projectionInt64(rows[left].Content["rank"])
		rightRank, _ := projectionInt64(rows[right].Content["rank"])
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
			effects, _ := effectrules.TechniqueEffects(definition.Content, row.LearnedLayer)
			view["layer_effects"] = effectMapView(effects)
			view["practice"] = techniquePracticeView(state.Character, row, definition, effects)
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
	_ = effects
	rules := practicerules.Evaluate(practicerules.Facts{
		Character: character, Technique: technique, Definition: definition,
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

func inventoryView(state *storage.CampaignDomainState, limit int) map[string]any {
	stacks := state.ItemStacks
	instances := state.ItemInstances
	if limit > 0 && len(stacks) > limit {
		stacks = stacks[:limit]
	}
	if limit > 0 && len(instances) > limit {
		instances = instances[:limit]
	}
	stackViews := make([]map[string]any, 0, len(stacks))
	for _, row := range stacks {
		name := row.ItemKey
		if definition, ok := state.Definition(storage.DefinitionItem, row.ItemKey); ok {
			name = definition.Name
		}
		stackViews = append(stackViews, map[string]any{
			"item_key": row.ItemKey, "name": name, "quality_key": row.QualityKey,
			"quantity": row.Quantity, "metadata": row.Metadata,
		})
	}
	instanceViews := make([]map[string]any, 0, len(instances))
	for _, row := range instances {
		name := row.DefinitionKey
		if definition, ok := state.Definition(storage.DefinitionEquipment, row.DefinitionKey); ok {
			name = definition.Name
		}
		view := map[string]any{
			"id": row.ID, "definition_key": row.DefinitionKey, "name": name,
			"quality_key": row.QualityKey, "state": row.State, "equipped_slot": row.EquippedSlot,
			"refinement": row.Refinement, "durability": row.Durability,
			"max_durability": row.MaxDurability, "properties": row.Properties,
		}
		if definition, ok := state.Definition(storage.DefinitionEquipment, row.DefinitionKey); ok {
			bonuses, _ := effectrules.EquipmentBonuses(definition.Content)
			view["bonuses"] = effectMapView(bonuses)
		}
		instanceViews = append(instanceViews, view)
	}
	return map[string]any{"stacks": stackViews, "instances": instanceViews}
}

func activeCharacterEffects(state *storage.CampaignDomainState) map[string]int64 {
	effects := map[string]int64{}
	for _, technique := range state.Techniques {
		if technique.Status != "active" || technique.EquippedSlot == nil {
			continue
		}
		definition, ok := state.Definition(storage.DefinitionTechnique, technique.TechniqueKey)
		if !ok {
			continue
		}
		current, valid := effectrules.TechniqueEffects(definition.Content, technique.LearnedLayer)
		if valid {
			effectrules.Add(effects, current)
		}
	}
	for _, item := range state.ItemInstances {
		if item.State != "equipped" || item.EquippedSlot == nil || item.Durability <= 0 {
			continue
		}
		definition, ok := state.Definition(storage.DefinitionEquipment, item.DefinitionKey)
		if !ok {
			continue
		}
		bonuses, valid := effectrules.EquipmentBonuses(definition.Content)
		if valid {
			effectrules.Add(effects, bonuses)
		}
	}
	return effects
}

func effectMapView(effects map[string]int64) map[string]int64 {
	if effects == nil {
		return map[string]int64{}
	}
	return effects
}

func worldView(row storage.Campaign, state *storage.CampaignDomainState, compact bool) map[string]any {
	discovered := map[string]storage.CampaignLocationDiscovery{}
	for _, discovery := range state.Discoveries {
		discovered[discovery.LocationKey] = discovery
	}
	locations := make([]map[string]any, 0, len(state.Discoveries))
	for _, discovery := range state.Discoveries {
		definition, ok := state.Definition(storage.DefinitionLocation, discovery.LocationKey)
		if !ok {
			continue
		}
		locations = append(locations, map[string]any{
			"key": definition.Key, "name": definition.Name, "visited": discovery.Visited,
			"node_type": locationNodeType(definition.Content), "parent_key": definition.Content["parent_key"],
			"knowledge": discovery.Knowledge,
		})
	}
	current := map[string]any{"key": row.CurrentLocationKey, "name": row.CurrentLocationKey}
	routes := []map[string]any{}
	if definition, ok := state.Definition(storage.DefinitionLocation, row.CurrentLocationKey); ok {
		current["name"] = definition.Name
		current["content"] = publicDefinitionContent(definition.Content, compact)
		current["gathering"] = gatheringResourceViews(state, definition.Content)
		connections, _ := definition.Content["connections"].([]any)
		for _, raw := range connections {
			connection, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			key, _ := connection["location_key"].(string)
			if _, known := discovered[key]; !known {
				continue
			}
			name := key
			target, targetExists := state.Definition(storage.DefinitionLocation, key)
			if targetExists {
				name = target.Name
			}
			accessible, missingRequirements := routeAccessView(state, target, targetExists)
			routes = append(routes, map[string]any{
				"location_key": key, "name": name, "travel_minutes": connection["travel_minutes"],
				"accessible": accessible, "missing_requirements": missingRequirements,
			})
		}
	}
	if compact && len(locations) > 12 {
		locations = locations[:12]
	}
	return map[string]any{
		"current": current, "current_path": currentLocationPath(state, row.CurrentLocationKey),
		"available_routes": routes, "discovered": locations,
	}
}

func gatheringResourceViews(state *storage.CampaignDomainState, content storage.JSONMap) []map[string]any {
	table, ok := gatheringrules.Parse(content["gathering"])
	if !ok {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(table.Entries))
	for _, entry := range table.Entries {
		name := entry.ItemKey
		if definition, ok := state.Definition(storage.DefinitionItem, entry.ItemKey); ok {
			name = definition.Name
		}
		out = append(out, map[string]any{
			"item_key": entry.ItemKey, "name": name, "quality_key": entry.QualityKey,
			"min_quantity": entry.MinQuantity, "max_quantity": entry.MaxQuantity, "minutes": table.Minutes,
			"risk_chance_bps": table.RiskChanceBPS, "risk_health_damage": table.RiskHealthDamage,
		})
	}
	return out
}

func currentLocationPath(state *storage.CampaignDomainState, currentKey string) []map[string]any {
	reversed := make([]map[string]any, 0, 4)
	seen := map[string]bool{}
	for key := currentKey; key != "" && len(reversed) < 4 && !seen[key]; {
		seen[key] = true
		definition, ok := state.Definition(storage.DefinitionLocation, key)
		if !ok {
			break
		}
		reversed = append(reversed, map[string]any{
			"key": definition.Key, "name": definition.Name,
			"node_type": locationNodeType(definition.Content),
		})
		key, _ = definition.Content["parent_key"].(string)
	}
	path := make([]map[string]any, len(reversed))
	for index := range reversed {
		path[len(reversed)-1-index] = reversed[index]
	}
	return path
}

func locationNodeType(content storage.JSONMap) string {
	if value, ok := content["node_type"].(string); ok && value != "" {
		return value
	}
	return "location"
}

func routeAccessView(
	state *storage.CampaignDomainState,
	target storage.GameplayDefinition,
	targetExists bool,
) (bool, []map[string]any) {
	if !targetExists {
		return false, []map[string]any{}
	}
	requirements, valid := factionrules.EntryRequirements(target.Content)
	if !valid {
		return false, []map[string]any{}
	}
	standings := map[string]storage.CampaignFactionStanding{}
	for _, standing := range state.Factions {
		standings[standing.FactionKey] = standing
	}
	missing := make([]map[string]any, 0, len(requirements))
	for _, requirement := range requirements {
		if requirement.MinRealmRank > 0 {
			currentRank := 0
			if state.Character != nil {
				currentRank = state.Character.RealmRank
			}
			if currentRank < requirement.MinRealmRank {
				missing = append(missing, map[string]any{
					"type": "realm", "required_realm_rank": requirement.MinRealmRank, "current_realm_rank": currentRank,
				})
			}
			continue
		}
		standing, exists := standings[requirement.FactionKey]
		if exists && factionrules.IsGranted(standing.Permissions, requirement.PermissionKey) {
			continue
		}
		factionName := requirement.FactionKey
		requiredReputation := 0
		if definition, ok := state.Definition(storage.DefinitionFaction, requirement.FactionKey); ok {
			factionName = definition.Name
			requiredReputation, _ = factionrules.PermissionThreshold(definition.Content, requirement.PermissionKey)
		}
		currentReputation := 0
		if exists {
			currentReputation = standing.Reputation
		}
		missing = append(missing, map[string]any{
			"type": "faction", "faction_key": requirement.FactionKey, "faction_name": factionName,
			"permission_key":      requirement.PermissionKey,
			"required_reputation": requiredReputation, "current_reputation": currentReputation,
		})
	}
	return len(missing) == 0, missing
}

func publicDefinitionContent(content storage.JSONMap, compact bool) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"description", "parent_key", "realm_layer", "danger_rank", "spiritual_energy", "facilities"} {
		if value, ok := content[key]; ok {
			out[key] = value
		}
	}
	if !compact {
		if value, ok := content["tags"]; ok {
			out["tags"] = value
		}
	}
	return out
}

func relationshipViews(row storage.Campaign, state *storage.CampaignDomainState, currentOnly bool, limit int) []map[string]any {
	relations := map[string]storage.CampaignRelationship{}
	for _, relation := range state.Relationships {
		relations[relation.NPCID.String()] = relation
	}
	out := make([]map[string]any, 0, len(state.NPCs))
	for _, npc := range state.NPCs {
		relation, hasRelation := relations[npc.ID.String()]
		if currentOnly && npc.CurrentLocationKey != row.CurrentLocationKey {
			continue
		}
		if !currentOnly && npc.CurrentLocationKey != row.CurrentLocationKey && !hasRelation {
			continue
		}
		view := relationshipView(npc, nil)
		if hasRelation {
			view = relationshipView(npc, &relation)
		}
		out = append(out, view)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func relationshipView(npc storage.CampaignNPC, relation *storage.CampaignRelationship) map[string]any {
	view := map[string]any{
		"instance_key": npc.InstanceKey, "name": npc.StableName,
		"faction_key": npc.FactionKey, "realm_key": npc.RealmKey,
		"stage_key": npc.StageKey, "location_key": npc.CurrentLocationKey,
		"status": npc.Status, "public_profile": npc.PublicProfile,
	}
	if relation != nil {
		view["relationship"] = map[string]any{
			"trust": relation.Trust, "fear": relation.Fear, "affinity": relation.Affinity,
			"hostility": relation.Hostility, "debt": relation.Debt,
			"public_stance": relation.PublicStance, "known_facts": relation.KnownFacts,
		}
	}
	return view
}

func privateNPCViews(row storage.Campaign, state *storage.CampaignDomainState, limit int) []map[string]any {
	out := make([]map[string]any, 0)
	for _, npc := range state.NPCs {
		if npc.Status != "active" || npc.CurrentLocationKey != row.CurrentLocationKey {
			continue
		}
		out = append(out, map[string]any{
			"instance_key": npc.InstanceKey, "private_state": npc.PrivateState, "goals": npc.Goals,
		})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func spawnableNPCArchetypeViews(row storage.Campaign, state *storage.CampaignDomainState, limit int) []map[string]any {
	counts := map[string]int64{}
	for _, npc := range state.NPCs {
		counts[npc.ArchetypeKey]++
	}
	out := make([]map[string]any, 0)
	for _, definition := range state.Definitions {
		if definition.Kind != storage.DefinitionNPCArchetype {
			continue
		}
		spawnable, _ := definition.Content["spawnable"].(bool)
		maximum, ok := projectionInt64(definition.Content["max_instances"])
		if !spawnable || !ok || counts[definition.Key] >= maximum ||
			!projectionStringListContains(definition.Content["allowed_locations"], row.CurrentLocationKey) {
			continue
		}
		out = append(out, map[string]any{
			"key": definition.Key, "name": definition.Name,
			"realm_key": definition.Content["realm_key"], "stage_key": definition.Content["stage_key"],
			"faction_key": definition.Content["faction_key"], "public_profile": definition.Content["public_profile"],
			"remaining_instances": maximum - counts[definition.Key],
		})
	}
	sort.Slice(out, func(left, right int) bool {
		return out[left]["key"].(string) < out[right]["key"].(string)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func projectionStringListContains(raw any, wanted string) bool {
	values, _ := raw.([]any)
	for _, value := range values {
		if item, ok := value.(string); ok && item == wanted {
			return true
		}
	}
	return false
}

func projectionInt64(raw any) (int64, bool) {
	switch value := raw.(type) {
	case int:
		return int64(value), true
	case int64:
		return value, true
	case float64:
		return int64(value), value == float64(int64(value))
	default:
		return 0, false
	}
}

func factionViews(state *storage.CampaignDomainState, limit int) []map[string]any {
	out := make([]map[string]any, 0, len(state.Factions))
	for _, row := range state.Factions {
		name := row.FactionKey
		if definition, ok := state.Definition(storage.DefinitionFaction, row.FactionKey); ok {
			name = definition.Name
		}
		out = append(out, map[string]any{
			"key": row.FactionKey, "name": name, "reputation": row.Reputation,
			"reputation_level": factionrules.ReputationLevel(row.Reputation),
			"position_key":     row.PositionKey, "contribution": row.Contribution,
			"wanted_level": row.WantedLevel, "permissions": row.Permissions,
		})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func definitionLabel(state *storage.CampaignDomainState, kind, key string) string {
	if definition, ok := state.Definition(kind, key); ok {
		return definition.Name
	}
	return fmt.Sprintf("%s:%s", kind, key)
}
