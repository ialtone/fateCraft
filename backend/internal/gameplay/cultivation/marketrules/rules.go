package marketrules

import "strings"

const (
	MaxEntries = 128
	MaxStock   = int64(1000000000)
	MaxPrice   = int64(1000000000000)
)

type Entry struct {
	Kind             string
	Key              string
	QualityKey       string
	MaxStock         int64
	InitialStock     int64
	RestockQuantity  int64
	BaseBuyPrice     int64
	BaseSellPrice    int64
	PriceModifierBPS int64
}

type Market struct {
	LocationKey             string
	CurrencyItemKey         string
	FactionKey              string
	RestockIntervalMinutes  int64
	PriceModifierBPS        int64
	ScarcityMarkupBPS       int64
	ReputationAdjustmentBPS int64
	Entries                 []Entry
}

func Parse(raw any) (Market, bool) {
	content, ok := object(raw)
	if !ok || !onlyKeys(content, "location_key", "currency_item_key", "faction_key", "restock_interval_minutes", "price_modifier_bps", "scarcity_markup_bps", "reputation_adjustment_bps", "items") {
		return Market{}, false
	}
	market := Market{}
	market.LocationKey, _ = content["location_key"].(string)
	market.CurrencyItemKey, _ = content["currency_item_key"].(string)
	market.FactionKey, _ = content["faction_key"].(string)
	if !stableKey(market.LocationKey) || !stableKey(market.CurrencyItemKey) || market.FactionKey != "" && !stableKey(market.FactionKey) {
		return Market{}, false
	}
	if market.RestockIntervalMinutes, ok = boundedInt(content["restock_interval_minutes"], 1, 5256000); !ok {
		return Market{}, false
	}
	if market.PriceModifierBPS, ok = boundedIntDefault(content, "price_modifier_bps", 10000, 1000, 30000); !ok {
		return Market{}, false
	}
	if market.ScarcityMarkupBPS, ok = boundedIntDefault(content, "scarcity_markup_bps", 2500, 0, 10000); !ok {
		return Market{}, false
	}
	if market.ReputationAdjustmentBPS, ok = boundedIntDefault(content, "reputation_adjustment_bps", 1500, 0, 5000); !ok {
		return Market{}, false
	}
	rawEntries, ok := content["items"].([]any)
	if !ok || len(rawEntries) == 0 || len(rawEntries) > MaxEntries {
		return Market{}, false
	}
	seen := map[string]bool{}
	for _, rawEntry := range rawEntries {
		value, ok := object(rawEntry)
		if !ok || !onlyKeys(value, "kind", "key", "quality_key", "max_stock", "initial_stock", "restock_quantity", "base_buy_price", "base_sell_price", "price_modifier_bps") {
			return Market{}, false
		}
		entry := Entry{}
		entry.Kind, _ = value["kind"].(string)
		entry.Key, _ = value["key"].(string)
		entry.QualityKey, _ = value["quality_key"].(string)
		if entry.QualityKey == "" {
			entry.QualityKey = "common"
		}
		if !map[string]bool{"item": true, "equipment": true}[entry.Kind] || !stableKey(entry.Key) || !stableKey(entry.QualityKey) || entry.Key == market.CurrencyItemKey {
			return Market{}, false
		}
		identity := entry.Kind + ":" + entry.Key + ":" + entry.QualityKey
		if seen[identity] {
			return Market{}, false
		}
		seen[identity] = true
		if entry.MaxStock, ok = boundedInt(value["max_stock"], 1, MaxStock); !ok {
			return Market{}, false
		}
		if entry.InitialStock, ok = boundedInt(value["initial_stock"], 0, entry.MaxStock); !ok {
			return Market{}, false
		}
		if entry.RestockQuantity, ok = boundedInt(value["restock_quantity"], 0, entry.MaxStock); !ok {
			return Market{}, false
		}
		if entry.BaseBuyPrice, ok = boundedInt(value["base_buy_price"], 0, MaxPrice); !ok {
			return Market{}, false
		}
		if entry.BaseSellPrice, ok = boundedInt(value["base_sell_price"], 0, MaxPrice); !ok || entry.BaseBuyPrice == 0 && entry.BaseSellPrice == 0 {
			return Market{}, false
		}
		if entry.PriceModifierBPS, ok = boundedIntDefault(value, "price_modifier_bps", 10000, 1000, 30000); !ok {
			return Market{}, false
		}
		market.Entries = append(market.Entries, entry)
	}
	return market, true
}

func FindEntry(market Market, kind, key, qualityKey string) (Entry, bool) {
	for _, entry := range market.Entries {
		if entry.Kind == kind && entry.Key == key && entry.QualityKey == qualityKey {
			return entry, true
		}
	}
	return Entry{}, false
}

