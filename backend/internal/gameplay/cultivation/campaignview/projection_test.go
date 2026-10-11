package campaignview

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldview"
	"github.com/ialtone/fatecraft/internal/storage"
	"strings"
	"testing"
)

func TestContextProjectionRemainsBoundedWithLargeDomainState(t *testing.T) {
	state := &storage.CampaignDomainState{Definitions: map[string]storage.GameplayDefinition{}}
	campaignID := uuid.New()
	state.Character = &storage.CampaignCharacter{CampaignID: campaignID, RealmKey: "foundation", RealmRank: 2}
	for index := 0; index < 5_000; index++ {
		key := fmt.Sprintf("item-%04d", index)
		state.ItemStacks = append(state.ItemStacks, storage.CampaignItemStack{CampaignID: campaignID, ItemKey: key, QualityKey: "common", Quantity: 1})
		state.Definitions[storage.DefinitionItem+":"+key] = storage.GameplayDefinition{Kind: storage.DefinitionItem, Key: key, Name: key, Content: storage.JSONMap{"stackable": true}}
		state.Encounters = append(state.Encounters, storage.CampaignEncounter{ID: uuid.New(), CampaignID: campaignID, EncounterKey: "encounter", Status: "won"})
	}
	projection := map[string]any{}
	addDomainProjection(projection, storage.Campaign{ID: campaignID}, state)
	inventory := projection["inventory"].(map[string]any)
	if len(inventory["stacks"].([]map[string]any)) > 40 || len(projection["encounters"].([]map[string]any)) > 4 {
		t.Fatal("context projection exceeded collection limits")
	}
	raw, err := json.Marshal(projection)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > 128*1024 {
		t.Fatalf("large state produced %d-byte context projection", len(raw))
	}
}

