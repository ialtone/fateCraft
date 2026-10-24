package orchestration

import (
	"testing"

	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
)

func TestUnbackedTimeClaimsCheckEachOccurrence(t *testing.T) {
	for _, text := range []string{
		"没有过去三日，但又过去四日。",
		"没有过去三日。过了四日。",
		"过去四日。并没有过去三日。",
	} {
		claims := unbackedTimeClaims(text)
		if len(claims) != 1 || claims[0].Claim != "四日" {
			t.Fatalf("%q: claims = %#v, want only the affirmed occurrence", text, claims)
		}
	}
	if claims := unbackedTimeClaims("你还剩余寿元 21535 天。"); len(claims) != 0 {
		t.Fatalf("remaining lifespan is not elapsed time: %#v", claims)
	}
}

func TestNarrativeGuardsApplyToEveryTurn(t *testing.T) {
	guards := []gameplay.NarrativeGuard{{Pattern: "永生不死", Message: "本玩法不允许永生"}}
	for _, committed := range []bool{false, true} {
		for _, narrative := range []*gameplay.TurnNarrativeContext{nil, {ChallengeMode: true}, ptrNarrative(testNarrativeContext())} {
			status, detail := ValidateTurnNarrative("你并非永生不死，但此刻确实永生不死。", narrative, committed, guards)
			if status != "failed" {
				t.Fatalf("guard skipped: committed=%v narrative=%#v detail=%#v", committed, narrative, detail)
			}
			violations, _ := detail["violations"].([]narrativeViolation)
			if len(violations) != 1 || violations[0].Code != "narrative_guard" {
				t.Fatalf("guard violations = %#v, want one affirmed guard match", violations)
			}
		}
	}
}

func TestTimeClaimsPreservePlansAndHistoricalReferences(t *testing.T) {
	for _, text := range []string{
		"准备需要三天。", "三天后再来找我。", "如果入冬，就带上棉衣。", "你回想曾经的三天。",
	} {
		if claims := unbackedTimeClaims(text); len(claims) != 0 {
			t.Fatalf("plan or history was treated as elapsed time: %q %#v", text, claims)
		}
	}
	for _, text := range []string{"3年后，你重返故乡。", "转眼已过三日。", "寿元还有五天。又过三日。"} {
		if claims := unbackedTimeClaims(text); len(claims) == 0 {
			t.Fatalf("elapsed time was exempted: %q", text)
		}
	}
}
