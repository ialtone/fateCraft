package campaignactions

import (
	"context"
	"errors"

	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldactions"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/progressrules"
	"github.com/ialtone/fatecraft/internal/storage"
)

// progressFacts evaluates the fixed-version milestones against trusted domain
// facts. It deliberately does not short-circuit on death or lifespan: a goal
// completed by the same action that ended the run still counts, and death does
// not erase earlier branch progress.
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
	character, err := cultivationStore(execution).Character(execution.Campaign.ID)
	if err != nil {
		return err
	}
	if character.Health <= 0 || character.AgeDays >= character.LifespanDays {
		return worldactions.Rejected("当前状态无法踏上最终飞升道路")
	}
	if c != nil && len(ready) != len(c.Stages) {
		return worldactions.Rejected("最终飞升还需要完成本版本的全部阶段事实")
	}
	return nil
}

// recordProgressRewards stores milestones reached by the action on the current
// branch. It never writes an account ledger entry or a settlement receipt.
func recordProgressRewards(ctx context.Context, execution campaign.ActionExecution, out *storage.CampaignActionOutcome) error {
	c, ready, err := progressFacts(execution)
	if err != nil || c == nil {
		return err
	}
	row := execution.Campaign
	row.GameTime += out.GameTimeDelta
	keys := make([]string, 0, len(ready))
	for _, stage := range ready {
		keys = append(keys, stage.Key)
	}
	added, _, err := cultivationStore(execution).RecordCampaignStageProgress(ctx, row, keys)
	if err != nil {
		return err
	}
	if out.Result == nil {
		out.Result = storage.JSONMap{}
	}
	// Only report milestones first reached by this action; the snapshot
	// exposes the complete current branch progress.
	views, pending := progressViews(c, added, row.LLMRoute)
	out.Result["stage_progress"] = views
	out.Result["pending_stage_points"] = pending
	return nil
}

// settleProgressRewards converts every milestone recorded on the current branch
// into immutable settlement receipts. It is called once from the terminal
// action wrapper, before the completion receipt is assembled.
func settleProgressRewards(ctx context.Context, execution campaign.ActionExecution, out *storage.CampaignActionOutcome) error {
	c, _, err := progressFacts(execution)
	if err != nil || c == nil {
		return err
	}
	row := execution.Campaign
	row.GameTime += out.GameTimeDelta
	grants := make([]storage.StageRewardGrant, 0, len(c.Stages))
	for _, stage := range c.Stages {
		grants = append(grants, storage.StageRewardGrant{
			Key: stage.Key, Name: stage.Name, SortOrder: len(grants), Points: stage.Points,
			SourceBranchEpoch: row.Revision,
		})
	}
	rows, points, err := cultivationStore(execution).SettleCampaignStageRewards(ctx, row, grants)
	if err != nil {
		return err
	}
	if out.Result == nil {
		out.Result = storage.JSONMap{}
	}
	out.Result["stage_progress"] = []storage.StageProgressView{}
	out.Result["pending_stage_points"] = int64(0)
	out.Result["stage_rewards"] = rows
	out.Result["stage_reward_points"] = points
	return nil
}

func progressViews(c *progressrules.Config, progress []storage.CampaignStageProgress, route string) ([]storage.StageProgressView, int64) {
	byKey := make(map[string]progressrules.Stage, len(c.Stages))
	for _, stage := range c.Stages {
		byKey[stage.Key] = stage
	}
	views := make([]storage.StageProgressView, 0, len(progress))
	var pending int64
	for _, row := range progress {
		stage, ok := byKey[row.StageKey]
		if !ok {
			continue
		}
		disabled := ""
		if route == storage.LLMRouteClient {
			disabled = storage.RewardsDisabledReasonClientRoute
		}
		views = append(views, storage.StageProgressView{
			StageKey: stage.Key, Name: stage.Name, SortOrder: len(views),
			ConfiguredPoints: stage.Points, ReachedAt: row.CreatedAt, DisabledReason: disabled,
		})
		pending += stage.Points
	}
	return views, pending
}
