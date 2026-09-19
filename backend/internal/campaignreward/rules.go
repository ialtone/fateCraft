package campaignreward

import (
	"fmt"
	"math"
	"strings"
)

const ItemStackCalculatorKey = "campaign.extract_item_stack"

const maximumQuantity int64 = 1_000_000_000

type ItemStackRule struct {
	ItemKey               string
	QualityKey            string
	SourceQuantity        int64
	RewardQuantity        int64
	MaximumRewardQuantity int64
	Consume               bool
}

func ParseItemStackRule(config map[string]any) (ItemStackRule, error) {
	allowed := map[string]bool{
		"item_key": true, "quality_key": true, "source_quantity": true,
		"reward_quantity": true, "maximum_reward_quantity": true, "consume": true,
	}
	for key := range config {
		if !allowed[key] {
			return ItemStackRule{}, fmt.Errorf("未知提取配置字段: %s", key)
		}
	}
	rule := ItemStackRule{}
	rule.ItemKey, _ = config["item_key"].(string)
	rule.QualityKey, _ = config["quality_key"].(string)
	if rule.QualityKey == "" {
		rule.QualityKey = "common"
	}
	if !stableKey(rule.ItemKey) || !stableKey(rule.QualityKey) {
		return ItemStackRule{}, fmt.Errorf("item_key 和 quality_key 必须是稳定 key")
	}
	var ok bool
	if rule.SourceQuantity, ok = positiveInt(config["source_quantity"]); !ok || rule.SourceQuantity > maximumQuantity {
		return ItemStackRule{}, fmt.Errorf("source_quantity 必须是 1 到 %d 的整数", maximumQuantity)
	}
	if rule.RewardQuantity, ok = positiveInt(config["reward_quantity"]); !ok || rule.RewardQuantity > maximumQuantity {
		return ItemStackRule{}, fmt.Errorf("reward_quantity 必须是 1 到 %d 的整数", maximumQuantity)
	}
	if rule.MaximumRewardQuantity, ok = positiveInt(config["maximum_reward_quantity"]); !ok ||
		rule.MaximumRewardQuantity > maximumQuantity || rule.MaximumRewardQuantity < rule.RewardQuantity ||
		rule.MaximumRewardQuantity%rule.RewardQuantity != 0 {
		return ItemStackRule{}, fmt.Errorf("maximum_reward_quantity 必须是 reward_quantity 的正整数倍且不超过 %d", maximumQuantity)
	}
	if rule.Consume, ok = config["consume"].(bool); !ok {
		return ItemStackRule{}, fmt.Errorf("consume 必须是布尔值")
	}
	return rule, nil
}

func CalculateItemStack(sourceQuantity int64, rule ItemStackRule) (rewardQuantity, consumedQuantity int64, err error) {
	if sourceQuantity < 0 {
		return 0, 0, fmt.Errorf("局内物品数量不能为负数")
	}
	maximumBatches := rule.MaximumRewardQuantity / rule.RewardQuantity
	batches := sourceQuantity / rule.SourceQuantity
	if batches > maximumBatches {
		batches = maximumBatches
	}
	if batches <= 0 {
		return 0, 0, nil
	}
	if batches > math.MaxInt64/rule.RewardQuantity || batches > math.MaxInt64/rule.SourceQuantity {
		return 0, 0, fmt.Errorf("提取奖励数量溢出")
	}
	rewardQuantity = batches * rule.RewardQuantity
	if rule.Consume {
		consumedQuantity = batches * rule.SourceQuantity
	}
	return rewardQuantity, consumedQuantity, nil
}

func positiveInt(value any) (int64, bool) {
	var number int64
	switch typed := value.(type) {
	case int:
		number = int64(typed)
	case int64:
		number = typed
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) || typed != math.Trunc(typed) || typed > math.MaxInt64 || typed < math.MinInt64 {
			return 0, false
		}
		number = int64(typed)
	default:
		return 0, false
	}
	return number, number > 0
}

func stableKey(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || len(value) > 128 {
		return false
	}
	for index, char := range value {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' ||
			index > 0 && (char == '.' || char == '_' || char == '-') {
			continue
		}
		return false
	}
	return true
}