func TestGenericContextProjectionOmitsCultivationDomain(t *testing.T) {
	campaignID := uuid.New()
	state := &storage.CampaignDomainState{
		Character:  &storage.CampaignCharacter{CampaignID: campaignID, RealmKey: "mortal"},
		Techniques: []storage.CampaignTechnique{{CampaignID: campaignID, TechniqueKey: "technique"}},
		Encounters: []storage.CampaignEncounter{{CampaignID: campaignID, EncounterKey: "encounter"}},
		Definitions: map[string]storage.GameplayDefinition{
			storage.DefinitionLocation + ":start": {
				Kind: storage.DefinitionLocation, Key: "start", Name: "Start",
				Content: storage.JSONMap{"node_type": "location", "connections": []any{}},
			},
		},
	}
	view, err := (worldview.Provider{}).Context(context.Background(), campaign.ProjectionContext{
		Campaign: storage.Campaign{ID: campaignID, CurrentLocationKey: "start"},
		Domain:   state, ContextPolicy: "campaign.generic_context",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"character", "realm_system", "techniques", "recipes", "encounters", "available_encounters"} {
		if _, exists := view.Data[key]; exists {
			t.Fatalf("generic projection exposed cultivation key %q: %#v", key, view.Data[key])
		}
	}
	if _, exists := view.Data["world"]; !exists {
		t.Fatal("generic projection omitted world data")
	}
}

func TestRealmProgressionViewIsOrderedAndPublic(t *testing.T) {
	state := &storage.CampaignDomainState{Definitions: map[string]storage.GameplayDefinition{
		storage.DefinitionRealm + ":foundation": {
			Kind: storage.DefinitionRealm, Key: "foundation", Name: "筑基",
			Content: storage.JSONMap{"rank": int64(2), "stages": []any{"early"}, "next_realm_key": "golden-core"},
		},
		storage.DefinitionRealm + ":mortal": {
			Kind: storage.DefinitionRealm, Key: "mortal", Name: "凡人",
			Content: storage.JSONMap{"rank": int64(0), "stages": []any{"ordinary"}, "next_realm_key": "qi-refining"},
		},
		storage.DefinitionRealm + ":hidden": {
			Kind: storage.DefinitionRealm, Key: "hidden", Name: "隐藏境界", Visibility: storage.CampaignVisibilityInternal,
			Content: storage.JSONMap{"rank": int64(1), "stages": []any{"early"}},
		},
	}}
	rows := realmProgressionView(state)
	if len(rows) != 2 || rows[0]["key"] != "mortal" || rows[1]["key"] != "foundation" {
		t.Fatalf("realm progression = %#v", rows)
	}
	if rows[0]["next_realm_key"] != "qi-refining" {
		t.Fatalf("realm transition missing: %#v", rows[0])
	}
}

func TestCampaignProjectionsBoundNPCVisibility(t *testing.T) {
	campaignID := uuid.New()
	currentID, remoteID := uuid.New(), uuid.New()
	row := storage.Campaign{ID: campaignID, CurrentLocationKey: "village"}
	state := &storage.CampaignDomainState{
		NPCs: []storage.CampaignNPC{
			{
				ID: currentID, CampaignID: campaignID, InstanceKey: "herbalist", StableName: "顾药师",
				CurrentLocationKey: "village", Status: "active",
				PublicProfile: storage.JSONMap{"role": "药师"},
				PrivateState:  storage.JSONMap{"secret": "current-secret"},
				Goals:         storage.JSONMap{"primary": "current-goal"},
			},
			{
				ID: remoteID, CampaignID: campaignID, InstanceKey: "ferryman", StableName: "老邢",
				CurrentLocationKey: "ferry", Status: "active",
				PublicProfile: storage.JSONMap{"role": "守渡人"},
				PrivateState:  storage.JSONMap{"secret": "remote-secret"},
				Goals:         storage.JSONMap{"primary": "remote-goal"},
			},
		},
		Relationships: []storage.CampaignRelationship{{
			CampaignID: campaignID, NPCID: remoteID, Trust: 10,
			PublicStance: "known", KnownFacts: storage.JSONMap{}, LastInteractionAt: 0,
		}},
		Definitions: map[string]storage.GameplayDefinition{
			storage.DefinitionNPCArchetype + ":current-prototype": {
				Kind: storage.DefinitionNPCArchetype, Key: "current-prototype", Name: "当前原型",
				Content: storage.JSONMap{
					"spawnable": true, "max_instances": int64(2), "allowed_locations": []any{"village"},
					"realm_key": "mortal", "public_profile": map[string]any{"role": "路人"},
					"private_state": map[string]any{"secret": "prototype-secret"},
				},
			},
			storage.DefinitionNPCArchetype + ":remote-prototype": {
				Kind: storage.DefinitionNPCArchetype, Key: "remote-prototype", Name: "远端原型",
				Content: storage.JSONMap{
					"spawnable": true, "max_instances": int64(2), "allowed_locations": []any{"ferry"},
				},
			},
		},
	}

	public := publicDomainSnapshot(row, state)
	publicJSON, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	publicText := string(publicJSON)
	if strings.Contains(publicText, "current-secret") || strings.Contains(publicText, "remote-secret") ||
		strings.Contains(publicText, "current-goal") || strings.Contains(publicText, "remote-goal") {
		t.Fatalf("player snapshot leaked private NPC data: %s", publicText)
	}
	publicRelationships := public["relationships"].([]map[string]any)
	if len(publicRelationships) != 2 {
		t.Fatalf("expected current and previously interacted NPCs, got %#v", publicRelationships)
	}

	model := map[string]any{}
	addDomainProjection(model, row, state)
	modelRelationships := model["relationships"].([]map[string]any)
	if len(modelRelationships) != 1 || modelRelationships[0]["instance_key"] != "herbalist" {
		t.Fatalf("remote NPC entered bounded model relationships: %#v", modelRelationships)
	}
	privateContext := model["current_npc_private_context"].([]map[string]any)
	if len(privateContext) != 1 || privateContext[0]["instance_key"] != "herbalist" {
		t.Fatalf("private context was not limited to the current location: %#v", privateContext)
	}
	modelJSON, err := json.Marshal(model)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(modelJSON), "remote-secret") || strings.Contains(string(modelJSON), "remote-goal") {
		t.Fatalf("remote private NPC data entered model context: %s", modelJSON)
	}
	archetypes := model["available_npc_archetypes"].([]map[string]any)
	if len(archetypes) != 1 || archetypes[0]["key"] != "current-prototype" || archetypes[0]["remaining_instances"] != int64(2) {
		t.Fatalf("available NPC archetypes were not location-bounded: %#v", archetypes)
	}
	if strings.Contains(string(modelJSON), "prototype-secret") {
		t.Fatalf("NPC archetype private template leaked into model catalog: %s", modelJSON)
	}
	referencedModel := map[string]any{}
	addDomainProjectionForQuery(referencedModel, row, state, "我想起老邢说过的夜船")
	referenced := referencedModel["referenced_relationships"].([]map[string]any)
	if len(referenced) != 1 || referenced[0]["instance_key"] != "ferryman" {
		t.Fatalf("explicitly referenced known NPC was not retrieved: %#v", referenced)
	}
	referencedJSON, err := json.Marshal(referenced)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(referencedJSON), "remote-secret") || strings.Contains(string(referencedJSON), "remote-goal") {
		t.Fatalf("referenced remote NPC leaked private state: %s", referencedJSON)
	}
}

