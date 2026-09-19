// Package worldrules defines the bounded, versioned protocol used by campaign
// world events, regional simulation and quests.
package worldrules

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
)

const (
	MaxVariants               = 16
	MaxObjectives             = 16
	MaxRewards                = 16
	MaxAssociations           = 16
	MaxRepeatIntervalMinutes  = int64(10 * 365 * 24 * 60)
	MaxDeadlineMinutes        = int64(10 * 365 * 24 * 60)
	MaxInitialDelayMinutes    = int64(10 * 365 * 24 * 60)
	MaxObjectiveQuantity      = int64(1_000_000_000)
	MaxRewardQuantity         = int64(1_000_000)
	MaxRegionalDelta          = int64(10_000)
	MaxMarketModifierDeltaBPS = int64(20_000)
	MaxNPCGoalDelta           = int64(1_000_000)
)

type RegionInitialState struct {
	ResourceLevel int64            `json:"resource_level"`
	DangerLevel   int64            `json:"danger_level"`
	Stability     int64            `json:"stability"`
	ConflictStage int64            `json:"conflict_stage"`
	FactionPower  map[string]int64 `json:"faction_power"`
}

type WorldEvent struct {
	RegionKey             string         `json:"region_key"`
	InitialDelayMinutes   int64          `json:"initial_delay_minutes"`
	RepeatIntervalMinutes int64          `json:"repeat_interval_minutes,omitempty"`
	Priority              int            `json:"priority"`
	Variants              []EventVariant `json:"variants"`
}

type EventVariant struct {
	Key     string       `json:"key"`
	Weight  int64        `json:"weight"`
	Summary string       `json:"summary"`
	Effects EventEffects `json:"effects"`
}

type EventEffects struct {
	ResourceDelta                int64                       `json:"resource_delta,omitempty"`
	DangerDelta                  int64                       `json:"danger_delta,omitempty"`
	StabilityDelta               int64                       `json:"stability_delta,omitempty"`
	ConflictStage                *int64                      `json:"conflict_stage,omitempty"`
	FactionPowerDeltas           map[string]int64            `json:"faction_power_deltas,omitempty"`
	MarketPriceModifierDeltasBPS map[string]int64            `json:"market_price_modifier_deltas_bps,omitempty"`
	NPCGoalDeltas                map[string]map[string]int64 `json:"npc_goal_deltas,omitempty"`
}

type Quest struct {
	QuestType          string            `json:"quest_type"`
	Initial            bool              `json:"initial"`
	StartingStatus     string            `json:"starting_status"`
	DeadlineMinutes    int64             `json:"deadline_minutes,omitempty"`
	AcceptLocationKeys []string          `json:"accept_location_keys,omitempty"`
	Objectives         []QuestObjective  `json:"objectives"`
	Rewards            []QuestReward     `json:"rewards,omitempty"`
	UnlockQuestKeys    []string          `json:"unlock_quest_keys,omitempty"`
	MilestoneKey       string            `json:"milestone_key,omitempty"`
	CampaignOutcomeKey string            `json:"campaign_outcome_key,omitempty"`
	Associations       QuestAssociations `json:"associations,omitempty"`
}

type QuestAssociations struct {
	RegionKeys     []string `json:"region_keys,omitempty"`
	FactionKeys    []string `json:"faction_keys,omitempty"`
	NPCKeys        []string `json:"npc_keys,omitempty"`
	WorldEventKeys []string `json:"world_event_keys,omitempty"`
}

type QuestObjective struct {
	Key          string `json:"key"`
	Type         string `json:"type"`
	ItemKey      string `json:"item_key,omitempty"`
	EncounterKey string `json:"encounter_key,omitempty"`
	QualityKey   string `json:"quality_key,omitempty"`
	Quantity     int64  `json:"quantity,omitempty"`
	Consume      bool   `json:"consume,omitempty"`
	LocationKey  string `json:"location_key,omitempty"`
	FactionKey   string `json:"faction_key,omitempty"`
	NPCKey       string `json:"npc_key,omitempty"`
	Dimension    string `json:"dimension,omitempty"`
	RegionKey    string `json:"region_key,omitempty"`
	RegionField  string `json:"region_field,omitempty"`
	Comparison   string `json:"comparison,omitempty"`
	Target       int64  `json:"target,omitempty"`
}

type QuestReward struct {
	Type       string `json:"type"`
	ItemKey    string `json:"item_key,omitempty"`
	QualityKey string `json:"quality_key,omitempty"`
	Quantity   int64  `json:"quantity,omitempty"`
	FactionKey string `json:"faction_key,omitempty"`
	Amount     int64  `json:"amount,omitempty"`
}

