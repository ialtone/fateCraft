package storage

import (
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// 对话状态取值。
const (
	StatusActive   = "active"
	StatusEnded    = "ended"
	StatusArchived = "archived"
	StatusDeleted  = "deleted"
)

const ChatTokenizerVersion = "heuristic-v1"

// Durable generation run states. A cancel_requested run still owns the
// conversation until the worker has actually stopped and seals the run.
const (
	GenerationRunRunning         = "running"
	GenerationRunCancelRequested = "cancel_requested"
	GenerationRunCompleted       = "completed"
	GenerationRunFailed          = "failed"
	GenerationRunCancelled       = "cancelled"
	GenerationRunAbandoned       = "abandoned"
)

// 回合、生成尝试、记忆任务与只读问答状态。
const (
	TurnStatusPending   = "pending"
	TurnStatusCompleted = "completed"
	TurnStatusPartial   = "partial"
	TurnStatusFailed    = "failed"
	TurnStatusCancelled = "cancelled"

	AttemptStatusRunning   = "running"
	AttemptStatusCompleted = "completed"
	AttemptStatusPartial   = "partial"
	AttemptStatusFailed    = "failed"
	AttemptStatusCancelled = "cancelled"

	MemoryJobPending   = "pending"
	MemoryJobRunning   = "running"
	MemoryJobSucceeded = "succeeded"
	MemoryJobFailed    = "failed"
	MemoryJobCancelled = "cancelled"
)

// 长期存档状态与领域事件可见性。
const (
	CampaignStatusActive    = "active"
	CampaignStatusCompleted = "completed"
	CampaignStatusDead      = "dead"
	CampaignStatusAbandoned = "abandoned"

	CampaignVisibilityPlayer   = "player"
	CampaignVisibilityModel    = "model"
	CampaignVisibilityInternal = "internal"
)

// 内部消息角色。
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
	RoleInternal  = "internal"
)

// 状态修订来源。
const (
	SourceAI     = "ai"
	SourceSystem = "system"
)

// ---------------------------------------------------------------------------
// Entity 接口（通用实体接口，用于泛型约束）
// ---------------------------------------------------------------------------

type Entity interface {
	GetID() string
	GetUpdatedAt() time.Time
}

// 模板内容结构。官方 JSON 包通过导入器写入 challenge_assets.content，
// 运行时只从 PostgreSQL 的已发布挑战版本读取。

type Character struct {
	ID                   string    `json:"id"`
	Name                 string    `json:"name"`
	Description          string    `json:"description"`
	Personality          string    `json:"personality"`
	Scenario             string    `json:"scenario"`
	FirstMessage         string    `json:"first_message"`
	ExampleDialogues     string    `json:"example_dialogues"`
	SystemPromptOverride string    `json:"system_prompt_override"`
	Tags                 []string  `json:"tags"`
	AvatarURL            *string   `json:"avatar_url,omitempty"`
	PackID               string    `json:"pack_id,omitempty"` // 所属预设包ID
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

func (c Character) GetID() string            { return c.ID }
func (c Character) GetUpdatedAt() time.Time  { return c.UpdatedAt }
func (c *Character) SetPackID(packID string) { c.PackID = packID }

type Persona struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	SpeakingStyle string    `json:"speaking_style"`
	Constraints   string    `json:"constraints"`
	PackID        string    `json:"pack_id,omitempty"` // 所属预设包ID
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (p Persona) GetID() string            { return p.ID }
func (p Persona) GetUpdatedAt() time.Time  { return p.UpdatedAt }
func (p *Persona) SetPackID(packID string) { p.PackID = packID }

type Scenario struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Opening     string    `json:"opening"`
	PackID      string    `json:"pack_id,omitempty"` // 所属预设包ID
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (s Scenario) GetID() string            { return s.ID }
func (s Scenario) GetUpdatedAt() time.Time  { return s.UpdatedAt }
func (s *Scenario) SetPackID(packID string) { s.PackID = packID }

type PromptPreset struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description *string         `json:"description,omitempty"`
	PackID      string          `json:"pack_id,omitempty"` // 所属预设包 ID
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	Segments    []PromptSegment `json:"segments"`
}

func (p PromptPreset) GetID() string            { return p.ID }
func (p PromptPreset) GetUpdatedAt() time.Time  { return p.UpdatedAt }
func (p *PromptPreset) SetPackID(packID string) { p.PackID = packID }

type PromptSegment struct {
	ID       string `json:"id"`
	PresetID string `json:"preset_id"`
	Key      string `json:"key"`
	Role     string `json:"role"`
	Content  string `json:"content"`
	Enabled  bool   `json:"enabled"`
	Required bool   `json:"required"`
	Priority int    `json:"priority"`
	Position int    `json:"position"`
}

type WorldBook struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description *string          `json:"description,omitempty"`
	PackID      string           `json:"pack_id,omitempty"` // 所属预设包 ID；为空表示管理员创建的共享世界书
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
	Entries     []WorldBookEntry `json:"entries"`
}

func (b WorldBook) GetID() string            { return b.ID }
func (b WorldBook) GetUpdatedAt() time.Time  { return b.UpdatedAt }
func (b *WorldBook) SetPackID(packID string) { b.PackID = packID }

type WorldBookEntry struct {
	ID          string         `json:"id"`
	WorldBookID string         `json:"world_book_id"`
	Title       string         `json:"title"`
	Content     string         `json:"content"`
	Keywords    []string       `json:"keywords"`
	Constant    bool           `json:"constant"`
	Enabled     bool           `json:"enabled"`
	Priority    int            `json:"priority"`
	BudgetGroup string         `json:"budget_group"`
	TokenCount  *int           `json:"token_count,omitempty"`
	Notes       string         `json:"notes"`
	Metadata    map[string]any `json:"metadata"`
}

// ---------------------------------------------------------------------------
// 挑战内容与版本
// ---------------------------------------------------------------------------

const (
	ChallengePackActive  = "active"
	ChallengePackHidden  = "hidden"
	ChallengePackRetired = "retired"

	ChallengeVersionDraft     = "draft"
	ChallengeVersionPublished = "published"
	ChallengeVersionRetired   = "retired"

	ChallengeAssetCharacter = "character"
	ChallengeAssetPersona   = "persona"
	ChallengeAssetScenario  = "scenario"
	ChallengeAssetPrompt    = "prompt"
	ChallengeAssetWorldBook = "worldbook"
)

type ChallengePack struct {
	ID               uuid.UUID  `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	Slug             string     `gorm:"column:slug;uniqueIndex;not null" json:"slug"`
	Name             string     `gorm:"column:name;not null" json:"name"`
	Description      string     `gorm:"column:description" json:"description"`
	CoverURL         *string    `gorm:"column:cover_url" json:"cover_url,omitempty"`
	Status           string     `gorm:"column:status;not null" json:"status"`
	CurrentVersionID *uuid.UUID `gorm:"column:current_version_id;type:uuid" json:"current_version_id,omitempty"`
	CreatedBy        *uuid.UUID `gorm:"column:created_by;type:uuid" json:"created_by,omitempty"`
	CreatedAt        time.Time  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt        time.Time  `gorm:"column:updated_at" json:"updated_at"`
}

func (ChallengePack) TableName() string { return "challenge_packs" }

type ChallengeVersion struct {
	ID              uuid.UUID  `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	ChallengePackID uuid.UUID  `gorm:"column:challenge_pack_id;type:uuid;not null;index" json:"challenge_pack_id"`
	Version         string     `gorm:"column:version;not null" json:"version"`
	Status          string     `gorm:"column:status;not null" json:"status"`
	Manifest        JSONDoc    `gorm:"column:manifest;type:jsonb;not null" json:"manifest"`
	ContentHash     string     `gorm:"column:content_hash;not null" json:"content_hash"`
	CreatedBy       *uuid.UUID `gorm:"column:created_by;type:uuid" json:"created_by,omitempty"`
	PublishedBy     *uuid.UUID `gorm:"column:published_by;type:uuid" json:"published_by,omitempty"`
	PublishedAt     *time.Time `gorm:"column:published_at" json:"published_at,omitempty"`
	CreatedAt       time.Time  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt       time.Time  `gorm:"column:updated_at" json:"updated_at"`
}

