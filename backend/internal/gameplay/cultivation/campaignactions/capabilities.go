package campaignactions

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/craftingrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/effectrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/encounterrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/factionrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/gatheringrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/marketrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/practicerules"
	"github.com/ialtone/fatecraft/internal/storage"
)

type cultivationCapabilityProvider struct{}

func (cultivationCapabilityProvider) Project(
	_ context.Context,
	input campaign.CapabilityContext,
	definition campaign.ActionDefinition,
) (campaign.ActionCapability, error) {
	capability := campaign.ActionCapability{
		Available: true, ArgumentSchema: cloneSchema(definition.ArgumentSchema),
		Options: []storage.JSONMap{}, Metadata: storage.JSONMap{},
	}
	state := input.Domain
	if state == nil {
		return denyCapability(capability, "campaign.state_unavailable", "玩法状态不可用"), nil
	}
	if active := activeEncounterFact(state); active != nil && definition.Key != ActionEncounterAct && definition.Key != ActionEncounterFlee {
		return denyCapability(capability, "encounter.active", fmt.Sprintf("必须先解决活动遭遇 %s", active.EncounterKey)), nil
	}

	switch definition.Key {
	case ActionRenameCharacter:
		requireCharacter(&capability, state)
	case ActionPractice:
		projectPracticeCapability(&capability, state)
	case ActionBreakthrough:
		projectBreakthroughCapability(&capability, state)
	case ActionLearnTechnique:
		projectLearnTechniqueCapability(&capability, state)
	case ActionEquipTechnique:
		projectEquipTechniqueCapability(&capability, state)
	case ActionUnequipTechnique:
		for _, technique := range state.Techniques {
			if technique.Status == "active" && technique.EquippedSlot != nil {
				capability.Options = append(capability.Options, storage.JSONMap{"technique_key": technique.TechniqueKey, "slot": *technique.EquippedSlot})
			}
		}
		applySingleEnum(&capability, "technique_key", optionStrings(capability.Options, "technique_key"), "technique.not_equipped", "当前没有已装配功法")
	case ActionApplyCondition, ActionAdjustVitals:
		requireCharacter(&capability, state)
	case ActionResolveCondition:
		projectResolveConditionCapability(&capability, state)
	case ActionGainItem:
		projectGrantDefinitionCapability(&capability, state, storage.DefinitionItem, "item_key", true)
	case ActionUseItem:
		projectUseItemCapability(&capability, state)
	case ActionGainEquipment:
		projectGrantDefinitionCapability(&capability, state, storage.DefinitionEquipment, "definition_key", false)
	case ActionEquipItem:
		projectEquipItemCapability(&capability, state)
	case ActionUnequipItem:
		for _, item := range state.ItemInstances {
			if item.State == "equipped" && item.EquippedSlot != nil {
				capability.Options = append(capability.Options, storage.JSONMap{
					"item_instance_id": item.ID.String(), "definition_key": item.DefinitionKey, "slot": *item.EquippedSlot,
				})
			}
		}
		applySingleEnum(&capability, "item_instance_id", optionStrings(capability.Options, "item_instance_id"), "equipment.not_equipped", "当前没有已装备物品")
	case ActionLearnRecipe:
		projectRecipeCapability(&capability, input.Campaign, state, true)
	case ActionCraftRecipe:
		projectRecipeCapability(&capability, input.Campaign, state, false)
	case ActionTradeBuy:
		projectTradeCapability(&capability, input.Campaign, state, true)
	case ActionTradeSell:
		projectTradeCapability(&capability, input.Campaign, state, false)
	case ActionAdjustMarket:
		projectMarketKeys(&capability, input.Campaign, state)
	case ActionTravel:
		projectTravelCapability(&capability, input.Campaign, state)
	case ActionInvestigate:
		if _, ok := state.Definition(storage.DefinitionLocation, input.Campaign.CurrentLocationKey); !ok {
			capability = denyCapability(capability, "location.unavailable", "当前位置定义不存在")
		}
	case ActionResolveUncertain:
		capability.Metadata = storage.JSONMap{"effects": "roll_only", "requires_followup_domain_action_for_state_change": true}
	case ActionGather:
		projectGatherCapability(&capability, input.Campaign, state)
	case ActionInstantiateNPC:
		projectInstantiateNPCCapability(&capability, input.Campaign, state)
	case ActionMoveNPC:
		projectMoveNPCCapability(&capability, input.Campaign, state)
	case ActionSetNPCStatus:
		projectNPCStatusCapability(&capability, input.Campaign, state)
	case ActionRecordInteraction:
		projectCurrentNPCCapability(&capability, input.Campaign, state)
	case ActionAdjustFaction:
		projectFactionCapability(&capability, state)
	case ActionAcceptQuest:
		projectQuestCapability(&capability, input.Campaign, state, true)
	case ActionSubmitQuest:
		projectQuestCapability(&capability, input.Campaign, state, false)
	case ActionStartEncounter:
		projectStartEncounterCapability(&capability, input.Campaign, state)
	case ActionEncounterAct:
		projectEncounterActCapability(&capability, state)
	case ActionEncounterFlee:
		projectEncounterFleeCapability(&capability, state)
	default:
		return campaign.ActionCapability{}, fmt.Errorf("未知修仙动作定义: %s", definition.Key)
	}
	if len(capability.Metadata) == 0 {
		capability.Metadata = nil
	}
	sortCapabilityOptions(capability.Options)
	return capability, nil
}

