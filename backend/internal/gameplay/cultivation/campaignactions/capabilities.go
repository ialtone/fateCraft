package campaignactions

import (
	"context"
	"fmt"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldactions"
	"math"
	"slices"

	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/components/craftingrules"
	"github.com/ialtone/fatecraft/internal/gameplay/components/factionrules"
	"github.com/ialtone/fatecraft/internal/gameplay/components/gatheringrules"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/ascensionrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/effectrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/encounterrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/practicerules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/trainingrules"
	"github.com/ialtone/fatecraft/internal/storage"
)

type cultivationCapabilityProvider struct{}

func (cultivationCapabilityProvider) Project(
	ctx context.Context,
	input campaign.CapabilityContext,
	definition campaign.ActionDefinition,
) (campaign.ActionCapability, error) {
	capability := campaign.ActionCapability{
		Available: true, ArgumentSchema: worldactions.CloneSchema(definition.ArgumentSchema),
		Options: []storage.JSONMap{}, Metadata: storage.JSONMap{},
	}
	state := input.Domain
	if state == nil {
		return worldactions.DenyCapability(capability, "campaign.state_unavailable", "玩法状态不可用"), nil
	}
	if active := activeEncounterFact(state); active != nil && definition.Key != ActionEncounterAct && definition.Key != ActionEncounterFlee && definition.Key != ActionAbandon {
		return worldactions.DenyCapability(capability, "encounter.active", fmt.Sprintf("必须先解决活动遭遇 %s", active.EncounterKey)), nil
	}

	switch definition.Key {
	case ActionAbandon:
		if input.ActorType != "player" {
			return worldactions.DenyCapability(capability, "action.forbidden", "中止本世必须由玩家确认"), nil
		}
	case ActionRetreat, ActionRecover:
		views, err := trainingrules.Views(input.Campaign, state)
		if err != nil {
			return capability, err
		}
		mode := "retreat"
		if definition.Key == ActionRecover {
			mode = "recovery"
		}
		for _, v := range views {
			if v.Mode == mode && v.Available {
				capability.Options = append(capability.Options, storage.JSONMap{"plan_key": v.Key, "name": v.Name, "days": v.Days, "costs": v.CostText, "mana_cost": v.ManaCost, "cultivation_gain": v.CultivationGain, "technique": v.TechniqueName})
			}
		}
		worldactions.ApplySingleEnum(&capability, "plan_key", worldactions.OptionStrings(capability.Options, "plan_key"), "training.unavailable", "当前没有合适方案，请检查地点、材料、功法、寿元及限时任务")
	case ActionBeginAscension:
		view, err := ascensionrules.Inspect(input.Campaign, state)
		if err != nil {
			return capability, err
		}
		if view == nil {
			return worldactions.DenyCapability(capability, "ascension.unavailable", "当前版本未开放最终渡劫"), nil
		}
		capability.Metadata["ascension"] = view
		if !view.Ready {
			return worldactions.DenyCapability(capability, "ascension.not_ready", view.Reason), nil
		}
	case ActionSubmitQuest:
		worldactions.ProjectQuestCapability(&capability, input.Campaign, state, false)
		view, err := ascensionrules.Inspect(input.Campaign, state)
		if err != nil {
			return capability, err
		}
		capability.Options = slices.DeleteFunc(capability.Options, func(option storage.JSONMap) bool {
			key, _ := option["quest_key"].(string)
			d, exists := state.Definition(storage.DefinitionQuest, key)
			ending, _ := d.Content["campaign_outcome_key"].(string)
			return exists && ending != "" && (input.ActorType != "player" || view == nil || !view.CanFinish)
		})
		worldactions.ApplySingleEnum(&capability, "quest_key", worldactions.OptionStrings(capability.Options, "quest_key"), "quest.not_ready", "当前没有满足提交条件的任务；飞升需玩家通过最终天劫后选择")
		if input.ActorType == "player" {
			plans := []contract.ActionPlan{}
			for _, option := range capability.Options {
				key, _ := option["quest_key"].(string)
				d, exists := state.Definition(storage.DefinitionQuest, key)
				ending, _ := d.Content["campaign_outcome_key"].(string)
				if exists && ending != "" {
					plans = append(plans, contract.ActionPlan{Label: d.Name, Arguments: map[string]any{"quest_key": key}})
				}
			}
			if len(plans) > 0 {
				capability.Metadata["ending_plans"] = plans
			}
		}

	case ActionRenameCharacter:
		requireCharacter(&capability, state)
	case ActionPractice:
		projectPracticeCapability(&capability, state)
	case ActionBreakthrough:
		projectBreakthroughCapability(&capability, input.Campaign, state)
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
		worldactions.ApplySingleEnum(&capability, "technique_key", worldactions.OptionStrings(capability.Options, "technique_key"), "technique.not_equipped", "当前没有已装配功法")
	case ActionAdjustVitals:
		requireCharacter(&capability, state)
		if input.ActorType == "model" {
			for _, key := range []string{"health_delta", "mana_delta", "spiritual_sense_delta"} {
				setIntegerMaximum(capability.ArgumentSchema, key, 0)
			}
		}
	case ActionApplyCondition:
		requireCharacter(&capability, state)
	case ActionResolveCondition:
		projectResolveConditionCapability(&capability, state, input.ActorType)
	case ActionUseItem:
		projectUseItemCapability(&capability, state)
	case ActionGainEquipment:
		worldactions.ProjectGrantDefinitionCapability(&capability, state, storage.DefinitionEquipment, "definition_key", false)
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
		worldactions.ApplySingleEnum(&capability, "item_instance_id", worldactions.OptionStrings(capability.Options, "item_instance_id"), "equipment.not_equipped", "当前没有已装备物品")
	case ActionLearnRecipe:
		projectRecipeCapability(&capability, input.Campaign, state, true)
	case ActionCraftRecipe:
		projectRecipeCapability(&capability, input.Campaign, state, false)
	case ActionGather:
		projectGatherCapability(&capability, input.Campaign, state)
	case ActionStartEncounter:
		projectStartEncounterCapability(&capability, input.Campaign, state)
	case ActionEncounterAct:
		projectEncounterActCapability(&capability, state)
	case ActionEncounterFlee:
		projectEncounterFleeCapability(&capability, state)
	default:
		return (worldactions.CapabilityProvider{EntryAvailable: entryRequirementsMet}).Project(ctx, input, definition)
	}
	if len(capability.Metadata) == 0 {
		capability.Metadata = nil
	}
	worldactions.SortCapabilityOptions(capability.Options)
	return capability, nil
}

