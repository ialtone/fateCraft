package campaignactions

import (
	"context"
	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldactions"
	"github.com/ialtone/fatecraft/internal/storage"
	"testing"
)

func TestEarlyRescueAndInvestigationRequireRealEncounterAndEvidence(t *testing.T) {
	s, e := trainingFixture()
	s.definitions = map[string]*storage.GameplayDefinition{}
	s.techniques = map[string]*storage.CampaignTechnique{}
	for _, d := range readCultivationDefinitions(t) {
		copy := d
		s.definitions[d.Kind+":"+d.Key] = &copy
		if d.Kind == storage.DefinitionFaction {
			s.factions[d.Key] = &storage.CampaignFactionStanding{CampaignID: e.Campaign.ID, FactionKey: d.Key, Permissions: storage.JSONMap{}}
		}
	}
	s.character.RealmKey = "foundation"
	s.character.Health = 1000
	s.character.MaxHealth = 1000
	s.character.BaseMaxHealth = 1000
	s.character.Injuries = storage.JSONMap{}
	e.Campaign.CurrentLocationKey = "underground-river"
	e.ActorType = "player"
	e.Campaign.Status = storage.CampaignStatusActive
	e.Roll = func(context.Context, campaign.CampaignRollRequest) (*campaign.CampaignRollResult, error) {
		return &campaign.CampaignRollResult{Success: true, Level: "success", Rolled: 1}, nil
	}
	s.discovered["abandoned-cave"] = true
	s.discovered["underground-river"] = true
	npcID := uuid.New()
	s.npcs["lin-hunter"] = &storage.CampaignNPC{ID: npcID, InstanceKey: "lin-hunter", Status: "active"}
	s.relationships[npcID] = &storage.CampaignRelationship{NPCID: npcID, Trust: 8}
	for _, k := range []string{"cave-rescue", "sect-cave-investigation"} {
		s.quests[k] = &storage.CampaignQuest{CampaignID: e.Campaign.ID, QuestKey: k, QuestType: "quest", Status: "active", Progress: storage.JSONMap{}}
		if _, err := worldactions.SubmitQuest(context.Background(), e, map[string]any{"quest_key": k}); err != nil || s.quests[k].Status != "blocked" {
			t.Fatalf("arrival/trust without rescue was not blocked: %v", err)
		}
	}
	for _, item := range []string{"rescued-traveler-token", "missing-disciple-testimony", "torn-herb-basket"} {
		model := e
		model.ActorType = "model"
		if _, err := worldactions.GainItem(context.Background(), model, map[string]any{"item_key": item, "quantity": 1}); err == nil {
			t.Fatal("model can forge quest evidence")
		}
	}
	// Even a supplied item cannot replace this branch's encounter victory.
	s.stacks[stackKey("missing-disciple-testimony", "common")] = &storage.CampaignItemStack{ItemKey: "missing-disciple-testimony", QualityKey: "common", Quantity: 1}
	e.Campaign.CurrentLocationKey = "abandoned-cave"
	if _, err := worldactions.SubmitQuest(context.Background(), e, map[string]any{"quest_key": "sect-cave-investigation"}); err != nil || s.quests["sect-cave-investigation"].Status != "blocked" {
		t.Fatalf("item alone replaced encounter evidence: %v", err)
	}
	e.Campaign.CurrentLocationKey = "underground-river"
	delete(s.stacks, stackKey("missing-disciple-testimony", "common"))
	if _, err := startEncounter(context.Background(), e, map[string]any{"encounter_key": "underground-river-captive-rescue"}); err != nil {
		t.Fatal(err)
	}
	encounter, err := s.ActiveEncounter(e.Campaign.ID)
	if err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 12 && encounter.Status == "active"; round++ {
		if _, err := actEncounter(context.Background(), e, map[string]any{"encounter_id": encounter.ID.String(), "move": "attack"}); err != nil {
			t.Fatal(err)
		}
	}
	if encounter.Status != "won" {
		t.Fatalf("rescue encounter ended %s", encounter.Status)
	}
	for _, k := range []string{"cave-rescue", "sect-cave-investigation"} {
		if k == "sect-cave-investigation" {
			e.Campaign.CurrentLocationKey = "abandoned-cave"
		}
		if _, err := worldactions.SubmitQuest(context.Background(), e, map[string]any{"quest_key": k}); err != nil {
			t.Fatalf("%s with real evidence: %v", k, err)
		}
		if s.quests[k].Status != "completed" {
			t.Fatal("quest not completed")
		}
	}
	for _, item := range []string{"rescued-traveler-token", "missing-disciple-testimony"} {
		if _, err := s.ItemStack(e.Campaign.ID, item, "common"); err == nil {
			t.Fatal("evidence was not consumed")
		}
	}
}
