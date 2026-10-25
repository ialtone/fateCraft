package challenge

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/google/uuid"
	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/kernel"
	"github.com/ialtone/fatecraft/internal/state"
	"github.com/ialtone/fatecraft/internal/storage"
)

type ModuleActionResult struct {
	Completion        *storage.CompletionView `json:"completion,omitempty"`
	Campaign          *storage.Campaign       `json:"campaign,omitempty"`
	Narrative         *gameplay.ActionResult  `json:"narrative,omitempty"`
	Revision          int64                   `json:"revision"`
	Result            map[string]any          `json:"result"`
	Events            []gameplay.RuntimeEvent `json:"events"`
	Settlement        *SettlementResult       `json:"settlement,omitempty"`
	NarrativeFeedback map[string]any          `json:"narrative_feedback,omitempty"`
	// StageProgress are the milestones first reached by this action on the
	// current branch. PendingStagePoints is only their expected configured
	// total; it never increases an account balance. The full branch progress is
	// available from the snapshot and ending endpoints.
	StageProgress      []storage.StageProgressView `json:"stage_progress,omitempty"`
	PendingStagePoints int64                       `json:"pending_stage_points"`
	// StageRewards and StageRewardPoints are filled only when this action
	// performed the terminal settlement. Conversation totals live in the
	// completion receipt.
	StageRewards      []storage.ChallengeStageReward `json:"stage_rewards,omitempty"`
	StageRewardPoints int64                          `json:"stage_reward_points"`
}

// campaignStageFields decodes the stage fields a Campaign handler placed in
// its persisted action result. Campaign receipts only exist after settlement,
// so a terminal action returns them through the completion receipt instead.
type campaignStageFields struct {
	StageProgress      []storage.StageProgressView `json:"stage_progress"`
	PendingStagePoints int64                       `json:"pending_stage_points"`
}

type CampaignModuleExecutor interface {
	ExecutePlayerModuleInTx(context.Context, *storage.ChallengeRepository, *storage.Conversation, *gameplay.RuntimeContract, int64, string, map[string]any) (*storage.CampaignActionResult, error)
}

func (s *Service) SetCampaignModuleExecutor(executor CampaignModuleExecutor) {
	s.campaignModules = executor
}

