package ascensionrules

import (
	"fmt"

	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/encounterrules"
	"github.com/ialtone/fatecraft/internal/storage"
)

type View struct {
	Ready     bool             `json:"ready"`
	Passed    bool             `json:"passed"`
	CanFinish bool             `json:"can_finish"`
	Reason    string           `json:"reason"`
	Title     string           `json:"title"`
	Materials []map[string]any `json:"materials"`
}

func FromState(state *storage.CampaignDomainState) (*Config, error) {
	if state == nil {
		return nil, nil
	}
	d, exists := state.Definition(DefinitionKind, DefinitionKey)
	if !exists {
		return nil, nil
	}
	c, err := Parse(d.Content)
	return &c, err
}

func Inspect(row storage.Campaign, state *storage.CampaignDomainState) (*View, error) {
	c, err := FromState(state)
	if err != nil || c == nil {
		return nil, err
	}
	v := &View{Title: "最终飞升渡劫", Materials: []map[string]any{}}
	realm, exists := state.Definition(storage.DefinitionRealm, c.RealmKey)
	if !exists {
		return nil, fmt.Errorf("飞升境界定义缺失")
	}
	completed := map[string]bool{}
	for _, q := range state.Quests {
		completed[q.QuestKey] = q.Status == "completed"
	}
	readyErr := Ready(*c, state.Character, realm.Content, completed)
	for _, encounter := range state.Encounters {
		if encounter.EncounterKey == c.EncounterKey && encounter.Status == "won" {
			v.Passed = true
		}
	}
	if readyErr != nil {
		v.Reason = readyErr.Error()
	}
	v.CanFinish = v.Passed && readyErr == nil && row.Status == storage.CampaignStatusActive
	if v.CanFinish {
		v.Reason = "已渡过最终天劫，可选择飞升道路"
	}
	d, exists := state.Definition(storage.DefinitionEncounter, c.EncounterKey)
	trial, valid := encounterrules.Parse(d.Content)
	if !exists || !valid {
		return nil, fmt.Errorf("最终天劫遭遇定义无效")
	}
	if !v.Passed && v.Reason == "" && row.CurrentLocationKey != trial.LocationKey {
		v.Reason = "请到渡劫地点准备引劫"
	}
	for _, m := range c.Materials {
		name := m.ItemKey
		if item, ok := state.Definition(storage.DefinitionItem, m.ItemKey); ok {
			name = item.Name
		}
		quantity := int64(0)
		for _, stack := range state.ItemStacks {
			if stack.ItemKey == m.ItemKey && stack.QualityKey == m.Quality {
				quantity += stack.Quantity
			}
		}
		v.Materials = append(v.Materials, map[string]any{"name": name, "required": m.Quantity, "owned": quantity})
		if !v.Passed && v.Reason == "" && quantity < m.Quantity {
			v.Reason = "渡劫准备材料不足"
		}
	}
	for _, encounter := range state.Encounters {
		if encounter.Status == "active" && v.Reason == "" {
			v.Reason = "请先解决当前遭遇"
		}
	}
	v.Ready = row.Status == storage.CampaignStatusActive && v.Reason == "" && !v.Passed
	if v.Ready {
		v.Reason = "准备就绪，可由玩家确认引动天劫"
	}
	return v, nil
}
