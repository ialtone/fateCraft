package roll

import (
	"strings"
)

// MatchTraits 匹配玩家的天赋和特质，返回总加成/减益。
func MatchTraits(
	playerState map[string]any,
	rollCategory string,
	contextAdvantages []string,
	config *Config,
) int {
	totalBonus := 0

	// 匹配初始天赋
	if profile, ok := playerState["profile"].(map[string]any); ok {
		if talent, ok := profile["talent"].(string); ok {
			totalBonus += matchTalent(talent, rollCategory, contextAdvantages, config)
		}
	}

	// 匹配动态特质
	if traits, ok := playerState["traits"].([]any); ok {
		for _, t := range traits {
			if traitStr, ok := t.(string); ok {
				totalBonus += matchTrait(traitStr, rollCategory, contextAdvantages, config)
			}
		}
	}

	return totalBonus
}

func matchTalent(talent string, category string, advantages []string, config *Config) int {
	talentLower := strings.ToLower(talent)

	for _, pattern := range config.TraitPatterns.Talents {
		// 检查关键词匹配
		matched := false
		for _, keyword := range pattern.Keywords {
			if strings.Contains(talentLower, strings.ToLower(keyword)) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}

		// 检查适用类别
		if !appliesToCategory(pattern.AppliesTo, category) {
			continue
		}

		// 检查前置条件
		if !checkConditions(pattern.Conditions, advantages) {
			continue
		}

		return pattern.Bonus - pattern.Penalty
	}

	return 0
}

func matchTrait(trait string, category string, advantages []string, config *Config) int {
	traitLower := strings.ToLower(trait)

	for _, pattern := range config.TraitPatterns.Traits {
		matched := false
		for _, keyword := range pattern.Keywords {
			if strings.Contains(traitLower, strings.ToLower(keyword)) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}

		if !appliesToCategory(pattern.AppliesTo, category) {
			continue
		}

		if !checkConditions(pattern.Conditions, advantages) {
			continue
		}

		return pattern.Bonus - pattern.Penalty
	}

	return 0
}

func appliesToCategory(appliesTo []string, category string) bool {
	if len(appliesTo) == 0 {
		return true
	}
	for _, cat := range appliesTo {
		if cat == "all" || cat == category {
			return true
		}
	}
	return false
}

func checkConditions(conditions []string, contextTokens []string) bool {
	if len(conditions) == 0 {
		return true
	}
	for _, cond := range conditions {
		found := false
		for _, tok := range contextTokens {
			if tok == cond {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
