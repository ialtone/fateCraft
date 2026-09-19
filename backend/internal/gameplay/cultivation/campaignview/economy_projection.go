package campaignview

import (
	"sort"

	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/craftingrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/marketrules"
	"github.com/ialtone/fatecraft/internal/storage"
)

func recipeViews(row storage.Campaign, state *storage.CampaignDomainState, limit int) []map[string]any {
	if state == nil {
		return []map[string]any{}
	}
	out := make([]map[string]any, 0, len(state.KnownRecipes))
	knownKeys := map[string]bool{}
	for _, known := range state.KnownRecipes {
		knownKeys[known.RecipeKey] = true
		definition, ok := state.Definition(storage.DefinitionRecipe, known.RecipeKey)
		if !ok {
			continue
		}
		recipe, valid := craftingrules.Parse(map[string]any(definition.Content))
		if !valid {
			continue
		}
		available, missing, mastery := recipeAvailability(row, state, recipe)
		out = append(out, map[string]any{
			"key": definition.Key, "name": definition.Name, "craft_type": recipe.CraftType,
			"known": true, "learnable": false,
			"craft_minutes": recipe.CraftMinutes, "mana_cost": recipe.ManaCost,
			"success_chance_bps": craftingrules.SuccessChance(recipe, mastery),
			"available":          available, "missing_requirements": missing,
			"inputs":          recipeInputViews(state, recipe.Inputs),
			"outputs":         recipeProductViews(state, recipe.Outputs),
			"failure_outputs": recipeProductViews(state, recipe.FailureOutputs),
		})
		if limit > 0 && len(out) >= limit {
			return out
		}
	}
	ownedItems := map[string]bool{}
	for _, stack := range state.ItemStacks {
		if stack.Quantity > 0 {
			ownedItems[stack.ItemKey] = true
		}
	}
	learnable := make([]storage.GameplayDefinition, 0)
	for _, definition := range state.Definitions {
		if definition.Kind != storage.DefinitionRecipe || knownKeys[definition.Key] {
			continue
		}
		recipe, valid := craftingrules.Parse(map[string]any(definition.Content))
		if valid && recipe.SourceItemKey != "" && ownedItems[recipe.SourceItemKey] {
			learnable = append(learnable, definition)
		}
	}
	sort.Slice(learnable, func(left, right int) bool { return learnable[left].Key < learnable[right].Key })
	for _, definition := range learnable {
		recipe, _ := craftingrules.Parse(map[string]any(definition.Content))
		out = append(out, map[string]any{
			"key": definition.Key, "name": definition.Name, "craft_type": recipe.CraftType,
			"known": false, "learnable": true, "source_item_key": recipe.SourceItemKey,
			"source_item_name": definitionLabel(state, storage.DefinitionItem, recipe.SourceItemKey),
			"craft_minutes":    recipe.CraftMinutes, "mana_cost": recipe.ManaCost,
			"success_chance_bps": craftingrules.SuccessChance(recipe, 0),
			"available":          false, "missing_requirements": []string{"recipe_knowledge"},
			"inputs": []map[string]any{}, "outputs": []map[string]any{}, "failure_outputs": []map[string]any{},
		})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func recipeAvailability(row storage.Campaign, state *storage.CampaignDomainState, recipe craftingrules.Recipe) (bool, []string, int64) {
	missing := make([]string, 0)
	if len(recipe.AllowedLocationKeys) > 0 && !projectionContains(recipe.AllowedLocationKeys, row.CurrentLocationKey) {
		missing = append(missing, "location")
	}
	if state.Character == nil || int64(state.Character.RealmRank) < recipe.MinRealmRank {
		missing = append(missing, "realm")
	}
	if state.Character == nil || state.Character.Mana < recipe.ManaCost {
		missing = append(missing, "mana")
	}
	mastery := int64(0)
	if recipe.TechniqueKey != "" {
		found := false
		for _, technique := range state.Techniques {
			if technique.TechniqueKey == recipe.TechniqueKey && technique.Status == "active" {
				mastery = technique.Mastery
				found = mastery >= recipe.MinTechniqueMastery
				break
			}
		}
		if !found {
			missing = append(missing, "technique:"+recipe.TechniqueKey)
		}
	}
	for _, required := range recipe.RequiredEquipmentKeys {
		found := false
		for _, instance := range state.ItemInstances {
			if instance.DefinitionKey == required && instance.Durability > 0 && (instance.State == "stored" || instance.State == "equipped") {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, "equipment:"+required)
		}
	}
	available := map[string]int64{}
	for _, stack := range state.ItemStacks {
		available[stack.ItemKey+":"+stack.QualityKey] = stack.Quantity
	}
	for _, ingredient := range recipe.Inputs {
		selected := ""
		for _, option := range ingredient.Options {
			identity := option.ItemKey + ":" + option.QualityKey
			if available[identity] >= ingredient.Quantity {
				selected = identity
				available[identity] -= ingredient.Quantity
				break
			}
		}
		if selected == "" {
			missing = append(missing, "materials")
			break
		}
	}
	return len(missing) == 0, missing, mastery
}

func recipeInputViews(state *storage.CampaignDomainState, inputs []craftingrules.Ingredient) []map[string]any {
	out := make([]map[string]any, 0, len(inputs))
	for _, input := range inputs {
		options := make([]map[string]any, 0, len(input.Options))
		for _, option := range input.Options {
			options = append(options, map[string]any{
				"item_key": option.ItemKey, "name": definitionLabel(state, storage.DefinitionItem, option.ItemKey),
				"quality_key": option.QualityKey,
			})
		}
		out = append(out, map[string]any{"quantity": input.Quantity, "options": options})
	}
	return out
}

func recipeProductViews(state *storage.CampaignDomainState, products []craftingrules.Product) []map[string]any {
	out := make([]map[string]any, 0, len(products))
	for _, product := range products {
		out = append(out, map[string]any{
			"kind": product.Kind, "key": product.Key, "name": definitionLabel(state, product.Kind, product.Key),
			"quantity": product.Quantity, "quality_weights": product.QualityWeights,
		})
	}
	return out
}

func marketViews(row storage.Campaign, state *storage.CampaignDomainState, itemLimit int) []map[string]any {
	if state == nil {
		return []map[string]any{}
	}
	stocks := map[string]storage.CampaignMarketStock{}
	for _, stock := range state.MarketStocks {
		stocks[marketProjectionIdentity(stock.MarketKey, stock.ItemKind, stock.ItemKey, stock.QualityKey)] = stock
	}
	standings := map[string]int64{}
	for _, standing := range state.Factions {
		standings[standing.FactionKey] = int64(standing.Reputation)
	}
	out := make([]map[string]any, 0)
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
		market.PriceModifierBPS = market.PriceModifierBPS * int64(marketState.PriceModifierBPS) / 10000
		items := make([]map[string]any, 0, len(market.Entries))
		effectiveRestockGameTime := marketState.LastRestockGameTime
		for _, entry := range market.Entries {
			stock, exists := stocks[marketProjectionIdentity(marketState.MarketKey, entry.Kind, entry.Key, entry.QualityKey)]
			if !exists {
				continue
			}
			quantity, restockedAt := marketrules.Restock(stock.Quantity, marketState.LastRestockGameTime, row.GameTime, market, entry)
			if restockedAt > effectiveRestockGameTime {
				effectiveRestockGameTime = restockedAt
			}
			buyPrice, buyable := marketrules.UnitPrice(market, entry, quantity, standings[market.FactionKey], true)
			sellPrice, sellable := marketrules.UnitPrice(market, entry, quantity, standings[market.FactionKey], false)
			items = append(items, map[string]any{
				"kind": entry.Kind, "key": entry.Key, "name": definitionLabel(state, entry.Kind, entry.Key),
				"quality_key": entry.QualityKey, "stock": quantity, "max_stock": entry.MaxStock,
				"buyable": buyable, "buy_price": buyPrice, "sellable": sellable, "sell_price": sellPrice,
			})
			if itemLimit > 0 && len(items) >= itemLimit {
				break
			}
		}
		sort.Slice(items, func(left, right int) bool {
			return items[left]["key"].(string) < items[right]["key"].(string)
		})
		out = append(out, map[string]any{
			"key": definition.Key, "name": definition.Name, "location_key": market.LocationKey,
			"currency_item_key":      market.CurrencyItemKey,
			"currency_name":          definitionLabel(state, storage.DefinitionItem, market.CurrencyItemKey),
			"last_restock_game_time": effectiveRestockGameTime,
			"items":                  items,
		})
	}
	return out
}

func projectionContains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func marketProjectionIdentity(marketKey, kind, key, quality string) string {
	return marketKey + ":" + kind + ":" + key + ":" + quality
}
