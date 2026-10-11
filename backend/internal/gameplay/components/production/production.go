// Package production provides bounded facilities and paid production queues.
// Industry, staff, maintenance and victory conditions belong to the module.
package production

import (
	"fmt"
	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
)

type Facility struct {
	Key         string           `json:"key"`
	Name        string           `json:"name"`
	BuildCost   map[string]int64 `json:"build_cost"`
	UpgradeCost map[string]int64 `json:"upgrade_cost"`
	MaxLevel    int64            `json:"max_level"`
}
type Recipe struct {
	Key      string           `json:"key"`
	Name     string           `json:"name"`
	Facility string           `json:"facility"`
	Level    int64            `json:"level"`
	Inputs   map[string]int64 `json:"inputs"`
	Outputs  map[string]int64 `json:"outputs"`
	Minutes  int64            `json:"minutes"`
}
type Job struct {
	Key      string `json:"key"`
	Recipe   string `json:"recipe"`
	Facility string `json:"facility"`
	Due      int64  `json:"due"`
}

func Validate(facilities []Facility, recipes []Recipe, resources map[string]bool) error {
	if len(facilities) < 1 || len(facilities) > 16 || len(recipes) < 1 || len(recipes) > 32 {
		return fmt.Errorf("设施或生产配方数量无效")
	}
	known := map[string]Facility{}
	cost := func(values map[string]int64) error {
		if len(values) == 0 {
			return fmt.Errorf("资源清单不能为空")
		}
		if err := ruledata.ValidateStock(values); err != nil {
			return err
		}
		for key, n := range values {
			if !resources[key] || n < 1 {
				return fmt.Errorf("未知资源或非正数量 %s", key)
			}
		}
		return nil
	}
	for _, f := range facilities {
		if !contract.ValidStableKey(f.Key) || f.Name == "" || known[f.Key].Key != "" || f.MaxLevel < 1 || f.MaxLevel > 5 {
			return fmt.Errorf("设施无效 %s", f.Key)
		}
		if err := cost(f.BuildCost); err != nil {
			return err
		}
		if err := cost(f.UpgradeCost); err != nil {
			return err
		}
		known[f.Key] = f
	}
	seen := map[string]bool{}
	for _, r := range recipes {
		if !contract.ValidStableKey(r.Key) || r.Name == "" || seen[r.Key] || known[r.Facility].Key == "" || r.Level < 1 || r.Level > known[r.Facility].MaxLevel || r.Minutes < 1 || r.Minutes > 7*1440 {
			return fmt.Errorf("生产配方无效 %s", r.Key)
		}
		if err := cost(r.Inputs); err != nil {
			return err
		}
		if err := cost(r.Outputs); err != nil {
			return err
		}
		seen[r.Key] = true
	}
	return nil
}

func Find(recipes []Recipe, key string) (Recipe, bool) {
	for _, r := range recipes {
		if r.Key == key {
			return r, true
		}
	}
	return Recipe{}, false
}
func CanStart(stock, levels map[string]int64, jobs []Job, recipe Recipe) bool {
	if levels[recipe.Facility] < recipe.Level || !ruledata.CanPay(stock, recipe.Inputs) || len(jobs) >= 32 {
		return false
	}
	busy := int64(0)
	for _, j := range jobs {
		if j.Facility == recipe.Facility {
			busy++
		}
	}
	return busy < levels[recipe.Facility]
}
func Start(stock, levels map[string]int64, jobs []Job, recipe Recipe, now int64, key string) ([]Job, error) {
	if !CanStart(stock, levels, jobs, recipe) {
		return nil, contract.Reject("production.unavailable", "设施、材料或生产工位不足")
	}
	if err := ruledata.Pay(stock, recipe.Inputs); err != nil {
		return nil, err
	}
	return append(jobs, Job{Key: key, Recipe: recipe.Key, Facility: recipe.Facility, Due: now + recipe.Minutes}), nil
}
func Complete(stock map[string]int64, jobs []Job, recipes []Recipe, now int64) ([]Job, []string, error) {
	pending := []Job{}
	completed := []string{}
	for _, job := range jobs {
		if job.Due > now {
			pending = append(pending, job)
			continue
		}
		r, ok := Find(recipes, job.Recipe)
		if !ok {
			return nil, nil, fmt.Errorf("队列配方缺失")
		}
		for key, n := range r.Outputs {
			if err := ruledata.Add(stock, key, n); err != nil {
				return nil, nil, err
			}
		}
		completed = append(completed, job.Key)
	}
	return pending, completed, nil
}
