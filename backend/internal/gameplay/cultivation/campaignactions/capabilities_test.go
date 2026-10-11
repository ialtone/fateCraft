package campaignactions

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/campaigncontract"
	"github.com/ialtone/fatecraft/internal/storage"
)

func TestEveryCampaignActionHasSchemaAndCapabilityImplementation(t *testing.T) {
	keys := ActionKeys()
	contractKeys := campaigncontract.ActionKeys()
	if len(keys) != len(contractKeys) {
		t.Fatalf("action key counts differ: registration=%d contract=%d", len(keys), len(contractKeys))
	}
	state := &storage.CampaignDomainState{Definitions: map[string]storage.GameplayDefinition{}}
	provider := cultivationCapabilityProvider{}
	for index, key := range keys {
		if key != contractKeys[index] {
			t.Fatalf("action key %d differs: %s != %s", index, key, contractKeys[index])
		}
		schema := actionArgumentSchema(key)
		if schema == nil || schema["additionalProperties"] != false {
			t.Fatalf("action %s has no closed schema", key)
		}
		_, err := provider.Project(context.Background(), campaign.CapabilityContext{
			Campaign: storage.Campaign{Revision: 1}, Domain: state, ActorType: "model",
		}, campaign.ActionDefinition{Key: key, Description: key, Actors: []string{"model"}, ArgumentSchema: schema})
		if err != nil {
			t.Fatalf("action %s capability error: %v", key, err)
		}
	}
}

func TestPracticeCapabilityUsesCurrentManaBoundary(t *testing.T) {
	slot := "primary"
	state := &storage.CampaignDomainState{
		Character:  &storage.CampaignCharacter{Mana: 11},
		Techniques: []storage.CampaignTechnique{{TechniqueKey: "breathing", Status: "active", EquippedSlot: &slot, LearnedLayer: 1}},
		Definitions: map[string]storage.GameplayDefinition{
			storage.DefinitionTechnique + ":breathing": {
				Kind: storage.DefinitionTechnique, Key: "breathing", Name: "引气诀", Content: storage.JSONMap{
					"max_layer": int64(12), "mana_cost": int64(1), "practice_minutes": int64(60), "cultivation_gain": int64(10), "mastery_gain": int64(25),
					"layer_effects": []any{map[string]any{"layer": int64(1), "effects": map[string]any{}}},
				},
			},
		},
	}
	capability, err := (cultivationCapabilityProvider{}).Project(context.Background(), campaign.CapabilityContext{
		Campaign: storage.Campaign{Revision: 9}, Domain: state, ActorType: "model",
	}, campaign.ActionDefinition{Key: ActionPractice, Description: "practice", Actors: []string{"model"}, ArgumentSchema: actionArgumentSchema(ActionPractice)})
	if err != nil {
		t.Fatal(err)
	}
	if !capability.Available || len(capability.Options) != 1 {
		t.Fatalf("capability = %#v", capability)
	}
	maximum := capability.ArgumentSchema["properties"].(map[string]any)["sessions"].(map[string]any)["maximum"]
	if maximum != int64(11) {
		t.Fatalf("sessions maximum = %#v", maximum)
	}
	limits := capability.Options[0]["limits"].(storage.JSONMap)["sessions"].(storage.JSONMap)
	if limits["maximum"] != int64(11) {
		t.Fatalf("option limits = %#v", limits)
	}
}

func TestActiveEncounterDisablesUnrelatedActions(t *testing.T) {
	encounterID := uuid.New()
	state := &storage.CampaignDomainState{
		Character:  &storage.CampaignCharacter{Health: 100, Mana: 5},
		Encounters: []storage.CampaignEncounter{{ID: encounterID, EncounterKey: "mist", Status: "active"}},
		Definitions: map[string]storage.GameplayDefinition{
			storage.DefinitionEncounter + ":mist": {
				Kind: storage.DefinitionEncounter, Key: "mist", Content: storage.JSONMap{
					"location_key": "mist-valley", "opponent_key": "mist-beast", "opponent_name": "雾兽",
					"opponent_realm_rank": int64(1), "opponent_max_health": int64(100), "opponent_attack": int64(10),
					"opponent_defense": int64(2), "player_damage_base": int64(5), "player_damage_scale_bps": int64(10000),
					"player_mana_cost": int64(10), "round_minutes": int64(5), "max_rounds": int64(10), "flee_chance_bps": int64(5000),
				},
			},
		},
	}
	provider := cultivationCapabilityProvider{}
	travel, err := provider.Project(context.Background(), campaign.CapabilityContext{Campaign: storage.Campaign{}, Domain: state}, campaign.ActionDefinition{
		Key: ActionTravel, Description: "travel", Actors: []string{"model"}, ArgumentSchema: actionArgumentSchema(ActionTravel),
	})
	if err != nil || travel.Available || travel.Violations[0].Code != "encounter.active" {
		t.Fatalf("travel capability = %#v, err=%v", travel, err)
	}
	act, err := provider.Project(context.Background(), campaign.CapabilityContext{Campaign: storage.Campaign{}, Domain: state}, campaign.ActionDefinition{
		Key: ActionEncounterAct, Description: "act", Actors: []string{"model"}, ArgumentSchema: actionArgumentSchema(ActionEncounterAct),
	})
	if err != nil || !act.Available || act.Options[0]["encounter_id"] != encounterID.String() {
		t.Fatalf("encounter capability = %#v, err=%v", act, err)
	}
	moves := act.ArgumentSchema["properties"].(map[string]any)["move"].(map[string]any)["enum"].([]any)
	if len(moves) != 1 || moves[0] != "defend" {
		t.Fatalf("moves with insufficient mana = %#v", moves)
	}
}

