package campaignactions

import (
	"context"
	"errors"
	"fmt"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldactions"
	"sort"
	"time"

	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/components/craftingrules"
	"github.com/ialtone/fatecraft/internal/storage"
)

type selectedIngredient struct {
	stack    *storage.CampaignItemStack
	required int64
}

func learnRecipe(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := worldactions.AllowArguments(arguments, "recipe_key"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	key, err := worldactions.RequiredKey(arguments, "recipe_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionRecipe, key)
	if err != nil {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("未知配方 %s", key)
	}
	recipe, ok := craftingrules.Parse(map[string]any(definition.Content))
	if !ok {
		return storage.CampaignActionOutcome{}, worldactions.Invalid("配方定义无效: %s", key)
	}
	if _, err := execution.Store.KnownRecipe(execution.Campaign.ID, key); err == nil {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("已经掌握配方 %s", key)
	} else if !errors.Is(err, storage.ErrNotFound) {
		return storage.CampaignActionOutcome{}, err
	}
	if recipe.StartingKnown || recipe.SourceItemKey == "" {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("配方 %s 不能通过物品学习", key)
	}
	stack, err := execution.Store.ItemStack(execution.Campaign.ID, recipe.SourceItemKey, "common")
	if err != nil || stack.Quantity < 1 {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("缺少配方载体 %s", recipe.SourceItemKey)
	}
	stack.Quantity--
	stack.UpdatedAt = time.Now()
	if err := execution.Store.SaveItemStack(stack); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	known := &storage.CampaignKnownRecipe{
		CampaignID: execution.Campaign.ID, RecipeKey: key,
		LearnedAtGameTime: execution.Campaign.GameTime,
		SourceSnapshot:    storage.JSONMap{"name": definition.Name, "source_item_key": recipe.SourceItemKey},
		CreatedAt:         time.Now(),
	}
	if err := execution.Store.CreateKnownRecipe(known); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return worldactions.SingleEvent("craft.recipe_learned", storage.JSONMap{
		"recipe_key": key, "name": definition.Name, "source_item_key": recipe.SourceItemKey,
	}), nil
}

func executeRecipe(ctx context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := worldactions.AllowArguments(arguments, "recipe_key"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	key, err := worldactions.RequiredKey(arguments, "recipe_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionRecipe, key)
	if err != nil {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("未知配方 %s", key)
	}
	recipe, ok := craftingrules.Parse(map[string]any(definition.Content))
	if !ok {
		return storage.CampaignActionOutcome{}, worldactions.Invalid("配方定义无效: %s", key)
	}
	if _, err := execution.Store.KnownRecipe(execution.Campaign.ID, key); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return storage.CampaignActionOutcome{}, worldactions.Rejected("尚未掌握配方 %s", key)
		}
		return storage.CampaignActionOutcome{}, err
	}
	if len(recipe.AllowedLocationKeys) > 0 && !containsActionString(recipe.AllowedLocationKeys, execution.Campaign.CurrentLocationKey) {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("当前位置不能执行配方 %s", key)
	}
	character, err := cultivationStore(execution).Character(execution.Campaign.ID)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if int64(character.RealmRank) < recipe.MinRealmRank {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("境界不足，配方需要境界 rank %d", recipe.MinRealmRank)
	}
	if character.Mana < recipe.ManaCost {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("法力不足，需要 %d", recipe.ManaCost)
	}
	mastery := int64(0)
	if recipe.TechniqueKey != "" {
		technique, techniqueErr := cultivationStore(execution).Technique(execution.Campaign.ID, recipe.TechniqueKey)
		if techniqueErr != nil || technique.Status != "active" || technique.Mastery < recipe.MinTechniqueMastery {
			return storage.CampaignActionOutcome{}, worldactions.Rejected("生产技艺 %s 未掌握或熟练度不足", recipe.TechniqueKey)
		}
		mastery = technique.Mastery
	}
	if err := requireCraftEquipment(execution, recipe.RequiredEquipmentKeys); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	selected, err := selectRecipeInputs(execution, recipe.Inputs)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	chance := craftingrules.SuccessChance(recipe, mastery)
	insight := int64(50)
	if recipe.TechniqueKey != "" {
		insight = worldactions.ClampInt64(mastery/100, 0, 100)
	}
	rollContext := campaignRollContext(character)
	if len(recipe.RequiredEquipmentKeys) > 0 {
		rollContext.Advantages = append(rollContext.Advantages, "充分准备")
	}
	rollResult, err := worldactions.ResolveCampaignRoll(ctx, execution, campaign.CampaignRollRequest{
		Key: "craft." + key, Title: "炼制判定", Description: "炼制" + definition.Name, Category: "crafting",
		BaseSuccessRate: campaignRollBaseFromBPS(chance), Sides: 100, RelevantAttributes: []string{"悟性", "慧根"},
		AttributeWeights: map[string]float64{"悟性": 0.7, "慧根": 0.3},
		PlayerState:      campaignRollPlayerState(character, insight), Context: rollContext,
	})
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	roll := int64(worldactions.DeterministicUint64(execution, "craft.success:"+key)%10000) + 1
	success := roll <= chance
	if rollResult != nil {
		roll, chance, success = int64(rollResult.Rolled), int64(rollResult.SuccessRate)*100, rollResult.Success
	}
	consumptionBPS := recipe.FailureInputLossBPS
	if success {
		consumptionBPS = 10000
	}
	consumed, err := consumeSelectedInputs(execution, selected, consumptionBPS)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	character.Mana -= recipe.ManaCost
	exhausted := advanceCharacterAge(character, execution.Campaign.GameTime, recipe.CraftMinutes)
	products := recipe.FailureOutputs
	eventType := "craft.failed"
	if success {
		products = recipe.Outputs
		eventType = "craft.completed"
	}
	produced, err := grantCraftProducts(execution, key, products)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	outcome := storage.CampaignActionOutcome{
		GameTimeDelta: recipe.CraftMinutes,
		Events: []storage.CampaignEventDraft{{
			EventType: eventType, Visibility: storage.CampaignVisibilityPlayer,
			Payload: storage.JSONMap{
				"recipe_key": key, "craft_type": recipe.CraftType, "success": success,
				"roll": roll, "success_chance_bps": chance, "consumed": consumed, "produced": produced,
			},
		}},
		Result: storage.JSONMap{
			"recipe_key": key, "craft_type": recipe.CraftType, "success": success,
			"roll": roll, "success_chance_bps": chance, "consumed": consumed, "produced": produced,
		},
	}
	worldactions.AttachCampaignRoll(&outcome, rollResult)
	if !success {
		applyCraftFailureEffect(execution, character, recipe.FailureEffect, &outcome)
	}
	character.UpdatedAt = time.Now()
	if err := cultivationStore(execution).SaveCharacter(character); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if character.Health == 0 {
		status := storage.CampaignStatusDead
		outcome.Status = &status
		outcome.Result["campaign_status"] = status
		outcome.Events = append(outcome.Events, storage.CampaignEventDraft{
			EventType: "character.died", Visibility: storage.CampaignVisibilityPlayer,
			Payload: storage.JSONMap{"cause_key": "craft.failure", "recipe_key": key},
		})
	} else {
		appendLifespanResolution(&outcome, character, exhausted)
	}
	return outcome, nil
}

func requireCraftEquipment(execution campaign.ActionExecution, required []string) error {
	if len(required) == 0 {
		return nil
	}
	instances, err := execution.Store.ItemInstances(execution.Campaign.ID)
	if err != nil {
		return err
	}
	available := map[string]bool{}
	for _, item := range instances {
		if (item.State == "stored" || item.State == "equipped") && item.Durability > 0 {
			available[item.DefinitionKey] = true
		}
	}
	for _, key := range required {
		if !available[key] {
			return worldactions.Rejected("缺少可用生产器具 %s", key)
		}
	}
	return nil
}

func selectRecipeInputs(execution campaign.ActionExecution, ingredients []craftingrules.Ingredient) ([]selectedIngredient, error) {
	cache := map[string]*storage.CampaignItemStack{}
	reserved := map[string]int64{}
	selected := make([]selectedIngredient, 0, len(ingredients))
	for _, ingredient := range ingredients {
		found := false
		for _, option := range ingredient.Options {
			identity := option.ItemKey + ":" + option.QualityKey
			stack, loaded := cache[identity]
			if !loaded {
				var err error
				stack, err = execution.Store.ItemStack(execution.Campaign.ID, option.ItemKey, option.QualityKey)
				if errors.Is(err, storage.ErrNotFound) {
					stack = nil
				} else if err != nil {
					return nil, err
				}
				cache[identity] = stack
			}
			if stack == nil || stack.Quantity-reserved[identity] < ingredient.Quantity {
				continue
			}
			reserved[identity] += ingredient.Quantity
			selected = append(selected, selectedIngredient{stack: stack, required: ingredient.Quantity})
			found = true
			break
		}
		if !found {
			return nil, worldactions.Rejected("制作材料不足")
		}
	}
	return selected, nil
}

func consumeSelectedInputs(execution campaign.ActionExecution, selected []selectedIngredient, bps int64) ([]storage.JSONMap, error) {
	rows := map[string]*storage.CampaignItemStack{}
	amounts := map[string]int64{}
	for _, input := range selected {
		identity := input.stack.ItemKey + ":" + input.stack.QualityKey
		loss := (input.required*bps + 9999) / 10000
		rows[identity] = input.stack
		amounts[identity] += loss
	}
	out := make([]storage.JSONMap, 0, len(rows))
	identities := make([]string, 0, len(rows))
	for identity := range rows {
		identities = append(identities, identity)
	}
	sort.Strings(identities)
	for _, identity := range identities {
		row := rows[identity]
		amount := amounts[identity]
		if row.Quantity < amount {
			return nil, worldactions.Rejected("制作材料数量发生冲突")
		}
		row.Quantity -= amount
		row.UpdatedAt = time.Now()
		if err := execution.Store.SaveItemStack(row); err != nil {
			return nil, err
		}
		out = append(out, storage.JSONMap{
			"item_key": row.ItemKey, "quality_key": row.QualityKey, "quantity": amount,
		})
	}
	return out, nil
}

func grantCraftProducts(execution campaign.ActionExecution, recipeKey string, products []craftingrules.Product) ([]storage.JSONMap, error) {
	out := make([]storage.JSONMap, 0, len(products))
	for index, product := range products {
		quality, ok := craftingrules.ChooseQuality(product.QualityWeights, worldactions.DeterministicUint64(execution, fmt.Sprintf("craft.quality:%s:%d", recipeKey, index)))
		if !ok {
			return nil, worldactions.Invalid("配方产物品质定义无效")
		}
		if product.Kind == "item" {
			if err := worldactions.AddItemStack(execution, product.Key, quality, product.Quantity, "craft:"+recipeKey); err != nil {
				return nil, err
			}
		} else {
			for unit := int64(0); unit < product.Quantity; unit++ {
				if err := worldactions.CreateEquipmentInstance(execution, product.Key, quality, "craft:"+recipeKey); err != nil {
					return nil, err
				}
			}
		}
		out = append(out, storage.JSONMap{
			"kind": product.Kind, "key": product.Key, "quality_key": quality, "quantity": product.Quantity,
		})
	}
	return out, nil
}

func applyCraftFailureEffect(execution campaign.ActionExecution, character *storage.CampaignCharacter, effect craftingrules.FailureEffect, outcome *storage.CampaignActionOutcome) {
	character.Health = worldactions.ClampInt64(character.Health-effect.HealthDamage, 0, character.MaxHealth)
	character.Mana = worldactions.ClampInt64(character.Mana-effect.ManaDamage, 0, character.MaxMana)
	if effect.EffectKey == "" {
		return
	}
	target := character.LongTermEffects
	if effect.Category == "injury" {
		target = character.Injuries
	}
	if target == nil {
		target = storage.JSONMap{}
		if effect.Category == "injury" {
			character.Injuries = target
		} else {
			character.LongTermEffects = target
		}
	}
	target[effect.EffectKey] = map[string]any{
		"category": effect.Category, "severity": effect.Severity, "description": effect.Description,
		"applied_at_game_time": execution.Campaign.GameTime + outcome.GameTimeDelta,
		"duration_minutes":     effect.DurationMinutes,
	}
	outcome.Events = append(outcome.Events, storage.CampaignEventDraft{
		EventType: "character.condition_applied", Visibility: storage.CampaignVisibilityPlayer,
		Payload: storage.JSONMap{
			"category": effect.Category, "effect_key": effect.EffectKey,
			"severity": effect.Severity, "duration_minutes": effect.DurationMinutes,
		},
	})
}

func containsActionString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