// ExecuteModuleInTx is shared by authenticated player actions and agent
// tools. Its repositories must belong to the caller's single transaction.
func (s *Service) ExecuteModuleInTx(ctx context.Context, states *storage.StateRepo, repo *storage.ChallengeRepository, conv *storage.Conversation, content gameplay.Content, revision int64, actor, key string, arguments map[string]any) (*ModuleActionResult, error) {
	if content.Runtime.Mode != gameplay.RuntimeModeCampaign && !slices.Contains(content.Runtime.Actions.Domain, key) {
		return nil, gameplay.Reject("action.forbidden", "当前内容版本没有启用此操作")
	}
	module, err := s.registry.Module(content.Runtime.Engine)
	if err != nil {
		return nil, err
	}
	if content.Runtime.Mode == gameplay.RuntimeModeCampaign {
		if s.campaignModules == nil || conv.CampaignID == nil || actor != "player" {
			return nil, fmt.Errorf("长期模块玩家适配器不可用")
		}
		committed, err := s.campaignModules.ExecutePlayerModuleInTx(ctx, repo, conv, content.Runtime, revision, key, arguments)
		if err != nil {
			return nil, err
		}
		result := &ModuleActionResult{Revision: committed.Campaign.Revision, Result: committed.Result, Campaign: &committed.Campaign, Narrative: committed.Narrative, Completion: storage.CompletionFromResult(committed.Result)}
		if raw, encodeErr := json.Marshal(committed.Result); encodeErr == nil {
			var fields campaignStageFields
			if json.Unmarshal(raw, &fields) == nil {
				result.StageProgress = fields.StageProgress
				result.PendingStagePoints = fields.PendingStagePoints
			}
		}
		if result.Completion != nil {
			result.StageRewards = result.Completion.StageRewards
			result.StageRewardPoints = result.Completion.StageRewardPoints
			result.StageProgress, result.PendingStagePoints = nil, 0
		}
		return result, nil
	}
	if module.Runtime == nil {
		return nil, fmt.Errorf("可信模块执行器未安装")
	}
	if conv.CampaignID != nil {
		return nil, fmt.Errorf("单局模块不能操作长期存档")
	}
	trusted, err := state.GetInTx(ctx, states, conv.ID, content.Runtime.StateSchema)
	if err != nil {
		return nil, err
	}
	envelope, err := kernel.ReadEnvelope(content, trusted)
	if err != nil {
		return nil, err
	}
	beforeView, beforePartial, err := kernel.NarrativeView(module.Runtime, content, envelope.State)
	if err != nil {
		return nil, err
	}
	next, transition, err := kernel.ExecuteEnvelope(module.Runtime, content, envelope, revision, actor, key, arguments)
	if err != nil {
		return nil, err
	}
	afterView, afterPartial, err := kernel.NarrativeView(module.Runtime, content, next.State)
	if err != nil {
		return nil, err
	}
	feedback, err := gameplay.NarrativeFeedback(key, envelope.State.Revision, next.State.Revision, beforeView, afterView, kernel.NarrativeFactPaths(module.Runtime), transition.Result, beforePartial || afterPartial)
	if err != nil {
		return nil, err
	}
	patch, err := kernel.EnvelopePatch(module.Runtime, content, next)
	if err != nil {
		return nil, err
	}
	after, _, err := state.ApplyInTx(ctx, states, conv.ID, content.Runtime.StateSchema, patch, storage.SourceModule, nil)
	if err != nil {
		return nil, err
	}
	_, reached, err := s.recordStageRewards(ctx, repo, conv, content, after, key)
	if err != nil {
		return nil, err
	}
	disabledReason := ""
	if conv.LLMRoute == storage.LLMRouteClient {
		disabledReason = storage.RewardsDisabledReasonClientRoute
	}
	progressViews, pendingPoints := stageProgressViewsForReached(reached, disabledReason)
	events := []gameplay.RuntimeEvent{}
	for _, event := range transition.Events {
		if event.Visibility == gameplay.VisibilityPlayer || actor == "model" && event.Visibility == gameplay.VisibilityModel {
			events = append(events, event)
		}
	}
	narrative, err := gameplay.BuildActionResult(feedback, gameplay.ActionResultMeta{
		ActionID:  fmt.Sprintf("%s:%d", key, transition.State.Revision),
		ActionKey: key, ActorType: actor, Revision: transition.State.Revision,
	})
	if err != nil {
		return nil, err
	}
	result := &ModuleActionResult{Revision: transition.State.Revision, Result: transition.Result, Events: events, NarrativeFeedback: feedback, Narrative: &narrative, StageProgress: progressViews, PendingStagePoints: pendingPoints}
	if transition.State.Status != gameplay.RunActive {
		playerID, err := uuid.Parse(conv.PlayerID)
		if err != nil {
			return nil, err
		}
		result.Settlement, err = s.SettleWithRepository(ctx, repo, SettlementInput{ConversationID: conv.ID, PlayerID: playerID, OutcomeKey: transition.State.OutcomeKey, Summary: "可信玩法动作已达成结局。", TrustedState: after})
		if err != nil {
			return nil, err
		}
		settled := result.Settlement
		result.StageProgress, result.PendingStagePoints = nil, 0
		result.StageRewards, result.StageRewardPoints = settled.StageRewards, settled.StageRewardPoints
		view := storage.NewCompletion(*conv, result.Revision, "challenge:"+settled.Run.ID.String(), storage.ChallengeOutcomeRule{OutcomeKey: settled.Outcome.Key, Category: settled.Outcome.Category, Title: settled.Outcome.Title, Tone: settled.Outcome.Tone})
		view.Summary, view.RewardsDisabledReason = settled.Run.Summary, settled.RewardsDisabledReason
		view.Chronicle = []string{
			"结局：" + settled.Outcome.Title,
			"经过：" + settled.Run.Summary,
		}
		for _, reward := range settled.Rewards {
			view.Rewards = append(view.Rewards, storage.CompletionReward{RewardKey: reward.RewardKeySnapshot, Name: reward.NameSnapshot, Unit: reward.UnitSnapshot, Icon: reward.IconSnapshot, Quantity: reward.Quantity, BalanceAfter: reward.BalanceAfter})
		}
		view.RewardPoints = storage.TerminalRewardPoints(view.Rewards)
		if err := repo.FillStageRewards(ctx, view); err != nil {
			return nil, err
		}
		result.Completion = view
	}
	return result, nil
}
