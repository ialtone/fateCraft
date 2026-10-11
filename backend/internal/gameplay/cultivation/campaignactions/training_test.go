package campaignactions

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/trainingrules"
	"github.com/ialtone/fatecraft/internal/storage"
)

func trainingFixture() (*actionStoreStub, campaign.ActionExecution) {
	s := newActionStore()
	id, version := uuid.New(), uuid.New()
	s.character = &storage.CampaignCharacter{CampaignID: id, RealmKey: "foundation", RealmRank: 2, StageKey: "early", Health: 10, MaxHealth: 100, BaseMaxHealth: 100, Mana: 100, MaxMana: 100, BaseMaxMana: 100, MaxSpiritualSense: 100, BaseMaxSpiritualSense: 100, CultivationRequired: 1000, LifespanDays: 30000, AgeDays: 5000, Injuries: storage.JSONMap{"wound": map[string]any{"category": "injury", "severity": int64(40)}, "poison": map[string]any{"category": "poison", "severity": int64(30)}}}
	slot := "primary"
	s.techniques["method"] = &storage.CampaignTechnique{CampaignID: id, TechniqueKey: "method", Status: "active", EquippedSlot: &slot, LearnedLayer: 1}
	s.definitions["technique:method"] = &storage.GameplayDefinition{Kind: "technique", Key: "method", Name: "疗伤功", Content: storage.JSONMap{"max_layer": int64(15), "layer_effects": []any{map[string]any{"layer": int64(1), "effects": map[string]any{"healing_power": int64(50)}}}}}
	s.definitions["realm:foundation"] = &storage.GameplayDefinition{Kind: "realm", Key: "foundation", Content: storage.JSONMap{"stages": []any{"early", "perfected"}}}
	s.definitions["item:stone"] = &storage.GameplayDefinition{Kind: "item", Key: "stone", Name: "灵石", Content: storage.JSONMap{"stackable": true}}
	s.stacks["stone:common"] = &storage.CampaignItemStack{CampaignID: id, ItemKey: "stone", QualityKey: "common", Quantity: 20}
	for _, mode := range []string{"retreat", "recovery"} {
		content := storage.JSONMap{"mode": mode, "min_realm_rank": 2, "max_realm_rank": 2, "days": int64(7), "location_keys": []any{"inn"}, "technique_keys": []any{}, "materials": []any{map[string]any{"item_key": "stone", "quantity": int64(2)}}}
		if mode == "retreat" {
			content["technique_keys"] = []any{"method"}
			content["cultivation_gain"] = int64(300)
			content["mastery_gain"] = int64(2500)
			content["mana_cost"] = int64(10)
		} else {
			content["health_restore_bps"] = int64(2000)
			content["mana_restore_bps"] = int64(7500)
			content["injury_relief"] = int64(25)
		}
		s.definitions[trainingrules.DefinitionKind+":"+mode] = &storage.GameplayDefinition{Kind: trainingrules.DefinitionKind, Key: mode, Name: mode, Content: content}
	}
	return s, campaign.ActionExecution{Campaign: storage.Campaign{ID: id, ChallengeVersionID: version, Status: storage.CampaignStatusActive, CurrentLocationKey: "inn"}, Store: s, ActorType: "player", MaxDueEventsPerTurn: 16}
}

func TestRetreatIsPaidBoundedAndReachesHighestMastery(t *testing.T) {
	s, e := trainingFixture()
	for i := 0; i < 4; i++ {
		out, err := retreat(context.Background(), e, map[string]any{"plan_key": "retreat"})
		if err != nil {
			t.Fatal(err)
		}
		e.Campaign.GameTime += out.GameTimeDelta
	}
	if s.character.CultivationProgress != 1000 || s.character.StageKey != "perfected" || s.character.AgeDays != 5028 || s.character.Mana != 60 || s.stacks["stone:common"].Quantity != 12 || s.techniques["method"].LearnedLayer != 15 {
		t.Fatalf("incorrect retreat settlement: %+v %+v", s.character, s.techniques["method"])
	}
	if _, err := retreat(context.Background(), e, map[string]any{"plan_key": "retreat"}); err == nil {
		t.Fatal("full cultivation allowed farming")
	}
}

