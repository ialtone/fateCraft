package campaignactions

import (
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/campaigncontract"
	"github.com/ialtone/fatecraft/internal/storage"
)

func ActionKeys() []string {
	return campaigncontract.ActionKeys()
}

func actionArgumentSchema(key string) storage.JSONMap {
	schemas := map[string]storage.JSONMap{
		ActionRenameCharacter: objectSchema([]string{"name"}, map[string]any{
			"name": boundedStringSchema("角色新的公开姓名", 128),
		}),
		ActionPractice: objectSchema([]string{"technique_key"}, map[string]any{
			"technique_key": stringSchema("已装配且当前可修炼的功法 key"),
			"sessions":      integerSchema("修炼次数", 1, 24),
		}),
		ActionBreakthrough:   objectSchema(nil, nil),
		ActionLearnTechnique: oneKeySchema("technique_key", "当前可学习的功法 key"),
		ActionEquipTechnique: objectSchema([]string{"technique_key", "slot"}, map[string]any{
			"technique_key": stringSchema("已掌握的功法 key"), "slot": stringSchema("功法槽位 key"),
		}),
		ActionUnequipTechnique: oneKeySchema("technique_key", "当前已装配的功法 key"),
		ActionApplyCondition: objectSchema([]string{"category", "effect_key", "severity"}, map[string]any{
			"category":   enumStringSchema("状态类别", "injury", "poison", "inner_demon", "long_term"),
			"effect_key": stringSchema("状态稳定 key"), "severity": integerSchema("严重度", 1, 100),
			"duration_minutes": integerSchema("持续分钟数；0 表示不自动到期", 0, 5_256_000),
			"description":      boundedStringSchema("公开描述", 256),
		}),
		ActionResolveCondition: objectSchema([]string{"category", "effect_key"}, map[string]any{
			"category":   enumStringSchema("状态类别", "injury", "poison", "inner_demon", "long_term"),
			"effect_key": stringSchema("当前存在的状态 key"),
		}),
		ActionAdjustVitals: objectSchema([]string{"cause_key"}, map[string]any{
			"health_delta":          integerSchema("气血变化", -10_000, 10_000),
			"mana_delta":            integerSchema("法力变化", -10_000, 10_000),
			"spiritual_sense_delta": integerSchema("神识变化", -10_000, 10_000),
			"cause_key":             stringSchema("变化原因稳定 key"),
		}),
		ActionGainItem: objectSchema([]string{"item_key", "quantity"}, map[string]any{
			"item_key": stringSchema("允许模型授予的可堆叠物品 key"), "quality_key": stringSchema("品质 key"),
			"quantity": integerSchema("数量", 1, 1_000_000),
		}),
		ActionUseItem: objectSchema([]string{"item_key", "quality_key", "quantity"}, map[string]any{
			"item_key": stringSchema("当前持有且可使用的物品 key"), "quality_key": stringSchema("品质 key"),
			"quantity": integerSchema("使用数量", 1, 20),
		}),
		ActionGainEquipment: objectSchema([]string{"definition_key"}, map[string]any{
			"definition_key": stringSchema("允许模型授予的装备定义 key"), "quality_key": stringSchema("品质 key"),
			"source": stringSchema("来源稳定 key"),
		}),
		ActionEquipItem: objectSchema([]string{"item_instance_id", "slot"}, map[string]any{
			"item_instance_id": stringSchema("库存装备实例 UUID"), "slot": stringSchema("装备槽位 key"),
		}),
		ActionUnequipItem: objectSchema([]string{"item_instance_id"}, map[string]any{
			"item_instance_id": stringSchema("已装备实例 UUID"),
		}),
		ActionLearnRecipe: oneKeySchema("recipe_key", "当前可学习的配方 key"),
		ActionCraftRecipe: oneKeySchema("recipe_key", "当前可执行的已掌握配方 key"),
		ActionTradeBuy:    tradeSchema(false),
		ActionTradeSell:   tradeSchema(true),
		ActionAdjustMarket: objectSchema([]string{"market_key", "price_modifier_bps"}, map[string]any{
			"market_key": stringSchema("市场 key"), "price_modifier_bps": integerSchema("价格基点系数", 1000, 30000),
		}),
		ActionTravel:      oneKeySchema("destination_key", "已发现、相邻且当前可进入的地点 key"),
		ActionInvestigate: objectSchema(nil, nil),
		ActionResolveUncertain: objectSchema([]string{"kind", "description"}, map[string]any{
			"kind":        enumStringSchema("受限不确定行动类别", "social", "insight", "stealth", "exploration", "escape"),
			"description": boundedStringSchema("面向玩家的具体行动描述；不能包含预设结果", 160),
		}),
		ActionGather: objectSchema(nil, map[string]any{"item_key": stringSchema("当前地点资源表中的物品 key")}),
		ActionInstantiateNPC: objectSchema([]string{"archetype_key", "instance_key"}, map[string]any{
			"archetype_key": stringSchema("当前地点可实例化的 NPC 原型 key"),
			"instance_key":  stringSchema("新 NPC 的唯一稳定 key"), "stable_name": boundedStringSchema("公开稳定名称", 128),
		}),
		ActionMoveNPC: objectSchema([]string{"npc_key", "destination_key"}, map[string]any{
			"npc_key": stringSchema("当前地点活动 NPC key"), "destination_key": stringSchema("相邻地点 key"),
		}),
		ActionSetNPCStatus: objectSchema([]string{"npc_key", "status"}, map[string]any{
			"npc_key": stringSchema("当前地点 NPC key"),
			"status":  enumStringSchema("目标状态", "active", "missing", "dead", "departed", "sealed"),
		}),
		ActionRecordInteraction: objectSchema([]string{"npc_key"}, map[string]any{
			"npc_key":     stringSchema("当前地点活动 NPC key"),
			"trust_delta": integerSchema("信任变化", -20, 20), "fear_delta": integerSchema("畏惧变化", -20, 20),
			"affinity_delta": integerSchema("亲近变化", -20, 20), "hostility_delta": integerSchema("敌意变化", -20, 20),
			"debt_delta": integerSchema("债务变化", -1000, 1000), "public_stance": boundedStringSchema("公开态度", 32),
			"known_fact_key": stringSchema("已知事实稳定 key"), "known_fact": boundedStringSchema("已知事实内容", 512),
		}),
		ActionAdjustFaction: objectSchema([]string{"faction_key"}, map[string]any{
			"faction_key": stringSchema("势力 key"), "reputation_delta": integerSchema("声望变化", -500, 500),
			"contribution_delta": integerSchema("贡献变化", -1000, 1000), "wanted_delta": integerSchema("通缉变化", -1, 1),
		}),
		ActionAcceptQuest:    oneKeySchema("quest_key", "当前可接受的任务 key"),
		ActionSubmitQuest:    oneKeySchema("quest_key", "当前可提交的任务 key"),
		ActionStartEncounter: oneKeySchema("encounter_key", "当前位置可开始的遭遇 key"),
		ActionEncounterAct: objectSchema([]string{"encounter_id", "move"}, map[string]any{
			"encounter_id": stringSchema("活动遭遇 UUID"), "move": enumStringSchema("本回合行动", "attack", "defend"),
			"technique_key": stringSchema("攻击时可选的已装配功法 key"),
		}),
		ActionEncounterFlee: objectSchema([]string{"encounter_id"}, map[string]any{
			"encounter_id": stringSchema("活动遭遇 UUID"),
		}),
	}
	return schemas[key]
}

