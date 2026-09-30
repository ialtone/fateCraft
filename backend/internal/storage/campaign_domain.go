package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"gorm.io/gorm"
)

const (
	DefinitionRealm        = "realm"
	DefinitionTechnique    = "technique"
	DefinitionItem         = "item"
	DefinitionEquipment    = "equipment"
	DefinitionRecipe       = "recipe"
	DefinitionMarket       = "market"
	DefinitionLocation     = "location"
	DefinitionFaction      = "faction"
	DefinitionNPCArchetype = "npc_archetype"
	DefinitionEncounter    = "encounter"
	DefinitionWorldEvent   = "world_event"
	DefinitionQuest        = "quest"
)

// GameplayDefinition is immutable, version-scoped structured gameplay content.
// Narrative assets reference definitions by stable kind/key pairs.
type GameplayDefinition struct {
	ID                 uuid.UUID      `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	ChallengeVersionID uuid.UUID      `gorm:"column:challenge_version_id;type:uuid;not null;index" json:"challenge_version_id"`
	Kind               string         `gorm:"column:kind;not null" json:"kind"`
	Key                string         `gorm:"column:key;not null" json:"key"`
	Name               string         `gorm:"column:name;not null" json:"name"`
	Content            JSONMap        `gorm:"column:content;type:jsonb;not null" json:"content"`
	Tags               pq.StringArray `gorm:"column:tags;type:text[];not null" json:"tags"`
	SearchText         string         `gorm:"column:search_text;not null" json:"search_text"`
	Visibility         string         `gorm:"column:visibility;not null" json:"visibility"`
	SortOrder          int            `gorm:"column:sort_order;not null" json:"sort_order"`
	CreatedAt          time.Time      `gorm:"column:created_at" json:"created_at"`
	UpdatedAt          time.Time      `gorm:"column:updated_at" json:"updated_at"`
}

func (GameplayDefinition) TableName() string { return "gameplay_definitions" }

type CampaignCharacter struct {
	ID                    uuid.UUID `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	CampaignID            uuid.UUID `gorm:"column:campaign_id;type:uuid;not null;uniqueIndex" json:"campaign_id"`
	Name                  string    `gorm:"column:name;not null" json:"name"`
	Origin                string    `gorm:"column:origin;not null" json:"origin"`
	PublicIdentity        string    `gorm:"column:public_identity;not null" json:"public_identity"`
	RealmKey              string    `gorm:"column:realm_key;not null" json:"realm_key"`
	StageKey              string    `gorm:"column:stage_key;not null" json:"stage_key"`
	RealmRank             int       `gorm:"column:realm_rank;not null" json:"realm_rank"`
	CultivationProgress   int64     `gorm:"column:cultivation_progress;not null" json:"cultivation_progress"`
	CultivationRequired   int64     `gorm:"column:cultivation_required;not null" json:"cultivation_required"`
	FoundationQuality     int       `gorm:"column:foundation_quality;not null" json:"foundation_quality"`
	AgeDays               int64     `gorm:"column:age_days;not null" json:"age_days"`
	LifespanDays          int64     `gorm:"column:lifespan_days;not null" json:"lifespan_days"`
	Health                int64     `gorm:"column:health;not null" json:"health"`
	BaseMaxHealth         int64     `gorm:"column:base_max_health;not null" json:"base_max_health"`
	MaxHealth             int64     `gorm:"column:max_health;not null" json:"max_health"`
	Mana                  int64     `gorm:"column:mana;not null" json:"mana"`
	BaseMaxMana           int64     `gorm:"column:base_max_mana;not null" json:"base_max_mana"`
	MaxMana               int64     `gorm:"column:max_mana;not null" json:"max_mana"`
	SpiritualSense        int64     `gorm:"column:spiritual_sense;not null" json:"spiritual_sense"`
	BaseMaxSpiritualSense int64     `gorm:"column:base_max_spiritual_sense;not null" json:"base_max_spiritual_sense"`
	MaxSpiritualSense     int64     `gorm:"column:max_spiritual_sense;not null" json:"max_spiritual_sense"`
	Injuries              JSONMap   `gorm:"column:injuries;type:jsonb;not null" json:"injuries"`
	LongTermEffects       JSONMap   `gorm:"column:long_term_effects;type:jsonb;not null" json:"long_term_effects"`
	CreatedAt             time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt             time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (CampaignCharacter) TableName() string { return "campaign_characters" }

type CampaignTechnique struct {
	ID             uuid.UUID `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	CampaignID     uuid.UUID `gorm:"column:campaign_id;type:uuid;not null;index" json:"campaign_id"`
	TechniqueKey   string    `gorm:"column:technique_key;not null" json:"technique_key"`
	Category       string    `gorm:"column:category;not null" json:"category"`
	LearnedLayer   int       `gorm:"column:learned_layer;not null" json:"learned_layer"`
	Mastery        int64     `gorm:"column:mastery;not null" json:"mastery"`
	Insight        int64     `gorm:"column:insight;not null" json:"insight"`
	EquippedSlot   *string   `gorm:"column:equipped_slot" json:"equipped_slot,omitempty"`
	Status         string    `gorm:"column:status;not null" json:"status"`
	SourceSnapshot JSONMap   `gorm:"column:source_snapshot;type:jsonb;not null" json:"source_snapshot"`
	CreatedAt      time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt      time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (CampaignTechnique) TableName() string { return "campaign_techniques" }

type CampaignItemStack struct {
	CampaignID uuid.UUID `gorm:"column:campaign_id;primaryKey;type:uuid" json:"campaign_id"`
	ItemKey    string    `gorm:"column:item_key;primaryKey" json:"item_key"`
	QualityKey string    `gorm:"column:quality_key;primaryKey" json:"quality_key"`
	Quantity   int64     `gorm:"column:quantity;not null" json:"quantity"`
	Metadata   JSONMap   `gorm:"column:metadata;type:jsonb;not null" json:"metadata"`
	UpdatedAt  time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (CampaignItemStack) TableName() string { return "campaign_item_stacks" }

type CampaignItemInstance struct {
	ID               uuid.UUID  `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	CampaignID       uuid.UUID  `gorm:"column:campaign_id;type:uuid;not null;index" json:"campaign_id"`
	DefinitionKey    string     `gorm:"column:definition_key;not null" json:"definition_key"`
	QualityKey       string     `gorm:"column:quality_key;not null" json:"quality_key"`
	State            string     `gorm:"column:state;not null" json:"state"`
	EquippedSlot     *string    `gorm:"column:equipped_slot" json:"equipped_slot,omitempty"`
	Refinement       int        `gorm:"column:refinement;not null" json:"refinement"`
	Durability       int        `gorm:"column:durability;not null" json:"durability"`
	MaxDurability    int        `gorm:"column:max_durability;not null" json:"max_durability"`
	BoundCharacterID *uuid.UUID `gorm:"column:bound_character_id;type:uuid" json:"bound_character_id,omitempty"`
	Properties       JSONMap    `gorm:"column:properties;type:jsonb;not null" json:"properties"`
	Provenance       JSONMap    `gorm:"column:provenance;type:jsonb;not null" json:"provenance"`
	CreatedAt        time.Time  `gorm:"column:created_at" json:"created_at"`
	UpdatedAt        time.Time  `gorm:"column:updated_at" json:"updated_at"`
}

func (CampaignItemInstance) TableName() string { return "campaign_item_instances" }

type CampaignKnownRecipe struct {
	CampaignID        uuid.UUID `gorm:"column:campaign_id;primaryKey;type:uuid" json:"campaign_id"`
	RecipeKey         string    `gorm:"column:recipe_key;primaryKey" json:"recipe_key"`
	LearnedAtGameTime int64     `gorm:"column:learned_at_game_time;not null" json:"learned_at_game_time"`
	SourceSnapshot    JSONMap   `gorm:"column:source_snapshot;type:jsonb;not null" json:"source_snapshot"`
	CreatedAt         time.Time `gorm:"column:created_at" json:"created_at"`
}

func (CampaignKnownRecipe) TableName() string { return "campaign_known_recipes" }

type CampaignMarketState struct {
	CampaignID          uuid.UUID `gorm:"column:campaign_id;primaryKey;type:uuid" json:"campaign_id"`
	MarketKey           string    `gorm:"column:market_key;primaryKey" json:"market_key"`
	LocationKey         string    `gorm:"column:location_key;not null" json:"location_key"`
	LastRestockGameTime int64     `gorm:"column:last_restock_game_time;not null" json:"last_restock_game_time"`
	PriceModifierBPS    int       `gorm:"column:price_modifier_bps;not null" json:"price_modifier_bps"`
	UpdatedAt           time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (CampaignMarketState) TableName() string { return "campaign_market_states" }

type CampaignMarketStock struct {
	CampaignID uuid.UUID `gorm:"column:campaign_id;primaryKey;type:uuid" json:"campaign_id"`
	MarketKey  string    `gorm:"column:market_key;primaryKey" json:"market_key"`
	ItemKind   string    `gorm:"column:item_kind;primaryKey" json:"item_kind"`
	ItemKey    string    `gorm:"column:item_key;primaryKey" json:"item_key"`
	QualityKey string    `gorm:"column:quality_key;primaryKey" json:"quality_key"`
	Quantity   int64     `gorm:"column:quantity;not null" json:"quantity"`
	UpdatedAt  time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (CampaignMarketStock) TableName() string { return "campaign_market_stocks" }

type CampaignLocationDiscovery struct {
	CampaignID           uuid.UUID `gorm:"column:campaign_id;primaryKey;type:uuid" json:"campaign_id"`
	LocationKey          string    `gorm:"column:location_key;primaryKey" json:"location_key"`
	DiscoveredAtGameTime int64     `gorm:"column:discovered_at_game_time;not null" json:"discovered_at_game_time"`
	Visited              bool      `gorm:"column:visited;not null" json:"visited"`
	Knowledge            JSONMap   `gorm:"column:knowledge;type:jsonb;not null" json:"knowledge"`
	UpdatedAt            time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (CampaignLocationDiscovery) TableName() string { return "campaign_location_discoveries" }

type CampaignNPC struct {
	ID                 uuid.UUID `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	CampaignID         uuid.UUID `gorm:"column:campaign_id;type:uuid;not null;index" json:"campaign_id"`
	ArchetypeKey       string    `gorm:"column:archetype_key;not null" json:"archetype_key"`
	InstanceKey        string    `gorm:"column:instance_key;not null" json:"instance_key"`
	StableName         string    `gorm:"column:stable_name;not null" json:"stable_name"`
	FactionKey         string    `gorm:"column:faction_key;not null" json:"faction_key"`
	RealmKey           string    `gorm:"column:realm_key;not null" json:"realm_key"`
	StageKey           string    `gorm:"column:stage_key;not null" json:"stage_key"`
	CurrentLocationKey string    `gorm:"column:current_location_key;not null" json:"current_location_key"`
	Status             string    `gorm:"column:status;not null" json:"status"`
	PublicProfile      JSONMap   `gorm:"column:public_profile;type:jsonb;not null" json:"public_profile"`
	PrivateState       JSONMap   `gorm:"column:private_state;type:jsonb;not null" json:"-"`
	Goals              JSONMap   `gorm:"column:goals;type:jsonb;not null" json:"-"`
	LastActiveAt       int64     `gorm:"column:last_active_at;not null" json:"last_active_at"`
	CreatedAt          time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt          time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (CampaignNPC) TableName() string { return "campaign_npcs" }

type CampaignRelationship struct {
	CampaignID        uuid.UUID `gorm:"column:campaign_id;primaryKey;type:uuid" json:"campaign_id"`
	NPCID             uuid.UUID `gorm:"column:npc_id;primaryKey;type:uuid" json:"npc_id"`
	Trust             int       `gorm:"column:trust;not null" json:"trust"`
	Fear              int       `gorm:"column:fear;not null" json:"fear"`
	Affinity          int       `gorm:"column:affinity;not null" json:"affinity"`
	Hostility         int       `gorm:"column:hostility;not null" json:"hostility"`
	Debt              int64     `gorm:"column:debt;not null" json:"debt"`
	PublicStance      string    `gorm:"column:public_stance;not null" json:"public_stance"`
	KnownFacts        JSONMap   `gorm:"column:known_facts;type:jsonb;not null" json:"known_facts"`
	LastInteractionAt int64     `gorm:"column:last_interaction_at;not null" json:"last_interaction_at"`
	UpdatedAt         time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (CampaignRelationship) TableName() string { return "campaign_relationships" }

type CampaignFactionStanding struct {
	CampaignID   uuid.UUID `gorm:"column:campaign_id;primaryKey;type:uuid" json:"campaign_id"`
	FactionKey   string    `gorm:"column:faction_key;primaryKey" json:"faction_key"`
	Reputation   int       `gorm:"column:reputation;not null" json:"reputation"`
	PositionKey  string    `gorm:"column:position_key;not null" json:"position_key"`
	Contribution int64     `gorm:"column:contribution;not null" json:"contribution"`
	WantedLevel  int       `gorm:"column:wanted_level;not null" json:"wanted_level"`
	Permissions  JSONMap   `gorm:"column:permissions;type:jsonb;not null" json:"permissions"`
	UpdatedAt    time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (CampaignFactionStanding) TableName() string { return "campaign_faction_standings" }

type CampaignScheduledEvent struct {
	ID                  uuid.UUID `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	CampaignID          uuid.UUID `gorm:"column:campaign_id;type:uuid;not null;index" json:"campaign_id"`
	EventKind           string    `gorm:"column:event_kind;not null" json:"event_kind"`
	TemplateKey         string    `gorm:"column:template_key;not null" json:"template_key"`
	DueGameTime         int64     `gorm:"column:due_game_time;not null" json:"due_game_time"`
	Priority            int       `gorm:"column:priority;not null" json:"priority"`
	Status              string    `gorm:"column:status;not null" json:"status"`
	Occurrence          int64     `gorm:"column:occurrence;not null" json:"occurrence"`
	Scope               JSONMap   `gorm:"column:scope;type:jsonb;not null" json:"-"`
	Payload             JSONMap   `gorm:"column:payload;type:jsonb;not null" json:"-"`
	ProcessedAtGameTime *int64    `gorm:"column:processed_at_game_time" json:"processed_at_game_time,omitempty"`
	CreatedAt           time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt           time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (CampaignScheduledEvent) TableName() string { return "campaign_scheduled_events" }

type CampaignRegionState struct {
	CampaignID        uuid.UUID `gorm:"column:campaign_id;primaryKey;type:uuid" json:"campaign_id"`
	RegionKey         string    `gorm:"column:region_key;primaryKey" json:"region_key"`
	ResourceLevel     int64     `gorm:"column:resource_level;not null" json:"resource_level"`
	DangerLevel       int64     `gorm:"column:danger_level;not null" json:"danger_level"`
	Stability         int64     `gorm:"column:stability;not null" json:"stability"`
	ConflictStage     int64     `gorm:"column:conflict_stage;not null" json:"conflict_stage"`
	FactionPower      JSONMap   `gorm:"column:faction_power;type:jsonb;not null" json:"faction_power"`
	LastEventKey      string    `gorm:"column:last_event_key;not null" json:"last_event_key"`
	LastEventVariant  string    `gorm:"column:last_event_variant;not null" json:"last_event_variant"`
	LastEventSummary  string    `gorm:"column:last_event_summary;not null" json:"last_event_summary"`
	LastEventGameTime *int64    `gorm:"column:last_event_game_time" json:"last_event_game_time,omitempty"`
	UpdatedAt         time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (CampaignRegionState) TableName() string { return "campaign_region_states" }

type CampaignQuest struct {
	CampaignID         uuid.UUID `gorm:"column:campaign_id;primaryKey;type:uuid" json:"campaign_id"`
	QuestKey           string    `gorm:"column:quest_key;primaryKey" json:"quest_key"`
	QuestType          string    `gorm:"column:quest_type;not null" json:"quest_type"`
	Status             string    `gorm:"column:status;not null" json:"status"`
	OfferedAtGameTime  int64     `gorm:"column:offered_at_game_time;not null" json:"offered_at_game_time"`
	AcceptedAtGameTime *int64    `gorm:"column:accepted_at_game_time" json:"accepted_at_game_time,omitempty"`
	DueGameTime        *int64    `gorm:"column:due_game_time" json:"due_game_time,omitempty"`
	ResolvedAtGameTime *int64    `gorm:"column:resolved_at_game_time" json:"resolved_at_game_time,omitempty"`
	Progress           JSONMap   `gorm:"column:progress;type:jsonb;not null" json:"progress"`
	SourceSnapshot     JSONMap   `gorm:"column:source_snapshot;type:jsonb;not null" json:"source_snapshot"`
	UpdatedAt          time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (CampaignQuest) TableName() string { return "campaign_quests" }

// CampaignEncounter stores one authoritative encounter instance. The player
// health remains on CampaignCharacter; opponent values are snapshotted from
// the immutable encounter definition so later content changes cannot rewrite
// an encounter already in progress.
type CampaignEncounter struct {
	ID                   uuid.UUID `gorm:"column:id;primaryKey;type:uuid" json:"id"`
	CampaignID           uuid.UUID `gorm:"column:campaign_id;type:uuid;not null;index" json:"campaign_id"`
	EncounterKey         string    `gorm:"column:encounter_key;not null" json:"encounter_key"`
	LocationKey          string    `gorm:"column:location_key;not null" json:"location_key"`
	OpponentKey          string    `gorm:"column:opponent_key;not null" json:"opponent_key"`
	OpponentName         string    `gorm:"column:opponent_name;not null" json:"opponent_name"`
	OpponentRealmRank    int       `gorm:"column:opponent_realm_rank;not null" json:"opponent_realm_rank"`
	OpponentHealth       int64     `gorm:"column:opponent_health;not null" json:"opponent_health"`
	OpponentMaxHealth    int64     `gorm:"column:opponent_max_health;not null" json:"opponent_max_health"`
	OpponentAttack       int64     `gorm:"column:opponent_attack;not null" json:"opponent_attack"`
	OpponentDefense      int64     `gorm:"column:opponent_defense;not null" json:"opponent_defense"`
	PlayerDamageBase     int64     `gorm:"column:player_damage_base;not null" json:"player_damage_base"`
	PlayerDamageScaleBPS int64     `gorm:"column:player_damage_scale_bps;not null" json:"player_damage_scale_bps"`
	Round                int       `gorm:"column:round;not null" json:"round"`
	MaxRounds            int       `gorm:"column:max_rounds;not null" json:"max_rounds"`
	Status               string    `gorm:"column:status;not null" json:"status"`
	LastMove             string    `gorm:"column:last_move;not null" json:"last_move"`
	StartedAtGameTime    int64     `gorm:"column:started_at_game_time;not null" json:"started_at_game_time"`
	ResolvedAtGameTime   *int64    `gorm:"column:resolved_at_game_time" json:"resolved_at_game_time,omitempty"`
	CreatedAt            time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt            time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (CampaignEncounter) TableName() string { return "campaign_encounters" }

type CampaignDomainState struct {
	Character      *CampaignCharacter
	Techniques     []CampaignTechnique
	ItemStacks     []CampaignItemStack
	ItemInstances  []CampaignItemInstance
	KnownRecipes   []CampaignKnownRecipe
	Markets        []CampaignMarketState
	MarketStocks   []CampaignMarketStock
	Discoveries    []CampaignLocationDiscovery
	NPCs           []CampaignNPC
	Relationships  []CampaignRelationship
	Factions       []CampaignFactionStanding
	RegionStates   []CampaignRegionState
	Quests         []CampaignQuest
	Encounters     []CampaignEncounter
	OutcomeRewards []CampaignOutcomeReward
	Definitions    map[string]GameplayDefinition
}

type CampaignOutcomeReward struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Unit     string `json:"unit"`
	Icon     string `json:"icon"`
	Quantity int64  `json:"quantity"`
	Balance  int64  `json:"balance"`
}

func definitionMapKey(kind, key string) string { return kind + ":" + key }

func (s CampaignDomainState) Definition(kind, key string) (GameplayDefinition, bool) {
	row, ok := s.Definitions[definitionMapKey(kind, key)]
	return row, ok
}

// CampaignActionStore is scoped to CommitAction's transaction. It is never
// exposed through HTTP and every method requires the locked Campaign id.
type CampaignActionStore struct {
	ctx                context.Context
	tx                 *gorm.DB
	campaign           Campaign
	definitionProvider GameplayDefinitionProvider
	definitions        map[string]GameplayDefinition
	definitionsLoaded  bool
	definitionsErr     error
}

func newCampaignActionStore(ctx context.Context, tx *gorm.DB, campaign Campaign, provider GameplayDefinitionProvider) *CampaignActionStore {
	return &CampaignActionStore{ctx: ctx, tx: tx, campaign: campaign, definitionProvider: provider}
}

// DomainState rebuilds the read model inside CommitAction's transaction. The
// Campaign row has already been locked by the committer when this is called.
func (s *CampaignActionStore) DomainState(ctx context.Context, campaign Campaign) (*CampaignDomainState, error) {
	return loadCampaignDomainState(ctx, s.tx, campaign, s.definitionProvider)
}

func (s *CampaignActionStore) Definition(versionID uuid.UUID, kind, key string) (*GameplayDefinition, error) {
	if s.definitionProvider != nil {
		if versionID != s.campaign.ChallengeVersionID {
			return nil, ErrNotFound
		}
		if !s.definitionsLoaded {
			s.definitions, s.definitionsErr = s.definitionProvider.DefinitionsForVersion(s.ctx, versionID, s.campaign.ChallengeHash)
			s.definitionsLoaded = true
		}
		if s.definitionsErr != nil {
			return nil, s.definitionsErr
		}
		row, ok := s.definitions[definitionMapKey(kind, key)]
		if !ok {
			return nil, ErrNotFound
		}
		return &row, nil
	}
	var row GameplayDefinition
	err := s.tx.First(&row, "challenge_version_id = ? AND kind = ? AND key = ?", versionID, kind, key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &row, err
}

func (s *CampaignActionStore) Character(campaignID uuid.UUID) (*CampaignCharacter, error) {
	var row CampaignCharacter
	err := s.tx.First(&row, "campaign_id = ?", campaignID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &row, err
}

func (s *CampaignActionStore) SaveCharacter(row *CampaignCharacter) error {
	return s.tx.Save(row).Error
}

func (s *CampaignActionStore) Technique(campaignID uuid.UUID, key string) (*CampaignTechnique, error) {
	var row CampaignTechnique
	err := s.tx.First(&row, "campaign_id = ? AND technique_key = ?", campaignID, key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &row, err
}

func (s *CampaignActionStore) CreateTechnique(row *CampaignTechnique) error {
	return s.tx.Create(row).Error
}
func (s *CampaignActionStore) SaveTechnique(row *CampaignTechnique) error {
	return s.tx.Save(row).Error
}

func (s *CampaignActionStore) Techniques(campaignID uuid.UUID) ([]CampaignTechnique, error) {
	var rows []CampaignTechnique
	err := s.tx.Where("campaign_id = ?", campaignID).Order("created_at ASC").Find(&rows).Error
	return rows, err
}

func (s *CampaignActionStore) ClearTechniqueSlot(campaignID uuid.UUID, slot string, except uuid.UUID) error {
	query := s.tx.Model(&CampaignTechnique{}).Where("campaign_id = ? AND equipped_slot = ?", campaignID, slot)
	if except != uuid.Nil {
		query = query.Where("id <> ?", except)
	}
	return query.Update("equipped_slot", nil).Error
}

func (s *CampaignActionStore) ItemStack(campaignID uuid.UUID, itemKey, qualityKey string) (*CampaignItemStack, error) {
	var row CampaignItemStack
	err := s.tx.First(&row, "campaign_id = ? AND item_key = ? AND quality_key = ?", campaignID, itemKey, qualityKey).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &row, err
}

func (s *CampaignActionStore) SaveItemStack(row *CampaignItemStack) error {
	if row.Quantity < 0 {
		return fmt.Errorf("物品数量不能为负数")
	}
	if row.Quantity == 0 {
		return s.tx.Delete(&CampaignItemStack{}, "campaign_id = ? AND item_key = ? AND quality_key = ?", row.CampaignID, row.ItemKey, row.QualityKey).Error
	}
	return s.tx.Save(row).Error
}

func (s *CampaignActionStore) ItemInstance(campaignID, id uuid.UUID) (*CampaignItemInstance, error) {
	var row CampaignItemInstance
	err := s.tx.First(&row, "campaign_id = ? AND id = ?", campaignID, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &row, err
}

func (s *CampaignActionStore) CreateItemInstance(row *CampaignItemInstance) error {
	return s.tx.Create(row).Error
}
func (s *CampaignActionStore) SaveItemInstance(row *CampaignItemInstance) error {
	return s.tx.Save(row).Error
}

func (s *CampaignActionStore) ItemInstances(campaignID uuid.UUID) ([]CampaignItemInstance, error) {
	var rows []CampaignItemInstance
	err := s.tx.Where("campaign_id = ? AND state <> ?", campaignID, "consumed").Order("created_at ASC").Find(&rows).Error
	return rows, err
}

func (s *CampaignActionStore) UnequipItemSlot(campaignID uuid.UUID, slot string, except uuid.UUID) error {
	query := s.tx.Model(&CampaignItemInstance{}).Where("campaign_id = ? AND equipped_slot = ?", campaignID, slot)
	if except != uuid.Nil {
		query = query.Where("id <> ?", except)
	}
	return query.Updates(map[string]any{"state": "stored", "equipped_slot": nil, "updated_at": time.Now()}).Error
}

func (s *CampaignActionStore) KnownRecipe(campaignID uuid.UUID, recipeKey string) (*CampaignKnownRecipe, error) {
	var row CampaignKnownRecipe
	err := s.tx.First(&row, "campaign_id = ? AND recipe_key = ?", campaignID, recipeKey).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &row, err
}

func (s *CampaignActionStore) CreateKnownRecipe(row *CampaignKnownRecipe) error {
	return s.tx.Create(row).Error
}

func (s *CampaignActionStore) MarketState(campaignID uuid.UUID, marketKey string) (*CampaignMarketState, error) {
	var row CampaignMarketState
	err := s.tx.First(&row, "campaign_id = ? AND market_key = ?", campaignID, marketKey).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &row, err
}

func (s *CampaignActionStore) SaveMarketState(row *CampaignMarketState) error {
	if row.PriceModifierBPS < 1000 || row.PriceModifierBPS > 30000 || row.LastRestockGameTime < 0 {
		return fmt.Errorf("市场状态越界")
	}
	return s.tx.Save(row).Error
}

func (s *CampaignActionStore) MarketStock(campaignID uuid.UUID, marketKey, itemKind, itemKey, qualityKey string) (*CampaignMarketStock, error) {
	var row CampaignMarketStock
	err := s.tx.First(&row,
		"campaign_id = ? AND market_key = ? AND item_kind = ? AND item_key = ? AND quality_key = ?",
		campaignID, marketKey, itemKind, itemKey, qualityKey,
	).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &row, err
}

func (s *CampaignActionStore) SaveMarketStock(row *CampaignMarketStock) error {
	if row.Quantity < 0 {
		return fmt.Errorf("市场库存不能为负数")
	}
	return s.tx.Save(row).Error
}

func (s *CampaignActionStore) DiscoverLocation(row *CampaignLocationDiscovery) error {
	// 已有记录只更新 visited（只允许 false→true）并合并 knowledge：
	// 旅行到已发现地点时调用方传空 knowledge，不能清空之前调查等写入的 knowledge。
	var existing CampaignLocationDiscovery
	if err := s.tx.Where("campaign_id = ? AND location_key = ?", row.CampaignID, row.LocationKey).First(&existing).Error; err == nil {
		merged := existing.Knowledge
		if merged == nil {
			merged = JSONMap{}
		}
		for key, value := range row.Knowledge {
			merged[key] = value
		}
		return s.tx.Model(&CampaignLocationDiscovery{}).
			Where("campaign_id = ? AND location_key = ?", row.CampaignID, row.LocationKey).
			Updates(map[string]any{
				"visited": existing.Visited || row.Visited, "knowledge": merged, "updated_at": row.UpdatedAt,
			}).Error
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return s.tx.Where("campaign_id = ? AND location_key = ?", row.CampaignID, row.LocationKey).
		Assign(map[string]any{"visited": row.Visited, "knowledge": row.Knowledge, "updated_at": row.UpdatedAt}).
		FirstOrCreate(row).Error
}

func (s *CampaignActionStore) IsLocationDiscovered(campaignID uuid.UUID, key string) (bool, error) {
	var count int64
	err := s.tx.Model(&CampaignLocationDiscovery{}).Where("campaign_id = ? AND location_key = ?", campaignID, key).Count(&count).Error
	return count == 1, err
}

func (s *CampaignActionStore) NPC(campaignID uuid.UUID, instanceKey string) (*CampaignNPC, error) {
	var row CampaignNPC
	err := s.tx.First(&row, "campaign_id = ? AND instance_key = ?", campaignID, instanceKey).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &row, err
}

func (s *CampaignActionStore) CountNPCsByArchetype(campaignID uuid.UUID, archetypeKey string) (int64, error) {
	var count int64
	err := s.tx.Model(&CampaignNPC{}).
		Where("campaign_id = ? AND archetype_key = ?", campaignID, archetypeKey).
		Count(&count).Error
	return count, err
}

func (s *CampaignActionStore) CreateNPC(row *CampaignNPC) error {
	return s.tx.Create(row).Error
}

func (s *CampaignActionStore) SaveNPC(row *CampaignNPC) error {
	return s.tx.Save(row).Error
}

func (s *CampaignActionStore) Relationship(campaignID, npcID uuid.UUID) (*CampaignRelationship, error) {
	var row CampaignRelationship
	err := s.tx.First(&row, "campaign_id = ? AND npc_id = ?", campaignID, npcID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &row, err
}

func (s *CampaignActionStore) SaveRelationship(row *CampaignRelationship) error {
	return s.tx.Save(row).Error
}

func (s *CampaignActionStore) FactionStanding(campaignID uuid.UUID, factionKey string) (*CampaignFactionStanding, error) {
	var row CampaignFactionStanding
	err := s.tx.First(&row, "campaign_id = ? AND faction_key = ?", campaignID, factionKey).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &row, err
}

func (s *CampaignActionStore) SaveFactionStanding(row *CampaignFactionStanding) error {
	return s.tx.Save(row).Error
}

func (s *CampaignActionStore) DueScheduledEvents(campaignID uuid.UUID, gameTime int64, limit int) ([]CampaignScheduledEvent, error) {
	if limit < 1 {
		return []CampaignScheduledEvent{}, nil
	}
	var rows []CampaignScheduledEvent
	err := s.tx.Where("campaign_id = ? AND status = ? AND due_game_time <= ?", campaignID, "pending", gameTime).
		Order("due_game_time ASC, priority DESC, event_kind ASC, template_key ASC, occurrence ASC, id ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (s *CampaignActionStore) CreateScheduledEvent(row *CampaignScheduledEvent) error {
	return s.tx.Create(row).Error
}

func (s *CampaignActionStore) SaveScheduledEvent(row *CampaignScheduledEvent) error {
	return s.tx.Save(row).Error
}

func (s *CampaignActionStore) CancelScheduledEvents(campaignID uuid.UUID, eventKind, templateKey string) error {
	return s.tx.Model(&CampaignScheduledEvent{}).
		Where("campaign_id = ? AND event_kind = ? AND template_key = ? AND status = ?", campaignID, eventKind, templateKey, "pending").
		Updates(map[string]any{"status": "cancelled", "updated_at": time.Now()}).Error
}

func (s *CampaignActionStore) RegionState(campaignID uuid.UUID, regionKey string) (*CampaignRegionState, error) {
	var row CampaignRegionState
	err := s.tx.First(&row, "campaign_id = ? AND region_key = ?", campaignID, regionKey).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &row, err
}

func (s *CampaignActionStore) SaveRegionState(row *CampaignRegionState) error {
	if row.ResourceLevel < 0 || row.ResourceLevel > 10000 || row.DangerLevel < 0 || row.DangerLevel > 10000 ||
		row.Stability < 0 || row.Stability > 10000 || row.ConflictStage < 0 || row.ConflictStage > 100 {
		return fmt.Errorf("区域聚合状态越界")
	}
	return s.tx.Save(row).Error
}

func (s *CampaignActionStore) Quest(campaignID uuid.UUID, questKey string) (*CampaignQuest, error) {
	var row CampaignQuest
	err := s.tx.First(&row, "campaign_id = ? AND quest_key = ?", campaignID, questKey).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &row, err
}

func (s *CampaignActionStore) SaveQuest(row *CampaignQuest) error {
	return s.tx.Save(row).Error
}

func (s *CampaignActionStore) Encounter(campaignID, id uuid.UUID) (*CampaignEncounter, error) {
	var row CampaignEncounter
	err := s.tx.First(&row, "campaign_id = ? AND id = ?", campaignID, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &row, err
}

func (s *CampaignActionStore) ActiveEncounter(campaignID uuid.UUID) (*CampaignEncounter, error) {
	var row CampaignEncounter
	err := s.tx.First(&row, "campaign_id = ? AND status = ?", campaignID, "active").Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &row, err
}

func (s *CampaignActionStore) CreateEncounter(row *CampaignEncounter) error {
	return s.tx.Create(row).Error
}

func (s *CampaignActionStore) SaveEncounter(row *CampaignEncounter) error {
	if row.OpponentHealth < 0 || row.OpponentHealth > row.OpponentMaxHealth ||
		row.Round < 1 || row.Round > row.MaxRounds {
		return fmt.Errorf("遭遇状态越界")
	}
	return s.tx.Save(row).Error
}

func (s *CampaignActionStore) EncounterWinCount(campaignID uuid.UUID, encounterKey string) (int64, error) {
	var count int64
	err := s.tx.Model(&CampaignEncounter{}).
		Where("campaign_id = ? AND encounter_key = ? AND status = ?", campaignID, encounterKey, "won").
		Count(&count).Error
	return count, err
}

func (s *CampaignActionStore) GrantCampaignOutcomeRewards(ctx context.Context, campaign Campaign, outcomeKey string) ([]CampaignOutcomeReward, error) {
	// client 路由：任务与 Campaign 照常完成，不发放账号积分。
	// 路由在事务内直接取执行中的 Campaign 行，不在调用方判断，保证所有入口一致。
	if campaign.LLMRoute == LLMRouteClient {
		return []CampaignOutcomeReward{}, nil
	}
	var outcome ChallengeOutcomeRule
	if err := s.tx.WithContext(ctx).
		First(&outcome, "challenge_version_id = ? AND outcome_key = ? AND enabled = ?", campaign.ChallengeVersionID, outcomeKey, true).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("Campaign 结局不存在: %s", outcomeKey)
		}
		return nil, err
	}
	if outcome.RewardPoints < 0 || outcome.RewardPoints > MaxRewardQuantity {
		return nil, ErrRewardQuantity
	}
	if outcome.RewardPoints == 0 {
		return []CampaignOutcomeReward{}, nil
	}
	_, err := NewRewardRepository(s.tx).GrantInCurrentTx(ctx, RewardMutation{PlayerID: campaign.PlayerID, RewardKey: PointsKey, Quantity: outcome.RewardPoints, SourceType: "campaign_outcome", SourceID: campaign.ID.String(), IdempotencyKey: "campaign:" + campaign.ID.String() + ":points", Metadata: JSONMap{"outcome_key": outcomeKey, "challenge_version_id": campaign.ChallengeVersionID}})
	if err != nil {
		return nil, err
	}
	return loadCampaignOutcomeRewards(ctx, s.tx, campaign)
}
func loadCampaignOutcomeRewards(ctx context.Context, db *gorm.DB, campaign Campaign) ([]CampaignOutcomeReward, error) {
	rows := []RewardLedgerEntry{}
	if err := db.WithContext(ctx).Where("player_id = ? AND source_type = 'campaign_outcome' AND source_id = ? AND operation = 'grant'", campaign.PlayerID, campaign.ID.String()).Find(&rows).Error; err != nil {
		return nil, err
	}
	rewards := []CampaignOutcomeReward{}
	for _, row := range rows {
		asset := RewardAssetFor(row.RewardKey)
		rewards = append(rewards, CampaignOutcomeReward{Key: asset.Key, Name: asset.Name, Unit: asset.Unit, Icon: asset.Icon, Quantity: row.QuantityDelta, Balance: row.BalanceAfter})
	}
	return rewards, nil
}

func loadCampaignDomainState(ctx context.Context, db *gorm.DB, campaign Campaign, providers ...GameplayDefinitionProvider) (*CampaignDomainState, error) {
	state := &CampaignDomainState{
		Definitions:    map[string]GameplayDefinition{},
		OutcomeRewards: []CampaignOutcomeReward{},
	}
	var character CampaignCharacter
	if err := db.WithContext(ctx).First(&character, "campaign_id = ?", campaign.ID).Error; err == nil {
		state.Character = &character
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	queries := []struct {
		out any
		q   *gorm.DB
	}{
		{&state.Techniques, db.WithContext(ctx).Where("campaign_id = ?", campaign.ID).Order("created_at ASC")},
		{&state.ItemStacks, db.WithContext(ctx).Where("campaign_id = ?", campaign.ID).Order("item_key ASC, quality_key ASC")},
		{&state.ItemInstances, db.WithContext(ctx).Where("campaign_id = ? AND state <> ?", campaign.ID, "consumed").Order("created_at ASC")},
		{&state.KnownRecipes, db.WithContext(ctx).Where("campaign_id = ?", campaign.ID).Order("created_at ASC")},
		{&state.Markets, db.WithContext(ctx).Where("campaign_id = ?", campaign.ID).Order("market_key ASC")},
		{&state.MarketStocks, db.WithContext(ctx).Where("campaign_id = ?", campaign.ID).Order("market_key ASC, item_kind ASC, item_key ASC, quality_key ASC")},
		{&state.Discoveries, db.WithContext(ctx).Where("campaign_id = ?", campaign.ID).Order("discovered_at_game_time ASC, location_key ASC")},
		{&state.NPCs, db.WithContext(ctx).Where("campaign_id = ?", campaign.ID).Order("stable_name ASC")},
		{&state.Relationships, db.WithContext(ctx).Where("campaign_id = ?", campaign.ID).Order("last_interaction_at DESC")},
		{&state.Factions, db.WithContext(ctx).Where("campaign_id = ?", campaign.ID).Order("faction_key ASC")},
		{&state.RegionStates, db.WithContext(ctx).Where("campaign_id = ?", campaign.ID).Order("region_key ASC")},
		{&state.Quests, db.WithContext(ctx).Where("campaign_id = ?", campaign.ID).Order("offered_at_game_time ASC, quest_key ASC")},
		{&state.Encounters, db.WithContext(ctx).Where("campaign_id = ?", campaign.ID).Order("created_at DESC")},
	}
	for _, query := range queries {
		if err := query.q.Find(query.out).Error; err != nil {
			return nil, err
		}
	}
	var provider GameplayDefinitionProvider
	if len(providers) > 0 {
		provider = providers[0]
	}
	if provider != nil {
		definitions, err := provider.DefinitionsForVersion(ctx, campaign.ChallengeVersionID, campaign.ChallengeHash)
		if err != nil {
			return nil, err
		}
		for key, definition := range definitions {
			state.Definitions[key] = definition
		}
	} else {
		var definitions []GameplayDefinition
		if err := db.WithContext(ctx).Where("challenge_version_id = ?", campaign.ChallengeVersionID).
			Order("kind ASC, sort_order ASC, key ASC").Find(&definitions).Error; err != nil {
			return nil, err
		}
		for _, definition := range definitions {
			state.Definitions[definitionMapKey(definition.Kind, definition.Key)] = definition
		}
	}
	if campaign.Status == CampaignStatusCompleted {
		var err error
		state.OutcomeRewards, err = loadCampaignOutcomeRewards(ctx, db, campaign)
		if err != nil {
			return nil, err
		}
	}
	return state, nil
}
