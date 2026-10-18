package campaignactions

import (
	"context"
	"errors"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldactions"
	"sort"
	"strconv"
	"testing"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/storage"
)

type actionStoreStub struct {
	definitions   map[string]*storage.GameplayDefinition
	character     *storage.CampaignCharacter
	techniques    map[string]*storage.CampaignTechnique
	stacks        map[string]*storage.CampaignItemStack
	instances     map[uuid.UUID]*storage.CampaignItemInstance
	recipes       map[string]*storage.CampaignKnownRecipe
	markets       map[string]*storage.CampaignMarketState
	marketStocks  map[string]*storage.CampaignMarketStock
	discovered    map[string]bool
	npcs          map[string]*storage.CampaignNPC
	relationships map[uuid.UUID]*storage.CampaignRelationship
	factions      map[string]*storage.CampaignFactionStanding
	scheduled     map[uuid.UUID]*storage.CampaignScheduledEvent
	regions       map[string]*storage.CampaignRegionState
	quests        map[string]*storage.CampaignQuest
	encounters    map[uuid.UUID]*storage.CampaignEncounter
	stageRewards  map[string]*storage.CampaignStageReward
	stageGranted  int64
}

func (s *actionStoreStub) Definition(_ uuid.UUID, kind, key string) (*storage.GameplayDefinition, error) {
	row := s.definitions[kind+":"+key]
	if row == nil {
		return nil, storage.ErrNotFound
	}
	return row, nil
}

func (s *actionStoreStub) Character(uuid.UUID) (*storage.CampaignCharacter, error) {
	if s.character == nil {
		return nil, storage.ErrNotFound
	}
	return s.character, nil
}

func (s *actionStoreStub) SaveCharacter(row *storage.CampaignCharacter) error {
	s.character = row
	return nil
}
func (s *actionStoreStub) Technique(_ uuid.UUID, key string) (*storage.CampaignTechnique, error) {
	row := s.techniques[key]
	if row == nil {
		return nil, storage.ErrNotFound
	}
	return row, nil
}
func (s *actionStoreStub) CreateTechnique(row *storage.CampaignTechnique) error {
	s.techniques[row.TechniqueKey] = row
	return nil
}
func (s *actionStoreStub) SaveTechnique(row *storage.CampaignTechnique) error {
	s.techniques[row.TechniqueKey] = row
	return nil
}
func (s *actionStoreStub) Techniques(uuid.UUID) ([]storage.CampaignTechnique, error) {
	rows := make([]storage.CampaignTechnique, 0, len(s.techniques))
	for _, row := range s.techniques {
		rows = append(rows, *row)
	}
	return rows, nil
}
func (s *actionStoreStub) ClearTechniqueSlot(_ uuid.UUID, slot string, except uuid.UUID) error {
	for _, row := range s.techniques {
		if row.ID != except && row.EquippedSlot != nil && *row.EquippedSlot == slot {
			row.EquippedSlot = nil
		}
	}
	return nil
}
func stackKey(item, quality string) string { return item + ":" + quality }
func (s *actionStoreStub) ItemStack(_ uuid.UUID, item, quality string) (*storage.CampaignItemStack, error) {
	row := s.stacks[stackKey(item, quality)]
	if row == nil {
		return nil, storage.ErrNotFound
	}
	return row, nil
}
func (s *actionStoreStub) SaveItemStack(row *storage.CampaignItemStack) error {
	if row.Quantity < 0 {
		return errors.New("negative stack")
	}
	if row.Quantity == 0 {
		delete(s.stacks, stackKey(row.ItemKey, row.QualityKey))
		return nil
	}
	s.stacks[stackKey(row.ItemKey, row.QualityKey)] = row
	return nil
}
func (s *actionStoreStub) ItemInstance(_ uuid.UUID, id uuid.UUID) (*storage.CampaignItemInstance, error) {
	row := s.instances[id]
	if row == nil {
		return nil, storage.ErrNotFound
	}
	return row, nil
}
func (s *actionStoreStub) CreateItemInstance(row *storage.CampaignItemInstance) error {
	s.instances[row.ID] = row
	return nil
}
func (s *actionStoreStub) SaveItemInstance(row *storage.CampaignItemInstance) error {
	s.instances[row.ID] = row
	return nil
}
func (s *actionStoreStub) ItemInstances(uuid.UUID) ([]storage.CampaignItemInstance, error) {
	rows := make([]storage.CampaignItemInstance, 0, len(s.instances))
	for _, row := range s.instances {
		rows = append(rows, *row)
	}
	return rows, nil
}
func (s *actionStoreStub) UnequipItemSlot(_ uuid.UUID, slot string, except uuid.UUID) error {
	for _, row := range s.instances {
		if row.ID != except && row.EquippedSlot != nil && *row.EquippedSlot == slot {
			row.State, row.EquippedSlot = "stored", nil
		}
	}
	return nil
}
func (s *actionStoreStub) KnownRecipe(_ uuid.UUID, key string) (*storage.CampaignKnownRecipe, error) {
	row := s.recipes[key]
	if row == nil {
		return nil, storage.ErrNotFound
	}
	return row, nil
}
func (s *actionStoreStub) CreateKnownRecipe(row *storage.CampaignKnownRecipe) error {
	if s.recipes[row.RecipeKey] != nil {
		return errors.New("duplicate recipe")
	}
	s.recipes[row.RecipeKey] = row
	return nil
}
func (s *actionStoreStub) MarketState(_ uuid.UUID, key string) (*storage.CampaignMarketState, error) {
	row := s.markets[key]
	if row == nil {
		return nil, storage.ErrNotFound
	}
	return row, nil
}
func (s *actionStoreStub) SaveMarketState(row *storage.CampaignMarketState) error {
	s.markets[row.MarketKey] = row
	return nil
}
func (s *actionStoreStub) MarketStock(_ uuid.UUID, market, kind, item, quality string) (*storage.CampaignMarketStock, error) {
	row := s.marketStocks[marketStockTestKey(market, kind, item, quality)]
	if row == nil {
		return nil, storage.ErrNotFound
	}
	return row, nil
}
func (s *actionStoreStub) SaveMarketStock(row *storage.CampaignMarketStock) error {
	s.marketStocks[marketStockTestKey(row.MarketKey, row.ItemKind, row.ItemKey, row.QualityKey)] = row
	return nil
}
func (s *actionStoreStub) DiscoverLocation(row *storage.CampaignLocationDiscovery) error {
	s.discovered[row.LocationKey] = true
	return nil
}
func (s *actionStoreStub) IsLocationDiscovered(_ uuid.UUID, key string) (bool, error) {
	return s.discovered[key], nil
}
func (s *actionStoreStub) NPC(_ uuid.UUID, key string) (*storage.CampaignNPC, error) {
	row := s.npcs[key]
	if row == nil {
		return nil, storage.ErrNotFound
	}
	return row, nil
}
func (s *actionStoreStub) CountNPCsByArchetype(_ uuid.UUID, key string) (int64, error) {
	var count int64
	for _, row := range s.npcs {
		if row.ArchetypeKey == key {
			count++
		}
	}
	return count, nil
}
func (s *actionStoreStub) CreateNPC(row *storage.CampaignNPC) error {
	if s.npcs[row.InstanceKey] != nil {
		return errors.New("duplicate NPC")
	}
	s.npcs[row.InstanceKey] = row
	return nil
}
func (s *actionStoreStub) SaveNPC(row *storage.CampaignNPC) error {
	s.npcs[row.InstanceKey] = row
	return nil
}
func (s *actionStoreStub) Relationship(_ uuid.UUID, id uuid.UUID) (*storage.CampaignRelationship, error) {
	row := s.relationships[id]
	if row == nil {
		return nil, storage.ErrNotFound
	}
	return row, nil
}
func (s *actionStoreStub) SaveRelationship(row *storage.CampaignRelationship) error {
	s.relationships[row.NPCID] = row
	return nil
}
func (s *actionStoreStub) FactionStanding(_ uuid.UUID, key string) (*storage.CampaignFactionStanding, error) {
	row := s.factions[key]
	if row == nil {
		return nil, storage.ErrNotFound
	}
	return row, nil
}
func (s *actionStoreStub) SaveFactionStanding(row *storage.CampaignFactionStanding) error {
	s.factions[row.FactionKey] = row
	return nil
}
func (s *actionStoreStub) DueScheduledEvents(_ uuid.UUID, gameTime int64, limit int) ([]storage.CampaignScheduledEvent, error) {
	rows := make([]storage.CampaignScheduledEvent, 0)
	for _, row := range s.scheduled {
		if row.Status == "pending" && row.DueGameTime <= gameTime {
			rows = append(rows, *row)
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].DueGameTime != rows[j].DueGameTime {
			return rows[i].DueGameTime < rows[j].DueGameTime
		}
		if rows[i].Priority != rows[j].Priority {
			return rows[i].Priority > rows[j].Priority
		}
		if rows[i].EventKind != rows[j].EventKind {
			return rows[i].EventKind < rows[j].EventKind
		}
		if rows[i].TemplateKey != rows[j].TemplateKey {
			return rows[i].TemplateKey < rows[j].TemplateKey
		}
		if rows[i].Occurrence != rows[j].Occurrence {
			return rows[i].Occurrence < rows[j].Occurrence
		}
		return rows[i].ID.String() < rows[j].ID.String()
	})
	if limit < len(rows) {
		rows = rows[:limit]
	}
	return rows, nil
}
func (s *actionStoreStub) CreateScheduledEvent(row *storage.CampaignScheduledEvent) error {
	for _, existing := range s.scheduled {
		if existing.EventKind == row.EventKind && existing.TemplateKey == row.TemplateKey && existing.Occurrence == row.Occurrence {
			return errors.New("duplicate scheduled event")
		}
	}
	s.scheduled[row.ID] = row
	return nil
}
func (s *actionStoreStub) SaveScheduledEvent(row *storage.CampaignScheduledEvent) error {
	s.scheduled[row.ID] = row
	return nil
}
func (s *actionStoreStub) CancelScheduledEvents(_ uuid.UUID, kind, key string) error {
	for _, row := range s.scheduled {
		if row.EventKind == kind && row.TemplateKey == key && row.Status == "pending" {
			row.Status = "cancelled"
		}
	}
	return nil
}
func (s *actionStoreStub) RegionState(_ uuid.UUID, key string) (*storage.CampaignRegionState, error) {
	row := s.regions[key]
	if row == nil {
		return nil, storage.ErrNotFound
	}
	return row, nil
}
func (s *actionStoreStub) SaveRegionState(row *storage.CampaignRegionState) error {
	s.regions[row.RegionKey] = row
	return nil
}
func (s *actionStoreStub) Quest(_ uuid.UUID, key string) (*storage.CampaignQuest, error) {
	row := s.quests[key]
	if row == nil {
		return nil, storage.ErrNotFound
	}
	return row, nil
}
func (s *actionStoreStub) SaveQuest(row *storage.CampaignQuest) error {
	s.quests[row.QuestKey] = row
	return nil
}

