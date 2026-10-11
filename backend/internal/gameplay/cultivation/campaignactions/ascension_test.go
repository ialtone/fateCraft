package campaignactions

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/ascensionrules"
	"github.com/ialtone/fatecraft/internal/storage"
)

func ascensionFixture(t *testing.T) (*actionStoreStub, campaign.ActionExecution) {
	t.Helper()
	store, execution := encounterFixture(t, func(c storage.JSONMap) { c["opponent_max_health"] = int64(1); c["rewards"] = []any{} })
	execution.Campaign.Status = storage.CampaignStatusActive
	store.character.RealmKey, store.character.RealmRank, store.character.StageKey = "highest", 8, "perfected"
	store.character.CultivationRequired, store.character.CultivationProgress = 1000, 1000
	store.definitions[storage.DefinitionRealm+":highest"] = &storage.GameplayDefinition{Kind: storage.DefinitionRealm, Key: "highest", Content: storage.JSONMap{"rank": int64(8), "stages": []any{"early", "perfected"}, "cultivation_required": int64(1000), "post_realm_transition": "tribulation_ascension"}}
	store.definitions[ascensionrules.DefinitionKind+":"+ascensionrules.DefinitionKey] = &storage.GameplayDefinition{Kind: ascensionrules.DefinitionKind, Key: ascensionrules.DefinitionKey, Content: storage.JSONMap{
		"realm_key": "highest", "encounter_key": "mist-beast", "quest_groups": []any{[]any{"healer", "sect"}, []any{"rift"}}, "materials": []any{map[string]any{"item_key": "law", "quality": "common", "quantity": int64(3)}}, "outcome_keys": []any{"warden", "seeker"},
	}}
	store.quests["healer"] = &storage.CampaignQuest{CampaignID: execution.Campaign.ID, QuestKey: "healer", Status: "completed"}
	store.quests["rift"] = &storage.CampaignQuest{CampaignID: execution.Campaign.ID, QuestKey: "rift", Status: "completed"}
	store.stacks["law:common"] = &storage.CampaignItemStack{CampaignID: execution.Campaign.ID, ItemKey: "law", QualityKey: "common", Quantity: 9}
	for _, ending := range []string{"warden", "seeker"} {
		store.quests[ending] = &storage.CampaignQuest{CampaignID: execution.Campaign.ID, QuestKey: ending, QuestType: "quest", Status: "active", Progress: storage.JSONMap{}}
		store.definitions[storage.DefinitionQuest+":"+ending] = &storage.GameplayDefinition{Kind: storage.DefinitionQuest, Key: ending, Content: storage.JSONMap{
			"quest_type": "quest", "initial": false, "starting_status": "offered", "campaign_outcome_key": ending, "milestone_key": "finished." + ending,
			"objectives": []any{map[string]any{"key": "arrive", "type": "location", "location_key": execution.Campaign.CurrentLocationKey}},
		}}
		worldQuestFromDefinition(t, store.definitions[storage.DefinitionQuest+":"+ending])
	}
	return store, execution
}

func TestBothAscensionEndingsRequireCurrentRealmMasteryStoryAndRealTribulation(t *testing.T) {
	for _, ending := range []string{"warden", "seeker"} {
		for _, missing := range []string{"realm", "progress", "stage", "story", "trial", "model-choice"} {
			t.Run(ending+"/"+missing, func(t *testing.T) {
				store, execution := ascensionFixture(t)
				won := &storage.CampaignEncounter{ID: uuid.New(), CampaignID: execution.Campaign.ID, EncounterKey: "mist-beast", Status: "won"}
				store.encounters[won.ID] = won
				switch missing {
				case "realm":
					store.character.RealmKey = "lower"
				case "progress":
					store.character.CultivationProgress = 999
				case "stage":
					store.character.StageKey = "early"
				case "story":
					store.quests["rift"].Status = "active"
				case "trial":
					won.EncounterKey = "unrelated-victory"
				case "model-choice":
					execution.ActorType = "model"
				}
				if _, err := submitCultivationQuest(context.Background(), execution, map[string]any{"quest_key": ending}); err == nil {
					t.Fatal("premature ending accepted")
				}
				if store.quests[ending].Status != "active" || store.stacks["law:common"].Quantity != 9 {
					t.Fatal("rejected ending changed resources or quest")
				}
			})
		}
	}
}

func TestPlayerStartsAndSurvivesOneRealTribulationBeforeChoosingEitherEnding(t *testing.T) {
	for _, ending := range []string{"warden", "seeker"} {
		t.Run(ending, func(t *testing.T) {
			store, execution := ascensionFixture(t)
			ctx := context.Background()
			if _, err := startEncounter(ctx, execution, map[string]any{"encounter_key": "mist-beast"}); err == nil {
				t.Fatal("direct encounter bypassed player gate")
			}
			model := execution
			model.ActorType = "model"
			if _, err := beginAscension(ctx, model, nil); err == nil {
				t.Fatal("model started player's trial")
			}
			if _, err := beginAscension(ctx, execution, nil); err != nil {
				t.Fatal(err)
			}
			if store.stacks["law:common"].Quantity != 6 {
				t.Fatal("trial materials not consumed")
			}
			active, err := store.ActiveEncounter(execution.Campaign.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := beginAscension(ctx, execution, nil); err == nil || store.stacks["law:common"].Quantity != 6 {
				t.Fatal("duplicate trial spent more materials")
			}
			for i := 0; i < 10 && active.Status == "active"; i++ {
				execution.Campaign.Revision++
				out, err := actEncounter(ctx, execution, map[string]any{"encounter_id": active.ID.String(), "move": "attack"})
				if err != nil {
					t.Fatal(err)
				}
				execution.Campaign.GameTime += out.GameTimeDelta
			}
			if active.Status != "won" {
				t.Fatalf("trial not actually won: %+v", active)
			}
			out, err := submitCultivationQuest(ctx, execution, map[string]any{"quest_key": ending})
			if err != nil || out.Status == nil || *out.Status != storage.CampaignStatusCompleted || out.OutcomeKey == nil || *out.OutcomeKey != ending {
				t.Fatalf("valid ending rejected: %+v %v", out, err)
			}
			if _, err := beginAscension(ctx, execution, nil); err == nil {
				t.Fatal("won trial can be farmed")
			}
			if _, err := submitCultivationQuest(ctx, execution, map[string]any{"quest_key": ending}); err == nil {
				t.Fatal("completed quest settled twice")
			}
		})
	}
}

func TestTribulationRefusesInsufficientMaterialsWithoutStarting(t *testing.T) {
	store, execution := ascensionFixture(t)
	store.stacks["law:common"].Quantity = 2
	if _, err := beginAscension(context.Background(), execution, nil); err == nil {
		t.Fatal("missing preparation accepted")
	}
	if len(store.encounters) != 0 || store.stacks["law:common"].Quantity != 2 {
		t.Fatal("refused preparation changed state")
	}
}
