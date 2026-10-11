package stealth

import (
	"encoding/json"
	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/kernel"
	"os"
	"testing"
)

type fixedRandom bool

func (r fixedRandom) Intn(_ string, upper int64) (int64, error) {
	if r {
		return upper - 1, nil
	}
	return 0, nil
}
func fixture(t *testing.T) Config {
	t.Helper()
	raw, err := os.ReadFile("../../../data/preset_packs/night-blockade/definitions/scenario.json")
	if err != nil {
		t.Fatal(err)
	}
	var entries []struct {
		Content Config `json:"content"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatal(err)
	}
	return entries[0].Content
}

func TestStealthDoesNotFinishLootingAfterDeadline(t *testing.T) {
	c := fixture(t)
	content := rules(t, c)
	s, err := kernel.Initialize(Module{}, content, contract.InitialInput{Random: fixedRandom(true)})
	if err != nil {
		t.Fatal(err)
	}
	s = act(t, content, s, "observe", "")
	s = act(t, content, s, "sneak", "tunnel")
	s = act(t, content, s, "observe", "")
	s = act(t, content, s, "unlock", "archive")
	s = act(t, content, s, "sneak", "archive")
	s.GameTime = c.Deadline - 1
	s = act(t, content, s, "take", "")
	facts, _ := ruledata.Decode[State](s.Data)
	if s.OutcomeKey != "timeout" || s.GameTime != c.Deadline || facts.Target {
		t.Fatal("late looting created target evidence")
	}
}
func rules(t *testing.T, c Config) contract.Content {
	t.Helper()
	data, err := ruledata.Object(c)
	if err != nil {
		t.Fatal(err)
	}
	content := contract.Content{Definitions: []contract.Definition{{Kind: DefinitionKind, Content: data}}}
	for _, key := range []string{"escaped", "withdrawn", "captured", "timeout", "abandoned"} {
		content.Outcomes = append(content.Outcomes, contract.Outcome{Key: key, Enabled: true})
	}
	if err := ValidateContent(content); err != nil {
		t.Fatal(err)
	}
	return content
}
func act(t *testing.T, c contract.Content, s contract.RunState, key, destination string) contract.RunState {
	t.Helper()
	args := map[string]any{}
	if destination != "" {
		args["destination"] = destination
	}
	r, err := kernel.Execute(Module{}, c, s, contract.RuntimeRequest{ActionKey: "stealth." + key, Actor: "player", Arguments: args, Random: fixedRandom(true)})
	if err != nil {
		t.Fatalf("%s %s: %v", key, destination, err)
	}
	return r.State
}
func TestStealthRoutesRequireRealGateAndReturnToExit(t *testing.T) {
	for _, intelRoute := range []bool{false, true} {
		c := fixture(t)
		if intelRoute {
			c.Lockpicks = 0
			c.Locations[1].Minutes = 8
		}
		content := rules(t, c)
		s, err := kernel.Initialize(Module{}, content, contract.InitialInput{Random: fixedRandom(true)})
		if err != nil {
			t.Fatal(err)
		}
		s = act(t, content, s, "observe", "")
		route := "tunnel"
		if intelRoute {
			route = "courtyard"
		}
		s = act(t, content, s, "sneak", route)
		s = act(t, content, s, "observe", "")
		if _, err := kernel.Execute(Module{}, content, s, contract.RuntimeRequest{ActionKey: "stealth.sneak", Actor: "player", Arguments: map[string]any{"destination": "archive"}, Random: fixedRandom(true)}); err == nil {
			t.Fatal("locked gate bypassed")
		}
		if intelRoute {
			s = act(t, content, s, "sneak", "guardroom")
			s = act(t, content, s, "observe", "")
			s = act(t, content, s, "sneak", "courtyard")
		}
		s = act(t, content, s, "unlock", "archive")
		s = act(t, content, s, "sneak", "archive")
		s = act(t, content, s, "take", "")
		if s.Status != contract.RunActive {
			t.Fatal("taking goal auto-completed run")
		}
		raw, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatal(err)
		}
		s = act(t, content, s, "sneak", route)
		s = act(t, content, s, "sneak", "entry")
		if _, err := kernel.Execute(Module{}, content, s, contract.RuntimeRequest{ActionKey: "stealth.escape", Actor: "model", Random: fixedRandom(true)}); err == nil {
			t.Fatal("model made final player decision")
		}
		s = act(t, content, s, "escape", "")
		if s.OutcomeKey != "escaped" {
			t.Fatalf("route did not escape %+v", s)
		}
		facts, _ := ruledata.Decode[State](s.Data)
		if facts.Lockpicks != 0 || facts.Intel != intelRoute {
			t.Fatal("gate cost or clue evidence lost")
		}
	}
}
func TestStealthPursuitRecoveryCaptureAndTimeout(t *testing.T) {
	c := fixture(t)
	content := rules(t, c)
	s, _ := kernel.Initialize(Module{}, content, contract.InitialInput{Random: fixedRandom(true)})
	s = act(t, content, s, "observe", "")
	s = act(t, content, s, "sneak", "tunnel")
	facts, _ := ruledata.Decode[State](s.Data)
	facts.Alert = 65
	s.Data, _ = ruledata.Object(facts)
	s = act(t, content, s, "hide", "")
	facts, _ = ruledata.Decode[State](s.Data)
	if facts.Alert != 35 || s.Status != contract.RunActive {
		t.Fatal("pursuit had no escape opportunity")
	}
	s = act(t, content, s, "distract", "")
	facts, _ = ruledata.Decode[State](s.Data)
	if facts.Decoys != c.Decoys-1 || !facts.Distracted {
		t.Fatal("distraction cost absent")
	}
	facts.Alert = 90
	facts.Distracted = false
	s.Data, _ = ruledata.Object(facts)
	result, err := kernel.Execute(Module{}, content, s, contract.RuntimeRequest{ActionKey: "stealth.rush", Actor: "player", Arguments: map[string]any{"destination": "entry"}, Random: fixedRandom(false)})
	if err != nil {
		t.Fatal(err)
	}
	if result.State.OutcomeKey != "captured" || len(result.State.RandomEvidence) != 1 {
		t.Fatal("detection had no committed consequence/evidence")
	}
	s, _ = kernel.Initialize(Module{}, content, contract.InitialInput{Random: fixedRandom(true)})
	for s.Status == contract.RunActive {
		s = act(t, content, s, "observe", "")
	}
	if s.OutcomeKey != "timeout" {
		t.Fatal("deadline ignored")
	}
}
func TestStealthPreviewDoesNotRevealPatrolOrChangeState(t *testing.T) {
	content := rules(t, fixture(t))
	s, _ := kernel.Initialize(Module{}, content, contract.InitialInput{Random: fixedRandom(true)})
	before, _ := json.Marshal(s)
	for range 3 {
		if _, err := kernel.Preview(Module{}, content, s, "player"); err != nil {
			t.Fatal(err)
		}
		for _, audience := range []string{contract.AudiencePlayer, contract.AudienceModel, contract.AudienceInquiry} {
			view, err := kernel.Project(Module{}, content, s, audience)
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"phase", "patrol", "random_evidence"} {
				if _, exists := view[key]; exists {
					t.Fatalf("hidden %s leaked", key)
				}
			}
			if len(view["locations"].([]any)) != 1 {
				t.Fatal("hidden route leaked")
			}
		}
	}
	after, _ := json.Marshal(s)
	if string(before) != string(after) {
		t.Fatal("preview advanced patrol")
	}
}