func (s *actionStoreStub) Encounter(_ uuid.UUID, id uuid.UUID) (*storage.CampaignEncounter, error) {
	row := s.encounters[id]
	if row == nil {
		return nil, storage.ErrNotFound
	}
	return row, nil
}
func (s *actionStoreStub) ActiveEncounter(_ uuid.UUID) (*storage.CampaignEncounter, error) {
	for _, row := range s.encounters {
		if row.Status == "active" {
			return row, nil
		}
	}
	return nil, storage.ErrNotFound
}
func (s *actionStoreStub) CreateEncounter(row *storage.CampaignEncounter) error {
	s.encounters[row.ID] = row
	return nil
}
func (s *actionStoreStub) SaveEncounter(row *storage.CampaignEncounter) error {
	s.encounters[row.ID] = row
	return nil
}
func (s *actionStoreStub) EncounterWinCount(_ uuid.UUID, encounterKey string) (int64, error) {
	var count int64
	for _, row := range s.encounters {
		if row.EncounterKey == encounterKey && row.Status == "won" {
			count++
		}
	}
	return count, nil
}
func (s *actionStoreStub) GrantCampaignOutcomeRewards(_ context.Context, _ storage.Campaign, _ string) ([]storage.CampaignOutcomeReward, error) {
	return []storage.CampaignOutcomeReward{{Key: storage.PointsKey, Name: "积分", Unit: "枚", Quantity: 1, Balance: 1}}, nil
}

func (s *actionStoreStub) GrantCampaignStageReward(_ context.Context, c storage.Campaign, key, name string, points int64) (*storage.CampaignStageReward, bool, error) {
	if s.stageRewards == nil {
		s.stageRewards = map[string]*storage.CampaignStageReward{}
	}
	if prior := s.stageRewards[key]; prior != nil {
		return prior, false, nil
	}
	row := &storage.CampaignStageReward{CampaignID: c.ID, StageKey: key, Name: name, ConfiguredPoints: points, GrantedPoints: points, GameTime: c.GameTime}
	if c.LLMRoute == storage.LLMRouteClient {
		row.GrantedPoints = 0
		row.DisabledReason = storage.RewardsDisabledReasonClientRoute
	} else if points == 0 {
		row.DisabledReason = "zero_points"
	}
	s.stageRewards[key] = row
	s.stageGranted += row.GrantedPoints
	return row, true, nil
}