func projectPracticeCapability(capability *campaign.ActionCapability, state *storage.CampaignDomainState) {
	for _, technique := range state.Techniques {
		definition, ok := state.Definition(storage.DefinitionTechnique, technique.TechniqueKey)
		if !ok {
			continue
		}
		rules := practicerules.Evaluate(practicerules.Facts{
			Character: state.Character, Technique: technique, Definition: definition,
		}, nil)
		if !rules.Available {
			continue
		}
		capability.Options = append(capability.Options, storage.JSONMap{
			"technique_key":                technique.TechniqueKey,
			"limits":                       storage.JSONMap{"sessions": storage.JSONMap{"minimum": int64(1), "maximum": rules.MaximumSessions}},
			"cost_preview":                 storage.JSONMap{"resource_key": "mana", "per_session": rules.ManaCostPerSession},
			"practice_minutes_per_session": rules.PracticeMinutesPerSession,
			"cultivation_gain_per_session": rules.CultivationGainPerSession,
			"mastery_gain_per_session":     rules.MasteryGainPerSession,
		})
	}
	keys := optionStrings(capability.Options, "technique_key")
	if len(keys) == 0 {
		*capability = denyCapability(*capability, "cultivation.unavailable", "当前没有可修炼的已装配功法或资源不足")
		return
	}
	setStringEnum(capability.ArgumentSchema, "technique_key", keys)
	maximum := int64(1)
	for _, option := range capability.Options {
		limits, _ := option["limits"].(storage.JSONMap)
		sessions, _ := limits["sessions"].(storage.JSONMap)
		if value, ok := numericInt(sessions["maximum"]); ok && value > maximum {
			maximum = value
		}
	}
	setIntegerMaximum(capability.ArgumentSchema, "sessions", maximum)
}

func projectBreakthroughCapability(capability *campaign.ActionCapability, state *storage.CampaignDomainState) {
	if !requireCharacter(capability, state) {
		return
	}
	character := state.Character
	if character.CultivationProgress < character.CultivationRequired {
		*capability = denyCapability(*capability, "cultivation.progress_insufficient", fmt.Sprintf(
			"修为尚未圆满：%d/%d", character.CultivationProgress, character.CultivationRequired,
		))
		return
	}
	current, ok := state.Definition(storage.DefinitionRealm, character.RealmKey)
	next, _ := current.Content["next_realm_key"].(string)
	if !ok || next == "" {
		*capability = denyCapability(*capability, "cultivation.no_next_realm", "当前境界没有可突破的下一境界")
		return
	}
	capability.Metadata["next_realm_key"] = next
}

func projectLearnTechniqueCapability(capability *campaign.ActionCapability, state *storage.CampaignDomainState) {
	if !requireCharacter(capability, state) {
		return
	}
	known := map[string]bool{}
	for _, row := range state.Techniques {
		known[row.TechniqueKey] = true
	}
	owned := itemQuantities(state)
	for _, definition := range sortedDefinitions(state, storage.DefinitionTechnique) {
		if known[definition.Key] || int64(state.Character.RealmRank) < definitionIntDefault(definition.Content["minimum_realm_rank"], 0) {
			continue
		}
		source, _ := definition.Content["source_item_key"].(string)
		if source != "" && owned[source+":common"] < 1 {
			continue
		}
		capability.Options = append(capability.Options, storage.JSONMap{"technique_key": definition.Key, "name": definition.Name, "source_item_key": source})
	}
	applySingleEnum(capability, "technique_key", optionStrings(capability.Options, "technique_key"), "technique.none_learnable", "当前没有可学习功法")
}

func projectEquipTechniqueCapability(capability *campaign.ActionCapability, state *storage.CampaignDomainState) {
	for _, technique := range state.Techniques {
		if technique.Status != "active" {
			continue
		}
		definition, ok := state.Definition(storage.DefinitionTechnique, technique.TechniqueKey)
		if !ok {
			continue
		}
		for _, slot := range definitionStrings(definition.Content["allowed_slots"]) {
			capability.Options = append(capability.Options, storage.JSONMap{"technique_key": technique.TechniqueKey, "slot": slot})
		}
	}
	if len(capability.Options) == 0 {
		*capability = denyCapability(*capability, "technique.none_equippable", "当前没有可装配功法")
		return
	}
	setStringEnum(capability.ArgumentSchema, "technique_key", optionStrings(capability.Options, "technique_key"))
	setStringEnum(capability.ArgumentSchema, "slot", optionStrings(capability.Options, "slot"))
}

