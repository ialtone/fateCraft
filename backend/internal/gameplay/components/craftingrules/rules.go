package craftingrules

import (
	"sort"
	"strings"
)

const (
	MaxIngredients = 16
	MaxOptions     = 8
	MaxProducts    = 8
	MaxQuantity    = int64(1000000)
)

type Material struct {
	ItemKey    string
	QualityKey string
}

type Ingredient struct {
	Quantity int64
	Options  []Material
}

type Product struct {
	Kind           string
	Key            string
	Quantity       int64
	QualityWeights map[string]int64
}

type FailureEffect struct {
	HealthDamage    int64
	ManaDamage      int64
	Category        string
	EffectKey       string
	Severity        int64
	DurationMinutes int64
	Description     string
}

type Recipe struct {
	CraftType             string
	StartingKnown         bool
	SourceItemKey         string
	AllowedLocationKeys   []string
	Inputs                []Ingredient
	RequiredEquipmentKeys []string
	TechniqueKey          string
	MinTechniqueMastery   int64
	MinRealmRank          int64
	ManaCost              int64
	CraftMinutes          int64
	SuccessChanceBPS      int64
	MasteryBonusBPS       int64
	FailureInputLossBPS   int64
	Outputs               []Product
	FailureOutputs        []Product
	FailureEffect         FailureEffect
}

func Parse(raw any) (Recipe, bool) {
	content, ok := object(raw)
	if !ok || !onlyKeys(content,
		"craft_type", "starting_known", "source_item_key", "allowed_location_keys", "inputs",
		"required_equipment_keys", "technique_key", "min_technique_mastery", "min_realm_rank",
		"mana_cost", "craft_minutes", "success_chance_bps", "mastery_bonus_bps",
		"failure_input_loss_bps", "outputs", "failure_outputs", "failure_effect",
	) {
		return Recipe{}, false
	}
	recipe := Recipe{}
	recipe.CraftType, _ = content["craft_type"].(string)
	if !map[string]bool{"alchemy": true, "artifact": true, "talisman": true}[recipe.CraftType] {
		return Recipe{}, false
	}
	if rawStarting, exists := content["starting_known"]; exists {
		recipe.StartingKnown, ok = rawStarting.(bool)
		if !ok {
			return Recipe{}, false
		}
	}
	recipe.SourceItemKey, _ = content["source_item_key"].(string)
	if recipe.SourceItemKey != "" && !stableKey(recipe.SourceItemKey) {
		return Recipe{}, false
	}
	if recipe.StartingKnown && recipe.SourceItemKey != "" {
		return Recipe{}, false
	}
	if rawLocations, exists := content["allowed_location_keys"]; exists {
		recipe.AllowedLocationKeys, ok = stableKeys(rawLocations, 32)
		if !ok || len(recipe.AllowedLocationKeys) == 0 {
			return Recipe{}, false
		}
	}
	recipe.RequiredEquipmentKeys, ok = optionalStableKeys(content, "required_equipment_keys", 8)
	if !ok {
		return Recipe{}, false
	}
	recipe.TechniqueKey, _ = content["technique_key"].(string)
	if recipe.TechniqueKey != "" && !stableKey(recipe.TechniqueKey) {
		return Recipe{}, false
	}
	if recipe.MinTechniqueMastery, ok = boundedIntDefault(content, "min_technique_mastery", 0, 0, 10000); !ok {
		return Recipe{}, false
	}
	if recipe.MinTechniqueMastery > 0 && recipe.TechniqueKey == "" {
		return Recipe{}, false
	}
	if recipe.MinRealmRank, ok = boundedIntDefault(content, "min_realm_rank", 0, 0, 1000); !ok {
		return Recipe{}, false
	}
	if recipe.ManaCost, ok = boundedIntDefault(content, "mana_cost", 0, 0, 1000000); !ok {
		return Recipe{}, false
	}
	if recipe.CraftMinutes, ok = boundedInt(content["craft_minutes"], 1, 5256000); !ok {
		return Recipe{}, false
	}
	if recipe.SuccessChanceBPS, ok = boundedInt(content["success_chance_bps"], 0, 10000); !ok {
		return Recipe{}, false
	}
	if recipe.MasteryBonusBPS, ok = boundedIntDefault(content, "mastery_bonus_bps", 0, 0, 10000); !ok {
		return Recipe{}, false
	}
	if recipe.MasteryBonusBPS > 0 && recipe.TechniqueKey == "" {
		return Recipe{}, false
	}
	if recipe.FailureInputLossBPS, ok = boundedInt(content["failure_input_loss_bps"], 1, 10000); !ok {
		return Recipe{}, false
	}
	if recipe.Inputs, ok = parseIngredients(content["inputs"]); !ok {
		return Recipe{}, false
	}
	if recipe.Outputs, ok = parseProducts(content["outputs"], false); !ok {
		return Recipe{}, false
	}
	if rawFailure, exists := content["failure_outputs"]; exists {
		if recipe.FailureOutputs, ok = parseProducts(rawFailure, true); !ok {
			return Recipe{}, false
		}
	}
	if rawEffect, exists := content["failure_effect"]; exists {
		if recipe.FailureEffect, ok = parseFailureEffect(rawEffect); !ok {
			return Recipe{}, false
		}
	}
	return recipe, true
}

