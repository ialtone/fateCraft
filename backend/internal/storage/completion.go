package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

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
	Chronicle             []string           `json:"chronicle,omitempty"`
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
	return &CompletionView{ID: fmt.Sprintf("%s/%d/%d", identity, conv.BranchEpoch, revision), ConversationID: conv.ID, BranchEpoch: conv.BranchEpoch, Revision: revision, Key: outcome.OutcomeKey, Category: outcome.Category, Title: outcome.Title, Tone: outcome.Tone, Summary: "本局已按可信玩法规则结束。", Chronicle: []string{}, Rewards: []CompletionReward{}, Metrics: []CompletionMetric{}, NextActions: []string{"replay", "back_to_challenges"}}
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
	if state, stateErr := s.DomainState(ctx, row); stateErr == nil && state != nil {
		view.Chronicle = campaignLifeChronicle(state, row, outcome, stagePoints)
		view.Summary = campaignCompletionSummary(state, row, outcome, stagePoints)
	}
	return view, nil
}

var campaignMainlineQuests = map[string]bool{
	"cave-rescue":                 true,
	"foundation-preparation":      true,
	"county-stability-choice":     true,
	"hunt-abyssal-isle":           true,
	"seal-rift-heart":             true,
	"great-ascension-preparation": true,
	"warden-ending":               true,
	"seeker-ending":               true,
}

func campaignLifeChronicle(state *CampaignDomainState, row Campaign, outcome ChallengeOutcomeRule, stagePoints int64) []string {
	if state == nil || state.Character == nil {
		return nil
	}
	character := state.Character
	lines := make([]string, 0, 8)
	if realm := realmStageName(state, character.RealmKey, character.StageKey); realm != "" {
		lines = append(lines, "境界："+realm)
	}
	if location := definitionName(state, DefinitionLocation, row.CurrentLocationKey); location != "" {
		lines = append(lines, "终点："+location)
	}
	lines = append(lines, fmt.Sprintf("寿元：%d / %d 天", character.AgeDays, character.LifespanDays))
	if len(state.StageRewards) == 0 {
		lines = append(lines, "阶段：未达成")
	} else {
		parts := make([]string, 0, len(state.StageRewards))
		for _, reward := range state.StageRewards {
			parts = append(parts, fmt.Sprintf("%s（+%d 分）", reward.Name, reward.GrantedPoints))
		}
		lines = append(lines, "阶段："+strings.Join(parts, "、"))
	}
	mainline := make([]string, 0, 6)
	for _, quest := range state.Quests {
		if quest.Status != "completed" || !campaignMainlineQuests[quest.QuestKey] {
			continue
		}
		if name := definitionName(state, DefinitionQuest, quest.QuestKey); name != "" {
			mainline = append(mainline, name)
		}
	}
	if len(mainline) > 0 {
		lines = append(lines, "主线："+strings.Join(mainline, "、"))
	}
	for _, encounter := range state.Encounters {
		if encounter.Status == "won" && encounter.EncounterKey == "final-ascension-tribulation" {
			lines = append(lines, "天劫：九霄劫云 · 渡过")
			break
		}
	}
	if outcome.OutcomeKey == "abandoned" {
		lines = append(lines, fmt.Sprintf("终局：中止求道（保留阶段积分 %d 分）", stagePoints))
	} else {
		lines = append(lines, "终局："+outcome.Title)
	}
	return lines
}

func campaignCompletionSummary(state *CampaignDomainState, row Campaign, outcome ChallengeOutcomeRule, stagePoints int64) string {
	if state == nil || state.Character == nil {
		return "本局已按可信玩法规则结束。"
	}
	character := state.Character
	realm := realmStageName(state, character.RealmKey, character.StageKey)
	switch {
	case row.Status == CampaignStatusAbandoned:
		return fmt.Sprintf("玩家主动终止本世，止步于%s，保留阶段积分 %d 分。", realm, stagePoints)
	case outcome.OutcomeKey == "lifespan_exhausted":
		return fmt.Sprintf("本世寿元耗尽，止步于%s。", realm)
	case row.Status == CampaignStatusDead:
		return fmt.Sprintf("本世在%s止步，气血耗尽。", realm)
	default:
		return fmt.Sprintf("本世历经 %d 天，最终%s。", character.AgeDays, outcome.Title)
	}
}

func definitionName(state *CampaignDomainState, kind, key string) string {
	if state == nil || strings.TrimSpace(key) == "" {
		return ""
	}
	definition, ok := state.Definition(kind, key)
	if !ok {
		return key
	}
	if strings.TrimSpace(definition.Name) != "" {
		return definition.Name
	}
	return key
}

func realmStageName(state *CampaignDomainState, realmKey, stageKey string) string {
	definition, ok := state.Definition(DefinitionRealm, realmKey)
	if !ok {
		return realmKey
	}
	name := definition.Name
	if strings.TrimSpace(name) == "" {
		name = realmKey
	}
	stages := lifeStrings(definition.Content["stages"])
	labels := lifeStrings(definition.Content["stage_labels"])
	for index, stage := range stages {
		if stage == stageKey && index < len(labels) {
			return name + " · " + labels[index]
		}
	}
	return name
}

func lifeStrings(value any) []string {
	values, _ := value.([]any)
	out := make([]string, 0, len(values))
	for _, raw := range values {
		if text, ok := raw.(string); ok && text != "" {
			out = append(out, text)
		}
	}
	return out
}
