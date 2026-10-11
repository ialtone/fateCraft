package tomb

import (
	"encoding/json"
	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/kernel"
	"os"
	"strings"
	"testing"
)

type fixedRandom int64

func (r fixedRandom) Intn(_ string, upper int64) (int64, error) { return int64(r) % upper, nil }
func fixture(t *testing.T) Config {
	t.Helper()
	raw, err := os.ReadFile("../../../data/preset_packs/lost-chambers/definitions/scenario.json")
	if err != nil {
		t.Fatal(err)
	}
	var entries []struct {
		Content Config `json:"content"`
	}
	if err = json.Unmarshal(raw, &entries); err != nil {
		t.Fatal(err)
	}
	return entries[0].Content
}
func rules(t *testing.T, c Config) contract.Content {
	t.Helper()
	data, err := ruledata.Object(c)
	if err != nil {
		t.Fatal(err)
	}
	r := contract.Content{Definitions: []contract.Definition{{Kind: DefinitionKind, Content: data}}}
	for _, key := range []string{"recovered", "retreated", "perished", "darkness", "timeout", "abandoned"} {
		r.Outcomes = append(r.Outcomes, contract.Outcome{Key: key, Enabled: true})
	}
	if err := ValidateContent(r); err != nil {
		t.Fatal(err)
	}
	return r
}
func start(t *testing.T, c contract.Content) contract.RunState {
	t.Helper()
	s, err := kernel.Initialize(Module{}, c, contract.InitialInput{})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func act(t *testing.T, c contract.Content, s contract.RunState, key, arg, value string) contract.RunState {
	t.Helper()
	args := map[string]any{}
	if arg != "" {
		args[arg] = value
	}
	tr, err := kernel.Execute(Module{}, c, s, contract.RuntimeRequest{ActionKey: "tomb." + key, Actor: "player", Arguments: args, Random: fixedRandom(0)})
	if err != nil {
		t.Fatalf("%s %s: %v", key, value, err)
	}
	return tr.State
}
func denied(t *testing.T, c contract.Content, s contract.RunState, key, actor, arg, value string) {
	t.Helper()
	args := map[string]any{}
	if arg != "" {
		args[arg] = value
	}
	_, err := kernel.Execute(Module{}, c, s, contract.RuntimeRequest{ActionKey: "tomb." + key, Actor: actor, Arguments: args, Random: fixedRandom(0)})
	if err == nil {
		t.Fatalf("accepted %s", key)
	}
}
func TestClueRouteCapacityAndConfigVariant(t *testing.T) {
	for _, capacity := range []int64{5, 7} {
		c := fixture(t)
		c.World.Capacity = capacity
		content := rules(t, c)
		s := start(t, content)
		for _, audience := range []string{"player", "model", "inquiry"} {
			view, err := kernel.Project(Module{}, content, s, audience)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(view)
			if strings.Contains(string(raw), "东、南、西、北") {
				t.Fatal("hidden clue leaked")
			}
		}
		s = act(t, content, s, "move", "destination", "altar")
		denied(t, content, s, "inspect", "player", "", "")
		denied(t, content, s, "solve", "player", "trap", "stone-door")
		denied(t, content, s, "move", "player", "destination", "vault")
		s = act(t, content, s, "move", "destination", "mural")
		s = act(t, content, s, "inspect", "", "")
		denied(t, content, s, "inspect", "player", "", "")
		s = act(t, content, s, "move", "destination", "altar")
		s = act(t, content, s, "inspect", "", "")
		s = act(t, content, s, "solve", "trap", "stone-door")
		denied(t, content, s, "solve", "player", "trap", "stone-door")
		s = act(t, content, s, "take", "item", "beads")
		s = act(t, content, s, "move", "destination", "vault")
		if capacity == 5 {
			denied(t, content, s, "take", "player", "item", "seal")
			s = act(t, content, s, "drop", "item", "beads")
		}
		s = act(t, content, s, "take", "item", "seal")
		denied(t, content, s, "take", "player", "item", "seal")
		denied(t, content, s, "extract", "player", "", "")
		raw, _ := json.Marshal(s)
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatal(err)
		}
		if err := kernel.ValidateState(Module{}, content, s); err != nil {
			t.Fatal(err)
		}
		s = act(t, content, s, "move", "destination", "altar")
		s = act(t, content, s, "move", "destination", "entry")
		denied(t, content, s, "extract", "model", "", "")
		s = act(t, content, s, "extract", "", "")
		if s.OutcomeKey != "recovered" {
			t.Fatal("target did not return")
		}
		facts, _ := ruledata.Decode[State](s.Data)
		if facts.Journey.Secured["seal"] != 1 || len(facts.Journey.Carried) != 0 {
			t.Fatal("loot not secured")
		}
	}
}
func TestBypassInjuryRestAndFiniteSupplies(t *testing.T) {
	c := fixture(t)
	content := rules(t, c)
	s := start(t, content)
	s = act(t, content, s, "move", "destination", "altar")
	s = act(t, content, s, "bypass", "trap", "stone-door")
	facts, _ := ruledata.Decode[State](s.Data)
	if facts.Journey.Health != 35 || facts.Tools != 0 || len(s.RandomEvidence) != 1 {
		t.Fatal("bypass had no real cost/evidence")
	}
	s = act(t, content, s, "move", "destination", "mural")
	s = act(t, content, s, "rest", "", "")
	denied(t, content, s, "rest", "player", "", "")
	c = fixture(t)
	c.World.Supplies = 1
	content = rules(t, c)
	s = start(t, content)
	s = act(t, content, s, "move", "destination", "altar")
	denied(t, content, s, "move", "player", "destination", "entry")
	s = act(t, content, s, "abandon", "", "")
	if s.OutcomeKey != "abandoned" {
		t.Fatal("no exit for depleted run")
	}
}
func TestTombFailureAndInterruptedLoot(t *testing.T) {
	for _, outcome := range []string{"darkness", "timeout", "perished"} {
		c := fixture(t)
		if outcome == "darkness" {
			c.Light = 30
		}
		if outcome == "timeout" {
			c.World.Deadline = 60
			c.Light = 120
		}
		if outcome == "perished" {
			c.Traps[0].Damage = 100
		}
		content := rules(t, c)
		s := start(t, content)
		s = act(t, content, s, "move", "destination", "altar")
		s = act(t, content, s, "take", "item", "beads")
		if outcome == "perished" {
			s = act(t, content, s, "bypass", "trap", "stone-door")
		} else {
			for s.Status == contract.RunActive {
				s = act(t, content, s, "wait", "", "")
			}
		}
		facts, _ := ruledata.Decode[State](s.Data)
		if s.OutcomeKey != outcome || facts.Journey.Lost["beads"] != 2 || len(facts.Journey.Carried) != 0 {
			t.Fatalf("failure loss inconsistent %+v", s)
		}
	}
	c := fixture(t)
	c.Light = 30
	content := rules(t, c)
	s := start(t, content)
	s = act(t, content, s, "move", "destination", "altar")
	s = act(t, content, s, "bypass", "trap", "stone-door")
	s = act(t, content, s, "move", "destination", "vault")
	for s.GameTime < 25 {
		s = act(t, content, s, "wait", "", "")
	}
	s.GameTime = 29
	facts, _ := ruledata.Decode[State](s.Data)
	facts.Light = 1
	s.Data, _ = ruledata.Object(facts)
	s = act(t, content, s, "take", "item", "seal")
	facts, _ = ruledata.Decode[State](s.Data)
	if len(facts.Journey.Taken) != 0 || s.GameTime != 30 {
		t.Fatal("unfinished looting granted relic")
	}
}
func TestTombInvalidDependenciesAndPreviewPurity(t *testing.T) {
	c := fixture(t)
	c.Clues[0].Requires = []string{"sequence"}
	if err := validate(c); err == nil {
		t.Fatal("cyclic clues accepted")
	}
	c = fixture(t)
	c.Clues[0].Node = "vault"
	if err := validate(c); err == nil {
		t.Fatal("clue locked behind itself")
	}
	c = fixture(t)
	content := rules(t, c)
	s := start(t, content)
	before, _ := json.Marshal(s)
	for range 3 {
		if _, err := kernel.Preview(Module{}, content, s, "player"); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := json.Marshal(s)
	if string(before) != string(after) {
		t.Fatal("preview advanced run")
	}
}
