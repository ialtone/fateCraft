package worldcontent

import (
	"errors"
	"fmt"
	"github.com/ialtone/fatecraft/internal/gameplay/components/factionrules"
	"github.com/ialtone/fatecraft/internal/gameplay/components/gatheringrules"
	"github.com/ialtone/fatecraft/internal/gameplay/components/marketrules"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldrules"
	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"strings"
)

var ErrInvalidContent = errors.New("玩法定义无效")

func ValidateShape(definition gameplay.Definition) error {
	content := definition.Content
	switch definition.Kind {
	case "market":
		market, ok := marketrules.Parse(map[string]any(content))
		if !ok {
			return fmt.Errorf("市场结构、库存、补货或价格边界无效")
		}
		// 最坏情况（声望 +10000、满库存买入、零库存卖出）下卖价必须不高于买价，
		// 否则市场可被零时间无限套利。见 marketrules.NoArbitrage。
		if !marketrules.NoArbitrage(market) {
			return fmt.Errorf("市场存在套利空间：最坏情况下卖价不低于买价")
		}
	case "location":
		nodeType, ok := content["node_type"].(string)
		if !map[string]bool{"world": true, "realm": true, "region": true, "location": true, "scene": true}[nodeType] {
			return fmt.Errorf("node_type 必须是 world、realm、region、location 或 scene")
		}
		if parentKey, _ := content["parent_key"].(string); parentKey != "" {
			if !StableDefinitionKey(parentKey) || parentKey == definition.Key {
				return fmt.Errorf("parent_key 必须是不同于自身的稳定 key")
			}
		}
		connections, ok := content["connections"].([]any)
		if !ok {
			return fmt.Errorf("connections 必须是数组")
		}
		seen := map[string]bool{}
		for _, raw := range connections {
			connection, ok := raw.(map[string]any)
			key, _ := connection["location_key"].(string)
			if !ok || !StableDefinitionKey(key) || seen[key] {
				return fmt.Errorf("connections 包含无效或重复 location_key")
			}
			if _, ok := PositiveDefinitionInt(connection["travel_minutes"]); !ok {
				return fmt.Errorf("connection.travel_minutes 必须是正整数")
			}
			seen[key] = true
		}
		if (nodeType == "world" || nodeType == "realm" || nodeType == "region") && len(connections) > 0 {
			return fmt.Errorf("world、realm 与 region 容器节点不能声明旅行连接")
		}
		if raw, exists := content["entry_requirements"]; exists {
			if _, ok := factionrules.EntryRequirements(map[string]any{"entry_requirements": raw}); !ok {
				return fmt.Errorf("entry_requirements 必须由不重复的势力权限或 min_realm_rank 组成，且最多 %d 项", factionrules.MaxEntryRequirements)
			}
		}
		if raw, exists := content["initial_state"]; exists {
			if nodeType != "region" {
				return fmt.Errorf("initial_state 只允许用于 region 节点")
			}
			if _, ok := worldrules.ParseRegionInitialState(raw); !ok {
				return fmt.Errorf("initial_state 的区域数值或 faction_power 无效")
			}
		}
		if raw, exists := content["gathering"]; exists {
			if nodeType == "realm" || nodeType == "region" {
				return fmt.Errorf("gathering 只允许用于可进入的 location 或 scene")
			}
			if _, ok := gatheringrules.Parse(raw); !ok {
				return fmt.Errorf("gathering 的时间、资源条目、权重或数量无效")
			}
		}
	case "faction":
		if raw, exists := content["initial_reputation"]; exists {
			reputation, ok := SignedDefinitionInt(raw)
			if !ok || reputation < factionrules.MinimumReputation || reputation > factionrules.MaximumReputation {
				return fmt.Errorf("initial_reputation 必须是 %d 到 %d 的整数", factionrules.MinimumReputation, factionrules.MaximumReputation)
			}
		}
		if raw, exists := content["permission_thresholds"]; exists {
			thresholds, ok := DefinitionObject(raw)
			if !ok || len(thresholds) > 32 {
				return fmt.Errorf("permission_thresholds 必须是最多 32 项的对象")
			}
			for permissionKey, rawThreshold := range thresholds {
				threshold, valid := SignedDefinitionInt(rawThreshold)
				if !StableDefinitionKey(permissionKey) || !valid ||
					threshold < factionrules.MinimumReputation || threshold > factionrules.MaximumReputation {
					return fmt.Errorf("permission_thresholds 的 key 必须稳定，阈值必须在 %d 到 %d 之间", factionrules.MinimumReputation, factionrules.MaximumReputation)
				}
			}
		}
		if raw, exists := content["position_thresholds"]; exists {
			thresholds, ok := DefinitionObject(raw)
			if !ok || len(thresholds) > 32 {
				return fmt.Errorf("position_thresholds 必须是最多 32 项的对象")
			}
			for positionKey, rawThreshold := range thresholds {
				threshold, valid := NonNegativeDefinitionInt(rawThreshold)
				if !StableDefinitionKey(positionKey) || !valid || threshold > factionrules.MaximumContribution {
					return fmt.Errorf("position_thresholds 的 key 必须稳定，阈值必须在 0 到 %d 之间", factionrules.MaximumContribution)
				}
			}
		}
	case "npc_archetype":
		initial, _ := content["initial"].(bool)
		spawnable, _ := content["spawnable"].(bool)
		if raw, exists := content["initial"]; exists {
			if _, ok := raw.(bool); !ok {
				return fmt.Errorf("initial 必须是布尔值")
			}
		}
		if raw, exists := content["spawnable"]; exists {
			if _, ok := raw.(bool); !ok {
				return fmt.Errorf("spawnable 必须是布尔值")
			}
		}
		if initial && spawnable {
			return fmt.Errorf("initial 与 spawnable 不能同时为 true")
		}
		if initial {
			for _, key := range []string{"instance_key", "location_key"} {
				value, _ := content[key].(string)
				if !StableDefinitionKey(value) {
					return fmt.Errorf("初始 NPC 的 %s 必须是稳定 key", key)
				}
			}
		} else if value, _ := content["instance_key"].(string); value != "" && !StableDefinitionKey(value) {
			return fmt.Errorf("instance_key 必须是稳定 key")
		}
		if realmKey, _ := content["realm_key"].(string); realmKey != "" && !StableDefinitionKey(realmKey) {
			return fmt.Errorf("realm_key 必须为空或稳定 key")
		}
		for _, key := range []string{"stage_key", "faction_key"} {
			if value, _ := content[key].(string); value != "" && !StableDefinitionKey(value) {
				return fmt.Errorf("%s 必须是稳定 key", key)
			}
		}
		for _, key := range []string{"public_profile", "private_state", "goals"} {
			if raw, exists := content[key]; exists {
				if _, ok := raw.(map[string]any); !ok {
					return fmt.Errorf("%s 必须是对象", key)
				}
			}
		}
		if spawnable {
			locations, ok := StringList(content["allowed_locations"])
			if !ok || len(locations) == 0 {
				return fmt.Errorf("可实例化 NPC 的 allowed_locations 必须是非空稳定 key 数组")
			}
			if maximum, ok := PositiveDefinitionInt(content["max_instances"]); !ok || maximum > 100 {
				return fmt.Errorf("可实例化 NPC 的 max_instances 必须是 1 到 100 的整数")
			}
		}
	case "world_event":
		if _, ok := worldrules.ParseWorldEvent(map[string]any(content)); !ok {
			return fmt.Errorf("世界事件结构、调度、变体权重或效果边界无效")
		}
	case "quest":
		if _, ok := worldrules.ParseQuest(map[string]any(content)); !ok {
			return fmt.Errorf("任务类型、状态、截止时间、目标或奖励结构无效")
		}
	case "item":
		if _, ok := content["stackable"].(bool); !ok {
			return fmt.Errorf("stackable 必须是布尔值")
		}
		for _, key := range []string{"model_grantable", "consumable"} {
			if raw, exists := content[key]; exists {
				if _, ok := raw.(bool); !ok {
					return fmt.Errorf("%s 必须是布尔值", key)
				}
			}
		}
		if _, exists := content["effects"]; exists {
			return fmt.Errorf("当前模块未声明物品消耗效果")
		}
	default:
		return fmt.Errorf("未声明定义类型 %s", definition.Kind)
	}
	return nil
}
func ValidateReferences(contract *gameplay.RuntimeContract, definitions []gameplay.Definition) error {
	catalog := map[string]gameplay.Definition{}
	for _, definition := range definitions {
		catalog[definition.Kind+":"+definition.Key] = definition
	}
	if _, ok := catalog["location"+":"+contract.Campaign.InitialLocationKey]; !ok {
		return fmt.Errorf("%w: campaign.initial_location_key 未在 location 定义中声明", ErrInvalidContent)
	}
	if initial := catalog["location"+":"+contract.Campaign.InitialLocationKey]; !TravelableLocationType(DefinitionLocationNodeType(initial.Content)) {
		return fmt.Errorf("%w: campaign.initial_location_key 必须指向 location 或 scene", ErrInvalidContent)
	}
	if len(definitions) == 0 {
		return fmt.Errorf("%w: campaign 模式必须提供玩法定义", ErrInvalidContent)
	}
	initialNPCKeys := map[string]string{}
	initialNPCDefinitions := map[string]gameplay.Definition{}
	quests := map[string]worldrules.Quest{}
	questUnlockSources := map[string][]string{}
	for _, definition := range definitions {
		if definition.Kind == "npc_archetype" {
			if initial, _ := definition.Content["initial"].(bool); initial {
				if key, _ := definition.Content["instance_key"].(string); key != "" {
					initialNPCDefinitions[key] = definition
				}
			}
		}
		if definition.Kind == "quest" {
			if parsed, ok := worldrules.ParseQuest(map[string]any(definition.Content)); ok {
				quests[definition.Key] = parsed
			}
		}
	}
	for _, definition := range definitions {
		switch definition.Kind {
		case "market":
			market, _ := marketrules.Parse(map[string]any(definition.Content))
			location, ok := catalog["location"+":"+market.LocationKey]
			if !ok || !TravelableLocationType(DefinitionLocationNodeType(location.Content)) {
				return fmt.Errorf("%w: market:%s 引用了未知或不可进入的 location_key", ErrInvalidContent, definition.Key)
			}
			if err := RequireStackableDefinition(catalog, market.CurrencyItemKey, "market:"+definition.Key+" 的 currency_item_key"); err != nil {
				return err
			}
			if market.FactionKey != "" {
				if _, ok := catalog["faction"+":"+market.FactionKey]; !ok {
					return fmt.Errorf("%w: market:%s 引用了未知 faction_key", ErrInvalidContent, definition.Key)
				}
			}
			for _, entry := range market.Entries {
				if err := RequireProductDefinition(catalog, entry.Kind, entry.Key, "market:"+definition.Key); err != nil {
					return err
				}
			}
		case "location":
			if parentKey, _ := definition.Content["parent_key"].(string); parentKey != "" {
				if _, ok := catalog["location"+":"+parentKey]; !ok {
					return fmt.Errorf("%w: location:%s 引用了未知 parent_key %s", ErrInvalidContent, definition.Key, parentKey)
				}
			}
			connections, _ := definition.Content["connections"].([]any)
			for _, raw := range connections {
				connection := raw.(map[string]any)
				key := connection["location_key"].(string)
				target, ok := catalog["location"+":"+key]
				if !ok {
					return fmt.Errorf("%w: location:%s 引用了未知地点 %s", ErrInvalidContent, definition.Key, key)
				}
				if !TravelableLocationType(DefinitionLocationNodeType(target.Content)) {
					return fmt.Errorf("%w: location:%s 的连接指向不可旅行的容器节点 %s", ErrInvalidContent, definition.Key, key)
				}
			}
			requirements, _ := definition.Content["entry_requirements"].([]any)
			for _, raw := range requirements {
				requirement, _ := DefinitionObject(raw)
				if _, progression := requirement["min_realm_rank"]; progression {
					continue
				}
				factionKey, _ := requirement["faction_key"].(string)
				permissionKey, _ := requirement["permission_key"].(string)
				faction, ok := catalog["faction"+":"+factionKey]
				if !ok {
					return fmt.Errorf("%w: location:%s 引用了未知 faction_key %s", ErrInvalidContent, definition.Key, factionKey)
				}
				thresholds, _ := DefinitionObject(faction.Content["permission_thresholds"])
				if _, ok := thresholds[permissionKey]; !ok {
					return fmt.Errorf("%w: location:%s 引用了 faction:%s 未声明的 permission_key %s", ErrInvalidContent, definition.Key, factionKey, permissionKey)
				}
			}
			if nodeType := DefinitionLocationNodeType(definition.Content); nodeType == "region" {
				initial, _ := worldrules.ParseRegionInitialState(definition.Content["initial_state"])
				for factionKey := range initial.FactionPower {
					if _, ok := catalog["faction"+":"+factionKey]; !ok {
						return fmt.Errorf("%w: location:%s 的 initial_state 引用了未知 faction %s", ErrInvalidContent, definition.Key, factionKey)
					}
				}
			}
			if raw, exists := definition.Content["gathering"]; exists {
				table, _ := gatheringrules.Parse(raw)
				for _, entry := range table.Entries {
					if err := RequireStackableDefinition(catalog, entry.ItemKey, "location:"+definition.Key+" 的 gathering 资源"); err != nil {
						return err
					}
				}
			}
		case "npc_archetype":
			if factionKey, _ := definition.Content["faction_key"].(string); factionKey != "" {
				if _, ok := catalog["faction"+":"+factionKey]; !ok {
					return fmt.Errorf("%w: npc_archetype:%s 引用了未知 faction_key", ErrInvalidContent, definition.Key)
				}
			}
			if initial, _ := definition.Content["initial"].(bool); initial {
				locationKey, _ := definition.Content["location_key"].(string)
				if _, ok := catalog["location"+":"+locationKey]; !ok {
					return fmt.Errorf("%w: npc_archetype:%s 引用了未知 location_key", ErrInvalidContent, definition.Key)
				}
				instanceKey, _ := definition.Content["instance_key"].(string)
				if previous, exists := initialNPCKeys[instanceKey]; exists {
					return fmt.Errorf("%w: npc_archetype:%s 与 npc_archetype:%s 使用了相同 instance_key %s", ErrInvalidContent, definition.Key, previous, instanceKey)
				}
				initialNPCKeys[instanceKey] = definition.Key
			}
			if spawnable, _ := definition.Content["spawnable"].(bool); spawnable {
				locations, _ := StringList(definition.Content["allowed_locations"])
				for _, locationKey := range locations {
					if _, ok := catalog["location"+":"+locationKey]; !ok {
						return fmt.Errorf("%w: npc_archetype:%s 引用了未知 allowed_location %s", ErrInvalidContent, definition.Key, locationKey)
					}
				}
			}
		case "world_event":
			event, _ := worldrules.ParseWorldEvent(map[string]any(definition.Content))
			region, ok := catalog["location"+":"+event.RegionKey]
			if !ok || DefinitionLocationNodeType(region.Content) != "region" {
				return fmt.Errorf("%w: world_event:%s 的 region_key 必须引用 region 节点", ErrInvalidContent, definition.Key)
			}
			for _, variant := range event.Variants {
				for factionKey := range variant.Effects.FactionPowerDeltas {
					if _, ok := catalog["faction"+":"+factionKey]; !ok {
						return fmt.Errorf("%w: world_event:%s 引用了未知 faction %s", ErrInvalidContent, definition.Key, factionKey)
					}
				}
				for marketKey := range variant.Effects.MarketPriceModifierDeltasBPS {
					if _, ok := catalog["market"+":"+marketKey]; !ok {
						return fmt.Errorf("%w: world_event:%s 引用了未知 market %s", ErrInvalidContent, definition.Key, marketKey)
					}
				}
				for npcKey := range variant.Effects.NPCGoalDeltas {
					if _, ok := initialNPCDefinitions[npcKey]; !ok {
						return fmt.Errorf("%w: world_event:%s 引用了未知初始 NPC 实例 %s", ErrInvalidContent, definition.Key, npcKey)
					}
				}
			}
		case "quest":
			quest, _ := worldrules.ParseQuest(map[string]any(definition.Content))
			for _, key := range quest.UnlockQuestKeys {
				target, ok := quests[key]
				if !ok {
					return fmt.Errorf("%w: quest:%s 解锁了未知 quest %s", ErrInvalidContent, definition.Key, key)
				}
				if key == definition.Key || target.Initial {
					return fmt.Errorf("%w: quest:%s 只能解锁其他非初始任务，当前为 %s", ErrInvalidContent, definition.Key, key)
				}
				questUnlockSources[key] = append(questUnlockSources[key], definition.Key)
			}
			for _, locationKey := range quest.AcceptLocationKeys {
				location, ok := catalog["location"+":"+locationKey]
				if !ok || !TravelableLocationType(DefinitionLocationNodeType(location.Content)) {
					return fmt.Errorf("%w: quest:%s 引用了未知或不可进入的接受地点 %s", ErrInvalidContent, definition.Key, locationKey)
				}
			}
			for _, objective := range quest.Objectives {
				switch objective.Type {
				case "item":
					if err := RequireStackableDefinition(catalog, objective.ItemKey, "quest:"+definition.Key+" 的目标"); err != nil {
						return err
					}
				case "encounter_win":
					if _, ok := catalog["encounter"+":"+objective.EncounterKey]; !ok {
						return fmt.Errorf("%w: quest:%s 引用了未知 encounter %s", ErrInvalidContent, definition.Key, objective.EncounterKey)
					}
				case "location":
					if _, ok := catalog["location"+":"+objective.LocationKey]; !ok {
						return fmt.Errorf("%w: quest:%s 引用了未知 location %s", ErrInvalidContent, definition.Key, objective.LocationKey)
					}
				case "faction_reputation":
					if _, ok := catalog["faction"+":"+objective.FactionKey]; !ok {
						return fmt.Errorf("%w: quest:%s 引用了未知 faction %s", ErrInvalidContent, definition.Key, objective.FactionKey)
					}
				case "npc_relationship":
					if _, ok := initialNPCDefinitions[objective.NPCKey]; !ok {
						return fmt.Errorf("%w: quest:%s 引用了未知初始 NPC 实例 %s", ErrInvalidContent, definition.Key, objective.NPCKey)
					}
				case "region_state":
					region, ok := catalog["location"+":"+objective.RegionKey]
					if !ok || DefinitionLocationNodeType(region.Content) != "region" {
						return fmt.Errorf("%w: quest:%s 引用了未知 region %s", ErrInvalidContent, definition.Key, objective.RegionKey)
					}
				}
			}
			for _, reward := range quest.Rewards {
				if reward.Type == "item" {
					if err := RequireStackableDefinition(catalog, reward.ItemKey, "quest:"+definition.Key+" 的奖励"); err != nil {
						return err
					}
				} else if _, ok := catalog["faction"+":"+reward.FactionKey]; !ok {
					return fmt.Errorf("%w: quest:%s 的奖励引用了未知 faction %s", ErrInvalidContent, definition.Key, reward.FactionKey)
				}
			}
			for _, key := range quest.Associations.RegionKeys {
				region, ok := catalog["location"+":"+key]
				if !ok || DefinitionLocationNodeType(region.Content) != "region" {
					return fmt.Errorf("%w: quest:%s 的 associations 引用了未知 region %s", ErrInvalidContent, definition.Key, key)
				}
			}
			for _, key := range quest.Associations.FactionKeys {
				if _, ok := catalog["faction"+":"+key]; !ok {
					return fmt.Errorf("%w: quest:%s 的 associations 引用了未知 faction %s", ErrInvalidContent, definition.Key, key)
				}
			}
			for _, key := range quest.Associations.NPCKeys {
				if _, ok := initialNPCDefinitions[key]; !ok {
					return fmt.Errorf("%w: quest:%s 的 associations 引用了未知初始 NPC %s", ErrInvalidContent, definition.Key, key)
				}
			}
			for _, key := range quest.Associations.WorldEventKeys {
				if _, ok := catalog["world_event"+":"+key]; !ok {
					return fmt.Errorf("%w: quest:%s 的 associations 引用了未知 world_event %s", ErrInvalidContent, definition.Key, key)
				}
			}
		}
	}
	for key, quest := range quests {
		if !quest.Initial && len(questUnlockSources[key]) == 0 {
			return fmt.Errorf("%w: 非初始 quest:%s 没有任何前置任务可以解锁它", ErrInvalidContent, key)
		}
	}
	if err := ValidateQuestUnlockGraph(quests); err != nil {
		return err
	}
	if err := ValidateLocationHierarchy(catalog, definitions); err != nil {
		return err
	}
	return nil
}

