package orchestration

import (
	"strings"
	"testing"

	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
)

func testNarrativeContext() gameplay.TurnNarrativeContext {
	return gameplay.TurnNarrativeContext{
		CampaignID:   "00000000-0000-0000-0000-000000000001",
		RevisionFrom: 6, RevisionTo: 7, StateHash: "hash",
		Actions: []gameplay.ActionResult{{
			SchemaVersion: 1, ActionID: "action-1", ActionKey: "cultivation.breakthrough",
			Revision: 7, GoalStatus: "failed",
			Changes: []gameplay.FactChange{
				{Path: "character.cultivation_progress", Before: 90, After: 100, Kind: "number"},
			},
			CurrentFacts: map[string]any{
				"character.realm_name":    "炼气",
				"character.stage_name":    "九层",
				"character.health":        30,
				"character.max_health":    100,
				"character.injuries":      map[string]any{"left_arm": "骨裂"},
				"character.lifespan_days": 10000,
			},
			Preserved: []gameplay.FactRef{
				{Path: "character.realm_name"},
				{Path: "character.stage_name"},
				{Path: "character.health"},
				{Path: "character.injuries"},
				{Path: "character.lifespan_days"},
			},
		}},
	}
}

func TestValidateNarrativeRejectsContradictoryClaims(t *testing.T) {
	text := "你突破成功，境界提升到筑基。伤势痊愈，气血回满，还获得三块灵石。"
	status, detail := ValidateNarrative(text, ptrNarrative(testNarrativeContext()))
	if status != "failed" {
		t.Fatalf("status = %q, want failed; detail=%#v", status, detail)
	}
	violations, _ := detail["violations"].([]narrativeViolation)
	if len(violations) < 4 {
		t.Fatalf("expected multiple violations, got %#v", detail["violations"])
	}
}

func TestValidateNarrativeAllowsNegationAndSafeText(t *testing.T) {
	for _, text := range []string{
		"你尝试突破，但灵气不足，境界并未突破成功，伤势仍在。",
		"你收敛气息，继续打坐，修为略有增长。",
	} {
		status, detail := ValidateNarrative(text, ptrNarrative(testNarrativeContext()))
		if status != "passed" {
			t.Fatalf("text %q status = %q, detail=%#v", text, status, detail)
		}
	}
}

func TestValidateNarrativeRejectsNumericMismatch(t *testing.T) {
	status, detail := ValidateNarrative("你查看面板：气血 80/100。", ptrNarrative(testNarrativeContext()))
	if status != "failed" {
		t.Fatalf("status = %q, want failed; detail=%#v", status, detail)
	}
}

func TestRenderNarrativeFallbackIsSafe(t *testing.T) {
	text := renderNarrativeFallback(ptrNarrative(testNarrativeContext()))
	if !strings.Contains(text, "未能达成目标") {
		t.Fatalf("fallback = %q", text)
	}
	if strings.Contains(text, "突破成功") || strings.Contains(text, "伤势痊愈") {
		t.Fatalf("fallback contains forbidden claim: %q", text)
	}
}

func TestRepairNarrativeRemovesContradictorySentence(t *testing.T) {
	context := testNarrativeContext()
	status, detail := ValidateNarrative("你突破成功。你盘膝而坐，气息仍未平息。", &context)
	if status != "failed" {
		t.Fatalf("status = %q, want failed", status)
	}
	repaired := RepairNarrative("你突破成功。你盘膝而坐，气息仍未平息。", detail)
	if repaired != "你盘膝而坐，气息仍未平息。" {
		t.Fatalf("repaired = %q", repaired)
	}
	if status, _ := ValidateNarrative(repaired, &context); status != "passed" {
		t.Fatalf("repaired text still fails: %q", repaired)
	}
}

func ptrNarrative(value gameplay.TurnNarrativeContext) *gameplay.TurnNarrativeContext {
	return &value
}
