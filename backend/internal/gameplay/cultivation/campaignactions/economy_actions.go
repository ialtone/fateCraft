package campaignactions

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/craftingrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/marketrules"
	"github.com/ialtone/fatecraft/internal/storage"
)

type selectedIngredient struct {
	stack    *storage.CampaignItemStack
	required int64
}

func learnRecipe(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := allowArguments(arguments, "recipe_key"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	key, err := requiredKey(arguments, "recipe_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionRecipe, key)
	if err != nil {
		return storage.CampaignActionOutcome{}, rejected("未知配方 %s", key)
	}
	recipe, ok := craftingrules.Parse(map[string]any(definition.Content))
	if !ok {
		return storage.CampaignActionOutcome{}, invalid("配方定义无效: %s", key)
	}
	if _, err := execution.Store.KnownRecipe(execution.Campaign.ID, key); err == nil {
		return storage.CampaignActionOutcome{}, rejected("已经掌握配方 %s", key)
	} else if !errors.Is(err, storage.ErrNotFound) {
		return storage.CampaignActionOutcome{}, err
	}
	if recipe.StartingKnown || recipe.SourceItemKey == "" {
		return storage.CampaignActionOutcome{}, rejected("配方 %s 不能通过物品学习", key)
	}
	stack, err := execution.Store.ItemStack(execution.Campaign.ID, recipe.SourceItemKey, "common")
	if err != nil || stack.Quantity < 1 {
		return storage.CampaignActionOutcome{}, rejected("缺少配方载体 %s", recipe.SourceItemKey)
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
	return singleEvent("craft.recipe_learned", storage.JSONMap{
		"recipe_key": key, "name": definition.Name, "source_item_key": recipe.SourceItemKey,
	}), nil
}

func executeRecipe(ctx context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := allowArguments(arguments, "recipe_key"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	key, err := requiredKey(arguments, "recipe_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionRecipe, key)
	if err != nil {
		return storage.CampaignActionOutcome{}, rejected("未知配方 %s", key)
	}
	recipe, ok := craftingrules.Parse(map[string]any(definition.Content))
	if !ok {
		return storage.CampaignActionOutcome{}, invalid("配方定义无效: %s", key)
	}
	if _, err := execution.Store.KnownRecipe(execution.Campaign.ID, key); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return storage.CampaignActionOutcome{}, rejected("尚未掌握配方 %s", key)
		}
		return storage.CampaignActionOutcome{}, err
	}
	if len(recipe.AllowedLocationKeys) > 0 && !containsActionString(recipe.AllowedLocationKeys, execution.Campaign.CurrentLocationKey) {
		return storage.CampaignActionOutcome{}, rejected("当前位置不能执行配方 %s", key)
	}
	character, err := execution.Store.Character(execution.Campaign.ID)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if int64(character.RealmRank) < recipe.MinRealmRank {
		return storage.CampaignActionOutcome{}, rejected("境界不足，配方需要境界 rank %d", recipe.MinRealmRank)
	}
	if character.Mana < recipe.ManaCost {
		return storage.CampaignActionOutcome{}, rejected("法力不足，需要 %d", recipe.ManaCost)
	}
	mastery := int64(0)
	if recipe.TechniqueKey != "" {
		technique, techniqueErr := execution.Store.Technique(execution.Campaign.ID, recipe.TechniqueKey)
		if techniqueErr != nil || technique.Status != "active" || technique.Mastery < recipe.MinTechniqueMastery {
			return storage.CampaignActionOutcome{}, rejected("生产技艺 %s 未掌握或熟练度不足", recipe.TechniqueKey)
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
		insight = clampInt64(mastery/100, 0, 100)
	}
	rollContext := campaignRollContext(character)
	if len(recipe.RequiredEquipmentKeys) > 0 {
		rollContext.Advantages = append(rollContext.Advantages, "充分准备")
	}
	rollResult, err := resolveCampaignRoll(ctx, execution, campaign.CampaignRollRequest{
		Key: "craft." + key, Title: "炼制判定", Description: "炼制" + definition.Name, Category: "crafting",
		BaseSuccessRate: campaignRollBaseFromBPS(chance), Sides: 100, RelevantAttributes: []string{"悟性", "慧根"},
		AttributeWeights: map[string]float64{"悟性": 0.7, "慧根": 0.3},
		PlayerState:      campaignRollPlayerState(character, insight), Context: rollContext,
	})
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	roll := int64(deterministicUint64(execution, "craft.success:"+key)%10000) + 1
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
	attachCampaignRoll(&outcome, rollResult)
	if !success {
		applyCraftFailureEffect(execution, character, recipe.FailureEffect, &outcome)
	}
	character.UpdatedAt = time.Now()
	if err := execution.Store.SaveCharacter(character); err != nil {
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

func tradeBuy(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := allowArguments(arguments, "market_key", "item_kind", "item_key", "quality_key", "quantity"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	request, err := parseTradeRequest(arguments, false)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	market, state, stocks, entry, err := prepareMarket(execution, request.marketKey, request.kind, request.itemKey, request.qualityKey)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	stock := stocks[marketStockIdentity(entry.Kind, entry.Key, entry.QualityKey)]
	if stock == nil || stock.Quantity < request.quantity {
		return storage.CampaignActionOutcome{}, rejected("市场库存不足")
	}
	unitPrice, ok := marketUnitPrice(execution, market, state, entry, stock.Quantity, true)
	if !ok || unitPrice > math.MaxInt64/request.quantity {
		return storage.CampaignActionOutcome{}, rejected("商品当前不可购买或价格溢出")
	}
	total := unitPrice * request.quantity
	currency, err := execution.Store.ItemStack(execution.Campaign.ID, market.CurrencyItemKey, "common")
	if err != nil || currency.Quantity < total {
		return storage.CampaignActionOutcome{}, rejected("%s 数量不足，需要 %d", market.CurrencyItemKey, total)
	}
	currency.Quantity -= total
	currency.UpdatedAt = time.Now()
	if err := execution.Store.SaveItemStack(currency); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := grantTradePurchase(execution, request, entry); err != nil {
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
	return singleEvent("trade.bought", payload), nil
}

func tradeSell(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := allowArguments(arguments, "market_key", "item_kind", "item_key", "quality_key", "quantity", "item_instance_id"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	request, err := parseTradeRequest(arguments, true)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	market, state, stocks, entry, err := prepareMarket(execution, request.marketKey, request.kind, request.itemKey, request.qualityKey)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	stock := stocks[marketStockIdentity(entry.Kind, entry.Key, entry.QualityKey)]
	if stock == nil || stock.Quantity > entry.MaxStock-request.quantity {
		return storage.CampaignActionOutcome{}, rejected("市场收购容量不足")
	}
	unitPrice, ok := marketUnitPrice(execution, market, state, entry, stock.Quantity, false)
	if !ok || unitPrice > math.MaxInt64/request.quantity {
		return storage.CampaignActionOutcome{}, rejected("商品当前不可出售或价格溢出")
	}
	if err := consumeTradeSale(execution, request); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	total := unitPrice * request.quantity
	if err := addItemStack(execution, market.CurrencyItemKey, "common", total, "trade.sell"); err != nil {
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
	return singleEvent("trade.sold", payload), nil
}

func adjustMarketPrice(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if execution.ActorType != "system" {
		return storage.CampaignActionOutcome{}, rejected("市场价格修正只能由系统执行")
	}
	if err := allowArguments(arguments, "market_key", "price_modifier_bps"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	marketKey, err := requiredKey(arguments, "market_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	modifier, err := requiredInt(arguments, "price_modifier_bps", 1000, 30000)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	state, err := execution.Store.MarketState(execution.Campaign.ID, marketKey)
	if err != nil {
		return storage.CampaignActionOutcome{}, rejected("未知市场 %s", marketKey)
	}
	previous := state.PriceModifierBPS
	state.PriceModifierBPS = int(modifier)
	state.UpdatedAt = time.Now()
	if err := execution.Store.SaveMarketState(state); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return singleEvent("market.price_modifier_changed", storage.JSONMap{
		"market_key": marketKey, "previous_bps": previous, "price_modifier_bps": modifier,
	}), nil
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
			return rejected("缺少可用生产器具 %s", key)
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
			return nil, rejected("制作材料不足")
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
			return nil, rejected("制作材料数量发生冲突")
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
		quality, ok := craftingrules.ChooseQuality(product.QualityWeights, deterministicUint64(execution, fmt.Sprintf("craft.quality:%s:%d", recipeKey, index)))
		if !ok {
			return nil, invalid("配方产物品质定义无效")
		}
		if product.Kind == "item" {
			if err := addItemStack(execution, product.Key, quality, product.Quantity, "craft:"+recipeKey); err != nil {
				return nil, err
			}
		} else {
			for unit := int64(0); unit < product.Quantity; unit++ {
				if err := createEquipmentInstance(execution, product.Key, quality, "craft:"+recipeKey); err != nil {
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
	character.Health = clampInt64(character.Health-effect.HealthDamage, 0, character.MaxHealth)
	character.Mana = clampInt64(character.Mana-effect.ManaDamage, 0, character.MaxMana)
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

type tradeRequest struct {
	marketKey  string
	kind       string
	itemKey    string
	qualityKey string
	quantity   int64
	instanceID uuid.UUID
}

func parseTradeRequest(arguments map[string]any, selling bool) (tradeRequest, error) {
	request := tradeRequest{}
	var err error
	if request.marketKey, err = requiredKey(arguments, "market_key"); err != nil {
		return request, err
	}
	request.kind, _ = arguments["item_kind"].(string)
	if request.kind != "item" && request.kind != "equipment" {
		return request, invalid("item_kind 必须是 item 或 equipment")
	}
	if request.itemKey, err = requiredKey(arguments, "item_key"); err != nil {
		return request, err
	}
	if request.qualityKey, err = optionalKey(arguments, "quality_key", "common"); err != nil {
		return request, err
	}
	maximum := int64(1000)
	if request.kind == "equipment" {
		maximum = 20
	}
	if request.quantity, err = optionalInt(arguments, "quantity", 1, 1, maximum); err != nil {
		return request, err
	}
	if request.kind == "equipment" && selling {
		if request.quantity != 1 {
			return request, invalid("出售装备实例时 quantity 必须为 1")
		}
		if request.instanceID, err = requiredUUID(arguments, "item_instance_id"); err != nil {
			return request, err
		}
	} else if _, exists := arguments["item_instance_id"]; exists {
		return request, invalid("item_instance_id 只用于出售装备实例")
	}
	return request, nil
}

func prepareMarket(execution campaign.ActionExecution, marketKey, kind, itemKey, qualityKey string) (marketrules.Market, *storage.CampaignMarketState, map[string]*storage.CampaignMarketStock, marketrules.Entry, error) {
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionMarket, marketKey)
	if err != nil {
		return marketrules.Market{}, nil, nil, marketrules.Entry{}, rejected("未知市场 %s", marketKey)
	}
	market, ok := marketrules.Parse(map[string]any(definition.Content))
	if !ok {
		return marketrules.Market{}, nil, nil, marketrules.Entry{}, invalid("市场定义无效: %s", marketKey)
	}
	if market.LocationKey != execution.Campaign.CurrentLocationKey {
		return marketrules.Market{}, nil, nil, marketrules.Entry{}, rejected("市场不在当前位置")
	}
	entry, ok := marketrules.FindEntry(market, kind, itemKey, qualityKey)
	if !ok {
		return marketrules.Market{}, nil, nil, marketrules.Entry{}, rejected("市场未经营该商品")
	}
	state, stocks, err := refreshMarket(execution, marketKey, market)
	return market, state, stocks, entry, err
}

func refreshMarket(execution campaign.ActionExecution, marketKey string, market marketrules.Market) (*storage.CampaignMarketState, map[string]*storage.CampaignMarketStock, error) {
	state, err := execution.Store.MarketState(execution.Campaign.ID, marketKey)
	if err != nil {
		return nil, nil, invalid("市场动态状态不存在: %s", marketKey)
	}
	stocks := map[string]*storage.CampaignMarketStock{}
	newLast := state.LastRestockGameTime
	for _, entry := range market.Entries {
		stock, err := execution.Store.MarketStock(execution.Campaign.ID, marketKey, entry.Kind, entry.Key, entry.QualityKey)
		if err != nil {
			return nil, nil, invalid("市场库存状态缺失: %s", entry.Key)
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
		stocks[marketStockIdentity(entry.Kind, entry.Key, entry.QualityKey)] = stock
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

func marketUnitPrice(execution campaign.ActionExecution, market marketrules.Market, state *storage.CampaignMarketState, entry marketrules.Entry, stock int64, buying bool) (int64, bool) {
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

func grantTradePurchase(execution campaign.ActionExecution, request tradeRequest, entry marketrules.Entry) error {
	if request.kind == "item" {
		return addItemStack(execution, request.itemKey, request.qualityKey, request.quantity, "trade.buy")
	}
	for count := int64(0); count < request.quantity; count++ {
		if err := createEquipmentInstance(execution, entry.Key, entry.QualityKey, "trade.buy"); err != nil {
			return err
		}
	}
	return nil
}

func consumeTradeSale(execution campaign.ActionExecution, request tradeRequest) error {
	if request.kind == "item" {
		stack, err := execution.Store.ItemStack(execution.Campaign.ID, request.itemKey, request.qualityKey)
		if err != nil || stack.Quantity < request.quantity {
			return rejected("出售物品数量不足")
		}
		stack.Quantity -= request.quantity
		stack.UpdatedAt = time.Now()
		return execution.Store.SaveItemStack(stack)
	}
	instance, err := execution.Store.ItemInstance(execution.Campaign.ID, request.instanceID)
	if err != nil || instance.State != "stored" || instance.DefinitionKey != request.itemKey || instance.QualityKey != request.qualityKey {
		return rejected("装备实例不存在、已装备或与商品不匹配")
	}
	instance.State = "consumed"
	instance.EquippedSlot = nil
	instance.UpdatedAt = time.Now()
	return execution.Store.SaveItemInstance(instance)
}

func addItemStack(execution campaign.ActionExecution, itemKey, qualityKey string, quantity int64, source string) error {
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionItem, itemKey)
	if err != nil || definition.Content["stackable"] != true {
		return invalid("产物或交易物品不是已发布的可堆叠物品: %s", itemKey)
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
		return invalid("物品数量无效或溢出")
	}
	stack.Quantity += quantity
	stack.UpdatedAt = time.Now()
	return execution.Store.SaveItemStack(stack)
}

func createEquipmentInstance(execution campaign.ActionExecution, definitionKey, qualityKey, source string) error {
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionEquipment, definitionKey)
	if err != nil {
		return invalid("未知装备产物 %s", definitionKey)
	}
	durability := definitionIntDefault(definition.Content["durability_max"], 0)
	if durability <= 0 || durability > math.MaxInt32 {
		return invalid("装备产物耐久定义无效: %s", definitionKey)
	}
	now := time.Now()
	return execution.Store.CreateItemInstance(&storage.CampaignItemInstance{
		ID: uuid.New(), CampaignID: execution.Campaign.ID, DefinitionKey: definitionKey,
		QualityKey: qualityKey, State: "stored", Durability: int(durability), MaxDurability: int(durability),
		Properties: storage.JSONMap{"name": definition.Name, "bonuses": definition.Content["bonuses"]},
		Provenance: storage.JSONMap{"source": source}, CreatedAt: now, UpdatedAt: now,
	})
}

func marketStockIdentity(kind, key, quality string) string {
	return kind + ":" + key + ":" + quality
}

func containsActionString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