func SuccessChance(recipe Recipe, mastery int64) int64 {
	mastery = clamp(mastery, 0, 10000)
	return clamp(recipe.SuccessChanceBPS+mastery*recipe.MasteryBonusBPS/10000, 0, 10000)
}

func ChooseQuality(weights map[string]int64, roll uint64) (string, bool) {
	keys := make([]string, 0, len(weights))
	var total uint64
	for key, weight := range weights {
		if !stableKey(key) || weight <= 0 {
			return "", false
		}
		keys = append(keys, key)
		total += uint64(weight)
	}
	if total == 0 {
		return "", false
	}
	sort.Strings(keys)
	target := roll % total
	var cumulative uint64
	for _, key := range keys {
		cumulative += uint64(weights[key])
		if target < cumulative {
			return key, true
		}
	}
	return "", false
}

func parseIngredients(raw any) ([]Ingredient, bool) {
	values, ok := raw.([]any)
	if !ok || len(values) == 0 || len(values) > MaxIngredients {
		return nil, false
	}
	out := make([]Ingredient, 0, len(values))
	for _, rawIngredient := range values {
		value, ok := object(rawIngredient)
		if !ok || !onlyKeys(value, "quantity", "options") {
			return nil, false
		}
		quantity, ok := boundedInt(value["quantity"], 1, MaxQuantity)
		if !ok {
			return nil, false
		}
		rawOptions, ok := value["options"].([]any)
		if !ok || len(rawOptions) == 0 || len(rawOptions) > MaxOptions {
			return nil, false
		}
		options := make([]Material, 0, len(rawOptions))
		seen := map[string]bool{}
		for _, rawOption := range rawOptions {
			option, ok := object(rawOption)
			if !ok || !onlyKeys(option, "item_key", "quality_key") {
				return nil, false
			}
			itemKey, _ := option["item_key"].(string)
			qualityKey, _ := option["quality_key"].(string)
			if qualityKey == "" {
				qualityKey = "common"
			}
			identity := itemKey + ":" + qualityKey
			if !stableKey(itemKey) || !stableKey(qualityKey) || seen[identity] {
				return nil, false
			}
			seen[identity] = true
			options = append(options, Material{ItemKey: itemKey, QualityKey: qualityKey})
		}
		out = append(out, Ingredient{Quantity: quantity, Options: options})
	}
	return out, true
}

