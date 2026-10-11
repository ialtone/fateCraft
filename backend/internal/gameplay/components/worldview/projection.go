package worldview

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/gameplay/components/factionrules"
	"github.com/ialtone/fatecraft/internal/gameplay/components/gatheringrules"
	"github.com/ialtone/fatecraft/internal/gameplay/components/marketrules"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldrules"
	"github.com/ialtone/fatecraft/internal/storage"
	"sort"
	"strings"
)

func InventoryView(state *storage.CampaignDomainState, limit int) map[string]any {
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
		instanceViews = append(instanceViews, view)
	}
	return map[string]any{"stacks": stackViews, "instances": instanceViews}
}

func WorldView(row storage.Campaign, state *storage.CampaignDomainState, compact bool) map[string]any {
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
			"node_type": LocationNodeType(definition.Content), "parent_key": definition.Content["parent_key"],
			"knowledge": discovery.Knowledge,
		})
	}
	current := map[string]any{"key": row.CurrentLocationKey, "name": row.CurrentLocationKey}
	routes := []map[string]any{}
	if definition, ok := state.Definition(storage.DefinitionLocation, row.CurrentLocationKey); ok {
		current["name"] = definition.Name
		current["content"] = PublicDefinitionContent(definition.Content, compact)
		current["gathering"] = GatheringResourceViews(state, definition.Content)
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
			accessible, missingRequirements := RouteAccessView(state, target, targetExists)
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
		"current": current, "current_path": CurrentLocationPath(state, row.CurrentLocationKey),
		"available_routes": routes, "discovered": locations,
	}
}

func GatheringResourceViews(state *storage.CampaignDomainState, content storage.JSONMap) []map[string]any {
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

func CurrentLocationPath(state *storage.CampaignDomainState, currentKey string) []map[string]any {
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
			"node_type": LocationNodeType(definition.Content),
		})
		key, _ = definition.Content["parent_key"].(string)
	}
	path := make([]map[string]any, len(reversed))
	for index := range reversed {
		path[len(reversed)-1-index] = reversed[index]
	}
	return path
}

func LocationNodeType(content storage.JSONMap) string {
	if value, ok := content["node_type"].(string); ok && value != "" {
		return value
	}
	return "location"
}