func TestPhaseFourProjectionOnlyExposesCurrentRegionAndVisibleQuestFacts(t *testing.T) {
	due := int64(200)
	row := storage.Campaign{CurrentLocationKey: "village", GameTime: 100}
	state := &storage.CampaignDomainState{
		ItemStacks: []storage.CampaignItemStack{{ItemKey: "herb", QualityKey: "common", Quantity: 2}},
		RegionStates: []storage.CampaignRegionState{
			{RegionKey: "county", ResourceLevel: 5000, DangerLevel: 1200, Stability: 6000, FactionPower: storage.JSONMap{}, LastEventKey: "weather", LastEventVariant: "rain", LastEventSummary: "灵雨落下。", LastEventGameTime: pointerInt64(80)},
			{RegionKey: "remote", ResourceLevel: 1, DangerLevel: 9999, Stability: 1, FactionPower: storage.JSONMap{}, LastEventSummary: "remote-secret"},
		},
		Quests: []storage.CampaignQuest{{QuestKey: "delivery", QuestType: "obligation", Status: "active", DueGameTime: &due, Progress: storage.JSONMap{}}},
		Definitions: map[string]storage.GameplayDefinition{
			storage.DefinitionLocation + ":village":   {Kind: storage.DefinitionLocation, Key: "village", Name: "村落", Content: storage.JSONMap{"node_type": "location", "parent_key": "county"}},
			storage.DefinitionLocation + ":county":    {Kind: storage.DefinitionLocation, Key: "county", Name: "本郡", Content: storage.JSONMap{"node_type": "region", "parent_key": "realm"}},
			storage.DefinitionLocation + ":remote":    {Kind: storage.DefinitionLocation, Key: "remote", Name: "远郡", Content: storage.JSONMap{"node_type": "region", "parent_key": "realm"}},
			storage.DefinitionWorldEvent + ":weather": {Kind: storage.DefinitionWorldEvent, Key: "weather", Name: "灵雨"},
			storage.DefinitionItem + ":herb":          {Kind: storage.DefinitionItem, Key: "herb", Name: "灵草"},
			storage.DefinitionQuest + ":delivery": {
				Kind: storage.DefinitionQuest, Key: "delivery", Name: "限时交付", Content: storage.JSONMap{
					"quest_type": "obligation", "initial": true, "starting_status": "offered", "deadline_minutes": int64(100),
					"objectives": []any{map[string]any{"key": "herbs", "type": "item", "item_key": "herb", "quality_key": "common", "quantity": int64(3), "consume": true}},
				},
			},
		},
	}
	public := publicDomainSnapshot(row, state)
	region := public["region_state"].(map[string]any)
	if region["key"] != "county" || region["latest_event"].(map[string]any)["summary"] != "灵雨落下。" {
		t.Fatalf("unexpected current region projection: %#v", region)
	}
	obligations := public["obligations"].([]map[string]any)
	if len(obligations) != 1 || obligations[0]["can_submit"] != false {
		t.Fatalf("unexpected obligation projection: %#v", obligations)
	}
	encoded, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "remote-secret") {
		t.Fatalf("remote region leaked into snapshot: %s", encoded)
	}
}

func pointerInt64(value int64) *int64 { return &value }

