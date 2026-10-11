package survival

import (
	"fmt"
	"maps"
	"slices"
)

// Prove that one rescue device can actually be crafted from finite reachable
// resources. Boolean reachability alone incorrectly accepts one unit of wood
// as a source for a recipe requiring ten. Travel and survival are tested by
// scenario simulations separately; this proof concerns material conservation.
func validateRescueMaterials(c Config, reachable map[string]bool) error {
	stock := maps.Clone(c.InitialStock)
	for _, location := range c.Locations {
		if reachable[location.Key] {
			for key, n := range location.Resources {
				stock[key] += n
			}
		}
	}
	// The goal requires crafting evidence even if the player starts with a device.
	delete(stock, c.RescueItem)
	steps := 0
	exhausted := false
	type requirement struct {
		key      string
		quantity int64
		produce  bool
	}
	var obtain func(map[string]int64, []requirement, map[string]bool) bool
	obtain = func(current map[string]int64, needs []requirement, visiting map[string]bool) bool {
		steps++
		if steps > 8192 {
			exhausted = true
			return false
		}
		if len(needs) == 0 {
			return true
		}
		wanted := needs[0]
		if wanted.quantity > 32_000_000 {
			return false
		}
		if wanted.produce {
			next, guard := maps.Clone(current), maps.Clone(visiting)
			next[wanted.key] += wanted.quantity
			delete(guard, wanted.key)
			return obtain(next, needs[1:], guard)
		}
		if current[wanted.key] >= wanted.quantity {
			next := maps.Clone(current)
			next[wanted.key] -= wanted.quantity
			return obtain(next, needs[1:], visiting)
		}
		if visiting[wanted.key] {
			return false
		}
		for _, r := range c.Recipes {
			if r.Output != wanted.key {
				continue
			}
			batches := (wanted.quantity - current[wanted.key] + r.Quantity - 1) / r.Quantity
			// Expand the recipe inputs while retaining all remaining obligations. A
			// continuation sentinel releases the cycle guard once production finishes.
			keys := []string{}
			for key := range r.Inputs {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			expanded := []requirement{}
			for _, key := range keys {
				expanded = append(expanded, requirement{key: key, quantity: r.Inputs[key] * batches})
			}
			expanded = append(expanded, requirement{key: wanted.key, quantity: r.Quantity * batches, produce: true})
			expanded = append(expanded, needs...)
			guard := maps.Clone(visiting)
			guard[wanted.key] = true
			if obtain(current, expanded, guard) {
				return true
			}
		}
		return false
	}
	if obtain(stock, []requirement{{key: c.RescueItem, quantity: 1}}, map[string]bool{}) {
		return nil
	}
	if exhausted {
		return fmt.Errorf("求援配方的材料证明超过预算，请简化交叉依赖")
	}
	return fmt.Errorf("可达地点的有限材料不足以制作求援装置")
}