func objectSchema(required []string, properties map[string]any) storage.JSONMap {
	if properties == nil {
		properties = map[string]any{}
	}
	if required == nil {
		required = []string{}
	}
	return storage.JSONMap{
		"type": "object", "required": required, "properties": properties, "additionalProperties": false,
	}
}

func oneKeySchema(key, description string) storage.JSONMap {
	return objectSchema([]string{key}, map[string]any{key: stringSchema(description)})
}

func tradeSchema(selling bool) storage.JSONMap {
	properties := map[string]any{
		"market_key": stringSchema("当前位置市场 key"), "item_kind": enumStringSchema("商品种类", "item", "equipment"),
		"item_key": stringSchema("商品定义 key"), "quality_key": stringSchema("品质 key"),
		"quantity": integerSchema("数量", 1, 1000),
	}
	if selling {
		properties["item_instance_id"] = stringSchema("出售装备时的实例 UUID")
	}
	return objectSchema([]string{"market_key", "item_kind", "item_key", "quality_key", "quantity"}, properties)
}

func stringSchema(description string) map[string]any {
	return map[string]any{"type": "string", "minLength": 1, "maxLength": 160, "description": description}
}

func boundedStringSchema(description string, maximum int64) map[string]any {
	return map[string]any{"type": "string", "minLength": 1, "maxLength": maximum, "description": description}
}

func enumStringSchema(description string, values ...string) map[string]any {
	enum := make([]any, 0, len(values))
	for _, value := range values {
		enum = append(enum, value)
	}
	return map[string]any{"type": "string", "enum": enum, "description": description}
}

func integerSchema(description string, minimum, maximum int64) map[string]any {
	return map[string]any{"type": "integer", "minimum": minimum, "maximum": maximum, "description": description}
}
