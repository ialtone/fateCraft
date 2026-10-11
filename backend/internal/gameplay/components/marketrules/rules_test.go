package marketrules

import "testing"

func TestTurnoverRestoresSupplyAndBuyingCapacityWithoutFreeReadRefresh(t *testing.T) {
	target := int64(6)
	m := Market{RestockIntervalMinutes: 60}
	e := Entry{MaxStock: 10, RestockQuantity: 2, RestockTargetStock: &target}
	for _, initial := range []int64{0, 3, 6, 9, 10} {
		all, last := Restock(initial, 0, 245, m, e)
		q, at := initial, int64(0)
		for _, now := range []int64{30, 60, 75, 121, 200, 245} {
			q, at = Restock(q, at, now, m, e)
		}
		if q != target || all != q || at != last || last != 240 {
			t.Fatalf("partition mismatch: %d -> %d/%d at %d/%d", initial, all, q, last, at)
		}
	}
	q, at := Restock(10, 0, 59, m, e)
	if q != 10 || at != 0 {
		t.Fatal("read before interval created buying capacity")
	}
	q, at = Restock(10, 0, 60, m, e)
	if q != 8 || at != 60 {
		t.Fatal("full stock never consumed")
	}
	q, _ = Restock(q, at, 60, m, e)
	if q != 8 {
		t.Fatal("repeated read consumed stock")
	}
}

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
