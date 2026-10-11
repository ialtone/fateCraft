package sect

import (
	"fmt"
	"github.com/ialtone/fatecraft/internal/gameplay/components/production"
)

// Check source dependencies and impossible defense targets. This is an
// optimistic reachability check; costs, scheduling and risk still require play.
func validateDependencies(c Config) error {
	available := map[string]bool{}
	for key, n := range c.InitialStock {
		available[key] = n > 0
	}
	var levels map[string]int64
	for {
		available, levels = production.DependencyClosure(c.Facilities, c.Recipes, available)
		changed := false
		for _, m := range c.Missions {
			if m.Minutes >= c.Deadline || !production.HasSources(available, m.Cost) {
				continue
			}
			for key := range m.Reward {
				if !available[key] {
					available[key], changed = true, true
				}
			}
		}
		if !changed {
			break
		}
	}
	for key, n := range c.GoalFacilities {
		if levels[key] < n {
			return fmt.Errorf("目标设施 %s 的材料或生产依赖不可达", key)
		}
	}
	for label, cost := range map[string]map[string]int64{"培养": c.TrainCost, "传承": c.TeachCost, "供养": {c.UpkeepResource: c.Upkeep}} {
		if !production.HasSources(available, cost) {
			return fmt.Errorf("宗门%s资源没有可达来源", label)
		}
	}
	canDispatch := false
	for _, m := range c.Missions {
		canDispatch = canDispatch || m.Minutes < c.Deadline && production.HasSources(available, m.Cost)
	}
	if !canDispatch {
		return fmt.Errorf("没有能在期限内完成且资源可达的派遣任务")
	}
	members := int64(0)
	maxDefense := levels[c.Hall] * 2
	for _, d := range c.Disciples {
		if !d.Initial && !production.HasSources(available, d.RecruitCost) {
			continue
		}
		members++
		// Three levels is the runtime cap; this is deliberately an upper bound.
		maxDefense += d.Power * 3
		if d.Specialty == "guard" {
			maxDefense += 2
		}
	}
	if members < c.GoalDisciples {
		return fmt.Errorf("收徒资源不足以解锁目标人数")
	}
	if levels[c.Hall] == 0 || maxDefense < c.Threat {
		return fmt.Errorf("守宗目标超过可达设施和弟子的防御上限")
	}
	return nil
}
