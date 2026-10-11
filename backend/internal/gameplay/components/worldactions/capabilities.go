package worldactions

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldcontract"
	"sort"

	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/components/marketrules"
	"github.com/ialtone/fatecraft/internal/storage"
)

type CapabilityProvider struct{ EntryAvailable EntryAvailability }

func (p CapabilityProvider) Project(_ context.Context, input campaign.CapabilityContext, definition campaign.ActionDefinition) (campaign.ActionCapability, error) {
	if p.EntryAvailable == nil {
		p.EntryAvailable = EntryRequirementsMet
	}
	capability := campaign.ActionCapability{Available: true, ArgumentSchema: CloneSchema(definition.ArgumentSchema), Options: []storage.JSONMap{}, Metadata: storage.JSONMap{}}
	state := input.Domain
	if state == nil {
		return DenyCapability(capability, "campaign.state_unavailable", "玩法状态不可用"), nil
	}
	switch definition.Key {
	case worldcontract.ActionGainItem:
		ProjectGrantDefinitionCapability(&capability, state, storage.DefinitionItem, "item_key", true)
	case worldcontract.ActionTradeBuy:
		ProjectTradeCapability(&capability, input.Campaign, state, true)
	case worldcontract.ActionTradeSell:
		ProjectTradeCapability(&capability, input.Campaign, state, false)
	case worldcontract.ActionAdjustMarket:
		ProjectMarketKeys(&capability, input.Campaign, state)
	case worldcontract.ActionTravel:
		ProjectTravelCapability(&capability, input.Campaign, state, p.EntryAvailable)
	case worldcontract.ActionInvestigate:
		if _, ok := state.Definition(storage.DefinitionLocation, input.Campaign.CurrentLocationKey); !ok {
			capability = DenyCapability(capability, "location.unavailable", "当前位置定义不存在")
		}
	case worldcontract.ActionResolveUncertain:
		capability.Metadata = storage.JSONMap{"effects": "roll_only", "requires_followup_domain_action_for_state_change": true}
	case worldcontract.ActionInstantiateNPC:
		ProjectInstantiateNPCCapability(&capability, input.Campaign, state)
	case worldcontract.ActionMoveNPC:
		ProjectMoveNPCCapability(&capability, input.Campaign, state)
	case worldcontract.ActionSetNPCStatus:
		ProjectNPCStatusCapability(&capability, input.Campaign, state)
	case worldcontract.ActionRecordInteraction:
		ProjectCurrentNPCCapability(&capability, input.Campaign, state)
	case worldcontract.ActionAdjustFaction:
		ProjectFactionCapability(&capability, state)
	case worldcontract.ActionAcceptQuest:
		ProjectQuestCapability(&capability, input.Campaign, state, true)
	case worldcontract.ActionSubmitQuest:
		ProjectQuestCapability(&capability, input.Campaign, state, false)
	default:
		return campaign.ActionCapability{}, fmt.Errorf("未知世界动作: %s", definition.Key)
	}
	if len(capability.Metadata) == 0 {
		capability.Metadata = nil
	}
	SortCapabilityOptions(capability.Options)
	return capability, nil
}
func ProjectGrantDefinitionCapability(capability *campaign.ActionCapability, state *storage.CampaignDomainState, kind, field string, stackable bool) {
	for _, definition := range SortedDefinitions(state, kind) {
		grantable, _ := definition.Content["model_grantable"].(bool)
		isStackable, _ := definition.Content["stackable"].(bool)
		if !grantable || kind == storage.DefinitionItem && isStackable != stackable {
			continue
		}
		option := storage.JSONMap{field: definition.Key, "name": definition.Name}
		if stackable {
			option["limits"] = storage.JSONMap{"quantity": storage.JSONMap{"minimum": int64(1), "maximum": DefinitionIntDefault(definition.Content["max_grant_per_action"], 100)}}
		}
		capability.Options = append(capability.Options, option)
	}
	ApplySingleEnum(capability, field, OptionStrings(capability.Options, field), "grant.none_allowed", "当前玩法没有允许模型直接授予的对象")
}