func ValidateQuestUnlockGraph(quests map[string]worldrules.Quest) error {
	const (
		visiting = iota + 1
		visited
	)
	states := make(map[string]int, len(quests))
	var visit func(string) error
	visit = func(key string) error {
		switch states[key] {
		case visiting:
			return fmt.Errorf("%w: 任务解锁关系存在循环，涉及 quest:%s", ErrInvalidContent, key)
		case visited:
			return nil
		}
		states[key] = visiting
		for _, target := range quests[key].UnlockQuestKeys {
			if err := visit(target); err != nil {
				return err
			}
		}
		states[key] = visited
		return nil
	}
	for key := range quests {
		if err := visit(key); err != nil {
			return err
		}
	}
	return nil
}

func RequireStackableDefinition(catalog map[string]gameplay.Definition, itemKey, reference string) error {
	item, ok := catalog["item"+":"+itemKey]
	stackable, _ := item.Content["stackable"].(bool)
	if !ok || !stackable {
		return fmt.Errorf("%w: %s 必须引用可堆叠 item，当前为 %s", ErrInvalidContent, reference, itemKey)
	}
	return nil
}

func RequireProductDefinition(catalog map[string]gameplay.Definition, kind, key, reference string) error {
	switch kind {
	case "item":
		return RequireStackableDefinition(catalog, key, reference+" 的产物")
	case "equipment":
		if _, ok := catalog["equipment"+":"+key]; !ok {
			return fmt.Errorf("%w: %s 引用了未知 equipment %s", ErrInvalidContent, reference, key)
		}
		return nil
	default:
		return fmt.Errorf("%w: %s 的产物 kind 无效", ErrInvalidContent, reference)
	}
}

