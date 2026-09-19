package effectrules

import "math"

const (
	MaxEffectEntries = 16
	MaxLayerEffects  = 100
	MaxMagnitude     = 10000
)

var supported = map[string]bool{
	"max_health": true, "max_mana": true, "max_spiritual_sense": true,
	"physical_attack": true, "physical_defense": true,
	"spiritual_attack": true, "spiritual_defense": true,
	"movement": true, "cultivation_gain": true, "mana_cost_reduction": true,
	"healing_power": true,
}

func ParseEffects(raw any) (map[string]int64, bool) {
	value, ok := raw.(map[string]any)
	if !ok || len(value) > MaxEffectEntries {
		return nil, false
	}
	effects := make(map[string]int64, len(value))
	for key, rawAmount := range value {
		amount, valid := integer(rawAmount)
		if !supported[key] || !valid || amount < -MaxMagnitude || amount > MaxMagnitude {
			return nil, false
		}
		effects[key] = amount
	}
	return effects, true
}

func EquipmentBonuses(content map[string]any) (map[string]int64, bool) {
	raw, exists := content["bonuses"]
	if !exists {
		return map[string]int64{}, true
	}
	return ParseEffects(raw)
}

func TechniqueEffects(content map[string]any, learnedLayer int) (map[string]int64, bool) {
	raw, exists := content["layer_effects"]
	if !exists {
		return map[string]int64{}, true
	}
	layers, ok := raw.([]any)
	if !ok || len(layers) > MaxLayerEffects {
		return nil, false
	}
	out := map[string]int64{}
	seen := map[int64]bool{}
	for _, rawLayer := range layers {
		layer, ok := rawLayer.(map[string]any)
		if !ok || len(layer) != 2 {
			return nil, false
		}
		number, numberOK := integer(layer["layer"])
		effects, effectsOK := ParseEffects(layer["effects"])
		if !numberOK || number < 1 || number > MaxLayerEffects || seen[number] || !effectsOK {
			return nil, false
		}
		seen[number] = true
		if number <= int64(learnedLayer) {
			Add(out, effects)
		}
	}
	return out, true
}

func Add(target, source map[string]int64) {
	for key, amount := range source {
		current := target[key]
		switch {
		case amount > 0 && current > math.MaxInt64-amount:
			target[key] = math.MaxInt64
		case amount < 0 && current < math.MinInt64-amount:
			target[key] = math.MinInt64
		default:
			target[key] = current + amount
		}
	}
}

func integer(value any) (int64, bool) {
	switch number := value.(type) {
	case int:
		return int64(number), true
	case int64:
		return number, true
	case float64:
		converted := int64(number)
		return converted, number == float64(converted)
	default:
		return 0, false
	}
}
