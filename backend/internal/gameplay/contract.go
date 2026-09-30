package gameplay

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ialtone/fatecraft/internal/storage"
)

const (
	RuntimeModeChallenge = "challenge"
	RuntimeModeCampaign  = "campaign"

	VisibilityPlayer   = "player"
	VisibilityModel    = "model"
	VisibilityInternal = "internal"

	UpdateModel     = "model"
	UpdateSystem    = "system"
	UpdateImmutable = "immutable"

	// State fields belong to the conversation runtime by default. Campaign
	// fields are declared separately in CampaignConfig and are never writable
	// through fc_state_* tools.
	OwnerConversation = "conversation"
	OwnerCampaign     = "campaign"
)

type EngineRef struct {
	Key     string `json:"key"`
	Version int    `json:"version"`
}

type StateSchema struct {
	SchemaVersion int          `json:"schema_version"`
	Fields        []StateField `json:"fields"`
}

type StateField struct {
	Path          string         `json:"path"`
	Type          string         `json:"type"`
	Owner         string         `json:"owner,omitempty"`
	Default       any            `json:"default,omitempty"`
	Visibility    string         `json:"visibility"`
	UpdatePolicy  string         `json:"update_policy"`
	AllowChildren bool           `json:"allow_children,omitempty"`
	Constraints   map[string]any `json:"constraints,omitempty"`
}

// FieldOwnership declares the authority for a public Campaign projection
// path. It is part of the immutable Runtime contract rather than frontend
// code, so tools, projections and release validation share one source.
type FieldOwnership struct {
	Path              string   `json:"path"`
	Owner             string   `json:"owner"`
	ValueType         string   `json:"value_type,omitempty"`
	AllowChildren     bool     `json:"allow_children,omitempty"`
	WriteCapability   string   `json:"write_capability,omitempty"`
	StorageProjection string   `json:"storage_projection,omitempty"`
	EventTypes        []string `json:"event_types,omitempty"`
	Visibility        string   `json:"visibility,omitempty"`
}

type Capability struct {
	Enabled       bool   `json:"enabled"`
	Label         string `json:"label"`
	Description   string `json:"description,omitempty"`
	CostRewardKey string `json:"cost_reward_key,omitempty"`
	CostQuantity  int64  `json:"cost_quantity,omitempty"`
}

type ActionConfig struct {
	Model        []string              `json:"model"`
	Player       []string              `json:"player"`
	Capabilities map[string]Capability `json:"capabilities,omitempty"`
	// Rolls 按判定 key 声明服务端前置条件与状态效果：证据类状态
	// （境界、飞升符等）只能经由判定结果由服务端写入，模型只负责叙事。
	Rolls map[string]RollRule `json:"rolls,omitempty"`
}

// RollRule 是一个判定 key 的服务端规则。Requires 在掷骰前用可信状态校验
// （不满足则判定不执行、不入流水）；Success/Failure 是判定结果落地后由
// 服务端以 system 身份应用的状态补丁（点号路径 → 绝对值，支持列表追加）。
type RollRule struct {
	Category        string         `json:"category"`
	BaseSuccessRate int            `json:"base_success_rate"`
	Sides           int            `json:"sides"`
	RewardValue     int            `json:"reward_value,omitempty"`
	Requires        map[string]any `json:"requires,omitempty"`
	Success         map[string]any `json:"success,omitempty"`
	Failure         map[string]any `json:"failure,omitempty"`
}

type SettlementConfig struct {
	Handler string `json:"handler"`
}

type StateViewField struct {
	Path   string `json:"path"`
	Label  string `json:"label"`
	Widget string `json:"widget"`
	Unit   string `json:"unit,omitempty"`
	Order  int    `json:"order"`
}

type StateSection struct {
	Key    string           `json:"key"`
	Title  string           `json:"title"`
	Order  int              `json:"order"`
	Fields []StateViewField `json:"fields"`
}

// CampaignView describes the player-facing layout of a long-running campaign.
// It is intentionally data-only: it may select public projection fields and
// submit registered actions, but it cannot grant permissions or perform
// settlement/authoritative calculations.
type CampaignView struct {
	Renderer    string                `json:"renderer"`
	Title       string                `json:"title,omitempty"`
	Header      CampaignViewHeader    `json:"header,omitempty"`
	Sections    []CampaignViewSection `json:"sections,omitempty"`
	Actions     []CampaignViewAction  `json:"actions,omitempty"`
	EmptyStates map[string]string     `json:"empty_states,omitempty"`
}

type CampaignViewHeader struct {
	TitlePath  string `json:"title_path,omitempty"`
	TimePath   string `json:"time_path,omitempty"`
	TimeFormat string `json:"time_format,omitempty"`
}

type CampaignViewSection struct {
	Key         string                 `json:"key"`
	Title       string                 `json:"title"`
	Order       int                    `json:"order"`
	VisibleWhen *CampaignViewCondition `json:"visible_when,omitempty"`
	Fields      []CampaignViewField    `json:"fields,omitempty"`
}

type CampaignViewField struct {
	Path           string                  `json:"path"`
	Label          string                  `json:"label"`
	Widget         string                  `json:"widget"`
	Unit           string                  `json:"unit,omitempty"`
	Order          int                     `json:"order"`
	ValueNamespace string                  `json:"value_namespace,omitempty"`
	VisibleWhen    *CampaignViewCondition  `json:"visible_when,omitempty"`
	ItemFields     []CampaignViewItemField `json:"item_fields,omitempty"`
	Sort           string                  `json:"sort,omitempty"`
	Limit          int                     `json:"limit,omitempty"`
}

type CampaignViewItemField struct {
	Path           string                  `json:"path"`
	Label          string                  `json:"label"`
	Widget         string                  `json:"widget,omitempty"`
	Unit           string                  `json:"unit,omitempty"`
	ValueNamespace string                  `json:"value_namespace,omitempty"`
	ItemFields     []CampaignViewItemField `json:"item_fields,omitempty"`
}

// CampaignViewCondition is a deliberately small, declarative condition
// language. Exactly one predicate is allowed; conditions are UI hints only.
type CampaignViewCondition struct {
	Path      string `json:"path"`
	Exists    *bool  `json:"exists,omitempty"`
	Equals    any    `json:"equals,omitempty"`
	NotEquals any    `json:"not_equals,omitempty"`
	Operator  string `json:"operator,omitempty"`
	Value     any    `json:"value,omitempty"`
}

type CampaignViewAction struct {
	Key         string                         `json:"key"`
	Label       string                         `json:"label"`
	Icon        string                         `json:"icon,omitempty"`
	Placement   string                         `json:"placement,omitempty"`
	SourcePath  string                         `json:"source_path,omitempty"`
	ItemPath    string                         `json:"item_path,omitempty"`
	VisibleWhen *CampaignViewCondition         `json:"visible_when,omitempty"`
	EnabledWhen *CampaignViewCondition         `json:"enabled_when,omitempty"`
	Arguments   map[string]CampaignViewBinding `json:"arguments,omitempty"`
}

type CampaignViewBinding struct {
	From  string `json:"from,omitempty"`
	Value any    `json:"value,omitempty"`
}

// Guidance is a topic-neutral set of bounded player prompts.  It is part of
// the immutable runtime presentation, not an authority or permission source.
// Message items are ordinary user input; action items are still checked by
// the registered capability/handler path on the server.
type Guidance struct {
	Groups []GuidanceGroup `json:"groups,omitempty"`
}

type GuidanceGroup struct {
	Key       string         `json:"key"`
	Title     string         `json:"title"`
	Placement string         `json:"placement,omitempty"`
	MaxItems  int            `json:"max_items,omitempty"`
	Items     []GuidanceItem `json:"items"`
}