func ValidateLocationHierarchy(catalog map[string]gameplay.Definition, definitions []gameplay.Definition) error {
	ranks := map[string]int{"world": 0, "realm": 0, "region": 1, "location": 2, "scene": 3}
	for _, definition := range definitions {
		if definition.Kind != "location" {
			continue
		}
		childType := DefinitionLocationNodeType(definition.Content)
		parentKey, _ := definition.Content["parent_key"].(string)
		if (childType == "world" || childType == "realm") && parentKey != "" {
			return fmt.Errorf("%w: location:%s 的根节点不能声明父节点", ErrInvalidContent, definition.Key)
		}
		if childType != "world" && childType != "realm" && parentKey == "" {
			return fmt.Errorf("%w: location:%s 的 %s 节点必须声明父节点", ErrInvalidContent, definition.Key, childType)
		}
		if parentKey != "" {
			parent := catalog["location"+":"+parentKey]
			parentType := DefinitionLocationNodeType(parent.Content)
			if ranks[parentType]+1 != ranks[childType] {
				return fmt.Errorf("%w: location:%s 的 %s 父节点不能是 %s", ErrInvalidContent, definition.Key, childType, parentType)
			}
		}
		seen := map[string]bool{}
		key := definition.Key
		for depth := 1; key != ""; depth++ {
			if seen[key] {
				return fmt.Errorf("%w: location:%s 的 parent_key 形成循环", ErrInvalidContent, definition.Key)
			}
			if depth > 4 {
				return fmt.Errorf("%w: location:%s 的层级深度超过 4", ErrInvalidContent, definition.Key)
			}
			seen[key] = true
			current, ok := catalog["location"+":"+key]
			if !ok {
				break
			}
			key, _ = current.Content["parent_key"].(string)
		}
	}
	return nil
}