func newActionStore() *actionStoreStub {
	return &actionStoreStub{
		definitions: map[string]*storage.GameplayDefinition{}, techniques: map[string]*storage.CampaignTechnique{},
		stacks: map[string]*storage.CampaignItemStack{}, instances: map[uuid.UUID]*storage.CampaignItemInstance{},
		recipes: map[string]*storage.CampaignKnownRecipe{}, markets: map[string]*storage.CampaignMarketState{},
		marketStocks: map[string]*storage.CampaignMarketStock{},
		discovered:   map[string]bool{}, npcs: map[string]*storage.CampaignNPC{},
		relationships: map[uuid.UUID]*storage.CampaignRelationship{}, factions: map[string]*storage.CampaignFactionStanding{},
		scheduled: map[uuid.UUID]*storage.CampaignScheduledEvent{}, regions: map[string]*storage.CampaignRegionState{},
		quests:     map[string]*storage.CampaignQuest{},
		encounters: map[uuid.UUID]*storage.CampaignEncounter{},
	}
}

func TestRenameCharacterUpdatesAuthoritativeNameAndEmitsEvent(t *testing.T) {
	store := newActionStore()
	campaignID := uuid.New()
	store.character = &storage.CampaignCharacter{CampaignID: campaignID, Name: "林氏旁支", Health: 10}
	outcome, err := renameCharacter(context.Background(), campaign.ActionExecution{
		Campaign: storage.Campaign{ID: campaignID}, Store: store, ActorType: "model", ActionID: "rename-1",
	}, map[string]any{"name": "林万里"})
	if err != nil {
		t.Fatal(err)
	}
	if store.character.Name != "林万里" || len(outcome.Events) != 1 || outcome.Events[0].EventType != "character.renamed" {
		t.Fatalf("rename result = character=%+v outcome=%+v", store.character, outcome)
	}
	if outcome.Events[0].Payload["old_name"] != "林氏旁支" || outcome.Events[0].Payload["new_name"] != "林万里" {
		t.Fatalf("rename event payload = %#v", outcome.Events[0].Payload)
	}
}

func TestPracticeMutatesCharacterAndTechniqueFromDefinition(t *testing.T) {
	store := newActionStore()
	campaignID := uuid.New()
	slot := "primary"
	store.character = &storage.CampaignCharacter{
		CampaignID: campaignID, RealmKey: "mortal", CultivationRequired: 100,
		Mana: 20, MaxMana: 20, LifespanDays: 1000,
	}
	store.techniques["breathing"] = &storage.CampaignTechnique{
		ID: uuid.New(), CampaignID: campaignID, TechniqueKey: "breathing", LearnedLayer: 1, Status: "active", EquippedSlot: &slot,
	}
	store.definitions[storage.DefinitionTechnique+":breathing"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionTechnique, Key: "breathing",
		Content: storage.JSONMap{"max_layer": int64(12), "practice_minutes": int64(60), "mana_cost": int64(2), "cultivation_gain": int64(10), "mastery_gain": int64(25), "insight_gain": int64(1)},
	}
	store.definitions[storage.DefinitionRealm+":mortal"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionRealm, Key: "mortal", Content: storage.JSONMap{"stages": []any{"early", "late"}},
	}
	execution := campaign.ActionExecution{Campaign: storage.Campaign{ID: campaignID}, Store: store, ActorType: "player", ActionID: "turn-1"}
	outcome, err := practice(context.Background(), execution, map[string]any{"technique_key": "breathing", "sessions": float64(2)})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.GameTimeDelta != 120 || store.character.CultivationProgress != 20 || store.character.Mana != 16 || store.techniques["breathing"].Mastery != 50 {
		t.Fatalf("unexpected practice result: outcome=%+v character=%+v technique=%+v", outcome, store.character, store.techniques["breathing"])
	}
}

func TestPracticeAppliesLayerEffectsAndRecalculatesEffectiveStats(t *testing.T) {
	store := newActionStore()
	campaignID, versionID := uuid.New(), uuid.New()
	slot := "primary"
	store.character = &storage.CampaignCharacter{
		CampaignID: campaignID, RealmKey: "mortal", CultivationRequired: 1000,
		Mana: 10, BaseMaxHealth: 100, MaxHealth: 100,
		BaseMaxMana: 20, MaxMana: 20, BaseMaxSpiritualSense: 10, MaxSpiritualSense: 10,
		LifespanDays: 1000,
	}
	store.techniques["breathing"] = &storage.CampaignTechnique{
		ID: uuid.New(), CampaignID: campaignID, TechniqueKey: "breathing", LearnedLayer: 1,
		Mastery: 4950, Status: "active", EquippedSlot: &slot,
	}
	store.definitions[storage.DefinitionTechnique+":breathing"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionTechnique, Key: "breathing",
		Content: storage.JSONMap{
			"practice_minutes": int64(60), "mana_cost": int64(3), "cultivation_gain": int64(10),
			"mastery_gain": int64(100), "insight_gain": int64(1), "max_layer": int64(3),
			"layer_effects": []any{
				map[string]any{"layer": int64(1), "effects": map[string]any{
					"cultivation_gain": int64(2), "mana_cost_reduction": int64(1),
				}},
				map[string]any{"layer": int64(2), "effects": map[string]any{"max_mana": int64(10)}},
			},
		},
	}
	store.definitions[storage.DefinitionRealm+":mortal"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionRealm, Key: "mortal", Content: storage.JSONMap{"stages": []any{"early"}},
	}
	execution := campaign.ActionExecution{
		Campaign: storage.Campaign{ID: campaignID, ChallengeVersionID: versionID}, Store: store, ActorType: "player",
	}
	outcome, err := practice(context.Background(), execution, map[string]any{"technique_key": "breathing"})
	if err != nil {
		t.Fatal(err)
	}
	if store.character.CultivationProgress != 12 || store.character.Mana != 8 {
		t.Fatalf("practice effects were not settled: %+v", store.character)
	}
	if store.techniques["breathing"].LearnedLayer != 2 || store.character.MaxMana != 30 {
		t.Fatalf("layer-derived stats were not recalculated: character=%+v technique=%+v", store.character, store.techniques["breathing"])
	}
	if effects := outcome.Result["active_effects"].(storage.JSONMap); effects["max_mana"] != int64(10) {
		t.Fatalf("active effects missing from result: %#v", effects)
	}
}

func TestPracticeAdvancesTechniqueLayerFromMastery(t *testing.T) {
	store := newActionStore()
	campaignID := uuid.New()
	slot := "primary"
	store.character = &storage.CampaignCharacter{
		CampaignID: campaignID, RealmKey: "mortal", CultivationRequired: 100,
		Mana: 10, LifespanDays: 1000,
	}
	store.techniques["breathing"] = &storage.CampaignTechnique{
		ID: uuid.New(), CampaignID: campaignID, TechniqueKey: "breathing",
		LearnedLayer: 1, Mastery: 4990, Status: "active", EquippedSlot: &slot,
	}
	store.definitions[storage.DefinitionTechnique+":breathing"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionTechnique, Key: "breathing",
		Content: storage.JSONMap{
			"practice_minutes": int64(10), "cultivation_gain": int64(1),
			"mastery_gain": int64(20), "insight_gain": int64(1),
			"max_layer": int64(3),
		},
	}
	store.definitions[storage.DefinitionRealm+":mortal"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionRealm, Key: "mortal", Content: storage.JSONMap{"stages": []any{"early"}},
	}
	outcome, err := practice(context.Background(), campaign.ActionExecution{
		Campaign: storage.Campaign{ID: campaignID}, Store: store, ActorType: "player",
	}, map[string]any{"technique_key": "breathing"})
	if err != nil {
		t.Fatal(err)
	}
	if store.techniques["breathing"].LearnedLayer != 2 || outcome.Result["learned_layer"] != 2 {
		t.Fatalf("technique layer did not advance: technique=%+v result=%+v", store.techniques["breathing"], outcome.Result)
	}
}