func parseProducts(raw any, allowEmpty bool) ([]Product, bool) {
	values, ok := raw.([]any)
	if !ok || len(values) > MaxProducts || !allowEmpty && len(values) == 0 {
		return nil, false
	}
	out := make([]Product, 0, len(values))
	for _, rawProduct := range values {
		value, ok := object(rawProduct)
		if !ok || !onlyKeys(value, "kind", "key", "quantity", "quality_weights") {
			return nil, false
		}
		kind, _ := value["kind"].(string)
		key, _ := value["key"].(string)
		quantity, quantityOK := boundedInt(value["quantity"], 1, MaxQuantity)
		weights, weightsOK := parseWeights(value["quality_weights"])
		if !map[string]bool{"item": true, "equipment": true}[kind] || !stableKey(key) || !quantityOK || !weightsOK {
			return nil, false
		}
		if kind == "equipment" && quantity > 20 {
			return nil, false
		}
		out = append(out, Product{Kind: kind, Key: key, Quantity: quantity, QualityWeights: weights})
	}
	return out, true
}

func parseWeights(raw any) (map[string]int64, bool) {
	value, ok := object(raw)
	if !ok || len(value) == 0 || len(value) > 16 {
		return nil, false
	}
	out := make(map[string]int64, len(value))
	var total int64
	for key, rawWeight := range value {
		weight, valid := boundedInt(rawWeight, 1, 1000000)
		if !stableKey(key) || !valid || total > 1000000-weight {
			return nil, false
		}
		total += weight
		out[key] = weight
	}
	return out, total > 0
}

func parseFailureEffect(raw any) (FailureEffect, bool) {
	value, ok := object(raw)
	if !ok || !onlyKeys(value, "health_damage", "mana_damage", "category", "effect_key", "severity", "duration_minutes", "description") {
		return FailureEffect{}, false
	}
	effect := FailureEffect{}
	if effect.HealthDamage, ok = boundedIntDefault(value, "health_damage", 0, 0, 1000000); !ok {
		return FailureEffect{}, false
	}
	if effect.ManaDamage, ok = boundedIntDefault(value, "mana_damage", 0, 0, 1000000); !ok {
		return FailureEffect{}, false
	}
	effect.Category, _ = value["category"].(string)
	effect.EffectKey, _ = value["effect_key"].(string)
	if (effect.Category == "") != (effect.EffectKey == "") {
		return FailureEffect{}, false
	}
	if effect.EffectKey != "" {
		if !map[string]bool{"injury": true, "poison": true, "inner_demon": true, "long_term": true}[effect.Category] || !stableKey(effect.EffectKey) {
			return FailureEffect{}, false
		}
		if effect.Severity, ok = boundedInt(value["severity"], 1, 100); !ok {
			return FailureEffect{}, false
		}
		if effect.DurationMinutes, ok = boundedIntDefault(value, "duration_minutes", 0, 0, 5256000); !ok {
			return FailureEffect{}, false
		}
		effect.Description, _ = value["description"].(string)
		if len([]rune(effect.Description)) > 256 {
			return FailureEffect{}, false
		}
	} else if _, severityExists := value["severity"]; severityExists {
		return FailureEffect{}, false
	}
	return effect, true
}

func object(raw any) (map[string]any, bool) {
	switch value := raw.(type) {
	case map[string]any:
		return value, true
	default:
		return nil, false
	}
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

func optionalStableKeys(content map[string]any, key string, maximum int) ([]string, bool) {
	raw, exists := content[key]
	if !exists {
		return []string{}, true
	}
	return stableKeys(raw, maximum)
}

func stableKeys(raw any, maximum int) ([]string, bool) {
	values, ok := raw.([]any)
	if !ok || len(values) > maximum {
		return nil, false
	}
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, rawValue := range values {
		value, ok := rawValue.(string)
		if !ok || !stableKey(value) || seen[value] {
			return nil, false
		}
		seen[value] = true
		out = append(out, value)
	}
	return out, true
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
