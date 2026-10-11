package management

import (
	"encoding/json"
	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/kernel"
	"os"
	"strconv"
	"testing"
)

func fixture(t *testing.T) Config {
	t.Helper()
	raw, err := os.ReadFile("../../../data/preset_packs/mountain-inn/definitions/business.json")
	if err != nil {
		t.Fatal(err)
	}
	var definitions []struct {
		Content Config `json:"content"`
	}
	if err := json.Unmarshal(raw, &definitions); err != nil {
		t.Fatal(err)
	}
	return definitions[0].Content
}

func TestFacilityCannotDependOnItsOwnProduction(t *testing.T) {
	c := fixture(t)
	c.Facilities[0].BuildCost["meal"] = 1
	if err := validate(c); err == nil {
		t.Fatal("facility built from its unavailable output")
	}
}
func rules(t *testing.T, c Config) contract.Content {
	t.Helper()
	data, err := ruledata.Object(c)
	if err != nil {
		t.Fatal(err)
	}
	result := contract.Content{Definitions: []contract.Definition{{Kind: DefinitionKind, Content: data}}, Outcomes: []contract.Outcome{{Key: "prosperous", Enabled: true}, {Key: "bankrupt", Enabled: true}, {Key: "timeout", Enabled: true}, {Key: "closed", Enabled: true}}}
	if err := ValidateContent(result); err != nil {
		t.Fatal(err)
	}
	return result
}
func perform(t *testing.T, c contract.Content, s contract.RunState, key string, args map[string]any) contract.RunState {
	t.Helper()
	r, err := kernel.Execute(Module{}, c, s, contract.RuntimeRequest{ActionKey: "management." + key, Actor: "player", Arguments: args, Random: kernel.NewRandom(1, "test")})
	if err != nil {
		t.Fatalf("%s: %v", key, err)
	}
	return r.State
}
func TestInnProductionOrderUpgradeAndConfigVariant(t *testing.T) {
	for _, minutes := range []int64{60, 90} {
		t.Run(strconv.FormatInt(minutes, 10), func(t *testing.T) {
			c := fixture(t)
			c.Recipes[0].Minutes = minutes
			content := rules(t, c)
			s, err := kernel.Initialize(Module{}, content, contract.InitialInput{})
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range []string{"kitchen", "guestroom"} {
				s = perform(t, content, s, "build", map[string]any{"facility": f})
			}
			s = perform(t, content, s, "upgrade", map[string]any{"facility": "guestroom"})
			for _, o := range c.Orders {
				s = perform(t, content, s, "accept", map[string]any{"order": o.Key})
			}
			for range 3 {
				s = perform(t, content, s, "produce", map[string]any{"recipe": "meal"})
				s = perform(t, content, s, "advance", map[string]any{"hours": int64(2)})
			}
			for range 3 {
				s = perform(t, content, s, "produce", map[string]any{"recipe": "lodging"})
				s = perform(t, content, s, "advance", map[string]any{"hours": int64(3)})
			}
			for _, o := range c.Orders {
				s = perform(t, content, s, "deliver", map[string]any{"order": o.Key})
				if _, err := kernel.Execute(Module{}, content, s, contract.RuntimeRequest{ActionKey: "management.deliver", Actor: "player", Arguments: map[string]any{"order": o.Key}, Random: kernel.NewRandom(1, "duplicate")}); err == nil {
					t.Fatal("order paid twice")
				}
			}
			saved, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			var restored contract.RunState
			if err := json.Unmarshal(saved, &restored); err != nil {
				t.Fatal(err)
			}
			if err := kernel.ValidateState(Module{}, content, restored); err != nil {
				t.Fatal(err)
			}
			if _, err := kernel.Execute(Module{}, content, restored, contract.RuntimeRequest{ActionKey: "management.finish", Actor: "model", Random: kernel.NewRandom(1, "model")}); err == nil {
				t.Fatal("model finalized player decision")
			}
			s = perform(t, content, restored, "finish", nil)
			if s.Status != contract.RunCompleted || s.OutcomeKey != "prosperous" {
				t.Fatal("business did not complete")
			}
			facts, _ := ruledata.Decode[State](s.Data)
			if facts.Revenue != 160 || facts.Facilities["guestroom"] != 2 || len(facts.Jobs) != 0 {
				t.Fatalf("production/reward evidence inconsistent %+v", facts)
			}
		})
	}
}
func TestBusinessBreachIsNotInstantBankruptcy(t *testing.T) {
	c := fixture(t)
	c.Orders[0].Deadline = 60
	content := rules(t, c)
	s, _ := kernel.Initialize(Module{}, content, contract.InitialInput{})
	s = perform(t, content, s, "accept", map[string]any{"order": c.Orders[0].Key})
	s = perform(t, content, s, "advance", map[string]any{"hours": int64(2)})
	facts, _ := ruledata.Decode[State](s.Data)
	if s.Status != contract.RunActive || facts.Orders[c.Orders[0].Key] != "breached" || facts.Stock["cash"] != c.InitialStock["cash"]-c.Orders[0].Penalty {
		t.Fatalf("bad breach: %+v %+v", s, facts)
	}
}
func TestBusinessFailureAndNoFreeProduction(t *testing.T) {
	c := fixture(t)
	c.InitialStock = map[string]int64{"cash": 1}
	content := rules(t, c)
	s, _ := kernel.Initialize(Module{}, content, contract.InitialInput{})
	if _, err := kernel.Execute(Module{}, content, s, contract.RuntimeRequest{ActionKey: "management.produce", Actor: "player", Arguments: map[string]any{"recipe": "meal"}, Random: kernel.NewRandom(1, "free")}); err == nil {
		t.Fatal("production accepted without facility/materials")
	}
	s = perform(t, content, s, "advance", map[string]any{"hours": int64(24)})
	if s.OutcomeKey != "bankrupt" {
		t.Fatal("empty business with unpaid expenses did not fail")
	}
	c = fixture(t)
	c.InitialStock["cash"] = 100000
	content = rules(t, c)
	s, _ = kernel.Initialize(Module{}, content, contract.InitialInput{})
	for s.Status == contract.RunActive {
		s = perform(t, content, s, "advance", map[string]any{"hours": int64(24)})
	}
	if s.OutcomeKey != "timeout" {
		t.Fatal("business deadline ignored")
	}
}
