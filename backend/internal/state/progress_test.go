package state

import "testing"

func TestReadStageProgressReadsReachedMilestones(t *testing.T) {
	progress := ReadStageProgress(map[string]any{
		PlatformStateKey: map[string]any{
			"stage_progress": []any{
				map[string]any{
					"stage_key":           "first",
					"source_turn_id":      "turn-1",
					"source_branch_epoch": float64(3),
					"source_action_key":   "trial.breakthrough",
					"reached_at":          "2026-10-25T08:00:00Z",
				},
				map[string]any{"stage_key": ""},
				"not-a-document",
			},
		},
	})
	if len(progress) != 1 {
		t.Fatalf("progress=%#v", progress)
	}
	entry := progress[0]
	if entry.StageKey != "first" || entry.SourceTurnID != "turn-1" ||
		entry.SourceBranchEpoch != 3 || entry.SourceActionKey != "trial.breakthrough" {
		t.Fatalf("unexpected entry: %#v", entry)
	}
	if entry.ReachedAt.IsZero() {
		t.Fatal("reached_at was not parsed")
	}
}

func TestReadStageProgressIgnoresMissingOrMalformedRegion(t *testing.T) {
	for _, state := range []map[string]any{
		nil,
		{},
		{PlatformStateKey: map[string]any{"stage_progress": "forged"}},
		{PlatformStateKey: "forged"},
	} {
		if progress := ReadStageProgress(state); len(progress) != 0 {
			t.Fatalf("malformed state accepted: %#v", state)
		}
	}
}

func TestReservedPlatformStateCopiesWithoutAliasing(t *testing.T) {
	original := map[string]any{
		PlatformStateKey: map[string]any{
			"stage_progress": []any{map[string]any{"stage_key": "first"}},
		},
	}
	reserved := ReservedPlatformState(original)
	if reserved == nil {
		t.Fatal("platform region missing")
	}
	reserved["forged"] = true
	region := original[PlatformStateKey].(map[string]any)
	if _, leaked := region["forged"]; leaked {
		t.Fatal("reserved copy aliased the source state")
	}
	if ReservedPlatformState(nil) != nil {
		t.Fatal("nil state should not produce a region")
	}
	if ReservedPlatformState(map[string]any{"profile": map[string]any{}}) != nil {
		t.Fatal("state without a platform region should not produce one")
	}
}
