package challenge

import (
	"testing"

	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/state"
	"github.com/ialtone/fatecraft/internal/storage"
)

func stageRewardsContent() gameplay.Content {
	return gameplay.Content{PlatformDefinitions: []gameplay.Definition{{
		Kind: gameplay.DefinitionKindChallengeProgressRewards,
		Content: map[string]any{
			"stages": []any{
				map[string]any{"key": "first", "name": "第一阶", "order": 10, "points": int64(25), "verifier_key": "core.conditions", "verifier_config": map[string]any{}},
				map[string]any{"key": "second", "name": "第二阶", "order": 20, "points": int64(30), "verifier_key": "core.conditions", "verifier_config": map[string]any{}},
			},
			"outcome_keys": []any{"win"},
			"stage_cap":    int64(55),
		},
	}}}
}

func TestStageGrantsUseRecordedProgressAndPinnedPoints(t *testing.T) {
	grants, err := stageGrantsForProgress(stageRewardsContent(), []state.StageProgressEntry{
		{StageKey: "second", SourceActionKey: "act.second"},
		{StageKey: "unknown", SourceActionKey: "act.unknown"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 1 {
		t.Fatalf("grants=%d", len(grants))
	}
	if grants[0].Key != "second" || grants[0].Name != "第二阶" || grants[0].Points != 30 || grants[0].ActionKey != "act.second" {
		t.Fatalf("grant used client or stale data: %#v", grants[0])
	}
}

func TestStageProgressViewsReportOnlyPresentedMilestones(t *testing.T) {
	reached := []gameplay.ProgressRewardStage{{Key: "first", Name: "第一阶", Order: 10, Points: 25}}
	views, pending := stageProgressViewsForReached(reached, storage.RewardsDisabledReasonClientRoute)
	if len(views) != 1 || views[0].StageKey != "first" || views[0].ConfiguredPoints != 25 {
		t.Fatalf("views=%#v", views)
	}
	if views[0].DisabledReason != storage.RewardsDisabledReasonClientRoute {
		t.Fatalf("client route reason missing: %#v", views[0])
	}
	if pending != 25 {
		t.Fatalf("pending=%d", pending)
	}
	if empty, zero := stageProgressViewsForReached(nil, ""); len(empty) != 0 || zero != 0 {
		t.Fatalf("empty progress=%#v %d", empty, zero)
	}
}
