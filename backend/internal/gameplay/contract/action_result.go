package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	ActionResultSchemaVersion = 1
	MaxActionResultBytes      = 65536
)

// ActionResult is the stable, player-safe description of one committed domain
// action. It is rebuilt from the same before/after projections used by the
// module narrative feedback, so the model never has to infer post-action state
// from prose or from a partial event stream.
type ActionResult struct {
	SchemaVersion  int            `json:"schema_version"`
	ActionID       string         `json:"action_id"`
	ActionKey      string         `json:"action_key"`
	ActorType      string         `json:"actor_type"`
	Revision       int64          `json:"revision"`
	StateHash      string         `json:"state_hash,omitempty"`
	GameTime       int64          `json:"game_time"`
	GoalStatus     string         `json:"goal_status"`
	Summary        string         `json:"summary"`
	Changes        []FactChange   `json:"changes"`
	CurrentFacts   map[string]any `json:"current_facts"`
	Preserved      []FactRef      `json:"preserved,omitempty"`
	Constraints    ClaimRuleSet   `json:"constraints,omitempty"`
	DetailsPartial bool           `json:"details_partial,omitempty"`
}

type FactChange struct {
	Path   string `json:"path"`
	Label  string `json:"label,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Before any    `json:"before"`
	After  any    `json:"after"`
}

type FactRef struct {
	Path   string `json:"path"`
	Label  string `json:"label,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type ClaimRuleSet struct {
	Allowed   []string `json:"allowed,omitempty"`
	Forbidden []string `json:"forbidden,omitempty"`
}

// TurnNarrativeContext binds one assistant narrative to every action that was
// committed during the turn. A text-only turn carries no actions and still
// binds the current revision so a stale generation can be rejected.
type TurnNarrativeContext struct {
	CampaignID   string         `json:"campaign_id"`
	RevisionFrom int64          `json:"revision_from"`
	RevisionTo   int64          `json:"revision_to"`
	StateHash    string         `json:"state_hash"`
	Actions      []ActionResult `json:"actions"`
}

type ActionResultMeta struct {
	ActionID  string
	ActionKey string
	ActorType string
	Revision  int64
	StateHash string
	GameTime  int64
	Summary   string
}

// BuildActionResult promotes the existing narrative_feedback map into the
// persistent contract. Unknown or missing fields degrade to an empty but valid
// result; callers that require rich facts must assert them in tests.
func BuildActionResult(feedback map[string]any, meta ActionResultMeta) (ActionResult, error) {
	result := ActionResult{
		SchemaVersion: ActionResultSchemaVersion,
		ActionID:      strings.TrimSpace(meta.ActionID),
		ActionKey:     strings.TrimSpace(meta.ActionKey),
		ActorType:     strings.TrimSpace(meta.ActorType),
		Revision:      meta.Revision,
		StateHash:     strings.TrimSpace(meta.StateHash),
		GameTime:      meta.GameTime,
		Changes:       []FactChange{},
		CurrentFacts:  map[string]any{},
		Preserved:     []FactRef{},
	}
	if feedback != nil {
		if raw, ok := feedback["changes"]; ok && raw != nil {
			encoded, err := json.Marshal(raw)
			if err != nil {
				return ActionResult{}, fmt.Errorf("序列化动作变化: %w", err)
			}
			if err := json.Unmarshal(encoded, &result.Changes); err != nil {
				return ActionResult{}, fmt.Errorf("解析动作变化: %w", err)
			}
		}
		if raw, ok := feedback["current_facts"]; ok && raw != nil {
			facts, err := normalizeJSONObject(raw)
			if err != nil {
				return ActionResult{}, fmt.Errorf("解析当前事实: %w", err)
			}
			result.CurrentFacts = facts
		}
		if partial, ok := feedback["details_partial"].(bool); ok {
			result.DetailsPartial = partial
		}
		if succeeded, ok := feedback["goal_succeeded"].(bool); ok {
			if succeeded {
				result.GoalStatus = "completed"
			} else {
				result.GoalStatus = "failed"
			}
		}
	}
	for index := range result.Changes {
		change := &result.Changes[index]
		change.Path = strings.TrimSpace(change.Path)
		if change.Label == "" {
			change.Label = FactLabel(change.Path)
		}
		if change.Kind == "" {
			change.Kind = inferFactKind(change.Before, change.After)
		}
	}
	if result.GoalStatus == "" {
		if len(result.Changes) == 0 {
			result.GoalStatus = "unchanged"
		} else {
			result.GoalStatus = "progressed"
		}
	}
	if strings.TrimSpace(meta.Summary) == "" {
		meta.Summary = defaultActionSummary(result)
	}
	result.Summary = strings.TrimSpace(meta.Summary)
	result.Preserved = derivePreserved(result.CurrentFacts, result.Changes)
	result.Constraints = deriveClaimRules(result)
	if err := result.Validate(); err != nil {
		return ActionResult{}, err
	}
	return result, nil
}

