package factionrules

import (
	"strconv"
	"strings"
)

const (
	MinimumReputation    = -10000
	MaximumReputation    = 10000
	MaximumContribution  = 1000000000000
	MaxEntryRequirements = 8
)

type EntryRequirement struct {
	FactionKey    string
	PermissionKey string
	MinRealmRank  int
}

func ReputationLevel(reputation int) string {
	switch {
	case reputation >= 5000:
		return "honored"
	case reputation >= 1000:
		return "ally"
	case reputation >= 200:
		return "friendly"
	case reputation <= -5000:
		return "nemesis"
	case reputation <= -1000:
		return "hostile"
	case reputation <= -200:
		return "unwelcome"
	default:
		return "outsider"
	}
}

func Position(content map[string]any, contribution int64) string {
	thresholds, ok := object(content["position_thresholds"])
	if !ok {
		return "outsider"
	}
	position := "outsider"
	var selected int64 = -1
	for key, raw := range thresholds {
		threshold, valid := integer(raw)
		if !valid || threshold < 0 || contribution < threshold {
			continue
		}
		if threshold > selected || threshold == selected && key < position {
			position = key
			selected = threshold
		}
	}
	return position
}

// DerivePermissions returns only granted permissions. Callers replace the
// stored projection with this result so permissions are also revoked when
// reputation falls below a threshold.
func DerivePermissions(content map[string]any, reputation int) map[string]any {
	permissions := map[string]any{}
	thresholds, ok := object(content["permission_thresholds"])
	if !ok {
		return permissions
	}
	for key, raw := range thresholds {
		threshold, valid := integer(raw)
		if valid && reputation >= int(threshold) {
			permissions[key] = true
		}
	}
	return permissions
}

func PermissionThreshold(content map[string]any, permissionKey string) (int, bool) {
	thresholds, ok := object(content["permission_thresholds"])
	if !ok {
		return 0, false
	}
	threshold, ok := integer(thresholds[permissionKey])
	return int(threshold), ok
}

func IsGranted(permissions map[string]any, permissionKey string) bool {
	granted, ok := permissions[permissionKey].(bool)
	return ok && granted
}

func EntryRequirements(content map[string]any) ([]EntryRequirement, bool) {
	raw, exists := content["entry_requirements"]
	if !exists {
		return []EntryRequirement{}, true
	}
	items, ok := raw.([]any)
	if !ok || len(items) > MaxEntryRequirements {
		return nil, false
	}
	requirements := make([]EntryRequirement, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		value, ok := object(item)
		if !ok {
			return nil, false
		}
		if len(value) == 1 {
			rank, valid := integer(value["min_realm_rank"])
			identity := "realm:" + strconv.FormatInt(rank, 10)
			if !valid || rank < 1 || rank > 100 || seen[identity] {
				return nil, false
			}
			seen[identity] = true
			requirements = append(requirements, EntryRequirement{MinRealmRank: int(rank)})
			continue
		}
		if len(value) != 2 {
			return nil, false
		}
		factionKey, factionOK := value["faction_key"].(string)
		permissionKey, permissionOK := value["permission_key"].(string)
		identity := factionKey + ":" + permissionKey
		if !factionOK || !permissionOK || !stableKey(factionKey) || !stableKey(permissionKey) || seen[identity] {
			return nil, false
		}
		seen[identity] = true
		requirements = append(requirements, EntryRequirement{FactionKey: factionKey, PermissionKey: permissionKey})
	}
	return requirements, true
}

func object(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	default:
		return nil, false
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

func stableKey(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || len(value) > 128 {
		return false
	}
	for index, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') ||
			index > 0 && (char == '.' || char == '_' || char == '-') {
			continue
		}
		return false
	}
	return true
}
