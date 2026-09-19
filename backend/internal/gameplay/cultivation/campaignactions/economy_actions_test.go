package campaignactions

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/storage"
)

func TestLearnRecipeConsumesSourceOnce(t *testing.T) {
	store := newActionStore()
	campaignID := uuid.New()
	store.definitions[storage.DefinitionRecipe+":token"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionRecipe, Key: "token", Name: "护符炼法",
		Content: storage.JSONMap{
			"craft_type": "artifact", "source_item_key": "recipe-note",
			"inputs":        []any{map[string]any{"quantity": int64(1), "options": []any{map[string]any{"item_key": "ore"}}}},
			"craft_minutes": int64(60), "success_chance_bps": int64(10000), "failure_input_loss_bps": int64(10000),
			"outputs": []any{map[string]any{"kind": "item", "key": "token", "quantity": int64(1), "quality_weights": map[string]any{"common": int64(1)}}},
		},
	}
	store.stacks[stackKey("recipe-note", "common")] = &storage.CampaignItemStack{
		CampaignID: campaignID, ItemKey: "recipe-note", QualityKey: "common", Quantity: 1,
	}
	execution := campaign.ActionExecution{Campaign: storage.Campaign{ID: campaignID}, Store: store, ActorType: "player"}
	if _, err := learnRecipe(context.Background(), execution, map[string]any{"recipe_key": "token"}); err != nil {
		t.Fatal(err)
	}
	if store.recipes["token"] == nil || store.stacks[stackKey("recipe-note", "common")] != nil {
		t.Fatalf("recipe learning state = recipes:%#v stack:%#v", store.recipes, store.stacks)
	}
	if _, err := learnRecipe(context.Background(), execution, map[string]any{"recipe_key": "token"}); !errors.Is(err, campaign.ErrActionRejected) {
		t.Fatalf("duplicate learning error = %v", err)
	}
}