func Restock(quantity, lastRestock, gameTime int64, market Market, entry Entry) (int64, int64) {
	if gameTime <= lastRestock || market.RestockIntervalMinutes <= 0 {
		return clamp(quantity, 0, entry.MaxStock), lastRestock
	}
	intervals := (gameTime - lastRestock) / market.RestockIntervalMinutes
	if intervals <= 0 {
		return clamp(quantity, 0, entry.MaxStock), lastRestock
	}
	newLast := lastRestock + intervals*market.RestockIntervalMinutes
	if entry.RestockQuantity == 0 || quantity >= entry.MaxStock {
		return clamp(quantity, 0, entry.MaxStock), newLast
	}
	space := entry.MaxStock - quantity
	if intervals > space/entry.RestockQuantity {
		return entry.MaxStock, newLast
	}
	return quantity + intervals*entry.RestockQuantity, newLast
}

func UnitPrice(market Market, entry Entry, stock, reputation int64, buying bool) (int64, bool) {
	base := entry.BaseSellPrice
	if buying {
		base = entry.BaseBuyPrice
	}
	if base <= 0 {
		return 0, false
	}
	stock = clamp(stock, 0, entry.MaxStock)
	scarcityBPS := int64(0)
	// 稀缺加价只作用于买价：整批交易按交易前库存统一定价时，
	// 若卖价也吃稀缺加价，高声望下会在满库存买入、空库存卖出之间形成稳定价差，
	// 导致零时间无限套利。卖价只吃声望与市场/条目系数。
	if buying && entry.MaxStock > 0 {
		scarcityBPS = (entry.MaxStock - stock) * market.ScarcityMarkupBPS / entry.MaxStock
	}
	reputation = clamp(reputation, -10000, 10000)
	reputationBPS := reputation * market.ReputationAdjustmentBPS / 10000
	tradeModifier := int64(10000) + scarcityBPS
	if buying {
		tradeModifier -= reputationBPS
	} else {
		tradeModifier += reputationBPS
	}
	tradeModifier = clamp(tradeModifier, 1000, 30000)
	price := multiplyBPS(base, market.PriceModifierBPS)
	price = multiplyBPS(price, entry.PriceModifierBPS)
	price = multiplyBPS(price, tradeModifier)
	return clamp(price, 1, MaxPrice), true
}

// NoArbitrage 按最坏情况检查市场是否存在零时间套利：声望 +10000、
// 买入时满库存、卖出时零库存。此时要求每条目的卖价不高于买价，
// 否则玩家可反复低买高卖无限刷局内货币。交易本身不推进游戏时间，
// 价格机制必须保证该不等式成立。注意允许等于：整数截断会让 1 灵石的
// 低价物品买卖同为 1，等价往返零利润，不构成套利。
func NoArbitrage(market Market) bool {
	for _, entry := range market.Entries {
		buy, okBuy := UnitPrice(market, entry, entry.MaxStock, 10000, true)
		sell, okSell := UnitPrice(market, entry, 0, 10000, false)
		if !okBuy || !okSell || sell > buy {
			return false
		}
	}
	return true
}

func multiplyBPS(value, bps int64) int64 {
	if value <= 0 || bps <= 0 {
		return 0
	}
	if value > MaxPrice*10000/bps {
		return MaxPrice
	}
	return value * bps / 10000
}

func object(raw any) (map[string]any, bool) {
	value, ok := raw.(map[string]any)
	return value, ok
}

func exactInt(raw any) (int64, bool) {
	switch value := raw.(type) {
	case int:
		return int64(value), true
	case int64:
		return value, true
	case float64:
		return int64(value), value == float64(int64(value))
	default:
		return 0, false
	}
}

func boundedInt(raw any, minimum, maximum int64) (int64, bool) {
	value, ok := exactInt(raw)
	return value, ok && value >= minimum && value <= maximum
}

func boundedIntDefault(content map[string]any, key string, fallback, minimum, maximum int64) (int64, bool) {
	raw, exists := content[key]
	if !exists {
		return fallback, true
	}
	return boundedInt(raw, minimum, maximum)
}

func stableKey(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || len(value) > 128 {
		return false
	}
	for index, char := range value {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || index > 0 && (char == '.' || char == '_' || char == '-') {
			continue
		}
		return false
	}
	return true
}

func onlyKeys(value map[string]any, allowed ...string) bool {
	set := map[string]bool{}
	for _, key := range allowed {
		set[key] = true
	}
	for key := range value {
		if !set[key] {
			return false
		}
	}
	return true
}

func clamp(value, minimum, maximum int64) int64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
