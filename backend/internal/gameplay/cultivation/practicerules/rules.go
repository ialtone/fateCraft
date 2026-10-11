package practicerules

import (
	"fmt"

	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/effectrules"
	"github.com/ialtone/fatecraft/internal/storage"
)

const MaximumSessions int64 = 24

type Facts struct {
	Character       *storage.CampaignCharacter
	Technique       storage.CampaignTechnique
	Definition      storage.GameplayDefinition
	EquippedEffects map[string]int64
}

type Result struct {
	Available                 bool
	MaximumSessions           int64
	ManaCostPerSession        int64
	PracticeMinutesPerSession int64
	CultivationGainPerSession int64
	MasteryGainPerSession     int64
	InsightGainPerSession     int64
	Effects                   map[string]int64
	ViolationCode             string
	ViolationMessage          string
	InvalidDefinition         bool
}

func Evaluate(facts Facts, requestedSessions *int64) Result {
	result := Result{MaximumSessions: MaximumSessions}
	if facts.Character == nil {
		return reject(result, "character.unavailable", "角色修炼数据不可用", false)
	}
	if facts.Technique.Status != "active" || facts.Technique.EquippedSlot == nil {
		return reject(result, "technique.not_equipped", "功法未处于已装配的可修炼状态", false)
	}
	effects, ok := effectrules.MasteredEffects(facts.Definition.Content, facts.Technique.Mastery, facts.Technique.LearnedLayer)
	if !ok {
		return reject(result, "technique.definition_invalid", "功法层级效果无效", true)
	}
	result.Effects = effects
	if facts.EquippedEffects != nil {
		effects = facts.EquippedEffects
		result.Effects = effects
	}
	result.ManaCostPerSession = maximum(0, integer(facts.Definition.Content["mana_cost"], 0)-effects["mana_cost_reduction"])
	if integer(facts.Definition.Content["mana_cost"], 0) > 0 {
		result.ManaCostPerSession = maximum(1, result.ManaCostPerSession)
	}
	result.PracticeMinutesPerSession = integer(facts.Definition.Content["practice_minutes"], 60)
	result.CultivationGainPerSession = integer(facts.Definition.Content["cultivation_gain"], 10) + effects["cultivation_gain"]
	result.MasteryGainPerSession = integer(facts.Definition.Content["mastery_gain"], 25)
	result.InsightGainPerSession = integer(facts.Definition.Content["insight_gain"], 1)
	if result.PracticeMinutesPerSession <= 0 || result.CultivationGainPerSession <= 0 || result.MasteryGainPerSession <= 0 {
		return reject(result, "technique.definition_invalid", "功法定义的修炼收益或耗时无效", true)
	}
	if result.ManaCostPerSession > 0 {
		result.MaximumSessions = facts.Character.Mana / result.ManaCostPerSession
		if result.MaximumSessions > MaximumSessions {
			result.MaximumSessions = MaximumSessions
		}
	}
	if result.MaximumSessions < 1 {
		return reject(result, "resource.insufficient", fmt.Sprintf(
			"法力不足：每次需要 %d，当前 %d", result.ManaCostPerSession, facts.Character.Mana,
		), false)
	}
	if requestedSessions != nil && (*requestedSessions < 1 || *requestedSessions > result.MaximumSessions) {
		return reject(result, "resource.insufficient", fmt.Sprintf(
			"法力不足：本次最多可修炼 %d 次，请求了 %d 次", result.MaximumSessions, *requestedSessions,
		), false)
	}
	result.Available = true
	return result
}

func reject(result Result, code, message string, invalid bool) Result {
	result.Available = false
	result.ViolationCode = code
	result.ViolationMessage = message
	result.InvalidDefinition = invalid
	return result
}

func integer(value any, fallback int64) int64 {
	switch number := value.(type) {
	case int:
		return int64(number)
	case int64:
		return number
	case float64:
		if number == float64(int64(number)) {
			return int64(number)
		}
	}
	return fallback
}

func maximum(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}