func TestCraftSuccessConsumesMaterialsAndCreatesQualityProduct(t *testing.T) {
	store := newActionStore()
	campaignID := uuid.New()
	store.character = &storage.CampaignCharacter{
		CampaignID: campaignID, RealmRank: 0, Mana: 10, MaxMana: 10, Health: 20, MaxHealth: 20, LifespanDays: 1000,
	}
	store.recipes["pill"] = &storage.CampaignKnownRecipe{CampaignID: campaignID, RecipeKey: "pill"}
	store.techniques["alchemy"] = &storage.CampaignTechnique{TechniqueKey: "alchemy", Status: "active", Mastery: 5000}
	toolID := uuid.New()
	store.instances[toolID] = &storage.CampaignItemInstance{ID: toolID, DefinitionKey: "furnace", State: "stored", Durability: 10}
	store.stacks[stackKey("herb", "common")] = &storage.CampaignItemStack{CampaignID: campaignID, ItemKey: "herb", QualityKey: "common", Quantity: 3}
	store.definitions[storage.DefinitionRecipe+":pill"] = recipeDefinition("pill", 10000, 5000)
	store.definitions[storage.DefinitionItem+":pill"] = stackableDefinition("pill", "丹药")
	execution := campaign.ActionExecution{
		Campaign: storage.Campaign{ID: campaignID, ChallengeVersionID: uuid.New(), CurrentLocationKey: "workshop", RandomSeed: 7},
		Store:    store, ActorType: "player", ActionID: "craft-success",
	}
	outcome, err := executeRecipe(context.Background(), execution, map[string]any{"recipe_key": "pill"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Result["success"] != true || store.stacks[stackKey("herb", "common")].Quantity != 1 || store.character.Mana != 8 {
		t.Fatalf("craft outcome=%#v herb=%#v character=%#v", outcome, store.stacks, store.character)
	}
	product := store.stacks[stackKey("pill", "fine")]
	if product == nil || product.Quantity != 2 {
		t.Fatalf("quality product = %#v", product)
	}
}

func TestCraftFailureConsumesConfiguredLossAndAppliesFailureOutcome(t *testing.T) {
	store := newActionStore()
	campaignID := uuid.New()
	store.character = &storage.CampaignCharacter{
		CampaignID: campaignID, Mana: 10, MaxMana: 10, Health: 20, MaxHealth: 20,
		LifespanDays: 1000, Injuries: storage.JSONMap{}, LongTermEffects: storage.JSONMap{},
	}
	store.recipes["pill"] = &storage.CampaignKnownRecipe{CampaignID: campaignID, RecipeKey: "pill"}
	store.techniques["alchemy"] = &storage.CampaignTechnique{TechniqueKey: "alchemy", Status: "active"}
	toolID := uuid.New()
	store.instances[toolID] = &storage.CampaignItemInstance{ID: toolID, DefinitionKey: "furnace", State: "stored", Durability: 10}
	store.stacks[stackKey("herb", "common")] = &storage.CampaignItemStack{CampaignID: campaignID, ItemKey: "herb", QualityKey: "common", Quantity: 3}
	definition := recipeDefinition("pill", 0, 5000)
	definition.Content["failure_outputs"] = []any{map[string]any{
		"kind": "item", "key": "dregs", "quantity": int64(1), "quality_weights": map[string]any{"common": int64(1)},
	}}
	definition.Content["failure_effect"] = map[string]any{
		"health_damage": int64(4), "category": "poison", "effect_key": "toxin",
		"severity": int64(10), "duration_minutes": int64(60), "description": "丹毒",
	}
	store.definitions[storage.DefinitionRecipe+":pill"] = definition
	store.definitions[storage.DefinitionItem+":pill"] = stackableDefinition("pill", "丹药")
	store.definitions[storage.DefinitionItem+":dregs"] = stackableDefinition("dregs", "药渣")
	execution := campaign.ActionExecution{
		Campaign: storage.Campaign{ID: campaignID, CurrentLocationKey: "workshop", RandomSeed: 9},
		Store:    store, ActorType: "player", ActionID: "craft-failure",
	}
	outcome, err := executeRecipe(context.Background(), execution, map[string]any{"recipe_key": "pill"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Result["success"] != false || store.stacks[stackKey("herb", "common")].Quantity != 2 {
		t.Fatalf("failure did not consume configured loss: outcome=%#v stacks=%#v", outcome, store.stacks)
	}
	if store.stacks[stackKey("dregs", "common")].Quantity != 1 || store.character.Health != 16 || store.character.LongTermEffects["toxin"] == nil {
		t.Fatalf("failure result missing: stacks=%#v character=%#v", store.stacks, store.character)
	}
}

func TestCraftUsesAuthoritativeCampaignRoll(t *testing.T) {
	store := newActionStore()
	campaignID := uuid.New()
	store.character = &storage.CampaignCharacter{CampaignID: campaignID, Mana: 10, MaxMana: 10, Health: 20, MaxHealth: 20, LifespanDays: 1000}
	store.recipes["pill"] = &storage.CampaignKnownRecipe{CampaignID: campaignID, RecipeKey: "pill"}
	store.techniques["alchemy"] = &storage.CampaignTechnique{TechniqueKey: "alchemy", Status: "active", Mastery: 10_000}
	toolID := uuid.New()
	store.instances[toolID] = &storage.CampaignItemInstance{ID: toolID, DefinitionKey: "furnace", State: "stored", Durability: 10}
	store.stacks[stackKey("herb", "common")] = &storage.CampaignItemStack{CampaignID: campaignID, ItemKey: "herb", QualityKey: "common", Quantity: 3}
	store.definitions[storage.DefinitionRecipe+":pill"] = recipeDefinition("pill", 10_000, 5_000)
	store.definitions[storage.DefinitionItem+":pill"] = stackableDefinition("pill", "丹药")
	execution := campaign.ActionExecution{
		Campaign: storage.Campaign{ID: campaignID, CurrentLocationKey: "workshop"}, Store: store, ActorType: "player",
		ActionID: "forced-craft", Roll: forcedCampaignRoll(false, "failure", 98, 99),
	}
	outcome, err := executeRecipe(context.Background(), execution, map[string]any{"recipe_key": "pill"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Result["success"] != false || outcome.Roll == nil || store.stacks[stackKey("pill", "fine")] != nil {
		t.Fatalf("authoritative roll did not decide craft: outcome=%+v stacks=%+v", outcome, store.stacks)
	}
}

func TestTradeRestocksThenBuysAndSellsAtomicallyInStore(t *testing.T) {
	store := newActionStore()
	campaignID := uuid.New()
	marketKey := "guild-market"
	store.definitions[storage.DefinitionMarket+":"+marketKey] = marketDefinition(marketKey)
	store.definitions[storage.DefinitionItem+":stone"] = stackableDefinition("stone", "灵石")
	store.definitions[storage.DefinitionItem+":herb"] = stackableDefinition("herb", "药材")
	store.markets[marketKey] = &storage.CampaignMarketState{
		CampaignID: campaignID, MarketKey: marketKey, LocationKey: "market", PriceModifierBPS: 10000,
	}
	stockKey := marketStockTestKey(marketKey, "item", "herb", "common")
	store.marketStocks[stockKey] = &storage.CampaignMarketStock{
		CampaignID: campaignID, MarketKey: marketKey, ItemKind: "item", ItemKey: "herb", QualityKey: "common", Quantity: 0,
	}
	store.stacks[stackKey("stone", "common")] = &storage.CampaignItemStack{
		CampaignID: campaignID, ItemKey: "stone", QualityKey: "common", Quantity: 100,
	}
	execution := campaign.ActionExecution{
		Campaign: storage.Campaign{ID: campaignID, CurrentLocationKey: "market", GameTime: 60},
		Store:    store, ActorType: "player", ActionID: "trade-buy",
	}
	arguments := map[string]any{
		"market_key": marketKey, "item_kind": "item", "item_key": "herb", "quality_key": "common", "quantity": int64(1),
	}
	if _, err := tradeBuy(context.Background(), execution, arguments); err != nil {
		t.Fatal(err)
	}
	if store.marketStocks[stockKey].Quantity != 1 || store.stacks[stackKey("herb", "common")].Quantity != 1 {
		t.Fatalf("buy state = stock:%#v stacks:%#v", store.marketStocks[stockKey], store.stacks)
	}
	execution.ActionID = "trade-sell"
	if _, err := tradeSell(context.Background(), execution, arguments); err != nil {
		t.Fatal(err)
	}
	if store.marketStocks[stockKey].Quantity != 2 || store.stacks[stackKey("herb", "common")] != nil {
		t.Fatalf("sell state = stock:%#v stacks:%#v", store.marketStocks[stockKey], store.stacks)
	}
}

func TestCraftMissingMaterialsDoesNotConsumeResources(t *testing.T) {
	store := newActionStore()
	campaignID := uuid.New()
	store.character = &storage.CampaignCharacter{
		CampaignID: campaignID, Mana: 10, MaxMana: 10, Health: 20, MaxHealth: 20, LifespanDays: 1000,
	}
	store.recipes["pill"] = &storage.CampaignKnownRecipe{CampaignID: campaignID, RecipeKey: "pill"}
	store.techniques["alchemy"] = &storage.CampaignTechnique{TechniqueKey: "alchemy", Status: "active"}
	toolID := uuid.New()
	store.instances[toolID] = &storage.CampaignItemInstance{ID: toolID, DefinitionKey: "furnace", State: "stored", Durability: 10}
	store.stacks[stackKey("herb", "common")] = &storage.CampaignItemStack{CampaignID: campaignID, ItemKey: "herb", QualityKey: "common", Quantity: 1}
	store.definitions[storage.DefinitionRecipe+":pill"] = recipeDefinition("pill", 10000, 10000)
	store.definitions[storage.DefinitionItem+":pill"] = stackableDefinition("pill", "丹药")

	_, err := executeRecipe(context.Background(), campaign.ActionExecution{
		Campaign: storage.Campaign{ID: campaignID, CurrentLocationKey: "workshop"}, Store: store, ActorType: "player",
	}, map[string]any{"recipe_key": "pill"})
	if !errors.Is(err, campaign.ErrActionRejected) {
		t.Fatalf("missing material error = %v", err)
	}
	if store.character.Mana != 10 || store.stacks[stackKey("herb", "common")].Quantity != 1 || store.stacks[stackKey("pill", "fine")] != nil {
		t.Fatalf("rejected craft mutated resources: character=%#v stacks=%#v", store.character, store.stacks)
	}
}

func TestTradeRejectsWrongLocationAndFullMarketWithoutMutation(t *testing.T) {
	store := newActionStore()
	campaignID := uuid.New()
	marketKey := "guild-market"
	store.definitions[storage.DefinitionMarket+":"+marketKey] = marketDefinition(marketKey)
	store.definitions[storage.DefinitionItem+":stone"] = stackableDefinition("stone", "灵石")
	store.definitions[storage.DefinitionItem+":herb"] = stackableDefinition("herb", "药材")
	store.markets[marketKey] = &storage.CampaignMarketState{
		CampaignID: campaignID, MarketKey: marketKey, LocationKey: "market", PriceModifierBPS: 10000,
	}
	stockKey := marketStockTestKey(marketKey, "item", "herb", "common")
	store.marketStocks[stockKey] = &storage.CampaignMarketStock{
		CampaignID: campaignID, MarketKey: marketKey, ItemKind: "item", ItemKey: "herb", QualityKey: "common", Quantity: 10,
	}
	store.stacks[stackKey("stone", "common")] = &storage.CampaignItemStack{CampaignID: campaignID, ItemKey: "stone", QualityKey: "common", Quantity: 100}
	store.stacks[stackKey("herb", "common")] = &storage.CampaignItemStack{CampaignID: campaignID, ItemKey: "herb", QualityKey: "common", Quantity: 1}
	arguments := map[string]any{"market_key": marketKey, "item_kind": "item", "item_key": "herb", "quality_key": "common", "quantity": int64(1)}

	wrongLocation := campaign.ActionExecution{Campaign: storage.Campaign{ID: campaignID, CurrentLocationKey: "village"}, Store: store, ActorType: "player"}
	if _, err := tradeBuy(context.Background(), wrongLocation, arguments); !errors.Is(err, campaign.ErrActionRejected) {
		t.Fatalf("wrong-location trade error = %v", err)
	}
	if store.marketStocks[stockKey].Quantity != 10 || store.stacks[stackKey("stone", "common")].Quantity != 100 {
		t.Fatalf("wrong-location trade mutated state: stock=%#v stacks=%#v", store.marketStocks[stockKey], store.stacks)
	}

	fullMarket := campaign.ActionExecution{Campaign: storage.Campaign{ID: campaignID, CurrentLocationKey: "market"}, Store: store, ActorType: "player"}
	if _, err := tradeSell(context.Background(), fullMarket, arguments); !errors.Is(err, campaign.ErrActionRejected) {
		t.Fatalf("full-market trade error = %v", err)
	}
	if store.marketStocks[stockKey].Quantity != 10 || store.stacks[stackKey("herb", "common")].Quantity != 1 {
		t.Fatalf("full-market rejection mutated state: stock=%#v stacks=%#v", store.marketStocks[stockKey], store.stacks)
	}
}

func TestTradeBuysAndSellsEquipmentInstances(t *testing.T) {
	store := newActionStore()
	campaignID := uuid.New()
	marketKey := "artifact-market"
	store.definitions[storage.DefinitionMarket+":"+marketKey] = equipmentMarketDefinition(marketKey)
	store.definitions[storage.DefinitionItem+":stone"] = stackableDefinition("stone", "灵石")
	store.definitions[storage.DefinitionEquipment+":token"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionEquipment, Key: "token", Name: "护符",
		Content: storage.JSONMap{"allowed_slots": []any{"accessory"}, "durability_max": int64(80), "bonuses": map[string]any{}},
	}
	store.markets[marketKey] = &storage.CampaignMarketState{
		CampaignID: campaignID, MarketKey: marketKey, LocationKey: "market", PriceModifierBPS: 10000,
	}
	stockKey := marketStockTestKey(marketKey, "equipment", "token", "common")
	store.marketStocks[stockKey] = &storage.CampaignMarketStock{
		CampaignID: campaignID, MarketKey: marketKey, ItemKind: "equipment", ItemKey: "token", QualityKey: "common", Quantity: 2,
	}
	store.stacks[stackKey("stone", "common")] = &storage.CampaignItemStack{CampaignID: campaignID, ItemKey: "stone", QualityKey: "common", Quantity: 100}
	execution := campaign.ActionExecution{Campaign: storage.Campaign{ID: campaignID, CurrentLocationKey: "market"}, Store: store, ActorType: "player"}
	arguments := map[string]any{"market_key": marketKey, "item_kind": "equipment", "item_key": "token", "quality_key": "common", "quantity": int64(1)}

	if _, err := tradeBuy(context.Background(), execution, arguments); err != nil {
		t.Fatal(err)
	}
	var purchased *storage.CampaignItemInstance
	for _, instance := range store.instances {
		purchased = instance
	}
	if purchased == nil || purchased.State != "stored" || purchased.Durability != 80 || store.marketStocks[stockKey].Quantity != 1 || store.stacks[stackKey("stone", "common")].Quantity != 80 {
		t.Fatalf("equipment purchase state: instance=%#v stock=%#v currency=%#v", purchased, store.marketStocks[stockKey], store.stacks[stackKey("stone", "common")])
	}
	arguments["item_instance_id"] = purchased.ID.String()
	if _, err := tradeSell(context.Background(), execution, arguments); err != nil {
		t.Fatal(err)
	}
	if purchased.State != "consumed" || store.marketStocks[stockKey].Quantity != 2 || store.stacks[stackKey("stone", "common")].Quantity != 90 {
		t.Fatalf("equipment sale state: instance=%#v stock=%#v currency=%#v", purchased, store.marketStocks[stockKey], store.stacks[stackKey("stone", "common")])
	}
}

func recipeDefinition(key string, successBPS, failureLossBPS int64) *storage.GameplayDefinition {
	return &storage.GameplayDefinition{
		Kind: storage.DefinitionRecipe, Key: key, Name: "丹方",
		Content: storage.JSONMap{
			"craft_type": "alchemy", "starting_known": true, "allowed_location_keys": []any{"workshop"},
			"inputs": []any{map[string]any{
				"quantity": int64(2), "options": []any{map[string]any{"item_key": "herb", "quality_key": "common"}},
			}},
			"required_equipment_keys": []any{"furnace"}, "technique_key": "alchemy",
			"mana_cost": int64(2), "craft_minutes": int64(60), "success_chance_bps": successBPS,
			"failure_input_loss_bps": failureLossBPS,
			"outputs": []any{map[string]any{
				"kind": "item", "key": "pill", "quantity": int64(2), "quality_weights": map[string]any{"fine": int64(1)},
			}},
		},
	}
}

func marketDefinition(key string) *storage.GameplayDefinition {
	return &storage.GameplayDefinition{
		Kind: storage.DefinitionMarket, Key: key, Name: "商行",
		Content: storage.JSONMap{
			"location_key": "market", "currency_item_key": "stone", "restock_interval_minutes": int64(60),
			"scarcity_markup_bps": int64(0), "reputation_adjustment_bps": int64(0),
			"items": []any{map[string]any{
				"kind": "item", "key": "herb", "quality_key": "common", "max_stock": int64(10),
				"initial_stock": int64(0), "restock_quantity": int64(2), "base_buy_price": int64(5), "base_sell_price": int64(3),
			}},
		},
	}
}

func equipmentMarketDefinition(key string) *storage.GameplayDefinition {
	return &storage.GameplayDefinition{
		Kind: storage.DefinitionMarket, Key: key, Name: "法器行",
		Content: storage.JSONMap{
			"location_key": "market", "currency_item_key": "stone", "restock_interval_minutes": int64(60),
			"scarcity_markup_bps": int64(0), "reputation_adjustment_bps": int64(0),
			"items": []any{map[string]any{
				"kind": "equipment", "key": "token", "quality_key": "common", "max_stock": int64(4),
				"initial_stock": int64(2), "restock_quantity": int64(1), "base_buy_price": int64(20), "base_sell_price": int64(10),
			}},
		},
	}
}

func stackableDefinition(key, name string) *storage.GameplayDefinition {
	return &storage.GameplayDefinition{Kind: storage.DefinitionItem, Key: key, Name: name, Content: storage.JSONMap{"stackable": true}}
}

func marketStockTestKey(market, kind, item, quality string) string {
	return market + ":" + marketStockIdentity(kind, item, quality)
}