func ParseRegionInitialState(raw any) (RegionInitialState, bool) {
	state := RegionInitialState{}
	if raw == nil {
		state.ResourceLevel, state.Stability = 5000, 5000
		state.FactionPower = map[string]int64{}
		return state, true
	}
	if !decodeStrict(raw, &state) || !bounded(state.ResourceLevel, 0, 10_000) ||
		!bounded(state.DangerLevel, 0, 10_000) || !bounded(state.Stability, 0, 10_000) ||
		!bounded(state.ConflictStage, 0, 100) || !validBoundedMap(state.FactionPower, 64, 0, 10_000) {
		return RegionInitialState{}, false
	}
	if state.FactionPower == nil {
		state.FactionPower = map[string]int64{}
	}
	return state, true
}

func ParseWorldEvent(raw any) (WorldEvent, bool) {
	var event WorldEvent
	if !decodeStrict(raw, &event) || !stableKey(event.RegionKey) ||
		!bounded(event.InitialDelayMinutes, 0, MaxInitialDelayMinutes) ||
		!bounded(event.RepeatIntervalMinutes, 0, MaxRepeatIntervalMinutes) ||
		event.Priority < 0 || event.Priority > 1000 || len(event.Variants) == 0 || len(event.Variants) > MaxVariants {
		return WorldEvent{}, false
	}
	seen := map[string]bool{}
	for _, variant := range event.Variants {
		if !stableKey(variant.Key) || seen[variant.Key] || variant.Weight < 1 || variant.Weight > 1_000_000 ||
			strings.TrimSpace(variant.Summary) == "" || len([]rune(variant.Summary)) > 256 || !validEffects(variant.Effects) {
			return WorldEvent{}, false
		}
		seen[variant.Key] = true
	}
	return event, true
}

func ParseQuest(raw any) (Quest, bool) {
	var quest Quest
	if !decodeStrict(raw, &quest) || (quest.QuestType != "quest" && quest.QuestType != "obligation") ||
		(quest.StartingStatus != "offered" && quest.StartingStatus != "active") ||
		!bounded(quest.DeadlineMinutes, 0, MaxDeadlineMinutes) ||
		len(quest.Objectives) == 0 || len(quest.Objectives) > MaxObjectives || len(quest.Rewards) > MaxRewards ||
		!validKeyList(quest.AcceptLocationKeys, MaxAssociations) ||
		!validKeyList(quest.UnlockQuestKeys, MaxAssociations) || !optionalStableKey(quest.MilestoneKey) ||
		!optionalStableKey(quest.CampaignOutcomeKey) || !validAssociations(quest.Associations) {
		return Quest{}, false
	}
	if !quest.Initial && quest.StartingStatus == "active" {
		return Quest{}, false
	}
	if quest.CampaignOutcomeKey != "" && quest.MilestoneKey == "" {
		return Quest{}, false
	}
	seen := map[string]bool{}
	for _, objective := range quest.Objectives {
		if !validObjective(objective) || seen[objective.Key] {
			return Quest{}, false
		}
		seen[objective.Key] = true
	}
	for _, reward := range quest.Rewards {
		if !validReward(reward) {
			return Quest{}, false
		}
	}
	return quest, true
}

func validEffects(effects EventEffects) bool {
	if !bounded(effects.ResourceDelta, -MaxRegionalDelta, MaxRegionalDelta) ||
		!bounded(effects.DangerDelta, -MaxRegionalDelta, MaxRegionalDelta) ||
		!bounded(effects.StabilityDelta, -MaxRegionalDelta, MaxRegionalDelta) ||
		!validBoundedMap(effects.FactionPowerDeltas, 64, -MaxRegionalDelta, MaxRegionalDelta) ||
		!validBoundedMap(effects.MarketPriceModifierDeltasBPS, 64, -MaxMarketModifierDeltaBPS, MaxMarketModifierDeltaBPS) {
		return false
	}
	if effects.ConflictStage != nil && !bounded(*effects.ConflictStage, 0, 100) {
		return false
	}
	if len(effects.NPCGoalDeltas) > 64 {
		return false
	}
	for npcKey, goals := range effects.NPCGoalDeltas {
		if !stableKey(npcKey) || !validBoundedMap(goals, 32, -MaxNPCGoalDelta, MaxNPCGoalDelta) {
			return false
		}
	}
	return true
}