func RouteAccessView(
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

func PublicDefinitionContent(content storage.JSONMap, compact bool) map[string]any {
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

func RelationshipViews(row storage.Campaign, state *storage.CampaignDomainState, currentOnly bool, limit int) []map[string]any {
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
		view := RelationshipView(npc, nil)
		if hasRelation {
			view = RelationshipView(npc, &relation)
		}
		out = append(out, view)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func RelationshipView(npc storage.CampaignNPC, relation *storage.CampaignRelationship) map[string]any {
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

func ReferencedRelationshipViews(
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
		out = append(out, RelationshipView(npc, &relation))
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func PrivateNPCViews(row storage.Campaign, state *storage.CampaignDomainState, limit int) []map[string]any {
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

func SpawnableNPCArchetypeViews(row storage.Campaign, state *storage.CampaignDomainState, limit int) []map[string]any {
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
		maximum, ok := ProjectionInt64(definition.Content["max_instances"])
		if !spawnable || !ok || counts[definition.Key] >= maximum ||
			!ProjectionStringListContains(definition.Content["allowed_locations"], row.CurrentLocationKey) {
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

func ProjectionStringListContains(raw any, wanted string) bool {
	values, _ := raw.([]any)
	for _, value := range values {
		if item, ok := value.(string); ok && item == wanted {
			return true
		}
	}
	return false
}

func ProjectionInt64(raw any) (int64, bool) {
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

func FactionViews(state *storage.CampaignDomainState, limit int) []map[string]any {
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

func CurrentRegionView(row storage.Campaign, state *storage.CampaignDomainState) any {
	regionKey := RegionForLocation(row.CurrentLocationKey, state)
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

func RegionForLocation(locationKey string, state *storage.CampaignDomainState) string {
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

func QuestViews(row storage.Campaign, state *storage.CampaignDomainState, questType string, activeOnly bool, limit int) []map[string]any {
	out := make([]map[string]any, 0)
	for _, questRow := range state.Quests {
		if questRow.QuestType != questType || activeOnly && !ContextQuestStatus(questRow.Status) {
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
		objectives, allSatisfied := ProjectedQuestObjectives(row, state, quest)
		view := map[string]any{
			"key": questRow.QuestKey, "name": definition.Name, "quest_type": questRow.QuestType,
			"status": questRow.Status, "offered_at_game_time": questRow.OfferedAtGameTime,
			"accepted_at_game_time": questRow.AcceptedAtGameTime, "due_game_time": questRow.DueGameTime,
			"resolved_at_game_time": questRow.ResolvedAtGameTime, "objectives": objectives,
			"rewards": ProjectedQuestRewards(state, quest), "accept_location_keys": quest.AcceptLocationKeys,
			"can_accept": questRow.Status == "offered" && (len(quest.AcceptLocationKeys) == 0 || StringInList(quest.AcceptLocationKeys, row.CurrentLocationKey)),
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

func ProjectedQuestObjectives(row storage.Campaign, state *storage.CampaignDomainState, quest worldrules.Quest) ([]map[string]any, bool) {
	out := make([]map[string]any, 0, len(quest.Objectives))
	allSatisfied := true
	for _, objective := range quest.Objectives {
		current, target, satisfied := ProjectedQuestObjective(row, state, objective)
		out = append(out, map[string]any{
			"key": objective.Key, "type": objective.Type, "label": QuestObjectiveLabel(state, objective),
			"current": current, "target": target, "comparison": objective.Comparison,
			"satisfied": satisfied, "consume": objective.Consume,
		})
		allSatisfied = allSatisfied && satisfied
	}
	return out, allSatisfied
}

func ProjectedQuestObjective(row storage.Campaign, state *storage.CampaignDomainState, objective worldrules.QuestObjective) (int64, int64, bool) {
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
				current = ProjectedRelationshipDimension(relation, objective.Dimension)
				break
			}
		}
		return current, objective.Target, current >= objective.Target
	case "region_state":
		var current int64
		for _, region := range state.RegionStates {
			if region.RegionKey == objective.RegionKey {
				current = ProjectedRegionField(region, objective.RegionField)
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

func ProjectedRelationshipDimension(relation storage.CampaignRelationship, dimension string) int64 {
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

func ProjectedRegionField(region storage.CampaignRegionState, field string) int64 {
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

func QuestObjectiveLabel(state *storage.CampaignDomainState, objective worldrules.QuestObjective) string {
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
				return npc.StableName + RelationshipDimensionLabel(objective.Dimension)
			}
		}
		return objective.NPCKey
	case "region_state":
		name := objective.RegionKey
		if definition, ok := state.Definition(storage.DefinitionLocation, objective.RegionKey); ok {
			name = definition.Name
		}
		return name + RegionFieldLabel(objective.RegionField)
	default:
		return objective.Key
	}
}

func RelationshipDimensionLabel(value string) string {
	return map[string]string{"trust": "信任", "fear": "畏惧", "affinity": "亲近", "hostility": "敌意", "debt": "债务"}[value]
}

func RegionFieldLabel(value string) string {
	return map[string]string{"resource_level": "资源", "danger_level": "危险", "stability": "稳定", "conflict_stage": "冲突阶段"}[value]
}

func ProjectedQuestRewards(state *storage.CampaignDomainState, quest worldrules.Quest) []map[string]any {
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

func ContextQuestStatus(status string) bool {
	return status == "offered" || status == "active" || status == "blocked"
}

func StringInList(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func MarketViews(row storage.Campaign, state *storage.CampaignDomainState, itemLimit int) []map[string]any {
	if state == nil {
		return []map[string]any{}
	}
	stocks := map[string]storage.CampaignMarketStock{}
	for _, stock := range state.MarketStocks {
		stocks[MarketProjectionIdentity(stock.MarketKey, stock.ItemKind, stock.ItemKey, stock.QualityKey)] = stock
	}
	standings := map[string]int64{}
	for _, standing := range state.Factions {
		standings[standing.FactionKey] = int64(standing.Reputation)
	}
	out := make([]map[string]any, 0)
	for _, marketState := range state.Markets {
		if marketState.LocationKey != row.CurrentLocationKey {
			continue
		}
		definition, ok := state.Definition(storage.DefinitionMarket, marketState.MarketKey)
		if !ok {
			continue
		}
		market, valid := marketrules.Parse(map[string]any(definition.Content))
		if !valid {
			continue
		}
		market.PriceModifierBPS = market.PriceModifierBPS * int64(marketState.PriceModifierBPS) / 10000
		items := make([]map[string]any, 0, len(market.Entries))
		effectiveRestockGameTime := marketState.LastRestockGameTime
		for _, entry := range market.Entries {
			stock, exists := stocks[MarketProjectionIdentity(marketState.MarketKey, entry.Kind, entry.Key, entry.QualityKey)]
			if !exists {
				continue
			}
			quantity, restockedAt := marketrules.Restock(stock.Quantity, marketState.LastRestockGameTime, row.GameTime, market, entry)
			if restockedAt > effectiveRestockGameTime {
				effectiveRestockGameTime = restockedAt
			}
			buyPrice, buyable := marketrules.UnitPrice(market, entry, quantity, standings[market.FactionKey], true)
			sellPrice, sellable := marketrules.UnitPrice(market, entry, quantity, standings[market.FactionKey], false)
			items = append(items, map[string]any{
				"kind": entry.Kind, "key": entry.Key, "name": DefinitionLabel(state, entry.Kind, entry.Key),
				"quality_key": entry.QualityKey, "stock": quantity, "max_stock": entry.MaxStock,
				"buyback_capacity": entry.MaxStock - quantity,
				"buyable":          buyable, "buy_price": buyPrice, "sellable": sellable, "sell_price": sellPrice,
			})
			if itemLimit > 0 && len(items) >= itemLimit {
				break
			}
		}
		sort.Slice(items, func(left, right int) bool {
			return items[left]["key"].(string) < items[right]["key"].(string)
		})
		out = append(out, map[string]any{
			"key": definition.Key, "name": definition.Name, "location_key": market.LocationKey,
			"currency_item_key":      market.CurrencyItemKey,
			"currency_name":          DefinitionLabel(state, storage.DefinitionItem, market.CurrencyItemKey),
			"last_restock_game_time": effectiveRestockGameTime,
			"turnover_text":          fmt.Sprintf("周转间隔 %d 分钟；库存与收购额度以当前列表为准", market.RestockIntervalMinutes),
			"items":                  items,
		})
	}
	return out
}

func MarketProjectionIdentity(marketKey, kind, key, quality string) string {
	return marketKey + ":" + kind + ":" + key + ":" + quality
}

func DefinitionLabel(state *storage.CampaignDomainState, kind, key string) string {
	if definition, ok := state.Definition(kind, key); ok {
		return definition.Name
	}
	return fmt.Sprintf("%s:%s", kind, key)
}