func DefinitionLocationNodeType(content map[string]any) string {
	if value, ok := content["node_type"].(string); ok && value != "" {
		return value
	}
	return "location"
}

func TravelableLocationType(nodeType string) bool {
	return nodeType == "location" || nodeType == "scene"
}

func StableDefinitionKey(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || len(value) > 128 {
		return false
	}
	for index, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') ||
			index > 0 && (char == '.' || char == '_' || char == '-') {
			continue
		}
		return false
	}
	return true
}

func StringList(value any) ([]string, bool) {
	raw, ok := value.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(raw))
	seen := map[string]bool{}
	for _, item := range raw {
		value, ok := item.(string)
		if !ok || !StableDefinitionKey(value) || seen[value] {
			return nil, false
		}
		seen[value] = true
		out = append(out, value)
	}
	return out, true
}

func DefinitionObject(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	default:
		return nil, false
	}
}

func ContainsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func PositiveDefinitionInt(value any) (int64, bool) {
	number, ok := NonNegativeDefinitionInt(value)
	return number, ok && number > 0
}

func NonNegativeDefinitionInt(value any) (int64, bool) {
	number, ok := SignedDefinitionInt(value)
	return number, ok && number >= 0
}

func SignedDefinitionInt(value any) (int64, bool) {
	switch number := value.(type) {
	case int:
		return int64(number), true
	case int64:
		return number, true
	case float64:
		return int64(number), number == float64(int64(number))
	default:
		return 0, false
	}
}

