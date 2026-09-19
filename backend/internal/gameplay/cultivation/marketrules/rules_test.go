package marketrules

import "testing"

func TestRestockAndPricesUseGameTimeStockAndReputation(t *testing.T) {
	market, ok := Parse(map[string]any{
		"location_key": "market", "currency_item_key": "stone", "faction_key": "guild",
		"restock_interval_minutes": int64(60), "scarcity_markup_bps": int64(4000),
		"reputation_adjustment_bps": int64(2000),
		"items": []any{map[string]any{
			"kind": "item", "key": "herb", "quality_key": "common",
			"max_stock": int64(10), "initial_stock": int64(2), "restock_quantity": int64(3),
			"base_buy_price": int64(100), "base_sell_price": int64(50),
		}},
	})
	if !ok {
		t.Fatal("valid market rejected")
	}
	entry := market.Entries[0]
	quantity, last := Restock(2, 0, 130, market, entry)
	if quantity != 8 || last != 120 {
		t.Fatalf("restock = %d at %d", quantity, last)
	}
	neutralBuy, _ := UnitPrice(market, entry, 2, 0, true)
	trustedBuy, _ := UnitPrice(market, entry, 2, 10000, true)
	neutralSell, _ := UnitPrice(market, entry, 2, 0, false)
	trustedSell, _ := UnitPrice(market, entry, 2, 10000, false)
	if trustedBuy >= neutralBuy || trustedSell <= neutralSell {
		t.Fatalf("reputation did not improve prices: buy %d/%d sell %d/%d", trustedBuy, neutralBuy, trustedSell, neutralSell)
	}
}
