package gatheringrules

import "testing"

func TestParseChooseAndQuantity(t *testing.T) {
	table, ok := Parse(map[string]any{
		"minutes": int64(90), "risk_chance_bps": int64(1200), "risk_health_damage": int64(4),
		"entries": []any{
			map[string]any{"item_key": "herb", "min_quantity": int64(1), "max_quantity": int64(3), "weight": int64(3)},
			map[string]any{"item_key": "ore", "quality_key": "fine", "min_quantity": int64(1), "max_quantity": int64(1), "weight": int64(1)},
		},
	})
	if !ok || table.Entries[0].QualityKey != "common" {
		t.Fatalf("valid table was rejected: %#v", table)
	}
	entry, ok := Choose(table.Entries, 3)
	if !ok || entry.ItemKey != "ore" || Quantity(entry, 99) != 1 {
		t.Fatalf("unexpected deterministic outcome: %#v", entry)
	}
	if matches := MatchingEntries(table, "herb"); len(matches) != 1 || Quantity(matches[0], 2) != 3 {
		t.Fatalf("targeted gathering failed: %#v", matches)
	}
}

func TestParseRejectsUnknownOrDuplicateEntries(t *testing.T) {
	for _, raw := range []any{
		map[string]any{"minutes": int64(10), "unknown": true, "entries": []any{}},
		map[string]any{"minutes": int64(10), "risk_chance_bps": int64(100), "entries": []any{
			map[string]any{"item_key": "herb", "min_quantity": int64(1), "max_quantity": int64(1), "weight": int64(1)},
		}},
		map[string]any{"minutes": int64(10), "entries": []any{
			map[string]any{"item_key": "herb", "min_quantity": int64(1), "max_quantity": int64(1), "weight": int64(1)},
			map[string]any{"item_key": "herb", "min_quantity": int64(1), "max_quantity": int64(2), "weight": int64(1)},
		}},
	} {
		if _, ok := Parse(raw); ok {
			t.Fatalf("invalid table was accepted: %#v", raw)
		}
	}
}
