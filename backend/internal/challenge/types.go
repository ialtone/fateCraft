package challenge

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/platformcapability"
	"github.com/ialtone/fatecraft/internal/storage"
)

type Manifest struct {
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	Version          string         `json:"version"`
	RuntimeEntry     string         `json:"runtime_entry,omitempty"`
	Description      string         `json:"description"`
	Author           string         `json:"author"`
	Icon             string         `json:"icon,omitempty"`
	Tags             []string       `json:"tags"`
	DefaultConfig    DefaultConfig  `json:"default_config"`
	QuickStartGuide  string         `json:"quick_start_guide"`
	Features         []string       `json:"features"`
	Dependencies     []string       `json:"dependencies"`
	EntryRequirement map[string]any `json:"entry_requirement,omitempty"`
	CreatedAt        string         `json:"created_at,omitempty"`
	UpdatedAt        string         `json:"updated_at,omitempty"`
	Outcomes         []OutcomeSpec  `json:"outcomes,omitempty"`
}

type DefaultConfig struct {
	RecommendedCharacter string   `json:"recommended_character"`
	RecommendedPersona   string   `json:"recommended_persona"`
	RecommendedScenario  string   `json:"recommended_scenario"`
	RecommendedPrompt    string   `json:"recommended_prompt"`
	RequiredWorldBooks   []string `json:"required_worldbooks"`
	AllowedPersonas      []string `json:"allowed_personas,omitempty"`
}

type OutcomeSpec struct {
	RewardPoints   int64          `json:"reward_points"`
	OutcomeKey     string         `json:"outcome_key"`
	Category       string         `json:"category"`
	Title          string         `json:"title"`
	Tone           string         `json:"tone"`
	VerifierKey    string         `json:"verifier_key"`
	VerifierConfig map[string]any `json:"verifier_config"`
	Enabled        *bool          `json:"enabled,omitempty"`
}

type AssetOption struct {
	ID          uuid.UUID `json:"id"`
	Slug        string    `json:"slug"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
}

type OutcomeView struct {
	RewardPoints int64  `json:"reward_points"`
	Key          string `json:"key"`
	Category     string `json:"category"`
	Title        string `json:"title"`
	Tone         string `json:"tone"`
}

type RewardView struct {
	Points       int64  `json:"points"`
	OutcomeKey   string `json:"outcome_key"`
	OutcomeTitle string `json:"outcome_title"`
}

type PublicRuntime struct {
	EngineKey         string                         `json:"engine_key"`
	Mode              string                         `json:"mode"`
	HasTrustedActions bool                           `json:"has_trusted_actions"`
	Presentation      gameplay.Presentation          `json:"presentation"`
	Capabilities      map[string]gameplay.Capability `json:"capabilities"`
	// PlatformCapabilities are owned by Fatecraft and are independent of the
	// selected challenge package. Gameplay capabilities remain in Capabilities.
	PlatformCapabilities map[string]platformcapability.Capability `json:"platform_capabilities"`
	Outcomes             []OutcomeView                            `json:"outcomes,omitempty"`
}

type PublicChallenge struct {
	ID                uuid.UUID     `json:"id"`
	Slug              string        `json:"slug"`
	Name              string        `json:"name"`
	Description       string        `json:"description"`
	Version           string        `json:"version"`
	Icon              string        `json:"icon,omitempty"`
	Tags              []string      `json:"tags"`
	Features          []string      `json:"features"`
	QuickStartGuide   string        `json:"quick_start_guide"`
	Personas          []AssetOption `json:"personas"`
	Outcomes          []OutcomeView `json:"outcomes"`
	RewardOverview    []RewardView  `json:"reward_overview"`
	Runtime           PublicRuntime `json:"runtime"`
	Available         bool          `json:"available"`
	UnavailableReason string        `json:"unavailable_reason,omitempty"`
}

type Selection struct {
	ChallengeName string
	Version       storage.ChallengeVersion
	Runtime       storage.ChallengeRuntimeConfig
	Contract      gameplay.RuntimeContract
	Character     *storage.ChallengeAsset
	Persona       *storage.ChallengeAsset
	Scenario      *storage.ChallengeAsset
}

type Bundle struct {
	Version      storage.ChallengeVersion
	Runtime      storage.ChallengeRuntimeConfig
	Contract     gameplay.RuntimeContract
	Manifest     Manifest
	Character    *storage.Character
	Persona      *storage.Persona
	Scenario     *storage.Scenario
	Prompt       *storage.PromptPreset
	WorldEntries []storage.WorldBookEntry
}

type AdminPack struct {
	Pack     storage.ChallengePack      `json:"pack"`
	Versions []storage.ChallengeVersion `json:"versions"`
}

type VersionDocument struct {
	Version     storage.ChallengeVersion       `json:"version"`
	Runtime     storage.ChallengeRuntimeConfig `json:"runtime"`
	Assets      []storage.ChallengeAsset       `json:"assets"`
	Definitions []storage.GameplayDefinition   `json:"definitions"`
	Outcomes    []storage.ChallengeOutcomeRule `json:"outcomes"`
}

type SaveVersionInput struct {
	ExpectedRevision int64                          `json:"expected_revision"`
	Manifest         json.RawMessage                `json:"manifest"`
	Runtime          storage.ChallengeRuntimeConfig `json:"runtime"`
	Assets           []storage.ChallengeAsset       `json:"assets"`
	Definitions      []storage.GameplayDefinition   `json:"definitions"`
	Outcomes         []storage.ChallengeOutcomeRule `json:"outcomes"`
}

type CreatePackInput struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type SettlementInput struct {
	ConversationID uuid.UUID
	PlayerID       uuid.UUID
	OutcomeKey     string
	Summary        string
	TrustedState   map[string]any
	TurnCount      int
	SettledAt      time.Time
}

type SettlementResult struct {
	Run     storage.ChallengeRun         `json:"run"`
	Outcome OutcomeView                  `json:"outcome"`
	Rewards []storage.ChallengeRunReward `json:"rewards"`
	// RewardsDisabledReason 非空表示本结算未发放奖励的原因（client 路由为 "client_llm_route"）。
	RewardsDisabledReason string `json:"rewards_disabled_reason,omitempty"`
}