func projectResolveConditionCapability(capability *campaign.ActionCapability, state *storage.CampaignDomainState) {
	if !requireCharacter(capability, state) {
		return
	}
	for key, value := range state.Character.Injuries {
		category := conditionCategoryFromValue(value, "injury")
		capability.Options = append(capability.Options, storage.JSONMap{"category": category, "effect_key": key})
	}
	for key, value := range state.Character.LongTermEffects {
		category := conditionCategoryFromValue(value, "long_term")
		capability.Options = append(capability.Options, storage.JSONMap{"category": category, "effect_key": key})
	}
	if len(capability.Options) == 0 {
		*capability = denyCapability(*capability, "condition.none_active", "当前没有可移除状态")
		return
	}
	setStringEnum(capability.ArgumentSchema, "effect_key", optionStrings(capability.Options, "effect_key"))
}

func projectGrantDefinitionCapability(capability *campaign.ActionCapability, state *storage.CampaignDomainState, kind, field string, stackable bool) {
	for _, definition := range sortedDefinitions(state, kind) {
		grantable, _ := definition.Content["model_grantable"].(bool)
		isStackable, _ := definition.Content["stackable"].(bool)
		if !grantable || kind == storage.DefinitionItem && isStackable != stackable {
			continue
		}
		option := storage.JSONMap{field: definition.Key, "name": definition.Name}
		if stackable {
			option["limits"] = storage.JSONMap{"quantity": storage.JSONMap{"minimum": int64(1), "maximum": definitionIntDefault(definition.Content["max_grant_per_action"], 100)}}
		}
		capability.Options = append(capability.Options, option)
	}
	applySingleEnum(capability, field, optionStrings(capability.Options, field), "grant.none_allowed", "当前玩法没有允许模型直接授予的对象")
}

func projectUseItemCapability(capability *campaign.ActionCapability, state *storage.CampaignDomainState) {
	for _, stack := range state.ItemStacks {
		if stack.Quantity < 1 {
			continue
		}
		definition, ok := state.Definition(storage.DefinitionItem, stack.ItemKey)
		consumable, _ := definition.Content["consumable"].(bool)
		if !ok || !consumable {
			continue
		}
		maximum := stack.Quantity
		if maximum > 20 {
			maximum = 20
		}
		capability.Options = append(capability.Options, storage.JSONMap{
			"item_key": stack.ItemKey, "quality_key": stack.QualityKey,
			"limits": storage.JSONMap{"quantity": storage.JSONMap{"minimum": int64(1), "maximum": maximum}},
		})
	}
	applySingleEnum(capability, "item_key", optionStrings(capability.Options, "item_key"), "inventory.no_usable_item", "当前没有可使用物品")
}

func projectEquipItemCapability(capability *campaign.ActionCapability, state *storage.CampaignDomainState) {
	for _, item := range state.ItemInstances {
		if item.Durability <= 0 || item.State != "stored" && item.State != "equipped" {
			continue
		}
		definition, ok := state.Definition(storage.DefinitionEquipment, item.DefinitionKey)
		if !ok {
			continue
		}
		for _, slot := range definitionStrings(definition.Content["allowed_slots"]) {
			capability.Options = append(capability.Options, storage.JSONMap{
				"item_instance_id": item.ID.String(), "definition_key": item.DefinitionKey, "slot": slot,
			})
		}
	}
	if len(capability.Options) == 0 {
		*capability = denyCapability(*capability, "equipment.none_equippable", "当前没有可装配物品")
		return
	}
	setStringEnum(capability.ArgumentSchema, "item_instance_id", optionStrings(capability.Options, "item_instance_id"))
	setStringEnum(capability.ArgumentSchema, "slot", optionStrings(capability.Options, "slot"))
}

func projectRecipeCapability(capability *campaign.ActionCapability, row storage.Campaign, state *storage.CampaignDomainState, learn bool) {
	known := map[string]bool{}
	for _, recipe := range state.KnownRecipes {
		known[recipe.RecipeKey] = true
	}
	owned := itemQuantities(state)
	for _, definition := range sortedDefinitions(state, storage.DefinitionRecipe) {
		recipe, ok := craftingrules.Parse(map[string]any(definition.Content))
		if !ok {
			continue
		}
		if learn {
			if known[definition.Key] || recipe.StartingKnown || recipe.SourceItemKey == "" || owned[recipe.SourceItemKey+":common"] < 1 {
				continue
			}
		} else if !known[definition.Key] || !recipeCurrentlyAvailable(row, state, recipe, owned) {
			continue
		}
		capability.Options = append(capability.Options, storage.JSONMap{"recipe_key": definition.Key, "name": definition.Name})
	}
	code, message := "craft.none_available", "当前没有可执行配方"
	if learn {
		code, message = "craft.none_learnable", "当前没有可学习配方"
	}
	applySingleEnum(capability, "recipe_key", optionStrings(capability.Options, "recipe_key"), code, message)
}

