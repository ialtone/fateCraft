package roll

import (
	"context"
	"math"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/protocol"
)

// CalculateResult 计算判定结果（完整流程），使用 Service 持有的配置与历史存储。
func (s *Service) CalculateResult(
	ctx context.Context,
	req *protocol.RollRequest,
	playerState map[string]any,
	convID uuid.UUID,
	difficultyMode string,
) *DetailedResult {
	config := s.config
	result := &DetailedResult{
		RollType:    req.Type,
		Category:    req.Category,
		Description: req.Description,
		Breakdown:   &Breakdown{},
	}

	// 1. 基础成功率
	base := calculateBase(req, config)
	result.Breakdown.Base = base

	// 2. 属性加成
	attrBonus := calculateAttributeBonus(req, playerState, config)
	result.Breakdown.Attributes = attrBonus

	// 3. 天赋特质加成（条件词条来自全部情境枚举，如"夜晚"在 environment、"使用剑类武器"在 advantages）
	contextTokens := collectContextTokens(req)
	traitBonus := MatchTraits(playerState, req.Category, contextTokens, config)
	result.Breakdown.Traits = traitBonus

	// 4. 情境修正
	contextBonus := calculateContextBonus(req, config)
	result.Breakdown.Context = contextBonus

	// 5. 收益平衡限制
	rewardValue := extractRewardValue(req)
	rewardCap := applyRewardCap(base+attrBonus+traitBonus+contextBonus, rewardValue, config)
	result.Breakdown.RewardCap = rewardCap - (base + attrBonus + traitBonus + contextBonus)

	// 6. 历史调整（从持久化流水实时计算，重启后仍生效）
	historyAdj := s.getAdjustment(ctx, convID, req.Type, rewardValue)
	result.Breakdown.History = historyAdj

	// 7. 难度模式
	diffAdj := config.DifficultyModes[difficultyMode]
	result.Breakdown.DifficultyMode = diffAdj

	// 计算最终成功率
	final := rewardCap + historyAdj + diffAdj
	final = clamp(final, 1, 99) // 限制在 1-99%
	result.Breakdown.Final = final

	// 8. 计算多级结果概率分布
	result.Outcomes = calculateOutcomes(final, config)

	return result
}

// DetailedResult 详细的判定结果。
type DetailedResult struct {
	RollType    string
	Category    string
	Description string
	Breakdown   *Breakdown
	Outcomes    *OutcomeDistribution
}

// Breakdown 成功率分解。
type Breakdown struct {
	Base           int `json:"base"`
	Attributes     int `json:"attributes"`
	Traits         int `json:"traits"`
	Context        int `json:"context"`
	RewardCap      int `json:"reward_cap"`
	History        int `json:"history"`
	DifficultyMode int `json:"difficulty_mode"`
	Final          int `json:"final"`
}

// OutcomeDistribution 结果概率分布。
type OutcomeDistribution struct {
	CriticalSuccess *OutcomeRange `json:"critical_success,omitempty"`
	Success         *OutcomeRange `json:"success"`
	Failure         *OutcomeRange `json:"failure"`
	CriticalFailure *OutcomeRange `json:"critical_failure,omitempty"`
}

// OutcomeRange 结果区间。
type OutcomeRange struct {
	Probability float64 `json:"probability"`
	RangeStart  int     `json:"range_start"`
	RangeEnd    int     `json:"range_end"`
}

func calculateBase(req *protocol.RollRequest, config *Config) int {
	// AI 提供了 base_difficulty，优先使用
	if req.BaseDifficulty > 0 {
		return int(req.BaseDifficulty)
	}

	// 否则从类别获取默认值
	if req.Category != "" {
		if cat, ok := config.Categories[req.Category]; ok {
			return cat.BaseSuccessRate
		}
	}

	// 兜底默认值
	return 50
}

func calculateAttributeBonus(req *protocol.RollRequest, state map[string]any, config *Config) int {
	attributes, ok := state["attributes"].(map[string]any)
	if !ok {
		return 0
	}

	// 获取属性和权重
	attrNames := req.RelevantAttributes
	weights := req.AttributeWeights

	// 如果AI没提供，从配置获取默认值
	if len(attrNames) == 0 && req.Category != "" {
		if cat, ok := config.Categories[req.Category]; ok {
			attrNames = cat.DefaultAttributes
			weights = cat.DefaultWeights
		}
	}

	if len(attrNames) == 0 {
		return 0
	}

	// 计算加权属性加成
	totalBonus := 0.0
	for _, attrName := range attrNames {
		val := getAttributeValue(attributes, attrName)
		weight := 1.0 / float64(len(attrNames)) // 默认均分
		if w, ok := weights[attrName]; ok {
			weight = w
		}

		// 属性值范围假设 0-100，转换为 -20 到 +20 的加成
		normalized := (float64(val) - 50.0) * 0.4 // (val-50) * 0.4
		totalBonus += normalized * weight
	}

	return int(math.Round(totalBonus))
}

func getAttributeValue(attributes map[string]any, name string) int {
	if val, ok := attributes[name]; ok {
		switch v := val.(type) {
		case int:
			return v
		case float64:
			return int(v)
		case int64:
			return int(v)
		}
	}
	return 50 // 默认中等属性
}

