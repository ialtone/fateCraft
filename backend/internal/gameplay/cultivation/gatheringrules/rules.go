// Package gatheringrules defines the bounded resource tables attached to
// versioned campaign locations. Gathering outcomes are selected by the trusted
// action service; narrative text cannot invent a resource outside these tables.
package gatheringrules

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

const (
	MaxEntries  = 32
	MaxQuantity = int64(1000)
	MaxMinutes  = int64(7 * 24 * 60)
	MaxWeight   = int64(1_000_000)
)

type Table struct {
	Minutes          int64   `json:"minutes"`
	RiskChanceBPS    int64   `json:"risk_chance_bps,omitempty"`
	RiskHealthDamage int64   `json:"risk_health_damage,omitempty"`
	Entries          []Entry `json:"entries"`
}

type Entry struct {
	ItemKey     string `json:"item_key"`
	QualityKey  string `json:"quality_key,omitempty"`
	MinQuantity int64  `json:"min_quantity"`
	MaxQuantity int64  `json:"max_quantity"`
	Weight      int64  `json:"weight"`
}

func Parse(raw any) (Table, bool) {
	var table Table
	if !decodeStrict(raw, &table) || table.Minutes < 1 || table.Minutes > MaxMinutes ||
		table.RiskChanceBPS < 0 || table.RiskChanceBPS > 10_000 ||
		table.RiskHealthDamage < 0 || table.RiskHealthDamage > 10_000 ||
		(table.RiskChanceBPS == 0) != (table.RiskHealthDamage == 0) ||
		len(table.Entries) == 0 || len(table.Entries) > MaxEntries {
		return Table{}, false
	}
	seen := map[string]bool{}
	for index := range table.Entries {
		entry := &table.Entries[index]
		if entry.QualityKey == "" {
			entry.QualityKey = "common"
		}
		identity := entry.ItemKey + ":" + entry.QualityKey
		if !stableKey(entry.ItemKey) || !stableKey(entry.QualityKey) || seen[identity] ||
			entry.MinQuantity < 1 || entry.MinQuantity > entry.MaxQuantity || entry.MaxQuantity > MaxQuantity ||
			entry.Weight < 1 || entry.Weight > MaxWeight {
			return Table{}, false
		}
		seen[identity] = true
	}
	return table, true
}

func MatchingEntries(table Table, itemKey string) []Entry {
	if itemKey == "" {
		return append([]Entry(nil), table.Entries...)
	}
	out := make([]Entry, 0, 1)
	for _, entry := range table.Entries {
		if entry.ItemKey == itemKey {
			out = append(out, entry)
		}
	}
	return out
}

func Choose(entries []Entry, roll uint64) (Entry, bool) {
	var total uint64
	for _, entry := range entries {
		if entry.Weight < 1 || uint64(entry.Weight) > ^uint64(0)-total {
			return Entry{}, false
		}
		total += uint64(entry.Weight)
	}
	if total == 0 {
		return Entry{}, false
	}
	target := roll % total
	for _, entry := range entries {
		if target < uint64(entry.Weight) {
			return entry, true
		}
		target -= uint64(entry.Weight)
	}
	return Entry{}, false
}

func Quantity(entry Entry, roll uint64) int64 {
	if entry.MinQuantity >= entry.MaxQuantity {
		return entry.MinQuantity
	}
	return entry.MinQuantity + int64(roll%uint64(entry.MaxQuantity-entry.MinQuantity+1))
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