func projectTradeCapability(capability *campaign.ActionCapability, row storage.Campaign, state *storage.CampaignDomainState, buying bool) {
	owned := itemQuantities(state)
	instanceIDs := make([]string, 0)
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
		market.PriceModifierBPS = market.PriceModifierBPS * int64(marketState.PriceModifierBPS) / 10_000
		for _, entry := range market.Entries {
			stock := marketStockFact(state, marketState.MarketKey, entry.Kind, entry.Key, entry.QualityKey)
			if stock == nil {
				continue
			}
			stockQuantity, _ := marketrules.Restock(
				stock.Quantity, marketState.LastRestockGameTime, row.GameTime, market, entry,
			)
			maximum := int64(0)
			if buying {
				maximum = stockQuantity
				price, priced := marketrules.UnitPrice(market, entry, stockQuantity, factionReputation(state, market.FactionKey), true)
				if !priced || price <= 0 {
					continue
				}
				maximum = minimum(maximum, owned[market.CurrencyItemKey+":common"]/price)
			} else {
				price, priced := marketrules.UnitPrice(market, entry, stockQuantity, factionReputation(state, market.FactionKey), false)
				if !priced || price <= 0 {
					continue
				}
				if entry.Kind == "item" {
					maximum = minimum(owned[entry.Key+":"+entry.QualityKey], entry.MaxStock-stockQuantity)
				} else {
					maximum = minimum(countSellableEquipment(state, entry.Key, entry.QualityKey), entry.MaxStock-stockQuantity)
					maximum = minimum(maximum, 1)
				}
			}
			if maximum < 1 {
				continue
			}
			option := storage.JSONMap{
				"market_key": marketState.MarketKey, "item_kind": entry.Kind, "item_key": entry.Key,
				"quality_key": entry.QualityKey, "limits": storage.JSONMap{"quantity": storage.JSONMap{"minimum": int64(1), "maximum": maximum}},
			}
			if !buying && entry.Kind == "equipment" {
				for _, instanceID := range sellableEquipmentIDs(state, entry.Key, entry.QualityKey) {
					candidate := cloneOption(option)
					candidate["item_instance_id"] = instanceID
					capability.Options = append(capability.Options, candidate)
					instanceIDs = append(instanceIDs, instanceID)
				}
				continue
			}
			capability.Options = append(capability.Options, option)
		}
	}
	if len(capability.Options) == 0 {
		message := "当前位置没有可出售商品"
		if buying {
			message = "当前位置没有当前可购买商品或货币不足"
		}
		*capability = denyCapability(*capability, "trade.none_available", message)
		return
	}
	for _, field := range []string{"market_key", "item_kind", "item_key", "quality_key"} {
		setStringEnum(capability.ArgumentSchema, field, optionStrings(capability.Options, field))
	}
	if !buying {
		setStringEnumIncludingEmpty(capability.ArgumentSchema, "item_instance_id", instanceIDs)
	}
}

func projectMarketKeys(capability *campaign.ActionCapability, row storage.Campaign, state *storage.CampaignDomainState) {
	for _, market := range state.Markets {
		if market.LocationKey == row.CurrentLocationKey {
			capability.Options = append(capability.Options, storage.JSONMap{"market_key": market.MarketKey})
		}
	}
	applySingleEnum(capability, "market_key", optionStrings(capability.Options, "market_key"), "market.none_available", "当前位置没有市场")
}

func projectTravelCapability(capability *campaign.ActionCapability, row storage.Campaign, state *storage.CampaignDomainState) {
	current, ok := state.Definition(storage.DefinitionLocation, row.CurrentLocationKey)
	if !ok {
		*capability = denyCapability(*capability, "location.unavailable", "当前位置定义不存在")
		return
	}
	discovered := map[string]bool{}
	for _, item := range state.Discoveries {
		discovered[item.LocationKey] = true
	}
	connections, _ := current.Content["connections"].([]any)
	for _, raw := range connections {
		connection, ok := raw.(map[string]any)
		key, _ := connection["location_key"].(string)
		target, targetOK := state.Definition(storage.DefinitionLocation, key)
		if !ok || !discovered[key] || !targetOK || !entryRequirementsMet(state, target.Content) {
			continue
		}
		capability.Options = append(capability.Options, storage.JSONMap{"destination_key": key, "name": target.Name, "travel_minutes": connection["travel_minutes"]})
	}
	applySingleEnum(capability, "destination_key", optionStrings(capability.Options, "destination_key"), "travel.no_route", "当前没有可进入的已发现相邻地点")
}

