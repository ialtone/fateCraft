package storage

import (
	"strings"
	"testing"
)

func TestCampaignLifeChronicleSummarizesSuccess(t *testing.T) {
	state := &CampaignDomainState{
		Character: &CampaignCharacter{
			RealmKey: "great-ascension", StageKey: "perfected",
			AgeDays: 6823, LifespanDays: 7300000,
		},
		Definitions: map[string]GameplayDefinition{
			DefinitionRealm + ":great-ascension": {
				Kind: DefinitionRealm, Key: "great-ascension", Name: "大乘",
				Content: JSONMap{
					"stages":       []any{"early", "perfected"},
					"stage_labels": []any{"初期", "大圆满"},
				},
			},
			DefinitionLocation + ":celestial-frontier": {
				Kind: DefinitionLocation, Key: "celestial-frontier", Name: "天关前线",
			},
			DefinitionQuest + ":cave-rescue": {
				Kind: DefinitionQuest, Key: "cave-rescue", Name: "伏流中的求救声",
			},
		},
		StageRewards: []CampaignStageReward{{Name: "青河立足", GrantedPoints: 100}},
		Quests:       []CampaignQuest{{QuestKey: "cave-rescue", Status: "completed"}},
		Encounters:   []CampaignEncounter{{EncounterKey: "final-ascension-tribulation", Status: "won"}},
	}
	row := Campaign{Status: CampaignStatusCompleted, CurrentLocationKey: "celestial-frontier"}
	outcome := ChallengeOutcomeRule{OutcomeKey: "law_seeker", Title: "携律远行"}
	chronicle := campaignLifeChronicle(state, row, outcome, 300)
	joined := strings.Join(chronicle, "\n")
	for _, want := range []string{
		"境界：大乘 · 大圆满",
		"终点：天关前线",
		"寿元：6823 / 7300000 天",
		"阶段：青河立足（+100 分）",
		"主线：伏流中的求救声",
		"天劫：九霄劫云 · 渡过",
		"终局：携律远行",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("chronicle missing %q:\n%s", want, joined)
		}
	}
	if summary := campaignCompletionSummary(state, row, outcome, 300); !strings.Contains(summary, "历经 6823 天") {
		t.Fatalf("unexpected summary: %s", summary)
	}
}

func TestCampaignLifeChronicleSummarizesAbandon(t *testing.T) {
	state := &CampaignDomainState{
		Character: &CampaignCharacter{
			RealmKey: "qi-refining", StageKey: "layer-3",
			AgeDays: 5900, LifespanDays: 43800,
		},
		Definitions: map[string]GameplayDefinition{
			DefinitionRealm + ":qi-refining": {
				Kind: DefinitionRealm, Key: "qi-refining", Name: "练气",
				Content: JSONMap{
					"stages":       []any{"layer-1", "layer-3"},
					"stage_labels": []any{"一层", "三层"},
				},
			},
		},
	}
	row := Campaign{Status: CampaignStatusAbandoned}
	outcome := ChallengeOutcomeRule{OutcomeKey: "abandoned", Title: "中止求道"}
	summary := campaignCompletionSummary(state, row, outcome, 0)
	if !strings.Contains(summary, "主动终止本世") || !strings.Contains(summary, "练气 · 三层") {
		t.Fatalf("unexpected abandon summary: %s", summary)
	}
	chronicle := campaignLifeChronicle(state, row, outcome, 0)
	if !strings.Contains(strings.Join(chronicle, "\n"), "终局：中止求道") {
		t.Fatalf("unexpected abandon chronicle: %#v", chronicle)
	}
}

func TestCampaignLifeChronicleSummarizesFailureCause(t *testing.T) {
	state := &CampaignDomainState{
		Character: &CampaignCharacter{
			RealmKey: "qi-refining", StageKey: "layer-3",
			AgeDays: 5900, LifespanDays: 43800,
		},
	}
	row := Campaign{Status: CampaignStatusDead}
	perished := ChallengeOutcomeRule{OutcomeKey: "perished", Title: "身陨道消"}
	if summary := campaignCompletionSummary(state, row, perished, 0); !strings.Contains(summary, "气血耗尽") {
		t.Fatalf("unexpected death summary: %s", summary)
	}
	exhausted := ChallengeOutcomeRule{OutcomeKey: "lifespan_exhausted", Title: "寿元耗尽"}
	if summary := campaignCompletionSummary(state, row, exhausted, 0); !strings.Contains(summary, "寿元耗尽") || strings.Contains(summary, "气血耗尽") {
		t.Fatalf("unexpected lifespan summary: %s", summary)
	}
	if chronicle := campaignLifeChronicle(state, row, exhausted, 0); !strings.Contains(strings.Join(chronicle, "\n"), "终局：寿元耗尽") {
		t.Fatalf("unexpected death chronicle: %#v", chronicle)
	}
}