func (r ActionResult) Validate() error {
	if r.SchemaVersion <= 0 {
		return fmt.Errorf("动作结果 schema_version 无效")
	}
	if strings.TrimSpace(r.ActionKey) == "" {
		return fmt.Errorf("动作结果缺少 action_key")
	}
	if r.Revision < 0 {
		return fmt.Errorf("动作结果 revision 无效")
	}
	for _, change := range r.Changes {
		if strings.TrimSpace(change.Path) == "" {
			return fmt.Errorf("动作结果包含空事实路径")
		}
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if len(encoded) > MaxActionResultBytes {
		return fmt.Errorf("动作结果超过 %d 字节上限", MaxActionResultBytes)
	}
	return nil
}

func (c TurnNarrativeContext) Validate() error {
	if strings.TrimSpace(c.CampaignID) == "" {
		return fmt.Errorf("叙事上下文缺少 campaign_id")
	}
	if c.RevisionFrom < 0 || c.RevisionTo < c.RevisionFrom {
		return fmt.Errorf("叙事上下文 revision 范围无效")
	}
	encoded, err := json.Marshal(c)
	if err != nil {
		return err
	}
	if len(encoded) > MaxActionResultBytes*4 {
		return fmt.Errorf("叙事上下文超过容量上限")
	}
	for _, action := range c.Actions {
		if err := action.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func normalizeJSONObject(raw any) (map[string]any, error) {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var out map[string]any
	if err := decoder.Decode(&out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

func inferFactKind(before, after any) string {
	switch value := after.(type) {
	case nil:
		if before == nil {
			return "unknown"
		}
		return "unknown"
	case bool:
		return "boolean"
	case string:
		return "string"
	case json.Number:
		return "number"
	case float64, float32, int, int32, int64, uint, uint32, uint64:
		return "number"
	case []any:
		return "set"
	case map[string]any:
		return "object"
	default:
		_ = value
		return "unknown"
	}
}

func defaultActionSummary(result ActionResult) string {
	if len(result.Changes) == 0 {
		return "已结算动作 " + result.ActionKey
	}
	parts := make([]string, 0, 3)
	for _, change := range result.Changes {
		if len(parts) == 3 {
			break
		}
		label := change.Label
		if label == "" {
			label = change.Path
		}
		parts = append(parts, label+"："+FactValueText(change.Before)+" → "+FactValueText(change.After))
	}
	return "动作 " + result.ActionKey + " 已结算；" + strings.Join(parts, "；")
}

var preservedFactPaths = []string{
	"character.realm_name",
	"character.stage_name",
	"character.health",
	"character.max_health",
	"character.mana",
	"character.max_mana",
	"character.spiritual_sense",
	"character.injuries",
	"character.age_days",
	"character.lifespan_days",
	"character.cultivation_progress",
}

func derivePreserved(facts map[string]any, changes []FactChange) []FactRef {
	if len(facts) == 0 {
		return nil
	}
	changed := func(path string) bool {
		for _, change := range changes {
			if change.Path == path ||
				strings.HasPrefix(change.Path, path+".") ||
				strings.HasPrefix(path, change.Path+".") {
				return true
			}
		}
		return false
	}
	out := make([]FactRef, 0, len(preservedFactPaths))
	for _, path := range preservedFactPaths {
		value, ok := facts[path]
		if !ok || value == nil || changed(path) {
			continue
		}
		out = append(out, FactRef{Path: path, Label: FactLabel(path), Reason: "本次动作未改变"})
	}
	return out
}

func deriveClaimRules(result ActionResult) ClaimRuleSet {
	rules := ClaimRuleSet{}
	if len(result.Changes) > 0 {
		for _, change := range result.Changes {
			if change.Label != "" {
				rules.Allowed = append(rules.Allowed, change.Label+"发生变化")
			}
		}
	}
	preserved := map[string]bool{}
	for _, fact := range result.Preserved {
		preserved[fact.Path] = true
	}
	if preserved["character.realm_name"] || preserved["character.stage_name"] {
		rules.Forbidden = append(rules.Forbidden, "境界或阶段发生变化")
	}
	if preserved["character.injuries"] {
		rules.Forbidden = append(rules.Forbidden, "伤势痊愈或清除")
	}
	if preserved["character.health"] {
		rules.Forbidden = append(rules.Forbidden, "气血已恢复或回满")
	}
	if preserved["character.lifespan_days"] {
		rules.Forbidden = append(rules.Forbidden, "寿元增加或减少")
	}
	if preserved["character.mana"] {
		rules.Forbidden = append(rules.Forbidden, "法力已恢复或回满")
	}
	if preserved["character.spiritual_sense"] {
		rules.Forbidden = append(rules.Forbidden, "神识发生变化")
	}
	if result.GoalStatus == "failed" {
		rules.Forbidden = append(rules.Forbidden, "行动成功")
	}
	rules.Allowed = dedupeStrings(rules.Allowed)
	rules.Forbidden = dedupeStrings(rules.Forbidden)
	return rules
}

func dedupeStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	seen := map[string]bool{}
	out := values[:0]
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

var factLabels = map[string]string{
	"character.realm_name":           "境界",
	"character.stage_name":           "阶段",
	"character.health":               "气血",
	"character.max_health":           "气血上限",
	"character.mana":                 "法力",
	"character.max_mana":             "法力上限",
	"character.spiritual_sense":      "神识",
	"character.injuries":             "伤势",
	"character.age_days":             "年龄",
	"character.lifespan_days":        "寿元",
	"character.cultivation_progress": "修为",
	"character.cultivation_required": "突破所需修为",
	"character.foundation_quality":   "根基",
	"campaign.game_time":             "游戏时间",
	"campaign.location":              "所在地点",
	"campaign.status":                "存档状态",
}

func FactLabel(path string) string {
	if label, ok := factLabels[path]; ok {
		return label
	}
	if index := strings.LastIndex(path, "."); index >= 0 {
		return path[index+1:]
	}
	if path == "" {
		return ""
	}
	return path
}

func FactValueText(value any) string {
	switch typed := value.(type) {
	case nil:
		return "无"
	case string:
		if typed == "" {
			return "空"
		}
		return typed
	case bool:
		if typed {
			return "是"
		}
		return "否"
	case json.Number:
		return typed.String()
	case float64:
		return fmt.Sprintf("%g", typed)
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed)
		}
		return string(encoded)
	}
}
