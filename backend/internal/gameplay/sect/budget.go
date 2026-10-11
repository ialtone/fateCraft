package sect

import (
	"encoding/json"
	"fmt"

	"github.com/ialtone/fatecraft/internal/gameplay/components/production"
	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
)

// Budget probes deliberately unlock all resources and disciples. They are not
// playable saves: publication must cover options that only appear later, while
// real actions continue to validate actual costs, assignments and prerequisites.
func validateBudget(content contract.Content, c Config) error {
	stock := map[string]int64{}
	for key := range c.Resources {
		stock[key] = 1_000_000
	}
	maxima := map[string]int{}
	for _, levelMode := range []int{0, 1, 2} {
		members := map[string]Member{}
		recruited := []string{}
		for _, d := range c.Disciples {
			if levelMode != 0 || d.Initial {
				members[d.Key] = Member{Level: 1, Loyalty: 80}
				recruited = append(recruited, d.Key)
			}
		}
		facilities := map[string]int64{}
		for _, f := range c.Facilities {
			if levelMode == 1 {
				facilities[f.Key] = f.MaxLevel
			}
			if levelMode == 2 {
				facilities[f.Key] = max(1, f.MaxLevel-1)
			}
		}
		s := State{Stock: stock, Facilities: facilities, Disciples: members, Recruited: recruited, Crisis: "pending", Missions: map[string]int64{}, Jobs: []production.Job{}}
		data, err := ruledata.Object(s)
		if err != nil {
			return err
		}
		previews, err := (Module{}).Preview(content, contract.RunState{Data: data}, "player")
		if err != nil {
			return err
		}
		for _, preview := range previews {
			raw, err := json.Marshal(preview)
			if err != nil {
				return err
			}
			maxima[preview.Action.Key] = max(maxima[preview.Action.Key], len(raw))
		}
	}
	bytes := 2
	for _, n := range maxima {
		bytes += n + 1
	}
	if bytes > 32*1024 {
		return fmt.Errorf("宗门发展后的动作预览预计需要 %d 字节，超过 32768 字节；请缩短名称或减少弟子/配方/派遣组合", bytes)
	}
	return nil
}