func (ChallengeVersion) TableName() string { return "challenge_versions" }

type ChallengeRuntimeConfig struct {
	ChallengeVersionID uuid.UUID `gorm:"column:challenge_version_id;primaryKey;type:uuid" json:"challenge_version_id"`
	EngineKey          string    `gorm:"column:engine_key;not null" json:"engine_key"`
	EngineVersion      int       `gorm:"column:engine_version;not null" json:"engine_version"`
	Mode               string    `gorm:"column:mode;not null" json:"mode"`
	CampaignConfig     *JSONDoc  `gorm:"column:campaign_config;type:jsonb" json:"campaign_config,omitempty"`
	StateSchema        JSONDoc   `gorm:"column:state_schema;type:jsonb;not null" json:"state_schema"`
	ActionConfig       JSONDoc   `gorm:"column:action_config;type:jsonb;not null" json:"action_config"`
	SettlementConfig   JSONDoc   `gorm:"column:settlement_config;type:jsonb;not null" json:"settlement_config"`
	Presentation       JSONDoc   `gorm:"column:presentation;type:jsonb;not null" json:"presentation"`
	CreatedAt          time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt          time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (ChallengeRuntimeConfig) TableName() string { return "challenge_runtime_configs" }

type ChallengeAsset struct {
	ID                 uuid.UUID `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	ChallengeVersionID uuid.UUID `gorm:"column:challenge_version_id;type:uuid;not null;index" json:"challenge_version_id"`
	Kind               string    `gorm:"column:kind;not null" json:"kind"`
	Slug               string    `gorm:"column:slug;not null" json:"slug"`
	Name               string    `gorm:"column:name;not null" json:"name"`
	Content            JSONDoc   `gorm:"column:content;type:jsonb;not null" json:"content"`
	SortOrder          int       `gorm:"column:sort_order;not null;default:0" json:"sort_order"`
	CreatedAt          time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt          time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (ChallengeAsset) TableName() string { return "challenge_assets" }

type ChallengeOutcomeRule struct {
	ID                 uuid.UUID `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	ChallengeVersionID uuid.UUID `gorm:"column:challenge_version_id;type:uuid;not null;index" json:"challenge_version_id"`
	OutcomeKey         string    `gorm:"column:outcome_key;not null" json:"outcome_key"`
	Category           string    `gorm:"column:category;not null" json:"category"`
	Title              string    `gorm:"column:title;not null" json:"title"`
	Tone               string    `gorm:"column:tone;not null" json:"tone"`
	VerifierKey        string    `gorm:"column:verifier_key;not null" json:"verifier_key"`
	VerifierConfig     JSONMap   `gorm:"column:verifier_config;type:jsonb;not null" json:"verifier_config"`
	Enabled            bool      `gorm:"column:enabled;not null;default:true" json:"enabled"`
	CreatedAt          time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt          time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (ChallengeOutcomeRule) TableName() string { return "challenge_outcome_rules" }

type ChallengeRewardRule struct {
	ID                 uuid.UUID `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	ChallengeVersionID uuid.UUID `gorm:"column:challenge_version_id;type:uuid;not null;index" json:"challenge_version_id"`
	OutcomeRuleID      uuid.UUID `gorm:"column:outcome_rule_id;type:uuid;not null" json:"outcome_rule_id"`
	RewardDefinitionID uuid.UUID `gorm:"column:reward_definition_id;type:uuid;not null;index" json:"reward_definition_id"`
	CalculatorKey      string    `gorm:"column:calculator_key;not null" json:"calculator_key"`
	CalculatorConfig   JSONMap   `gorm:"column:calculator_config;type:jsonb;not null" json:"calculator_config"`
	PublicSnapshot     JSONMap   `gorm:"column:public_snapshot;type:jsonb;not null" json:"public_snapshot"`
	Enabled            bool      `gorm:"column:enabled;not null;default:true" json:"enabled"`
	CreatedAt          time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt          time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (ChallengeRewardRule) TableName() string { return "challenge_reward_rules" }

type ChallengeRun struct {
	ID                 uuid.UUID  `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	ConversationID     uuid.UUID  `gorm:"column:conversation_id;type:uuid;not null;uniqueIndex" json:"conversation_id"`
	PlayerID           uuid.UUID  `gorm:"column:player_id;type:uuid;not null;index" json:"player_id"`
	ChallengeVersionID uuid.UUID  `gorm:"column:challenge_version_id;type:uuid;not null;index" json:"challenge_version_id"`
	OutcomeRuleID      uuid.UUID  `gorm:"column:outcome_rule_id;type:uuid;not null" json:"outcome_rule_id"`
	OutcomeKey         string     `gorm:"column:outcome_key;not null" json:"outcome_key"`
	Category           string     `gorm:"column:category;not null" json:"category"`
	TitleSnapshot      string     `gorm:"column:title_snapshot;not null" json:"title_snapshot"`
	Summary            string     `gorm:"column:summary;not null" json:"summary"`
	SettlementDetail   JSONMap    `gorm:"column:settlement_detail;type:jsonb;not null" json:"settlement_detail"`
	TurnID             *uuid.UUID `gorm:"column:turn_id;type:uuid" json:"turn_id,omitempty"`
	AttemptID          *uuid.UUID `gorm:"column:attempt_id;type:uuid" json:"attempt_id,omitempty"`
	SettledAt          time.Time  `gorm:"column:settled_at;not null" json:"settled_at"`
}

func (ChallengeRun) TableName() string { return "challenge_runs" }

type RewardDefinition struct {
	ID          uuid.UUID  `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	Key         string     `gorm:"column:key;uniqueIndex;not null" json:"key"`
	Kind        string     `gorm:"column:kind;not null" json:"kind"`
	Name        string     `gorm:"column:name;not null" json:"name"`
	Description string     `gorm:"column:description;not null" json:"description"`
	Unit        string     `gorm:"column:unit;not null" json:"unit"`
	Icon        string     `gorm:"column:icon;not null" json:"icon"`
	Stackable   bool       `gorm:"column:stackable;not null" json:"stackable"`
	Consumable  bool       `gorm:"column:consumable;not null" json:"consumable"`
	Featured    bool       `gorm:"column:featured;not null" json:"featured"`
	SortOrder   int        `gorm:"column:sort_order;not null" json:"sort_order"`
	Metadata    JSONMap    `gorm:"column:metadata;type:jsonb;not null" json:"metadata"`
	CreatedBy   *uuid.UUID `gorm:"column:created_by;type:uuid" json:"created_by,omitempty"`
	CreatedAt   time.Time  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt   time.Time  `gorm:"column:updated_at" json:"updated_at"`
}

func (RewardDefinition) TableName() string { return "reward_definitions" }

type PlayerRewardBalance struct {
	PlayerID           uuid.UUID `gorm:"column:player_id;primaryKey;type:uuid" json:"player_id"`
	RewardDefinitionID uuid.UUID `gorm:"column:reward_definition_id;primaryKey;type:uuid" json:"reward_definition_id"`
	Balance            int64     `gorm:"column:balance;not null" json:"balance"`
	GrantedTotal       int64     `gorm:"column:granted_total;not null" json:"granted_total"`
	ConsumedTotal      int64     `gorm:"column:consumed_total;not null" json:"consumed_total"`
	UpdatedAt          time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (PlayerRewardBalance) TableName() string { return "player_reward_balances" }

type RewardLedgerEntry struct {
	ID                 uuid.UUID  `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	PlayerID           uuid.UUID  `gorm:"column:player_id;type:uuid;not null;index" json:"player_id"`
	RewardDefinitionID uuid.UUID  `gorm:"column:reward_definition_id;type:uuid;not null" json:"reward_definition_id"`
	Operation          string     `gorm:"column:operation;not null" json:"operation"`
	QuantityDelta      int64      `gorm:"column:quantity_delta;not null" json:"quantity_delta"`
	SourceType         string     `gorm:"column:source_type;not null" json:"source_type"`
	SourceID           string     `gorm:"column:source_id;not null" json:"source_id"`
	ChallengeRunID     *uuid.UUID `gorm:"column:challenge_run_id;type:uuid" json:"challenge_run_id,omitempty"`
	IdempotencyKey     string     `gorm:"column:idempotency_key;uniqueIndex;not null" json:"idempotency_key"`
	Metadata           JSONMap    `gorm:"column:metadata;type:jsonb;not null" json:"metadata"`
	CreatedAt          time.Time  `gorm:"column:created_at" json:"created_at"`
}

func (RewardLedgerEntry) TableName() string { return "reward_ledger" }

type ChallengeRunReward struct {
	ChallengeRunID     uuid.UUID `gorm:"column:challenge_run_id;primaryKey;type:uuid" json:"challenge_run_id"`
	RewardDefinitionID uuid.UUID `gorm:"column:reward_definition_id;primaryKey;type:uuid" json:"reward_definition_id"`
	LedgerID           uuid.UUID `gorm:"column:ledger_id;type:uuid;not null;uniqueIndex" json:"ledger_id"`
	Quantity           int64     `gorm:"column:quantity;not null" json:"quantity"`
	RewardKeySnapshot  string    `gorm:"column:reward_key_snapshot;not null" json:"reward_key"`
	NameSnapshot       string    `gorm:"column:name_snapshot;not null" json:"name"`
	UnitSnapshot       string    `gorm:"column:unit_snapshot;not null" json:"unit"`
	IconSnapshot       string    `gorm:"column:icon_snapshot;not null" json:"icon"`
	BalanceAfter       int64     `gorm:"column:balance_after;not null" json:"balance_after"`
}

func (ChallengeRunReward) TableName() string { return "challenge_run_rewards" }

// ---------------------------------------------------------------------------
// 对话与运行时数据
// ---------------------------------------------------------------------------

// Campaign 是长期玩法的权威存档头。具体功法、物品、NPC 等事实由后续领域表
// 持有；这里仅保存生命周期、版本锚点、游戏时钟和并发游标。
type Campaign struct {
	ID                  uuid.UUID  `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	PlayerID            uuid.UUID  `gorm:"column:player_id;type:uuid;not null" json:"player_id"`
	Title               string     `gorm:"column:title;not null" json:"title"`
	ChallengeVersionID  uuid.UUID  `gorm:"column:challenge_version_id;type:uuid;not null" json:"challenge_version_id"`
	ChallengeHash       string     `gorm:"column:challenge_hash;not null" json:"challenge_hash"`
	CharacterAssetID    *uuid.UUID `gorm:"column:character_asset_id;type:uuid" json:"character_asset_id,omitempty"`
	PersonaAssetID      *uuid.UUID `gorm:"column:persona_asset_id;type:uuid" json:"persona_asset_id,omitempty"`
	ScenarioAssetID     *uuid.UUID `gorm:"column:scenario_asset_id;type:uuid" json:"scenario_asset_id,omitempty"`
	Status              string     `gorm:"column:status;not null" json:"status"`
	OutcomeKey          string     `gorm:"column:outcome_key;not null" json:"outcome_key"`
	CompletedAtGameTime *int64     `gorm:"column:completed_at_game_time" json:"completed_at_game_time,omitempty"`
	CurrentLocationKey  string     `gorm:"column:current_location_key;not null" json:"current_location_key"`
	GameTime            int64      `gorm:"column:game_time;not null" json:"game_time"`
	Revision            int64      `gorm:"column:revision;not null" json:"revision"`
	EventSequence       int64      `gorm:"column:event_sequence;not null" json:"event_sequence"`
	RandomSeed          int64      `gorm:"column:random_seed;not null" json:"-"`
	CreatedAt           time.Time  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt           time.Time  `gorm:"column:updated_at" json:"updated_at"`
}

func (Campaign) TableName() string { return "campaigns" }

// CampaignEvent 是只追加的领域事实审计记录。一次动作可以产生多条事件，
// 因此 sequence 与 Campaign revision 分开维护。
type CampaignEvent struct {
	ID              uuid.UUID  `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	CampaignID      uuid.UUID  `gorm:"column:campaign_id;type:uuid;not null" json:"campaign_id"`
	Sequence        int64      `gorm:"column:sequence;not null" json:"sequence"`
	EventType       string     `gorm:"column:event_type;not null" json:"event_type"`
	Visibility      string     `gorm:"column:visibility;not null" json:"visibility"`
	ActorType       string     `gorm:"column:actor_type;not null" json:"actor_type"`
	ActorID         *string    `gorm:"column:actor_id" json:"actor_id,omitempty"`
	Payload         JSONMap    `gorm:"column:payload;type:jsonb;not null" json:"payload"`
	SourceMessageID *uuid.UUID `gorm:"column:source_message_id;type:uuid" json:"source_message_id,omitempty"`
	TurnID          *uuid.UUID `gorm:"column:turn_id;type:uuid" json:"turn_id,omitempty"`
	AttemptID       *uuid.UUID `gorm:"column:attempt_id;type:uuid" json:"attempt_id,omitempty"`
	IdempotencyKey  string     `gorm:"column:idempotency_key;not null" json:"idempotency_key"`
	GameTime        int64      `gorm:"column:game_time;not null" json:"game_time"`
	CreatedAt       time.Time  `gorm:"column:created_at" json:"created_at"`
}

func (CampaignEvent) TableName() string { return "campaign_events" }

// CampaignActionCommit 记录领域动作的幂等结果。一条 commit 可覆盖多条事件，
// 重试时直接返回原 revision、事件区间和公开结果。
type CampaignActionCommit struct {
	CampaignID     uuid.UUID  `gorm:"column:campaign_id;primaryKey;type:uuid" json:"campaign_id"`
	IdempotencyKey string     `gorm:"column:idempotency_key;primaryKey" json:"idempotency_key"`
	ActionKey      string     `gorm:"column:action_key;not null" json:"action_key"`
	Revision       int64      `gorm:"column:revision;not null" json:"revision"`
	FirstSequence  int64      `gorm:"column:first_sequence;not null" json:"first_sequence"`
	LastSequence   int64      `gorm:"column:last_sequence;not null" json:"last_sequence"`
	Result         JSONMap    `gorm:"column:result;type:jsonb;not null" json:"result"`
	TurnID         *uuid.UUID `gorm:"column:turn_id;type:uuid" json:"turn_id,omitempty"`
	AttemptID      *uuid.UUID `gorm:"column:attempt_id;type:uuid" json:"attempt_id,omitempty"`
	CreatedAt      time.Time  `gorm:"column:created_at" json:"created_at"`
}

func (CampaignActionCommit) TableName() string { return "campaign_action_commits" }

type Conversation struct {
	ID                 uuid.UUID  `gorm:"column:id;primaryKey" json:"id"`
	PlayerID           string     `gorm:"column:player_id;type:uuid" json:"player_id"`
	Title              string     `gorm:"column:title" json:"title"`
	Status             string     `gorm:"column:status" json:"status"`
	IsProcessing       bool       `gorm:"column:is_processing" json:"is_processing"`
	ChallengeVersionID uuid.UUID  `gorm:"column:challenge_version_id;type:uuid;not null" json:"challenge_version_id"`
	ChallengeHash      string     `gorm:"column:challenge_hash;not null" json:"challenge_hash"`
	CampaignID         *uuid.UUID `gorm:"column:campaign_id;type:uuid" json:"campaign_id,omitempty"`
	CharacterAssetID   *uuid.UUID `gorm:"column:character_asset_id;type:uuid" json:"character_asset_id,omitempty"`
	PersonaAssetID     *uuid.UUID `gorm:"column:persona_asset_id;type:uuid" json:"persona_asset_id,omitempty"`
	ScenarioAssetID    *uuid.UUID `gorm:"column:scenario_asset_id;type:uuid" json:"scenario_asset_id,omitempty"`
	BranchEpoch        int64      `gorm:"column:branch_epoch;not null" json:"branch_epoch"`
	NextTurnSeq        int64      `gorm:"column:next_turn_seq;not null;default:1" json:"-"`
	NextMessageSeq     int64      `gorm:"column:next_message_seq;not null;default:1" json:"-"`
	CurrentMemoryID    *uuid.UUID `gorm:"column:current_memory_id;type:uuid" json:"current_memory_id,omitempty"`
	CreatedAt          time.Time  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt          time.Time  `gorm:"column:updated_at" json:"updated_at"`
	EndedAt            *time.Time `gorm:"column:ended_at" json:"ended_at,omitempty"`
	ArchivedAt         *time.Time `gorm:"column:archived_at" json:"archived_at,omitempty"`
	DeletedAt          *time.Time `gorm:"column:deleted_at" json:"deleted_at,omitempty"`
}

func (Conversation) TableName() string { return "conversations" }

// ConversationGenerationRun is the durable lease for one writable generation
// task. It prevents an old task from releasing a newer task's processing state
// and gives restart recovery a precise owner to reconcile.
type ConversationGenerationRun struct {
	ID                   uuid.UUID  `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	ConversationID       uuid.UUID  `gorm:"column:conversation_id;type:uuid;not null" json:"conversation_id"`
	RequestID            uuid.UUID  `gorm:"column:request_id;type:uuid;not null;uniqueIndex" json:"request_id"`
	OperationType        string     `gorm:"column:operation_type;not null" json:"operation_type"`
	TargetMessageID      *uuid.UUID `gorm:"column:target_message_id;type:uuid" json:"target_message_id,omitempty"`
	PayloadHash          string     `gorm:"column:payload_hash;not null" json:"-"`
	ConversationRevision int64      `gorm:"column:conversation_revision;not null" json:"conversation_revision"`
	Status               string     `gorm:"column:status;not null" json:"status"`
	// OwnerInstanceID 是持有该租约的后端实例；实例重启时只回收自己的残留租约，
	// 其他实例正在进行的生成只按 lease_expires_at 过期回收。
	OwnerInstanceID string     `gorm:"column:owner_instance_id;not null" json:"owner_instance_id"`
	StartedAt       time.Time  `gorm:"column:started_at;not null" json:"started_at"`
	LastHeartbeatAt time.Time  `gorm:"column:last_heartbeat_at;not null" json:"last_heartbeat_at"`
	LeaseExpiresAt  time.Time  `gorm:"column:lease_expires_at;not null" json:"lease_expires_at"`
	FinishedAt      *time.Time `gorm:"column:finished_at" json:"finished_at,omitempty"`
	ErrorCode       string     `gorm:"column:error_code;not null" json:"error_code,omitempty"`
}

func (ConversationGenerationRun) TableName() string { return "conversation_generation_runs" }

type ConversationTurn struct {
	ID                 uuid.UUID  `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	ConversationID     uuid.UUID  `gorm:"column:conversation_id;type:uuid;not null" json:"conversation_id"`
	TurnSeq            int64      `gorm:"column:turn_seq;not null" json:"turn_seq"`
	TriggerType        string     `gorm:"column:trigger_type;not null" json:"trigger_type"`
	Status             string     `gorm:"column:status;not null" json:"status"`
	FailureStage       string     `gorm:"column:failure_stage" json:"failure_stage,omitempty"`
	InvalidatedAt      *time.Time `gorm:"column:invalidated_at" json:"invalidated_at,omitempty"`
	InvalidationReason string     `gorm:"column:invalidation_reason" json:"invalidation_reason,omitempty"`
	CreatedAt          time.Time  `gorm:"column:created_at" json:"created_at"`
	SealedAt           *time.Time `gorm:"column:sealed_at" json:"sealed_at,omitempty"`
}

func (ConversationTurn) TableName() string { return "conversation_turns" }

type GenerationAttempt struct {
	ID                         uuid.UUID  `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	ConversationID             uuid.UUID  `gorm:"column:conversation_id;type:uuid;not null" json:"conversation_id"`
	TurnID                     uuid.UUID  `gorm:"column:turn_id;type:uuid;not null" json:"turn_id"`
	AttemptNo                  int        `gorm:"column:attempt_no;not null" json:"attempt_no"`
	BranchEpoch                int64      `gorm:"column:branch_epoch;not null" json:"branch_epoch"`
	MemoryID                   *uuid.UUID `gorm:"column:memory_id;type:uuid" json:"memory_id,omitempty"`
	MemoryCoveredUntilTurnSeq  int64      `gorm:"column:memory_covered_until_turn_seq;not null" json:"memory_covered_until_turn_seq"`
	Status                     string     `gorm:"column:status;not null" json:"status"`
	StepCount                  int        `gorm:"column:step_count;not null" json:"step_count"`
	CommittedMutatingToolCount int        `gorm:"column:committed_mutating_tool_count;not null" json:"committed_mutating_tool_count"`
	ToolCatalogVersion         string     `gorm:"column:tool_catalog_version;not null" json:"tool_catalog_version"`
	RuntimeHash                string     `gorm:"column:runtime_hash;not null" json:"runtime_hash"`
	FailureStage               string     `gorm:"column:failure_stage" json:"failure_stage,omitempty"`
	ErrorCode                  string     `gorm:"column:error_code" json:"error_code,omitempty"`
	StartedAt                  time.Time  `gorm:"column:started_at" json:"started_at"`
	FinishedAt                 *time.Time `gorm:"column:finished_at" json:"finished_at,omitempty"`
}

func (GenerationAttempt) TableName() string { return "generation_attempts" }

type AgentStep struct {
	ID               uuid.UUID  `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	ConversationID   uuid.UUID  `gorm:"column:conversation_id;type:uuid;not null" json:"conversation_id"`
	TurnID           uuid.UUID  `gorm:"column:turn_id;type:uuid;not null" json:"turn_id"`
	AttemptID        uuid.UUID  `gorm:"column:attempt_id;type:uuid;not null" json:"attempt_id"`
	StepNo           int        `gorm:"column:step_no;not null" json:"step_no"`
	Status           string     `gorm:"column:status;not null" json:"status"`
	OutputLane       string     `gorm:"column:output_lane;not null" json:"output_lane"`
	Model            string     `gorm:"column:model;not null" json:"model"`
	Protocol         string     `gorm:"column:protocol;not null" json:"protocol"`
	ToolSnapshot     JSONDoc    `gorm:"column:tool_snapshot_json;type:jsonb;not null" json:"tool_snapshot"`
	ToolSnapshotHash string     `gorm:"column:tool_snapshot_hash;not null" json:"tool_snapshot_hash"`
	CampaignRevision *int64     `gorm:"column:campaign_revision" json:"campaign_revision,omitempty"`
	FinishReason     string     `gorm:"column:finish_reason;not null" json:"finish_reason"`
	PromptTokens     int        `gorm:"column:prompt_tokens;not null" json:"prompt_tokens"`
	CompletionTokens int        `gorm:"column:completion_tokens;not null" json:"completion_tokens"`
	TokenUsageSource string     `gorm:"column:token_usage_source;not null" json:"token_usage_source"`
	FirstEventMS     int64      `gorm:"column:first_event_ms;not null" json:"first_event_ms"`
	FirstTextMS      int64      `gorm:"column:first_text_ms;not null" json:"first_text_ms"`
	ModelDurationMS  int64      `gorm:"column:model_duration_ms;not null" json:"model_duration_ms"`
	GenerationMS     int64      `gorm:"column:generation_ms;not null" json:"generation_ms"`
	ErrorCode        string     `gorm:"column:error_code;not null" json:"error_code"`
	StartedAt        time.Time  `gorm:"column:started_at;not null" json:"started_at"`
	FinishedAt       *time.Time `gorm:"column:finished_at" json:"finished_at,omitempty"`
}

func (AgentStep) TableName() string { return "agent_steps" }

type AgentToolInvocation struct {
	ID                   uuid.UUID  `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	ConversationID       uuid.UUID  `gorm:"column:conversation_id;type:uuid;not null" json:"conversation_id"`
	TurnID               uuid.UUID  `gorm:"column:turn_id;type:uuid;not null" json:"turn_id"`
	AttemptID            uuid.UUID  `gorm:"column:attempt_id;type:uuid;not null" json:"attempt_id"`
	StepID               uuid.UUID  `gorm:"column:step_id;type:uuid;not null" json:"step_id"`
	StepToolSnapshotHash string     `gorm:"column:step_tool_snapshot_hash;not null" json:"step_tool_snapshot_hash"`
	CallID               string     `gorm:"column:call_id;not null" json:"call_id"`
	ToolName             string     `gorm:"column:tool_name;not null" json:"tool_name"`
	Arguments            JSONMap    `gorm:"column:arguments_json;type:jsonb;not null" json:"arguments"`
	ArgumentsHash        string     `gorm:"column:arguments_hash;not null" json:"arguments_hash"`
	Status               string     `gorm:"column:status;not null" json:"status"`
	ResultCode           string     `gorm:"column:result_code;not null" json:"result_code"`
	Result               JSONMap    `gorm:"column:result_json;type:jsonb;not null" json:"result"`
	IsMutating           bool       `gorm:"column:is_mutating;not null" json:"is_mutating"`
	CommittedAt          *time.Time `gorm:"column:committed_at" json:"committed_at,omitempty"`
	CreatedAt            time.Time  `gorm:"column:created_at;not null" json:"created_at"`
	UpdatedAt            time.Time  `gorm:"column:updated_at;not null" json:"updated_at"`
}

func (AgentToolInvocation) TableName() string { return "agent_tool_invocations" }

type ChatMessage struct {
	ID                 uuid.UUID  `gorm:"column:id;primaryKey" json:"id"`
	ConversationID     uuid.UUID  `gorm:"column:conversation_id" json:"conversation_id"`
	Role               string     `gorm:"column:role" json:"role"`
	Kind               string     `gorm:"column:kind;not null;default:text" json:"kind"`
	Content            string     `gorm:"column:content" json:"content"`
	ContentBlocks      JSONDoc    `gorm:"column:content_blocks;type:jsonb;not null" json:"-"`
	ToolCallID         string     `gorm:"column:tool_call_id;not null" json:"tool_call_id,omitempty"`
	ToolName           string     `gorm:"column:tool_name;not null" json:"tool_name,omitempty"`
	StepID             *uuid.UUID `gorm:"column:step_id;type:uuid" json:"step_id,omitempty"`
	ToolInvocationID   *uuid.UUID `gorm:"column:tool_invocation_id;type:uuid" json:"tool_invocation_id,omitempty"`
	Hidden             bool       `gorm:"column:hidden" json:"hidden"`
	TokenCount         *int       `gorm:"column:token_count" json:"token_count,omitempty"`
	ParentMessageID    *uuid.UUID `gorm:"column:parent_message_id" json:"parent_message_id,omitempty"`
	TurnID             *uuid.UUID `gorm:"column:turn_id;type:uuid" json:"turn_id,omitempty"`
	AttemptID          *uuid.UUID `gorm:"column:attempt_id;type:uuid" json:"attempt_id,omitempty"`
	MessageSeq         int64      `gorm:"column:message_seq;not null" json:"message_seq"`
	TurnSeq            int64      `gorm:"column:turn_seq;->" json:"turn_seq,omitempty"`
	TokenizerVersion   string     `gorm:"column:tokenizer_version;not null" json:"tokenizer_version"`
	InvalidatedAt      *time.Time `gorm:"column:invalidated_at" json:"invalidated_at,omitempty"`
	InvalidationReason string     `gorm:"column:invalidation_reason" json:"invalidation_reason,omitempty"`
	CreatedAt          time.Time  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt          time.Time  `gorm:"column:updated_at" json:"updated_at"`
}

func (ChatMessage) TableName() string { return "chat_messages" }

type DisplayMessage struct {
	ID             uuid.UUID  `gorm:"column:id;primaryKey" json:"id"`
	ConversationID uuid.UUID  `gorm:"column:conversation_id" json:"conversation_id"`
	ChatMessageID  *uuid.UUID `gorm:"column:chat_message_id" json:"chat_message_id,omitempty"`
	Type           string     `gorm:"column:type" json:"type"`
	Markdown       string     `gorm:"column:markdown" json:"markdown"`
	Metadata       JSONMap    `gorm:"column:metadata;type:jsonb" json:"metadata"`
	// Reasoning is a safe, player-facing projection of the linked assistant
	// ChatMessage. It is never persisted in display_messages.
	Reasoning          string     `gorm:"-" json:"reasoning,omitempty"`
	TurnID             *uuid.UUID `gorm:"column:turn_id;type:uuid" json:"turn_id,omitempty"`
	AttemptID          *uuid.UUID `gorm:"column:attempt_id;type:uuid" json:"attempt_id,omitempty"`
	InvalidatedAt      *time.Time `gorm:"column:invalidated_at" json:"invalidated_at,omitempty"`
	InvalidationReason string     `gorm:"column:invalidation_reason" json:"invalidation_reason,omitempty"`
	CreatedAt          time.Time  `gorm:"column:created_at" json:"created_at"`
}

func (DisplayMessage) TableName() string { return "display_messages" }

type ConversationShare struct {
	ID                   uuid.UUID  `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	ConversationID       uuid.UUID  `gorm:"column:conversation_id;type:uuid;not null" json:"conversation_id"`
	PlayerID             uuid.UUID  `gorm:"column:player_id;type:uuid;not null" json:"-"`
	RequestID            uuid.UUID  `gorm:"column:request_id;type:uuid;not null" json:"-"`
	RequestHash          string     `gorm:"column:request_hash;not null" json:"-"`
	BranchEpoch          int64      `gorm:"column:branch_epoch;not null" json:"branch_epoch"`
	Scope                string     `gorm:"column:scope;not null" json:"scope"`
	SourceStartMessageID uuid.UUID  `gorm:"column:source_start_message_id;type:uuid;not null" json:"source_start_message_id"`
	SourceEndMessageID   uuid.UUID  `gorm:"column:source_end_message_id;type:uuid;not null" json:"source_end_message_id"`
	SnapshotVersion      int        `gorm:"column:snapshot_version;not null" json:"snapshot_version"`
	Snapshot             JSONDoc    `gorm:"column:snapshot;type:jsonb;not null" json:"snapshot,omitempty"`
	SnapshotHash         string     `gorm:"column:snapshot_hash;not null" json:"snapshot_hash"`
	Title                string     `gorm:"column:title;not null" json:"title"`
	PublicAttribution    string     `gorm:"column:public_attribution;not null" json:"public_attribution"`
	MessageCount         int        `gorm:"column:message_count;not null" json:"message_count"`
	Status               string     `gorm:"column:status;not null" json:"status"`
	ExpiresAt            *time.Time `gorm:"column:expires_at" json:"expires_at,omitempty"`
	CreatedAt            time.Time  `gorm:"column:created_at;not null" json:"created_at"`
	RevokedAt            *time.Time `gorm:"column:revoked_at" json:"revoked_at,omitempty"`
	ModeratedAt          *time.Time `gorm:"column:moderated_at" json:"moderated_at,omitempty"`
	ModerationReason     string     `gorm:"column:moderation_reason;not null" json:"-"`
	ConversationTitle    string     `gorm:"column:conversation_title;->;-:migration" json:"conversation_title,omitempty"`
}

func (ConversationShare) TableName() string { return "conversation_shares" }

type ConversationShareReport struct {
	ID                  uuid.UUID  `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	ShareID             uuid.UUID  `gorm:"column:share_id;type:uuid;not null" json:"share_id"`
	ReporterFingerprint string     `gorm:"column:reporter_fingerprint;not null" json:"-"`
	Reason              string     `gorm:"column:reason;not null" json:"reason"`
	Details             string     `gorm:"column:details;not null" json:"details"`
	Status              string     `gorm:"column:status;not null" json:"status"`
	CreatedAt           time.Time  `gorm:"column:created_at;not null" json:"created_at"`
	ResolvedAt          *time.Time `gorm:"column:resolved_at" json:"resolved_at,omitempty"`
	ResolvedBy          *uuid.UUID `gorm:"column:resolved_by;type:uuid" json:"resolved_by,omitempty"`
}

func (ConversationShareReport) TableName() string { return "conversation_share_reports" }

type ConversationShareAuditEvent struct {
	ID        uuid.UUID  `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	ShareID   *uuid.UUID `gorm:"column:share_id;type:uuid" json:"share_id,omitempty"`
	ReportID  *uuid.UUID `gorm:"column:report_id;type:uuid" json:"report_id,omitempty"`
	AdminID   *uuid.UUID `gorm:"column:admin_id;type:uuid" json:"admin_id,omitempty"`
	EventType string     `gorm:"column:event_type;not null" json:"event_type"`
	Detail    JSONMap    `gorm:"column:detail;type:jsonb;not null" json:"detail"`
	CreatedAt time.Time  `gorm:"column:created_at;not null" json:"created_at"`
}

func (ConversationShareAuditEvent) TableName() string { return "conversation_share_audit_events" }

type ConversationState struct {
	ConversationID uuid.UUID `gorm:"column:conversation_id;primaryKey" json:"conversation_id"`
	State          JSONMap   `gorm:"column:state;type:jsonb" json:"state"`
	SchemaVersion  int       `gorm:"column:schema_version" json:"schema_version"`
	UpdatedAt      time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (ConversationState) TableName() string { return "conversation_states" }

type StateRevision struct {
	ID                 uuid.UUID  `gorm:"column:id;primaryKey" json:"id"`
	ConversationID     uuid.UUID  `gorm:"column:conversation_id" json:"conversation_id"`
	Source             string     `gorm:"column:source" json:"source"`
	RelatedMessageID   *uuid.UUID `gorm:"column:related_message_id" json:"related_message_id,omitempty"`
	Patch              JSONMap    `gorm:"column:patch;type:jsonb" json:"patch"`
	BeforeState        JSONMap    `gorm:"column:before_state;type:jsonb" json:"before_state"`
	AfterState         JSONMap    `gorm:"column:after_state;type:jsonb" json:"after_state"`
	TurnID             *uuid.UUID `gorm:"column:turn_id;type:uuid" json:"turn_id,omitempty"`
	AttemptID          *uuid.UUID `gorm:"column:attempt_id;type:uuid" json:"attempt_id,omitempty"`
	InvalidatedAt      *time.Time `gorm:"column:invalidated_at" json:"invalidated_at,omitempty"`
	InvalidationReason string     `gorm:"column:invalidation_reason" json:"invalidation_reason,omitempty"`
	CreatedAt          time.Time  `gorm:"column:created_at" json:"created_at"`
}

func (StateRevision) TableName() string { return "state_revisions" }

type ConversationMemory struct {
	ID                     uuid.UUID  `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	ConversationID         uuid.UUID  `gorm:"column:conversation_id;type:uuid;not null" json:"conversation_id"`
	Version                int        `gorm:"column:version;not null" json:"version"`
	PreviousMemoryID       *uuid.UUID `gorm:"column:previous_memory_id;type:uuid" json:"previous_memory_id,omitempty"`
	SourceBranchEpoch      int64      `gorm:"column:source_branch_epoch;not null" json:"source_branch_epoch"`
	CoveredFromTurnSeq     int64      `gorm:"column:covered_from_turn_seq;not null" json:"covered_from_turn_seq"`
	CoveredUntilTurnSeq    int64      `gorm:"column:covered_until_turn_seq;not null" json:"covered_until_turn_seq"`
	CoveredUntilMessageSeq int64      `gorm:"column:covered_until_message_seq;not null" json:"covered_until_message_seq"`
	SchemaVersion          int        `gorm:"column:schema_version;not null" json:"schema_version"`
	PromptVersion          string     `gorm:"column:prompt_version;not null" json:"prompt_version"`
	RendererVersion        string     `gorm:"column:renderer_version;not null" json:"renderer_version"`
	Content                JSONDoc    `gorm:"column:content;type:jsonb;not null" json:"content"`
	RenderedText           string     `gorm:"column:rendered_text;not null" json:"rendered_text"`
	SourceHash             string     `gorm:"column:source_hash;not null" json:"source_hash"`
	GenerationFingerprint  string     `gorm:"column:generation_fingerprint;not null" json:"generation_fingerprint"`
	Model                  string     `gorm:"column:model;not null" json:"model"`
	TokenCount             int        `gorm:"column:token_count;not null" json:"token_count"`
	TokenizerVersion       string     `gorm:"column:tokenizer_version;not null" json:"tokenizer_version"`
	InvalidatedAt          *time.Time `gorm:"column:invalidated_at" json:"invalidated_at,omitempty"`
	InvalidationReason     string     `gorm:"column:invalidation_reason" json:"invalidation_reason,omitempty"`
	CreatedAt              time.Time  `gorm:"column:created_at" json:"created_at"`
}

func (ConversationMemory) TableName() string { return "conversation_memories" }

type MemoryJob struct {
	ID                    uuid.UUID  `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	ConversationID        uuid.UUID  `gorm:"column:conversation_id;type:uuid;not null" json:"conversation_id"`
	PreviousMemoryID      *uuid.UUID `gorm:"column:previous_memory_id;type:uuid" json:"previous_memory_id,omitempty"`
	SourceBranchEpoch     int64      `gorm:"column:source_branch_epoch;not null" json:"source_branch_epoch"`
	TargetUntilTurnSeq    int64      `gorm:"column:target_until_turn_seq;not null" json:"target_until_turn_seq"`
	SourceHash            string     `gorm:"column:source_hash" json:"source_hash,omitempty"`
	GenerationFingerprint string     `gorm:"column:generation_fingerprint" json:"generation_fingerprint,omitempty"`
	Status                string     `gorm:"column:status;not null" json:"status"`
	ExecutionCount        int        `gorm:"column:execution_count;not null" json:"execution_count"`
	MaxExecutions         int        `gorm:"column:max_executions;not null" json:"max_executions"`
	AvailableAt           time.Time  `gorm:"column:available_at;not null" json:"available_at"`
	WorkerID              string     `gorm:"column:worker_id" json:"worker_id,omitempty"`
	LeaseToken            *uuid.UUID `gorm:"column:lease_token;type:uuid" json:"lease_token,omitempty"`
	LeaseExpiresAt        *time.Time `gorm:"column:lease_expires_at" json:"lease_expires_at,omitempty"`
	LastErrorCode         string     `gorm:"column:last_error_code" json:"last_error_code,omitempty"`
	LastErrorMessage      string     `gorm:"column:last_error_message" json:"last_error_message,omitempty"`
	ResultMemoryID        *uuid.UUID `gorm:"column:result_memory_id;type:uuid" json:"result_memory_id,omitempty"`
	CreatedAt             time.Time  `gorm:"column:created_at" json:"created_at"`
	StartedAt             *time.Time `gorm:"column:started_at" json:"started_at,omitempty"`
	FinishedAt            *time.Time `gorm:"column:finished_at" json:"finished_at,omitempty"`
	UpdatedAt             time.Time  `gorm:"column:updated_at" json:"updated_at"`
}

func (MemoryJob) TableName() string { return "memory_jobs" }

type ContextBuildLog struct {
	ID                        uuid.UUID      `gorm:"column:id;primaryKey" json:"id"`
	ConversationID            uuid.UUID      `gorm:"column:conversation_id" json:"conversation_id"`
	TriggerMessageID          *uuid.UUID     `gorm:"column:trigger_message_id" json:"trigger_message_id,omitempty"`
	ChallengeVersionID        uuid.UUID      `gorm:"column:challenge_version_id;type:uuid;not null" json:"challenge_version_id"`
	ChallengeHash             string         `gorm:"column:challenge_hash;not null" json:"challenge_hash"`
	Model                     string         `gorm:"column:model" json:"model"`
	MaxContextTokens          int            `gorm:"column:max_context_tokens" json:"max_context_tokens"`
	ReservedResponseTokens    int            `gorm:"column:reserved_response_tokens" json:"reserved_response_tokens"`
	UsedPromptTokens          int            `gorm:"column:used_prompt_tokens" json:"used_prompt_tokens"`
	Segments                  JSONDoc        `gorm:"column:segments;type:jsonb" json:"segments"`
	IncludedMessageIDs        pq.StringArray `gorm:"column:included_message_ids;type:text[]" json:"included_message_ids"`
	ActivatedWorldEntryIDs    pq.StringArray `gorm:"column:activated_world_entry_ids;type:text[]" json:"activated_world_entry_ids"`
	TurnID                    *uuid.UUID     `gorm:"column:turn_id;type:uuid" json:"turn_id,omitempty"`
	AttemptID                 *uuid.UUID     `gorm:"column:attempt_id;type:uuid" json:"attempt_id,omitempty"`
	BranchEpoch               int64          `gorm:"column:branch_epoch;not null" json:"branch_epoch"`
	MemoryID                  *uuid.UUID     `gorm:"column:memory_id;type:uuid" json:"memory_id,omitempty"`
	MemoryCoveredUntilTurnSeq int64          `gorm:"column:memory_covered_until_turn_seq;not null" json:"memory_covered_until_turn_seq"`
	MemoryLag                 bool           `gorm:"column:memory_lag;not null" json:"memory_lag"`
	OmittedTurnFrom           *int64         `gorm:"column:omitted_turn_from" json:"omitted_turn_from,omitempty"`
	OmittedTurnUntil          *int64         `gorm:"column:omitted_turn_until" json:"omitted_turn_until,omitempty"`
	MemoryJobID               *uuid.UUID     `gorm:"column:memory_job_id;type:uuid" json:"memory_job_id,omitempty"`
	CreatedAt                 time.Time      `gorm:"column:created_at" json:"created_at"`
}

func (ContextBuildLog) TableName() string { return "context_build_logs" }

type LLMSettings struct {
	PlayerID          string  `gorm:"column:player_id;primaryKey;type:uuid" json:"-"`
	Provider          string  `gorm:"column:provider;size:64;not null" json:"provider"`
	BaseURL           string  `gorm:"column:base_url" json:"base_url"`
	Protocol          string  `gorm:"column:protocol" json:"protocol"`
	APIKey            string  `gorm:"-" json:"-"`
	APIKeyEncrypted   string  `gorm:"column:api_key_encrypted" json:"-"`
	HasAPIKey         bool    `gorm:"-" json:"has_api_key"`
	AccessMode        string  `gorm:"-" json:"access_mode"`
	Source            string  `gorm:"-" json:"source"`
	Editable          bool    `gorm:"-" json:"editable"`
	Model             string  `gorm:"column:model" json:"model"`
	MemoryModel       string  `gorm:"column:memory_model" json:"memory_model"`
	InquiryModel      string  `gorm:"column:inquiry_model" json:"inquiry_model"`
	MaxContextTokens  int     `gorm:"column:max_context_tokens" json:"max_context_tokens"`
	MaxResponseTokens int     `gorm:"column:max_response_tokens" json:"max_response_tokens"`
	Temperature       float64 `gorm:"column:temperature" json:"temperature"`
	TopP              float64 `gorm:"column:top_p" json:"top_p"`
	StreamEnabled     bool    `gorm:"column:stream_enabled" json:"stream_enabled"`
	// ReasoningEffort 为空表示不传思考强度；非空时原样透传给叙事请求。
	// 记忆和问答的专用字段为空时继承 ReasoningEffort。
	ReasoningEffort        string    `gorm:"column:reasoning_effort" json:"reasoning_effort"`
	MemoryReasoningEffort  string    `gorm:"column:memory_reasoning_effort" json:"memory_reasoning_effort"`
	InquiryReasoningEffort string    `gorm:"column:inquiry_reasoning_effort" json:"inquiry_reasoning_effort"`
	SettingsRevision       int64     `gorm:"column:settings_revision;not null" json:"settings_revision"`
	UpdatedAt              time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (LLMSettings) TableName() string { return "llm_settings" }

// SystemSettings 是全局玩法配置，只能由管理员修改。
type SystemSettings struct {
	ID                              int       `gorm:"column:id;primaryKey" json:"id"`
	RollSystemEnabled               bool      `gorm:"column:roll_system_enabled;not null;default:true" json:"roll_system_enabled"`
	RollDifficultyMode              string    `gorm:"column:roll_difficulty_mode;size:10;not null;default:normal" json:"roll_difficulty_mode"`
	DetailedLLMLoggingEnabled       bool      `gorm:"column:detailed_llm_logging_enabled;not null;default:false" json:"detailed_llm_logging_enabled"`
	LLMAccessMode                   string    `gorm:"column:llm_access_mode;size:32;not null;default:user_managed" json:"llm_access_mode"`
	PublicLLMBaseURL                string    `gorm:"column:public_llm_base_url" json:"public_llm_base_url"`
	PublicLLMProvider               string    `gorm:"column:public_llm_provider;size:64;not null;default:openai" json:"public_llm_provider"`
	PublicLLMProtocol               string    `gorm:"column:public_llm_protocol;size:32;not null;default:responses" json:"public_llm_protocol"`
	PublicLLMAPIKeyEncrypted        string    `gorm:"column:public_llm_api_key_encrypted" json:"-"`
	PublicLLMHasAPIKey              bool      `gorm:"-" json:"public_llm_has_api_key"`
	PublicLLMModel                  string    `gorm:"column:public_llm_model" json:"public_llm_model"`
	PublicLLMMemoryModel            string    `gorm:"column:public_llm_memory_model" json:"public_llm_memory_model"`
	PublicLLMInquiryModel           string    `gorm:"column:public_llm_inquiry_model" json:"public_llm_inquiry_model"`
	PublicLLMMaxContextTokens       int       `gorm:"column:public_llm_max_context_tokens;not null;default:128000" json:"public_llm_max_context_tokens"`
	PublicLLMMaxResponseTokens      int       `gorm:"column:public_llm_max_response_tokens;not null;default:16384" json:"public_llm_max_response_tokens"`
	PublicLLMTemperature            float64   `gorm:"column:public_llm_temperature;not null;default:0.8" json:"public_llm_temperature"`
	PublicLLMTopP                   float64   `gorm:"column:public_llm_top_p;not null;default:1" json:"public_llm_top_p"`
	PublicLLMStreamEnabled          bool      `gorm:"column:public_llm_stream_enabled;not null;default:true" json:"public_llm_stream_enabled"`
	PublicLLMReasoningEffort        string    `gorm:"column:public_llm_reasoning_effort" json:"public_llm_reasoning_effort"`
	PublicLLMMemoryReasoningEffort  string    `gorm:"column:public_llm_memory_reasoning_effort" json:"public_llm_memory_reasoning_effort"`
	PublicLLMInquiryReasoningEffort string    `gorm:"column:public_llm_inquiry_reasoning_effort" json:"public_llm_inquiry_reasoning_effort"`
	PublicLLMSettingsRevision       int64     `gorm:"column:public_llm_settings_revision;not null;default:1" json:"public_llm_settings_revision"`
	UpdatedAt                       time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (SystemSettings) TableName() string { return "system_settings" }

func DefaultSystemSettings() *SystemSettings {
	return &SystemSettings{
		ID: 1, RollSystemEnabled: true, RollDifficultyMode: "normal",
		PublicLLMBaseURL: "https://api.openai.com/v1",
		LLMAccessMode:    "user_managed", PublicLLMProvider: "openai", PublicLLMProtocol: DefaultLLMProtocol,
		PublicLLMMaxContextTokens: DefaultLLMMaxContextTokens, PublicLLMMaxResponseTokens: DefaultLLMMaxResponseTokens,
		PublicLLMTemperature: 0.8, PublicLLMTopP: 1, PublicLLMStreamEnabled: true, PublicLLMSettingsRevision: 1,
	}
}

// LLMRequestLog 保存一次模型请求的详细诊断快照。请求体不含认证头和 API Key。
type LLMRequestLog struct {
	ID                         uuid.UUID      `gorm:"column:id;primaryKey" json:"id"`
	ConversationID             *uuid.UUID     `gorm:"column:conversation_id;type:uuid" json:"conversation_id,omitempty"`
	MessageID                  *uuid.UUID     `gorm:"column:message_id;type:uuid" json:"message_id,omitempty"`
	Purpose                    string         `gorm:"column:purpose;not null;default:narrative" json:"purpose"`
	AttemptID                  *uuid.UUID     `gorm:"column:attempt_id;type:uuid" json:"attempt_id,omitempty"`
	MemoryJobID                *uuid.UUID     `gorm:"column:memory_job_id;type:uuid" json:"memory_job_id,omitempty"`
	MemoryID                   *uuid.UUID     `gorm:"column:memory_id;type:uuid" json:"memory_id,omitempty"`
	InquiryID                  *uuid.UUID     `gorm:"column:inquiry_id;type:uuid" json:"inquiry_id,omitempty"`
	InquirySnapshotID          *uuid.UUID     `gorm:"column:inquiry_snapshot_id;type:uuid" json:"inquiry_snapshot_id,omitempty"`
	InquirySnapshotBranchEpoch int64          `gorm:"column:inquiry_snapshot_branch_epoch;not null" json:"inquiry_snapshot_branch_epoch"`
	InquirySnapshotRevision    int64          `gorm:"column:inquiry_snapshot_revision;not null" json:"inquiry_snapshot_revision"`
	Model                      string         `gorm:"column:model;not null" json:"model"`
	BaseURL                    string         `gorm:"column:base_url;not null" json:"base_url"`
	Stream                     bool           `gorm:"column:stream;not null" json:"stream"`
	RequestBody                JSONDoc        `gorm:"column:request_body;type:jsonb;not null" json:"request_body"`
	RawResponse                string         `gorm:"column:raw_response;type:text;not null" json:"raw_response"`
	ResponseText               string         `gorm:"column:response_text;type:text;not null" json:"response_text"`
	AuditTruncated             bool           `gorm:"column:audit_truncated;not null" json:"audit_truncated"`
	ResponseStatus             *int           `gorm:"column:response_status" json:"response_status,omitempty"`
	FinishReason               string         `gorm:"column:finish_reason" json:"finish_reason,omitempty"`
	ReceivedDone               bool           `gorm:"column:received_done;not null" json:"received_done"`
	Success                    bool           `gorm:"column:success;not null" json:"success"`
	Error                      string         `gorm:"column:error;type:text" json:"error,omitempty"`
	DurationMS                 int64          `gorm:"column:duration_ms;not null" json:"duration_ms"`
	SettingsRevision           int64          `gorm:"column:settings_revision;not null" json:"settings_revision"`
	SettingsSource             string         `gorm:"column:settings_source;not null;default:user" json:"settings_source"`
	OutputMode                 string         `gorm:"column:output_mode" json:"output_mode,omitempty"`
	ContractName               string         `gorm:"column:contract_name" json:"contract_name,omitempty"`
	ContractVersion            string         `gorm:"column:contract_version" json:"contract_version,omitempty"`
	ContractFingerprint        string         `gorm:"column:contract_fingerprint" json:"contract_fingerprint,omitempty"`
	TransportSuccess           bool           `gorm:"column:transport_success;not null" json:"transport_success"`
	StructuredSuccess          bool           `gorm:"column:structured_success;not null" json:"structured_success"`
	StructuredErrorCode        string         `gorm:"column:structured_error_code" json:"structured_error_code,omitempty"`
	OutputTypes                pq.StringArray `gorm:"column:output_types;type:text[]" json:"output_types"`
	CreatedAt                  time.Time      `gorm:"column:created_at" json:"created_at"`
}

func (LLMRequestLog) TableName() string { return "llm_request_logs" }

type CharacterInquiryThread struct {
	ID             uuid.UUID `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	ConversationID uuid.UUID `gorm:"column:conversation_id;type:uuid;not null" json:"conversation_id"`
	PlayerID       uuid.UUID `gorm:"column:player_id;type:uuid;not null" json:"player_id"`
	CreatedAt      time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt      time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (CharacterInquiryThread) TableName() string { return "character_inquiry_threads" }

type CharacterInquiryMessage struct {
	ID                  uuid.UUID  `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	ThreadID            uuid.UUID  `gorm:"column:thread_id;type:uuid;not null" json:"thread_id"`
	ConversationID      uuid.UUID  `gorm:"column:conversation_id;type:uuid;not null" json:"conversation_id"`
	PlayerID            uuid.UUID  `gorm:"column:player_id;type:uuid;not null" json:"player_id"`
	Role                string     `gorm:"column:role;not null" json:"role"`
	Content             string     `gorm:"column:content;not null" json:"content"`
	SnapshotID          *uuid.UUID `gorm:"column:snapshot_id;type:uuid" json:"snapshot_id,omitempty"`
	SnapshotBranchEpoch int64      `gorm:"column:snapshot_branch_epoch;not null" json:"snapshot_branch_epoch"`
	SnapshotRevision    int64      `gorm:"column:snapshot_revision;not null" json:"snapshot_revision"`
	SourceRefs          JSONDoc    `gorm:"column:source_refs;type:jsonb;not null" json:"source_refs"`
	Status              string     `gorm:"column:status;not null" json:"status"`
	ErrorCode           string     `gorm:"column:error_code" json:"error_code,omitempty"`
	CreatedAt           time.Time  `gorm:"column:created_at" json:"created_at"`
}

func (CharacterInquiryMessage) TableName() string { return "character_inquiry_messages" }

type CharacterInquiryRateLimit struct {
	PlayerID        uuid.UUID `gorm:"column:player_id;primaryKey;type:uuid" json:"player_id"`
	WindowStartedAt time.Time `gorm:"column:window_started_at;not null" json:"window_started_at"`
	RequestCount    int       `gorm:"column:request_count;not null" json:"request_count"`
	UpdatedAt       time.Time `gorm:"column:updated_at;not null" json:"updated_at"`
}

func (CharacterInquiryRateLimit) TableName() string { return "character_inquiry_rate_limits" }