type GuidanceItem struct {
	Key         string                         `json:"key"`
	Label       string                         `json:"label"`
	Description string                         `json:"description,omitempty"`
	Mode        string                         `json:"mode"`
	Message     string                         `json:"message,omitempty"`
	ActionKey   string                         `json:"action_key,omitempty"`
	VisibleWhen *CampaignViewCondition         `json:"visible_when,omitempty"`
	EnabledWhen *CampaignViewCondition         `json:"enabled_when,omitempty"`
	Arguments   map[string]CampaignViewBinding `json:"arguments,omitempty"`
	// Enabled is populated only in the public projection.  It is intentionally
	// omitted from authored runtime JSON and never grants server-side authority.
	Enabled *bool `json:"enabled,omitempty"`
}

type CompletionPresentation struct {
	ReplayLabel     string `json:"replay_label,omitempty"`
	BackLabel       string `json:"back_label,omitempty"`
	EmptyRewardText string `json:"empty_reward_text,omitempty"`
}

type Presentation struct {
	StatePanelTitle string                       `json:"state_panel_title"`
	StateSections   []StateSection               `json:"state_sections"`
	Completion      CompletionPresentation       `json:"completion_view"`
	Terminology     map[string]string            `json:"terminology,omitempty"`
	ValueLabels     map[string]map[string]string `json:"value_labels,omitempty"`
	CampaignView    *CampaignView                `json:"campaign_view,omitempty"`
	Guidance        *Guidance                    `json:"guidance,omitempty"`
}

type WorldClockConfig struct {
	CalendarKey         string `json:"calendar_key"`
	MaxDueEventsPerTurn int    `json:"max_due_events_per_turn"`
}

// CampaignConfig 绑定长期存档所使用的可信生命周期、投影和上下文策略。
// 这些 key 必须由对应玩法模块声明，不能由 Prompt 临时发明。
type CampaignConfig struct {
	LifecycleHandler   string           `json:"lifecycle_handler"`
	ProjectionHandler  string           `json:"projection_handler"`
	ContextPolicy      string           `json:"context_policy"`
	ActionAllowlist    []string         `json:"action_allowlist"`
	FieldOwnership     []FieldOwnership `json:"field_ownership,omitempty"`
	InitialLocationKey string           `json:"initial_location_key"`
	WorldClock         WorldClockConfig `json:"world_clock"`
}

type RuntimeContract struct {
	Engine       EngineRef        `json:"engine"`
	Mode         string           `json:"mode,omitempty"`
	Campaign     *CampaignConfig  `json:"campaign,omitempty"`
	StateSchema  StateSchema      `json:"state_schema"`
	Actions      ActionConfig     `json:"actions"`
	Settlement   SettlementConfig `json:"settlement"`
	Presentation Presentation     `json:"presentation"`
}

func DecodeRuntime(row storage.ChallengeRuntimeConfig) (*RuntimeContract, error) {
	var contract RuntimeContract
	contract.Engine = EngineRef{Key: row.EngineKey, Version: row.EngineVersion}
	contract.Mode = NormalizeRuntimeMode(row.Mode)
	if row.CampaignConfig != nil && len(*row.CampaignConfig) > 0 && string(*row.CampaignConfig) != "null" {
		var campaign CampaignConfig
		if err := json.Unmarshal(*row.CampaignConfig, &campaign); err != nil {
			return nil, fmt.Errorf("解析 campaign_config: %w", err)
		}
		contract.Campaign = &campaign
	}
	if err := json.Unmarshal(row.StateSchema, &contract.StateSchema); err != nil {
		return nil, fmt.Errorf("解析 state_schema: %w", err)
	}
	if err := json.Unmarshal(row.ActionConfig, &contract.Actions); err != nil {
		return nil, fmt.Errorf("解析 action_config: %w", err)
	}
	if err := json.Unmarshal(row.SettlementConfig, &contract.Settlement); err != nil {
		return nil, fmt.Errorf("解析 settlement_config: %w", err)
	}
	if err := json.Unmarshal(row.Presentation, &contract.Presentation); err != nil {
		return nil, fmt.Errorf("解析 presentation: %w", err)
	}
	return &contract, nil
}

func EncodeRuntime(contract RuntimeContract) (storage.ChallengeRuntimeConfig, error) {
	mode := NormalizeRuntimeMode(contract.Mode)
	var campaignConfig *storage.JSONDoc
	if contract.Campaign != nil {
		encoded, err := json.Marshal(contract.Campaign)
		if err != nil {
			return storage.ChallengeRuntimeConfig{}, err
		}
		document := storage.JSONDoc(encoded)
		campaignConfig = &document
	}
	stateSchema, err := json.Marshal(contract.StateSchema)
	if err != nil {
		return storage.ChallengeRuntimeConfig{}, err
	}
	actions, err := json.Marshal(contract.Actions)
	if err != nil {
		return storage.ChallengeRuntimeConfig{}, err
	}
	settlement, err := json.Marshal(contract.Settlement)
	if err != nil {
		return storage.ChallengeRuntimeConfig{}, err
	}
	presentation, err := json.Marshal(contract.Presentation)
	if err != nil {
		return storage.ChallengeRuntimeConfig{}, err
	}
	row := storage.ChallengeRuntimeConfig{
		EngineKey: contract.Engine.Key, EngineVersion: contract.Engine.Version,
		Mode: mode, CampaignConfig: campaignConfig,
		StateSchema: storage.JSONDoc(stateSchema), ActionConfig: storage.JSONDoc(actions),
		SettlementConfig: storage.JSONDoc(settlement), Presentation: storage.JSONDoc(presentation),
	}
	return row, nil
}

func NormalizeRuntimeMode(mode string) string {
	if strings.TrimSpace(mode) == "" {
		return RuntimeModeChallenge
	}
	return strings.TrimSpace(mode)
}

