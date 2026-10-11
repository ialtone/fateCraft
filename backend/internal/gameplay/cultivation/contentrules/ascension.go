package contentrules

import (
	"fmt"
	"slices"

	"github.com/ialtone/fatecraft/internal/gameplay/components/worldcontent"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldrules"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/ascensionrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/campaigncontract"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/encounterrules"
)

func validateAscension(content contract.Content) error {
	definitions := map[string]contract.Definition{}
	var config *ascensionrules.Config
	needsRules := false
	for _, d := range content.Definitions {
		definitions[d.Kind+":"+d.Key] = d
		if d.Kind == "realm" && d.Content["post_realm_transition"] != nil {
			needsRules = true
		}
		if d.Kind == "quest" && d.Content["campaign_outcome_key"] != nil {
			needsRules = true
		}
		if d.Kind == ascensionrules.DefinitionKind {
			if d.Key != ascensionrules.DefinitionKey || config != nil {
				return fmt.Errorf("飞升规则必须唯一且使用 ascension 标识")
			}
			parsed, err := ascensionrules.Parse(d.Content)
			if err != nil {
				return err
			}
			config = &parsed
		}
	}
	if config == nil {
		if needsRules {
			return fmt.Errorf("修仙终局或渡劫境界缺少可信飞升规则")
		}
		return nil
	}
	c := *config
	if content.Runtime == nil || content.Runtime.Campaign == nil || !slices.Contains(content.Runtime.Campaign.ActionAllowlist, ascensionrules.ActionBegin) {
		return fmt.Errorf("飞升规则需要启用引动天劫行动")
	}
	for _, key := range []string{campaigncontract.ActionEncounterAct, campaigncontract.ActionEncounterFlee, campaigncontract.ActionAcceptQuest, campaigncontract.ActionSubmitQuest, campaigncontract.ActionPractice, campaigncontract.ActionBreakthrough, campaigncontract.ActionLearnTechnique, campaigncontract.ActionEquipTechnique, campaigncontract.ActionTravel, campaigncontract.ActionInvestigate} {
		if !slices.Contains(content.Runtime.Campaign.ActionAllowlist, key) {
			return fmt.Errorf("完整飞升流程缺少必要行动 %s", key)
		}
	}
	realm, ok := definitions["realm:"+c.RealmKey]
	if !ok || realm.Content["post_realm_transition"] != "tribulation_ascension" || realm.Content["next_realm_key"] != nil {
		return fmt.Errorf("飞升必须绑定最后一个支持渡劫的境界")
	}
	finalRank, validRank := worldcontent.NonNegativeDefinitionInt(realm.Content["rank"])
	if !validRank {
		return fmt.Errorf("飞升境界级别无效")
	}
	for _, d := range content.Definitions {
		if d.Kind == "realm" {
			rank, valid := worldcontent.NonNegativeDefinitionInt(d.Content["rank"])
			if !valid || rank > finalRank {
				return fmt.Errorf("飞升必须位于版本定义的最高境界")
			}
		}
		if d.Kind == "realm" && d.Key != c.RealmKey && d.Content["post_realm_transition"] != nil {
			return fmt.Errorf("只有飞升规则指定的最终境界可以声明渡劫")
		}
	}
	encounter, ok := definitions["encounter:"+c.EncounterKey]
	trial, valid := encounterrules.Parse(encounter.Content)
	if !ok || !valid || len(trial.Rewards) > 0 {
		return fmt.Errorf("渡劫需要有效的无重复掉落遭遇")
	}
	for _, group := range c.QuestGroups {
		for _, key := range group {
			d, ok := definitions["quest:"+key]
			q, valid := worldrules.ParseQuest(d.Content)
			if !ok || !valid || q.CampaignOutcomeKey != "" {
				return fmt.Errorf("飞升前置任务不存在或依赖终局: %s", key)
			}
			for _, objective := range q.Objectives {
				if objective.EncounterKey == c.EncounterKey {
					return fmt.Errorf("飞升前置任务不能依赖尚未开放的天劫")
				}
			}
		}
	}
	for _, m := range c.Materials {
		d, ok := definitions["item:"+m.ItemKey]
		if !ok || d.Content["stackable"] != true || d.Content["model_grantable"] != false {
			return fmt.Errorf("渡劫材料必须来自可信产出且可堆叠: %s", m.ItemKey)
		}
	}
	endingSources := map[string]bool{}
	for _, d := range content.Definitions {
		if d.Kind != "quest" {
			continue
		}
		q, valid := worldrules.ParseQuest(d.Content)
		if valid && q.CampaignOutcomeKey != "" {
			if !slices.Contains(c.OutcomeKeys, q.CampaignOutcomeKey) {
				return fmt.Errorf("修仙终局缺少共同飞升门槛: %s", q.CampaignOutcomeKey)
			}
			endingSources[q.CampaignOutcomeKey] = true
		}
	}
	for _, key := range c.OutcomeKeys {
		if !endingSources[key] || !slices.ContainsFunc(content.Outcomes, func(o contract.Outcome) bool { return o.Enabled && o.Key == key }) {
			return fmt.Errorf("飞升结局没有启用的规则或任务: %s", key)
		}
	}
	return nil
}