func calculateContextBonus(req *protocol.RollRequest, config *Config) int {
	if req.Context == nil {
		return 0
	}

	total := 0

	// 有利/不利/环境因素：取值来自枚举词表，按精确查表计分（键完全一致才命中）。
	total += sumByExactKey(req.Context.Advantages, config.ContextModifiers.Advantages)
	total += sumByExactKey(req.Context.Disadvantages, config.ContextModifiers.Disadvantages)
	total += sumByExactKey(req.Context.Environment, config.ContextModifiers.Environment)

	// 对手因素（战斗类判定）：结构化字段，境界差距按级数乘算，状态/克制精确查表。
	if opp := req.Context.Opponent; opp != nil {
		total += calculateOpponentBonus(opp, config)
	}

	return total
}

// sumByExactKey 对枚举词条按精确键查表求和（未命中的词条忽略）。
// 相比旧的子串包含匹配，精确查表消除了同义漏报、否定误报、子串歧义三类问题；
// 词表通过 JSON Schema 的 enum 约束模型只能输出表内值。
func sumByExactKey(items []string, table map[string]int) int {
	total := 0
	for _, item := range items {
		if v, ok := table[item]; ok {
			total += v
		}
	}
	return total
}

// calculateOpponentBonus 计算对手情境修正（结构化字段，精确计分）：
//   - RealmGap（对手比玩家高的境界数，可为负）× 配置的「境界差距_每级」；
//   - Statuses（对手受伤/对手疲惫等）与 Element（属性克制/属性被克）按枚举精确查表。
func calculateOpponentBonus(opp *protocol.RollOpponent, config *Config) int {
	rules := config.ContextModifiers.Opponent
	if len(rules) == 0 {
		return 0
	}

	total := 0

	// 境界差距：数值型，按级数乘算。
	if opp.RealmGap != 0 {
		total += int(opp.RealmGap) * rules["境界差距_每级"]
	}

	// 对手状态：枚举精确查表。
	for _, st := range opp.Statuses {
		if v, ok := rules[st]; ok {
			total += v
		}
	}

	// 克制关系：枚举精确查表。
	if opp.Element != "" {
		if v, ok := rules[opp.Element]; ok {
			total += v
		}
	}

	return total
}

// applyRewardCap 按收益价值封顶成功率：取所有命中档位（rewardValue >= threshold）
// 中最严格的上限（min MaxRate）。不能命中即返回原值。
// 注意不能命中首个低阈值档就提前返回——高收益必须吃到更低的上限。
func applyRewardCap(currentRate int, rewardValue int, config *Config) int {
	capped := currentRate
	for _, cap := range config.RewardBalance.Caps {
		if rewardValue >= cap.Threshold && capped > cap.MaxRate {
			capped = cap.MaxRate
		}
	}
	return capped
}

func calculateOutcomes(successRate int, config *Config) *OutcomeDistribution {
	dist := &OutcomeDistribution{}

	critSuccProb := config.OutcomeLevels.CriticalSuccess.Probability
	critFailProb := config.OutcomeLevels.CriticalFailure.Probability

	// 大成功（仅当成功率足够高）
	if successRate >= config.OutcomeLevels.CriticalSuccess.MinThreshold {
		critSuccEnd := int(critSuccProb * 100)
		dist.CriticalSuccess = &OutcomeRange{
			Probability: critSuccProb,
			RangeStart:  1,
			RangeEnd:    critSuccEnd,
		}
	}

	// 成功
	successStart := 1
	if dist.CriticalSuccess != nil {
		successStart = dist.CriticalSuccess.RangeEnd + 1
	}
	dist.Success = &OutcomeRange{
		Probability: float64(successRate) / 100.0,
		RangeStart:  successStart,
		RangeEnd:    successRate,
	}

	// 失败
	failStart := successRate + 1
	failEnd := 100
	if (100 - successRate) >= config.OutcomeLevels.CriticalFailure.MinThreshold {
		failEnd = 100 - int(critFailProb*100)
		dist.CriticalFailure = &OutcomeRange{
			Probability: critFailProb,
			RangeStart:  failEnd + 1,
			RangeEnd:    100,
		}
	}

	dist.Failure = &OutcomeRange{
		Probability: float64(failEnd-failStart+1) / 100.0,
		RangeStart:  failStart,
		RangeEnd:    failEnd,
	}

	return dist
}

// collectContextTokens 汇总情境里的全部枚举词条（有利/不利/环境/对手状态与克制），
// 供特质匹配的 conditions 做精确比对。
func collectContextTokens(req *protocol.RollRequest) []string {
	if req.Context == nil {
		return nil
	}
	tokens := make([]string, 0,
		len(req.Context.Advantages)+len(req.Context.Disadvantages)+len(req.Context.Environment)+3)
	tokens = append(tokens, req.Context.Advantages...)
	tokens = append(tokens, req.Context.Disadvantages...)
	tokens = append(tokens, req.Context.Environment...)
	if opp := req.Context.Opponent; opp != nil {
		tokens = append(tokens, opp.Statuses...)
		if opp.Element != "" {
			tokens = append(tokens, opp.Element)
		}
	}
	return tokens
}

func extractRewardValue(req *protocol.RollRequest) int {
	if req.Stakes != nil {
		return req.Stakes.RewardValue
	}
	return 0
}

func clamp(val, min, max int) int {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}
