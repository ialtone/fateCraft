package orchestration

import (
	"context"
	"errors"

	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/realtime"
	"github.com/ialtone/fatecraft/internal/storage"
)

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
