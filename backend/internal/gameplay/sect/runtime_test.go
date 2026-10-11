package sect

import (
	"encoding/json"
	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/kernel"
	"os"
	"testing"
)

type fixedRandom int64

func (r fixedRandom) Intn(_ string, upper int64) (int64, error) { return int64(r) % upper, nil }
func fixture(t *testing.T) Config {
	t.Helper()
	raw, err := os.ReadFile("../../../data/preset_packs/qinglan-sect/definitions/scenario.json")
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
	for _, key := range []string{"rebuilt", "fallen", "dispersed", "timeout", "dissolved"} {
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
func perform(t *testing.T, c contract.Content, s contract.RunState, key, arg string, value any, random fixedRandom) contract.RunState {
	t.Helper()
	args := map[string]any{}
	if arg != "" {
		args[arg] = value
	}
	tr, err := kernel.Execute(Module{}, c, s, contract.RuntimeRequest{ActionKey: "sect." + key, Actor: "player", Arguments: args, Random: random})
	if err != nil {
		t.Fatalf("%s %v: %v", key, value, err)
	}
	return tr.State
}
func act(t *testing.T, c contract.Content, s contract.RunState, key, arg string, value any) contract.RunState {
	t.Helper()
	return perform(t, c, s, key, arg, value, 0)
}
func denied(t *testing.T, c contract.Content, s contract.RunState, key, actor, arg string, value any) {
	t.Helper()
	args := map[string]any{}
	if arg != "" {
		args[arg] = value
	}
	if _, err := kernel.Execute(Module{}, c, s, contract.RuntimeRequest{ActionKey: "sect." + key, Actor: actor, Arguments: args, Random: fixedRandom(0)}); err == nil {
		t.Fatalf("accepted %s %v", key, value)
	}
}

func TestSectRebuildAndConfigVariant(t *testing.T) {
	for _, duration := range []int64{240, 320} {
		config := fixture(t)
		config.Recipes[0].Minutes = duration
		c := rules(t, config)
		s := start(t, c)
		denied(t, c, s, "finish", "player", "", nil)
		for _, f := range []string{"hall", "workshop"} {
			s = act(t, c, s, "build", "facility", f)
		}
		s = act(t, c, s, "recruit", "disciple", "qinghe")
		denied(t, c, s, "recruit", "player", "disciple", "qinghe")
		s = act(t, c, s, "teach", "disciple", "shuning")
		denied(t, c, s, "teach", "player", "disciple", "shuning")
		s = act(t, c, s, "train", "disciple", "shuning")
		denied(t, c, s, "guard", "player", "disciple", "shuning")
		s = act(t, c, s, "produce", "plan", "shiyan/stone")
		denied(t, c, s, "dispatch", "player", "plan", "shiyan/recover")
		s = act(t, c, s, "dispatch", "plan", "qinghe/recover")
		denied(t, c, s, "train", "player", "disciple", "qinghe")
		s = act(t, c, s, "guard", "disciple", "lufeng")
		denied(t, c, s, "produce", "player", "plan", "lufeng/stone")
		raw, _ := json.Marshal(s)
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatal(err)
		}
		if err := kernel.ValidateState(Module{}, c, s); err != nil {
			t.Fatal(err)
		}
		s = act(t, c, s, "advance", "hours", int64(4))
		s = act(t, c, s, "upgrade", "facility", "hall")
		denied(t, c, s, "upgrade", "player", "facility", "hall")
		s = act(t, c, s, "advance", "hours", int64(20))
		facts, _ := ruledata.Decode[State](s.Data)
		if facts.Crisis != "defended" || facts.Disciples["shuning"].Level != 2 || !facts.Disciples["shuning"].Learned || facts.Missions["recover"] != 1 || facts.UpkeepDays != 1 || len(facts.Jobs) != 0 {
			t.Fatalf("rebuild evidence incomplete %+v", facts)
		}
		denied(t, c, s, "finish", "model", "", nil)
		s = act(t, c, s, "finish", "", nil)
		if s.OutcomeKey != "rebuilt" {
			t.Fatal("sect did not rebuild")
		}
	}
}

func TestSectMissionFailureIsRecoverableAndCannotDoubleAssign(t *testing.T) {
	c := rules(t, fixture(t))
	s := start(t, c)
	s = act(t, c, s, "dispatch", "plan", "shiyan/recover")
	denied(t, c, s, "guard", "player", "disciple", "shiyan")
	denied(t, c, s, "dispatch", "player", "plan", "shiyan/recover")
	s = perform(t, c, s, "advance", "hours", int64(8), 99)
	facts, _ := ruledata.Decode[State](s.Data)
	if s.Status != contract.RunActive || facts.Failures != 1 || len(facts.Missions) != 0 || facts.Disciples["shiyan"].InjuredUntil != 600 || len(s.RandomEvidence) != 1 {
		t.Fatalf("failed mission behavior invalid %+v", facts)
	}
	denied(t, c, s, "dispatch", "player", "plan", "shiyan/recover")
	s = act(t, c, s, "advance", "hours", int64(2))
	s = act(t, c, s, "guard", "disciple", "shiyan")
	s = act(t, c, s, "recall", "disciple", "shiyan")
	s = act(t, c, s, "dispatch", "plan", "shiyan/recover")
}

func TestSectCrisisRequiresActualDefenders(t *testing.T) {
	config := fixture(t)
	c := rules(t, config)
	s := start(t, c)
	s = act(t, c, s, "build", "facility", "hall")
	s = act(t, c, s, "dispatch", "plan", "lufeng/recover")
	s = act(t, c, s, "advance", "hours", int64(24))
	if s.OutcomeKey != "fallen" {
		t.Fatal("unassigned returned disciple counted as defender")
	}
}

func TestSectUpkeepDeparturesDeadlineAndDissolve(t *testing.T) {
	config := fixture(t)
	config.InitialStock["food"] = 1
	config.CrisisTime = 7 * 1440
	config.Deadline = 8 * 1440
	c := rules(t, config)
	s := start(t, c)
	for s.Status == contract.RunActive {
		s = act(t, c, s, "advance", "hours", int64(24))
	}
	facts, _ := ruledata.Decode[State](s.Data)
	if s.OutcomeKey != "dispersed" || len(facts.Departed) != 3 || facts.UpkeepDays != 4 {
		t.Fatal("starvation did not lead to departure")
	}
	config = fixture(t)
	config.Threat = 2
	c = rules(t, config)
	s = start(t, c)
	s = act(t, c, s, "build", "facility", "hall")
	for s.Status == contract.RunActive {
		s = act(t, c, s, "advance", "hours", int64(24))
	}
	if s.OutcomeKey != "timeout" {
		t.Fatal("deadline ignored")
	}
	s = start(t, c)
	s = act(t, c, s, "dissolve", "", nil)
	if s.OutcomeKey != "dissolved" {
		t.Fatal("dissolve failed")
	}
}

func TestSectAdvanceStopsAtCrisisAndDoesNotProduceLater(t *testing.T) {
	config := fixture(t)
	config.CrisisTime = 300
	config.Missions[0].Minutes = 480
	c := rules(t, config)
	s := start(t, c)
	s = act(t, c, s, "dispatch", "plan", "shiyan/recover")
	s = act(t, c, s, "advance", "hours", int64(24))
	facts, _ := ruledata.Decode[State](s.Data)
	if s.GameTime != 300 || s.OutcomeKey != "fallen" || len(facts.Missions) != 0 || len(s.RandomEvidence) != 0 {
		t.Fatal("mission resolved after destroyed sect")
	}
}

func TestSectValidationAndPreviewPurity(t *testing.T) {
	config := fixture(t)
	config.Disciples[3].Key = config.Disciples[0].Key
	if err := validate(config); err == nil {
		t.Fatal("duplicate disciple identity accepted")
	}
	config = fixture(t)
	config.Recipes[0].Outputs = map[string]int64{"unknown": 1}
	if err := validate(config); err == nil {
		t.Fatal("unknown production resource accepted")
	}
	config = fixture(t)
	c := rules(t, config)
	s := start(t, c)
	before, _ := json.Marshal(s)
	for range 3 {
		if _, err := kernel.Preview(Module{}, c, s, "player"); err != nil {
			t.Fatal(err)
		}
		if _, err := kernel.Project(Module{}, c, s, "inquiry"); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := json.Marshal(s)
	if string(before) != string(after) {
		t.Fatal("read-only access advanced sect")
	}
}