func ApplySingleEnum(capability *campaign.ActionCapability, field string, values []string, code, message string) {
	values = UniqueStrings(values)
	if len(values) == 0 {
		*capability = DenyCapability(*capability, code, message)
		return
	}
	SetStringEnum(capability.ArgumentSchema, field, values)
}

func DenyCapability(capability campaign.ActionCapability, code, message string) campaign.ActionCapability {
	capability.Available = false
	capability.Options = nil
	capability.Metadata = nil
	capability.Violations = []campaign.ActionViolation{{Code: code, Message: message, Public: true}}
	return capability
}

func SetStringEnum(schema storage.JSONMap, field string, values []string) {
	property := SchemaProperty(schema, field)
	enum := make([]any, 0, len(values))
	for _, value := range UniqueStrings(values) {
		enum = append(enum, value)
	}
	if len(enum) > 0 {
		property["enum"] = enum
	}
}

func SchemaProperty(schema storage.JSONMap, field string) map[string]any {
	properties, _ := schema["properties"].(map[string]any)
	property, _ := properties[field].(map[string]any)
	return property
}

func UniqueStrings(values []string) []string {
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

func OptionStrings(options []storage.JSONMap, field string) []string {
	values := make([]string, 0, len(options))
	for _, option := range options {
		if value, ok := option[field].(string); ok && value != "" {
			values = append(values, value)
		}
	}
	return UniqueStrings(values)
}

func SortedDefinitions(state *storage.CampaignDomainState, kind string) []storage.GameplayDefinition {
	out := make([]storage.GameplayDefinition, 0)
	for _, definition := range state.Definitions {
		if definition.Kind == kind {
			out = append(out, definition)
		}
	}
	sort.Slice(out, func(left, right int) bool { return out[left].Key < out[right].Key })
	return out
}

func ProjectTradeCapability(capability *campaign.ActionCapability, row storage.Campaign, state *storage.CampaignDomainState, buying bool) {
	owned := ItemQuantities(state)
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
			stock := MarketStockFact(state, marketState.MarketKey, entry.Kind, entry.Key, entry.QualityKey)
			if stock == nil {
				continue
			}
			stockQuantity, _ := marketrules.Restock(
				stock.Quantity, marketState.LastRestockGameTime, row.GameTime, market, entry,
			)
			maximum := int64(0)
			if buying {
				maximum = stockQuantity
				price, priced := marketrules.UnitPrice(market, entry, stockQuantity, FactionReputation(state, market.FactionKey), true)
				if !priced || price <= 0 {
					continue
				}
				maximum = Minimum(maximum, owned[market.CurrencyItemKey+":common"]/price)
			} else {
				price, priced := marketrules.UnitPrice(market, entry, stockQuantity, FactionReputation(state, market.FactionKey), false)
				if !priced || price <= 0 {
					continue
				}
				if entry.Kind == "item" {
					maximum = Minimum(owned[entry.Key+":"+entry.QualityKey], entry.MaxStock-stockQuantity)
				} else {
					maximum = Minimum(CountSellableEquipment(state, entry.Key, entry.QualityKey), entry.MaxStock-stockQuantity)
					maximum = Minimum(maximum, 1)
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
				for _, instanceID := range SellableEquipmentIDs(state, entry.Key, entry.QualityKey) {
					candidate := CloneOption(option)
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
		*capability = DenyCapability(*capability, "trade.none_available", message)
		return
	}
	for _, field := range []string{"market_key", "item_kind", "item_key", "quality_key"} {
		SetStringEnum(capability.ArgumentSchema, field, OptionStrings(capability.Options, field))
	}
	if !buying {
		SetStringEnumIncludingEmpty(capability.ArgumentSchema, "item_instance_id", instanceIDs)
	}
}

func SetStringEnumIncludingEmpty(schema storage.JSONMap, field string, values []string) {
	property := SchemaProperty(schema, field)
	enum := make([]any, 0, len(values))
	for _, value := range UniqueStrings(values) {
		enum = append(enum, value)
	}
	property["enum"] = enum
}

func CloneOption(option storage.JSONMap) storage.JSONMap {
	clone := make(storage.JSONMap, len(option)+1)
	for key, value := range option {
		clone[key] = value
	}
	return clone
}

func ItemQuantities(state *storage.CampaignDomainState) map[string]int64 {
	out := map[string]int64{}
	for _, stack := range state.ItemStacks {
		out[stack.ItemKey+":"+stack.QualityKey] = stack.Quantity
	}
	return out
}

func MarketStockFact(state *storage.CampaignDomainState, market, kind, key, quality string) *storage.CampaignMarketStock {
	for index := range state.MarketStocks {
		stock := &state.MarketStocks[index]
		if stock.MarketKey == market && stock.ItemKind == kind && stock.ItemKey == key && stock.QualityKey == quality {
			return stock
		}
	}
	return nil
}

func FactionReputation(state *storage.CampaignDomainState, key string) int64 {
	for _, standing := range state.Factions {
		if standing.FactionKey == key {
			return int64(standing.Reputation)
		}
	}
	return 0
}

func CountSellableEquipment(state *storage.CampaignDomainState, key, quality string) int64 {
	return int64(len(SellableEquipmentIDs(state, key, quality)))
}

func SellableEquipmentIDs(state *storage.CampaignDomainState, key, quality string) []string {
	out := make([]string, 0)
	for _, item := range state.ItemInstances {
		if item.DefinitionKey == key && item.QualityKey == quality && item.State == "stored" {
			out = append(out, item.ID.String())
		}
	}
	sort.Strings(out)
	return out
}

func Minimum(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}

func ProjectMarketKeys(capability *campaign.ActionCapability, row storage.Campaign, state *storage.CampaignDomainState) {
	for _, market := range state.Markets {
		if market.LocationKey == row.CurrentLocationKey {
			capability.Options = append(capability.Options, storage.JSONMap{"market_key": market.MarketKey})
		}
	}
	ApplySingleEnum(capability, "market_key", OptionStrings(capability.Options, "market_key"), "market.none_available", "当前位置没有市场")
}

func ProjectTravelCapability(capability *campaign.ActionCapability, row storage.Campaign, state *storage.CampaignDomainState, checkEntry EntryAvailability) {
	current, ok := state.Definition(storage.DefinitionLocation, row.CurrentLocationKey)
	if !ok {
		*capability = DenyCapability(*capability, "location.unavailable", "当前位置定义不存在")
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
		if !ok || !discovered[key] || !targetOK || !checkEntry(state, target.Content) {
			continue
		}
		capability.Options = append(capability.Options, storage.JSONMap{"destination_key": key, "name": target.Name, "travel_minutes": connection["travel_minutes"]})
	}
	ApplySingleEnum(capability, "destination_key", OptionStrings(capability.Options, "destination_key"), "travel.no_route", "当前没有可进入的已发现相邻地点")
}

func ProjectInstantiateNPCCapability(capability *campaign.ActionCapability, row storage.Campaign, state *storage.CampaignDomainState) {
	counts := map[string]int64{}
	for _, npc := range state.NPCs {
		counts[npc.ArchetypeKey]++
	}
	for _, definition := range SortedDefinitions(state, storage.DefinitionNPCArchetype) {
		spawnable, _ := definition.Content["spawnable"].(bool)
		initial, _ := definition.Content["initial"].(bool)
		maximum := DefinitionIntDefault(definition.Content["max_instances"], 1)
		if !spawnable || initial || counts[definition.Key] >= maximum || !ContainsDefinitionString(definition.Content["allowed_locations"], row.CurrentLocationKey) {
			continue
		}
		capability.Options = append(capability.Options, storage.JSONMap{
			"archetype_key": definition.Key, "name": definition.Name, "remaining_instances": maximum - counts[definition.Key],
		})
	}
	ApplySingleEnum(capability, "archetype_key", OptionStrings(capability.Options, "archetype_key"), "npc.none_spawnable", "当前位置没有可实例化 NPC 原型")
}

func ProjectMoveNPCCapability(capability *campaign.ActionCapability, row storage.Campaign, state *storage.CampaignDomainState) {
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
		*capability = DenyCapability(*capability, "npc.none_movable", "当前地点没有可移动 NPC 或相邻地点")
		return
	}
	SetStringEnum(capability.ArgumentSchema, "npc_key", OptionStrings(capability.Options, "npc_key"))
	SetStringEnum(capability.ArgumentSchema, "destination_key", OptionStrings(capability.Options, "destination_key"))
}

func ProjectNPCStatusCapability(capability *campaign.ActionCapability, row storage.Campaign, state *storage.CampaignDomainState) {
	for _, npc := range state.NPCs {
		if npc.CurrentLocationKey != row.CurrentLocationKey {
			continue
		}
		for _, status := range []string{"active", "missing", "dead", "departed", "sealed"} {
			if ValidNPCStatusTransition(npc.Status, status) {
				capability.Options = append(capability.Options, storage.JSONMap{"npc_key": npc.InstanceKey, "status": status})
			}
		}
	}
	if len(capability.Options) == 0 {
		*capability = DenyCapability(*capability, "npc.no_status_transition", "当前地点没有可变更状态的 NPC")
		return
	}
	SetStringEnum(capability.ArgumentSchema, "npc_key", OptionStrings(capability.Options, "npc_key"))
}

func ProjectCurrentNPCCapability(capability *campaign.ActionCapability, row storage.Campaign, state *storage.CampaignDomainState) {
	for _, npc := range state.NPCs {
		if npc.Status == "active" && npc.CurrentLocationKey == row.CurrentLocationKey {
			capability.Options = append(capability.Options, storage.JSONMap{"npc_key": npc.InstanceKey, "name": npc.StableName})
		}
	}
	ApplySingleEnum(capability, "npc_key", OptionStrings(capability.Options, "npc_key"), "npc.none_interactable", "当前地点没有可交互 NPC")
}

func ProjectFactionCapability(capability *campaign.ActionCapability, state *storage.CampaignDomainState) {
	for _, definition := range SortedDefinitions(state, storage.DefinitionFaction) {
		capability.Options = append(capability.Options, storage.JSONMap{"faction_key": definition.Key, "name": definition.Name})
	}
	ApplySingleEnum(capability, "faction_key", OptionStrings(capability.Options, "faction_key"), "faction.none_available", "当前玩法没有可调整势力")
}

func ProjectQuestCapability(capability *campaign.ActionCapability, row storage.Campaign, state *storage.CampaignDomainState, accepting bool) {
	for _, quest := range state.Quests {
		if accepting {
			if quest.Status != "offered" {
				continue
			}
			definition, ok := state.Definition(storage.DefinitionQuest, quest.QuestKey)
			if !ok || !QuestAcceptsAt(definition.Content, row.CurrentLocationKey) {
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
	ApplySingleEnum(capability, "quest_key", OptionStrings(capability.Options, "quest_key"), "quest.none_available", message)
}

func QuestAcceptsAt(content storage.JSONMap, location string) bool {
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

func CloneSchema(schema storage.JSONMap) storage.JSONMap {
	raw, _ := json.Marshal(schema)
	var clone storage.JSONMap
	_ = json.Unmarshal(raw, &clone)
	return clone
}

func SortCapabilityOptions(options []storage.JSONMap) {
	sort.SliceStable(options, func(left, right int) bool {
		leftJSON, _ := json.Marshal(options[left])
		rightJSON, _ := json.Marshal(options[right])
		return string(leftJSON) < string(rightJSON)
	})
}
