package contentrules

import (
	"fmt"
	"github.com/ialtone/fatecraft/internal/gameplay/components/craftingrules"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldcontent"
	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/ascensionrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/effectrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/encounterrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/originrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/progressrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/trainingrules"
)

var ErrInvalidContent = worldcontent.ErrInvalidContent

func ValidateShape(definition gameplay.Definition) error {
	content := definition.Content
	switch definition.Kind {
	case "realm":
		if _, ok := worldcontent.PositiveDefinitionInt(content["cultivation_required"]); !ok {
			return fmt.Errorf("cultivation_required 必须是正整数")
		}
		if rank, ok := worldcontent.NonNegativeDefinitionInt(content["rank"]); !ok || rank > 1000 {
			return fmt.Errorf("rank 必须是 0 到 1000 的整数")
		}
		if raw, exists := content["next_realm_key"]; exists && raw != nil {
			next, ok := raw.(string)
			if !ok || !worldcontent.StableDefinitionKey(next) || next == definition.Key {
				return fmt.Errorf("next_realm_key 必须是其他境界的稳定 key")
			}
		}
		if _, ok := worldcontent.PositiveDefinitionInt(content["lifespan_days"]); !ok {
			return fmt.Errorf("lifespan_days 必须是正整数")
		}
		if stages, ok := worldcontent.StringList(content["stages"]); !ok || len(stages) == 0 {
			return fmt.Errorf("stages 必须是非空稳定 key 数组")
		}
		for _, field := range []string{"max_health", "max_mana", "max_spiritual_sense"} {
			if raw, exists := content[field]; exists {
				if value, ok := worldcontent.PositiveDefinitionInt(raw); !ok || value > 1000000000 {
					return fmt.Errorf("%s 必须是 1 到 1000000000 的整数", field)
				}
			}
		}
	case "technique":
		category, _ := content["category"].(string)
		if !map[string]bool{"cultivation": true, "body": true, "movement": true, "spell": true, "secret": true, "craft": true}[category] {
			return fmt.Errorf("category 无效")
		}
		layers, layersOK := worldcontent.PositiveDefinitionInt(content["max_layer"])
		if !layersOK || layers > 100 {
			return fmt.Errorf("max_layer 必须是 1 到 100 的整数")
		}
		if _, ok := worldcontent.PositiveDefinitionInt(content["practice_minutes"]); !ok {
			return fmt.Errorf("practice_minutes 必须是正整数")
		}
		slots, ok := worldcontent.StringList(content["allowed_slots"])
		if !ok || len(slots) == 0 {
			return fmt.Errorf("allowed_slots 必须是非空稳定 key 数组")
		}
		if _, exists := content["mastery_per_layer"]; exists {
			return fmt.Errorf("功法已使用 0–10000 归一化熟练度，请移除 mastery_per_layer")
		}
		starting, _ := content["starting_known"].(bool)
		pool, hasPool := content["starting_pool"]
		poolKey, poolOK := pool.(string)
		if hasPool && (!poolOK || !worldcontent.StableDefinitionKey(poolKey)) {
			return fmt.Errorf("starting_pool 必须是稳定 key")
		}
		if starting && poolKey != "" {
			return fmt.Errorf("starting_known 与 starting_pool 不能同时声明")
		}
		if rawWeight, hasWeight := content["starting_weight"]; hasWeight {
			weight, valid := worldcontent.PositiveDefinitionInt(rawWeight)
			if poolKey == "" || !valid || weight > 100000 {
				return fmt.Errorf("starting_weight 只允许用于 starting_pool 且必须是 1 到 100000 的整数")
			}
		}
		if slot, _ := content["starting_slot"].(string); slot != "" {
			if (!starting && poolKey == "") || !worldcontent.ContainsString(slots, slot) {
				return fmt.Errorf("starting_slot 只允许用于固定或候选池开局功法且必须属于 allowed_slots")
			}
		}
		if source, _ := content["source_item_key"].(string); source != "" && !worldcontent.StableDefinitionKey(source) {
			return fmt.Errorf("source_item_key 必须是稳定 key")
		}
		if raw, exists := content["layer_effects"]; exists {
			layerEffects, ok := raw.([]any)
			if !ok || len(layerEffects) > effectrules.MaxLayerEffects {
				return fmt.Errorf("layer_effects 必须是最多 %d 项的数组", effectrules.MaxLayerEffects)
			}
			seenLayers := map[int64]bool{}
			for _, rawLayer := range layerEffects {
				layer, ok := worldcontent.DefinitionObject(rawLayer)
				number, numberOK := worldcontent.PositiveDefinitionInt(layer["layer"])
				_, effectsOK := effectrules.ParseEffects(layer["effects"])
				if !ok || len(layer) != 2 || !numberOK || number > layers || seenLayers[number] || !effectsOK {
					return fmt.Errorf("layer_effects 必须使用不重复且不超过 max_layer 的层数和受支持效果")
				}
				seenLayers[number] = true
			}
		}
	case "item":
		if _, ok := content["stackable"].(bool); !ok {
			return fmt.Errorf("stackable 必须是布尔值")
		}
		if raw, exists := content["model_grantable"]; exists {
			if _, ok := raw.(bool); !ok {
				return fmt.Errorf("model_grantable 必须是布尔值")
			}
		}
		if raw, exists := content["consumable"]; exists {
			if _, ok := raw.(bool); !ok {
				return fmt.Errorf("consumable 必须是布尔值")
			}
		}
		if raw, exists := content["effects"]; exists {
			effects, ok := worldcontent.DefinitionObject(raw)
			if !ok || len(effects) > 8 {
				return fmt.Errorf("effects 必须是最多 8 项的对象")
			}
			for key, rawAmount := range effects {
				amount, valid := worldcontent.SignedDefinitionInt(rawAmount)
				if !map[string]bool{"health": true, "mana": true, "spiritual_sense": true, "cultivation_progress": true}[key] ||
					!valid || amount < -10000 || amount > 10000 {
					return fmt.Errorf("effects 包含不受支持或越界的效果")
				}
			}
		}
	case "equipment":
		if raw, exists := content["model_grantable"]; exists {
			if _, ok := raw.(bool); !ok {
				return fmt.Errorf("model_grantable 必须是布尔值")
			}
		}
		if slots, ok := worldcontent.StringList(content["allowed_slots"]); !ok || len(slots) == 0 {
			return fmt.Errorf("allowed_slots 必须是非空稳定 key 数组")
		}
		if _, ok := worldcontent.PositiveDefinitionInt(content["durability_max"]); !ok {
			return fmt.Errorf("durability_max 必须是正整数")
		}
		if raw, exists := content["bonuses"]; exists {
			if _, ok := effectrules.ParseEffects(raw); !ok {
				return fmt.Errorf("bonuses 包含不受支持或越界的效果")
			}
		}
	case "recipe":
		if _, ok := craftingrules.Parse(map[string]any(content)); !ok {
			return fmt.Errorf("配方结构、数值边界或品质权重无效")
		}
	case "encounter":
		if _, ok := encounterrules.Parse(map[string]any(content)); !ok {
			return fmt.Errorf("遭遇地点、对手数值、回合规则或奖励结构无效")
		}

	default:
		return worldcontent.ValidateShape(definition)
	}
	return nil
}
func validateProgression(contract *gameplay.RuntimeContract, definitions []gameplay.Definition) error {
	catalog := map[string]gameplay.Definition{}
	for _, d := range definitions {
		catalog[d.Kind+":"+d.Key] = d
	}
	realmCount := 0
	realmRanks := map[int64]string{}
	startingTechniqueSlots := map[string]string{}
	startingTechniquePools := map[string]int{}
	for _, definition := range definitions {
		switch definition.Kind {
		case "realm":
			realmCount++
			rank, rankOK := worldcontent.NonNegativeDefinitionInt(definition.Content["rank"])
			if !rankOK {
				return fmt.Errorf("%w: realm:%s 的 rank 无效", ErrInvalidContent, definition.Key)
			}
			if previous, exists := realmRanks[rank]; exists {
				return fmt.Errorf("%w: realm:%s 与 realm:%s 使用了相同 rank %d", ErrInvalidContent, definition.Key, previous, rank)
			}
			realmRanks[rank] = definition.Key
			if next, _ := definition.Content["next_realm_key"].(string); next != "" {
				nextDefinition, ok := catalog["realm"+":"+next]
				if !ok {
					return fmt.Errorf("%w: realm:%s 引用了未知 next_realm_key %s", ErrInvalidContent, definition.Key, next)
				}
				nextRank, nextRankOK := worldcontent.NonNegativeDefinitionInt(nextDefinition.Content["rank"])
				if !nextRankOK || nextRank <= rank {
					return fmt.Errorf("%w: realm:%s 的 next_realm_key %s 必须指向更高 rank", ErrInvalidContent, definition.Key, next)
				}
			}
		case "technique":
			if source, _ := definition.Content["source_item_key"].(string); source != "" {
				if _, ok := catalog["item"+":"+source]; !ok {
					return fmt.Errorf("%w: technique:%s 引用了未知 source_item_key %s", ErrInvalidContent, definition.Key, source)
				}
			}
			starting, _ := definition.Content["starting_known"].(bool)
			pool, _ := definition.Content["starting_pool"].(string)
			if pool != "" {
				startingTechniquePools[pool]++
			}
			if starting || pool != "" {
				if slot, _ := definition.Content["starting_slot"].(string); slot != "" {
					owner := "fixed:" + definition.Key
					if pool != "" {
						owner = "pool:" + pool
					}
					if previous, exists := startingTechniqueSlots[slot]; exists && previous != owner {
						return fmt.Errorf("%w: technique:%s 与 %s 可能使用相同初始槽位 %s", ErrInvalidContent, definition.Key, previous, slot)
					}
					startingTechniqueSlots[slot] = owner
				}
			}
		case "recipe":
			recipe, _ := craftingrules.Parse(map[string]any(definition.Content))
			if recipe.SourceItemKey != "" {
				if err := worldcontent.RequireStackableDefinition(catalog, recipe.SourceItemKey, "recipe:"+definition.Key+" 的 source_item_key"); err != nil {
					return err
				}
			}
			for _, ingredient := range recipe.Inputs {
				for _, option := range ingredient.Options {
					if err := worldcontent.RequireStackableDefinition(catalog, option.ItemKey, "recipe:"+definition.Key+" 的材料"); err != nil {
						return err
					}
				}
			}
			for _, equipmentKey := range recipe.RequiredEquipmentKeys {
				if _, ok := catalog["equipment"+":"+equipmentKey]; !ok {
					return fmt.Errorf("%w: recipe:%s 引用了未知生产器具 %s", ErrInvalidContent, definition.Key, equipmentKey)
				}
			}
			if recipe.TechniqueKey != "" {
				technique, ok := catalog["technique"+":"+recipe.TechniqueKey]
				category, _ := technique.Content["category"].(string)
				if !ok || category != "craft" {
					return fmt.Errorf("%w: recipe:%s 的 technique_key 必须引用 craft 类功法", ErrInvalidContent, definition.Key)
				}
			}
			for _, locationKey := range recipe.AllowedLocationKeys {
				location, ok := catalog["location"+":"+locationKey]
				if !ok || !worldcontent.TravelableLocationType(worldcontent.DefinitionLocationNodeType(location.Content)) {
					return fmt.Errorf("%w: recipe:%s 引用了未知或不可使用的地点 %s", ErrInvalidContent, definition.Key, locationKey)
				}
			}
			for _, products := range [][]craftingrules.Product{recipe.Outputs, recipe.FailureOutputs} {
				for _, product := range products {
					if err := worldcontent.RequireProductDefinition(catalog, product.Kind, product.Key, "recipe:"+definition.Key); err != nil {
						return err
					}
				}
			}
		case "encounter":
			encounter, _ := encounterrules.Parse(map[string]any(definition.Content))
			location, ok := catalog["location"+":"+encounter.LocationKey]
			if !ok || !worldcontent.TravelableLocationType(worldcontent.DefinitionLocationNodeType(location.Content)) {
				return fmt.Errorf("%w: encounter:%s 引用了未知或不可进入的 location_key", ErrInvalidContent, definition.Key)
			}
			for _, reward := range encounter.Rewards {
				if err := worldcontent.RequireStackableDefinition(catalog, reward.ItemKey, "encounter:"+definition.Key+" 的奖励"); err != nil {
					return err
				}
			}

		case "npc_archetype":
			realmKey, _ := definition.Content["realm_key"].(string)
			if realmKey != "" {
				if _, ok := catalog["realm"+":"+realmKey]; ok {
					// Valid optional progression reference.
				} else {
					return fmt.Errorf("%w: npc_archetype:%s 引用了未知 realm_key", ErrInvalidContent, definition.Key)
				}
			} else {
				return fmt.Errorf("%w: npc_archetype:%s 引用了未知 realm_key", ErrInvalidContent, definition.Key)
			}
			// stage_key 必须属于其 realm 声明的 stages：跨境界的 stage
			//（例如练气期 archetype 配筑基层的 early/middle/late）一律拒绝。
			if stageKey, _ := definition.Content["stage_key"].(string); stageKey != "" && realmKey != "" {
				realm := catalog["realm"+":"+realmKey]
				stages, ok := worldcontent.StringList(realm.Content["stages"])
				if !ok || !worldcontent.ContainsString(stages, stageKey) {
					return fmt.Errorf("%w: npc_archetype:%s 的 stage_key %s 不属于 realm:%s 的 stages", ErrInvalidContent, definition.Key, stageKey, realmKey)
				}
			}

		}
	}
	for pool, count := range startingTechniquePools {
		if count < 2 {
			return fmt.Errorf("starting_pool %s 至少需要两个候选功法", pool)
		}
	}
	if realmCount == 0 {
		return fmt.Errorf("campaign 模式至少需要一个 realm 定义")
	}
	return nil
}

