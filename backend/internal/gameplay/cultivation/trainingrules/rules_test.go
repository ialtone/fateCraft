package trainingrules

import (
	"errors"
	"strings"
	"testing"

	"github.com/ialtone/fatecraft/internal/storage"
)

func TestViewsExplainIneligibleEquippedTechnique(t *testing.T) {
	slot := "primary"
	state := &storage.CampaignDomainState{
		Character: &storage.CampaignCharacter{
			RealmRank: 2, Health: 100, MaxHealth: 100, Mana: 100, MaxMana: 100,
			CultivationProgress: 10, CultivationRequired: 1000, AgeDays: 100, LifespanDays: 10000,
		},
		Techniques: []storage.CampaignTechnique{{
			TechniqueKey: "greenwood-nurturing", Status: "active", EquippedSlot: &slot,
			Mastery: 0, LearnedLayer: 1,
		}},
		ItemStacks: []storage.CampaignItemStack{{ItemKey: "herb", QualityKey: "common", Quantity: 5}},
		Definitions: map[string]storage.GameplayDefinition{
			"technique:greenwood-nurturing": {Kind: "technique", Key: "greenwood-nurturing", Name: "青木养元功", Content: storage.JSONMap{"max_layer": 12}},
			"technique:star-tide-scripture": {Kind: "technique", Key: "star-tide-scripture", Name: "星潮经", Content: storage.JSONMap{"max_layer": 12}},
			"item:herb":                     {Kind: "item", Key: "herb", Name: "灵草"},
			"cultivation.training:retreat-foundation": {
				Kind: DefinitionKind, Key: "retreat-foundation", Name: "筑基静修",
				Content: storage.JSONMap{
					"mode": "retreat", "min_realm_rank": 2, "max_realm_rank": 2, "days": 5,
					"location_keys":    []any{"village"},
					"technique_keys":   []any{"star-tide-scripture"},
					"materials":        []any{map[string]any{"item_key": "herb", "quantity": 1}},
					"cultivation_gain": 10, "mastery_gain": 10, "mana_cost": 1,
					"health_restore_bps": 0, "mana_restore_bps": 0, "sense_restore_bps": 0, "injury_relief": 0,
				},
			},
		},
	}
	row := storage.Campaign{GameTime: 0, Status: storage.CampaignStatusActive, CurrentLocationKey: "village"}

	views, err := Views(row, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 {
		t.Fatalf("views = %#v", views)
	}
	view := views[0]
	if view.Available || view.ReasonCode != "technique.ineligible" {
		t.Fatalf("view = %#v", view)
	}
	if !strings.Contains(view.Reason, "星潮经") || !strings.Contains(view.Reason, "青木养元功") {
		t.Fatalf("reason does not name required and equipped techniques: %q", view.Reason)
	}
	if len(view.RequiredTechniques) != 1 || view.RequiredTechniques[0] != "星潮经" {
		t.Fatalf("required = %#v", view.RequiredTechniques)
	}
	if len(view.EquippedTechniques) != 1 || view.EquippedTechniques[0] != "青木养元功" {
		t.Fatalf("equipped = %#v", view.EquippedTechniques)
	}
}

func TestReadyReportsMissingTechniqueSeparately(t *testing.T) {
	state := &storage.CampaignDomainState{
		Character: &storage.CampaignCharacter{
			RealmRank: 2, Health: 100, MaxHealth: 100, Mana: 100, MaxMana: 100,
			CultivationProgress: 10, CultivationRequired: 1000, AgeDays: 100, LifespanDays: 10000,
		},
		ItemStacks: []storage.CampaignItemStack{{ItemKey: "herb", QualityKey: "common", Quantity: 5}},
		Definitions: map[string]storage.GameplayDefinition{
			"technique:star-tide-scripture": {Kind: "technique", Key: "star-tide-scripture", Name: "星潮经", Content: storage.JSONMap{"max_layer": 12}},
		},
	}
	plan := Plan{
		Mode: "retreat", MinRealm: 2, MaxRealm: 2, Days: 5,
		LocationKeys: []string{"village"}, TechniqueKeys: []string{"star-tide-scripture"},
		Materials:       []Material{{ItemKey: "herb", Quantity: 1}},
		CultivationGain: 10, MasteryGain: 10, ManaCost: 1,
	}
	err := Ready(plan, storage.Campaign{Status: storage.CampaignStatusActive, CurrentLocationKey: "village"}, state)
	var unavailable *Unavailable
	if !errors.As(err, &unavailable) || unavailable.Code != "technique.none" {
		t.Fatalf("err = %#v", err)
	}
	if !strings.Contains(unavailable.Message, "星潮经") {
		t.Fatalf("message = %q", unavailable.Message)
	}
}
