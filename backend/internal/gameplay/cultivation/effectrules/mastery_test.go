package effectrules

import "testing"

func TestEveryTechniqueLayerIsReachable(t *testing.T) {
	for _, maximum := range []int64{1, 6, 9, 12, 15, 18, 100} {
		previous := 1
		for mastery := int64(0); mastery <= MaximumMastery; mastery++ {
			layer, ok := LayerForMastery(mastery, maximum)
			if !ok || layer < previous || layer > previous+1 || layer > int(maximum) {
				t.Fatalf("max=%d mastery=%d layer=%d previous=%d", maximum, mastery, layer, previous)
			}
			previous = layer
		}
		if previous != int(maximum) {
			t.Fatalf("max=%d capped at %d", maximum, previous)
		}
		for layer := int64(2); layer <= maximum; layer++ {
			threshold := ((layer-1)*MaximumMastery + maximum - 2) / (maximum - 1)
			before, _ := LayerForMastery(threshold-1, maximum)
			after, _ := LayerForMastery(threshold, maximum)
			if before != int(layer)-1 || after != int(layer) {
				t.Fatalf("incorrect boundary max=%d layer=%d threshold=%d", maximum, layer, threshold)
			}
		}
	}
}

func TestMasteryRejectsInvalidStateAndForgedLayer(t *testing.T) {
	for _, c := range [][2]int64{{-1, 12}, {10001, 12}, {0, 0}, {0, 101}} {
		if _, ok := LayerForMastery(c[0], c[1]); ok {
			t.Fatalf("invalid mastery accepted: %v", c)
		}
	}
	content := map[string]any{"max_layer": int64(18), "layer_effects": []any{map[string]any{"layer": int64(18), "effects": map[string]any{"max_mana": int64(50)}}}}
	if _, ok := MasteredEffects(content, 9999, 18); ok {
		t.Fatal("unearned final effect accepted")
	}
	if effects, ok := MasteredEffects(content, 10000, 18); !ok || effects["max_mana"] != 50 {
		t.Fatal("earned final effect missing")
	}
}