func TestTrainingRejectsBeforeSpendingMaterialsOrTime(t *testing.T) {
	for _, reason := range []string{"location", "realm", "mana", "material", "unequipped", "lifespan", "deadline", "encounter", "mode", "arguments"} {
		t.Run(reason, func(t *testing.T) {
			s, e := trainingFixture()
			args := map[string]any{"plan_key": "retreat"}
			switch reason {
			case "location":
				e.Campaign.CurrentLocationKey = "wilderness"
			case "realm":
				s.character.RealmRank = 1
			case "mana":
				s.character.Mana = 0
			case "material":
				s.stacks["stone:common"].Quantity = 1
			case "unequipped":
				s.techniques["method"].EquippedSlot = nil
			case "lifespan":
				s.character.AgeDays = s.character.LifespanDays - 7
			case "deadline":
				due := int64(100)
				s.quests["promise"] = &storage.CampaignQuest{QuestKey: "promise", Status: "active", DueGameTime: &due}
				id := uuid.New()
				s.scheduled[id] = &storage.CampaignScheduledEvent{ID: id, EventKind: "quest_deadline", TemplateKey: "promise", DueGameTime: due, Status: "pending"}
			case "encounter":
				id := uuid.New()
				s.encounters[id] = &storage.CampaignEncounter{ID: id, Status: "active"}
			case "mode":
				args["plan_key"] = "recovery"
			case "arguments":
				args["cultivation_gain"] = 100000
			}
			age, mana, cost := s.character.AgeDays, s.character.Mana, s.stacks["stone:common"].Quantity
			if _, err := retreat(context.Background(), e, args); err == nil {
				t.Fatal("unqualified retreat accepted")
			}
			if s.character.AgeDays != age || s.character.Mana != mana || s.stacks["stone:common"].Quantity != cost || s.character.CultivationProgress != 0 {
				t.Fatal("rejected retreat changed state")
			}
		})
	}
}

func TestRecoveryConsumesTimeUsesHealingBonusAndTreatsOnlyInjuries(t *testing.T) {
	s, e := trainingFixture()
	s.character.Mana = 0
	_, err := recoverCharacter(context.Background(), e, map[string]any{"plan_key": "recovery"})
	if err != nil {
		t.Fatal(err)
	}
	if s.character.Health != 40 || s.character.Mana != 75 || s.character.AgeDays != 5007 || s.stacks["stone:common"].Quantity != 18 {
		t.Fatalf("recovery not applied: %+v", s.character)
	}
	if s.character.Injuries["wound"].(map[string]any)["severity"] != int64(15) || s.character.Injuries["poison"].(map[string]any)["severity"] != int64(30) {
		t.Fatal("recovery changed unrelated condition")
	}
}

func TestHealingPowerIsConsumedByItemsAndRemovedOnUnequip(t *testing.T) {
	for _, equipped := range []bool{true, false} {
		s, e := trainingFixture()
		if !equipped {
			s.techniques["method"].EquippedSlot = nil
		}
		s.definitions["item:medicine"] = &storage.GameplayDefinition{Kind: "item", Key: "medicine", Content: storage.JSONMap{"consumable": true, "effects": map[string]any{"health": int64(20)}}}
		s.stacks["medicine:common"] = &storage.CampaignItemStack{ItemKey: "medicine", QualityKey: "common", Quantity: 1}
		if _, err := useItem(context.Background(), e, map[string]any{"item_key": "medicine", "quantity": 1}); err != nil {
			t.Fatal(err)
		}
		want := int64(30)
		if equipped {
			want = 40
		}
		if s.character.Health != want {
			t.Fatalf("equipped=%v health=%d", equipped, s.character.Health)
		}
	}
}

func TestModelCannotBypassPaidRecoveryWithPositiveVitalDelta(t *testing.T) {
	for _, key := range []string{"health_delta", "mana_delta", "spiritual_sense_delta"} {
		s, e := trainingFixture()
		e.ActorType = "model"
		before := s.character.Health
		if _, err := adjustVitals(context.Background(), e, map[string]any{key: 1, "cause_key": "rest"}); err == nil {
			t.Fatal("free model recovery accepted")
		}
		if s.character.Health != before {
			t.Fatal("rejected recovery modified state")
		}
	}
}