func TestGainItemRequiresModelAuthorityAndDefinitionBound(t *testing.T) {
	store := newActionStore()
	campaignID := uuid.New()
	store.character = &storage.CampaignCharacter{CampaignID: campaignID, LifespanDays: 1000}
	store.definitions[storage.DefinitionItem+":herb"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionItem, Key: "herb", Name: "灵草",
		Content: storage.JSONMap{"stackable": true, "max_grant_per_action": int64(5)},
	}
	arguments := map[string]any{"item_key": "herb", "quantity": float64(3)}
	player := campaign.ActionExecution{Campaign: storage.Campaign{ID: campaignID}, Store: store, ActorType: "player", ActionKey: ActionGainItem}
	if _, err := worldactions.GainItem(context.Background(), player, arguments); !errors.Is(err, campaign.ErrActionRejected) {
		t.Fatalf("player grant error = %v", err)
	}
	model := player
	model.ActorType = "model"
	if _, err := worldactions.GainItem(context.Background(), model, arguments); !errors.Is(err, campaign.ErrActionRejected) {
		t.Fatalf("non-whitelisted model grant error = %v", err)
	}
	store.definitions[storage.DefinitionItem+":herb"].Content["model_grantable"] = true
	outcome, err := worldactions.GainItem(context.Background(), model, arguments)
	if err != nil {
		t.Fatal(err)
	}
	if store.stacks["herb:common"].Quantity != 3 || outcome.Result["quantity_after"] != int64(3) {
		t.Fatalf("unexpected grant: %+v", outcome)
	}
	if _, err := worldactions.GainItem(context.Background(), model, map[string]any{"item_key": "herb", "quantity": float64(6)}); !errors.Is(err, campaign.ErrInvalidAction) {
		t.Fatalf("unbounded grant error = %v", err)
	}
	system := model
	system.ActorType = "system"
	delete(store.definitions[storage.DefinitionItem+":herb"].Content, "model_grantable")
	if _, err := worldactions.GainItem(context.Background(), system, map[string]any{"item_key": "herb", "quantity": float64(1)}); err != nil {
		t.Fatalf("system grant error = %v", err)
	}
}

func TestTravelRequiresDiscoveredAdjacentLocation(t *testing.T) {
	store := newActionStore()
	campaignID := uuid.New()
	store.character = &storage.CampaignCharacter{CampaignID: campaignID, LifespanDays: 1000}
	store.definitions[storage.DefinitionLocation+":village"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionLocation, Key: "village",
		Content: storage.JSONMap{"connections": []any{map[string]any{"location_key": "ferry", "travel_minutes": float64(45)}}},
	}
	store.definitions[storage.DefinitionLocation+":ferry"] = &storage.GameplayDefinition{Kind: storage.DefinitionLocation, Key: "ferry"}
	execution := campaign.ActionExecution{Campaign: storage.Campaign{ID: campaignID, CurrentLocationKey: "village"}, Store: store, ActorType: "player"}
	arguments := map[string]any{"destination_key": "ferry"}
	if _, err := travel(context.Background(), execution, arguments); !errors.Is(err, campaign.ErrActionRejected) {
		t.Fatalf("undiscovered travel error = %v", err)
	}
	store.discovered["ferry"] = true
	outcome, err := travel(context.Background(), execution, arguments)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.GameTimeDelta != 45 || outcome.CurrentLocationKey == nil || *outcome.CurrentLocationKey != "ferry" || len(outcome.Events) != 2 {
		t.Fatalf("unexpected travel outcome: %+v", outcome)
	}
}

