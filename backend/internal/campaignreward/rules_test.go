package campaignreward

import "testing"

func TestItemStackRuleCalculatesBoundedExtraction(t *testing.T) {
	rule, err := ParseItemStackRule(map[string]any{
		"item_key": "law-fragment", "quality_key": "rare", "source_quantity": int64(3),
		"reward_quantity": int64(2), "maximum_reward_quantity": int64(6), "consume": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	reward, consumed, err := CalculateItemStack(20, rule)
	if err != nil || reward != 6 || consumed != 9 {
		t.Fatalf("extraction = reward %d consumed %d err %v", reward, consumed, err)
	}
}

func TestItemStackRuleCanValueWithoutConsumption(t *testing.T) {
	rule, err := ParseItemStackRule(map[string]any{
		"item_key": "proof", "source_quantity": float64(2), "reward_quantity": float64(1),
		"maximum_reward_quantity": float64(4), "consume": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	reward, consumed, err := CalculateItemStack(5, rule)
	if err != nil || reward != 2 || consumed != 0 || rule.QualityKey != "common" {
		t.Fatalf("extraction = reward %d consumed %d quality %q err %v", reward, consumed, rule.QualityKey, err)
	}
}

func TestItemStackRuleSkipsQuantityBelowOneBatch(t *testing.T) {
	rule, err := ParseItemStackRule(map[string]any{
		"item_key": "proof", "source_quantity": int64(3), "reward_quantity": int64(2),
		"maximum_reward_quantity": int64(6), "consume": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	reward, consumed, err := CalculateItemStack(2, rule)
	if err != nil || reward != 0 || consumed != 0 {
		t.Fatalf("below-batch extraction = reward %d consumed %d err %v", reward, consumed, err)
	}
}

func TestItemStackRuleRejectsUnboundedOrAmbiguousConfiguration(t *testing.T) {
	base := map[string]any{
		"item_key": "proof", "source_quantity": int64(1), "reward_quantity": int64(2),
		"maximum_reward_quantity": int64(4), "consume": true,
	}
	tests := []func(map[string]any){
		func(config map[string]any) { delete(config, "maximum_reward_quantity") },
		func(config map[string]any) { config["maximum_reward_quantity"] = int64(3) },
		func(config map[string]any) { config["consume"] = "true" },
		func(config map[string]any) { config["unexpected"] = true },
	}
	for index, mutate := range tests {
		config := map[string]any{}
		for key, value := range base {
			config[key] = value
		}
		mutate(config)
		if _, err := ParseItemStackRule(config); err == nil {
			t.Fatalf("invalid config %d was accepted", index)
		}
	}
}
