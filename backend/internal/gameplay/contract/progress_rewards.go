package contract

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ialtone/fatecraft/internal/accountasset"
	"github.com/ialtone/fatecraft/internal/jsonstrict"
)

// DefinitionKindChallengeProgressRewards is a platform-owned definition kind.
// Modules do not register it themselves; the platform validates it before the
// pinned module dispatch so every challenge pack can use the same contract.
const DefinitionKindChallengeProgressRewards = "challenge.progress_rewards"

const (
	progressRewardStageLimit = 8
	progressRewardNameLimit  = 128
)

// ProgressRewardStage is one trusted milestone of a single-run challenge.
// Reaching it only records branch progress; points are posted once, by the
// terminal settlement, and progress rolls back with the story branch.
type ProgressRewardStage struct {
	Key            string         `json:"key"`
	Name           string         `json:"name"`
	Order          int            `json:"order"`
	Points         int64          `json:"points"`
	VerifierKey    string         `json:"verifier_key"`
	VerifierConfig map[string]any `json:"verifier_config"`
}

// ProgressRewards is the decoded content of challenge.progress_rewards.
type ProgressRewards struct {
	Stages      []ProgressRewardStage `json:"stages"`
	OutcomeKeys []string              `json:"outcome_keys"`
	StageCap    int64                 `json:"stage_cap"`
}

func (p ProgressRewards) TotalPoints() int64 {
	var total int64
	for _, stage := range p.Stages {
		total += stage.Points
	}
	return total
}

// decodeProgressRewardsDefinition decodes the content of the platform kind.
func decodeProgressRewardsDefinition(definition Definition) (ProgressRewards, error) {
	raw, err := json.Marshal(definition.Content)
	if err != nil {
		return ProgressRewards{}, fmt.Errorf("阶段积分内容无法编码: %w", err)
	}
	var parsed ProgressRewards
	if err := jsonstrict.Decode(raw, &parsed); err != nil {
		return ProgressRewards{}, fmt.Errorf("阶段积分内容无效: %w", err)
	}
	return parsed, nil
}

// ParseProgressRewards decodes the single platform definition. A pack may omit
// it; declaring it twice is always an error so the grant path cannot pick an
// arbitrary rule set.
func ParseProgressRewards(content Content) (ProgressRewards, bool, error) {
	var found *Definition
	for index := range content.PlatformDefinitions {
		if content.PlatformDefinitions[index].Kind != DefinitionKindChallengeProgressRewards {
			continue
		}
		if found != nil {
			return ProgressRewards{}, false, fmt.Errorf("每个版本最多只能声明一条 %s", DefinitionKindChallengeProgressRewards)
		}
		found = &content.PlatformDefinitions[index]
	}
	if found == nil {
		return ProgressRewards{}, false, nil
	}
	parsed, err := decodeProgressRewardsDefinition(*found)
	if err != nil {
		return ProgressRewards{}, true, err
	}
	return parsed, true, nil
}