func TestGatherUsesCurrentLocationResourceTableAndAdvancesTime(t *testing.T) {
	store := newActionStore()
	campaignID, versionID := uuid.New(), uuid.New()
	store.character = &storage.CampaignCharacter{CampaignID: campaignID, Health: 100, MaxHealth: 100, LifespanDays: 1000}
	store.definitions[storage.DefinitionLocation+":woods"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionLocation, Key: "woods", Name: "竹林", Content: storage.JSONMap{
			"node_type": "location", "parent_key": "county",
			"gathering": map[string]any{
				"minutes": int64(120),
				"entries": []any{map[string]any{
					"item_key": "herb", "min_quantity": int64(2), "max_quantity": int64(2), "weight": int64(1),
				}},
			},
		},
	}
	store.definitions[storage.DefinitionLocation+":county"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionLocation, Key: "county", Content: storage.JSONMap{"node_type": "region"},
	}
	store.definitions[storage.DefinitionItem+":herb"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionItem, Key: "herb", Name: "灵草", Content: storage.JSONMap{"stackable": true},
	}
	store.regions["county"] = &storage.CampaignRegionState{
		CampaignID: campaignID, RegionKey: "county", ResourceLevel: 5000, Stability: 5000, FactionPower: storage.JSONMap{},
	}
	execution := campaign.ActionExecution{
		Campaign: storage.Campaign{ID: campaignID, ChallengeVersionID: versionID, CurrentLocationKey: "woods", RandomSeed: 7},
		Store:    store, ActorType: "player", ActionID: "gather-1", ActionKey: ActionGather,
	}
	outcome, err := gather(context.Background(), execution, map[string]any{"item_key": "herb"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.GameTimeDelta != 120 || store.stacks["herb:common"].Quantity != 2 ||
		store.regions["county"].ResourceLevel != 4980 || len(outcome.Events) != 1 {
		t.Fatalf("unexpected gathering outcome: outcome=%#v stacks=%#v", outcome, store.stacks)
	}
	if _, err := gather(context.Background(), execution, map[string]any{"item_key": "ore"}); !errors.Is(err, campaign.ErrActionRejected) {
		t.Fatalf("undeclared resource error = %v", err)
	}
}

func TestFactionPermissionIsDerivedAndGatesTravel(t *testing.T) {
	store := newActionStore()
	campaignID, versionID := uuid.New(), uuid.New()
	store.character = &storage.CampaignCharacter{CampaignID: campaignID, LifespanDays: 1000}
	store.discovered["sect-gate"] = true
	store.definitions[storage.DefinitionLocation+":market"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionLocation, Key: "market",
		Content: storage.JSONMap{"connections": []any{map[string]any{
			"location_key": "sect-gate", "travel_minutes": float64(45),
		}}},
	}
	store.definitions[storage.DefinitionLocation+":sect-gate"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionLocation, Key: "sect-gate",
		Content: storage.JSONMap{"entry_requirements": []any{map[string]any{
			"faction_key": "sect", "permission_key": "outer_gate_access",
		}}},
	}
	store.definitions[storage.DefinitionFaction+":sect"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionFaction, Key: "sect",
		Content: storage.JSONMap{
			"permission_thresholds": map[string]any{"outer_gate_access": float64(100)},
			"position_thresholds":   map[string]any{"outer_disciple": float64(1000)},
		},
	}
	store.factions["sect"] = &storage.CampaignFactionStanding{
		CampaignID: campaignID, FactionKey: "sect", Reputation: 99,
		PositionKey: "outsider", Permissions: storage.JSONMap{},
	}
	campaignRow := storage.Campaign{
		ID: campaignID, ChallengeVersionID: versionID, CurrentLocationKey: "market",
	}
	player := campaign.ActionExecution{Campaign: campaignRow, Store: store, ActorType: "player", ActionKey: ActionTravel}
	travelArguments := map[string]any{"destination_key": "sect-gate"}
	if _, err := travel(context.Background(), player, travelArguments); !errors.Is(err, campaign.ErrActionRejected) {
		t.Fatalf("travel without permission error = %v", err)
	}

	model := campaign.ActionExecution{Campaign: campaignRow, Store: store, ActorType: "model", ActionKey: ActionAdjustFaction}
	if _, err := worldactions.AdjustFaction(context.Background(), model, map[string]any{
		"faction_key": "sect", "reputation_delta": int64(1),
	}); err != nil {
		t.Fatal(err)
	}
	if granted, _ := store.factions["sect"].Permissions["outer_gate_access"].(bool); !granted {
		t.Fatalf("permission was not granted at threshold: %+v", store.factions["sect"])
	}
	if _, err := worldactions.AdjustFaction(context.Background(), model, map[string]any{
		"faction_key": "sect", "contribution_delta": int64(1000),
	}); err != nil {
		t.Fatal(err)
	}
	if store.factions["sect"].PositionKey != "outer_disciple" {
		t.Fatalf("contribution position was not derived: %+v", store.factions["sect"])
	}
	if _, err := travel(context.Background(), player, travelArguments); err != nil {
		t.Fatalf("travel with permission failed: %v", err)
	}

	if _, err := worldactions.AdjustFaction(context.Background(), model, map[string]any{
		"faction_key": "sect", "reputation_delta": int64(-1),
	}); err != nil {
		t.Fatal(err)
	}
	if _, exists := store.factions["sect"].Permissions["outer_gate_access"]; exists {
		t.Fatalf("permission was not revoked below threshold: %+v", store.factions["sect"])
	}
	if _, err := travel(context.Background(), player, travelArguments); !errors.Is(err, campaign.ErrActionRejected) {
		t.Fatalf("travel after permission revocation error = %v", err)
	}
}

func TestInstantiateNPCUsesSpawnableArchetypeAndLimit(t *testing.T) {
	store := newActionStore()
	campaignID, versionID := uuid.New(), uuid.New()
	store.definitions[storage.DefinitionNPCArchetype+":gatherer"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionNPCArchetype, Key: "gatherer", Name: "采药散修",
		Content: storage.JSONMap{
			"spawnable": true, "max_instances": int64(1),
			"allowed_locations": []any{"village"}, "realm_key": "mortal", "stage_key": "ordinary",
			"public_profile": map[string]any{"role": "采药人"},
			"private_state":  map[string]any{"secret": "隐藏线索"},
			"goals":          map[string]any{"primary": "出售药材"},
		},
	}
	execution := campaign.ActionExecution{
		Campaign: storage.Campaign{
			ID: campaignID, ChallengeVersionID: versionID, CurrentLocationKey: "village", GameTime: 30,
		},
		Store: store, ActorType: "model", ActionKey: ActionInstantiateNPC,
	}
	if _, err := instantiateNPC(context.Background(), execution, map[string]any{
		"archetype_key": "gatherer", "instance_key": "gatherer-1", "stable_name": "陈七",
	}); err != nil {
		t.Fatal(err)
	}
	npc := store.npcs["gatherer-1"]
	if npc == nil || npc.StableName != "陈七" || npc.CurrentLocationKey != "village" || npc.LastActiveAt != 30 {
		t.Fatalf("unexpected NPC instance: %+v", npc)
	}
	if _, err := instantiateNPC(context.Background(), execution, map[string]any{
		"archetype_key": "gatherer", "instance_key": "gatherer-2",
	}); !errors.Is(err, campaign.ErrActionRejected) {
		t.Fatalf("instance limit error = %v", err)
	}
	execution.ActorType = "player"
	if _, err := instantiateNPC(context.Background(), execution, map[string]any{
		"archetype_key": "gatherer", "instance_key": "gatherer-3",
	}); !errors.Is(err, campaign.ErrActionRejected) {
		t.Fatalf("player instantiate error = %v", err)
	}
}

func TestNPCLifecycleMovementAndStatusTransitionsAreBounded(t *testing.T) {
	store := newActionStore()
	campaignID := uuid.New()
	store.definitions[storage.DefinitionLocation+":village"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionLocation, Key: "village",
		Content: storage.JSONMap{"connections": []any{map[string]any{
			"location_key": "ferry", "travel_minutes": int64(30),
		}}},
	}
	store.definitions[storage.DefinitionLocation+":ferry"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionLocation, Key: "ferry", Content: storage.JSONMap{"connections": []any{}},
	}
	store.npcs["ferryman"] = &storage.CampaignNPC{
		ID: uuid.New(), CampaignID: campaignID, InstanceKey: "ferryman", StableName: "老邢",
		CurrentLocationKey: "village", Status: "active",
	}
	execution := campaign.ActionExecution{
		Campaign: storage.Campaign{ID: campaignID, CurrentLocationKey: "village", GameTime: 90},
		Store:    store, ActorType: "model", ActionKey: ActionMoveNPC,
	}
	if _, err := worldactions.MoveNPC(context.Background(), execution, map[string]any{
		"npc_key": "ferryman", "destination_key": "ferry",
	}); err != nil {
		t.Fatal(err)
	}
	if store.npcs["ferryman"].CurrentLocationKey != "ferry" || store.npcs["ferryman"].LastActiveAt != 90 {
		t.Fatalf("NPC move was not persisted: %+v", store.npcs["ferryman"])
	}
	if _, err := worldactions.SetNPCStatus(context.Background(), execution, map[string]any{
		"npc_key": "ferryman", "status": "missing",
	}); !errors.Is(err, campaign.ErrActionRejected) {
		t.Fatalf("model changed remote NPC status: %v", err)
	}
	execution.ActorType = "system"
	if _, err := worldactions.SetNPCStatus(context.Background(), execution, map[string]any{
		"npc_key": "ferryman", "status": "missing",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := worldactions.SetNPCStatus(context.Background(), execution, map[string]any{
		"npc_key": "ferryman", "status": "dead",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := worldactions.SetNPCStatus(context.Background(), execution, map[string]any{
		"npc_key": "ferryman", "status": "active",
	}); !errors.Is(err, campaign.ErrActionRejected) {
		t.Fatalf("dead NPC was revived: %v", err)
	}
}

func TestBreakthroughIsDeterministicForSameAction(t *testing.T) {
	build := func() (*actionStoreStub, campaign.ActionExecution) {
		store := newActionStore()
		campaignID := uuid.New()
		store.character = &storage.CampaignCharacter{
			CampaignID: campaignID, RealmKey: "mortal", RealmRank: 0,
			CultivationProgress: 100, CultivationRequired: 100, FoundationQuality: 50,
			Health: 100, MaxHealth: 100, Mana: 20, MaxMana: 20, SpiritualSense: 10, MaxSpiritualSense: 10,
			LifespanDays: 1000, Injuries: storage.JSONMap{},
		}
		store.definitions[storage.DefinitionRealm+":mortal"] = &storage.GameplayDefinition{
			Kind: storage.DefinitionRealm, Key: "mortal", Content: storage.JSONMap{
				"rank": int64(0), "next_realm_key": "qi", "breakthrough_chance": int64(50), "breakthrough_minutes": int64(60),
			},
		}
		store.definitions[storage.DefinitionRealm+":qi"] = &storage.GameplayDefinition{
			Kind: storage.DefinitionRealm, Key: "qi", Content: storage.JSONMap{
				"rank": int64(1), "cultivation_required": int64(500), "lifespan_days": int64(2000), "stages": []any{"early"},
			},
		}
		return store, campaign.ActionExecution{
			Campaign: storage.Campaign{ID: campaignID, RandomSeed: 42, Revision: 7},
			Store:    store, ActorType: "player", ActionID: "same-action", MaxDueEventsPerTurn: 32,
		}
	}
	firstStore, firstExecution := build()
	secondStore, secondExecution := build()
	first, err := attemptBreakthrough(context.Background(), firstExecution, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := attemptBreakthrough(context.Background(), secondExecution, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Result["roll"] != second.Result["roll"] || first.Result["success"] != second.Result["success"] || firstStore.character.RealmKey != secondStore.character.RealmKey {
		t.Fatalf("breakthrough was not deterministic: first=%+v second=%+v", first.Result, second.Result)
	}
}

func TestBreakthroughUsesAuthoritativeCampaignRoll(t *testing.T) {
	store := newActionStore()
	campaignID := uuid.New()
	store.character = &storage.CampaignCharacter{
		CampaignID: campaignID, RealmKey: "mortal", RealmRank: 0, CultivationProgress: 100, CultivationRequired: 100,
		FoundationQuality: 100, Health: 100, MaxHealth: 100, Mana: 20, MaxMana: 20,
		SpiritualSense: 10, MaxSpiritualSense: 10, LifespanDays: 1000, Injuries: storage.JSONMap{},
	}
	store.definitions[storage.DefinitionRealm+":mortal"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionRealm, Key: "mortal", Name: "凡人", Content: storage.JSONMap{
			"rank": int64(0), "next_realm_key": "qi", "breakthrough_chance": int64(95), "breakthrough_minutes": int64(60),
		},
	}
	store.definitions[storage.DefinitionRealm+":qi"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionRealm, Key: "qi", Name: "练气", Content: storage.JSONMap{
			"rank": int64(1), "cultivation_required": int64(500), "lifespan_days": int64(2000), "stages": []any{"early"},
		},
	}
	execution := campaign.ActionExecution{
		Campaign: storage.Campaign{ID: campaignID, RandomSeed: 42}, Store: store, ActorType: "player",
		ActionID: "forced-roll", Roll: forcedCampaignRoll(false, "failure", 99, 95), MaxDueEventsPerTurn: 32,
	}
	outcome, err := attemptBreakthrough(context.Background(), execution, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Result["success"] != false || outcome.Result["roll"] != 99 || outcome.Roll == nil || store.character.RealmKey != "mortal" {
		t.Fatalf("authoritative roll did not decide breakthrough: outcome=%+v character=%+v", outcome, store.character)
	}
}

func forcedCampaignRoll(success bool, level string, rolled, chance int) campaign.CampaignRollFunc {
	return func(context.Context, campaign.CampaignRollRequest) (*campaign.CampaignRollResult, error) {
		formatted := "【天命判定】测试"
		return &campaign.CampaignRollResult{
			Success: success, Level: level, Rolled: rolled, Sides: 100, SuccessRate: chance,
			Snapshot: &storage.CampaignRollSnapshot{
				RollKey: "test.roll", Title: "测试判定", Category: "exploration", Success: success,
				Content: formatted, Markdown: formatted, Metadata: storage.JSONMap{"roll_level": level},
			},
		}, nil
	}
}

func TestBreakthroughRejectsInvalidRealmRankState(t *testing.T) {
	tests := []struct {
		name        string
		character   int
		currentRank any
		nextRank    any
	}{
		{name: "character cache drift", character: 1, currentRank: int64(0), nextRank: int64(2)},
		{name: "missing next rank", character: 0, currentRank: int64(0), nextRank: nil},
		{name: "non increasing next rank", character: 1, currentRank: int64(1), nextRank: int64(1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newActionStore()
			campaignID := uuid.New()
			store.character = &storage.CampaignCharacter{
				CampaignID: campaignID, RealmKey: "mortal", RealmRank: test.character,
				CultivationProgress: 100, CultivationRequired: 100, FoundationQuality: 50,
			}
			store.definitions[storage.DefinitionRealm+":mortal"] = &storage.GameplayDefinition{
				Kind: storage.DefinitionRealm, Key: "mortal", Content: storage.JSONMap{
					"rank": test.currentRank, "next_realm_key": "qi",
				},
			}
			nextContent := storage.JSONMap{"cultivation_required": int64(200), "stages": []any{"early"}}
			if test.nextRank != nil {
				nextContent["rank"] = test.nextRank
			}
			store.definitions[storage.DefinitionRealm+":qi"] = &storage.GameplayDefinition{
				Kind: storage.DefinitionRealm, Key: "qi", Content: nextContent,
			}
			_, err := attemptBreakthrough(context.Background(), campaign.ActionExecution{
				Campaign: storage.Campaign{ID: campaignID}, Store: store, ActorType: "player", ActionID: "invalid-rank",
			}, map[string]any{})
			if !errors.Is(err, campaign.ErrActionRejected) || store.character.RealmKey != "mortal" || store.character.RealmRank != test.character {
				t.Fatalf("invalid rank state was not rejected atomically: character=%+v err=%v", store.character, err)
			}
		})
	}
}

func TestBreakthroughUpdatesRealmKeyAndDerivedRankTogether(t *testing.T) {
	store := newActionStore()
	campaignID := uuid.New()
	store.character = &storage.CampaignCharacter{
		CampaignID: campaignID, RealmKey: "mortal", RealmRank: 0,
		CultivationProgress: 100, CultivationRequired: 100, FoundationQuality: 50,
		Health: 100, MaxHealth: 100, Mana: 20, MaxMana: 20, SpiritualSense: 10, MaxSpiritualSense: 10,
		LifespanDays: 1000, Injuries: storage.JSONMap{},
	}
	store.definitions[storage.DefinitionRealm+":mortal"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionRealm, Key: "mortal", Content: storage.JSONMap{
			"rank": int64(0), "next_realm_key": "qi", "breakthrough_chance": int64(95), "breakthrough_minutes": int64(60),
		},
	}
	store.definitions[storage.DefinitionRealm+":qi"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionRealm, Key: "qi", Content: storage.JSONMap{
			"rank": int64(7), "cultivation_required": int64(500), "lifespan_days": int64(2000), "stages": []any{"early"},
		},
	}
	execution := campaign.ActionExecution{
		Campaign: storage.Campaign{ID: campaignID, RandomSeed: 42, Revision: 7},
		Store:    store, ActorType: "player", MaxDueEventsPerTurn: 32,
	}
	for attempt := 0; ; attempt++ {
		execution.ActionID = "rank-pair-" + strconv.Itoa(attempt)
		if worldactions.DeterministicPercent(execution, "breakthrough:mortal") <= 95 {
			break
		}
	}
	outcome, err := attemptBreakthrough(context.Background(), execution, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Result["success"] != true || store.character.RealmKey != "qi" || store.character.RealmRank != 7 {
		t.Fatalf("realm identity pair was not updated from the target definition: character=%+v result=%+v", store.character, outcome.Result)
	}
}

func TestPracticeExhaustsLifespanAtDayBoundary(t *testing.T) {
	store := newActionStore()
	campaignID := uuid.New()
	slot := "primary"
	store.character = &storage.CampaignCharacter{
		CampaignID: campaignID, RealmKey: "mortal", StageKey: "early",
		CultivationRequired: 100, AgeDays: 99, LifespanDays: 100, Mana: 10,
	}
	store.techniques["breathing"] = &storage.CampaignTechnique{
		ID: uuid.New(), CampaignID: campaignID, TechniqueKey: "breathing",
		LearnedLayer: 1, Status: "active", EquippedSlot: &slot,
	}
	store.definitions[storage.DefinitionTechnique+":breathing"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionTechnique, Key: "breathing",
		Content: storage.JSONMap{
			"max_layer": int64(12), "practice_minutes": int64(20), "cultivation_gain": int64(1),
			"mastery_gain": int64(1), "insight_gain": int64(1),
		},
	}
	store.definitions[storage.DefinitionRealm+":mortal"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionRealm, Key: "mortal", Content: storage.JSONMap{"stages": []any{"early"}},
	}
	execution := campaign.ActionExecution{
		Campaign: storage.Campaign{ID: campaignID, GameTime: 1430}, Store: store, ActorType: "player",
	}
	outcome, err := practice(context.Background(), execution, map[string]any{"technique_key": "breathing"})
	if err != nil {
		t.Fatal(err)
	}
	if store.character.AgeDays != 100 || outcome.Status == nil || *outcome.Status != storage.CampaignStatusDead {
		t.Fatalf("lifespan was not exhausted: character=%+v outcome=%+v", store.character, outcome)
	}
	if len(outcome.Events) != 2 || outcome.Events[1].EventType != "character.lifespan_exhausted" {
		t.Fatalf("lifespan event missing: %+v", outcome.Events)
	}
}

func TestConditionsRequireAuthorityAndCanBeResolved(t *testing.T) {
	store := newActionStore()
	campaignID := uuid.New()
	store.character = &storage.CampaignCharacter{CampaignID: campaignID, LifespanDays: 1000}
	arguments := map[string]any{
		"category": "injury", "effect_key": "broken-rib", "severity": int64(35),
		"duration_minutes": int64(1440), "description": "肋骨骨裂",
	}
	player := campaign.ActionExecution{
		Campaign: storage.Campaign{ID: campaignID}, Store: store, ActorType: "player", ActionKey: ActionApplyCondition,
	}
	if _, err := applyCondition(context.Background(), player, arguments); !errors.Is(err, campaign.ErrActionRejected) {
		t.Fatalf("player condition error = %v", err)
	}
	model := player
	model.ActorType = "model"
	if _, err := applyCondition(context.Background(), model, arguments); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.character.Injuries["broken-rib"]; !ok {
		t.Fatalf("injury was not stored: %+v", store.character.Injuries)
	}
	if _, err := resolveCondition(context.Background(), model, map[string]any{
		"category": "injury", "effect_key": "broken-rib",
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.character.Injuries["broken-rib"]; ok {
		t.Fatalf("injury was not resolved: %+v", store.character.Injuries)
	}
}

func TestEquipItemKeepsOneInstancePerSlot(t *testing.T) {
	store := newActionStore()
	campaignID := uuid.New()
	characterID := uuid.New()
	slot := "weapon"
	store.character = &storage.CampaignCharacter{ID: characterID, CampaignID: campaignID, LifespanDays: 1000}
	store.definitions[storage.DefinitionEquipment+":sword"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionEquipment, Key: "sword",
		Content: storage.JSONMap{"allowed_slots": []any{"weapon"}},
	}
	oldID, newID := uuid.New(), uuid.New()
	store.instances[oldID] = &storage.CampaignItemInstance{
		ID: oldID, CampaignID: campaignID, DefinitionKey: "sword", State: "equipped",
		EquippedSlot: &slot, Durability: 100,
	}
	store.instances[newID] = &storage.CampaignItemInstance{
		ID: newID, CampaignID: campaignID, DefinitionKey: "sword", State: "stored", Durability: 100,
	}
	execution := campaign.ActionExecution{Campaign: storage.Campaign{ID: campaignID}, Store: store, ActorType: "player"}
	if _, err := equipItem(context.Background(), execution, map[string]any{
		"item_instance_id": newID.String(), "slot": "weapon",
	}); err != nil {
		t.Fatal(err)
	}
	if store.instances[oldID].State != "stored" || store.instances[oldID].EquippedSlot != nil {
		t.Fatalf("old equipment was not cleared: %+v", store.instances[oldID])
	}
	if store.instances[newID].State != "equipped" || store.instances[newID].EquippedSlot == nil || *store.instances[newID].EquippedSlot != "weapon" {
		t.Fatalf("new equipment was not equipped: %+v", store.instances[newID])
	}
}

func TestEquipmentBonusesRecalculateAndClampEffectiveStats(t *testing.T) {
	store := newActionStore()
	campaignID := uuid.New()
	characterID := uuid.New()
	store.character = &storage.CampaignCharacter{
		ID: characterID, CampaignID: campaignID, Health: 100,
		BaseMaxHealth: 100, MaxHealth: 100, BaseMaxMana: 20, MaxMana: 20,
		BaseMaxSpiritualSense: 10, MaxSpiritualSense: 10, LifespanDays: 1000,
	}
	store.definitions[storage.DefinitionEquipment+":amulet"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionEquipment, Key: "amulet",
		Content: storage.JSONMap{
			"allowed_slots": []any{"accessory"},
			"bonuses":       map[string]any{"max_health": int64(20), "physical_defense": int64(3)},
		},
	}
	itemID := uuid.New()
	store.instances[itemID] = &storage.CampaignItemInstance{
		ID: itemID, CampaignID: campaignID, DefinitionKey: "amulet", State: "stored", Durability: 10,
	}
	execution := campaign.ActionExecution{Campaign: storage.Campaign{ID: campaignID}, Store: store, ActorType: "player"}
	if _, err := equipItem(context.Background(), execution, map[string]any{
		"item_instance_id": itemID.String(), "slot": "accessory",
	}); err != nil {
		t.Fatal(err)
	}
	if store.character.MaxHealth != 120 {
		t.Fatalf("equipment max health bonus was not applied: %+v", store.character)
	}
	store.character.Health = 120
	if _, err := unequipItem(context.Background(), execution, map[string]any{"item_instance_id": itemID.String()}); err != nil {
		t.Fatal(err)
	}
	if store.character.MaxHealth != 100 || store.character.Health != 100 {
		t.Fatalf("unequip did not remove and clamp bonus: %+v", store.character)
	}
}

func TestAdjustVitalsEndsCampaignAtZeroHealth(t *testing.T) {
	store := newActionStore()
	campaignID := uuid.New()
	store.character = &storage.CampaignCharacter{
		CampaignID: campaignID, Health: 5, MaxHealth: 100, MaxMana: 20, MaxSpiritualSense: 10,
	}
	execution := campaign.ActionExecution{
		Campaign: storage.Campaign{ID: campaignID, GameTime: 77}, Store: store,
		ActorType: "model", ActionKey: ActionAdjustVitals,
	}
	outcome, err := adjustVitals(context.Background(), execution, map[string]any{
		"health_delta": int64(-10), "cause_key": "combat.defeat",
	})
	if err != nil {
		t.Fatal(err)
	}
	if store.character.Health != 0 || outcome.Status == nil || *outcome.Status != storage.CampaignStatusDead {
		t.Fatalf("lethal vital change did not end campaign: character=%+v outcome=%+v", store.character, outcome)
	}
	if len(outcome.Events) != 2 || outcome.Events[1].EventType != "character.died" {
		t.Fatalf("death event missing: %+v", outcome.Events)
	}
}

func TestTimedConditionsExpireAfterTimeAdvance(t *testing.T) {
	store := newActionStore()
	campaignID := uuid.New()
	store.character = &storage.CampaignCharacter{
		CampaignID: campaignID,
		Injuries: storage.JSONMap{"sprain": map[string]any{
			"applied_at_game_time": int64(10), "duration_minutes": int64(30),
		}},
		LongTermEffects: storage.JSONMap{},
	}
	outcome := storage.CampaignActionOutcome{GameTimeDelta: 20, Result: storage.JSONMap{}}
	execution := campaign.ActionExecution{Campaign: storage.Campaign{ID: campaignID, GameTime: 20}, Store: store}
	if err := expireDueConditions(execution, &outcome); err != nil {
		t.Fatal(err)
	}
	if _, exists := store.character.Injuries["sprain"]; exists || len(outcome.Events) != 1 || outcome.Events[0].EventType != "character.condition_expired" {
		t.Fatalf("condition did not expire: character=%+v outcome=%+v", store.character, outcome)
	}
}

func TestRelationshipDeltasAreBounded(t *testing.T) {
	store := newActionStore()
	campaignID, npcID := uuid.New(), uuid.New()
	store.npcs["herbalist"] = &storage.CampaignNPC{
		ID: npcID, CampaignID: campaignID, InstanceKey: "herbalist", StableName: "顾药师",
		CurrentLocationKey: "village", Status: "active",
	}
	store.relationships[npcID] = &storage.CampaignRelationship{
		CampaignID: campaignID, NPCID: npcID, Trust: 95, Fear: 2,
		Affinity: -95, Hostility: 95, PublicStance: "neutral", KnownFacts: storage.JSONMap{},
	}
	execution := campaign.ActionExecution{
		Campaign: storage.Campaign{ID: campaignID, CurrentLocationKey: "village", GameTime: 120},
		Store:    store, ActorType: "model", ActionKey: ActionRecordInteraction,
	}
	_, err := worldactions.RecordInteraction(context.Background(), execution, map[string]any{
		"npc_key": "herbalist", "trust_delta": int64(20), "fear_delta": int64(-20),
		"affinity_delta": int64(-20), "hostility_delta": int64(20),
	})
	if err != nil {
		t.Fatal(err)
	}
	relation := store.relationships[npcID]
	if relation.Trust != 100 || relation.Fear != 0 || relation.Affinity != -100 || relation.Hostility != 100 {
		t.Fatalf("relationship bounds were not enforced: %+v", relation)
	}
}

func TestPlayerRelationshipInteractionIsRestricted(t *testing.T) {
	store := newActionStore()
	campaignID, npcID := uuid.New(), uuid.New()
	store.npcs["ferryman"] = &storage.CampaignNPC{
		ID: npcID, CampaignID: campaignID, InstanceKey: "ferryman", StableName: "老邢",
		CurrentLocationKey: "ferry", Status: "active",
	}
	store.relationships[npcID] = &storage.CampaignRelationship{
		CampaignID: campaignID, NPCID: npcID, Trust: 3, Affinity: 0,
		PublicStance: "neutral", KnownFacts: storage.JSONMap{},
	}
	execution := campaign.ActionExecution{
		Campaign: storage.Campaign{ID: campaignID, CurrentLocationKey: "ferry", GameTime: 120},
		Store:    store, ActorType: "player", ActionKey: ActionRecordInteraction,
	}
	outcome, err := worldactions.RecordInteraction(context.Background(), execution, map[string]any{
		"npc_key": "ferryman", "trust_delta": int64(1), "affinity_delta": int64(1),
	})
	if err != nil {
		t.Fatal(err)
	}
	relation := store.relationships[npcID]
	if relation.Trust != 4 || relation.Affinity != 1 || relation.PublicStance != "cordial" {
		t.Fatalf("unexpected player interaction: %+v", relation)
	}
	if outcome.GameTimeDelta != 15 {
		t.Fatalf("player interaction time = %d, want 15", outcome.GameTimeDelta)
	}
	if _, err := worldactions.RecordInteraction(context.Background(), execution, map[string]any{
		"npc_key": "ferryman", "trust_delta": int64(20), "affinity_delta": int64(1),
	}); err == nil {
		t.Fatal("player interaction must reject trust_delta above 1")
	}
}
