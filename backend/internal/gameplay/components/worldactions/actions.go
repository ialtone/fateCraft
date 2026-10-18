package worldactions

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/components/factionrules"
	"github.com/ialtone/fatecraft/internal/gameplay/components/marketrules"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldrules"
	"github.com/ialtone/fatecraft/internal/storage"
	"math"
	"sort"
	"strings"
	"time"
)

func GainItem(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := RequireModelAuthority(execution); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := AllowArguments(arguments, "item_key", "quality_key", "quantity"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	key, err := RequiredKey(arguments, "item_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	quality, err := OptionalKey(arguments, "quality_key", "common")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionItem, key)
	if err != nil {
		return storage.CampaignActionOutcome{}, Rejected("未知物品 %s", key)
	}
	if err := RequireDefinitionGrantAuthority(execution, definition); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if stackable, _ := definition.Content["stackable"].(bool); !stackable {
		return storage.CampaignActionOutcome{}, Rejected("物品 %s 不是可堆叠物品", key)
	}
	maximum := DefinitionIntDefault(definition.Content["max_grant_per_action"], 100)
	quantity, err := RequiredInt(arguments, "quantity", 1, maximum)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	stack, err := execution.Store.ItemStack(execution.Campaign.ID, key, quality)
	if errors.Is(err, storage.ErrNotFound) {
		stack = &storage.CampaignItemStack{
			CampaignID: execution.Campaign.ID, ItemKey: key, QualityKey: quality,
			Metadata: storage.JSONMap{"name": definition.Name},
		}
	} else if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if stack.Quantity > math.MaxInt64-quantity {
		return storage.CampaignActionOutcome{}, Rejected("物品数量溢出")
	}
	stack.Quantity += quantity
	stack.UpdatedAt = time.Now()
	if err := execution.Store.SaveItemStack(stack); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return SingleEvent("inventory.gained", storage.JSONMap{
		"item_key": key, "name": definition.Name, "quality_key": quality,
		"quantity": quantity, "quantity_after": stack.Quantity,
	}), nil
}

func SingleEvent(eventType string, payload storage.JSONMap) storage.CampaignActionOutcome {
	return storage.CampaignActionOutcome{
		Events: []storage.CampaignEventDraft{{EventType: eventType, Visibility: storage.CampaignVisibilityPlayer, Payload: payload}},
		Result: payload,
	}
}

func RequireModelAuthority(execution campaign.ActionExecution) error {
	if execution.ActorType != "model" && execution.ActorType != "system" {
		return Rejected("动作 %s 只能由叙事裁定或系统执行", execution.ActionKey)
	}
	return nil
}

// RequireDefinitionGrantAuthority prevents narrative actions from minting
// economy-backed items or equipment. System-owned workflows may still grant any
// validated definition; model grants require an explicit content whitelist.
func Rejected(format string, args ...any) error {
	return campaign.RejectAction(campaign.ActionViolation{
		Code: "action.precondition_failed", Message: fmt.Sprintf(format, args...), Public: true,
	})
}

func RequireDefinitionGrantAuthority(execution campaign.ActionExecution, definition *storage.GameplayDefinition) error {
	if execution.ActorType == "system" {
		return nil
	}
	if execution.ActorType != "model" {
		return Rejected("动作 %s 只能由叙事裁定或系统执行", execution.ActionKey)
	}
	if grantable, _ := definition.Content["model_grantable"].(bool); !grantable {
		return Rejected("%s 必须通过采集、任务、制作、交易或系统结算获得", definition.Name)
	}
	return nil
}

func AllowArguments(arguments map[string]any, allowed ...string) error {
	set := map[string]bool{}
	for _, key := range allowed {
		set[key] = true
	}
	for key := range arguments {
		if !set[key] {
			return Invalid("包含未知参数 %s", key)
		}
	}
	return nil
}

func Invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", campaign.ErrInvalidAction, fmt.Sprintf(format, args...))
}

func RequiredKey(arguments map[string]any, field string) (string, error) {
	value, ok := arguments[field].(string)
	if !ok || !StableActionKey(value) {
		return "", Invalid("%s 必须是稳定 key", field)
	}
	return value, nil
}

func StableActionKey(value string) bool {
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

func OptionalKey(arguments map[string]any, field, fallback string) (string, error) {
	if _, ok := arguments[field]; !ok {
		return fallback, nil
	}
	return RequiredKey(arguments, field)
}

func RequiredInt(arguments map[string]any, field string, minimum, maximum int64) (int64, error) {
	value, ok := ExactActionInt(arguments[field])
	if !ok || value < minimum || value > maximum {
		return 0, Invalid("%s 必须是 %d 到 %d 的整数", field, minimum, maximum)
	}
	return value, nil
}

func ExactActionInt(value any) (int64, bool) {
	switch number := value.(type) {
	case int:
		return int64(number), true
	case int64:
		return number, true
	case float64:
		if math.IsNaN(number) || math.IsInf(number, 0) || number != math.Trunc(number) ||
			number < math.MinInt64 || number > math.MaxInt64 {
			return 0, false
		}
		return int64(number), true
	default:
		return 0, false
	}
}

func DefinitionIntDefault(value any, fallback int64) int64 {
	if number, ok := ExactActionInt(value); ok {
		return number
	}
	return fallback
}

func TradeBuy(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := AllowArguments(arguments, "market_key", "item_kind", "item_key", "quality_key", "quantity"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	request, err := ParseTradeRequest(arguments, false)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	market, state, stocks, entry, err := PrepareMarket(execution, request.marketKey, request.kind, request.itemKey, request.qualityKey)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	stock := stocks[MarketStockIdentity(entry.Kind, entry.Key, entry.QualityKey)]
	if stock == nil || stock.Quantity < request.quantity {
		return storage.CampaignActionOutcome{}, Rejected("市场库存不足")
	}
	unitPrice, ok := MarketUnitPrice(execution, market, state, entry, stock.Quantity, true)
	if !ok || unitPrice > math.MaxInt64/request.quantity {
		return storage.CampaignActionOutcome{}, Rejected("商品当前不可购买或价格溢出")
	}
	total := unitPrice * request.quantity
	currency, err := execution.Store.ItemStack(execution.Campaign.ID, market.CurrencyItemKey, "common")
	if err != nil || currency.Quantity < total {
		return storage.CampaignActionOutcome{}, Rejected("%s 数量不足，需要 %d", market.CurrencyItemKey, total)
	}
	currency.Quantity -= total
	currency.UpdatedAt = time.Now()
	if err := execution.Store.SaveItemStack(currency); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := GrantTradePurchase(execution, request, entry); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	stock.Quantity -= request.quantity
	stock.UpdatedAt = time.Now()
	if err := execution.Store.SaveMarketStock(stock); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	payload := storage.JSONMap{
		"market_key": request.marketKey, "item_kind": request.kind, "item_key": request.itemKey,
		"quality_key": request.qualityKey, "quantity": request.quantity,
		"unit_price": unitPrice, "total_price": total, "currency_item_key": market.CurrencyItemKey,
		"stock_after": stock.Quantity,
	}
	return SingleEvent("trade.bought", payload), nil
}

func ParseTradeRequest(arguments map[string]any, selling bool) (tradeRequest, error) {
	request := tradeRequest{}
	var err error
	if request.marketKey, err = RequiredKey(arguments, "market_key"); err != nil {
		return request, err
	}
	request.kind, _ = arguments["item_kind"].(string)
	if request.kind != "item" && request.kind != "equipment" {
		return request, Invalid("item_kind 必须是 item 或 equipment")
	}
	if request.itemKey, err = RequiredKey(arguments, "item_key"); err != nil {
		return request, err
	}
	if request.qualityKey, err = OptionalKey(arguments, "quality_key", "common"); err != nil {
		return request, err
	}
	maximum := int64(1000)
	if request.kind == "equipment" {
		maximum = 20
	}
	if request.quantity, err = OptionalInt(arguments, "quantity", 1, 1, maximum); err != nil {
		return request, err
	}
	if request.kind == "equipment" && selling {
		if request.quantity != 1 {
			return request, Invalid("出售装备实例时 quantity 必须为 1")
		}
		if request.instanceID, err = RequiredUUID(arguments, "item_instance_id"); err != nil {
			return request, err
		}
	} else if _, exists := arguments["item_instance_id"]; exists {
		return request, Invalid("item_instance_id 只用于出售装备实例")
	}
	return request, nil
}

func RequiredUUID(arguments map[string]any, field string) (uuid.UUID, error) {
	value, ok := arguments[field].(string)
	if !ok {
		return uuid.Nil, Invalid("%s 必须是 UUID", field)
	}
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, Invalid("%s 必须是 UUID", field)
	}
	return id, nil
}

func OptionalInt(arguments map[string]any, field string, fallback, minimum, maximum int64) (int64, error) {
	if _, ok := arguments[field]; !ok {
		return fallback, nil
	}
	return RequiredInt(arguments, field, minimum, maximum)
}

func PrepareMarket(execution campaign.ActionExecution, marketKey, kind, itemKey, qualityKey string) (marketrules.Market, *storage.CampaignMarketState, map[string]*storage.CampaignMarketStock, marketrules.Entry, error) {
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionMarket, marketKey)
	if err != nil {
		return marketrules.Market{}, nil, nil, marketrules.Entry{}, Rejected("未知市场 %s", marketKey)
	}
	market, ok := marketrules.Parse(map[string]any(definition.Content))
	if !ok {
		return marketrules.Market{}, nil, nil, marketrules.Entry{}, Invalid("市场定义无效: %s", marketKey)
	}
	if market.LocationKey != execution.Campaign.CurrentLocationKey {
		return marketrules.Market{}, nil, nil, marketrules.Entry{}, Rejected("市场不在当前位置")
	}
	entry, ok := marketrules.FindEntry(market, kind, itemKey, qualityKey)
	if !ok {
		return marketrules.Market{}, nil, nil, marketrules.Entry{}, Rejected("市场未经营该商品")
	}
	state, stocks, err := RefreshMarket(execution, marketKey, market)
	return market, state, stocks, entry, err
}

func RefreshMarket(execution campaign.ActionExecution, marketKey string, market marketrules.Market) (*storage.CampaignMarketState, map[string]*storage.CampaignMarketStock, error) {
	state, err := execution.Store.MarketState(execution.Campaign.ID, marketKey)
	if err != nil {
		return nil, nil, Invalid("市场动态状态不存在: %s", marketKey)
	}
	stocks := map[string]*storage.CampaignMarketStock{}
	newLast := state.LastRestockGameTime
	for _, entry := range market.Entries {
		stock, err := execution.Store.MarketStock(execution.Campaign.ID, marketKey, entry.Kind, entry.Key, entry.QualityKey)
		if err != nil {
			return nil, nil, Invalid("市场库存状态缺失: %s", entry.Key)
		}
		quantity, restockedAt := marketrules.Restock(stock.Quantity, state.LastRestockGameTime, execution.Campaign.GameTime, market, entry)
		if restockedAt > newLast {
			newLast = restockedAt
		}
		if quantity != stock.Quantity {
			stock.Quantity = quantity
			stock.UpdatedAt = time.Now()
			if err := execution.Store.SaveMarketStock(stock); err != nil {
				return nil, nil, err
			}
		}
		stocks[MarketStockIdentity(entry.Kind, entry.Key, entry.QualityKey)] = stock
	}
	if newLast != state.LastRestockGameTime {
		state.LastRestockGameTime = newLast
		state.UpdatedAt = time.Now()
		if err := execution.Store.SaveMarketState(state); err != nil {
			return nil, nil, err
		}
	}
	return state, stocks, nil
}

func MarketStockIdentity(kind, key, quality string) string {
	return kind + ":" + key + ":" + quality
}

func MarketUnitPrice(execution campaign.ActionExecution, market marketrules.Market, state *storage.CampaignMarketState, entry marketrules.Entry, stock int64, buying bool) (int64, bool) {
	market.PriceModifierBPS = market.PriceModifierBPS * int64(state.PriceModifierBPS) / 10000
	reputation := int64(0)
	if market.FactionKey != "" {
		standing, err := execution.Store.FactionStanding(execution.Campaign.ID, market.FactionKey)
		if err == nil {
			reputation = int64(standing.Reputation)
		}
	}
	return marketrules.UnitPrice(market, entry, stock, reputation, buying)
}

func GrantTradePurchase(execution campaign.ActionExecution, request tradeRequest, entry marketrules.Entry) error {
	if request.kind == "item" {
		return AddItemStack(execution, request.itemKey, request.qualityKey, request.quantity, "trade.buy")
	}
	for count := int64(0); count < request.quantity; count++ {
		if err := CreateEquipmentInstance(execution, entry.Key, entry.QualityKey, "trade.buy"); err != nil {
			return err
		}
	}
	return nil
}

func AddItemStack(execution campaign.ActionExecution, itemKey, qualityKey string, quantity int64, source string) error {
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionItem, itemKey)
	if err != nil || definition.Content["stackable"] != true {
		return Invalid("产物或交易物品不是已发布的可堆叠物品: %s", itemKey)
	}
	stack, err := execution.Store.ItemStack(execution.Campaign.ID, itemKey, qualityKey)
	if errors.Is(err, storage.ErrNotFound) {
		stack = &storage.CampaignItemStack{
			CampaignID: execution.Campaign.ID, ItemKey: itemKey, QualityKey: qualityKey,
			Metadata: storage.JSONMap{"name": definition.Name, "source": source},
		}
	} else if err != nil {
		return err
	}
	if quantity <= 0 || stack.Quantity > math.MaxInt64-quantity {
		return Invalid("物品数量无效或溢出")
	}
	stack.Quantity += quantity
	stack.UpdatedAt = time.Now()
	return execution.Store.SaveItemStack(stack)
}

