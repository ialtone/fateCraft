package dungeon

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
	raw, err := os.ReadFile("../../../data/preset_packs/underground-watchtower/definitions/scenario.json")
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
	for _, key := range []string{"cleared", "retreated", "defeated", "timeout", "abandoned"} {
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
	tr, err := kernel.Execute(Module{}, c, s, contract.RuntimeRequest{ActionKey: "dungeon." + key, Actor: "player", Arguments: args, Random: fixedRandom(0)})
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
	_, err := kernel.Execute(Module{}, c, s, contract.RuntimeRequest{ActionKey: "dungeon." + key, Actor: actor, Arguments: args, Random: fixedRandom(0)})
	if err == nil {
		t.Fatalf("accepted %s", key)
	}
}
func winFight(t *testing.T, c contract.Content, s contract.RunState) contract.RunState {
	t.Helper()
	config, _ := ruledata.Singleton[Config](c, DefinitionKind)
	for range 40 {
		facts, _ := ruledata.Decode[State](s.Data)
		if !engaged(config, facts) {
			return s
		}
		s = act(t, c, s, "fight", "style", "guarded")
	}
	t.Fatal("fight did not end")
	return s
}
func TestDungeonTwoRoutesAndConfigVariant(t *testing.T) {
	for _, route := range []string{"barracks", "waterway"} {
		c := fixture(t)
		if route == "waterway" {
			c.Power = 10
		}
		content := rules(t, c)
		s := start(t, content)
		for _, audience := range []string{"player", "model", "inquiry"} {
			view, err := kernel.Project(Module{}, content, s, audience)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(view)
			if strings.Contains(string(raw), "哨塔统领") {
				t.Fatal("hidden enemy leaked")
			}
		}
		s = act(t, content, s, "move", "destination", route)
		denied(t, content, s, "move", "player", "destination", "camp")
		s = winFight(t, content, s)
		denied(t, content, s, "fight", "player", "style", "guarded")
		s = act(t, content, s, "move", "destination", "camp")
		s = act(t, content, s, "rest", "", "")
		denied(t, content, s, "rest", "player", "", "")
		s = act(t, content, s, "move", "destination", "command")
		denied(t, content, s, "take", "player", "item", "token")
		s = winFight(t, content, s)
		s = act(t, content, s, "take", "item", "token")
		denied(t, content, s, "take", "player", "item", "token")
		denied(t, content, s, "extract", "player", "", "")
		raw, _ := json.Marshal(s)
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatal(err)
		}
		if err := kernel.ValidateState(Module{}, content, s); err != nil {
			t.Fatal(err)
		}
		s = act(t, content, s, "move", "destination", "camp")
		s = act(t, content, s, "move", "destination", route)
		s = act(t, content, s, "move", "destination", "entry")
		denied(t, content, s, "extract", "model", "", "")
		s = act(t, content, s, "extract", "", "")
		if s.OutcomeKey != "cleared" {
			t.Fatal("dungeon incomplete")
		}
	}
}
func TestDungeonFleePersistsEnemyAndLosesCarriedLoot(t *testing.T) {
	c := fixture(t)
	content := rules(t, c)
	s := start(t, content)
	s = act(t, content, s, "move", "destination", "barracks")
	s = winFight(t, content, s)
	s = act(t, content, s, "take", "item", "supplies-cache")
	s = act(t, content, s, "move", "destination", "camp")
	s = act(t, content, s, "move", "destination", "command")
	s = act(t, content, s, "fight", "style", "guarded")
	if len(s.RandomEvidence) != 2 {
		t.Fatal("missing combat draws")
	}
	facts, _ := ruledata.Decode[State](s.Data)
	hp := facts.Enemies["commander"]
	s = act(t, content, s, "flee", "", "")
	facts, _ = ruledata.Decode[State](s.Data)
	if facts.Journey.Carried["supplies-cache"] != 2 || facts.Journey.Lost["supplies-cache"] != 2 {
		t.Fatal("flee loss not applied")
	}
	s = act(t, content, s, "move", "destination", "command")
	facts, _ = ruledata.Decode[State](s.Data)
	if facts.Enemies["commander"] != hp {
		t.Fatal("enemy reset on return")
	}
	s = act(t, content, s, "flee", "", "")
	s = act(t, content, s, "move", "destination", "barracks")
	denied(t, content, s, "take", "player", "item", "supplies-cache")
	s = act(t, content, s, "move", "destination", "entry")
	s = act(t, content, s, "retreat", "", "")
	if s.OutcomeKey != "retreated" {
		t.Fatal("retreat failed")
	}
}
func TestDungeonFiniteHealingDeathAndTimeout(t *testing.T) {
	c := fixture(t)
	c.Medicine = 1
	content := rules(t, c)
	s := start(t, content)
	s = act(t, content, s, "move", "destination", "barracks")
	s = winFight(t, content, s)
	s = act(t, content, s, "heal", "", "")
	denied(t, content, s, "heal", "player", "", "")
	for _, outcome := range []string{"defeated", "timeout"} {
		c = fixture(t)
		if outcome == "defeated" {
			c.Enemies[0].Attack = 100
		}
		content = rules(t, c)
		s = start(t, content)
		if outcome == "defeated" {
			s = act(t, content, s, "move", "destination", "barracks")
			s = act(t, content, s, "fight", "style", "assault")
		} else {
			for s.Status == contract.RunActive {
				s = act(t, content, s, "wait", "", "")
			}
		}
		if s.OutcomeKey != outcome {
			t.Fatalf("expected %s: %+v", outcome, s)
		}
	}
	c = fixture(t)
	c.World.Supplies = 1
	content = rules(t, c)
	s = start(t, content)
	s = act(t, content, s, "move", "destination", "barracks")
	denied(t, content, s, "fight", "player", "style", "assault")
	denied(t, content, s, "flee", "player", "", "")
	s = act(t, content, s, "abandon", "", "")
	if s.OutcomeKey != "abandoned" {
		t.Fatal("abandon failed")
	}
}
func TestDungeonRejectsInvalidRulesAndPreviewMutation(t *testing.T) {
	c := fixture(t)
	c.Boss = "absent"
	if err := validate(c); err == nil {
		t.Fatal("invalid boss accepted")
	}
	c = fixture(t)
	c.World.Nodes[1].Links = []string{"entry", "absent"}
	if err := validate(c); err == nil {
		t.Fatal("unknown route accepted")
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
		t.Fatal("preview changed state")
	}
}