func projectGatherCapability(capability *campaign.ActionCapability, row storage.Campaign, state *storage.CampaignDomainState) {
	location, ok := state.Definition(storage.DefinitionLocation, row.CurrentLocationKey)
	if !ok {
		*capability = denyCapability(*capability, "location.unavailable", "当前位置定义不存在")
		return
	}
	table, ok := gatheringrules.Parse(location.Content["gathering"])
	if !ok || !regionHasResources(state, location) {
		*capability = denyCapability(*capability, "gathering.unavailable", "当前位置没有可采集资源或区域资源已枯竭")
		return
	}
	for _, entry := range table.Entries {
		capability.Options = append(capability.Options, storage.JSONMap{
			"item_key": entry.ItemKey, "quality_key": entry.QualityKey,
			"quantity_preview": storage.JSONMap{"minimum": entry.MinQuantity, "maximum": entry.MaxQuantity},
			"minutes":          table.Minutes,
		})
	}
	setStringEnum(capability.ArgumentSchema, "item_key", optionStrings(capability.Options, "item_key"))
}

func projectInstantiateNPCCapability(capability *campaign.ActionCapability, row storage.Campaign, state *storage.CampaignDomainState) {
	counts := map[string]int64{}
	for _, npc := range state.NPCs {
		counts[npc.ArchetypeKey]++
	}
	for _, definition := range sortedDefinitions(state, storage.DefinitionNPCArchetype) {
		spawnable, _ := definition.Content["spawnable"].(bool)
		initial, _ := definition.Content["initial"].(bool)
		maximum := definitionIntDefault(definition.Content["max_instances"], 1)
		if !spawnable || initial || counts[definition.Key] >= maximum || !containsDefinitionString(definition.Content["allowed_locations"], row.CurrentLocationKey) {
			continue
		}
		capability.Options = append(capability.Options, storage.JSONMap{
			"archetype_key": definition.Key, "name": definition.Name, "remaining_instances": maximum - counts[definition.Key],
		})
	}
	applySingleEnum(capability, "archetype_key", optionStrings(capability.Options, "archetype_key"), "npc.none_spawnable", "当前位置没有可实例化 NPC 原型")
}

func projectMoveNPCCapability(capability *campaign.ActionCapability, row storage.Campaign, state *storage.CampaignDomainState) {
	for _, npc := range state.NPCs {
		if npc.Status != "active" || npc.CurrentLocationKey != row.CurrentLocationKey {
			continue
		}
		location, ok := state.Definition(storage.DefinitionLocation, npc.CurrentLocationKey)
		if !ok {
			continue
		}
		connections, _ := location.Content["connections"].([]any)
		for _, raw := range connections {
			connection, ok := raw.(map[string]any)
			destination, _ := connection["location_key"].(string)
			if !ok || destination == "" {
				continue
			}
			if _, exists := state.Definition(storage.DefinitionLocation, destination); exists {
				capability.Options = append(capability.Options, storage.JSONMap{"npc_key": npc.InstanceKey, "destination_key": destination})
			}
		}
	}
	if len(capability.Options) == 0 {
		*capability = denyCapability(*capability, "npc.none_movable", "当前地点没有可移动 NPC 或相邻地点")
		return
	}
	setStringEnum(capability.ArgumentSchema, "npc_key", optionStrings(capability.Options, "npc_key"))
	setStringEnum(capability.ArgumentSchema, "destination_key", optionStrings(capability.Options, "destination_key"))
}

func projectNPCStatusCapability(capability *campaign.ActionCapability, row storage.Campaign, state *storage.CampaignDomainState) {
	for _, npc := range state.NPCs {
		if npc.CurrentLocationKey != row.CurrentLocationKey {
			continue
		}
		for _, status := range []string{"active", "missing", "dead", "departed", "sealed"} {
			if validNPCStatusTransition(npc.Status, status) {
				capability.Options = append(capability.Options, storage.JSONMap{"npc_key": npc.InstanceKey, "status": status})
			}
		}
	}
	if len(capability.Options) == 0 {
		*capability = denyCapability(*capability, "npc.no_status_transition", "当前地点没有可变更状态的 NPC")
		return
	}
	setStringEnum(capability.ArgumentSchema, "npc_key", optionStrings(capability.Options, "npc_key"))
}