func CreateEquipmentInstance(execution campaign.ActionExecution, definitionKey, qualityKey, source string) error {
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionEquipment, definitionKey)
	if err != nil {
		return Invalid("未知装备产物 %s", definitionKey)
	}
	durability := DefinitionIntDefault(definition.Content["durability_max"], 0)
	if durability <= 0 || durability > math.MaxInt32 {
		return Invalid("装备产物耐久定义无效: %s", definitionKey)
	}
	now := time.Now()
	return execution.Store.CreateItemInstance(&storage.CampaignItemInstance{
		ID: uuid.New(), CampaignID: execution.Campaign.ID, DefinitionKey: definitionKey,
		QualityKey: qualityKey, State: "stored", Durability: int(durability), MaxDurability: int(durability),
		Properties: storage.JSONMap{"name": definition.Name, "bonuses": definition.Content["bonuses"]},
		Provenance: storage.JSONMap{"source": source}, CreatedAt: now, UpdatedAt: now,
	})
}

func TradeSell(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := AllowArguments(arguments, "market_key", "item_kind", "item_key", "quality_key", "quantity", "item_instance_id"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	request, err := ParseTradeRequest(arguments, true)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	market, state, stocks, entry, err := PrepareMarket(execution, request.marketKey, request.kind, request.itemKey, request.qualityKey)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	stock := stocks[MarketStockIdentity(entry.Kind, entry.Key, entry.QualityKey)]
	if stock == nil || stock.Quantity > entry.MaxStock-request.quantity {
		return storage.CampaignActionOutcome{}, Rejected("市场收购容量不足")
	}
	unitPrice, ok := MarketUnitPrice(execution, market, state, entry, stock.Quantity, false)
	if !ok || unitPrice > math.MaxInt64/request.quantity {
		return storage.CampaignActionOutcome{}, Rejected("商品当前不可出售或价格溢出")
	}
	if err := ConsumeTradeSale(execution, request); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	total := unitPrice * request.quantity
	if err := AddItemStack(execution, market.CurrencyItemKey, "common", total, "trade.sell"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	stock.Quantity += request.quantity
	stock.UpdatedAt = time.Now()
	if err := execution.Store.SaveMarketStock(stock); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	payload := storage.JSONMap{
		"market_key": request.marketKey, "item_kind": request.kind, "item_key": request.itemKey,
		"quality_key": request.qualityKey, "quantity": request.quantity,
		"unit_price": unitPrice, "total_price": total, "currency_item_key": market.CurrencyItemKey,
		"stock_after": stock.Quantity,
	}
	if request.instanceID != uuid.Nil {
		payload["item_instance_id"] = request.instanceID
	}
	return SingleEvent("trade.sold", payload), nil
}

func ConsumeTradeSale(execution campaign.ActionExecution, request tradeRequest) error {
	if request.kind == "item" {
		stack, err := execution.Store.ItemStack(execution.Campaign.ID, request.itemKey, request.qualityKey)
		if err != nil || stack.Quantity < request.quantity {
			return Rejected("出售物品数量不足")
		}
		stack.Quantity -= request.quantity
		stack.UpdatedAt = time.Now()
		return execution.Store.SaveItemStack(stack)
	}
	instance, err := execution.Store.ItemInstance(execution.Campaign.ID, request.instanceID)
	if err != nil || instance.State != "stored" || instance.DefinitionKey != request.itemKey || instance.QualityKey != request.qualityKey {
		return Rejected("装备实例不存在、已装备或与商品不匹配")
	}
	instance.State = "consumed"
	instance.EquippedSlot = nil
	instance.UpdatedAt = time.Now()
	return execution.Store.SaveItemInstance(instance)
}

func AdjustMarketPrice(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if execution.ActorType != "system" {
		return storage.CampaignActionOutcome{}, Rejected("市场价格修正只能由系统执行")
	}
	if err := AllowArguments(arguments, "market_key", "price_modifier_bps"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	marketKey, err := RequiredKey(arguments, "market_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	modifier, err := RequiredInt(arguments, "price_modifier_bps", 1000, 30000)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	state, err := execution.Store.MarketState(execution.Campaign.ID, marketKey)
	if err != nil {
		return storage.CampaignActionOutcome{}, Rejected("未知市场 %s", marketKey)
	}
	previous := state.PriceModifierBPS
	state.PriceModifierBPS = int(modifier)
	state.UpdatedAt = time.Now()
	if err := execution.Store.SaveMarketState(state); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return SingleEvent("market.price_modifier_changed", storage.JSONMap{
		"market_key": marketKey, "previous_bps": previous, "price_modifier_bps": modifier,
	}), nil
}

func Travel(_ context.Context, execution campaign.ActionExecution, arguments map[string]any, checkEntry EntryCheck) (storage.CampaignActionOutcome, error) {
	if err := AllowArguments(arguments, "destination_key"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	destination, err := RequiredKey(arguments, "destination_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if destination == execution.Campaign.CurrentLocationKey {
		return storage.CampaignActionOutcome{}, Rejected("已经位于目标地点")
	}
	known, err := execution.Store.IsLocationDiscovered(execution.Campaign.ID, destination)
	if err != nil || !known {
		return storage.CampaignActionOutcome{}, Rejected("目标地点尚未发现: %s", destination)
	}
	current, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionLocation, execution.Campaign.CurrentLocationKey)
	if err != nil {
		return storage.CampaignActionOutcome{}, Rejected("当前位置定义不存在")
	}
	minutes, connected := ConnectionTravelMinutes(current.Content, destination)
	if !connected {
		return storage.CampaignActionOutcome{}, Rejected("目标地点与当前位置不相邻")
	}
	destinationDefinition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionLocation, destination)
	if err != nil {
		return storage.CampaignActionOutcome{}, Rejected("目标地点定义不存在")
	}
	missing, err := checkEntry(execution, destinationDefinition.Content)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if len(missing) > 0 {
		return storage.CampaignActionOutcome{}, Rejected("缺少进入权限: %s", strings.Join(missing, ", "))
	}
	now := time.Now()
	if err := execution.Store.DiscoverLocation(&storage.CampaignLocationDiscovery{
		CampaignID: execution.Campaign.ID, LocationKey: destination,
		DiscoveredAtGameTime: execution.Campaign.GameTime, Visited: true,
		Knowledge: storage.JSONMap{}, UpdatedAt: now,
	}); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	outcome := storage.CampaignActionOutcome{
		GameTimeDelta: minutes, CurrentLocationKey: &destination,
		Events: []storage.CampaignEventDraft{
			{EventType: "travel.started", Visibility: storage.CampaignVisibilityModel, Payload: storage.JSONMap{
				"from_location_key": execution.Campaign.CurrentLocationKey, "to_location_key": destination, "minutes": minutes,
			}},
			{EventType: "travel.arrived", Visibility: storage.CampaignVisibilityPlayer, Payload: storage.JSONMap{
				"from_location_key": execution.Campaign.CurrentLocationKey, "location_key": destination, "minutes": minutes,
			}},
		},
		Result: storage.JSONMap{"location_key": destination, "game_time_delta": minutes},
	}
	return outcome, nil
}

func ConnectionTravelMinutes(content storage.JSONMap, destination string) (int64, bool) {
	connections, ok := content["connections"].([]any)
	if !ok {
		return 0, false
	}
	for _, raw := range connections {
		connection, ok := raw.(map[string]any)
		if !ok || connection["location_key"] != destination {
			continue
		}
		minutes, ok := ExactActionInt(connection["travel_minutes"])
		return minutes, ok && minutes > 0
	}
	return 0, false
}

func Investigate(ctx context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := AllowArguments(arguments); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	current, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionLocation, execution.Campaign.CurrentLocationKey)
	if err != nil {
		return storage.CampaignActionOutcome{}, Rejected("当前位置定义不存在")
	}
	connections, _ := current.Content["connections"].([]any)
	unknown := make([]string, 0)
	for _, raw := range connections {
		connection, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		key, _ := connection["location_key"].(string)
		known, checkErr := execution.Store.IsLocationDiscovered(execution.Campaign.ID, key)
		if checkErr != nil {
			return storage.CampaignActionOutcome{}, checkErr
		}
		if !known {
			unknown = append(unknown, key)
		}
	}
	minutes := DefinitionIntDefault(current.Content["investigation_minutes"], 120)
	payload := storage.JSONMap{"location_key": execution.Campaign.CurrentLocationKey, "minutes": minutes}
	result := storage.JSONMap{"discovered": false, "game_time_delta": minutes}
	var rollResult *campaign.CampaignRollResult
	if len(unknown) > 0 {
		danger := DefinitionIntDefault(current.Content["danger_rank"], 0)
		rollResult, err = ResolveCampaignRoll(ctx, execution, campaign.CampaignRollRequest{
			Key: "world.investigate." + execution.Campaign.CurrentLocationKey, Title: "调查判定",
			Description: "调查" + current.Name, Category: "exploration", BaseSuccessRate: int(ClampInt64(70-danger*4, 30, 80)), Sides: 100,
		})
		if err != nil {
			return storage.CampaignActionOutcome{}, err
		}
		success := true
		if rollResult != nil {
			success = rollResult.Success
		}
		if success {
			index := int(DeterministicUint64(execution, "investigate:"+execution.Campaign.CurrentLocationKey) % uint64(len(unknown)))
			discovered := unknown[index]
			if err := execution.Store.DiscoverLocation(&storage.CampaignLocationDiscovery{
				CampaignID: execution.Campaign.ID, LocationKey: discovered,
				DiscoveredAtGameTime: execution.Campaign.GameTime + minutes, Visited: false,
				Knowledge: storage.JSONMap{"source": "investigation"}, UpdatedAt: time.Now(),
			}); err != nil {
				return storage.CampaignActionOutcome{}, err
			}
			payload["discovered_location_key"] = discovered
			result["discovered"] = true
			result["location_key"] = discovered
		}
	}
	outcome := storage.CampaignActionOutcome{
		GameTimeDelta: minutes,
		Events: []storage.CampaignEventDraft{{
			EventType: "world.investigated", Visibility: storage.CampaignVisibilityPlayer, Payload: payload,
		}}, Result: result,
	}
	AttachCampaignRoll(&outcome, rollResult)
	return outcome, nil
}

func DeterministicUint64(execution campaign.ActionExecution, purpose string) uint64 {
	payload := fmt.Sprintf("%d:%d:%s:%s", execution.Campaign.RandomSeed, execution.Campaign.Revision, execution.ActionID, purpose)
	sum := sha256.Sum256([]byte(payload))
	return binary.BigEndian.Uint64(sum[:8])
}

func ClampInt64(value, minimum, maximum int64) int64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func ResolveCampaignRoll(ctx context.Context, execution campaign.ActionExecution, request campaign.CampaignRollRequest) (*campaign.CampaignRollResult, error) {
	if execution.Roll != nil {
		result, err := execution.Roll(ctx, request)
		if err != nil || result != nil {
			return result, err
		}
	}
	if request.Sides != 100 || request.BaseSuccessRate < 0 || request.BaseSuccessRate > 100 {
		return nil, Invalid("中立判定需要 D100 与 0 到 100 的成功率")
	}
	rolled := int(DeterministicPercent(execution, "roll:"+request.Key))
	success := rolled <= request.BaseSuccessRate
	level := "failure"
	if success {
		level = "success"
	}
	text := fmt.Sprintf("%s：D100 = %d，成功阈值 %d（%s）", request.Title, rolled, request.BaseSuccessRate, level)
	return &campaign.CampaignRollResult{Success: success, Level: level, Rolled: rolled, Sides: 100, SuccessRate: request.BaseSuccessRate, Snapshot: &storage.CampaignRollSnapshot{
		RollKey: request.Key, Title: request.Title, Category: request.Category, Success: success, Content: text, Markdown: text,
		Metadata: storage.JSONMap{"roll_key": request.Key, "campaign_domain_roll": true, "roll": rolled, "chance": request.BaseSuccessRate, "rule": "world.d100@1"},
	}}, nil
}

func AttachCampaignRoll(outcome *storage.CampaignActionOutcome, result *campaign.CampaignRollResult) {
	if outcome == nil || result == nil {
		return
	}
	outcome.Roll = result.Snapshot
	if outcome.Result == nil {
		outcome.Result = storage.JSONMap{}
	}
	outcome.Result["roll"] = result.Rolled
	outcome.Result["chance"] = result.SuccessRate
	outcome.Result["roll_level"] = result.Level
}

func ResolveUncertainAction(ctx context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := AllowArguments(arguments, "kind", "description"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	kind, ok := arguments["kind"].(string)
	rules := map[string]struct {
		title    string
		category string
		base     int
	}{
		"social":      {"交涉判定", "social", 60},
		"insight":     {"洞察判定", "exploration", 60},
		"stealth":     {"潜行判定", "exploration", 55},
		"exploration": {"探索判定", "exploration", 60},
		"escape":      {"脱险判定", "exploration", 50},
	}
	rule, ok := rules[kind]
	if !ok {
		return storage.CampaignActionOutcome{}, Invalid("kind 必须是 social、insight、stealth、exploration 或 escape")
	}
	description, err := OptionalText(arguments, "description", 160)
	if err != nil || strings.TrimSpace(description) == "" {
		return storage.CampaignActionOutcome{}, Invalid("description 必须是 1 到 160 个字符的公开行动描述")
	}
	rollResult, err := ResolveCampaignRoll(ctx, execution, campaign.CampaignRollRequest{
		Key: "uncertain." + kind, Title: rule.title, Description: description, Category: rule.category,
		BaseSuccessRate: rule.base, Sides: 100,
	})
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	success := DeterministicPercent(execution, "uncertain:"+kind) <= int64(rule.base)
	level, rolled, chance := "success", int(DeterministicPercent(execution, "uncertain:"+kind)), rule.base
	if !success {
		level = "failure"
	}
	if rollResult != nil {
		success, level, rolled, chance = rollResult.Success, rollResult.Level, rollResult.Rolled, rollResult.SuccessRate
	}
	payload := storage.JSONMap{
		"kind": kind, "description": description, "success": success, "level": level,
		"roll": rolled, "chance": chance,
	}
	outcome := storage.CampaignActionOutcome{
		Events: []storage.CampaignEventDraft{{EventType: "fortune.uncertain_resolved", Visibility: storage.CampaignVisibilityPlayer, Payload: payload}},
		Result: payload,
	}
	AttachCampaignRoll(&outcome, rollResult)
	return outcome, nil
}

func OptionalText(arguments map[string]any, field string, maxLength int) (string, error) {
	raw, ok := arguments[field]
	if !ok {
		return "", nil
	}
	value, ok := raw.(string)
	if !ok || strings.TrimSpace(value) != value || len([]rune(value)) > maxLength {
		return "", Invalid("%s 必须是不超过 %d 个字符的文本", field, maxLength)
	}
	return value, nil
}

func DeterministicPercent(execution campaign.ActionExecution, purpose string) int64 {
	return int64(DeterministicUint64(execution, purpose)%100) + 1
}

func InstantiateNPC(_ context.Context, execution campaign.ActionExecution, arguments map[string]any, prepareNPC NPCInitializer) (storage.CampaignActionOutcome, error) {
	if err := RequireModelAuthority(execution); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := AllowArguments(arguments, "archetype_key", "instance_key", "stable_name"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	archetypeKey, err := RequiredKey(arguments, "archetype_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	instanceKey, err := RequiredKey(arguments, "instance_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	definition, err := execution.Store.Definition(
		execution.Campaign.ChallengeVersionID, storage.DefinitionNPCArchetype, archetypeKey,
	)
	if err != nil {
		return storage.CampaignActionOutcome{}, Rejected("未知 NPC 原型 %s", archetypeKey)
	}
	spawnable, _ := definition.Content["spawnable"].(bool)
	initial, _ := definition.Content["initial"].(bool)
	if !spawnable || initial {
		return storage.CampaignActionOutcome{}, Rejected("NPC 原型不可动态实例化: %s", archetypeKey)
	}
	if !ContainsDefinitionString(definition.Content["allowed_locations"], execution.Campaign.CurrentLocationKey) {
		return storage.CampaignActionOutcome{}, Rejected("NPC 原型不能在当前地点实例化")
	}
	if existing, lookupErr := execution.Store.NPC(execution.Campaign.ID, instanceKey); lookupErr == nil && existing != nil {
		return storage.CampaignActionOutcome{}, Rejected("NPC instance_key 已存在: %s", instanceKey)
	} else if lookupErr != nil && !errors.Is(lookupErr, storage.ErrNotFound) {
		return storage.CampaignActionOutcome{}, lookupErr
	}
	maximum := DefinitionIntDefault(definition.Content["max_instances"], 1)
	count, err := execution.Store.CountNPCsByArchetype(execution.Campaign.ID, archetypeKey)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if maximum <= 0 || count >= maximum {
		return storage.CampaignActionOutcome{}, Rejected("NPC 原型实例数已达上限")
	}
	name, err := OptionalText(arguments, "stable_name", 128)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if name == "" {
		name = definition.Name
	}
	factionKey, _ := definition.Content["faction_key"].(string)
	publicProfile, _ := definition.Content["public_profile"].(map[string]any)
	privateState, _ := definition.Content["private_state"].(map[string]any)
	goals, _ := definition.Content["goals"].(map[string]any)
	if publicProfile == nil {
		publicProfile = map[string]any{}
	}
	if privateState == nil {
		privateState = map[string]any{}
	}
	if goals == nil {
		goals = map[string]any{}
	}
	now := time.Now()
	npc := &storage.CampaignNPC{
		ID: uuid.New(), CampaignID: execution.Campaign.ID, ArchetypeKey: archetypeKey,
		InstanceKey: instanceKey, StableName: name, FactionKey: factionKey,
		CurrentLocationKey: execution.Campaign.CurrentLocationKey,
		Status:             "active", PublicProfile: storage.JSONMap(publicProfile), PrivateState: storage.JSONMap(privateState),
		Goals: storage.JSONMap(goals), LastActiveAt: execution.Campaign.GameTime,
		CreatedAt: now, UpdatedAt: now,
	}
	if prepareNPC != nil {
		prepareNPC(*definition, npc)
	}
	if err := execution.Store.CreateNPC(npc); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return SingleEvent("npc.instantiated", storage.JSONMap{
		"instance_key": instanceKey, "archetype_key": archetypeKey, "name": name,
		"location_key": execution.Campaign.CurrentLocationKey,
	}), nil
}

func ContainsDefinitionString(value any, wanted string) bool {
	list, ok := value.([]any)
	if !ok {
		return false
	}
	for _, raw := range list {
		if raw == wanted {
			return true
		}
	}
	return false
}

func MoveNPC(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := RequireModelAuthority(execution); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := AllowArguments(arguments, "npc_key", "destination_key"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	npcKey, err := RequiredKey(arguments, "npc_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	destination, err := RequiredKey(arguments, "destination_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	npc, err := execution.Store.NPC(execution.Campaign.ID, npcKey)
	if err != nil || npc.Status != "active" {
		return storage.CampaignActionOutcome{}, Rejected("NPC 不存在或当前不能移动: %s", npcKey)
	}
	if execution.ActorType == "model" && npc.CurrentLocationKey != execution.Campaign.CurrentLocationKey {
		return storage.CampaignActionOutcome{}, Rejected("模型只能移动当前地点的 NPC")
	}
	if destination == npc.CurrentLocationKey {
		return storage.CampaignActionOutcome{}, Rejected("NPC 已位于目标地点")
	}
	current, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionLocation, npc.CurrentLocationKey)
	if err != nil {
		return storage.CampaignActionOutcome{}, Invalid("NPC 当前位置定义不存在")
	}
	if _, connected := ConnectionTravelMinutes(current.Content, destination); !connected {
		return storage.CampaignActionOutcome{}, Rejected("NPC 目标地点与当前位置不相邻")
	}
	if _, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionLocation, destination); err != nil {
		return storage.CampaignActionOutcome{}, Rejected("NPC 目标地点定义不存在")
	}
	from := npc.CurrentLocationKey
	npc.CurrentLocationKey = destination
	npc.LastActiveAt = execution.Campaign.GameTime
	npc.UpdatedAt = time.Now()
	if err := execution.Store.SaveNPC(npc); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	visibility := storage.CampaignVisibilityModel
	if from == execution.Campaign.CurrentLocationKey || destination == execution.Campaign.CurrentLocationKey {
		visibility = storage.CampaignVisibilityPlayer
	}
	payload := storage.JSONMap{"npc_key": npcKey, "from_location_key": from, "location_key": destination}
	return storage.CampaignActionOutcome{
		Events: []storage.CampaignEventDraft{{EventType: "npc.moved", Visibility: visibility, Payload: payload}},
		Result: payload,
	}, nil
}

func SetNPCStatus(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := RequireModelAuthority(execution); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := AllowArguments(arguments, "npc_key", "status"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	npcKey, err := RequiredKey(arguments, "npc_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	status, err := RequiredKey(arguments, "status")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	npc, err := execution.Store.NPC(execution.Campaign.ID, npcKey)
	if err != nil {
		return storage.CampaignActionOutcome{}, Rejected("NPC 不存在: %s", npcKey)
	}
	if execution.ActorType == "model" && npc.CurrentLocationKey != execution.Campaign.CurrentLocationKey {
		return storage.CampaignActionOutcome{}, Rejected("模型只能更新当前地点的 NPC")
	}
	if !ValidNPCStatusTransition(npc.Status, status) {
		return storage.CampaignActionOutcome{}, Rejected("NPC 状态不能从 %s 变为 %s", npc.Status, status)
	}
	previous := npc.Status
	npc.Status = status
	npc.LastActiveAt = execution.Campaign.GameTime
	npc.UpdatedAt = time.Now()
	if err := execution.Store.SaveNPC(npc); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	visibility := storage.CampaignVisibilityModel
	if npc.CurrentLocationKey == execution.Campaign.CurrentLocationKey {
		visibility = storage.CampaignVisibilityPlayer
	}
	payload := storage.JSONMap{"npc_key": npcKey, "previous_status": previous, "status": status}
	return storage.CampaignActionOutcome{
		Events: []storage.CampaignEventDraft{{EventType: "npc.status_changed", Visibility: visibility, Payload: payload}},
		Result: payload,
	}, nil
}

func ValidNPCStatusTransition(current, target string) bool {
	if current == target {
		return false
	}
	allowed := map[string]map[string]bool{
		"active":   {"missing": true, "dead": true, "departed": true, "sealed": true},
		"missing":  {"active": true, "dead": true, "departed": true},
		"sealed":   {"active": true, "dead": true},
		"departed": {"active": true},
	}
	return allowed[current][target]
}

func RecordInteraction(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	playerMode := execution.ActorType == "player"
	if playerMode {
		if err := AllowArguments(arguments, "npc_key", "trust_delta", "affinity_delta"); err != nil {
			return storage.CampaignActionOutcome{}, err
		}
	} else if err := RequireModelAuthority(execution); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if !playerMode {
		if err := AllowArguments(arguments, "npc_key", "trust_delta", "fear_delta", "affinity_delta", "hostility_delta", "debt_delta", "public_stance", "known_fact_key", "known_fact"); err != nil {
			return storage.CampaignActionOutcome{}, err
		}
	}
	npcKey, err := RequiredKey(arguments, "npc_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	npc, err := execution.Store.NPC(execution.Campaign.ID, npcKey)
	if err != nil || npc.Status != "active" {
		return storage.CampaignActionOutcome{}, Rejected("NPC 不存在或不可交互: %s", npcKey)
	}
	if npc.CurrentLocationKey != execution.Campaign.CurrentLocationKey {
		return storage.CampaignActionOutcome{}, Rejected("NPC 不在当前地点")
	}
	relation, err := execution.Store.Relationship(execution.Campaign.ID, npc.ID)
	if errors.Is(err, storage.ErrNotFound) {
		relation = &storage.CampaignRelationship{
			CampaignID: execution.Campaign.ID, NPCID: npc.ID, PublicStance: "neutral", KnownFacts: storage.JSONMap{},
		}
	} else if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	deltas := map[string]int64{}
	if playerMode {
		trustDelta, fieldErr := OptionalInt(arguments, "trust_delta", 1, 1, 1)
		if fieldErr != nil {
			return storage.CampaignActionOutcome{}, fieldErr
		}
		affinityDelta, fieldErr := OptionalInt(arguments, "affinity_delta", 1, 1, 1)
		if fieldErr != nil {
			return storage.CampaignActionOutcome{}, fieldErr
		}
		deltas["trust_delta"] = trustDelta
		deltas["affinity_delta"] = affinityDelta
	} else {
		for _, field := range []string{"trust_delta", "fear_delta", "affinity_delta", "hostility_delta"} {
			value, fieldErr := OptionalInt(arguments, field, 0, -20, 20)
			if fieldErr != nil {
				return storage.CampaignActionOutcome{}, fieldErr
			}
			deltas[field] = value
		}
	}
	relation.Trust = int(ClampInt64(int64(relation.Trust)+deltas["trust_delta"], -100, 100))
	relation.Fear = int(ClampInt64(int64(relation.Fear)+deltas["fear_delta"], 0, 100))
	relation.Affinity = int(ClampInt64(int64(relation.Affinity)+deltas["affinity_delta"], -100, 100))
	relation.Hostility = int(ClampInt64(int64(relation.Hostility)+deltas["hostility_delta"], 0, 100))
	if playerMode {
		switch {
		case relation.Trust >= 8 && relation.Hostility == 0:
			relation.PublicStance = "friendly"
		case relation.Trust >= 4 && relation.Hostility == 0:
			relation.PublicStance = "cordial"
		case relation.Hostility > 0:
			relation.PublicStance = "hostile"
		default:
			relation.PublicStance = "neutral"
		}
	} else {
		debtDelta, err := OptionalInt(arguments, "debt_delta", 0, -1000, 1000)
		if err != nil {
			return storage.CampaignActionOutcome{}, err
		}
		relation.Debt += debtDelta
		if stance, ok := arguments["public_stance"]; ok {
			value, valid := stance.(string)
			if !valid || strings.TrimSpace(value) == "" || len([]rune(value)) > 32 {
				return storage.CampaignActionOutcome{}, Invalid("public_stance 无效")
			}
			relation.PublicStance = value
		}
		if factKeyRaw, ok := arguments["known_fact_key"]; ok {
			factKey, valid := factKeyRaw.(string)
			fact, hasFact := arguments["known_fact"]
			if !valid || !StableActionKey(factKey) || !hasFact {
				return storage.CampaignActionOutcome{}, Invalid("known_fact_key 与 known_fact 必须成对且 key 合法")
			}
			if relation.KnownFacts == nil {
				relation.KnownFacts = storage.JSONMap{}
			}
			relation.KnownFacts[factKey] = fact
		} else if _, hasFact := arguments["known_fact"]; hasFact {
			return storage.CampaignActionOutcome{}, Invalid("known_fact 缺少 known_fact_key")
		}
	}
	relation.LastInteractionAt = execution.Campaign.GameTime
	relation.UpdatedAt = time.Now()
	if err := execution.Store.SaveRelationship(relation); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	outcome := SingleEvent("npc.relationship_changed", storage.JSONMap{
		"npc_key": npcKey, "npc_name": npc.StableName, "trust": relation.Trust,
		"fear": relation.Fear, "affinity": relation.Affinity, "hostility": relation.Hostility,
		"debt": relation.Debt, "public_stance": relation.PublicStance,
	})
	if playerMode {
		outcome.GameTimeDelta = 15
	}
	return outcome, nil
}

func AdjustFaction(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := RequireModelAuthority(execution); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := AllowArguments(arguments, "faction_key", "reputation_delta", "contribution_delta", "wanted_delta"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	key, err := RequiredKey(arguments, "faction_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionFaction, key)
	if err != nil {
		return storage.CampaignActionOutcome{}, Rejected("未知势力 %s", key)
	}
	reputationDelta, err := OptionalInt(arguments, "reputation_delta", 0, -500, 500)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	contributionDelta, err := OptionalInt(arguments, "contribution_delta", 0, -1000, 1000)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	wantedDelta, err := OptionalInt(arguments, "wanted_delta", 0, -1, 1)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	standing, err := execution.Store.FactionStanding(execution.Campaign.ID, key)
	if errors.Is(err, storage.ErrNotFound) {
		standing = &storage.CampaignFactionStanding{
			CampaignID: execution.Campaign.ID, FactionKey: key, PositionKey: "outsider", Permissions: storage.JSONMap{},
		}
	} else if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	standing.Reputation = int(ClampInt64(int64(standing.Reputation)+reputationDelta, -10000, 10000))
	standing.Contribution = ClampInt64(standing.Contribution+contributionDelta, 0, factionrules.MaximumContribution)
	standing.WantedLevel = int(ClampInt64(int64(standing.WantedLevel)+wantedDelta, 0, 5))
	standing.PositionKey = factionrules.Position(definition.Content, standing.Contribution)
	standing.Permissions = storage.JSONMap(factionrules.DerivePermissions(definition.Content, standing.Reputation))
	standing.UpdatedAt = time.Now()
	if err := execution.Store.SaveFactionStanding(standing); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return SingleEvent("faction.reputation_changed", storage.JSONMap{
		"faction_key": key, "reputation": standing.Reputation,
		"reputation_level": factionrules.ReputationLevel(standing.Reputation), "position_key": standing.PositionKey,
		"contribution": standing.Contribution, "wanted_level": standing.WantedLevel,
		"permissions": standing.Permissions,
	}), nil
}

func AcceptQuest(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := AllowArguments(arguments, "quest_key"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	key, err := RequiredKey(arguments, "quest_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	definition, parsed, err := QuestDefinition(execution, key)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	row, err := execution.Store.Quest(execution.Campaign.ID, key)
	if err != nil || row.Status != "offered" {
		return storage.CampaignActionOutcome{}, Rejected("任务 %s 当前不可接受", key)
	}
	if len(parsed.AcceptLocationKeys) > 0 && !ContainsStringValue(parsed.AcceptLocationKeys, execution.Campaign.CurrentLocationKey) {
		return storage.CampaignActionOutcome{}, Rejected("任务 %s 不能在当前位置接受", key)
	}
	now := execution.Campaign.GameTime
	row.Status = "active"
	row.AcceptedAtGameTime = &now
	row.Progress = storage.JSONMap{}
	row.UpdatedAt = time.Now()
	if parsed.DeadlineMinutes > 0 {
		if now > math.MaxInt64-parsed.DeadlineMinutes {
			return storage.CampaignActionOutcome{}, Invalid("任务截止时间溢出")
		}
		due := now + parsed.DeadlineMinutes
		row.DueGameTime = &due
		if err := execution.Store.CreateScheduledEvent(&storage.CampaignScheduledEvent{
			ID: uuid.New(), CampaignID: execution.Campaign.ID, EventKind: "quest_deadline",
			TemplateKey: key, DueGameTime: due, Priority: 1000, Status: "pending", Occurrence: 0,
			Scope: storage.JSONMap{}, Payload: storage.JSONMap{"quest_key": key}, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}); err != nil {
			return storage.CampaignActionOutcome{}, err
		}
	}
	if err := execution.Store.SaveQuest(row); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return storage.CampaignActionOutcome{
		Events: []storage.CampaignEventDraft{{EventType: "quest.state_changed", Visibility: storage.CampaignVisibilityPlayer, Payload: storage.JSONMap{
			"quest_key": key, "name": definition.Name, "quest_type": row.QuestType, "from": "offered", "to": "active", "due_game_time": row.DueGameTime,
		}}},
		Result: storage.JSONMap{"quest_key": key, "status": "active", "due_game_time": row.DueGameTime},
	}, nil
}

func QuestDefinition(execution campaign.ActionExecution, key string) (*storage.GameplayDefinition, worldrules.Quest, error) {
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionQuest, key)
	if err != nil {
		return nil, worldrules.Quest{}, Rejected("任务定义不存在: %s", key)
	}
	parsed, ok := worldrules.ParseQuest(map[string]any(definition.Content))
	if !ok {
		return nil, worldrules.Quest{}, Invalid("任务定义无效: %s", key)
	}
	return definition, parsed, nil
}

func ContainsStringValue(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func SubmitQuest(ctx context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := AllowArguments(arguments, "quest_key"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	key, err := RequiredKey(arguments, "quest_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	definition, parsed, err := QuestDefinition(execution, key)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	row, err := execution.Store.Quest(execution.Campaign.ID, key)
	if err != nil || row.Status != "active" && row.Status != "blocked" {
		return storage.CampaignActionOutcome{}, Rejected("任务 %s 当前不可提交", key)
	}
	if row.DueGameTime != nil && *row.DueGameTime <= execution.Campaign.GameTime {
		return storage.CampaignActionOutcome{}, Rejected("任务 %s 已到截止时间，需先结算到期事件", key)
	}
	progress, satisfied, err := EvaluateQuestObjectives(execution, parsed)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	from := row.Status
	row.Progress = progress
	row.UpdatedAt = time.Now()
	if !satisfied {
		row.Status = "blocked"
		if err := execution.Store.SaveQuest(row); err != nil {
			return storage.CampaignActionOutcome{}, err
		}
		return storage.CampaignActionOutcome{
			Events: []storage.CampaignEventDraft{{EventType: "quest.state_changed", Visibility: storage.CampaignVisibilityPlayer, Payload: storage.JSONMap{
				"quest_key": key, "name": definition.Name, "quest_type": row.QuestType, "from": from, "to": "blocked", "progress": progress,
			}}},
			Result: storage.JSONMap{"quest_key": key, "status": "blocked", "progress": progress},
		}, nil
	}
	if err := ConsumeQuestObjectives(execution, parsed); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := GrantQuestRewards(execution, parsed); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	now := execution.Campaign.GameTime
	row.Status = "completed"
	row.ResolvedAtGameTime = &now
	if err := execution.Store.SaveQuest(row); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := execution.Store.CancelScheduledEvents(execution.Campaign.ID, "quest_deadline", key); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	unlockedEvents, unlockedKeys, err := OfferUnlockedQuests(execution, parsed)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	events := []storage.CampaignEventDraft{{EventType: "quest.state_changed", Visibility: storage.CampaignVisibilityPlayer, Payload: storage.JSONMap{
		"quest_key": key, "name": definition.Name, "quest_type": row.QuestType, "from": from, "to": "completed", "progress": progress,
	}}}
	events = append(events, unlockedEvents...)
	result := storage.JSONMap{"quest_key": key, "status": "completed", "progress": progress}
	if len(unlockedKeys) > 0 {
		result["unlocked_quest_keys"] = unlockedKeys
	}
	outcome := storage.CampaignActionOutcome{Events: events, Result: result}
	if parsed.MilestoneKey != "" {
		outcome.Events = append(outcome.Events, storage.CampaignEventDraft{
			EventType: "campaign.milestone_reached", Visibility: storage.CampaignVisibilityPlayer,
			Payload: storage.JSONMap{"milestone_key": parsed.MilestoneKey, "quest_key": key, "game_time": execution.Campaign.GameTime},
		})
		result["milestone_key"] = parsed.MilestoneKey
	}
	if parsed.CampaignOutcomeKey != "" {
		rewards, err := execution.Store.GrantCampaignOutcomeRewards(ctx, execution.Campaign, parsed.CampaignOutcomeKey)
		if err != nil {
			return storage.CampaignActionOutcome{}, err
		}
		status := storage.CampaignStatusCompleted
		outcome.Status = &status
		outcome.OutcomeKey = &parsed.CampaignOutcomeKey
		result["campaign_status"] = status
		result["campaign_outcome_key"] = parsed.CampaignOutcomeKey
		result["account_rewards"] = rewards
		var points int64
		for _, reward := range rewards {
			points += reward.Quantity
		}
		result["reward_points"] = points
		completedPayload := storage.JSONMap{
			"outcome_key": parsed.CampaignOutcomeKey, "milestone_key": parsed.MilestoneKey,
			"quest_key": key, "account_rewards": rewards, "reward_points": points,
		}
		// client 路由不发放奖励：原因随结果与事件下发，前端据此展示替代文案。
		if execution.Campaign.LLMRoute == storage.LLMRouteClient {
			result["rewards_disabled_reason"] = storage.RewardsDisabledReasonClientRoute
			completedPayload["rewards_disabled_reason"] = storage.RewardsDisabledReasonClientRoute
		}
		outcome.Events = append(outcome.Events, storage.CampaignEventDraft{
			EventType: "campaign.completed", Visibility: storage.CampaignVisibilityPlayer,
			Payload: completedPayload,
		})
	}
	return outcome, nil
}

func OfferUnlockedQuests(execution campaign.ActionExecution, completed worldrules.Quest) ([]storage.CampaignEventDraft, []string, error) {
	events := make([]storage.CampaignEventDraft, 0, len(completed.UnlockQuestKeys))
	keys := make([]string, 0, len(completed.UnlockQuestKeys))
	for _, key := range completed.UnlockQuestKeys {
		if _, err := execution.Store.Quest(execution.Campaign.ID, key); err == nil {
			continue
		} else if !errors.Is(err, storage.ErrNotFound) {
			return nil, nil, err
		}
		definition, quest, err := QuestDefinition(execution, key)
		if err != nil {
			return nil, nil, err
		}
		if quest.Initial || quest.StartingStatus != "offered" {
			return nil, nil, Invalid("解锁任务必须是非初始 offered 任务: %s", key)
		}
		row := &storage.CampaignQuest{
			CampaignID: execution.Campaign.ID, QuestKey: key, QuestType: quest.QuestType,
			Status: "offered", OfferedAtGameTime: execution.Campaign.GameTime,
			Progress: storage.JSONMap{}, SourceSnapshot: storage.JSONMap{"key": key, "name": definition.Name}, UpdatedAt: time.Now(),
		}
		if err := execution.Store.SaveQuest(row); err != nil {
			return nil, nil, err
		}
		keys = append(keys, key)
		events = append(events, storage.CampaignEventDraft{EventType: "quest.offered", Visibility: storage.CampaignVisibilityPlayer, Payload: storage.JSONMap{
			"quest_key": key, "name": definition.Name, "quest_type": quest.QuestType, "status": "offered",
		}})
	}
	return events, keys, nil
}

func EvaluateQuestObjectives(execution campaign.ActionExecution, quest worldrules.Quest) (storage.JSONMap, bool, error) {
	entries := make([]any, 0, len(quest.Objectives))
	allSatisfied := true
	for _, objective := range quest.Objectives {
		current, target, satisfied, err := EvaluateQuestObjective(execution, objective)
		if err != nil {
			return nil, false, err
		}
		entries = append(entries, storage.JSONMap{
			"key": objective.Key, "type": objective.Type, "current": current, "target": target,
			"comparison": objective.Comparison, "satisfied": satisfied,
		})
		allSatisfied = allSatisfied && satisfied
	}
	return storage.JSONMap{"objectives": entries, "satisfied": allSatisfied}, allSatisfied, nil
}

func EvaluateQuestObjective(execution campaign.ActionExecution, objective worldrules.QuestObjective) (int64, int64, bool, error) {
	switch objective.Type {
	case "item":
		quality := objective.QualityKey
		if quality == "" {
			quality = "common"
		}
		stack, err := execution.Store.ItemStack(execution.Campaign.ID, objective.ItemKey, quality)
		if errors.Is(err, storage.ErrNotFound) {
			return 0, objective.Quantity, false, nil
		}
		if err != nil {
			return 0, 0, false, err
		}
		return stack.Quantity, objective.Quantity, stack.Quantity >= objective.Quantity, nil
	case "encounter_win":
		count, err := execution.Store.EncounterWinCount(execution.Campaign.ID, objective.EncounterKey)
		if err != nil {
			return 0, 0, false, err
		}
		return count, objective.Quantity, count >= objective.Quantity, nil
	case "location":
		current := int64(0)
		if execution.Campaign.CurrentLocationKey == objective.LocationKey {
			current = 1
		}
		return current, 1, current == 1, nil
	case "faction_reputation":
		standing, err := execution.Store.FactionStanding(execution.Campaign.ID, objective.FactionKey)
		if err != nil {
			return 0, 0, false, err
		}
		return int64(standing.Reputation), objective.Target, int64(standing.Reputation) >= objective.Target, nil
	case "npc_relationship":
		npc, err := execution.Store.NPC(execution.Campaign.ID, objective.NPCKey)
		if err != nil {
			return 0, 0, false, err
		}
		relation, err := execution.Store.Relationship(execution.Campaign.ID, npc.ID)
		if errors.Is(err, storage.ErrNotFound) {
			return 0, objective.Target, 0 >= objective.Target, nil
		}
		if err != nil {
			return 0, 0, false, err
		}
		current := RelationshipDimension(*relation, objective.Dimension)
		return current, objective.Target, current >= objective.Target, nil
	case "region_state":
		region, err := execution.Store.RegionState(execution.Campaign.ID, objective.RegionKey)
		if err != nil {
			return 0, 0, false, err
		}
		current := RegionStateField(*region, objective.RegionField)
		satisfied := current >= objective.Target
		if objective.Comparison == "at_most" {
			satisfied = current <= objective.Target
		}
		return current, objective.Target, satisfied, nil
	default:
		return 0, 0, false, Invalid("任务目标类型无效")
	}
}

func RelationshipDimension(relation storage.CampaignRelationship, dimension string) int64 {
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

func RegionStateField(region storage.CampaignRegionState, field string) int64 {
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

func ConsumeQuestObjectives(execution campaign.ActionExecution, quest worldrules.Quest) error {
	for _, objective := range quest.Objectives {
		if objective.Type != "item" || !objective.Consume {
			continue
		}
		quality := objective.QualityKey
		if quality == "" {
			quality = "common"
		}
		stack, err := execution.Store.ItemStack(execution.Campaign.ID, objective.ItemKey, quality)
		if err != nil || stack.Quantity < objective.Quantity {
			return Rejected("提交任务时物品不足: %s", objective.ItemKey)
		}
		stack.Quantity -= objective.Quantity
		stack.UpdatedAt = time.Now()
		if err := execution.Store.SaveItemStack(stack); err != nil {
			return err
		}
	}
	return nil
}

func GrantQuestRewards(execution campaign.ActionExecution, quest worldrules.Quest) error {
	for _, reward := range quest.Rewards {
		switch reward.Type {
		case "item":
			quality := reward.QualityKey
			if quality == "" {
				quality = "common"
			}
			stack, err := execution.Store.ItemStack(execution.Campaign.ID, reward.ItemKey, quality)
			if errors.Is(err, storage.ErrNotFound) {
				stack = &storage.CampaignItemStack{CampaignID: execution.Campaign.ID, ItemKey: reward.ItemKey, QualityKey: quality, Metadata: storage.JSONMap{}}
			} else if err != nil {
				return err
			}
			if stack.Quantity > math.MaxInt64-reward.Quantity {
				return Invalid("任务奖励数量溢出")
			}
			stack.Quantity += reward.Quantity
			stack.UpdatedAt = time.Now()
			if err := execution.Store.SaveItemStack(stack); err != nil {
				return err
			}
		case "faction_reputation", "faction_contribution":
			standing, err := execution.Store.FactionStanding(execution.Campaign.ID, reward.FactionKey)
			if err != nil {
				return err
			}
			faction, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionFaction, reward.FactionKey)
			if err != nil {
				return err
			}
			if reward.Type == "faction_reputation" {
				standing.Reputation = ClampInt(standing.Reputation+int(reward.Amount), factionrules.MinimumReputation, factionrules.MaximumReputation)
			} else {
				standing.Contribution = Clamp64(standing.Contribution+reward.Amount, 0, factionrules.MaximumContribution)
			}
			standing.PositionKey = factionrules.Position(faction.Content, standing.Contribution)
			standing.Permissions = storage.JSONMap(factionrules.DerivePermissions(faction.Content, standing.Reputation))
			standing.UpdatedAt = time.Now()
			if err := execution.Store.SaveFactionStanding(standing); err != nil {
				return err
			}
		}
	}
	return nil
}

func Clamp64(value, minimum, maximum int64) int64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func ClampInt(value, minimum, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func ProcessScheduledEvent(execution campaign.ActionExecution, scheduled *storage.CampaignScheduledEvent, gameTime int64) (storage.CampaignEventDraft, error) {
	switch scheduled.EventKind {
	case storage.DefinitionWorldEvent:
		return ProcessWorldEvent(execution, scheduled, gameTime)
	case "quest_deadline":
		return ProcessQuestDeadline(execution, scheduled, gameTime)
	default:
		return storage.CampaignEventDraft{}, Invalid("未知计划事件类型: %s", scheduled.EventKind)
	}
}

func ProcessWorldEvent(execution campaign.ActionExecution, scheduled *storage.CampaignScheduledEvent, gameTime int64) (storage.CampaignEventDraft, error) {
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionWorldEvent, scheduled.TemplateKey)
	if err != nil {
		return storage.CampaignEventDraft{}, err
	}
	event, ok := worldrules.ParseWorldEvent(map[string]any(definition.Content))
	if !ok {
		return storage.CampaignEventDraft{}, Invalid("世界事件定义无效: %s", scheduled.TemplateKey)
	}
	variant := DeterministicVariant(execution.Campaign.RandomSeed, scheduled.TemplateKey, scheduled.Occurrence, event.Variants)
	region, err := execution.Store.RegionState(execution.Campaign.ID, event.RegionKey)
	if err != nil {
		return storage.CampaignEventDraft{}, err
	}
	ApplyRegionEffects(region, variant.Effects)
	region.LastEventKey, region.LastEventVariant, region.LastEventSummary = scheduled.TemplateKey, variant.Key, variant.Summary
	region.LastEventGameTime = &gameTime
	region.UpdatedAt = time.Now()
	if err := execution.Store.SaveRegionState(region); err != nil {
		return storage.CampaignEventDraft{}, err
	}
	marketKeys := SortedIntMapKeys(variant.Effects.MarketPriceModifierDeltasBPS)
	for _, marketKey := range marketKeys {
		market, err := execution.Store.MarketState(execution.Campaign.ID, marketKey)
		if err != nil {
			return storage.CampaignEventDraft{}, err
		}
		market.PriceModifierBPS = ClampInt(market.PriceModifierBPS+int(variant.Effects.MarketPriceModifierDeltasBPS[marketKey]), 1000, 30000)
		market.UpdatedAt = time.Now()
		if err := execution.Store.SaveMarketState(market); err != nil {
			return storage.CampaignEventDraft{}, err
		}
	}
	npcKeys := make([]string, 0, len(variant.Effects.NPCGoalDeltas))
	for npcKey := range variant.Effects.NPCGoalDeltas {
		npcKeys = append(npcKeys, npcKey)
	}
	sort.Strings(npcKeys)
	for _, npcKey := range npcKeys {
		npc, err := execution.Store.NPC(execution.Campaign.ID, npcKey)
		if err != nil {
			return storage.CampaignEventDraft{}, err
		}
		if npc.Goals == nil {
			npc.Goals = storage.JSONMap{}
		}
		goalKeys := SortedIntMapKeys(variant.Effects.NPCGoalDeltas[npcKey])
		for _, goalKey := range goalKeys {
			current, _ := NumericInt64(npc.Goals[goalKey])
			npc.Goals[goalKey] = Clamp64(current+variant.Effects.NPCGoalDeltas[npcKey][goalKey], -1_000_000_000, 1_000_000_000)
		}
		npc.LastActiveAt = gameTime
		npc.UpdatedAt = time.Now()
		if err := execution.Store.SaveNPC(npc); err != nil {
			return storage.CampaignEventDraft{}, err
		}
	}
	processed := gameTime
	scheduled.Status, scheduled.ProcessedAtGameTime, scheduled.UpdatedAt = "processed", &processed, time.Now()
	if err := execution.Store.SaveScheduledEvent(scheduled); err != nil {
		return storage.CampaignEventDraft{}, err
	}
	if event.RepeatIntervalMinutes > 0 {
		if scheduled.DueGameTime > math.MaxInt64-event.RepeatIntervalMinutes || scheduled.Occurrence == math.MaxInt64 {
			return storage.CampaignEventDraft{}, Invalid("周期世界事件调度溢出")
		}
		if err := execution.Store.CreateScheduledEvent(&storage.CampaignScheduledEvent{
			ID: uuid.New(), CampaignID: execution.Campaign.ID, EventKind: storage.DefinitionWorldEvent,
			TemplateKey: scheduled.TemplateKey, DueGameTime: scheduled.DueGameTime + event.RepeatIntervalMinutes,
			Priority: event.Priority, Status: "pending", Occurrence: scheduled.Occurrence + 1,
			Scope: storage.JSONMap{"region_key": event.RegionKey}, Payload: storage.JSONMap{}, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}); err != nil {
			return storage.CampaignEventDraft{}, err
		}
	}
	return storage.CampaignEventDraft{EventType: "world.event_triggered", Visibility: storage.CampaignVisibilityPlayer, Payload: storage.JSONMap{
		"template_key": scheduled.TemplateKey, "name": definition.Name, "region_key": event.RegionKey,
		"variant_key": variant.Key, "summary": variant.Summary, "occurrence": scheduled.Occurrence,
		"region_state": PublicRegionState(*region),
	}}, nil
}

func DeterministicVariant(seed int64, templateKey string, occurrence int64, variants []worldrules.EventVariant) worldrules.EventVariant {
	hash := sha256.New()
	var buffer [8]byte
	binary.BigEndian.PutUint64(buffer[:], uint64(seed))
	hash.Write(buffer[:])
	hash.Write([]byte{0})
	hash.Write([]byte(templateKey))
	hash.Write([]byte{0})
	binary.BigEndian.PutUint64(buffer[:], uint64(occurrence))
	hash.Write(buffer[:])
	sum := hash.Sum(nil)
	roll := binary.BigEndian.Uint64(sum[:8])
	var total uint64
	for _, variant := range variants {
		total += uint64(variant.Weight)
	}
	roll %= total
	for _, variant := range variants {
		if roll < uint64(variant.Weight) {
			return variant
		}
		roll -= uint64(variant.Weight)
	}
	return variants[len(variants)-1]
}

func ApplyRegionEffects(region *storage.CampaignRegionState, effects worldrules.EventEffects) {
	region.ResourceLevel = Clamp64(region.ResourceLevel+effects.ResourceDelta, 0, 10000)
	region.DangerLevel = Clamp64(region.DangerLevel+effects.DangerDelta, 0, 10000)
	region.Stability = Clamp64(region.Stability+effects.StabilityDelta, 0, 10000)
	if effects.ConflictStage != nil {
		region.ConflictStage = *effects.ConflictStage
	}
	if region.FactionPower == nil {
		region.FactionPower = storage.JSONMap{}
	}
	for _, key := range SortedIntMapKeys(effects.FactionPowerDeltas) {
		current, _ := NumericInt64(region.FactionPower[key])
		region.FactionPower[key] = Clamp64(current+effects.FactionPowerDeltas[key], 0, 10000)
	}
}

func SortedIntMapKeys(values map[string]int64) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func NumericInt64(value any) (int64, bool) {
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

func PublicRegionState(region storage.CampaignRegionState) storage.JSONMap {
	return storage.JSONMap{
		"region_key": region.RegionKey, "resource_level": region.ResourceLevel, "danger_level": region.DangerLevel,
		"stability": region.Stability, "conflict_stage": region.ConflictStage,
	}
}

func ProcessQuestDeadline(execution campaign.ActionExecution, scheduled *storage.CampaignScheduledEvent, gameTime int64) (storage.CampaignEventDraft, error) {
	row, err := execution.Store.Quest(execution.Campaign.ID, scheduled.TemplateKey)
	if err != nil {
		return storage.CampaignEventDraft{}, err
	}
	definition, _, err := QuestDefinition(execution, scheduled.TemplateKey)
	if err != nil {
		return storage.CampaignEventDraft{}, err
	}
	from := row.Status
	to := row.Status
	if row.Status == "active" || row.Status == "blocked" {
		if row.QuestType == "obligation" {
			to = "failed"
		} else {
			to = "expired"
		}
		row.Status = to
		row.ResolvedAtGameTime = &gameTime
		row.UpdatedAt = time.Now()
		if err := execution.Store.SaveQuest(row); err != nil {
			return storage.CampaignEventDraft{}, err
		}
	}
	processed := gameTime
	scheduled.Status, scheduled.ProcessedAtGameTime, scheduled.UpdatedAt = "processed", &processed, time.Now()
	if err := execution.Store.SaveScheduledEvent(scheduled); err != nil {
		return storage.CampaignEventDraft{}, err
	}
	return storage.CampaignEventDraft{EventType: "quest.state_changed", Visibility: storage.CampaignVisibilityPlayer, Payload: storage.JSONMap{
		"quest_key": row.QuestKey, "name": definition.Name, "quest_type": row.QuestType,
		"from": from, "to": to, "deadline_processed": true,
	}}, nil
}

type tradeRequest struct {
	marketKey  string
	kind       string
	itemKey    string
	qualityKey string
	quantity   int64
	instanceID uuid.UUID
}
