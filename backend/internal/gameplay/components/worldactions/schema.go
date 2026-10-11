package worldactions

import (
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldcontract"
	"github.com/ialtone/fatecraft/internal/storage"
)

func ArgumentSchema(key string) storage.JSONMap {
	schemas := map[string]storage.JSONMap{
		worldcontract.ActionGainItem: ObjectSchema([]string{"item_key", "quantity"}, map[string]any{
			"item_key": StringSchema("允许模型授予的可堆叠物品 key"), "quality_key": StringSchema("品质 key"),
			"quantity": IntegerSchema("数量", 1, 1_000_000),
		}),
		worldcontract.ActionTradeBuy:  TradeSchema(false),
		worldcontract.ActionTradeSell: TradeSchema(true),
		worldcontract.ActionAdjustMarket: ObjectSchema([]string{"market_key", "price_modifier_bps"}, map[string]any{
			"market_key": StringSchema("市场 key"), "price_modifier_bps": IntegerSchema("价格基点系数", 1000, 30000),
		}),
		worldcontract.ActionTravel:      OneKeySchema("destination_key", "已发现、相邻且当前可进入的地点 key"),
		worldcontract.ActionInvestigate: ObjectSchema(nil, nil),
		worldcontract.ActionResolveUncertain: ObjectSchema([]string{"kind", "description"}, map[string]any{
			"kind":        EnumStringSchema("受限不确定行动类别", "social", "insight", "stealth", "exploration", "escape"),
			"description": BoundedStringSchema("面向玩家的具体行动描述；不能包含预设结果", 160),
		}),
		worldcontract.ActionInstantiateNPC: ObjectSchema([]string{"archetype_key", "instance_key"}, map[string]any{
			"archetype_key": StringSchema("当前地点可实例化的 NPC 原型 key"),
			"instance_key":  StringSchema("新 NPC 的唯一稳定 key"), "stable_name": BoundedStringSchema("公开稳定名称", 128),
		}),
		worldcontract.ActionMoveNPC: ObjectSchema([]string{"npc_key", "destination_key"}, map[string]any{
			"npc_key": StringSchema("当前地点活动 NPC key"), "destination_key": StringSchema("相邻地点 key"),
		}),
		worldcontract.ActionSetNPCStatus: ObjectSchema([]string{"npc_key", "status"}, map[string]any{
			"npc_key": StringSchema("当前地点 NPC key"),
			"status":  EnumStringSchema("目标状态", "active", "missing", "dead", "departed", "sealed"),
		}),
		worldcontract.ActionRecordInteraction: ObjectSchema([]string{"npc_key"}, map[string]any{
			"npc_key":     StringSchema("当前地点活动 NPC key"),
			"trust_delta": IntegerSchema("信任变化", -20, 20), "fear_delta": IntegerSchema("畏惧变化", -20, 20),
			"affinity_delta": IntegerSchema("亲近变化", -20, 20), "hostility_delta": IntegerSchema("敌意变化", -20, 20),
			"debt_delta": IntegerSchema("债务变化", -1000, 1000), "public_stance": BoundedStringSchema("公开态度", 32),
			"known_fact_key": StringSchema("已知事实稳定 key"), "known_fact": BoundedStringSchema("已知事实内容", 512),
		}),
		worldcontract.ActionAdjustFaction: ObjectSchema([]string{"faction_key"}, map[string]any{
			"faction_key": StringSchema("势力 key"), "reputation_delta": IntegerSchema("声望变化", -500, 500),
			"contribution_delta": IntegerSchema("贡献变化", -1000, 1000), "wanted_delta": IntegerSchema("通缉变化", -1, 1),
		}),
		worldcontract.ActionAcceptQuest: OneKeySchema("quest_key", "当前可接受的任务 key"),
		worldcontract.ActionSubmitQuest: OneKeySchema("quest_key", "当前可提交的任务 key"),
	}
	return schemas[key]
}
func ObjectSchema(required []string, properties map[string]any) storage.JSONMap {
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

func OneKeySchema(key, description string) storage.JSONMap {
	return ObjectSchema([]string{key}, map[string]any{key: StringSchema(description)})
}

func TradeSchema(selling bool) storage.JSONMap {
	properties := map[string]any{
		"market_key": StringSchema("当前位置市场 key"), "item_kind": EnumStringSchema("商品种类", "item", "equipment"),
		"item_key": StringSchema("商品定义 key"), "quality_key": StringSchema("品质 key"),
		"quantity": IntegerSchema("数量", 1, 1000),
	}
	if selling {
		properties["item_instance_id"] = StringSchema("出售装备时的实例 UUID")
	}
	return ObjectSchema([]string{"market_key", "item_kind", "item_key", "quality_key", "quantity"}, properties)
}

func StringSchema(description string) map[string]any {
	return map[string]any{"type": "string", "minLength": 1, "maxLength": 160, "description": description}
}

func BoundedStringSchema(description string, maximum int64) map[string]any {
	return map[string]any{"type": "string", "minLength": 1, "maxLength": maximum, "description": description}
}

func EnumStringSchema(description string, values ...string) map[string]any {
	enum := make([]any, 0, len(values))
	for _, value := range values {
		enum = append(enum, value)
	}
	return map[string]any{"type": "string", "enum": enum, "description": description}
}

func IntegerSchema(description string, minimum, maximum int64) map[string]any {
	return map[string]any{"type": "integer", "minimum": minimum, "maximum": maximum, "description": description}
}