func TestWorldProjectionMarksFactionGatedRoutes(t *testing.T) {
	row := storage.Campaign{CurrentLocationKey: "market"}
	state := &storage.CampaignDomainState{
		Discoveries: []storage.CampaignLocationDiscovery{{LocationKey: "market"}, {LocationKey: "sect-gate"}},
		Factions: []storage.CampaignFactionStanding{{
			FactionKey: "sect", Reputation: 99, Permissions: storage.JSONMap{},
		}},
		Definitions: map[string]storage.GameplayDefinition{
			storage.DefinitionLocation + ":market": {
				Kind: storage.DefinitionLocation, Key: "market", Name: "坊市",
				Content: storage.JSONMap{"connections": []any{map[string]any{
					"location_key": "sect-gate", "travel_minutes": float64(45),
				}}},
			},
			storage.DefinitionLocation + ":sect-gate": {
				Kind: storage.DefinitionLocation, Key: "sect-gate", Name: "山门",
				Content: storage.JSONMap{"entry_requirements": []any{map[string]any{
					"faction_key": "sect", "permission_key": "outer_gate_access",
				}}},
			},
			storage.DefinitionFaction + ":sect": {
				Kind: storage.DefinitionFaction, Key: "sect", Name: "青岚门",
				Content: storage.JSONMap{"permission_thresholds": map[string]any{"outer_gate_access": float64(100)}},
			},
		},
	}

	world := worldview.WorldView(row, state, false)
	routes := world["available_routes"].([]map[string]any)
	if len(routes) != 1 || routes[0]["accessible"] != false {
		t.Fatalf("locked route projection = %#v", routes)
	}
	missing := routes[0]["missing_requirements"].([]map[string]any)
	if len(missing) != 1 || missing[0]["faction_name"] != "青岚门" ||
		missing[0]["required_reputation"] != 100 || missing[0]["current_reputation"] != 99 {
		t.Fatalf("missing requirement projection = %#v", missing)
	}

	state.Factions[0].Reputation = 100
	state.Factions[0].Permissions = storage.JSONMap{"outer_gate_access": true}
	world = worldview.WorldView(row, state, false)
	routes = world["available_routes"].([]map[string]any)
	if routes[0]["accessible"] != true || len(routes[0]["missing_requirements"].([]map[string]any)) != 0 {
		t.Fatalf("unlocked route projection = %#v", routes)
	}
}

func TestProjectionIncludesActiveEffectsAndCurrentLocationPath(t *testing.T) {
	slot, equipmentSlot := "primary", "body"
	state := &storage.CampaignDomainState{
		Character: &storage.CampaignCharacter{
			RealmKey: "mortal", BaseMaxHealth: 100, MaxHealth: 110,
			BaseMaxMana: 20, MaxMana: 25, BaseMaxSpiritualSense: 10, MaxSpiritualSense: 10,
		},
		Techniques: []storage.CampaignTechnique{{
			TechniqueKey: "breathing", Mastery: 10000, LearnedLayer: 2, Status: "active", EquippedSlot: &slot,
		}},
		ItemInstances: []storage.CampaignItemInstance{{
			DefinitionKey: "robe", State: "equipped", EquippedSlot: &equipmentSlot, Durability: 10,
		}},
		Definitions: map[string]storage.GameplayDefinition{
			storage.DefinitionTechnique + ":breathing": {
				Kind: storage.DefinitionTechnique, Key: "breathing",
				Content: storage.JSONMap{"max_layer": int64(2), "layer_effects": []any{map[string]any{
					"layer": int64(1), "effects": map[string]any{"max_mana": int64(5)},
				}}},
			},
			storage.DefinitionEquipment + ":robe": {
				Kind: storage.DefinitionEquipment, Key: "robe",
				Content: storage.JSONMap{"bonuses": map[string]any{"max_health": int64(10), "physical_defense": int64(3)}},
			},
			storage.DefinitionLocation + ":world": {
				Kind: storage.DefinitionLocation, Key: "world", Name: "人间界",
				Content: storage.JSONMap{"node_type": "realm"},
			},
			storage.DefinitionLocation + ":county": {
				Kind: storage.DefinitionLocation, Key: "county", Name: "青河郡",
				Content: storage.JSONMap{"node_type": "region", "parent_key": "world"},
			},
			storage.DefinitionLocation + ":village": {
				Kind: storage.DefinitionLocation, Key: "village", Name: "青河村",
				Content: storage.JSONMap{"node_type": "location", "parent_key": "county", "connections": []any{}},
			},
		},
	}
	character := characterView(state).(map[string]any)
	effects := character["active_effects"].(map[string]int64)
	if effects["max_mana"] != 5 || effects["max_health"] != 10 || effects["physical_defense"] != 3 {
		t.Fatalf("active effects projection = %#v", effects)
	}
	world := worldview.WorldView(storage.Campaign{CurrentLocationKey: "village"}, state, false)
	path := world["current_path"].([]map[string]any)
	if len(path) != 3 || path[0]["key"] != "world" || path[2]["key"] != "village" {
		t.Fatalf("current location path = %#v", path)
	}
}

