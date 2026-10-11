package contentrules

import (
	"fmt"
	"slices"

	"github.com/ialtone/fatecraft/internal/gameplay/components/worldcontent"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldrules"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/ascensionrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/progressrules"
)

func validateProgressRewards(content contract.Content) error {
	catalog := map[string]contract.Definition{}
	var config *progressrules.Config
	for _, d := range content.Definitions {
		catalog[d.Kind+":"+d.Key] = d
		if d.Kind == progressrules.DefinitionKind {
			if config != nil || d.Key != progressrules.DefinitionKey {
				return fmt.Errorf("阶段奖励只能使用唯一 progress-rewards 定义")
			}
			c, err := progressrules.Parse(d.Content)
			if err != nil {
				return err
			}
			config = &c
		}
	}
	if config == nil {
		return nil
	}
	ascension, err := ascensionrules.Parse(catalog[ascensionrules.DefinitionKind+":"+ascensionrules.DefinitionKey].Content)
	if err != nil {
		return fmt.Errorf("阶段奖励需绑定可信飞升终局")
	}
	if len(ascension.OutcomeKeys) != len(config.OutcomeKeys) {
		return fmt.Errorf("阶段奖励须覆盖所有飞升结局")
	}
	for _, key := range ascension.OutcomeKeys {
		if !slices.Contains(config.OutcomeKeys, key) {
			return fmt.Errorf("阶段奖励缺少飞升结局 %s", key)
		}
	}
	var points int64
	maxRank, _ := worldcontent.NonNegativeDefinitionInt(catalog["realm:"+ascension.RealmKey].Content["rank"])
	previousRank := 0
	for _, stage := range config.Stages {
		if stage.MinRealm < previousRank || int64(stage.MinRealm) > maxRank {
			return fmt.Errorf("阶段境界必须顺序递增且不超过最终境界")
		}
		previousRank = stage.MinRealm
		points += stage.Points
		for _, group := range stage.QuestGroups {
			for _, key := range group {
				q, ok := worldrules.ParseQuest(catalog["quest:"+key].Content)
				if !ok || q.CampaignOutcomeKey != "" {
					return fmt.Errorf("阶段任务 %s 不存在或依赖终局", key)
				}
			}
		}
		for _, key := range stage.EncounterKeys {
			if _, ok := catalog["encounter:"+key]; !ok || key == ascension.EncounterKey {
				return fmt.Errorf("阶段遭遇 %s 不存在或依赖最终天劫", key)
			}
		}
	}
	for _, key := range config.OutcomeKeys {
		found := false
		for _, o := range content.Outcomes {
			if o.Key == key && o.Enabled {
				found = true
				if points+o.RewardPoints != config.TotalPoints {
					return fmt.Errorf("阶段积分与结局 %s 合计必须等于完整通关预算 %d", key, config.TotalPoints)
				}
			}
		}
		if !found {
			return fmt.Errorf("阶段奖励结局 %s 未启用", key)
		}
	}
	return nil
}
