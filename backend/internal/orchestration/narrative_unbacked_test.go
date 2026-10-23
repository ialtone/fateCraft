package orchestration

import (
	"testing"

	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
)

func TestValidateTurnNarrativeRejectsUnbackedClaims(t *testing.T) {
	text := "你运转功法，竟一举突破筑基成功！伤势痊愈，气血回满，还意外获得了一百块灵石和三枚聚气丹。"
	for _, ctx := range []*gameplay.TurnNarrativeContext{{}, {CampaignID: "00000000-0000-0000-0000-000000000001"}} {
		status, detail := ValidateTurnNarrative(text, ctx, false)
		if status != "failed" {
			t.Fatalf("status = %s, detail = %#v", status, detail)
		}
		violations := detail["violations"].([]narrativeViolation)
		codes := map[string]bool{}
		for _, v := range violations {
			codes[v.Code] = true
		}
		for _, code := range []string{"unbacked_realm_change", "unbacked_healing", "unbacked_item_gain"} {
			if !codes[code] {
				t.Fatalf("missing %s in %#v", code, violations)
			}
		}
		if repaired := RepairNarrative(text+"夜色渐深。", detail); repaired != "夜色渐深。" {
			t.Fatalf("repaired = %q", repaired)
		}

	}
	if status, _ := ValidateTurnNarrative("你稳住心神，没有获得灵石。", &gameplay.TurnNarrativeContext{}, false); status != "passed" {
		t.Fatalf("neutral text status = %s", status)
	}
	if status, _ := ValidateTurnNarrative(text, &gameplay.TurnNarrativeContext{}, true); status != "passed" {
		t.Fatalf("committed turn status = %s", status)
	}
}