func projectPracticeCapability(capability *campaign.ActionCapability, state *storage.CampaignDomainState) {
	effects, err := effectrules.Equipped(state)
	if err != nil {
		*capability = worldactions.DenyCapability(*capability, "technique.invalid", err.Error())
		return
	}
	for _, technique := range state.Techniques {
		definition, ok := state.Definition(storage.DefinitionTechnique, technique.TechniqueKey)
		if !ok {
			continue
		}
		rules := practicerules.Evaluate(practicerules.Facts{
			Character: state.Character, Technique: technique, Definition: definition, EquippedEffects: effects,
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
	keys := worldactions.OptionStrings(capability.Options, "technique_key")
	if len(keys) == 0 {
		*capability = worldactions.DenyCapability(*capability, "cultivation.unavailable", "当前没有可修炼的已装配功法或资源不足")
		return
	}
	worldactions.SetStringEnum(capability.ArgumentSchema, "technique_key", keys)
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

func projectBreakthroughCapability(capability *campaign.ActionCapability, row storage.Campaign, state *storage.CampaignDomainState) {
	if !requireCharacter(capability, state) {
		return
	}
	character := state.Character
	if character.CultivationProgress < character.CultivationRequired {
		*capability = worldactions.DenyCapability(*capability, "cultivation.progress_insufficient", fmt.Sprintf(
			"修为尚未圆满：%d/%d", character.CultivationProgress, character.CultivationRequired,
		))
		return
	}
	current, ok := state.Definition(storage.DefinitionRealm, character.RealmKey)
	next, _ := current.Content["next_realm_key"].(string)
	if !ok || next == "" {
		*capability = worldactions.DenyCapability(*capability, "cultivation.no_next_realm", "当前境界没有可突破的下一境界")
		return
	}
	minutes := worldactions.DefinitionIntDefault(current.Content["breakthrough_minutes"], 24*60)
	if err := breakthroughTimeReady(character, row.GameTime, minutes); err != nil {
		*capability = worldactions.DenyCapability(*capability, "cultivation.time_unavailable", err.Error())
		return
	}
	capability.Metadata["next_realm_key"] = next
	capability.Metadata["minutes"] = minutes
}

func projectLearnTechniqueCapability(capability *campaign.ActionCapability, state *storage.CampaignDomainState) {
	if !requireCharacter(capability, state) {
		return
	}
	known := map[string]bool{}
	for _, row := range state.Techniques {
		known[row.TechniqueKey] = true
	}
	owned := worldactions.ItemQuantities(state)
	for _, definition := range worldactions.SortedDefinitions(state, storage.DefinitionTechnique) {
		if known[definition.Key] || int64(state.Character.RealmRank) < worldactions.DefinitionIntDefault(definition.Content["minimum_realm_rank"], 0) {
			continue
		}
		source, _ := definition.Content["source_item_key"].(string)
		if source != "" && owned[source+":common"] < 1 {
			continue
		}
		capability.Options = append(capability.Options, storage.JSONMap{"technique_key": definition.Key, "name": definition.Name, "source_item_key": source})
	}
	worldactions.ApplySingleEnum(capability, "technique_key", worldactions.OptionStrings(capability.Options, "technique_key"), "technique.none_learnable", "当前没有可学习功法")
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
		*capability = worldactions.DenyCapability(*capability, "technique.none_equippable", "当前没有可装配功法")
		return
	}
	worldactions.SetStringEnum(capability.ArgumentSchema, "technique_key", worldactions.OptionStrings(capability.Options, "technique_key"))
	worldactions.SetStringEnum(capability.ArgumentSchema, "slot", worldactions.OptionStrings(capability.Options, "slot"))
}

func projectResolveConditionCapability(capability *campaign.ActionCapability, state *storage.CampaignDomainState, actor string) {
	if !requireCharacter(capability, state) {
		return
	}
	for key, value := range state.Character.Injuries {
		if actor == "model" && key == "breakthrough_backlash" {
			continue
		}
		category := conditionCategoryFromValue(value, "injury")
		capability.Options = append(capability.Options, storage.JSONMap{"category": category, "effect_key": key})
	}
	for key, value := range state.Character.LongTermEffects {
		category := conditionCategoryFromValue(value, "long_term")
		capability.Options = append(capability.Options, storage.JSONMap{"category": category, "effect_key": key})
	}
	if len(capability.Options) == 0 {
		*capability = worldactions.DenyCapability(*capability, "condition.none_active", "当前没有可移除状态")
		return
	}
	worldactions.SetStringEnum(capability.ArgumentSchema, "effect_key", worldactions.OptionStrings(capability.Options, "effect_key"))
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
	worldactions.ApplySingleEnum(capability, "item_key", worldactions.OptionStrings(capability.Options, "item_key"), "inventory.no_usable_item", "当前没有可使用物品")
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
		*capability = worldactions.DenyCapability(*capability, "equipment.none_equippable", "当前没有可装配物品")
		return
	}
	worldactions.SetStringEnum(capability.ArgumentSchema, "item_instance_id", worldactions.OptionStrings(capability.Options, "item_instance_id"))
	worldactions.SetStringEnum(capability.ArgumentSchema, "slot", worldactions.OptionStrings(capability.Options, "slot"))
}

func projectRecipeCapability(capability *campaign.ActionCapability, row storage.Campaign, state *storage.CampaignDomainState, learn bool) {
	known := map[string]bool{}
	for _, recipe := range state.KnownRecipes {
		known[recipe.RecipeKey] = true
	}
	owned := worldactions.ItemQuantities(state)
	for _, definition := range worldactions.SortedDefinitions(state, storage.DefinitionRecipe) {
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
	worldactions.ApplySingleEnum(capability, "recipe_key", worldactions.OptionStrings(capability.Options, "recipe_key"), code, message)
}

func projectGatherCapability(capability *campaign.ActionCapability, row storage.Campaign, state *storage.CampaignDomainState) {
	location, ok := state.Definition(storage.DefinitionLocation, row.CurrentLocationKey)
	if !ok {
		*capability = worldactions.DenyCapability(*capability, "location.unavailable", "当前位置定义不存在")
		return
	}
	table, ok := gatheringrules.Parse(location.Content["gathering"])
	if !ok || !regionHasResources(state, location) {
		*capability = worldactions.DenyCapability(*capability, "gathering.unavailable", "当前位置没有可采集资源或区域资源已枯竭")
		return
	}
	for _, entry := range table.Entries {
		capability.Options = append(capability.Options, storage.JSONMap{
			"item_key": entry.ItemKey, "quality_key": entry.QualityKey,
			"quantity_preview": storage.JSONMap{"minimum": entry.MinQuantity, "maximum": entry.MaxQuantity},
			"minutes":          table.Minutes,
		})
	}
	worldactions.SetStringEnum(capability.ArgumentSchema, "item_key", worldactions.OptionStrings(capability.Options, "item_key"))
}

func projectStartEncounterCapability(capability *campaign.ActionCapability, row storage.Campaign, state *storage.CampaignDomainState) {
	if activeEncounterFact(state) != nil {
		*capability = worldactions.DenyCapability(*capability, "encounter.active", "已有活动遭遇")
		return
	}
	if state.Character == nil || state.Character.Health <= 0 {
		*capability = worldactions.DenyCapability(*capability, "character.unavailable", "角色已无法开始遭遇")
		return
	}
	for _, definition := range worldactions.SortedDefinitions(state, storage.DefinitionEncounter) {
		if c, _ := ascensionrules.FromState(state); c != nil && definition.Key == c.EncounterKey {
			continue
		}
		if definition.Visibility == storage.CampaignVisibilityInternal {
			continue
		}
		rules, ok := encounterrules.Parse(map[string]any(definition.Content))
		if ok && rules.LocationKey == row.CurrentLocationKey {
			capability.Options = append(capability.Options, storage.JSONMap{"encounter_key": definition.Key, "name": definition.Name})
		}
	}
	worldactions.ApplySingleEnum(capability, "encounter_key", worldactions.OptionStrings(capability.Options, "encounter_key"), "encounter.none_available", "当前位置没有可开始遭遇")
}

func projectEncounterActCapability(capability *campaign.ActionCapability, state *storage.CampaignDomainState) {
	active := activeEncounterFact(state)
	if active == nil {
		*capability = worldactions.DenyCapability(*capability, "encounter.none_active", "当前没有活动遭遇")
		return
	}
	if state.Character == nil || state.Character.Health <= 0 {
		*capability = worldactions.DenyCapability(*capability, "character.unavailable", "角色已无法继续遭遇")
		return
	}
	definition, exists := state.Definition(storage.DefinitionEncounter, active.EncounterKey)
	rules, valid := encounterrules.Parse(map[string]any(definition.Content))
	if !exists || !valid {
		*capability = worldactions.DenyCapability(*capability, "encounter.definition_unavailable", "活动遭遇规则不可用")
		return
	}
	worldactions.SetStringEnum(capability.ArgumentSchema, "encounter_id", []string{active.ID.String()})
	capability.Options = append(capability.Options, storage.JSONMap{"encounter_id": active.ID.String(), "move": "defend"})
	effects, err := effectrules.Equipped(state)
	if err != nil {
		*capability = worldactions.DenyCapability(*capability, "technique.invalid", err.Error())
		return
	}
	reduceCost := func(cost int64) int64 {
		if cost > 0 {
			return max(int64(1), cost-effects["mana_cost_reduction"])
		}
		return cost
	}
	techniques := make([]string, 0)
	if state.Character.Mana >= reduceCost(rules.PlayerManaCost) {
		capability.Options = append(capability.Options, storage.JSONMap{
			"encounter_id": active.ID.String(), "move": "attack",
			"cost_preview": storage.JSONMap{"resource_key": "mana", "amount": reduceCost(rules.PlayerManaCost)},
		})
		for _, technique := range state.Techniques {
			if technique.Status != "active" || technique.EquippedSlot == nil {
				continue
			}
			techniqueDefinition, ok := state.Definition(storage.DefinitionTechnique, technique.TechniqueKey)
			if !ok {
				continue
			}
			if _, ok := effectrules.MasteredEffects(techniqueDefinition.Content, technique.Mastery, technique.LearnedLayer); !ok {
				continue
			}
			manaCost := rules.PlayerManaCost + worldactions.DefinitionIntDefault(
				techniqueDefinition.Content["combat_mana_cost"], worldactions.DefinitionIntDefault(techniqueDefinition.Content["mana_cost"], 0),
			)
			manaCost = reduceCost(manaCost)
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
	worldactions.SetStringEnum(capability.ArgumentSchema, "move", worldactions.OptionStrings(capability.Options, "move"))
	worldactions.SetStringEnumIncludingEmpty(capability.ArgumentSchema, "technique_key", techniques)
}

func projectEncounterFleeCapability(capability *campaign.ActionCapability, state *storage.CampaignDomainState) {
	active := activeEncounterFact(state)
	if active == nil {
		*capability = worldactions.DenyCapability(*capability, "encounter.none_active", "当前没有活动遭遇")
		return
	}
	if state.Character == nil || state.Character.Health <= 0 {
		*capability = worldactions.DenyCapability(*capability, "character.unavailable", "角色已无法撤离遭遇")
		return
	}
	worldactions.SetStringEnum(capability.ArgumentSchema, "encounter_id", []string{active.ID.String()})
	capability.Options = append(capability.Options, storage.JSONMap{"encounter_id": active.ID.String()})
}

func requireCharacter(capability *campaign.ActionCapability, state *storage.CampaignDomainState) bool {
	if state.Character != nil && state.Character.Health > 0 {
		return true
	}
	*capability = worldactions.DenyCapability(*capability, "character.unavailable", "角色领域状态不可用")
	return false
}

func setIntegerMaximum(schema storage.JSONMap, field string, maximum int64) {
	worldactions.SchemaProperty(schema, field)["maximum"] = maximum
}

func activeEncounterFact(state *storage.CampaignDomainState) *storage.CampaignEncounter {
	for index := range state.Encounters {
		if state.Encounters[index].Status == "active" {
			return &state.Encounters[index]
		}
	}
	return nil
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
