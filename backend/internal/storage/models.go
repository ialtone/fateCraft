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
	SourceUser   = "user"
	SourceSystem = "system"
)

// ---------------------------------------------------------------------------
// Entity 接口（通用实体接口，用于泛型约束）
// ---------------------------------------------------------------------------

type Entity interface {
	GetID() string
	GetUpdatedAt() time.Time
}

// ---------------------------------------------------------------------------
// 模板实体（存储于外部 JSON 文件，不入数据库；id 为 slug 字符串即文件名）
// ---------------------------------------------------------------------------

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
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	Segments    []PromptSegment `json:"segments"`
}

func (p PromptPreset) GetID() string           { return p.ID }
func (p PromptPreset) GetUpdatedAt() time.Time { return p.UpdatedAt }

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
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
	Entries     []WorldBookEntry `json:"entries"`
}

func (b WorldBook) GetID() string           { return b.ID }
func (b WorldBook) GetUpdatedAt() time.Time { return b.UpdatedAt }

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
// 对话与运行时数据
// ---------------------------------------------------------------------------

type Conversation struct {
	ID                   uuid.UUID  `gorm:"column:id;primaryKey" json:"id"`
	PlayerID             string     `gorm:"column:player_id" json:"player_id"`
	Title                string     `gorm:"column:title" json:"title"`
	Status               string     `gorm:"column:status" json:"status"`
	IsProcessing         bool       `gorm:"column:is_processing" json:"is_processing"`
	CharacterID          *string    `gorm:"column:character_id" json:"character_id,omitempty"`
	PersonaID            *string    `gorm:"column:persona_id" json:"persona_id,omitempty"`
	ScenarioID           *string    `gorm:"column:scenario_id" json:"scenario_id,omitempty"`
	PromptPresetID       *string    `gorm:"column:prompt_preset_id" json:"prompt_preset_id,omitempty"`
	RewindsUsedThisTrial int        `gorm:"column:rewinds_used_this_trial;not null;default:0" json:"rewinds_used_this_trial"`
	RollSystemEnabled    *bool      `gorm:"column:roll_system_enabled" json:"roll_system_enabled,omitempty"`   // 对话级判定系统开关（null=使用全局）
	RollDifficultyMode   *string    `gorm:"column:roll_difficulty_mode" json:"roll_difficulty_mode,omitempty"` // 对话级难度（null=使用全局）
	CreatedAt            time.Time  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt            time.Time  `gorm:"column:updated_at" json:"updated_at"`
	EndedAt              *time.Time `gorm:"column:ended_at" json:"ended_at,omitempty"`
	ArchivedAt           *time.Time `gorm:"column:archived_at" json:"archived_at,omitempty"`
	DeletedAt            *time.Time `gorm:"column:deleted_at" json:"deleted_at,omitempty"`
}

func (Conversation) TableName() string { return "conversations" }

type ConversationWorldBook struct {
	ConversationID uuid.UUID `gorm:"column:conversation_id;primaryKey" json:"conversation_id"`
	WorldBookID    string    `gorm:"column:world_book_id;primaryKey" json:"world_book_id"`
}

func (ConversationWorldBook) TableName() string { return "conversation_world_books" }

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
	ID                 int       `gorm:"column:id;primaryKey" json:"id"`
	BaseURL            string    `gorm:"column:base_url" json:"base_url"`
	APIKey             string    `gorm:"column:api_key" json:"-"`
	HasAPIKey          bool      `gorm:"-" json:"has_api_key"`
	Model              string    `gorm:"column:model" json:"model"`
	MaxContextTokens   int       `gorm:"column:max_context_tokens" json:"max_context_tokens"`
	MaxResponseTokens  int       `gorm:"column:max_response_tokens" json:"max_response_tokens"`
	Temperature        float64   `gorm:"column:temperature" json:"temperature"`
	TopP               float64   `gorm:"column:top_p" json:"top_p"`
	StreamEnabled      bool      `gorm:"column:stream_enabled" json:"stream_enabled"`
	RollSystemEnabled  bool      `gorm:"column:roll_system_enabled;default:false" json:"roll_system_enabled"`      // 是否启用判定系统
	RollDifficultyMode string    `gorm:"column:roll_difficulty_mode;default:'normal'" json:"roll_difficulty_mode"` // 判定难度：easy/normal/hard
	UpdatedAt          time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (LLMSettings) TableName() string { return "llm_settings" }

// AllModels 列出所有数据库模型，供 AutoMigrate 使用（模板实体存文件，不在其中）。
func AllModels() []any {
	return []any{
		&Conversation{}, &ConversationWorldBook{},
		&ChatMessage{}, &DisplayMessage{},
		&ConversationState{}, &StateRevision{},
		&MemorySummary{}, &ContextBuildLog{},
		&LLMSettings{}, &Account{}, &TrialHistory{}, &RollHistoryEntry{},
	}
}
