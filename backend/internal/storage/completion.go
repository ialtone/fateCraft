package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// CompletionView is a durable, public receipt, shared by both gameplay modes.
// It never decides an outcome or computes a new reward while being read.
type CompletionView struct {
	ID                    string             `json:"id"`
	ConversationID        uuid.UUID          `json:"conversation_id"`
	BranchEpoch           int64              `json:"branch_epoch"`
	Revision              int64              `json:"revision"`
	Key                   string             `json:"key"`
	Category              string             `json:"category"`
	Title                 string             `json:"title"`
	Tone                  string             `json:"tone"`
	Summary               string             `json:"summary"`
	Rewards               []CompletionReward `json:"rewards"`
	Metrics               []CompletionMetric `json:"metrics"`
	NextActions           []string           `json:"next_actions"`
	RewardsDisabledReason string             `json:"rewards_disabled_reason,omitempty"`
}

type CompletionReward struct {
	RewardKey    string `json:"reward_key"`
	Name         string `json:"name"`
	Unit         string `json:"unit"`
	Icon         string `json:"icon"`
	Quantity     int64  `json:"quantity"`
	BalanceAfter int64  `json:"balance_after"`
}

type CompletionMetric struct {
	Label string `json:"label"`
	Value any    `json:"value"`
	Unit  string `json:"unit,omitempty"`
}

func NewCompletion(conv Conversation, revision int64, identity string, outcome ChallengeOutcomeRule) *CompletionView {
	return &CompletionView{ID: fmt.Sprintf("%s/%d/%d", identity, conv.BranchEpoch, revision), ConversationID: conv.ID, BranchEpoch: conv.BranchEpoch, Revision: revision, Key: outcome.OutcomeKey, Category: outcome.Category, Title: outcome.Title, Tone: outcome.Tone, Summary: "本局已按可信玩法规则结束。", Rewards: []CompletionReward{}, Metrics: []CompletionMetric{}, NextActions: []string{"replay", "back_to_challenges"}}
}

func CompletionFromResult(result JSONMap) *CompletionView {
	raw, err := json.Marshal(result["completion"])
	if err != nil {
		return nil
	}
	var view *CompletionView
	if json.Unmarshal(raw, &view) != nil {
		return nil
	}
	return view
}

// Completion only reads receipts belonging to the current, effective branch.
// Immutable historical rewards are deliberately not used to infer an ending.
func (r *ChallengeRepository) Completion(ctx context.Context, conv Conversation) (*CompletionView, error) {
	if conv.Status != StatusEnded {
		return nil, nil
	}
	var raw []byte
	err := r.db.WithContext(ctx).Raw(`SELECT receipt FROM (
		SELECT c.result->'completion' receipt, c.created_at FROM module_action_commits c
		JOIN conversation_turns t ON t.id=c.turn_id
		WHERE c.conversation_id=? AND c.branch_epoch=? AND t.invalidated_at IS NULL AND t.status IN ('completed','partial') AND jsonb_exists(c.result, 'completion')
		UNION ALL
		SELECT a.result_json->'data'->'completion', a.created_at FROM agent_tool_invocations a
		JOIN conversation_turns t ON t.id=a.turn_id
		WHERE a.conversation_id=? AND a.status='committed' AND t.invalidated_at IS NULL AND t.status IN ('completed','partial') AND jsonb_exists(a.result_json->'data', 'completion')
		UNION ALL
		SELECT c.result->'completion', c.created_at FROM campaign_action_commits c
		JOIN conversations v ON v.campaign_id=c.campaign_id
		JOIN conversation_turns t ON t.id=c.turn_id
		WHERE v.id=? AND t.invalidated_at IS NULL AND t.status IN ('completed','partial') AND jsonb_exists(c.result, 'completion')
	) receipts WHERE receipt->>'branch_epoch'=? ORDER BY created_at DESC LIMIT 1`, conv.ID, conv.BranchEpoch, conv.ID, conv.ID, fmt.Sprint(conv.BranchEpoch)).Row().Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var view CompletionView
	if err := json.Unmarshal(raw, &view); err != nil {
		return nil, err
	}
	if view.ConversationID != conv.ID || view.BranchEpoch != conv.BranchEpoch {
		return nil, ErrModuleActionConflict
	}
	return &view, nil
}

// CampaignCompletion is invoked inside the existing action transaction.
func (s *CampaignActionStore) CampaignCompletion(ctx context.Context, row Campaign, actionID string) (*CompletionView, error) {
	var conv Conversation
	if err := s.tx.WithContext(ctx).First(&conv, "campaign_id = ?", row.ID).Error; err != nil {
		return nil, err
	}
	var outcome ChallengeOutcomeRule
	if err := s.tx.WithContext(ctx).First(&outcome, "challenge_version_id=? AND outcome_key=? AND enabled=true", row.ChallengeVersionID, row.OutcomeKey).Error; err != nil {
		return nil, err
	}
	view := NewCompletion(conv, row.Revision, "campaign:"+row.ID.String()+":"+actionID, outcome)
	if row.Status == CampaignStatusAbandoned {
		view.Summary = "玩家主动终止本局；保留此前已到账的阶段积分，不发放终局积分。"
	}
	if row.LLMRoute == LLMRouteClient {
		view.RewardsDisabledReason = RewardsDisabledReasonClientRoute
	}
	if outcome.Category == "success" {
		rewards, err := loadCampaignOutcomeRewards(ctx, s.tx, row)
		if err != nil {
			return nil, err
		}
		for _, reward := range rewards {
			view.Rewards = append(view.Rewards, CompletionReward{RewardKey: reward.Key, Name: reward.Name, Unit: reward.Unit, Icon: reward.Icon, Quantity: reward.Quantity, BalanceAfter: reward.Balance})
		}
	}
	var stagePoints int64
	if err := s.tx.WithContext(ctx).Raw("SELECT COALESCE(SUM(granted_points),0) FROM campaign_stage_rewards WHERE campaign_id=?", row.ID).Scan(&stagePoints).Error; err != nil {
		return nil, err
	}
	view.Metrics = append(view.Metrics, CompletionMetric{Label: "已到账阶段积分（保留）", Value: stagePoints, Unit: "积分"})
	return view, nil
}
