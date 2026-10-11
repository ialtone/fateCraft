package campaignactions

import (
	"context"
	"errors"

	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldactions"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/progressrules"
	"github.com/ialtone/fatecraft/internal/storage"
)

func progressFacts(execution campaign.ActionExecution) (*progressrules.Config, []progressrules.Stage, error) {
	d, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, progressrules.DefinitionKind, progressrules.DefinitionKey)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	c, err := progressrules.Parse(d.Content)
	if err != nil {
		return nil, nil, err
	}
	character, err := cultivationStore(execution).Character(execution.Campaign.ID)
	if err != nil {
		return nil, nil, err
	}
	ready := []progressrules.Stage{}
	if character.Health <= 0 || character.AgeDays >= character.LifespanDays {
		return &c, ready, nil
	}
	for _, stage := range c.Stages {
		quests, wins := map[string]bool{}, map[string]bool{}
		for _, group := range stage.QuestGroups {
			for _, key := range group {
				q, err := execution.Store.Quest(execution.Campaign.ID, key)
				if err != nil && !errors.Is(err, storage.ErrNotFound) {
					return nil, nil, err
				}
				quests[key] = err == nil && q.Status == "completed"
			}
		}
		for _, key := range stage.EncounterKeys {
			count, err := execution.Store.EncounterWinCount(execution.Campaign.ID, key)
			if err != nil {
				return nil, nil, err
			}
			wins[key] = count > 0
		}
		if !progressrules.Ready(stage, character.RealmRank, quests, wins) {
			break
		}
		ready = append(ready, stage)
	}
	return &c, ready, nil
}

func requireFinalProgress(execution campaign.ActionExecution) error {
	c, ready, err := progressFacts(execution)
	if err != nil {
		return err
	}
	if c != nil && len(ready) != len(c.Stages) {
		return worldactions.Rejected("最终飞升还需要完成本版本的全部阶段事实")
	}
	return nil
}

func settleProgressRewards(ctx context.Context, execution campaign.ActionExecution, out *storage.CampaignActionOutcome) error {
	_, ready, err := progressFacts(execution)
	if err != nil {
		return err
	}
	for _, stage := range ready {
		row := execution.Campaign
		row.GameTime += out.GameTimeDelta
		reward, applied, err := cultivationStore(execution).GrantCampaignStageReward(ctx, row, stage.Key, stage.Name, stage.Points)
		if err != nil {
			return err
		}
		if !applied {
			continue
		}
		out.Events = append(out.Events, storage.CampaignEventDraft{EventType: "campaign.stage_rewarded", Visibility: storage.CampaignVisibilityPlayer, Payload: storage.JSONMap{"stage_key": reward.StageKey, "name": reward.Name, "configured_points": reward.ConfiguredPoints, "granted_points": reward.GrantedPoints, "disabled_reason": reward.DisabledReason}})
		if out.Result == nil {
			out.Result = storage.JSONMap{}
		}
		points, _ := out.Result["stage_points_awarded"].(int64)
		out.Result["stage_points_awarded"] = points + reward.GrantedPoints
	}
	return nil
}
