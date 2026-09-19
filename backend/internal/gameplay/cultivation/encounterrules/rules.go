// Package encounterrules defines the bounded, versioned rules for one-on-one
// campaign encounters. The action layer owns all random resolution; this
// package only parses and validates immutable encounter definitions.
package encounterrules

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

const (
	MaxRewards      = 8
	MaxRounds       = int64(100)
	MaxHealth       = int64(1_000_000)
	MaxAttack       = int64(100_000)
	MaxDefense      = int64(100_000)
	MaxDamage       = int64(100_000)
	MaxRoundMinutes = int64(24 * 60)
	MaxRewardAmount = int64(1_000_000)
)

type Encounter struct {
	LocationKey          string   `json:"location_key"`
	OpponentKey          string   `json:"opponent_key"`
	OpponentName         string   `json:"opponent_name"`
	OpponentRealmRank    int64    `json:"opponent_realm_rank"`
	OpponentMaxHealth    int64    `json:"opponent_max_health"`
	OpponentAttack       int64    `json:"opponent_attack"`
	OpponentDefense      int64    `json:"opponent_defense"`
	PlayerDamageBase     int64    `json:"player_damage_base"`
	PlayerDamageScaleBPS int64    `json:"player_damage_scale_bps"`
	PlayerManaCost       int64    `json:"player_mana_cost"`
	RoundMinutes         int64    `json:"round_minutes"`
	MaxEncounterRounds   int64    `json:"max_rounds"`
	FleeChanceBPS        int64    `json:"flee_chance_bps"`
	Rewards              []Reward `json:"rewards,omitempty"`
}

type Reward struct {
	ItemKey    string `json:"item_key"`
	QualityKey string `json:"quality_key,omitempty"`
	Quantity   int64  `json:"quantity"`
}

func Parse(raw any) (Encounter, bool) {
	var encounter Encounter
	if !decodeStrict(raw, &encounter) ||
		!stableKey(encounter.LocationKey) || !stableKey(encounter.OpponentKey) ||
		strings.TrimSpace(encounter.OpponentName) == "" || len([]rune(encounter.OpponentName)) > 128 ||
		encounter.OpponentRealmRank < 0 || encounter.OpponentRealmRank > 100 ||
		encounter.OpponentMaxHealth < 1 || encounter.OpponentMaxHealth > MaxHealth ||
		encounter.OpponentAttack < 0 || encounter.OpponentAttack > MaxAttack ||
		encounter.OpponentDefense < 0 || encounter.OpponentDefense > MaxDefense ||
		encounter.PlayerDamageBase < 1 || encounter.PlayerDamageBase > MaxDamage ||
		encounter.PlayerDamageScaleBPS < 0 || encounter.PlayerDamageScaleBPS > 100_000 ||
		encounter.PlayerManaCost < 0 || encounter.PlayerManaCost > MaxDamage ||
		encounter.RoundMinutes < 1 || encounter.RoundMinutes > MaxRoundMinutes ||
		encounter.MaxEncounterRounds < 1 || encounter.MaxEncounterRounds > MaxRounds ||
		encounter.FleeChanceBPS < 0 || encounter.FleeChanceBPS > 10_000 ||
		len(encounter.Rewards) > MaxRewards {
		return Encounter{}, false
	}
	seen := map[string]bool{}
	for index := range encounter.Rewards {
		reward := &encounter.Rewards[index]
		if reward.QualityKey == "" {
			reward.QualityKey = "common"
		}
		identity := reward.ItemKey + ":" + reward.QualityKey
		if !stableKey(reward.ItemKey) || !stableKey(reward.QualityKey) || seen[identity] ||
			reward.Quantity < 1 || reward.Quantity > MaxRewardAmount {
			return Encounter{}, false
		}
		seen[identity] = true
	}
	return encounter, true
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
