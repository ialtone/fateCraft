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

type ChallengeRewardRule struct {
	ID                 uuid.UUID `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	ChallengeVersionID uuid.UUID `gorm:"column:challenge_version_id;type:uuid;not null;index" json:"challenge_version_id"`
	RewardKey          string    `gorm:"column:reward_key;not null" json:"reward_key"`
	EndingType         string    `gorm:"column:ending_type;not null" json:"ending_type"`
	EndingTitle        string    `gorm:"column:ending_title;not null" json:"ending_title"`
	Conditions         JSONMap   `gorm:"column:conditions;type:jsonb;not null" json:"conditions"`
	Calculation        JSONMap   `gorm:"column:calculation;type:jsonb;not null" json:"calculation"`
	MaxReward          *int64    `gorm:"column:max_reward" json:"max_reward,omitempty"`
	Enabled            bool      `gorm:"column:enabled;not null;default:true" json:"enabled"`
	CreatedAt          time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt          time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (ChallengeRewardRule) TableName() string { return "challenge_reward_rules" }

type ChallengeRun struct {
	ID                 uuid.UUID `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	ConversationID     uuid.UUID `gorm:"column:conversation_id;type:uuid;not null;uniqueIndex" json:"conversation_id"`
	PlayerID           uuid.UUID `gorm:"column:player_id;type:uuid;not null;index" json:"player_id"`
	ChallengeVersionID uuid.UUID `gorm:"column:challenge_version_id;type:uuid;not null;index" json:"challenge_version_id"`
	Result             string    `gorm:"column:result;not null" json:"result"`
	RewardKey          *string   `gorm:"column:reward_key" json:"reward_key,omitempty"`
	RewardAmount       int64     `gorm:"column:reward_amount;not null;default:0" json:"reward_amount"`
	SettlementDetail   JSONMap   `gorm:"column:settlement_detail;type:jsonb;not null" json:"settlement_detail"`
	SettledAt          time.Time `gorm:"column:settled_at;not null" json:"settled_at"`
}

func (ChallengeRun) TableName() string { return "challenge_runs" }

// ---------------------------------------------------------------------------
// 对话与运行时数据
// ---------------------------------------------------------------------------

type Conversation struct {
	ID                   uuid.UUID  `gorm:"column:id;primaryKey" json:"id"`
	PlayerID             string     `gorm:"column:player_id;type:uuid" json:"player_id"`
	Title                string     `gorm:"column:title" json:"title"`
	Status               string     `gorm:"column:status" json:"status"`
	IsProcessing         bool       `gorm:"column:is_processing" json:"is_processing"`
	ChallengeVersionID   uuid.UUID  `gorm:"column:challenge_version_id;type:uuid;not null" json:"challenge_version_id"`
	ChallengeHash        string     `gorm:"column:challenge_hash;not null" json:"challenge_hash"`
	CharacterAssetID     *uuid.UUID `gorm:"column:character_asset_id;type:uuid" json:"character_asset_id,omitempty"`
	PersonaAssetID       *uuid.UUID `gorm:"column:persona_asset_id;type:uuid" json:"persona_asset_id,omitempty"`
	ScenarioAssetID      *uuid.UUID `gorm:"column:scenario_asset_id;type:uuid" json:"scenario_asset_id,omitempty"`
	RewindsUsedThisTrial int        `gorm:"column:rewinds_used_this_trial;not null;default:0" json:"rewinds_used_this_trial"`
	CreatedAt            time.Time  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt            time.Time  `gorm:"column:updated_at" json:"updated_at"`
	EndedAt              *time.Time `gorm:"column:ended_at" json:"ended_at,omitempty"`
	ArchivedAt           *time.Time `gorm:"column:archived_at" json:"archived_at,omitempty"`
	DeletedAt            *time.Time `gorm:"column:deleted_at" json:"deleted_at,omitempty"`
}

func (Conversation) TableName() string { return "conversations" }

type ChatMessage struct {
	ID              uuid.UUID  `gorm:"column:id;primaryKey" json:"id"`
	ConversationID  uuid.UUID  `gorm:"column:conversation_id" json:"conversation_id"`
	Role            string     `gorm:"column:role" json:"role"`
	Content         string     `gorm:"column:content" json:"content"`
	Hidden          bool       `gorm:"column:hidden" json:"hidden"`
	TokenCount      *int       `gorm:"column:token_count" json:"token_count,omitempty"`
	ParentMessageID *uuid.UUID `gorm:"column:parent_message_id" json:"parent_message_id,omitempty"`
	CreatedAt       time.Time  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt       time.Time  `gorm:"column:updated_at" json:"updated_at"`
}

func (ChatMessage) TableName() string { return "chat_messages" }

type DisplayMessage struct {
	ID             uuid.UUID  `gorm:"column:id;primaryKey" json:"id"`
	ConversationID uuid.UUID  `gorm:"column:conversation_id" json:"conversation_id"`
	ChatMessageID  *uuid.UUID `gorm:"column:chat_message_id" json:"chat_message_id,omitempty"`
	Type           string     `gorm:"column:type" json:"type"`
	Markdown       string     `gorm:"column:markdown" json:"markdown"`
	Metadata       JSONMap    `gorm:"column:metadata;type:jsonb" json:"metadata"`
	CreatedAt      time.Time  `gorm:"column:created_at" json:"created_at"`
}

