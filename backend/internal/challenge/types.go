package challenge

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/storage"
)

type Manifest struct {
	ID              string        `json:"id"`
	Name            string        `json:"name"`
	Version         string        `json:"version"`
	Description     string        `json:"description"`
	Author          string        `json:"author"`
	Icon            string        `json:"icon,omitempty"`
	Tags            []string      `json:"tags"`
	DefaultConfig   DefaultConfig `json:"default_config"`
	QuickStartGuide string        `json:"quick_start_guide"`
	Features        []string      `json:"features"`
	Dependencies    []string      `json:"dependencies"`
	CreatedAt       string        `json:"created_at,omitempty"`
	UpdatedAt       string        `json:"updated_at,omitempty"`
	RewardRules     []RewardSpec  `json:"reward_rules,omitempty"`
}

type DefaultConfig struct {
	RecommendedCharacter string   `json:"recommended_character"`
	RecommendedPersona   string   `json:"recommended_persona"`
	RecommendedScenario  string   `json:"recommended_scenario"`
	RecommendedPrompt    string   `json:"recommended_prompt"`
	RequiredWorldBooks   []string `json:"required_worldbooks"`
	AllowedPersonas      []string `json:"allowed_personas,omitempty"`
}

type RewardSpec struct {
	RewardKey   string         `json:"reward_key"`
	EndingType  string         `json:"ending_type"`
	EndingTitle string         `json:"ending_title"`
	Conditions  map[string]any `json:"conditions"`
	Calculation map[string]any `json:"calculation"`
	MaxReward   *int64         `json:"max_reward,omitempty"`
	Enabled     *bool          `json:"enabled,omitempty"`
}

type AssetOption struct {
	ID          uuid.UUID `json:"id"`
	Slug        string    `json:"slug"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
}

type PublicChallenge struct {
	ID              uuid.UUID     `json:"id"`
	Slug            string        `json:"slug"`
	Name            string        `json:"name"`
	Description     string        `json:"description"`
	Version         string        `json:"version"`
	Icon            string        `json:"icon,omitempty"`
	Tags            []string      `json:"tags"`
	Features        []string      `json:"features"`
	QuickStartGuide string        `json:"quick_start_guide"`
	Personas        []AssetOption `json:"personas"`
	RewardOverview  []RewardView  `json:"reward_overview"`
}

type RewardView struct {
	RewardKey   string `json:"reward_key"`
	EndingTitle string `json:"ending_title"`
	Multiplier  string `json:"multiplier"`
}

type Selection struct {
	Version   storage.ChallengeVersion
	Character *storage.ChallengeAsset
	Persona   *storage.ChallengeAsset
	Scenario  *storage.ChallengeAsset
}

type Bundle struct {
	Version    storage.ChallengeVersion
	Manifest   Manifest
	Character  *storage.Character
	Persona    *storage.Persona
	Scenario   *storage.Scenario
	Prompt     *storage.PromptPreset
	WorldBooks []storage.WorldBook
}

type AdminPack struct {
	Pack     storage.ChallengePack      `json:"pack"`
	Versions []storage.ChallengeVersion `json:"versions"`
}

type VersionDocument struct {
	Version     storage.ChallengeVersion      `json:"version"`
	Assets      []storage.ChallengeAsset      `json:"assets"`
	RewardRules []storage.ChallengeRewardRule `json:"reward_rules"`
}

type SaveVersionInput struct {
	Manifest    json.RawMessage               `json:"manifest"`
	Assets      []storage.ChallengeAsset      `json:"assets"`
	RewardRules []storage.ChallengeRewardRule `json:"reward_rules"`
}

type CreatePackInput struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type SettlementInput struct {
	ConversationID   uuid.UUID
	PlayerID         uuid.UUID
	EndingType       string
	EndingTitle      string
	RewardKey        string
	BaseReward       int64
	CharacterName    string
	CultivationLevel string
	SurvivalTurns    int
	SettledAt        time.Time
}

type SettlementResult struct {
	Run storage.ChallengeRun `json:"run"`
}