func Shapes() map[string]gameplay.DefinitionValidator {
	out := worldcontent.Shapes()
	out[originrules.DefinitionKind] = func(d gameplay.Definition) error { _, err := originrules.Parse(d.Content); return err }
	out[progressrules.DefinitionKind] = func(d gameplay.Definition) error { _, err := progressrules.Parse(d.Content); return err }
	out[trainingrules.DefinitionKind] = func(d gameplay.Definition) error { _, err := trainingrules.Parse(d.Content); return err }
	out[ascensionrules.DefinitionKind] = func(d gameplay.Definition) error { _, err := ascensionrules.Parse(d.Content); return err }
	for _, kind := range []string{"realm", "technique", "item", "equipment", "recipe", "encounter"} {
		out[kind] = ValidateShape
	}
	return out
}
func Validate(content gameplay.Content) error {
	if err := originrules.Validate(content); err != nil {
		return err
	}
	if err := validateProgressRewards(content); err != nil {
		return err
	}
	if err := validateTraining(content); err != nil {
		return err
	}
	if err := validateAscension(content); err != nil {
		return err
	}
	if err := validateProgression(content.Runtime, content.Definitions); err != nil {
		return err
	}
	return worldcontent.Validate(content)
}

func ValidateReferences(runtime *gameplay.RuntimeContract, definitions []gameplay.Definition) error {
	if err := validateProgression(runtime, definitions); err != nil {
		return err
	}
	return worldcontent.ValidateReferences(runtime, definitions)
}
