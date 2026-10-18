package campaignactions

import (
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldactions"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/campaigncontract"
	"github.com/ialtone/fatecraft/internal/storage"
)

func ActionKeys() []string {
	return campaigncontract.ActionKeys()
}

func actionArgumentSchema(key string) storage.JSONMap {
	schemas := map[string]storage.JSONMap{
		ActionAbandon:        worldactions.ObjectSchema(nil, nil),
		ActionRetreat:        worldactions.OneKeySchema("plan_key", "已公开的闭关方案 key"),
		ActionRecover:        worldactions.OneKeySchema("plan_key", "已公开的休整方案 key"),
		ActionBeginAscension: worldactions.ObjectSchema(nil, nil),
		ActionRenameCharacter: worldactions.ObjectSchema([]string{"name"}, map[string]any{
			"name": worldactions.BoundedStringSchema("角色新的公开姓名", 128),
		}),
		ActionPractice: worldactions.ObjectSchema([]string{"technique_key"}, map[string]any{
			"technique_key": worldactions.StringSchema("已装配且当前可修炼的功法 key"),
			"sessions":      worldactions.IntegerSchema("修炼次数", 1, 24),
		}),
		ActionBreakthrough:   worldactions.ObjectSchema(nil, nil),
		ActionLearnTechnique: worldactions.OneKeySchema("technique_key", "当前可学习的功法 key"),
		ActionEquipTechnique: worldactions.ObjectSchema([]string{"technique_key"}, map[string]any{
			"technique_key": worldactions.StringSchema("已掌握的功法 key"), "slot": worldactions.StringSchema("功法槽位 key"),
		}),
		ActionUnequipTechnique: worldactions.OneKeySchema("technique_key", "当前已装配的功法 key"),
		ActionApplyCondition: worldactions.ObjectSchema([]string{"category", "effect_key", "severity"}, map[string]any{
			"category":   worldactions.EnumStringSchema("状态类别", "injury", "poison", "inner_demon", "long_term"),
			"effect_key": worldactions.StringSchema("状态稳定 key"), "severity": worldactions.IntegerSchema("严重度", 1, 100),
			"duration_minutes": worldactions.IntegerSchema("持续分钟数；0 表示不自动到期", 0, 5_256_000),
			"description":      worldactions.BoundedStringSchema("公开描述", 256),
		}),
		ActionResolveCondition: worldactions.ObjectSchema([]string{"category", "effect_key"}, map[string]any{
			"category":   worldactions.EnumStringSchema("状态类别", "injury", "poison", "inner_demon", "long_term"),
			"effect_key": worldactions.StringSchema("当前存在的状态 key"),
		}),
		ActionAdjustVitals: worldactions.ObjectSchema([]string{"cause_key"}, map[string]any{
			"health_delta":          worldactions.IntegerSchema("气血变化", -10_000, 10_000),
			"mana_delta":            worldactions.IntegerSchema("法力变化", -10_000, 10_000),
			"spiritual_sense_delta": worldactions.IntegerSchema("神识变化", -10_000, 10_000),
			"cause_key":             worldactions.StringSchema("变化原因稳定 key"),
		}),
		ActionUseItem: worldactions.ObjectSchema([]string{"item_key", "quality_key", "quantity"}, map[string]any{
			"item_key": worldactions.StringSchema("当前持有且可使用的物品 key"), "quality_key": worldactions.StringSchema("品质 key"),
			"quantity": worldactions.IntegerSchema("使用数量", 1, 20),
		}),
		ActionGainEquipment: worldactions.ObjectSchema([]string{"definition_key"}, map[string]any{
			"definition_key": worldactions.StringSchema("允许模型授予的装备定义 key"), "quality_key": worldactions.StringSchema("品质 key"),
			"source": worldactions.StringSchema("来源稳定 key"),
		}),
		ActionEquipItem: worldactions.ObjectSchema([]string{"item_instance_id", "slot"}, map[string]any{
			"item_instance_id": worldactions.StringSchema("库存装备实例 UUID"), "slot": worldactions.StringSchema("装备槽位 key"),
		}),
		ActionUnequipItem: worldactions.ObjectSchema([]string{"item_instance_id"}, map[string]any{
			"item_instance_id": worldactions.StringSchema("已装备实例 UUID"),
		}),
		ActionLearnRecipe:    worldactions.OneKeySchema("recipe_key", "当前可学习的配方 key"),
		ActionCraftRecipe:    worldactions.OneKeySchema("recipe_key", "当前可执行的已掌握配方 key"),
		ActionGather:         worldactions.ObjectSchema(nil, map[string]any{"item_key": worldactions.StringSchema("当前地点资源表中的物品 key")}),
		ActionStartEncounter: worldactions.OneKeySchema("encounter_key", "当前位置可开始的遭遇 key"),
		ActionEncounterAct: worldactions.ObjectSchema([]string{"encounter_id", "move"}, map[string]any{
			"encounter_id": worldactions.StringSchema("活动遭遇 UUID"), "move": worldactions.EnumStringSchema("本回合行动", "attack", "defend"),
			"technique_key": worldactions.StringSchema("攻击时可选的已装配功法 key"),
		}),
		ActionEncounterFlee: worldactions.ObjectSchema([]string{"encounter_id"}, map[string]any{
			"encounter_id": worldactions.StringSchema("活动遭遇 UUID"),
		}),
	}
	if schema, ok := schemas[key]; ok {
		return schema
	}
	return worldactions.ArgumentSchema(key)
}