func ValidateContract(contract *RuntimeContract) error {
	if contract == nil || strings.TrimSpace(contract.Engine.Key) == "" || contract.Engine.Version <= 0 {
		return fmt.Errorf("engine 配置无效")
	}
	contract.Mode = NormalizeRuntimeMode(contract.Mode)
	switch contract.Mode {
	case RuntimeModeChallenge:
		if contract.Campaign != nil {
			return fmt.Errorf("challenge 模式不能声明 campaign 配置")
		}
	case RuntimeModeCampaign:
		if err := validateCampaignConfig(contract.Campaign); err != nil {
			return err
		}
	default:
		return fmt.Errorf("runtime mode 无效: %s", contract.Mode)
	}
	if contract.StateSchema.SchemaVersion <= 0 || len(contract.StateSchema.Fields) == 0 {
		return fmt.Errorf("state_schema 不能为空")
	}
	if len(contract.StateSchema.Fields) > 256 {
		return fmt.Errorf("state_schema 字段不能超过 256 个")
	}
	paths := map[string]StateField{}
	for _, field := range contract.StateSchema.Fields {
		if err := validateStateField(field); err != nil {
			return err
		}
		if _, exists := paths[field.Path]; exists {
			return fmt.Errorf("state_schema 路径重复: %s", field.Path)
		}
		paths[field.Path] = field
	}
	ownership, err := CompileOwnershipRegistry(contract)
	if err != nil {
		return err
	}
	for _, section := range contract.Presentation.StateSections {
		if strings.TrimSpace(section.Key) == "" || strings.TrimSpace(section.Title) == "" {
			return fmt.Errorf("状态展示分组缺少 key 或 title")
		}
		for _, item := range section.Fields {
			field, ok := paths[item.Path]
			if !ok {
				return fmt.Errorf("状态展示引用未知路径: %s", item.Path)
			}
			if field.Visibility != VisibilityPlayer {
				return fmt.Errorf("状态展示引用了非玩家可见路径: %s", item.Path)
			}
			switch item.Widget {
			case "text", "number", "progress", "badge", "boolean", "list", "item_list":
			default:
				return fmt.Errorf("状态展示控件无效: %s", item.Widget)
			}
		}
	}
	if err := validateValueLabels(contract.Presentation.ValueLabels); err != nil {
		return err
	}
	if contract.Presentation.Guidance != nil {
		if err := validateGuidance(*contract.Presentation.Guidance, contract); err != nil {
			return err
		}
		if contract.Mode == RuntimeModeCampaign {
			if err := validateGuidanceOwnership(*contract.Presentation.Guidance, ownership); err != nil {
				return err
			}
		}
	}
	if contract.Presentation.CampaignView != nil {
		if contract.Mode != RuntimeModeCampaign {
			return fmt.Errorf("campaign_view 只能用于 campaign runtime")
		}
		if err := validateCampaignView(*contract.Presentation.CampaignView, contract.Campaign); err != nil {
			return err
		}
		if err := validateCampaignViewOwnership(*contract.Presentation.CampaignView, ownership); err != nil {
			return err
		}
	}
	if contract.Mode == RuntimeModeChallenge && strings.TrimSpace(contract.Settlement.Handler) == "" {
		return fmt.Errorf("settlement.handler 不能为空")
	}
	for key, capability := range contract.Actions.Capabilities {
		if capability.CostQuantity < 0 || capability.CostQuantity > storage.MaxRewardQuantity ||
			(capability.CostQuantity > 0 && capability.CostRewardKey == "") ||
			(capability.CostRewardKey != "" && !storage.ValidRewardKey(capability.CostRewardKey)) {
			return fmt.Errorf("能力 %s 的消耗配置无效", key)
		}
	}
	for key, rule := range contract.Actions.Rolls {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("判定规则的 key 不能为空")
		}
		if strings.TrimSpace(key) != key || len([]rune(key)) > 128 {
			return fmt.Errorf("判定规则 key %q 必须去除首尾空白且不超过 128 个字符", key)
		}
		if strings.TrimSpace(rule.Category) == "" {
			return fmt.Errorf("判定 %s 的 category 不能为空", key)
		}
		if strings.TrimSpace(rule.Category) != rule.Category || len([]rune(rule.Category)) > 64 {
			return fmt.Errorf("判定 %s 的 category 必须去除首尾空白且不超过 64 个字符", key)
		}
		if rule.BaseSuccessRate < 1 || rule.BaseSuccessRate > 99 {
			return fmt.Errorf("判定 %s 的 base_success_rate 必须在 1 到 99 之间", key)
		}
		// 成功率以 1% 为粒度，只有 100 的倍数面才能精确表示；其他面数会扭曲概率。
		if rule.Sides < 100 || rule.Sides > 1000 || rule.Sides%100 != 0 {
			return fmt.Errorf("判定 %s 的 sides 必须是 100 到 1000 之间 100 的倍数", key)
		}
		if rule.RewardValue < 0 {
			return fmt.Errorf("判定 %s 的 reward_value 不能为负数", key)
		}
		for path, cond := range rule.Requires {
			if _, ok := FieldForPath(contract.StateSchema, path); !ok {
				return fmt.Errorf("判定 %s 的前置条件引用未知路径 %s", key, path)
			}
			if err := validateRequireCondition(cond); err != nil {
				return fmt.Errorf("判定 %s 的前置条件 %s 无效: %w", key, path, err)
			}
		}
		for label, patch := range map[string]map[string]any{"success": rule.Success, "failure": rule.Failure} {
			for patchKey, value := range patch {
				if err := validateEffectEntry(contract.StateSchema, patchKey, value); err != nil {
					return fmt.Errorf("判定 %s 的 %s 效果无效: %w", key, label, err)
				}
			}
		}
	}
	return nil
}

const (
	maxCampaignViewSections  = 32
	maxCampaignViewFields    = 256
	maxCampaignViewActions   = 64
	maxCampaignViewItems     = 16
	maxGuidanceGroups        = 16
	maxGuidanceItems         = 64
	maxGuidanceItemsPerGroup = 16
)

func validateGuidance(guidance Guidance, contract *RuntimeContract) error {
	if len(guidance.Groups) > maxGuidanceGroups {
		return fmt.Errorf("guidance 分组不能超过 %d 个", maxGuidanceGroups)
	}
	seenGroups := map[string]bool{}
	itemTotal := 0
	for _, group := range guidance.Groups {
		if !validStableKey(group.Key) || seenGroups[group.Key] {
			return fmt.Errorf("guidance 分组 key 无效或重复: %q", group.Key)
		}
		seenGroups[group.Key] = true
		if group.Title == "" || strings.TrimSpace(group.Title) != group.Title || len([]rune(group.Title)) > 128 {
			return fmt.Errorf("guidance 分组 %s 的 title 无效", group.Key)
		}
		if group.Placement != "" && !validStableKey(group.Placement) {
			return fmt.Errorf("guidance 分组 %s 的 placement 无效", group.Key)
		}
		if group.MaxItems < 0 || group.MaxItems > maxGuidanceItemsPerGroup {
			return fmt.Errorf("guidance 分组 %s 的 max_items 无效", group.Key)
		}
		if len(group.Items) > maxGuidanceItemsPerGroup {
			return fmt.Errorf("guidance 分组 %s 的项目不能超过 %d 个", group.Key, maxGuidanceItemsPerGroup)
		}
		seenItems := map[string]bool{}
		for _, item := range group.Items {
			itemTotal++
			if itemTotal > maxGuidanceItems {
				return fmt.Errorf("guidance 项目不能超过 %d 个", maxGuidanceItems)
			}
			if !validStableKey(item.Key) || seenItems[item.Key] {
				return fmt.Errorf("guidance 分组 %s 的 item key 无效或重复: %q", group.Key, item.Key)
			}
			seenItems[item.Key] = true
			if item.Enabled != nil {
				return fmt.Errorf("guidance 项目 %s 不能在 Runtime 配置中声明 enabled", item.Key)
			}
			if item.Label == "" || strings.TrimSpace(item.Label) != item.Label || len([]rune(item.Label)) > 128 {
				return fmt.Errorf("guidance 项目 %s 的 label 无效", item.Key)
			}
			if item.Description != "" && (strings.TrimSpace(item.Description) != item.Description || len([]rune(item.Description)) > 256) {
				return fmt.Errorf("guidance 项目 %s 的 description 无效", item.Key)
			}
			switch item.Mode {
			case "message":
				if item.Message == "" || strings.TrimSpace(item.Message) != item.Message || len([]rune(item.Message)) > 512 {
					return fmt.Errorf("guidance 项目 %s 的 message 无效", item.Key)
				}
				if item.ActionKey != "" {
					return fmt.Errorf("guidance 项目 %s 的 message 模式不能声明 action_key", item.Key)
				}
				if len(item.Arguments) > 0 {
					return fmt.Errorf("guidance 项目 %s 的 message 模式不能声明 arguments", item.Key)
				}
			case "action":
				if !validStableKey(item.ActionKey) {
					return fmt.Errorf("guidance 项目 %s 的 action_key 无效", item.Key)
				}
				if contract.Mode != RuntimeModeCampaign || contract.Campaign == nil || !containsStableKey(contract.Campaign.ActionAllowlist, item.ActionKey) {
					return fmt.Errorf("guidance 项目 %s 的 action_key 未加入 campaign action_allowlist", item.Key)
				}
				if item.Message != "" {
					return fmt.Errorf("guidance 项目 %s 的 action 模式不能声明 message", item.Key)
				}
			default:
				return fmt.Errorf("guidance 项目 %s 的 mode 无效: %q", item.Key, item.Mode)
			}
			if err := validateGuidanceCondition(item.VisibleWhen, contract); err != nil {
				return fmt.Errorf("guidance 项目 %s 的 visible_when 无效: %w", item.Key, err)
			}
			if err := validateGuidanceCondition(item.EnabledWhen, contract); err != nil {
				return fmt.Errorf("guidance 项目 %s 的 enabled_when 无效: %w", item.Key, err)
			}
			if len(item.Arguments) > 16 {
				return fmt.Errorf("guidance 项目 %s 的参数过多", item.Key)
			}
			for name, binding := range item.Arguments {
				if !validStableKey(name) {
					return fmt.Errorf("guidance 项目 %s 的参数名无效: %q", item.Key, name)
				}
				hasFrom, hasValue := binding.From != "", binding.Value != nil
				if hasFrom == hasValue {
					return fmt.Errorf("guidance 项目 %s 的参数 %s 必须且只能声明 from 或 value", item.Key, name)
				}
				if hasValue && !validConditionScalar(binding.Value) {
					return fmt.Errorf("guidance 项目 %s 的参数 %s 的 value 必须是标量", item.Key, name)
				}
				if hasFrom && binding.From != "" {
					if strings.HasPrefix(binding.From, "item.") || strings.HasPrefix(binding.From, "parent.") || strings.HasPrefix(binding.From, "option.") {
						if err := validateCampaignViewRelativePath(strings.SplitN(binding.From, ".", 2)[1]); err != nil {
							return fmt.Errorf("guidance 项目 %s 的参数 %s 路径无效", item.Key, name)
						}
					} else if err := validateGuidancePath(binding.From, contract); err != nil {
						return fmt.Errorf("guidance 项目 %s 的参数 %s 路径无效: %w", item.Key, name, err)
					}
				}
			}
		}
	}
	return nil
}

