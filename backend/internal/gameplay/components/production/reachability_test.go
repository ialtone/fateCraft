package production

import "testing"

func TestDependencyClosureHighLevelAndCycle(t *testing.T) {
	facilities := []Facility{{Key: "forge", MaxLevel: 5, BuildCost: map[string]int64{"wood": 1}, UpgradeCost: map[string]int64{"wood": 1}}}
	recipes := []Recipe{{Facility: "forge", Level: 5, Inputs: map[string]int64{"wood": 1}, Outputs: map[string]int64{"steel": 1}}}
	sources := map[string]bool{"wood": true}
	available, levels := DependencyClosure(facilities, recipes, sources)
	if !available["steel"] || levels["forge"] != 5 || sources["steel"] {
		t.Fatalf("highest-level recipe not reached or input mutated: %v %v", available, levels)
	}
	facilities[0].UpgradeCost = map[string]int64{"steel": 1}
	available, levels = DependencyClosure(facilities, recipes, sources)
	if available["steel"] || levels["forge"] != 1 {
		t.Fatalf("self-dependent upgrade accepted: %v %v", available, levels)
	}
}
