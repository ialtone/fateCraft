package effectrules

const MaximumMastery int64 = 10000

// LayerForMastery uses the entire mastery scale for every technique. The last
// layer requires mastery 10000, including techniques with more than ten layers.
func LayerForMastery(mastery, maxLayer int64) (int, bool) {
	if mastery < 0 || mastery > MaximumMastery || maxLayer < 1 || maxLayer > MaxLayerEffects {
		return 0, false
	}
	return 1 + int(mastery*(maxLayer-1)/MaximumMastery), true
}

// MasteredEffects verifies the persisted cache before it can affect gameplay.
func MasteredEffects(content map[string]any, mastery int64, learnedLayer int) (map[string]int64, bool) {
	maximum, ok := integer(content["max_layer"])
	if !ok {
		return nil, false
	}
	layer, ok := LayerForMastery(mastery, maximum)
	if !ok || layer != learnedLayer {
		return nil, false
	}
	return TechniqueEffects(content, layer)
}