func containsStableKey(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func validateGuidanceCondition(condition *CampaignViewCondition, contract *RuntimeContract) error {
	if condition == nil {
		return nil
	}
	if strings.HasPrefix(condition.Path, "item.") || strings.HasPrefix(condition.Path, "parent.") || strings.HasPrefix(condition.Path, "option.") {
		if err := validateCampaignViewRelativePath(strings.SplitN(condition.Path, ".", 2)[1]); err != nil {
			return err
		}
	} else if err := validateGuidancePath(condition.Path, contract); err != nil {
		return err
	}
	predicates := 0
	if condition.Exists != nil {
		predicates++
	}
	if condition.Equals != nil {
		predicates++
	}
	if condition.NotEquals != nil {
		predicates++
	}
	if condition.Operator != "" || condition.Value != nil {
		predicates++
		if condition.Operator == "" || condition.Value == nil {
			return fmt.Errorf("数值条件必须同时提供 operator 和 value")
		}
		switch condition.Operator {
		case "gte", "gt", "lte", "lt", "eq":
		default:
			return fmt.Errorf("operator 无效")
		}
		if _, ok := numericValue(condition.Value); !ok {
			return fmt.Errorf("value 必须是数值")
		}
	}
	if predicates != 1 {
		return fmt.Errorf("必须且只能声明一种条件")
	}
	if condition.Equals != nil && !validConditionScalar(condition.Equals) || condition.NotEquals != nil && !validConditionScalar(condition.NotEquals) {
		return fmt.Errorf("equals/not_equals 只能是标量")
	}
	return nil
}

func validateGuidancePath(path string, contract *RuntimeContract) error {
	if strings.HasPrefix(path, "state.") {
		path = strings.TrimPrefix(path, "state.")
		field, ok := FieldForPath(contract.StateSchema, path)
		if !ok || field.Visibility != VisibilityPlayer {
			return fmt.Errorf("只能引用玩家可见 state 路径")
		}
		return nil
	}
	if contract.Mode != RuntimeModeCampaign {
		return fmt.Errorf("challenge guidance 只能引用 state 路径")
	}
	path = strings.TrimPrefix(path, "snapshot.")
	return validateCampaignViewPath(path, false)
}

// ProjectGuidance evaluates presentation-only guidance against public
// snapshots.  The returned value is safe for an API response; action handlers
// must still perform their own authorization and revision checks.
func ProjectGuidance(guidance Guidance, state, snapshot map[string]any) Guidance {
	projected := Guidance{Groups: make([]GuidanceGroup, 0, len(guidance.Groups))}
	for _, group := range guidance.Groups {
		items := make([]GuidanceItem, 0, len(group.Items))
		for _, item := range group.Items {
			if !guidanceConditionMatches(item.VisibleWhen, state, snapshot, nil, nil) {
				continue
			}
			copyItem := item
			enabled := guidanceConditionMatches(item.EnabledWhen, state, snapshot, nil, nil)
			copyItem.Enabled = &enabled
			items = append(items, copyItem)
		}
		sort.SliceStable(items, func(i, j int) bool { return items[i].Key < items[j].Key })
		limit := group.MaxItems
		if limit <= 0 || limit > len(items) {
			limit = len(items)
		}
		if limit == 0 {
			continue
		}
		copyGroup := group
		copyGroup.Items = items[:limit]
		projected.Groups = append(projected.Groups, copyGroup)
	}
	return projected
}

func guidanceConditionMatches(condition *CampaignViewCondition, state, snapshot map[string]any, item, parent any) bool {
	if condition == nil {
		return true
	}
	actual, exists := guidanceValueAtPath(condition.Path, state, snapshot, item, parent)
	if condition.Exists != nil {
		return *condition.Exists == exists && (!*condition.Exists || actual != nil)
	}
	if condition.Equals != nil {
		return fmt.Sprint(actual) == fmt.Sprint(condition.Equals)
	}
	if condition.NotEquals != nil {
		return fmt.Sprint(actual) != fmt.Sprint(condition.NotEquals)
	}
	if condition.Operator != "" && condition.Value != nil {
		actualNumber, ok := numericValue(actual)
		expected, expectedOK := numericValue(condition.Value)
		if !ok || !expectedOK {
			return false
		}
		switch condition.Operator {
		case "gte":
			return actualNumber >= expected
		case "gt":
			return actualNumber > expected
		case "lte":
			return actualNumber <= expected
		case "lt":
			return actualNumber < expected
		case "eq":
			return actualNumber == expected
		}
	}
	return false
}

func guidanceValueAtPath(path string, state, snapshot map[string]any, item, parent any) (any, bool) {
	root := any(state)
	if strings.HasPrefix(path, "state.") {
		root, path = state, strings.TrimPrefix(path, "state.")
	} else if strings.HasPrefix(path, "snapshot.") {
		root, path = snapshot, strings.TrimPrefix(path, "snapshot.")
	} else if strings.HasPrefix(path, "item.") {
		root, path = item, strings.TrimPrefix(path, "item.")
	} else if strings.HasPrefix(path, "parent.") {
		root, path = parent, strings.TrimPrefix(path, "parent.")
	} else if value, ok := ValueAtPath(state, path); ok {
		return value, true
	} else {
		root = snapshot
	}
	if root == nil {
		return nil, false
	}
	value, ok := valueAtAnyPath(root, path)
	return value, ok
}

func valueAtAnyPath(root any, path string) (any, bool) {
	current := root
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

var campaignSnapshotRoots = map[string]bool{
	"campaign": true, "character": true, "realm_system": true, "techniques": true,
	"inventory": true, "recipes": true, "markets": true, "world": true,
	"relationships": true, "factions": true, "region_state": true,
	"quests": true, "obligations": true, "encounters": true,
	"available_encounters": true, "outcome_rewards": true,
}

func validateCampaignView(view CampaignView, campaign *CampaignConfig) error {
	if view.Renderer != "generic" {
		return fmt.Errorf("campaign_view.renderer 无效: %q", view.Renderer)
	}
	if view.Title != "" && (strings.TrimSpace(view.Title) != view.Title || len([]rune(view.Title)) > 128) {
		return fmt.Errorf("campaign_view.title 无效")
	}
	if len(view.Sections) > maxCampaignViewSections {
		return fmt.Errorf("campaign_view 分组不能超过 %d 个", maxCampaignViewSections)
	}
	if len(view.Actions) > maxCampaignViewActions {
		return fmt.Errorf("campaign_view 动作不能超过 %d 个", maxCampaignViewActions)
	}
	if err := validateCampaignViewPath(view.Header.TitlePath, true); err != nil {
		return fmt.Errorf("campaign_view.header.title_path 无效: %w", err)
	}
	if err := validateCampaignViewPath(view.Header.TimePath, true); err != nil {
		return fmt.Errorf("campaign_view.header.time_path 无效: %w", err)
	}
	if view.Header.TimeFormat != "" && view.Header.TimeFormat != "game_time" {
		return fmt.Errorf("campaign_view.header.time_format 无效: %q", view.Header.TimeFormat)
	}
	sectionKeys := map[string]bool{}
	fieldCount := 0
	for _, section := range view.Sections {
		if !validStableKey(section.Key) || sectionKeys[section.Key] {
			return fmt.Errorf("campaign_view 分组 key 无效或重复: %q", section.Key)
		}
		sectionKeys[section.Key] = true
		if section.Title == "" || strings.TrimSpace(section.Title) != section.Title || len([]rune(section.Title)) > 128 {
			return fmt.Errorf("campaign_view 分组 %s 的 title 无效", section.Key)
		}
		if err := validateCampaignViewCondition(section.VisibleWhen, false); err != nil {
			return fmt.Errorf("campaign_view 分组 %s 的 visible_when 无效: %w", section.Key, err)
		}
		fieldKeys := map[string]bool{}
		for _, field := range section.Fields {
			fieldCount++
			if fieldCount > maxCampaignViewFields {
				return fmt.Errorf("campaign_view 字段不能超过 %d 个", maxCampaignViewFields)
			}
			if fieldKeys[field.Path] || field.Label == "" || strings.TrimSpace(field.Label) != field.Label || len([]rune(field.Label)) > 128 {
				return fmt.Errorf("campaign_view 分组 %s 的字段无效或重复: %q", section.Key, field.Path)
			}
			fieldKeys[field.Path] = true
			if err := validateCampaignViewPath(field.Path, false); err != nil {
				return fmt.Errorf("campaign_view 字段 %s.%s 路径无效: %w", section.Key, field.Path, err)
			}
			switch field.Widget {
			case "title", "text", "badge", "number", "progress", "list", "item_list":
			default:
				return fmt.Errorf("campaign_view 字段 %s 控件无效: %q", field.Path, field.Widget)
			}
			if field.ValueNamespace != "" && !validStableKey(field.ValueNamespace) {
				return fmt.Errorf("campaign_view 字段 %s 的 value_namespace 无效", field.Path)
			}
			if field.Limit < 0 || field.Limit > 256 {
				return fmt.Errorf("campaign_view 字段 %s 的 limit 无效", field.Path)
			}
			switch field.Sort {
			case "", "asc", "desc", "name_asc", "name_desc", "order_asc", "order_desc":
			default:
				return fmt.Errorf("campaign_view 字段 %s 的 sort 无效", field.Path)
			}
			if err := validateCampaignViewCondition(field.VisibleWhen, false); err != nil {
				return fmt.Errorf("campaign_view 字段 %s 的 visible_when 无效: %w", field.Path, err)
			}
			if len(field.ItemFields) > maxCampaignViewItems {
				return fmt.Errorf("campaign_view 字段 %s 的 item_fields 不能超过 %d 个", field.Path, maxCampaignViewItems)
			}
			if err := validateCampaignViewItemFields(field.Path, field.ItemFields, 0); err != nil {
				return err
			}
		}
	}
	for key := range view.EmptyStates {
		if !sectionKeys[key] {
			return fmt.Errorf("campaign_view.empty_states 引用了未知分组: %s", key)
		}
	}
	if campaign == nil {
		return fmt.Errorf("campaign_view 缺少 campaign 配置")
	}
	actionKeys := map[string]bool{}
	for _, action := range view.Actions {
		if !validStableKey(action.Key) || actionKeys[action.Key] {
			return fmt.Errorf("campaign_view 动作 key 无效或重复: %q", action.Key)
		}
		actionKeys[action.Key] = true
		allowed := false
		for _, key := range campaign.ActionAllowlist {
			if key == action.Key {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("campaign_view 动作未加入 action_allowlist: %s", action.Key)
		}
		if action.Label == "" || strings.TrimSpace(action.Label) != action.Label || len([]rune(action.Label)) > 128 {
			return fmt.Errorf("campaign_view 动作 %s 的 label 无效", action.Key)
		}
		if action.Placement != "" && (!validStableKey(action.Placement) || !sectionKeys[action.Placement]) {
			return fmt.Errorf("campaign_view 动作 %s 的 placement 无效: %q", action.Key, action.Placement)
		}
		if action.Icon != "" && !validStableKey(action.Icon) {
			return fmt.Errorf("campaign_view 动作 %s 的 icon 无效", action.Key)
		}
		if err := validateCampaignViewPath(action.SourcePath, true); err != nil {
			return fmt.Errorf("campaign_view 动作 %s 的 source_path 无效: %w", action.Key, err)
		}
		if action.ItemPath != "" {
			if err := validateCampaignViewRelativePath(action.ItemPath); err != nil {
				return fmt.Errorf("campaign_view 动作 %s 的 item_path 无效: %w", action.Key, err)
			}
			if action.SourcePath == "" {
				return fmt.Errorf("campaign_view 动作 %s 声明 item_path 时必须提供 source_path", action.Key)
			}
		}
		if err := validateCampaignViewCondition(action.VisibleWhen, true); err != nil {
			return fmt.Errorf("campaign_view 动作 %s 的 visible_when 无效: %w", action.Key, err)
		}
		if err := validateCampaignViewCondition(action.EnabledWhen, true); err != nil {
			return fmt.Errorf("campaign_view 动作 %s 的 enabled_when 无效: %w", action.Key, err)
		}
		if len(action.Arguments) > 32 {
			return fmt.Errorf("campaign_view 动作 %s 的参数过多", action.Key)
		}
		for name, binding := range action.Arguments {
			if !validStableKey(name) {
				return fmt.Errorf("campaign_view 动作 %s 的参数名无效: %q", action.Key, name)
			}
			hasFrom, hasValue := binding.From != "", binding.Value != nil
			if hasFrom == hasValue {
				return fmt.Errorf("campaign_view 动作 %s 的参数 %s 必须且只能声明 from 或 value", action.Key, name)
			}
			if hasValue && !validConditionScalar(binding.Value) {
				return fmt.Errorf("campaign_view 动作 %s 的参数 %s 的 value 必须是标量", action.Key, name)
			}
			if binding.From != "" {
				if err := validateCampaignBindingPath(binding.From); err != nil {
					return fmt.Errorf("campaign_view 动作 %s 的参数 %s 无效: %w", action.Key, name, err)
				}
			}
		}
	}
	for key, text := range view.EmptyStates {
		if !validStableKey(key) || text == "" || strings.TrimSpace(text) != text || len([]rune(text)) > 256 {
			return fmt.Errorf("campaign_view.empty_states %s 无效", key)
		}
	}
	return nil
}

func validateCampaignViewItemFields(parentPath string, fields []CampaignViewItemField, depth int) error {
	if len(fields) > maxCampaignViewItems {
		return fmt.Errorf("campaign_view 字段 %s 的 item_fields 不能超过 %d 个", parentPath, maxCampaignViewItems)
	}
	if depth > 3 {
		return fmt.Errorf("campaign_view 字段 %s 的 item_fields 嵌套过深", parentPath)
	}
	seen := map[string]bool{}
	for _, item := range fields {
		if err := validateCampaignViewRelativePath(item.Path); err != nil || seen[item.Path] {
			return fmt.Errorf("campaign_view 字段 %s 的 item_fields 路径无效或重复: %q", parentPath, item.Path)
		}
		seen[item.Path] = true
		if item.Label == "" || strings.TrimSpace(item.Label) != item.Label || len([]rune(item.Label)) > 128 || (item.Widget != "" && !validCampaignViewWidget(item.Widget)) {
			return fmt.Errorf("campaign_view 字段 %s 的 item_fields %s 无效", parentPath, item.Path)
		}
		if item.ValueNamespace != "" && !validStableKey(item.ValueNamespace) {
			return fmt.Errorf("campaign_view 字段 %s 的 item_fields %s 的 value_namespace 无效", parentPath, item.Path)
		}
		if err := validateCampaignViewItemFields(parentPath+"."+item.Path, item.ItemFields, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func validateCampaignViewRelativePath(path string) error {
	if path == "" || strings.TrimSpace(path) != path || strings.HasPrefix(path, ".") || strings.HasSuffix(path, ".") || strings.Contains(path, "..") || strings.ContainsAny(path, "+{}[]()=<>!&|/'\\") {
		return fmt.Errorf("路径格式无效")
	}
	parts := strings.Split(path, ".")
	if len(parts) > 4 {
		return fmt.Errorf("路径嵌套过深")
	}
	for _, part := range parts {
		if part == "" || strings.TrimSpace(part) != part || len([]rune(part)) > 128 {
			return fmt.Errorf("路径片段无效")
		}
	}
	return nil
}

func validCampaignViewWidget(widget string) bool {
	switch widget {
	case "title", "text", "badge", "number", "progress", "list", "item_list":
		return true
	default:
		return false
	}
}

func validateCampaignViewPath(path string, optional bool) error {
	if path == "" && optional {
		return nil
	}
	if path == "" || strings.TrimSpace(path) != path || strings.HasPrefix(path, ".") || strings.HasSuffix(path, ".") || strings.Contains(path, "..") || strings.ContainsAny(path, "+{}[]()=<>!&|/'\\") {
		return fmt.Errorf("路径格式无效")
	}
	parts := strings.Split(path, ".")
	if len(parts) == 0 || len(parts) > 8 || !campaignSnapshotRoots[parts[0]] {
		return fmt.Errorf("只能引用公开 snapshot 路径")
	}
	for _, part := range parts {
		if part == "" || strings.TrimSpace(part) != part || len([]rune(part)) > 128 {
			return fmt.Errorf("路径片段无效")
		}
	}
	return nil
}

func validateCampaignBindingPath(path string) error {
	if strings.HasPrefix(path, "item.") || strings.HasPrefix(path, "parent.") || strings.HasPrefix(path, "option.") || strings.HasPrefix(path, "input.") {
		path = strings.SplitN(path, ".", 2)[1]
		if err := validateCampaignViewRelativePath(path); err != nil {
			return fmt.Errorf("上下文路径格式无效")
		}
		return nil
	}
	if strings.HasPrefix(path, "snapshot.") {
		path = strings.TrimPrefix(path, "snapshot.")
	}
	return validateCampaignViewPath(path, false)
}

func validateCampaignViewCondition(condition *CampaignViewCondition, contextual bool) error {
	if condition == nil {
		return nil
	}
	if condition.Path == "" {
		return fmt.Errorf("path 不能为空")
	}
	if contextual && (strings.HasPrefix(condition.Path, "item.") || strings.HasPrefix(condition.Path, "parent.") || strings.HasPrefix(condition.Path, "option.")) {
		if err := validateCampaignBindingPath(condition.Path); err != nil {
			return err
		}
	} else {
		path := strings.TrimPrefix(condition.Path, "snapshot.")
		if err := validateCampaignViewPath(path, false); err != nil {
			return err
		}
	}
	predicates := 0
	if condition.Exists != nil {
		predicates++
	}
	if condition.Equals != nil {
		predicates++
	}
	if condition.NotEquals != nil {
		predicates++
	}
	if condition.Operator != "" || condition.Value != nil {
		predicates++
		if condition.Operator == "" || condition.Value == nil {
			return fmt.Errorf("数值条件必须同时提供 operator 和 value")
		}
		switch condition.Operator {
		case "gte", "gt", "lte", "lt", "eq":
		default:
			return fmt.Errorf("operator 无效")
		}
		if _, ok := numericValue(condition.Value); !ok {
			return fmt.Errorf("value 必须是数值")
		}
	}
	if predicates != 1 {
		return fmt.Errorf("必须且只能声明一种条件")
	}
	if condition.Equals != nil && !validConditionScalar(condition.Equals) || condition.NotEquals != nil && !validConditionScalar(condition.NotEquals) {
		return fmt.Errorf("equals/not_equals 只能是标量")
	}
	return nil
}

func validateValueLabels(groups map[string]map[string]string) error {
	const maxEntries = 512
	entryCount := 0
	for namespace, labels := range groups {
		if !validStableKey(namespace) {
			return fmt.Errorf("展示值标签命名空间无效: %q", namespace)
		}
		if len(labels) == 0 {
			return fmt.Errorf("展示值标签 %s 不能为空", namespace)
		}
		for value, label := range labels {
			entryCount++
			if entryCount > maxEntries {
				return fmt.Errorf("展示值标签不能超过 %d 项", maxEntries)
			}
			if value == "" || strings.TrimSpace(value) != value || len([]rune(value)) > 128 {
				return fmt.Errorf("展示值标签 %s 的值 key 无效: %q", namespace, value)
			}
			if label == "" || strings.TrimSpace(label) != label || len([]rune(label)) > 128 {
				return fmt.Errorf("展示值标签 %s.%s 的文案无效", namespace, value)
			}
		}
	}
	return nil
}

func validateCampaignConfig(config *CampaignConfig) error {
	if config == nil {
		return fmt.Errorf("campaign 模式必须声明 campaign 配置")
	}
	for name, value := range map[string]string{
		"lifecycle_handler":        config.LifecycleHandler,
		"projection_handler":       config.ProjectionHandler,
		"context_policy":           config.ContextPolicy,
		"initial_location_key":     config.InitialLocationKey,
		"world_clock.calendar_key": config.WorldClock.CalendarKey,
	} {
		if !validStableKey(value) {
			return fmt.Errorf("campaign.%s 必须是稳定 key", name)
		}
	}
	if config.WorldClock.MaxDueEventsPerTurn < 1 || config.WorldClock.MaxDueEventsPerTurn > 64 {
		return fmt.Errorf("campaign.world_clock.max_due_events_per_turn 必须在 1 到 64 之间")
	}
	if len(config.ActionAllowlist) == 0 || len(config.ActionAllowlist) > 64 {
		return fmt.Errorf("campaign.action_allowlist 必须包含 1 到 64 个动作")
	}
	seen := map[string]bool{}
	for _, key := range config.ActionAllowlist {
		if !validStableKey(key) || seen[key] {
			return fmt.Errorf("campaign.action_allowlist 包含无效或重复动作: %s", key)
		}
		seen[key] = true
	}
	return nil
}

func validateCampaignStateOwnership(schema StateSchema, ownership []FieldOwnership, actionAllowlist []string) error {
	if len(ownership) == 0 {
		return fmt.Errorf("campaign.field_ownership 不能为空；每个长期玩法必须声明权威字段")
	}
	// Campaign conversation state is intentionally namespaced. This prevents a
	// narrative field such as scene.character_name from masquerading as the
	// authoritative Campaign character.name projection.
	for _, field := range schema.Fields {
		if field.AllowChildren {
			return fmt.Errorf("campaign state_schema 禁止无边界 allow_children: %s；请声明 narrative 下的固定字段", field.Path)
		}
		if field.Path != "narrative" && !strings.HasPrefix(field.Path, "narrative.") {
			return fmt.Errorf("campaign 状态字段必须位于 narrative 命名空间: %s", field.Path)
		}
	}
	seen := map[string]bool{}
	for _, item := range ownership {
		if !validOwnershipPath(item.Path) || seen[item.Path] {
			return fmt.Errorf("campaign.field_ownership 路径无效或重复: %q", item.Path)
		}
		seen[item.Path] = true
		if item.Owner != OwnerCampaign {
			return fmt.Errorf("campaign.field_ownership %s 的 owner 必须为 %s", item.Path, OwnerCampaign)
		}
		if item.Path == "narrative" || strings.HasPrefix(item.Path, "narrative.") {
			return fmt.Errorf("Campaign 权威字段不能位于 narrative 命名空间: %s", item.Path)
		}
		if item.WriteCapability == "" && len(item.EventTypes) > 0 {
			return fmt.Errorf("只读 Campaign 字段 %s 不能声明 event_types", item.Path)
		}
		if item.WriteCapability != "" && !validStableKey(item.WriteCapability) {
			return fmt.Errorf("Campaign 字段 %s 的 write_capability 无效", item.Path)
		}
		if item.WriteCapability != "" && !contains(actionAllowlist, item.WriteCapability) {
			return fmt.Errorf("Campaign 字段 %s 的 write_capability 未加入 action_allowlist: %s", item.Path, item.WriteCapability)
		}
		if item.ValueType != "" {
			switch item.ValueType {
			case "string", "number", "boolean", "object", "list":
			default:
				return fmt.Errorf("Campaign 字段 %s 的 value_type 无效: %s", item.Path, item.ValueType)
			}
		}
		if item.ValueType == "" {
			return fmt.Errorf("Campaign 字段 %s 必须声明 value_type", item.Path)
		}
		if item.Visibility != "" {
			switch item.Visibility {
			case VisibilityPlayer, VisibilityModel, VisibilityInternal:
			default:
				return fmt.Errorf("Campaign 字段 %s 的 visibility 无效: %s", item.Path, item.Visibility)
			}
		}
		if item.Visibility == "" {
			return fmt.Errorf("Campaign 字段 %s 必须声明 visibility", item.Path)
		}
		if item.StorageProjection != "" && strings.TrimSpace(item.StorageProjection) != item.StorageProjection {
			return fmt.Errorf("Campaign 字段 %s 的 storage_projection 不能包含首尾空白", item.Path)
		}
		if strings.TrimSpace(item.StorageProjection) == "" {
			return fmt.Errorf("Campaign 字段 %s 必须声明 storage_projection", item.Path)
		}
		for _, eventType := range item.EventTypes {
			if !validStableKey(eventType) {
				return fmt.Errorf("Campaign 字段 %s 的 event_types 包含无效 key: %s", item.Path, eventType)
			}
		}
	}
	for statePath := range schemaPaths(schema) {
		for campaignPath := range seen {
			if pathsOverlap(statePath, campaignPath) {
				return fmt.Errorf("状态字段 %s 与 Campaign 权威字段 %s 重叠", statePath, campaignPath)
			}
		}
	}
	return nil
}

func schemaPaths(schema StateSchema) map[string]struct{} {
	paths := make(map[string]struct{}, len(schema.Fields))
	for _, field := range schema.Fields {
		paths[field.Path] = struct{}{}
	}
	return paths
}

func validOwnershipPath(path string) bool {
	if path == "" || strings.TrimSpace(path) != path || strings.HasPrefix(path, ".") || strings.HasSuffix(path, ".") || strings.Contains(path, "..") || strings.ContainsAny(path, "+{}[]()=<>!&|/'\\") {
		return false
	}
	for _, part := range strings.Split(path, ".") {
		if !validStableKey(part) {
			return false
		}
	}
	return true
}

func pathsOverlap(left, right string) bool {
	return left == right || strings.HasPrefix(left, right+".") || strings.HasPrefix(right, left+".")
}

func validStableKey(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || len(value) > 128 {
		return false
	}
	for index, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') ||
			(char == '.' || char == '_' || char == '-') && index > 0 {
			continue
		}
		return false
	}
	return true
}

// validateRequireCondition 校验前置条件的取值形态：
// 标量（精确匹配）、{"any_of": [...]}、或 {"min"/"max": 数值}。
func validateRequireCondition(cond any) error {
	object, ok := cond.(map[string]any)
	if !ok {
		switch cond.(type) {
		case string, bool, float64, int, int64:
			return nil
		default:
			return fmt.Errorf("不支持的条件类型 %T", cond)
		}
	}
	if anyOf, ok := object["any_of"]; ok {
		values, isList := anyOf.([]any)
		if !isList || len(values) == 0 {
			return fmt.Errorf("any_of 必须是非空数组")
		}
		return nil
	}
	_, hasMin := object["min"]
	_, hasMax := object["max"]
	if !hasMin && !hasMax {
		return fmt.Errorf("对象条件必须包含 any_of、min 或 max")
	}
	for _, bound := range []string{"min", "max"} {
		if value, ok := object[bound]; ok {
			if _, isNumber := numericValue(value); !isNumber {
				return fmt.Errorf("%s 必须是数值", bound)
			}
		}
	}
	return nil
}

// validateEffectEntry 按 system 来源校验效果补丁的单个键值。
func validateEffectEntry(schema StateSchema, key string, value any) error {
	isAppend := strings.HasSuffix(key, "+")
	path := strings.TrimSuffix(key, "+")
	if path == "" || strings.Contains(path, "+") || strings.HasPrefix(path, ".") ||
		strings.HasSuffix(path, ".") || strings.Contains(path, "..") {
		return fmt.Errorf("路径 %q 无效", key)
	}
	field, ok := FieldForPath(schema, path)
	if !ok {
		return fmt.Errorf("路径 %s 未在 state_schema 声明", path)
	}
	if !CanUpdate(schema, path, storage.SourceSystem) {
		return fmt.Errorf("路径 %s 不允许服务端写入", path)
	}
	if isAppend && !((field.Path == path && field.Type == "list") || (field.AllowChildren && field.Path != path)) {
		return fmt.Errorf("路径 %s 不支持追加", key)
	}
	if !isAppend && field.Path == path && field.Type == "object" && !CanReplaceObject(schema, path) {
		return fmt.Errorf("路径 %s 包含已声明子字段，必须逐个叶子路径更新", key)
	}
	if !ValidUpdateValue(field, path, value, isAppend) {
		return fmt.Errorf("路径 %s 的值不符合类型或约束", key)
	}
	return nil
}

// CheckRollRequires 用可信状态评估判定前置条件；nil 表示放行。
// 返回的错误信息面向模型与玩家展示，附带当前值与要求。
func CheckRollRequires(rule RollRule, state map[string]any) error {
	for path, cond := range rule.Requires {
		value, ok := ValueAtPath(state, path)
		if !ok {
			return fmt.Errorf("状态 %s 尚无取值，%s", path, describeRequire(cond))
		}
		if !matchRequire(value, cond) {
			return fmt.Errorf("%s 当前为 %v，%s", path, value, describeRequire(cond))
		}
	}
	return nil
}

func matchRequire(value, cond any) bool {
	object, ok := cond.(map[string]any)
	if !ok {
		return fmt.Sprint(value) == fmt.Sprint(cond)
	}
	if anyOf, ok := object["any_of"].([]any); ok {
		for _, candidate := range anyOf {
			if fmt.Sprint(value) == fmt.Sprint(candidate) {
				return true
			}
		}
		return false
	}
	if anyOf, ok := object["any_of"].([]string); ok {
		for _, candidate := range anyOf {
			if fmt.Sprint(value) == candidate {
				return true
			}
		}
		return false
	}
	number, isNumber := numericValue(value)
	if !isNumber {
		return false
	}
	if minimum, ok := numericValue(object["min"]); ok && number < minimum {
		return false
	}
	if maximum, ok := numericValue(object["max"]); ok && number > maximum {
		return false
	}
	return true
}

func describeRequire(cond any) string {
	object, ok := cond.(map[string]any)
	if !ok {
		return fmt.Sprintf("要求为 %v", cond)
	}
	if anyOf, ok := object["any_of"].([]any); ok {
		parts := make([]string, 0, len(anyOf))
		for _, candidate := range anyOf {
			parts = append(parts, fmt.Sprint(candidate))
		}
		return "要求为以下之一：" + strings.Join(parts, "、")
	}
	minimum, hasMin := numericValue(object["min"])
	maximum, hasMax := numericValue(object["max"])
	switch {
	case hasMin && hasMax:
		return fmt.Sprintf("要求在 %v～%v 之间", minimum, maximum)
	case hasMin:
		return fmt.Sprintf("要求不小于 %v", minimum)
	case hasMax:
		return fmt.Sprintf("要求不大于 %v", maximum)
	default:
		return "要求条件无效"
	}
}

func validateStateField(field StateField) error {
	if strings.TrimSpace(field.Path) == "" || strings.HasPrefix(field.Path, ".") ||
		strings.HasSuffix(field.Path, ".") || strings.Contains(field.Path, "..") ||
		strings.Contains(field.Path, "+") {
		return fmt.Errorf("状态路径无效: %q", field.Path)
	}
	switch field.Type {
	case "string", "integer", "number", "boolean", "enum", "object", "list":
	default:
		return fmt.Errorf("状态字段 %s 类型无效", field.Path)
	}
	if field.Owner == "" {
		field.Owner = OwnerConversation
	}
	if field.Owner != OwnerConversation {
		return fmt.Errorf("状态字段 %s 的 owner 必须为 %s；Campaign 字段必须声明在 campaign.field_ownership", field.Path, OwnerConversation)
	}
	switch field.Visibility {
	case VisibilityPlayer, VisibilityModel, VisibilityInternal:
	default:
		return fmt.Errorf("状态字段 %s 可见性无效", field.Path)
	}
	switch field.UpdatePolicy {
	case UpdateModel, UpdateSystem, UpdateImmutable:
	default:
		return fmt.Errorf("状态字段 %s 更新策略无效", field.Path)
	}
	if field.AllowChildren && field.Type != "object" {
		return fmt.Errorf("只有 object 字段可以允许动态子路径: %s", field.Path)
	}
	if field.Type == "enum" {
		values, ok := field.Constraints["values"].([]any)
		if !ok || len(values) == 0 {
			return fmt.Errorf("枚举字段 %s 必须声明非空 values", field.Path)
		}
		for _, value := range values {
			if text, ok := value.(string); !ok || strings.TrimSpace(text) == "" {
				return fmt.Errorf("枚举字段 %s 的 values 必须是非空字符串", field.Path)
			}
		}
	}
	if field.Default != nil && !ValidUpdateValue(field, field.Path, field.Default, false) {
		return fmt.Errorf("状态字段 %s 的默认值不符合类型或约束", field.Path)
	}
	return nil
}

func InitialState(schema StateSchema) map[string]any {
	state := map[string]any{}
	fields := append([]StateField(nil), schema.Fields...)
	sort.Slice(fields, func(i, j int) bool {
		return strings.Count(fields[i].Path, ".") < strings.Count(fields[j].Path, ".")
	})
	for _, field := range fields {
		value := field.Default
		if value == nil {
			switch field.Type {
			case "object":
				value = map[string]any{}
			case "list":
				value = []any{}
			}
		}
		if value != nil {
			setPath(state, field.Path, cloneValue(value))
		}
	}
	return state
}

func FieldForPath(schema StateSchema, path string) (StateField, bool) {
	path = strings.TrimSuffix(path, "+")
	for _, field := range schema.Fields {
		if path == field.Path {
			return field, true
		}
	}

	// The closest declared ancestor owns the path. A broad allow_children
	// object must never let callers tunnel through a more specific scalar or
	// protected field (for example profile.cultivation_level.extra).
	var ancestor StateField
	found := false
	for _, field := range schema.Fields {
		if strings.HasPrefix(path, field.Path+".") {
			if !found || len(field.Path) > len(ancestor.Path) {
				ancestor, found = field, true
			}
		}
	}
	if !found || ancestor.Type != "object" || !ancestor.AllowChildren {
		return StateField{}, false
	}
	return ancestor, true
}

// CanReplaceObject reports whether an object path can be assigned as a whole.
// Parents with declared descendants must be updated through leaf paths so an
// object replacement cannot bypass child types, constraints, or ownership.
func CanReplaceObject(schema StateSchema, path string) bool {
	for _, field := range schema.Fields {
		if strings.HasPrefix(field.Path, path+".") {
			return false
		}
	}
	return true
}

func CanUpdate(schema StateSchema, path, source string) bool {
	field, ok := FieldForPath(schema, path)
	if !ok {
		return false
	}
	if source == storage.SourceAI {
		return field.UpdatePolicy == UpdateModel
	}
	return field.UpdatePolicy == UpdateModel || field.UpdatePolicy == UpdateSystem
}

// ValidUpdateValue applies the bounded type rules from a published state
// schema. Dynamic children of an allow_children object are JSON values whose
// detailed shape is intentionally owned by that gameplay.
func ValidUpdateValue(field StateField, path string, value any, appendValue bool) bool {
	if field.AllowChildren && path != field.Path {
		return true
	}
	if appendValue {
		return field.Type == "list"
	}
	switch field.Type {
	case "string":
		text, ok := value.(string)
		return ok && withinLengthConstraints(len([]rune(text)), field.Constraints)
	case "integer":
		number, ok := numericValue(value)
		return ok && number == float64(int64(number)) && withinNumberConstraints(number, field.Constraints)
	case "number":
		number, ok := numericValue(value)
		return ok && withinNumberConstraints(number, field.Constraints)
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "enum":
		values, ok := field.Constraints["values"].([]any)
		if !ok {
			return false
		}
		for _, candidate := range values {
			if fmt.Sprint(candidate) == fmt.Sprint(value) {
				return true
			}
		}
		return false
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "list":
		items, ok := value.([]any)
		return ok && withinLengthConstraints(len(items), field.Constraints)
	default:
		return false
	}
}

func numericValue(value any) (float64, bool) {
	switch number := value.(type) {
	case int:
		return float64(number), true
	case int64:
		return float64(number), true
	case float64:
		return number, true
	case float32:
		return float64(number), true
	default:
		return 0, false
	}
}

func withinNumberConstraints(number float64, constraints map[string]any) bool {
	if minimum, ok := numericValue(constraints["min"]); ok && number < minimum {
		return false
	}
	if maximum, ok := numericValue(constraints["max"]); ok && number > maximum {
		return false
	}
	return true
}

func withinLengthConstraints(length int, constraints map[string]any) bool {
	if minimum, ok := numericValue(constraints["min_length"]); ok && float64(length) < minimum {
		return false
	}
	if maximum, ok := numericValue(constraints["max_length"]); ok && float64(length) > maximum {
		return false
	}
	return true
}

func PlayerState(contract RuntimeContract, state map[string]any) map[string]any {
	projected := map[string]any{}
	for key, value := range state {
		if visible, ok := projectPlayerValue(contract.StateSchema, key, value); ok {
			projected[key] = visible
		}
	}
	return projected
}

func projectPlayerValue(schema StateSchema, path string, value any) (any, bool) {
	field, ok := FieldForPath(schema, path)
	if !ok || field.Visibility != VisibilityPlayer {
		return nil, false
	}
	object, isObject := value.(map[string]any)
	if !isObject {
		return cloneValue(value), true
	}
	projected := map[string]any{}
	for key, child := range object {
		childPath := path + "." + key
		if visible, childOK := projectPlayerValue(schema, childPath, child); childOK {
			projected[key] = visible
		}
	}
	return projected, true
}

func ValueAtPath(state map[string]any, path string) (any, bool) {
	var current any = state
	for _, part := range strings.Split(path, ".") {
		obj, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = obj[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func setPath(root map[string]any, path string, value any) {
	parts := strings.Split(path, ".")
	current := root
	for _, part := range parts[:len(parts)-1] {
		next, ok := current[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[part] = next
		}
		current = next
	}
	current[parts[len(parts)-1]] = value
}

func cloneValue(value any) any {
	data, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var cloned any
	if err := json.Unmarshal(data, &cloned); err != nil {
		return value
	}
	return cloned
}
