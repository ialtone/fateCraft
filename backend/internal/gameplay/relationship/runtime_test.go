package relationship

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/kernel"
)

func fixture(t *testing.T) Config {
	t.Helper()
	raw, err := os.ReadFile("../../../data/preset_packs/old-town-promise/definitions/scenario.json")
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
	content := contract.Content{Definitions: []contract.Definition{{Kind: DefinitionKind, Content: data}}}
	for _, key := range []string{"reunited", "honest_distance", "broken", "timeout", "leave"} {
		content.Outcomes = append(content.Outcomes, contract.Outcome{Key: key, Enabled: true})
	}
	if err := ValidateContent(content); err != nil {
		t.Fatal(err)
	}
	return content
}
func perform(t *testing.T, c contract.Content, s contract.RunState, key, arg, value string) contract.RunState {
	t.Helper()
	args := map[string]any{}
	if arg != "" {
		args[arg] = value
	}
	result, err := kernel.Execute(Module{}, c, s, contract.RuntimeRequest{ActionKey: "relationship." + key, Actor: "player", Arguments: args, Random: kernel.NewRandom(1, "test")})
	if err != nil {
		t.Fatalf("%s: %v", key, err)
	}
	return result.State
}
func start(t *testing.T, c contract.Content) contract.RunState {
	t.Helper()
	s, err := kernel.Initialize(Module{}, c, contract.InitialInput{})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func rejected(t *testing.T, c contract.Content, s contract.RunState, key, actor, arg, value string) {
	t.Helper()
	args := map[string]any{}
	if arg != "" {
		args[arg] = value
	}
	if _, err := kernel.Execute(Module{}, c, s, contract.RuntimeRequest{ActionKey: "relationship." + key, Actor: actor, Arguments: args, Random: kernel.NewRandom(1, "denied")}); err == nil {
		t.Fatalf("accepted %s", key)
	}
}

func TestBothRelationshipEndingsAndConfigVariant(t *testing.T) {
	for _, duration := range []int64{60, 90} {
		for _, ending := range []string{"reunited", "honest_distance"} {
			t.Run(ending, func(t *testing.T) {
				config := fixture(t)
				config.Promises[0].Duration = duration
				c := rules(t, config)
				s := start(t, c)
				rejected(t, c, s, "conclude", "player", "ending", ending)
				for _, person := range config.People {
					s = perform(t, c, s, "listen", "person", person.Key)
				}
				before, _ := json.Marshal(s)
				view, err := kernel.Project(Module{}, c, s, contract.VisibilityPlayer)
				if err != nil {
					t.Fatal(err)
				}
				if len(view["facts"].([]any)) != 3 {
					t.Fatal("known facts absent")
				}
				for range 2 {
					if _, err := kernel.Preview(Module{}, c, s, "player"); err != nil {
						t.Fatal(err)
					}
				}
				after, _ := json.Marshal(s)
				if string(before) != string(after) {
					t.Fatal("preview mutated state")
				}
				rejected(t, c, s, "listen", "player", "person", "keeper")
				if ending == "reunited" {
					s = perform(t, c, s, "choose", "choice", "support")
					rejected(t, c, s, "choose", "player", "choice", "refuse")
					s = perform(t, c, s, "fulfill", "promise", "bridge")
					rejected(t, c, s, "fulfill", "player", "promise", "bridge")
					s = perform(t, c, s, "choose", "choice", "share")
				} else {
					s = perform(t, c, s, "choose", "choice", "refuse")
					s = perform(t, c, s, "choose", "choice", "private")
				}
				raw, _ := json.Marshal(s)
				var restored contract.RunState
				if err := json.Unmarshal(raw, &restored); err != nil {
					t.Fatal(err)
				}
				if err := kernel.ValidateState(Module{}, c, restored); err != nil {
					t.Fatal(err)
				}
				rejected(t, c, restored, "conclude", "model", "ending", ending)
				s = perform(t, c, restored, "conclude", "ending", ending)
				if s.Status != contract.RunCompleted || s.OutcomeKey != ending {
					t.Fatalf("bad ending %+v", s)
				}
			})
		}
	}
}

func TestBreachInterruptsUnfinishedInteraction(t *testing.T) {
	config := fixture(t)
	config.Promises[0].Duration = 30
	c := rules(t, config)
	s := start(t, c)
	s = perform(t, c, s, "listen", "person", "keeper")
	s = perform(t, c, s, "choose", "choice", "support")
	s = perform(t, c, s, "wait", "", "")
	started := s.GameTime
	s = perform(t, c, s, "listen", "person", "teacher")
	facts, _ := ruledata.Decode[State](s.Data)
	if s.OutcomeKey != "broken" || s.GameTime != started+1 || len(facts.Listened) != 1 || facts.Trust["teacher"] != 0 {
		t.Fatalf("post-terminal interaction applied: %+v %+v", s, facts)
	}
	if err := kernel.ValidateState(Module{}, c, s); err != nil {
		t.Fatal(err)
	}
}

func TestSimultaneousPromiseExpiry(t *testing.T) {
	config := fixture(t)
	config.Promises = append(config.Promises, Promise{Key: "letter", Name: "送信", Duration: 45, Effort: 1})
	config.Events[1].Choices[0].Promise = "letter"
	c := rules(t, config)
	s := start(t, c)
	for _, p := range config.People {
		s = perform(t, c, s, "listen", "person", p.Key)
	}
	s = perform(t, c, s, "choose", "choice", "support")
	s = perform(t, c, s, "choose", "choice", "share")
	for s.Status == contract.RunActive {
		s = perform(t, c, s, "wait", "", "")
	}
	facts, _ := ruledata.Decode[State](s.Data)
	if facts.Promises["bridge"].Status != "broken" || facts.Promises["letter"].Status != "broken" {
		t.Fatal("simultaneous expirations incomplete")
	}
	if err := kernel.ValidateState(Module{}, c, s); err != nil {
		t.Fatal(err)
	}
}

func TestExpiredPromiseCanInvalidateAnotherInteraction(t *testing.T) {
	config := fixture(t)
	config.Promises[0].Duration = 30
	config.Promises = append(config.Promises, Promise{Key: "mail", Name: "送信", Duration: 60, Effort: 1})
	config.MaxBreaches = 2
	config.Events[1].Choices[0].Promise = "mail"
	config.Events[1].Requires.Trust = map[string]int64{"keeper": 20}
	c := rules(t, config)
	s := start(t, c)
	s = perform(t, c, s, "listen", "person", "keeper")
	s = perform(t, c, s, "listen", "person", "teacher")
	s = perform(t, c, s, "choose", "choice", "support")
	s = perform(t, c, s, "wait", "", "")
	s = perform(t, c, s, "choose", "choice", "share")
	facts, _ := ruledata.Decode[State](s.Data)
	if s.Status != contract.RunActive || facts.Promises["bridge"].Status != "broken" || facts.Choices["revelation"] != "" {
		t.Fatal("interaction ignored relationship change during its duration")
	}
}

func TestRelationshipDeadlineAndUnfinishedPromises(t *testing.T) {
	config := fixture(t)
	config.Endings[0].Requires = Conditions{Facts: []string{"supported"}}
	c := rules(t, config)
	s := start(t, c)
	s = perform(t, c, s, "listen", "person", "keeper")
	s = perform(t, c, s, "choose", "choice", "support")
	rejected(t, c, s, "conclude", "player", "ending", "reunited")
	s = start(t, c)
	for s.Status == contract.RunActive {
		s = perform(t, c, s, "wait", "", "")
	}
	if s.OutcomeKey != "timeout" {
		t.Fatal("deadline ignored")
	}
	s = start(t, c)
	s = perform(t, c, s, "leave", "", "")
	if s.OutcomeKey != "leave" {
		t.Fatal("leave ignored")
	}
}

func TestRelationshipRejectsImpossibleCausalHistories(t *testing.T) {
	c := fixture(t)
	c.Endings[0].Requires.Facts = []string{"refused", "shared"}
	if err := validate(c); err == nil {
		t.Fatal("promise requires mutually exclusive support choice")
	}
	c = fixture(t)
	c.Promises[0].Requires.Promises = []string{"bridge"}
	if err := validate(c); err == nil {
		t.Fatal("cyclic promise accepted")
	}
	c = fixture(t)
	c.Events[0].Requires.Facts = []string{"absent"}
	if err := validate(c); err == nil {
		t.Fatal("unknown fact accepted")
	}
}

func TestRelationshipSchemasAllowNegativeChanges(t *testing.T) {
	schema := Descriptor().DefinitionSchemas[DefinitionKind]
	properties := schema["properties"].(map[string]any)
	promise := properties["promises"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
	bounds := promise["breach_changes"].(map[string]any)["additionalProperties"].(map[string]any)
	if !reflect.DeepEqual(bounds["minimum"], int64(-40)) {
		t.Fatalf("negative deltas not editable: %+v", bounds)
	}
}
