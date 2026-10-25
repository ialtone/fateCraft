package campaignactions

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/progressrules"
	"github.com/ialtone/fatecraft/internal/storage"
)

func TestStageProgressRecordsDuringPlayAndSettlesOnce(t *testing.T) {
	for _, route := range []string{storage.LLMRouteServer, storage.LLMRouteClient} {
		s, e := trainingFixture()
		e.Campaign.LLMRoute = route
		s.definitions[progressrules.DefinitionKind+":"+progressrules.DefinitionKey] = &storage.GameplayDefinition{Kind: progressrules.DefinitionKind, Key: progressrules.DefinitionKey, Content: storage.JSONMap{"stages": []any{
			map[string]any{"key": "established", "name": "立足", "points": 100, "min_realm_rank": 2, "quest_groups": []any{[]any{"healer", "sect"}}, "encounter_keys": []any{}},
			map[string]any{"key": "sea", "name": "星海", "points": 100, "min_realm_rank": 3, "quest_groups": []any{[]any{"sea-quest"}}, "encounter_keys": []any{"sea-beast"}},
			map[string]any{"key": "zero", "name": "零分阶段", "points": 0, "min_realm_rank": 3, "quest_groups": []any{[]any{"sea-quest"}}, "encounter_keys": []any{}},
		}, "outcome_keys": []any{"end"}, "total_points": 1000}}
		record := func() storage.CampaignActionOutcome {
			out := storage.CampaignActionOutcome{Result: storage.JSONMap{}}
			if err := recordProgressRewards(context.Background(), e, &out); err != nil {
				t.Fatal(err)
			}
			return out
		}
		settle := func() storage.CampaignActionOutcome {
			out := storage.CampaignActionOutcome{Result: storage.JSONMap{}}
			if err := settleProgressRewards(context.Background(), e, &out); err != nil {
				t.Fatal(err)
			}
			return out
		}

		record()
		if len(s.stageProgress) != 0 || len(s.stageRewards) != 0 || s.stageGranted != 0 {
			t.Fatal("missing main quest recorded progress or paid")
		}
		s.quests["healer"] = &storage.CampaignQuest{QuestKey: "healer", Status: "completed"}
		first := record()
		if len(s.stageProgress) != 1 {
			t.Fatal("first stage progress missing")
		}
		view := first.Result["stage_progress"].([]storage.StageProgressView)[0]
		if (view.DisabledReason == storage.RewardsDisabledReasonClientRoute) != (route == storage.LLMRouteClient) {
			t.Fatalf("progress route label: %+v", view)
		}
		if len(s.stageRewards) != 0 || s.stageGranted != 0 {
			t.Fatal("in-run action paid a stage")
		}
		record()
		if len(s.stageProgress) != 1 {
			t.Fatal("retry recorded a duplicate")
		}
		s.quests["healer"].Status = "active"
		s.character.RealmRank = 3
		s.quests["sea-quest"] = &storage.CampaignQuest{QuestKey: "sea-quest", Status: "completed"}
		id := uuid.New()
		s.encounters[id] = &storage.CampaignEncounter{ID: id, EncounterKey: "sea-beast", Status: "won"}
		record()
		if len(s.stageProgress) != 1 {
			t.Fatal("recorded a later stage using invalidated prior facts")
		}
		if err := requireFinalProgress(e); err == nil {
			t.Fatal("ending without all facts accepted")
		}
		s.quests["healer"].Status = "completed"
		record()
		if len(s.stageProgress) != 3 {
			t.Fatal("ordered progress was not recorded")
		}
		if err := requireFinalProgress(e); err != nil {
			t.Fatal(err)
		}
		out := settle()
		if len(s.stageRewards) != 3 {
			t.Fatal("terminal settlement did not write every reached receipt")
		}
		want := int64(200)
		if route == storage.LLMRouteClient {
			want = 0
		}
		if s.stageGranted != want {
			t.Fatalf("route=%s points=%d", route, s.stageGranted)
		}
		if len(out.Result["stage_rewards"].([]storage.CampaignStageReward)) != 3 {
			t.Fatal("settlement result is missing receipts")
		}
		again := settle()
		if s.stageGranted != want || len(s.stageRewards) != 3 {
			t.Fatal("repeat settlement paid again")
		}
		if got := again.Result["stage_reward_points"].(int64); got != 0 {
			t.Fatalf("repeat settlement awarded %d", got)
		}
		s.character.Health = 0
		record()
		if len(s.stageProgress) != 3 || s.stageGranted != want {
			t.Fatal("death replayed or reversed progress")
		}
	}
}