func validObjective(objective QuestObjective) bool {
	if !stableKey(objective.Key) {
		return false
	}
	switch objective.Type {
	case "item":
		return stableKey(objective.ItemKey) && optionalStableKey(objective.QualityKey) && bounded(objective.Quantity, 1, MaxObjectiveQuantity) &&
			objective.EncounterKey == "" && objective.LocationKey == "" && objective.FactionKey == "" && objective.NPCKey == "" && objective.RegionKey == "" && objective.Target == 0
	case "encounter_win":
		return stableKey(objective.EncounterKey) && bounded(objective.Quantity, 1, MaxObjectiveQuantity) &&
			objective.ItemKey == "" && objective.QualityKey == "" && !objective.Consume && objective.LocationKey == "" &&
			objective.FactionKey == "" && objective.NPCKey == "" && objective.Dimension == "" &&
			objective.RegionKey == "" && objective.RegionField == "" && objective.Comparison == "" && objective.Target == 0
	case "location":
		return stableKey(objective.LocationKey) && noQuestTargetFields(objective, "location")
	case "faction_reputation":
		return stableKey(objective.FactionKey) && bounded(objective.Target, -10_000, 10_000) && noQuestTargetFields(objective, "faction")
	case "npc_relationship":
		if !stableKey(objective.NPCKey) || !noQuestTargetFields(objective, "npc") {
			return false
		}
		switch objective.Dimension {
		case "trust", "affinity":
			return bounded(objective.Target, -100, 100)
		case "fear", "hostility":
			return bounded(objective.Target, 0, 100)
		case "debt":
			return bounded(objective.Target, -1_000_000_000, 1_000_000_000)
		default:
			return false
		}
	case "region_state":
		if !stableKey(objective.RegionKey) || (objective.Comparison != "at_least" && objective.Comparison != "at_most") || !noQuestTargetFields(objective, "region") {
			return false
		}
		if objective.RegionField == "conflict_stage" {
			return bounded(objective.Target, 0, 100)
		}
		return map[string]bool{"resource_level": true, "danger_level": true, "stability": true}[objective.RegionField] && bounded(objective.Target, 0, 10_000)
	default:
		return false
	}
}

func noQuestTargetFields(objective QuestObjective, keep string) bool {
	if objective.ItemKey != "" || objective.EncounterKey != "" || objective.QualityKey != "" || objective.Quantity != 0 || objective.Consume {
		return false
	}
	if keep != "location" && objective.LocationKey != "" || keep != "faction" && objective.FactionKey != "" ||
		keep != "npc" && (objective.NPCKey != "" || objective.Dimension != "") ||
		keep != "region" && (objective.RegionKey != "" || objective.RegionField != "" || objective.Comparison != "") {
		return false
	}
	if keep == "location" && objective.Target != 0 {
		return false
	}
	return true
}

func validReward(reward QuestReward) bool {
	switch reward.Type {
	case "item":
		return stableKey(reward.ItemKey) && optionalStableKey(reward.QualityKey) && bounded(reward.Quantity, 1, MaxRewardQuantity) && reward.FactionKey == "" && reward.Amount == 0
	case "faction_reputation":
		return stableKey(reward.FactionKey) && bounded(reward.Amount, -1000, 1000) && reward.Amount != 0 && reward.ItemKey == "" && reward.QualityKey == "" && reward.Quantity == 0
	case "faction_contribution":
		return stableKey(reward.FactionKey) && bounded(reward.Amount, 1, 1_000_000) && reward.ItemKey == "" && reward.QualityKey == "" && reward.Quantity == 0
	default:
		return false
	}
}

func validAssociations(value QuestAssociations) bool {
	return validKeyList(value.RegionKeys, MaxAssociations) && validKeyList(value.FactionKeys, MaxAssociations) &&
		validKeyList(value.NPCKeys, MaxAssociations) && validKeyList(value.WorldEventKeys, MaxAssociations)
}

func validKeyList(values []string, maximum int) bool {
	if len(values) > maximum {
		return false
	}
	seen := map[string]bool{}
	for _, value := range values {
		if !stableKey(value) || seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}

func validBoundedMap(values map[string]int64, maximum int, minimum, maximumValue int64) bool {
	if len(values) > maximum {
		return false
	}
	for key, value := range values {
		if !stableKey(key) || !bounded(value, minimum, maximumValue) || value == 0 && minimum < 0 {
			return false
		}
	}
	return true
}

func decodeStrict(raw any, target any) bool {
	data, err := json.Marshal(raw)
	if err != nil || len(data) > 64*1024 {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return false
	}
	return errors.Is(decoder.Decode(&struct{}{}), io.EOF)
}

func bounded(value, minimum, maximum int64) bool {
	return value >= minimum && value <= maximum && value != math.MinInt64
}

func optionalStableKey(value string) bool { return value == "" || stableKey(value) }

func stableKey(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > 128 {
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