func ValidateOutcomes(content gameplay.Content) error {
	enabledOutcomeKeys := map[string]bool{}
	for _, outcome := range content.Outcomes {
		enabledOutcomeKeys[outcome.Key] = outcome.Enabled
	}
	if gameplay.NormalizeRuntimeMode(content.Runtime.Mode) == gameplay.RuntimeModeCampaign {
		campaignOutcomes := map[string]bool{}
		for _, definition := range content.Definitions {
			if definition.Kind != "quest" {
				continue
			}
			quest, _ := worldrules.ParseQuest(map[string]any(definition.Content))
			if quest.CampaignOutcomeKey == "" {
				continue
			}
			if !enabledOutcomeKeys[quest.CampaignOutcomeKey] {
				return fmt.Errorf("%w: quest:%s 引用了未知或未启用的 Campaign 结局 %s", ErrInvalidContent, definition.Key, quest.CampaignOutcomeKey)
			}
			campaignOutcomes[quest.CampaignOutcomeKey] = true
		}
		for _, outcome := range content.Outcomes {
			if outcome.Enabled && outcome.RewardPoints > 0 && !campaignOutcomes[outcome.Key] {
				return fmt.Errorf("%w: 积分结局没有终局任务 %s", ErrInvalidContent, outcome.Key)
			}
		}
	}
	return nil
}

func Shapes() map[string]gameplay.DefinitionValidator {
	out := map[string]gameplay.DefinitionValidator{}
	for _, kind := range []string{"item", "market", "location", "faction", "npc_archetype", "world_event", "quest"} {
		out[kind] = ValidateShape
	}
	return out
}

func Validate(content gameplay.Content) error {
	if err := ValidateReferences(content.Runtime, content.Definitions); err != nil {
		return err
	}
	return ValidateOutcomes(content)
}

func ValidateGeneric(content gameplay.Content) error {
	for _, d := range content.Definitions {
		if key, _ := d.Content["realm_key"].(string); key != "" {
			return fmt.Errorf("通用世界不支持境界引用")
		}
		if requirements, ok := d.Content["entry_requirements"].([]any); ok {
			for _, raw := range requirements {
				m, _ := DefinitionObject(raw)
				if _, ok := m["min_realm_rank"]; ok {
					return fmt.Errorf("通用世界不支持境界准入条件")
				}
			}
		}
	}
	return Validate(content)
}
