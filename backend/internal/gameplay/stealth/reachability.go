package stealth

import (
	"fmt"
	"math/bits"
)

// Every opened gate consumes one pick until intelligence is obtained. Flooding
// an unlocked component is sound because the map is bidirectional; returning
// through an opened gate does not consume another tool. Risk/time are separate.
func validateGateRoute(c Config) error {
	indices := map[string]int{}
	for i, l := range c.Locations {
		indices[l.Key] = i
	}
	queue := []uint32{0}
	seen := map[uint32]bool{0: true}
	for head := 0; head < len(queue); head++ {
		opened := queue[head]
		reached := map[string]bool{c.Start: true}
		intel := false
		for {
			changed := false
			for _, l := range c.Locations {
				if !reached[l.Key] {
					continue
				}
				if l.Intel && !intel {
					intel, changed = true, true
				}
				for _, key := range l.Links {
					target := c.Locations[indices[key]]
					if !reached[key] && (!target.Locked || intel || opened&(1<<indices[key]) != 0) {
						reached[key], changed = true, true
					}
				}
			}
			if !changed {
				break
			}
		}
		if reached[c.Target] {
			for _, exit := range c.Exits {
				if reached[exit] {
					return nil
				}
			}
		}
		if intel || int64(bits.OnesCount32(opened)) >= c.Lockpicks {
			continue
		}
		for _, l := range c.Locations {
			if !reached[l.Key] {
				continue
			}
			for _, key := range l.Links {
				if reached[key] {
					continue
				}
				next := opened | 1<<indices[key]
				if !seen[next] {
					if len(queue) >= 8192 {
						return fmt.Errorf("门禁路线校验超过 8192 种组合，请减少独立门禁或增加可达情报")
					}
					seen[next] = true
					queue = append(queue, next)
				}
			}
		}
	}
	return fmt.Errorf("开锁器数量或情报来源不足以取得目标并抵达出口")
}
