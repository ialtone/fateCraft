package progressrules

import (
	"fmt"
	"strings"

	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
)

const DefinitionKind = "cultivation.progress_rewards"
const DefinitionKey = "progress-rewards"

type Stage struct {
	Key           string     `json:"key"`
	Name          string     `json:"name"`
	Points        int64      `json:"points" minimum:"0" maximum:"1000000"`
	MinRealm      int        `json:"min_realm_rank" minimum:"0" maximum:"1000"`
	QuestGroups   [][]string `json:"quest_groups"`
	EncounterKeys []string   `json:"encounter_keys"`
}
type Config struct {
	Stages      []Stage  `json:"stages"`
	OutcomeKeys []string `json:"outcome_keys"`
	TotalPoints int64    `json:"total_points" minimum:"0" maximum:"100000000"`
}

func Parse(raw map[string]any) (Config, error) {
	c, err := ruledata.Decode[Config](raw)
	if err != nil {
		return c, err
	}
	if len(c.Stages) < 1 || len(c.Stages) > 8 || len(c.OutcomeKeys) < 1 || len(c.OutcomeKeys) > 8 || c.TotalPoints < 0 || c.TotalPoints > 100000000 {
		return c, fmt.Errorf("阶段奖励配置规模或总额无效")
	}
	seen := map[string]bool{}
	for _, s := range c.Stages {
		if !contract.ValidStableKey(s.Key) || len(s.Key) > 64 || seen[s.Key] || strings.TrimSpace(s.Name) == "" || len([]rune(s.Name)) > 128 || s.Points < 0 || s.Points > 1000000 || s.MinRealm < 0 || s.MinRealm > 1000 || len(s.QuestGroups) < 1 || len(s.QuestGroups) > 8 || len(s.EncounterKeys) > 8 {
			return c, fmt.Errorf("阶段名称、积分、境界或条件无效")
		}
		seen[s.Key] = true
		for _, keys := range append(append([][]string{}, s.QuestGroups...), s.EncounterKeys) {
			if len(keys) == 0 {
				continue
			}
			refs := map[string]bool{}
			for _, key := range keys {
				if !contract.ValidStableKey(key) || refs[key] {
					return c, fmt.Errorf("阶段事实引用无效或重复")
				}
				refs[key] = true
			}
		}
		for _, group := range s.QuestGroups {
			if len(group) < 1 || len(group) > 8 {
				return c, fmt.Errorf("阶段任务条件组需要 1–8 项")
			}
		}
	}
	seen = map[string]bool{}
	for _, key := range c.OutcomeKeys {
		if !contract.ValidStableKey(key) || seen[key] {
			return c, fmt.Errorf("阶段终局配置无效")
		}
		seen[key] = true
	}
	return c, nil
}

func Schema() map[string]any {
	schema := ruledata.Schema[Config](map[string]string{"stages": "按顺序核验的阶段", "key": "阶段标识", "name": "阶段名称", "points": "本阶段积分", "min_realm_rank": "最低境界", "quest_groups": "必需任务（每组任一项完成）", "encounter_keys": "必需胜利的遭遇", "outcome_keys": "完整通关结局", "total_points": "完整通关积分总额"})
	// UI metadata describes budget fields without coupling the editor to a
	// particular engine. Authoritative equality is still checked at publication.
	schema["x-reward-budget"] = map[string]any{"stages": "stages", "points": "points", "outcomes": "outcome_keys", "total": "total_points"}
	return schema
}

func Ready(s Stage, rank int, quests, wins map[string]bool) bool {
	if rank < s.MinRealm {
		return false
	}
	for _, group := range s.QuestGroups {
		met := false
		for _, key := range group {
			met = met || quests[key]
		}
		if !met {
			return false
		}
	}
	for _, key := range s.EncounterKeys {
		if !wins[key] {
			return false
		}
	}
	return true
}