func projectCurrentNPCCapability(capability *campaign.ActionCapability, row storage.Campaign, state *storage.CampaignDomainState) {
	for _, npc := range state.NPCs {
		if npc.Status == "active" && npc.CurrentLocationKey == row.CurrentLocationKey {
			capability.Options = append(capability.Options, storage.JSONMap{"npc_key": npc.InstanceKey, "name": npc.StableName})
		}
	}
	applySingleEnum(capability, "npc_key", optionStrings(capability.Options, "npc_key"), "npc.none_interactable", "当前地点没有可交互 NPC")
}

func projectFactionCapability(capability *campaign.ActionCapability, state *storage.CampaignDomainState) {
	for _, definition := range sortedDefinitions(state, storage.DefinitionFaction) {
		capability.Options = append(capability.Options, storage.JSONMap{"faction_key": definition.Key, "name": definition.Name})
	}
	applySingleEnum(capability, "faction_key", optionStrings(capability.Options, "faction_key"), "faction.none_available", "当前玩法没有可调整势力")
}

func projectQuestCapability(capability *campaign.ActionCapability, row storage.Campaign, state *storage.CampaignDomainState, accepting bool) {
	for _, quest := range state.Quests {
		if accepting {
			if quest.Status != "offered" {
				continue
			}
			definition, ok := state.Definition(storage.DefinitionQuest, quest.QuestKey)
			if !ok || !questAcceptsAt(definition.Content, row.CurrentLocationKey) {
				continue
			}
		} else if quest.Status != "active" && quest.Status != "blocked" || quest.DueGameTime != nil && *quest.DueGameTime <= row.GameTime {
			continue
		}
		capability.Options = append(capability.Options, storage.JSONMap{"quest_key": quest.QuestKey, "status": quest.Status})
	}
	message := "当前没有可提交任务"
	if accepting {
		message = "当前没有可接受任务"
	}
	applySingleEnum(capability, "quest_key", optionStrings(capability.Options, "quest_key"), "quest.none_available", message)
}

func projectStartEncounterCapability(capability *campaign.ActionCapability, row storage.Campaign, state *storage.CampaignDomainState) {
	if activeEncounterFact(state) != nil {
		*capability = denyCapability(*capability, "encounter.active", "已有活动遭遇")
		return
	}
	if state.Character == nil || state.Character.Health <= 0 {
		*capability = denyCapability(*capability, "character.unavailable", "角色已无法开始遭遇")
		return
	}
	for _, definition := range sortedDefinitions(state, storage.DefinitionEncounter) {
		if definition.Visibility == storage.CampaignVisibilityInternal {
			continue
		}
		rules, ok := encounterrules.Parse(map[string]any(definition.Content))
		if ok && rules.LocationKey == row.CurrentLocationKey {
			capability.Options = append(capability.Options, storage.JSONMap{"encounter_key": definition.Key, "name": definition.Name})
		}
	}
	applySingleEnum(capability, "encounter_key", optionStrings(capability.Options, "encounter_key"), "encounter.none_available", "当前位置没有可开始遭遇")
}

func projectEncounterActCapability(capability *campaign.ActionCapability, state *storage.CampaignDomainState) {
	active := activeEncounterFact(state)
	if active == nil {
		*capability = denyCapability(*capability, "encounter.none_active", "当前没有活动遭遇")
		return
	}
	if state.Character == nil || state.Character.Health <= 0 {
		*capability = denyCapability(*capability, "character.unavailable", "角色已无法继续遭遇")
		return
	}
	definition, exists := state.Definition(storage.DefinitionEncounter, active.EncounterKey)
	rules, valid := encounterrules.Parse(map[string]any(definition.Content))
	if !exists || !valid {
		*capability = denyCapability(*capability, "encounter.definition_unavailable", "活动遭遇规则不可用")
		return
	}
	setStringEnum(capability.ArgumentSchema, "encounter_id", []string{active.ID.String()})
	capability.Options = append(capability.Options, storage.JSONMap{"encounter_id": active.ID.String(), "move": "defend"})
	techniques := make([]string, 0)
	if state.Character.Mana >= rules.PlayerManaCost {
		capability.Options = append(capability.Options, storage.JSONMap{
			"encounter_id": active.ID.String(), "move": "attack",
			"cost_preview": storage.JSONMap{"resource_key": "mana", "amount": rules.PlayerManaCost},
		})
		for _, technique := range state.Techniques {
			if technique.Status != "active" || technique.EquippedSlot == nil {
				continue
			}
			techniqueDefinition, ok := state.Definition(storage.DefinitionTechnique, technique.TechniqueKey)
			if !ok {
				continue
			}
			if _, ok := effectrules.TechniqueEffects(techniqueDefinition.Content, technique.LearnedLayer); !ok {
				continue
			}
			manaCost := rules.PlayerManaCost + definitionIntDefault(
				techniqueDefinition.Content["combat_mana_cost"], definitionIntDefault(techniqueDefinition.Content["mana_cost"], 0),
			)
			if manaCost < 0 || state.Character.Mana < manaCost {
				continue
			}
			techniques = append(techniques, technique.TechniqueKey)
			capability.Options = append(capability.Options, storage.JSONMap{
				"encounter_id": active.ID.String(), "move": "attack", "technique_key": technique.TechniqueKey,
				"cost_preview": storage.JSONMap{"resource_key": "mana", "amount": manaCost},
			})
		}
	}
	setStringEnum(capability.ArgumentSchema, "move", optionStrings(capability.Options, "move"))
	setStringEnumIncludingEmpty(capability.ArgumentSchema, "technique_key", techniques)
}

