package campaignview

import (
	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/worldrules"
	"github.com/ialtone/fatecraft/internal/storage"
)

func currentRegionView(row storage.Campaign, state *storage.CampaignDomainState) any {
	regionKey := regionForLocation(row.CurrentLocationKey, state)
	if regionKey == "" {
		return nil
	}
	for _, region := range state.RegionStates {
		if region.RegionKey != regionKey {
			continue
		}
		name := region.RegionKey
		if definition, ok := state.Definition(storage.DefinitionLocation, region.RegionKey); ok {
			name = definition.Name
		}
		view := map[string]any{
			"key": region.RegionKey, "name": name, "resource_level": region.ResourceLevel,
			"danger_level": region.DangerLevel, "stability": region.Stability,
			"conflict_stage": region.ConflictStage, "faction_power": region.FactionPower,
		}
		if region.LastEventGameTime != nil {
			eventName := region.LastEventKey
			if definition, ok := state.Definition(storage.DefinitionWorldEvent, region.LastEventKey); ok {
				eventName = definition.Name
			}
			view["latest_event"] = map[string]any{
				"key": region.LastEventKey, "name": eventName, "variant_key": region.LastEventVariant,
				"summary": region.LastEventSummary, "game_time": *region.LastEventGameTime,
			}
		}
		return view
	}
	return nil
}

func regionForLocation(locationKey string, state *storage.CampaignDomainState) string {
	key := locationKey
	for depth := 0; depth < 4 && key != ""; depth++ {
		definition, ok := state.Definition(storage.DefinitionLocation, key)
		if !ok {
			return ""
		}
		if nodeType, _ := definition.Content["node_type"].(string); nodeType == "region" {
			return key
		}
		key, _ = definition.Content["parent_key"].(string)
	}
	return ""
}

