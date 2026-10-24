package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/realtime"
	"github.com/ialtone/fatecraft/internal/storage"
)

// Tool results may be freshly normalized or replayed from storage. Both carry
// the same trusted narrative/feedback; never derive an action from model prose.
func (cache *attemptAssembleCache) recordModuleAction(outcome agentToolOutcome, actionKey string) error {
	if outcome.Status != storage.ToolInvocationCommitted || !outcome.Mutating {
		return nil
	}
	raw, err := json.Marshal(outcome.Result["data"])
	if err != nil {
		return err
	}
	var data struct {
		Narrative *gameplay.ActionResult `json:"narrative"`
		Feedback  map[string]any         `json:"narrative_feedback"`
		Revision  int64                  `json:"revision"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return err
	}
	if data.Narrative == nil {
		// Bounded tool results retain feedback even when other fields are cut.
		if data.Feedback == nil {
			return errors.New("已提交的玩法动作缺少可信叙事结果")
		}
		action, err := gameplay.BuildActionResult(data.Feedback, gameplay.ActionResultMeta{
			ActionID:  fmt.Sprintf("%s:%d", actionKey, data.Revision),
			ActionKey: actionKey, ActorType: "model", Revision: data.Revision,
		})
		if err != nil {
			return err
		}
		data.Narrative = &action
	}
	if err := data.Narrative.Validate(); err != nil {
		return err
	}
	for _, action := range cache.moduleActions {
		if action.ActionID == data.Narrative.ActionID {
			return nil
		}
	}
	cache.moduleActions = append(cache.moduleActions, *data.Narrative)
	return nil
}

func (g *Generator) executeModuleToolInTx(ctx context.Context, conv *storage.Conversation, binding agentToolBinding, arguments map[string]any, tx *agentToolTx) agentToolOutcome {
	failure := func(err error) agentToolOutcome {
		var violation *gameplay.RuleViolation
		if errors.As(err, &violation) {
			return agentToolOutcome{Status: storage.ToolInvocationRejected, Code: violation.Code, Result: storage.JSONMap{"ok": false, "code": violation.Code, "message": violation.Message, "data": rejectedActionFacts()}}
		}
		return agentToolOutcome{Status: storage.ToolInvocationFailed, Code: "module_commit_failed", Fatal: true, Result: storage.JSONMap{"ok": false, "code": "module_commit_failed", "message": "玩法动作未提交", "data": map[string]any{}}}
	}
	if tx == nil || binding.ModuleContent == nil || g.challenge == nil {
		return failure(errors.New("模块事务输入不完整"))
	}
	result, err := g.challenge.ExecuteModuleInTx(ctx, tx.states, tx.challenges, conv, *binding.ModuleContent, binding.CampaignRevision, "model", binding.ActionKey, arguments)
	if err != nil {
		return failure(err)
	}
	code := "module_committed"
	if result.Settlement != nil {
		code = "module_settled"
	}
	outcome := agentToolOutcome{Status: storage.ToolInvocationCommitted, Code: code, Mutating: true, Result: storage.JSONMap{"ok": true, "code": code, "message": "玩法动作已执行", "data": result}}
	if settlement := result.Settlement; settlement != nil {
		outcome.Events = append(outcome.Events, realtime.Event{Type: realtime.TypeChallengeCompleted, ConversationID: conv.ID.String(), Completion: result.Completion, RunID: settlement.Run.ID.String(), Outcome: map[string]any{"key": settlement.Outcome.Key, "category": settlement.Outcome.Category, "title": settlement.Outcome.Title, "summary": settlement.Run.Summary, "tone": settlement.Outcome.Tone, "rewards_disabled_reason": settlement.RewardsDisabledReason}, RewardGrants: settlement.Rewards, NextActions: []string{"replay", "back_to_challenges"}})
	}
	return outcome
}
