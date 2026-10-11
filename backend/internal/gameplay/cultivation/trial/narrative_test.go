package trial

import (
	"encoding/json"
	"testing"

	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/kernel"
)

func TestPracticeAndBreakthroughNarrativeFactsFollowRealTransitions(t *testing.T) {
	content, initial := fixture(t)
	for _, tc := range []struct {
		name, action     string
		progress, random int64
		realm            int
		success          bool
	}{
		{"practice does not confer a realm", "trial.practice", 26, 0, 0, true},
		{"failed breakthrough keeps realm and costs", "trial.breakthrough", 100, 99, 0, false},
		{"successful breakthrough changes realm", "trial.breakthrough", 100, 0, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := ready(t, content, initial, 0)
			run = change(t, run, func(s *State) { s.Progress = tc.progress })
			before, bp, err := kernel.NarrativeView(Module{}, content, run)
			if err != nil {
				t.Fatal(err)
			}
			transition, err := kernel.Execute(Module{}, content, run, contract.RuntimeRequest{ActionKey: tc.action, Actor: "model", Arguments: map[string]any{}, Random: fixedRandom(tc.random)})
			if err != nil {
				t.Fatal(err)
			}
			after, ap, err := kernel.NarrativeView(Module{}, content, transition.State)
			if err != nil {
				t.Fatal(err)
			}
			feedback, err := contract.NarrativeFeedback(tc.action, run.Revision, transition.State.Revision, before, after, kernel.NarrativeFactPaths(Module{}), transition.Result, bp || ap)
			if err != nil {
				t.Fatal(err)
			}
			cfg, state, _ := read(content, transition.State)
			facts := feedback["current_facts"].(map[string]any)
			if state.Realm != tc.realm || facts["realm"] != cfg.Realms[tc.realm].Name || feedback["goal_succeeded"] != tc.success || facts["progress"] != json.Number(jsonInt(state.Progress)) {
				t.Fatalf("contradictory feedback: %v", feedback)
			}
			if !tc.success && (state.HP >= 100 || state.Stock["breakthrough-pill"] >= 100) {
				t.Fatal("failed breakthrough did not retain its real costs")
			}
		})
	}
	blocked := change(t, ready(t, content, initial, 0), func(s *State) { s.Progress = 52 })
	denied(t, content, blocked, "trial.breakthrough", "model", map[string]any{})
}

func jsonInt(value int64) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}
