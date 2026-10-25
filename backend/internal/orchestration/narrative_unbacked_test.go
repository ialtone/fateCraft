package orchestration

import (
	"testing"

	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
)

func TestValidateTurnNarrativeRejectsUnbackedClaims(t *testing.T) {
	text := "你运转功法，竟一举突破筑基成功！伤势痊愈，气血回满，还意外获得了一百块灵石和三枚聚气丹。"
	for _, ctx := range []*gameplay.TurnNarrativeContext{{}, {CampaignID: "00000000-0000-0000-0000-000000000001"}} {
		status, detail := ValidateTurnNarrative(text, ctx, BuildTurnEvidence(ctx, nil, nil, false), nil)
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
	if status, _ := ValidateTurnNarrative("你稳住心神，没有获得灵石。", &gameplay.TurnNarrativeContext{}, BuildTurnEvidence(&gameplay.TurnNarrativeContext{}, nil, nil, false), nil); status != "passed" {
		t.Fatalf("neutral text status = %s", status)
	}
	if status, _ := ValidateTurnNarrative(text, &gameplay.TurnNarrativeContext{}, BuildTurnEvidence(&gameplay.TurnNarrativeContext{}, nil, nil, true), nil); status != "passed" {
		t.Fatalf("committed turn status = %s", status)
	}
}

func TestValidateTurnNarrativeScopesEvidenceToClaimDomain(t *testing.T) {
	travel := gameplay.ActionResult{ActionID: "travel:1", ActionKey: "travel.move"}
	interaction := gameplay.ActionResult{
		ActionID: "talk:1", ActionKey: "relationship.record_interaction",
		Changes: []gameplay.FactChange{{
			Path: `relationships["老邢"].relationship.trust`, Before: 10, After: 12,
		}},
	}
	accept := gameplay.ActionResult{ActionID: "quest:1", ActionKey: "quest.accept"}

	cases := []struct {
		name   string
		text   string
		action gameplay.ActionResult
		want   string
	}{
		{"travel cannot prove loot", "你沿着山路前行，顺手获得三块灵石。", travel, "failed"},
		{"interaction backs trust", "老邢对你更信任了。", interaction, "passed"},
		{"travel cannot back trust", "老邢对你更信任了。", travel, "failed"},
		{"interaction cannot back another npc", "赵四对你更信任了。", interaction, "failed"},
		{"accept backs accepted quest", "你接下了这份委托。", accept, "passed"},
		{"accept cannot prove completion", "你接下的委托已经完成。", accept, "failed"},
		{"travel cannot back accepted quest", "你接下了这份委托。", travel, "failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			narrative := &gameplay.TurnNarrativeContext{CampaignID: "00000000-0000-0000-0000-000000000001", Actions: []gameplay.ActionResult{tc.action}}
			status, detail := ValidateTurnNarrative(tc.text, narrative, BuildTurnEvidence(narrative, nil, nil, true), nil)
			if status != tc.want {
				t.Fatalf("status = %s, want %s, detail = %#v", status, tc.want, detail)
			}
		})
	}
}

func TestValidateTurnNarrativeIgnoresSpeculativeClaims(t *testing.T) {
	for _, text := range []string{
		"据说你获得了三块灵石。",
		"如果获得一百块灵石，就能买下那本功法。",
		"你似乎更信任他了。",
	} {
		narrative := &gameplay.TurnNarrativeContext{CampaignID: "00000000-0000-0000-0000-000000000001"}
		if status, detail := ValidateTurnNarrative(text, narrative, BuildTurnEvidence(narrative, nil, nil, false), nil); status != "passed" {
			t.Fatalf("%q status = %s, detail = %#v", text, status, detail)
		}
	}
}