func questViews(row storage.Campaign, state *storage.CampaignDomainState, questType string, activeOnly bool, limit int) []map[string]any {
	out := make([]map[string]any, 0)
	for _, questRow := range state.Quests {
		if questRow.QuestType != questType || activeOnly && !contextQuestStatus(questRow.Status) {
			continue
		}
		definition, ok := state.Definition(storage.DefinitionQuest, questRow.QuestKey)
		if !ok {
			continue
		}
		quest, ok := worldrules.ParseQuest(map[string]any(definition.Content))
		if !ok {
			continue
		}
		objectives, allSatisfied := projectedQuestObjectives(row, state, quest)
		view := map[string]any{
			"key": questRow.QuestKey, "name": definition.Name, "quest_type": questRow.QuestType,
			"status": questRow.Status, "offered_at_game_time": questRow.OfferedAtGameTime,
			"accepted_at_game_time": questRow.AcceptedAtGameTime, "due_game_time": questRow.DueGameTime,
			"resolved_at_game_time": questRow.ResolvedAtGameTime, "objectives": objectives,
			"rewards": projectedQuestRewards(state, quest), "accept_location_keys": quest.AcceptLocationKeys,
			"can_accept": questRow.Status == "offered" && (len(quest.AcceptLocationKeys) == 0 || stringInList(quest.AcceptLocationKeys, row.CurrentLocationKey)),
			"can_submit": (questRow.Status == "active" || questRow.Status == "blocked") && allSatisfied &&
				(questRow.DueGameTime == nil || row.GameTime < *questRow.DueGameTime),
		}
		out = append(out, view)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func projectedQuestObjectives(row storage.Campaign, state *storage.CampaignDomainState, quest worldrules.Quest) ([]map[string]any, bool) {
	out := make([]map[string]any, 0, len(quest.Objectives))
	allSatisfied := true
	for _, objective := range quest.Objectives {
		current, target, satisfied := projectedQuestObjective(row, state, objective)
		out = append(out, map[string]any{
			"key": objective.Key, "type": objective.Type, "label": questObjectiveLabel(state, objective),
			"current": current, "target": target, "comparison": objective.Comparison,
			"satisfied": satisfied, "consume": objective.Consume,
		})
		allSatisfied = allSatisfied && satisfied
	}
	return out, allSatisfied
}

func projectedQuestObjective(row storage.Campaign, state *storage.CampaignDomainState, objective worldrules.QuestObjective) (int64, int64, bool) {
	switch objective.Type {
	case "item":
		quality := objective.QualityKey
		if quality == "" {
			quality = "common"
		}
		var quantity int64
		for _, stack := range state.ItemStacks {
			if stack.ItemKey == objective.ItemKey && stack.QualityKey == quality {
				quantity = stack.Quantity
				break
			}
		}
		return quantity, objective.Quantity, quantity >= objective.Quantity
	case "encounter_win":
		var count int64
		for _, encounter := range state.Encounters {
			if encounter.EncounterKey == objective.EncounterKey && encounter.Status == "won" {
				count++
			}
		}
		return count, objective.Quantity, count >= objective.Quantity
	case "location":
		current := int64(0)
		if row.CurrentLocationKey == objective.LocationKey {
			current = 1
		}
		return current, 1, current == 1
	case "faction_reputation":
		var current int64
		for _, standing := range state.Factions {
			if standing.FactionKey == objective.FactionKey {
				current = int64(standing.Reputation)
				break
			}
		}
		return current, objective.Target, current >= objective.Target
	case "npc_relationship":
		var npcID uuid.UUID
		for _, npc := range state.NPCs {
			if npc.InstanceKey == objective.NPCKey {
				npcID = npc.ID
				break
			}
		}
		var current int64
		for _, relation := range state.Relationships {
			if relation.NPCID == npcID {
				current = projectedRelationshipDimension(relation, objective.Dimension)
				break
			}
		}
		return current, objective.Target, current >= objective.Target
	case "region_state":
		var current int64
		for _, region := range state.RegionStates {
			if region.RegionKey == objective.RegionKey {
				current = projectedRegionField(region, objective.RegionField)
				break
			}
		}
		satisfied := current >= objective.Target
		if objective.Comparison == "at_most" {
			satisfied = current <= objective.Target
		}
		return current, objective.Target, satisfied
	default:
		return 0, objective.Target, false
	}
}

func projectedQuestRewards(state *storage.CampaignDomainState, quest worldrules.Quest) []map[string]any {
	out := make([]map[string]any, 0, len(quest.Rewards))
	for _, reward := range quest.Rewards {
		label := reward.ItemKey
		amount := reward.Quantity
		if reward.Type == "item" {
			if definition, ok := state.Definition(storage.DefinitionItem, reward.ItemKey); ok {
				label = definition.Name
			}
		} else {
			label = reward.FactionKey
			amount = reward.Amount
			if definition, ok := state.Definition(storage.DefinitionFaction, reward.FactionKey); ok {
				label = definition.Name
			}
		}
		out = append(out, map[string]any{"type": reward.Type, "label": label, "amount": amount})
	}
	return out
}

func questObjectiveLabel(state *storage.CampaignDomainState, objective worldrules.QuestObjective) string {
	switch objective.Type {
	case "item":
		if definition, ok := state.Definition(storage.DefinitionItem, objective.ItemKey); ok {
			return definition.Name
		}
		return objective.ItemKey
	case "encounter_win":
		if definition, ok := state.Definition(storage.DefinitionEncounter, objective.EncounterKey); ok {
			return "击败" + definition.Name
		}
		return "击败" + objective.EncounterKey
	case "location":
		if definition, ok := state.Definition(storage.DefinitionLocation, objective.LocationKey); ok {
			return definition.Name
		}
		return objective.LocationKey
	case "faction_reputation":
		if definition, ok := state.Definition(storage.DefinitionFaction, objective.FactionKey); ok {
			return definition.Name + "声望"
		}
		return objective.FactionKey
	case "npc_relationship":
		for _, npc := range state.NPCs {
			if npc.InstanceKey == objective.NPCKey {
				return npc.StableName + relationshipDimensionLabel(objective.Dimension)
			}
		}
		return objective.NPCKey
	case "region_state":
		name := objective.RegionKey
		if definition, ok := state.Definition(storage.DefinitionLocation, objective.RegionKey); ok {
			name = definition.Name
		}
		return name + regionFieldLabel(objective.RegionField)
	default:
		return objective.Key
	}
}

func contextQuestStatus(status string) bool {
	return status == "offered" || status == "active" || status == "blocked"
}

func stringInList(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func projectedRelationshipDimension(relation storage.CampaignRelationship, dimension string) int64 {
	switch dimension {
	case "trust":
		return int64(relation.Trust)
	case "fear":
		return int64(relation.Fear)
	case "affinity":
		return int64(relation.Affinity)
	case "hostility":
		return int64(relation.Hostility)
	case "debt":
		return relation.Debt
	default:
		return 0
	}
}

func projectedRegionField(region storage.CampaignRegionState, field string) int64 {
	switch field {
	case "resource_level":
		return region.ResourceLevel
	case "danger_level":
		return region.DangerLevel
	case "stability":
		return region.Stability
	case "conflict_stage":
		return region.ConflictStage
	default:
		return 0
	}
}

func relationshipDimensionLabel(value string) string {
	return map[string]string{"trust": "信任", "fear": "畏惧", "affinity": "亲近", "hostility": "敌意", "debt": "债务"}[value]
}

func regionFieldLabel(value string) string {
	return map[string]string{"resource_level": "资源", "danger_level": "危险", "stability": "稳定", "conflict_stage": "冲突阶段"}[value]
}