func TestCharacterProjectionUsesRealmStageLabel(t *testing.T) {
	state := &storage.CampaignDomainState{
		Character: &storage.CampaignCharacter{RealmKey: "qi-refining", StageKey: "layer-2"},
		Definitions: map[string]storage.GameplayDefinition{
			storage.DefinitionRealm + ":qi-refining": {
				Kind: storage.DefinitionRealm, Key: "qi-refining",
				Content: storage.JSONMap{
					"stages":       []any{"layer-1", "layer-2"},
					"stage_labels": []any{"一层", "二层"},
				},
			},
		},
	}
	view := characterView(state).(map[string]any)
	if view["stage_key"] != "layer-2" || view["stage_name"] != "二层" {
		t.Fatalf("stage projection = %#v", view)
	}
}

func TestTechniqueProjectionIncludesDynamicPracticeLimit(t *testing.T) {
	slot := "primary"
	state := &storage.CampaignDomainState{
		Character: &storage.CampaignCharacter{Mana: 11},
		Techniques: []storage.CampaignTechnique{{
			TechniqueKey: "basic-breathing", LearnedLayer: 1, Status: "active", EquippedSlot: &slot,
		}},
		Definitions: map[string]storage.GameplayDefinition{
			storage.DefinitionTechnique + ":basic-breathing": {
				Kind: storage.DefinitionTechnique, Key: "basic-breathing", Name: "基础引气诀",
				Content: storage.JSONMap{
					"max_layer": int64(12), "mana_cost": int64(1), "practice_minutes": int64(120),
					"cultivation_gain": int64(12), "mastery_gain": int64(35),
					"layer_effects": []any{map[string]any{
						"layer": int64(1), "effects": map[string]any{"max_mana": int64(5)},
					}},
				},
			},
		},
	}

	views := techniqueViews(state, 0)
	practice := views[0]["practice"].(map[string]any)
	if practice["available"] != true || practice["max_sessions"] != int64(11) ||
		practice["mana_cost_per_session"] != int64(1) ||
		practice["practice_minutes_per_session"] != int64(120) ||
		practice["cultivation_gain_per_session"] != int64(12) ||
		practice["mastery_gain_per_session"] != int64(35) {
		t.Fatalf("practice projection = %#v", practice)
	}

	state.Character.Mana = 0
	practice = techniqueViews(state, 0)[0]["practice"].(map[string]any)
	if practice["available"] != false || practice["max_sessions"] != int64(0) ||
		!strings.Contains(practice["unavailable_reason"].(string), "法力不足") {
		t.Fatalf("unavailable practice projection = %#v", practice)
	}
}