func TestTradeSellCapabilityUsesConcreteEquipmentInstances(t *testing.T) {
	firstID, secondID := uuid.New(), uuid.New()
	state := &storage.CampaignDomainState{
		Markets: []storage.CampaignMarketState{{
			CampaignID: uuid.New(), MarketKey: "green-market", LocationKey: "green-river",
			PriceModifierBPS: 10000, LastRestockGameTime: 0,
		}},
		MarketStocks: []storage.CampaignMarketStock{{
			MarketKey: "green-market", ItemKind: "equipment", ItemKey: "wood-token", QualityKey: "common", Quantity: 1,
		}},
		ItemInstances: []storage.CampaignItemInstance{
			{ID: firstID, DefinitionKey: "wood-token", QualityKey: "common", State: "stored", Durability: 10},
			{ID: secondID, DefinitionKey: "wood-token", QualityKey: "common", State: "stored", Durability: 10},
		},
		Definitions: map[string]storage.GameplayDefinition{
			storage.DefinitionMarket + ":green-market": {
				Kind: storage.DefinitionMarket, Key: "green-market", Content: storage.JSONMap{
					"location_key": "green-river", "currency_item_key": "spirit-stone", "restock_interval_minutes": int64(60),
					"items": []any{map[string]any{
						"kind": "equipment", "key": "wood-token", "quality_key": "common", "max_stock": int64(4),
						"initial_stock": int64(1), "restock_quantity": int64(0), "base_buy_price": int64(80), "base_sell_price": int64(42),
					}},
				},
			},
		},
	}
	capability, err := (cultivationCapabilityProvider{}).Project(context.Background(), campaign.CapabilityContext{
		Campaign: storage.Campaign{CurrentLocationKey: "green-river"}, Domain: state, ActorType: "model",
	}, campaign.ActionDefinition{
		Key: ActionTradeSell, Description: "sell", Actors: []string{"model"}, ArgumentSchema: actionArgumentSchema(ActionTradeSell),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !capability.Available || len(capability.Options) != 2 {
		t.Fatalf("equipment sell capability = %#v", capability)
	}
	seen := map[string]bool{}
	for _, option := range capability.Options {
		seen[option["item_instance_id"].(string)] = true
		limit := option["limits"].(storage.JSONMap)["quantity"].(storage.JSONMap)["maximum"]
		if limit != int64(1) {
			t.Fatalf("equipment quantity limit = %#v", limit)
		}
	}
	if !seen[firstID.String()] || !seen[secondID.String()] {
		t.Fatalf("equipment instance options = %#v", capability.Options)
	}
}

func TestPlayerExitPreviewRemainsAvailableDuringEncounter(t *testing.T) {
	state := &storage.CampaignDomainState{Encounters: []storage.CampaignEncounter{{Status: "active", EncounterKey: "danger"}}}
	definition := campaign.ActionDefinition{Key: ActionAbandon, Ending: true, Actors: []string{"player"}, ArgumentSchema: actionArgumentSchema(ActionAbandon)}
	provider := cultivationCapabilityProvider{}
	for _, actor := range []string{"player", "model"} {
		preview, err := provider.Project(context.Background(), campaign.CapabilityContext{Domain: state, ActorType: actor}, definition)
		if err != nil {
			t.Fatal(err)
		}
		if preview.Available != (actor == "player") {
			t.Fatalf("unexpected %s permission: %+v", actor, preview)
		}
	}
}
