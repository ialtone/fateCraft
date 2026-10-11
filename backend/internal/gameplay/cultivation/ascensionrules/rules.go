// Package ascensionrules owns the common gates for long-running cultivation
// endings. A completed encounter row, not free text or an unrelated roll,
// proves that this Campaign survived its final tribulation.
package ascensionrules

import (
	"fmt"
	"slices"

	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/storage"
)

const DefinitionKind = "cultivation.ascension"
const DefinitionKey = "ascension"
const ActionBegin = "cultivation.begin_ascension"

type Material struct {
	ItemKey  string `json:"item_key"`
	Quality  string `json:"quality"`
	Quantity int64  `json:"quantity" minimum:"1" maximum:"10000"`
}

type Config struct {
	RealmKey     string     `json:"realm_key"`
	EncounterKey string     `json:"encounter_key"`
	QuestGroups  [][]string `json:"quest_groups"`
	Materials    []Material `json:"materials"`
	OutcomeKeys  []string   `json:"outcome_keys"`
}

func Parse(raw map[string]any) (Config, error) {
	c, err := ruledata.Decode[Config](raw)
	if err != nil {
		return c, err
	}
	if !contract.ValidStableKey(c.RealmKey) || !contract.ValidStableKey(c.EncounterKey) || len(c.QuestGroups) < 1 || len(c.QuestGroups) > 8 || len(c.Materials) < 1 || len(c.Materials) > 8 || len(c.OutcomeKeys) < 1 || len(c.OutcomeKeys) > 8 {
		return c, fmt.Errorf("飞升境界、渡劫遭遇、主线、材料或终局配置无效")
	}
	seen := map[string]bool{}
	for _, group := range c.QuestGroups {
		if len(group) < 1 || len(group) > 8 {
			return c, fmt.Errorf("飞升主线条件组需包含 1–8 个备选任务")
		}
		for _, key := range group {
			if !contract.ValidStableKey(key) || seen[key] {
				return c, fmt.Errorf("飞升主线任务无效或重复: %s", key)
			}
			seen[key] = true
		}
	}
	seen = map[string]bool{}
	for _, m := range c.Materials {
		key := m.ItemKey + ":" + m.Quality
		if !contract.ValidStableKey(m.ItemKey) || !contract.ValidStableKey(m.Quality) || m.Quantity < 1 || m.Quantity > 10000 || seen[key] {
			return c, fmt.Errorf("渡劫材料无效或重复")
		}
		seen[key] = true
	}
	seen = map[string]bool{}
	for _, key := range c.OutcomeKeys {
		if !contract.ValidStableKey(key) || seen[key] {
			return c, fmt.Errorf("飞升终局无效或重复")
		}
		seen[key] = true
	}
	return c, nil
}

func Schema() map[string]any {
	return ruledata.Schema[Config](map[string]string{"realm_key": "圆满境界", "encounter_key": "最终渡劫遭遇", "quest_groups": "必需主线（各组至少完成一项）", "materials": "每次引劫材料", "outcome_keys": "飞升结局", "item_key": "物品", "quality": "品质", "quantity": "数量"})
}

// Ready checks the same facts for starting a trial and claiming an ending.
// Materials are consumed only when starting, and are not charged again at end.
func Ready(c Config, character *storage.CampaignCharacter, realm map[string]any, completed map[string]bool) error {
	if character == nil || character.Health <= 0 || character.AgeDays >= character.LifespanDays {
		return fmt.Errorf("角色状态不允许飞升")
	}
	if character.RealmKey != c.RealmKey {
		return fmt.Errorf("需要达到终极境界并修至圆满")
	}
	rank, validRank := integer(realm["rank"])
	stages, validStages := realm["stages"].([]any)
	if !validRank || int64(character.RealmRank) != rank || !validStages || len(stages) == 0 || character.StageKey != stages[len(stages)-1] {
		return fmt.Errorf("境界身份与圆满阶段不一致")
	}
	required, ok := integer(realm["cultivation_required"])
	if !ok || required < 1 || character.CultivationRequired != required || character.CultivationProgress < required {
		return fmt.Errorf("当前修为尚未达到圆满")
	}
	if realm["post_realm_transition"] != "tribulation_ascension" {
		return fmt.Errorf("当前境界未开放飞升渡劫")
	}
	for index, group := range c.QuestGroups {
		met := slices.ContainsFunc(group, func(key string) bool { return completed[key] })
		if !met {
			return fmt.Errorf("尚未完成第 %d 组主线目标", index+1)
		}
	}
	return nil
}

func integer(value any) (int64, bool) {
	switch n := value.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		return int64(n), n == float64(int64(n))
	default:
		return 0, false
	}
}
