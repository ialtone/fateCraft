package campaignactions

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/progressrules"
	"github.com/ialtone/fatecraft/internal/storage"
)

func TestStageRewardsUseOrderedFactsAndDoNotReplayAfterRewind(t *testing.T) {
	for _, route := range []string{storage.LLMRouteServer, storage.LLMRouteClient} {
		s, e := trainingFixture()
		e.Campaign.LLMRoute = route
		s.definitions[progressrules.DefinitionKind+":"+progressrules.DefinitionKey] = &storage.GameplayDefinition{Kind: progressrules.DefinitionKind, Key: progressrules.DefinitionKey, Content: storage.JSONMap{"stages": []any{
			map[string]any{"key": "established", "name": "立足", "points": 100, "min_realm_rank": 2, "quest_groups": []any{[]any{"healer", "sect"}}, "encounter_keys": []any{}},
			map[string]any{"key": "sea", "name": "星海", "points": 100, "min_realm_rank": 3, "quest_groups": []any{[]any{"sea-quest"}}, "encounter_keys": []any{"sea-beast"}},
			map[string]any{"key": "zero", "name": "零分阶段", "points": 0, "min_realm_rank": 3, "quest_groups": []any{[]any{"sea-quest"}}, "encounter_keys": []any{}},
		}, "outcome_keys": []any{"end"}, "total_points": 1000}}
		run := func() storage.CampaignActionOutcome {
			out := storage.CampaignActionOutcome{}
			if err := settleProgressRewards(context.Background(), e, &out); err != nil {
				t.Fatal(err)
			}
			return out
		}
		if len(run().Events) != 0 {
			t.Fatal("missing main quest paid")
		}
		s.quests["healer"] = &storage.CampaignQuest{QuestKey: "healer", Status: "completed"}
		if len(run().Events) != 1 {
			t.Fatal("first stage missing")
		}
		if len(run().Events) != 0 {
			t.Fatal("retry paid again")
		}
		s.quests["healer"].Status = "active"
		s.character.RealmRank = 3
		s.quests["sea-quest"] = &storage.CampaignQuest{QuestKey: "sea-quest", Status: "completed"}
		id := uuid.New()
		s.encounters[id] = &storage.CampaignEncounter{ID: id, EncounterKey: "sea-beast", Status: "won"}
		if len(run().Events) != 0 {
			t.Fatal("paid a later stage using invalidated prior facts")
		}
		if err := requireFinalProgress(e); err == nil {
			t.Fatal("ending without all facts accepted")
		}
		s.quests["healer"].Status = "completed"
		if len(run().Events) != 2 {
			t.Fatal("second stage and zero receipt missing")
		}
		if len(s.stageRewards) != 3 {
			t.Fatal("zero points were not recorded")
		}
		if err := requireFinalProgress(e); err != nil {
			t.Fatal(err)
		}
		want := int64(200)
		if route == storage.LLMRouteClient {
			want = 0
		}
		if s.stageGranted != want {
			t.Fatalf("route=%s points=%d", route, s.stageGranted)
		}
		s.character.Health = 0
		if len(run().Events) != 0 || s.stageGranted != want {
			t.Fatal("death replayed/reversed awards")
		}
	}
}
