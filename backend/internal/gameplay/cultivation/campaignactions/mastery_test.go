package campaignactions

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/storage"
)

func TestPracticeReachesHighTechniqueLayersAndEquipmentDoesNotDuplicateEffects(t *testing.T) {
	for _, maximum := range []int{12, 15, 18} {
		t.Run(fmt.Sprint(maximum), func(t *testing.T) {
			store := newActionStore()
			id := uuid.New()
			slot := "primary"
			store.character = &storage.CampaignCharacter{CampaignID: id, RealmKey: "mortal", CultivationRequired: 10000, BaseMaxHealth: 100, MaxHealth: 100, Health: 100, BaseMaxMana: 20, MaxMana: 20, Mana: 20, LifespanDays: 10000}
			store.techniques["high"] = &storage.CampaignTechnique{ID: uuid.New(), CampaignID: id, TechniqueKey: "high", Mastery: 9990, LearnedLayer: maximum - 1, Status: "active", EquippedSlot: &slot}
			store.definitions[storage.DefinitionTechnique+":high"] = &storage.GameplayDefinition{Kind: storage.DefinitionTechnique, Key: "high", Content: storage.JSONMap{
				"max_layer": int64(maximum), "practice_minutes": int64(10), "cultivation_gain": int64(2), "mastery_gain": int64(20), "mana_cost": int64(1), "allowed_slots": []any{"primary"},
				"layer_effects": []any{map[string]any{"layer": int64(maximum), "effects": map[string]any{"max_mana": int64(40)}}},
			}}
			store.definitions[storage.DefinitionRealm+":mortal"] = &storage.GameplayDefinition{Kind: storage.DefinitionRealm, Key: "mortal", Content: storage.JSONMap{"stages": []any{"early"}}}
			execution := campaign.ActionExecution{Campaign: storage.Campaign{ID: id}, Store: store, ActorType: "player"}
			ctx := context.Background()
			if _, err := practice(ctx, execution, map[string]any{"technique_key": "high"}); err != nil {
				t.Fatal(err)
			}
			if row := store.techniques["high"]; row.Mastery != 10000 || row.LearnedLayer != maximum || store.character.MaxMana != 60 {
				t.Fatalf("final layer/effect missing: %+v %+v", row, store.character)
			}
			if _, err := unequipTechnique(ctx, execution, map[string]any{"technique_key": "high"}); err != nil || store.character.MaxMana != 20 {
				t.Fatalf("unequip retained effect: %v %+v", err, store.character)
			}
			for i := 0; i < 2; i++ {
				if _, err := equipTechnique(ctx, execution, map[string]any{"technique_key": "high", "slot": "primary"}); err != nil || store.character.MaxMana != 60 {
					t.Fatalf("repeated equipment changed effective bonus: %v %+v", err, store.character)
				}
			}
		})
	}
}

func TestEquipTechniqueInfersSlotWhenUIOmitsIt(t *testing.T) {
	store := newActionStore()
	id := uuid.New()
	primary := "primary"
	store.character = &storage.CampaignCharacter{
		CampaignID: id, RealmKey: "mortal", CultivationRequired: 100,
		BaseMaxHealth: 100, MaxHealth: 100, Health: 100,
		BaseMaxMana: 20, MaxMana: 20, Mana: 20, LifespanDays: 10000,
	}
	store.techniques["occupied"] = &storage.CampaignTechnique{
		ID: uuid.New(), CampaignID: id, TechniqueKey: "occupied", Status: "active", EquippedSlot: &primary,
		LearnedLayer: 1,
	}
	store.definitions[storage.DefinitionTechnique+":occupied"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionTechnique, Key: "occupied",
		Content: storage.JSONMap{"allowed_slots": []any{"primary"}, "max_layer": int64(1), "layer_effects": []any{}},
	}
	store.techniques["multi"] = &storage.CampaignTechnique{
		ID: uuid.New(), CampaignID: id, TechniqueKey: "multi", Status: "active", LearnedLayer: 1,
	}
	store.definitions[storage.DefinitionTechnique+":multi"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionTechnique, Key: "multi",
		Content: storage.JSONMap{"allowed_slots": []any{"primary", "secondary"}, "max_layer": int64(1), "layer_effects": []any{}},
	}
	execution := campaign.ActionExecution{
		Campaign: storage.Campaign{ID: id}, Store: store, ActorType: "player",
	}
	if _, err := equipTechnique(context.Background(), execution, map[string]any{"technique_key": "multi"}); err != nil {
		t.Fatal(err)
	}
	row := store.techniques["multi"]
	if row.EquippedSlot == nil || *row.EquippedSlot != "secondary" {
		t.Fatalf("expected free secondary slot, got %+v", row.EquippedSlot)
	}
	if store.techniques["occupied"].EquippedSlot == nil {
		t.Fatal("occupied technique must remain equipped when a free slot exists")
	}
}