func projectEncounterFleeCapability(capability *campaign.ActionCapability, state *storage.CampaignDomainState) {
	active := activeEncounterFact(state)
	if active == nil {
		*capability = denyCapability(*capability, "encounter.none_active", "当前没有活动遭遇")
		return
	}
	if state.Character == nil || state.Character.Health <= 0 {
		*capability = denyCapability(*capability, "character.unavailable", "角色已无法撤离遭遇")
		return
	}
	setStringEnum(capability.ArgumentSchema, "encounter_id", []string{active.ID.String()})
	capability.Options = append(capability.Options, storage.JSONMap{"encounter_id": active.ID.String()})
}

func requireCharacter(capability *campaign.ActionCapability, state *storage.CampaignDomainState) bool {
	if state.Character != nil && state.Character.Health > 0 {
		return true
	}
	*capability = denyCapability(*capability, "character.unavailable", "角色领域状态不可用")
	return false
}

func denyCapability(capability campaign.ActionCapability, code, message string) campaign.ActionCapability {
	capability.Available = false
	capability.Options = nil
	capability.Metadata = nil
	capability.Violations = []campaign.ActionViolation{{Code: code, Message: message, Public: true}}
	return capability
}

func applySingleEnum(capability *campaign.ActionCapability, field string, values []string, code, message string) {
	values = uniqueStrings(values)
	if len(values) == 0 {
		*capability = denyCapability(*capability, code, message)
		return
	}
	setStringEnum(capability.ArgumentSchema, field, values)
}

func setStringEnum(schema storage.JSONMap, field string, values []string) {
	property := schemaProperty(schema, field)
	enum := make([]any, 0, len(values))
	for _, value := range uniqueStrings(values) {
		enum = append(enum, value)
	}
	if len(enum) > 0 {
		property["enum"] = enum
	}
}

func setStringEnumIncludingEmpty(schema storage.JSONMap, field string, values []string) {
	property := schemaProperty(schema, field)
	enum := make([]any, 0, len(values))
	for _, value := range uniqueStrings(values) {
		enum = append(enum, value)
	}
	property["enum"] = enum
}

func cloneOption(option storage.JSONMap) storage.JSONMap {
	clone := make(storage.JSONMap, len(option)+1)
	for key, value := range option {
		clone[key] = value
	}
	return clone
}

func setIntegerMaximum(schema storage.JSONMap, field string, maximum int64) {
	schemaProperty(schema, field)["maximum"] = maximum
}

func schemaProperty(schema storage.JSONMap, field string) map[string]any {
	properties, _ := schema["properties"].(map[string]any)
	property, _ := properties[field].(map[string]any)
	return property
}

func cloneSchema(schema storage.JSONMap) storage.JSONMap {
	raw, _ := json.Marshal(schema)
	var clone storage.JSONMap
	_ = json.Unmarshal(raw, &clone)
	return clone
}

