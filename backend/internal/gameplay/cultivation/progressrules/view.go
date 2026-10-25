package progressrules

import (
	"fmt"
	"strings"

	"github.com/ialtone/fatecraft/internal/storage"
)

func Views(state *storage.CampaignDomainState) ([]map[string]any, error) {
	views := []map[string]any{}
	if state == nil {
		return views, nil
	}
	d, ok := state.Definition(DefinitionKind, DefinitionKey)
	if !ok {
		return views, nil
	}
	c, err := Parse(d.Content)
	if err != nil {
		return nil, err
	}
	for _, stage := range c.Stages {
		parts := []string{}
		for _, realm := range state.Definitions {
			if realm.Kind == storage.DefinitionRealm {
				var rank int
				switch n := realm.Content["rank"].(type) {
				case float64:
					rank = int(n)
				case int:
					rank = n
				case int64:
					rank = int(n)
				default:
					continue
				}
				if rank == stage.MinRealm {
					parts = append(parts, "达到"+realm.Name)
					break
				}
			}
		}
		for _, group := range stage.QuestGroups {
			names := []string{}
			for _, key := range group {
				q, _ := state.Definition(storage.DefinitionQuest, key)
				names = append(names, q.Name)
			}
			parts = append(parts, "完成 "+strings.Join(names, " / "))
		}
		for _, key := range stage.EncounterKeys {
			encounter, _ := state.Definition(storage.DefinitionEncounter, key)
			parts = append(parts, "战胜"+encounter.Name)
		}
		status := "尚未达成"
		for _, entry := range state.StageProgress {
			if entry.StageKey == stage.Key {
				status = fmt.Sprintf("已达成 · 结束时结算 %d 积分", stage.Points)
			}
		}
		for _, claim := range state.StageRewards {
			if claim.StageKey == stage.Key {
				status = fmt.Sprintf("已结算 · 到账 %d 积分", claim.GrantedPoints)
				if claim.DisabledReason == storage.RewardsDisabledReasonClientRoute {
					status += "（客户端直连）"
				}
			}
		}
		views = append(views, map[string]any{"key": stage.Key, "name": stage.Name, "points": stage.Points, "requirements": strings.Join(parts, "；"), "status": status})
	}
	return views, nil
}
