package production

// DependencyClosure checks sources and facility prerequisites, not material
// quantities, elapsed time or profitability. Inputs must already be validated.
// Each pass either discovers a resource, raises a facility level, or stops;
// the finite configuration therefore bounds the work without a guessed depth.
func DependencyClosure(facilities []Facility, recipes []Recipe, sources map[string]bool) (map[string]bool, map[string]int64) {
	available := make(map[string]bool, len(sources))
	for key, value := range sources {
		available[key] = value
	}
	levels := map[string]int64{}
	for {
		changed := false
		for _, f := range facilities {
			if levels[f.Key] >= f.MaxLevel {
				continue
			}
			cost := f.BuildCost
			if levels[f.Key] > 0 {
				cost = f.UpgradeCost
			}
			if HasSources(available, cost) {
				levels[f.Key]++
				changed = true
			}
		}
		for _, r := range recipes {
			if levels[r.Facility] < r.Level || !HasSources(available, r.Inputs) {
				continue
			}
			for key := range r.Outputs {
				if !available[key] {
					available[key] = true
					changed = true
				}
			}
		}
		if !changed {
			return available, levels
		}
	}
}

func HasSources(available map[string]bool, cost map[string]int64) bool {
	for key, amount := range cost {
		if amount > 0 && !available[key] {
			return false
		}
	}
	return true
}