func TestEconomyProjectionBoundsMarketsAndLearnableRecipes(t *testing.T) {
	row := storage.Campaign{CurrentLocationKey: "town", GameTime: 60}
	state := &storage.CampaignDomainState{
		Character:    &storage.CampaignCharacter{RealmRank: 0, Mana: 20},
		KnownRecipes: []storage.CampaignKnownRecipe{{RecipeKey: "known-pill"}},
		ItemStacks: []storage.CampaignItemStack{
			{ItemKey: "herb", QualityKey: "common", Quantity: 3},
			{ItemKey: "recipe-note", QualityKey: "common", Quantity: 1},
		},
		Markets: []storage.CampaignMarketState{
			{MarketKey: "town-market", LocationKey: "town", LastRestockGameTime: 0, PriceModifierBPS: 10000},
			{MarketKey: "remote-market", LocationKey: "remote", LastRestockGameTime: 0, PriceModifierBPS: 10000},
		},
		MarketStocks: []storage.CampaignMarketStock{
			{MarketKey: "town-market", ItemKind: "item", ItemKey: "herb", QualityKey: "common", Quantity: 0},
			{MarketKey: "remote-market", ItemKind: "item", ItemKey: "herb", QualityKey: "common", Quantity: 9},
		},
		Factions: []storage.CampaignFactionStanding{{FactionKey: "guild", Reputation: 5000}},
		Definitions: map[string]storage.GameplayDefinition{
			storage.DefinitionItem + ":coin":             {Kind: storage.DefinitionItem, Key: "coin", Name: "灵石"},
			storage.DefinitionItem + ":herb":             {Kind: storage.DefinitionItem, Key: "herb", Name: "灵草"},
			storage.DefinitionItem + ":pill":             {Kind: storage.DefinitionItem, Key: "pill", Name: "丹药"},
			storage.DefinitionItem + ":recipe-note":      {Kind: storage.DefinitionItem, Key: "recipe-note", Name: "丹方玉简"},
			storage.DefinitionItem + ":other-note":       {Kind: storage.DefinitionItem, Key: "other-note", Name: "陌生玉简"},
			storage.DefinitionRecipe + ":known-pill":     economyRecipeDefinition("known-pill", true, ""),
			storage.DefinitionRecipe + ":learnable-pill": economyRecipeDefinition("learnable-pill", false, "recipe-note"),
			storage.DefinitionRecipe + ":hidden-pill":    economyRecipeDefinition("hidden-pill", false, "other-note"),
			storage.DefinitionMarket + ":town-market":    economyMarketDefinition("town-market", "town"),
			storage.DefinitionMarket + ":remote-market":  economyMarketDefinition("remote-market", "remote"),
		},
	}

	recipes := recipeViews(row, state, 0)
	if len(recipes) != 2 || recipes[0]["key"] != "known-pill" || recipes[1]["key"] != "learnable-pill" {
		t.Fatalf("recipe projection leaked or omitted recipes: %#v", recipes)
	}
	if recipes[1]["learnable"] != true || recipes[1]["source_item_name"] != "丹方玉简" || len(recipes[1]["outputs"].([]map[string]any)) != 0 {
		t.Fatalf("learnable recipe projection = %#v", recipes[1])
	}

	markets := worldview.MarketViews(row, state, 0)
	if len(markets) != 1 || markets[0]["key"] != "town-market" {
		t.Fatalf("remote market entered current projection: %#v", markets)
	}
	if markets[0]["last_restock_game_time"] != int64(60) {
		t.Fatalf("effective restock time = %#v", markets[0]["last_restock_game_time"])
	}
	items := markets[0]["items"].([]map[string]any)
	if len(items) != 1 || items[0]["stock"] != int64(2) || items[0]["buy_price"] != int64(90) || items[0]["sell_price"] != int64(66) {
		t.Fatalf("restocked reputation-adjusted market item = %#v", items)
	}
}

func economyRecipeDefinition(key string, startingKnown bool, sourceItemKey string) storage.GameplayDefinition {
	return storage.GameplayDefinition{
		Kind: storage.DefinitionRecipe, Key: key, Name: key,
		Content: storage.JSONMap{
			"craft_type": "alchemy", "starting_known": startingKnown, "source_item_key": sourceItemKey,
			"inputs":        []any{map[string]any{"quantity": int64(1), "options": []any{map[string]any{"item_key": "herb"}}}},
			"craft_minutes": int64(30), "success_chance_bps": int64(8000), "failure_input_loss_bps": int64(10000),
			"outputs": []any{map[string]any{"kind": "item", "key": "pill", "quantity": int64(1), "quality_weights": map[string]any{"common": int64(1)}}},
		},
	}
}

func economyMarketDefinition(key, location string) storage.GameplayDefinition {
	return storage.GameplayDefinition{
		Kind: storage.DefinitionMarket, Key: key, Name: key,
		Content: storage.JSONMap{
			"location_key": location, "currency_item_key": "coin", "faction_key": "guild",
			"restock_interval_minutes": int64(60), "scarcity_markup_bps": int64(0), "reputation_adjustment_bps": int64(2000),
			"items": []any{map[string]any{
				"kind": "item", "key": "herb", "quality_key": "common", "max_stock": int64(10),
				"initial_stock": int64(0), "restock_quantity": int64(2), "base_buy_price": int64(100), "base_sell_price": int64(60),
			}},
		},
	}
}