func (DisplayMessage) TableName() string { return "display_messages" }

type ConversationState struct {
	ConversationID uuid.UUID `gorm:"column:conversation_id;primaryKey" json:"conversation_id"`
	State          JSONMap   `gorm:"column:state;type:jsonb" json:"state"`
	SchemaVersion  int       `gorm:"column:schema_version" json:"schema_version"`
	UpdatedAt      time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (ConversationState) TableName() string { return "conversation_states" }

type StateRevision struct {
	ID               uuid.UUID  `gorm:"column:id;primaryKey" json:"id"`
	ConversationID   uuid.UUID  `gorm:"column:conversation_id" json:"conversation_id"`
	Source           string     `gorm:"column:source" json:"source"`
	RelatedMessageID *uuid.UUID `gorm:"column:related_message_id" json:"related_message_id,omitempty"`
	Patch            JSONMap    `gorm:"column:patch;type:jsonb" json:"patch"`
	BeforeState      JSONMap    `gorm:"column:before_state;type:jsonb" json:"before_state"`
	AfterState       JSONMap    `gorm:"column:after_state;type:jsonb" json:"after_state"`
	CreatedAt        time.Time  `gorm:"column:created_at" json:"created_at"`
}

func (StateRevision) TableName() string { return "state_revisions" }

type MemorySummary struct {
	ID                    uuid.UUID  `gorm:"column:id;primaryKey" json:"id"`
	ConversationID        uuid.UUID  `gorm:"column:conversation_id" json:"conversation_id"`
	Version               int        `gorm:"column:version" json:"version"`
	Content               string     `gorm:"column:content" json:"content"`
	CoveredUntilMessageID *uuid.UUID `gorm:"column:covered_until_message_id" json:"covered_until_message_id,omitempty"`
	TokenCount            *int       `gorm:"column:token_count" json:"token_count,omitempty"`
	CreatedAt             time.Time  `gorm:"column:created_at" json:"created_at"`
}

func (MemorySummary) TableName() string { return "memory_summaries" }

type ContextBuildLog struct {
	ID                     uuid.UUID      `gorm:"column:id;primaryKey" json:"id"`
	ConversationID         uuid.UUID      `gorm:"column:conversation_id" json:"conversation_id"`
	TriggerMessageID       *uuid.UUID     `gorm:"column:trigger_message_id" json:"trigger_message_id,omitempty"`
	ChallengeVersionID     uuid.UUID      `gorm:"column:challenge_version_id;type:uuid;not null" json:"challenge_version_id"`
	ChallengeHash          string         `gorm:"column:challenge_hash;not null" json:"challenge_hash"`
	Model                  string         `gorm:"column:model" json:"model"`
	MaxContextTokens       int            `gorm:"column:max_context_tokens" json:"max_context_tokens"`
	ReservedResponseTokens int            `gorm:"column:reserved_response_tokens" json:"reserved_response_tokens"`
	UsedPromptTokens       int            `gorm:"column:used_prompt_tokens" json:"used_prompt_tokens"`
	Segments               JSONDoc        `gorm:"column:segments;type:jsonb" json:"segments"`
	IncludedMessageIDs     pq.StringArray `gorm:"column:included_message_ids;type:text[]" json:"included_message_ids"`
	ActivatedWorldEntryIDs pq.StringArray `gorm:"column:activated_world_entry_ids;type:text[]" json:"activated_world_entry_ids"`
	CreatedAt              time.Time      `gorm:"column:created_at" json:"created_at"`
}

func (ContextBuildLog) TableName() string { return "context_build_logs" }

type LLMSettings struct {
	PlayerID          string    `gorm:"column:player_id;primaryKey;type:uuid" json:"-"`
	BaseURL           string    `gorm:"column:base_url" json:"base_url"`
	APIKey            string    `gorm:"column:api_key" json:"-"`
	HasAPIKey         bool      `gorm:"-" json:"has_api_key"`
	Model             string    `gorm:"column:model" json:"model"`
	MaxContextTokens  int       `gorm:"column:max_context_tokens" json:"max_context_tokens"`
	MaxResponseTokens int       `gorm:"column:max_response_tokens" json:"max_response_tokens"`
	Temperature       float64   `gorm:"column:temperature" json:"temperature"`
	TopP              float64   `gorm:"column:top_p" json:"top_p"`
	StreamEnabled     bool      `gorm:"column:stream_enabled" json:"stream_enabled"`
	UpdatedAt         time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (LLMSettings) TableName() string { return "llm_settings" }

// SystemSettings 是全局玩法配置，只能由管理员修改。
type SystemSettings struct {
	ID                 int       `gorm:"column:id;primaryKey" json:"id"`
	RollSystemEnabled  bool      `gorm:"column:roll_system_enabled;not null;default:true" json:"roll_system_enabled"`
	RollDifficultyMode string    `gorm:"column:roll_difficulty_mode;size:10;not null;default:normal" json:"roll_difficulty_mode"`
	UpdatedAt          time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (SystemSettings) TableName() string { return "system_settings" }

func DefaultSystemSettings() *SystemSettings {
	return &SystemSettings{ID: 1, RollSystemEnabled: true, RollDifficultyMode: "normal"}
}