func optionStrings(options []storage.JSONMap, field string) []string {
	values := make([]string, 0, len(options))
	for _, option := range options {
		if value, ok := option[field].(string); ok && value != "" {
			values = append(values, value)
		}
	}
	return uniqueStrings(values)
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

func sortedDefinitions(state *storage.CampaignDomainState, kind string) []storage.GameplayDefinition {
	out := make([]storage.GameplayDefinition, 0)
	for _, definition := range state.Definitions {
		if definition.Kind == kind {
			out = append(out, definition)
		}
	}
	sort.Slice(out, func(left, right int) bool { return out[left].Key < out[right].Key })
	return out
}

func sortCapabilityOptions(options []storage.JSONMap) {
	sort.SliceStable(options, func(left, right int) bool {
		leftJSON, _ := json.Marshal(options[left])
		rightJSON, _ := json.Marshal(options[right])
		return string(leftJSON) < string(rightJSON)
	})
}

func activeEncounterFact(state *storage.CampaignDomainState) *storage.CampaignEncounter {
	for index := range state.Encounters {
		if state.Encounters[index].Status == "active" {
			return &state.Encounters[index]
		}
	}
	return nil
}

func itemQuantities(state *storage.CampaignDomainState) map[string]int64 {
	out := map[string]int64{}
	for _, stack := range state.ItemStacks {
		out[stack.ItemKey+":"+stack.QualityKey] = stack.Quantity
	}
	return out
}

func definitionStrings(value any) []string {
	values, _ := value.([]any)
	out := make([]string, 0, len(values))
	for _, raw := range values {
		if text, ok := raw.(string); ok && text != "" {
			out = append(out, text)
		}
	}
	return out
}

func conditionCategoryFromValue(value any, fallback string) string {
	entry, _ := value.(map[string]any)
	if category, ok := entry["category"].(string); ok && category != "" {
		return category
	}
	return fallback
}

func recipeCurrentlyAvailable(row storage.Campaign, state *storage.CampaignDomainState, recipe craftingrules.Recipe, owned map[string]int64) bool {
	if state.Character == nil || int64(state.Character.RealmRank) < recipe.MinRealmRank || state.Character.Mana < recipe.ManaCost ||
		len(recipe.AllowedLocationKeys) > 0 && !containsActionString(recipe.AllowedLocationKeys, row.CurrentLocationKey) {
		return false
	}
	if recipe.TechniqueKey != "" {
		found := false
		for _, technique := range state.Techniques {
			if technique.TechniqueKey == recipe.TechniqueKey && technique.Status == "active" && technique.Mastery >= recipe.MinTechniqueMastery {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	for _, required := range recipe.RequiredEquipmentKeys {
		found := false
		for _, item := range state.ItemInstances {
			if item.DefinitionKey == required && item.Durability > 0 && (item.State == "stored" || item.State == "equipped") {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	remaining := map[string]int64{}
	for key, quantity := range owned {
		remaining[key] = quantity
	}
	for _, ingredient := range recipe.Inputs {
		selected := false
		for _, option := range ingredient.Options {
			key := option.ItemKey + ":" + option.QualityKey
			if remaining[key] >= ingredient.Quantity {
				remaining[key] -= ingredient.Quantity
				selected = true
				break
			}
		}
		if !selected {
			return false
		}
	}
	return true
}

func marketStockFact(state *storage.CampaignDomainState, market, kind, key, quality string) *storage.CampaignMarketStock {
	for index := range state.MarketStocks {
		stock := &state.MarketStocks[index]
		if stock.MarketKey == market && stock.ItemKind == kind && stock.ItemKey == key && stock.QualityKey == quality {
			return stock
		}
	}
	return nil
}

func factionReputation(state *storage.CampaignDomainState, key string) int64 {
	for _, standing := range state.Factions {
		if standing.FactionKey == key {
			return int64(standing.Reputation)
		}
	}
	return 0
}

func countSellableEquipment(state *storage.CampaignDomainState, key, quality string) int64 {
	return int64(len(sellableEquipmentIDs(state, key, quality)))
}

func sellableEquipmentIDs(state *storage.CampaignDomainState, key, quality string) []string {
	out := make([]string, 0)
	for _, item := range state.ItemInstances {
		if item.DefinitionKey == key && item.QualityKey == quality && item.State == "stored" {
			out = append(out, item.ID.String())
		}
	}
	sort.Strings(out)
	return out
}

func entryRequirementsMet(state *storage.CampaignDomainState, content storage.JSONMap) bool {
	requirements, ok := factionrules.EntryRequirements(content)
	if !ok {
		return false
	}
	for _, requirement := range requirements {
		if requirement.MinRealmRank > 0 {
			if state.Character == nil || state.Character.RealmRank < requirement.MinRealmRank {
				return false
			}
			continue
		}
		granted := false
		for _, standing := range state.Factions {
			if standing.FactionKey == requirement.FactionKey && factionrules.IsGranted(standing.Permissions, requirement.PermissionKey) {
				granted = true
				break
			}
		}
		if !granted {
			return false
		}
	}
	return true
}

func regionHasResources(state *storage.CampaignDomainState, location storage.GameplayDefinition) bool {
	current := location
	for depth := 0; depth < 4; depth++ {
		if nodeType, _ := current.Content["node_type"].(string); nodeType == "region" {
			for _, region := range state.RegionStates {
				if region.RegionKey == current.Key {
					return region.ResourceLevel > 0
				}
			}
			return false
		}
		parent, _ := current.Content["parent_key"].(string)
		next, ok := state.Definition(storage.DefinitionLocation, parent)
		if !ok {
			return false
		}
		current = next
	}
	return false
}

func questAcceptsAt(content storage.JSONMap, location string) bool {
	values, _ := content["accept_location_keys"].([]any)
	if len(values) == 0 {
		return true
	}
	for _, value := range values {
		if value == location {
			return true
		}
	}
	return false
}

func numericInt(value any) (int64, bool) {
	switch number := value.(type) {
	case int64:
		return number, true
	case int:
		return int64(number), true
	case float64:
		return int64(number), number == math.Trunc(number)
	default:
		return 0, false
	}
}

func minimum(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}