// validateProgressRewardsDefinition checks the structural rules that do not
// require the outcome table. Outcome references and the reward budget are
// checked by ValidateContent once both definitions and outcomes are known.
func (r *Registry) validateProgressRewardsDefinition(contract *RuntimeContract, definition Definition) error {
	if contract == nil {
		return fmt.Errorf("缺少 Runtime")
	}
	mode := NormalizeRuntimeMode(contract.Mode)
	if mode != RuntimeModeChallenge && mode != RuntimeModeCampaign {
		return fmt.Errorf("%s 只能用于可信玩法运行时", DefinitionKindChallengeProgressRewards)
	}
	parsed, err := decodeProgressRewardsDefinition(definition)
	if err != nil {
		return err
	}
	if len(parsed.Stages) == 0 || len(parsed.Stages) > progressRewardStageLimit {
		return fmt.Errorf("阶段数量必须在 1 到 %d 之间", progressRewardStageLimit)
	}
	if len(parsed.OutcomeKeys) == 0 || len(parsed.OutcomeKeys) > 64 {
		return fmt.Errorf("outcome_keys 必须包含 1 到 64 个成功结局")
	}
	seenOutcomes := map[string]bool{}
	for _, key := range parsed.OutcomeKeys {
		if !ValidStableKey(key) || seenOutcomes[key] {
			return fmt.Errorf("outcome_keys 包含无效或重复的结局 %q", key)
		}
		seenOutcomes[key] = true
	}
	seenKeys := map[string]bool{}
	seenOrders := map[int]bool{}
	for index, stage := range parsed.Stages {
		if !ValidStableKey(stage.Key) || len(stage.Key) > 64 || seenKeys[stage.Key] {
			return fmt.Errorf("阶段 %d 的 key 无效或重复", index+1)
		}
		seenKeys[stage.Key] = true
		if strings.TrimSpace(stage.Name) == "" || len([]rune(stage.Name)) > progressRewardNameLimit {
			return fmt.Errorf("阶段 %s 的名称无效", stage.Key)
		}
		if stage.Order < 0 || stage.Order > 1000000 || seenOrders[stage.Order] {
			return fmt.Errorf("阶段 %s 的 order 无效或重复", stage.Key)
		}
		seenOrders[stage.Order] = true
		if stage.Points < 1 || stage.Points > accountasset.MaxQuantity {
			return fmt.Errorf("阶段 %s 的积分必须是 1 到 %d 的安全整数", stage.Key, accountasset.MaxQuantity)
		}
		if stage.VerifierKey == "" {
			return fmt.Errorf("阶段 %s 缺少 verifier", stage.Key)
		}
		if err := r.ValidateVerifier(contract, stage.VerifierKey, stage.VerifierConfig); err != nil {
			return fmt.Errorf("阶段 %s: %v", stage.Key, err)
		}
		if err := rejectTerminalConditionPaths(stage.VerifierConfig); err != nil {
			return fmt.Errorf("阶段 %s: %v", stage.Key, err)
		}
		if mode == RuntimeModeCampaign {
			ownership, err := CompileOwnershipRegistry(contract)
			if err != nil {
				return err
			}
			for _, path := range conditionPaths(stage.VerifierConfig) {
				field, ok := ownership.Resolve(path)
				if !ok || field.Owner != OwnerCampaign || field.Visibility != VisibilityPlayer {
					return fmt.Errorf("阶段 %s 只能依赖玩家可见的 Campaign 存档字段: %s", stage.Key, path)
				}
			}
			continue
		}
		for _, path := range conditionPaths(stage.VerifierConfig) {
			field, ok := FieldForPath(contract.StateSchema, path)
			if !ok || (field.UpdatePolicy != UpdateModule && field.UpdatePolicy != UpdateSystem && field.UpdatePolicy != UpdateImmutable) {
				return fmt.Errorf("阶段 %s 只能依赖服务端可信状态: %s", stage.Key, path)
			}
		}
	}
	if parsed.StageCap < 1 || parsed.StageCap > accountasset.MaxQuantity {
		return fmt.Errorf("stage_cap 必须是正整数")
	}
	if parsed.TotalPoints() > parsed.StageCap {
		return fmt.Errorf("阶段积分合计 %d 超过 stage_cap %d", parsed.TotalPoints(), parsed.StageCap)
	}
	return nil
}

// validateProgressRewardsBudget is called once outcomes are available.
func validateProgressRewardsBudget(content Content) error {
	parsed, found, err := ParseProgressRewards(content)
	if err != nil || !found {
		return err
	}
	lowest := int64(0)
	outcomes := map[string]Outcome{}
	for _, outcome := range content.Outcomes {
		if !outcome.Enabled {
			continue
		}
		outcomes[outcome.Key] = outcome
		if outcome.Category == "success" && outcome.RewardPoints > 0 && (lowest == 0 || outcome.RewardPoints < lowest) {
			lowest = outcome.RewardPoints
		}
	}
	if lowest == 0 {
		return fmt.Errorf("%s 需要至少一个正积分成功结局", DefinitionKindChallengeProgressRewards)
	}
	for _, key := range parsed.OutcomeKeys {
		outcome, ok := outcomes[key]
		if !ok {
			return fmt.Errorf("阶段积分引用了不存在的结局 %s", key)
		}
		if outcome.Category != "success" || outcome.RewardPoints <= 0 {
			return fmt.Errorf("阶段积分只能引用正积分成功结局: %s", key)
		}
	}
	if expected := lowest / 2; parsed.StageCap != expected {
		return fmt.Errorf("stage_cap 必须等于最低正积分成功结局 %d 的 50%%（%d），当前为 %d", lowest, expected, parsed.StageCap)
	}
	if parsed.TotalPoints() > parsed.StageCap {
		return fmt.Errorf("阶段积分合计 %d 超过上限 %d", parsed.TotalPoints(), parsed.StageCap)
	}
	return nil
}

// ProgressRewardsSchema publishes the platform editor for the kind. The
// x-reward-budget annotation has no total field on purpose: single-run routes
// have different terminal amounts, so the editable budget is the stage cap
// derived from the lowest positive success outcome.
func ProgressRewardsSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"stages", "outcome_keys", "stage_cap"},
		"properties": map[string]any{
			"stages": map[string]any{
				"type": "array", "title": "阶段", "maxItems": progressRewardStageLimit,
				"items": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"required":             []string{"key", "name", "order", "points", "verifier_key", "verifier_config"},
					"properties": map[string]any{
						"key":             map[string]any{"type": "string", "title": "标识", "maxLength": 64},
						"name":            map[string]any{"type": "string", "title": "名称", "maxLength": progressRewardNameLimit},
						"order":           map[string]any{"type": "integer", "title": "顺序", "minimum": 0, "maximum": 1000000},
						"points":          map[string]any{"type": "integer", "title": "积分", "minimum": 1, "maximum": accountasset.MaxQuantity},
						"verifier_key":    map[string]any{"type": "string", "title": "验证器", "maxLength": 128},
						"verifier_config": map[string]any{"type": "object", "title": "验证配置", "additionalProperties": true},
					},
				},
			},
			"outcome_keys": map[string]any{
				"type": "array", "title": "适用成功结局", "maxItems": 64,
				"items": map[string]any{"type": "string", "maxLength": 128},
			},
			"stage_cap": map[string]any{"type": "integer", "title": "阶段积分上限", "minimum": 1, "maximum": accountasset.MaxQuantity},
		},
		"x-reward-budget": map[string]any{"stages": "stages", "points": "points", "outcomes": "outcome_keys", "cap": "stage_cap"},
	}
}

func rejectTerminalConditionPaths(config map[string]any) error {
	for _, path := range conditionPaths(config) {
		normalized := strings.ToLower(strings.TrimSpace(path))
		if normalized == "" {
			return fmt.Errorf("条件 path 不能为空")
		}
		for _, prefix := range []string{"gameplay.outcome", "gameplay.outcome_key", "gameplay.death", "gameplay.death_cause", "module_state.status", "module_state.outcome_key", "narrative", "display", "message", "model"} {
			if normalized == prefix || strings.HasPrefix(normalized, prefix+".") {
				return fmt.Errorf("阶段条件不能依赖终局状态或展示文本: %s", path)
			}
		}
	}
	return nil
}

func conditionPaths(config map[string]any) []string {
	if config == nil {
		return nil
	}
	raw, ok := config["conditions"]
	if !ok {
		return nil
	}
	conditions, ok := raw.([]any)
	if !ok {
		return nil
	}
	paths := []string{}
	var walk func([]any)
	walk = func(items []any) {
		for _, item := range items {
			condition, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if path, ok := condition["path"].(string); ok {
				paths = append(paths, path)
			}
			if children, ok := condition["conditions"].([]any); ok {
				walk(children)
			}
		}
	}
	walk(conditions)
	return paths
}

// SortedProgressRewardStages returns stages in display order. The authored
// order is a declaration order, not a prerequisite chain.
func SortedProgressRewardStages(stages []ProgressRewardStage) []ProgressRewardStage {
	out := append([]ProgressRewardStage(nil), stages...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].Key < out[j].Key
	})
	return out
}
