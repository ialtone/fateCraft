package kernel

import (
	"errors"
	"fmt"
	"testing"

	"github.com/ialtone/fatecraft/internal/gameplay/contract"
)

// The mystery fixture is an internal contract proof, not a product category.
// Its facts contain no character, realm, mana, equipment or combat state.
type mysteryFixture struct{ writeForbidden, wrongEvent bool }

func (m mysteryFixture) Actions() []contract.RuntimeAction {
	return []contract.RuntimeAction{{Key: "mystery.choose", Label: "核验线索", Description: "根据真实线索选择出口", Actors: []string{"player", "model"}, Parameters: []contract.RuntimeParameter{{Key: "exit", Label: "出口", Type: "string", Required: true, Options: []string{"east", "west"}}}, Writes: []string{"choice"}, Events: []string{"mystery.resolved"}}}
}
func (m mysteryFixture) Initialize(content contract.Content, _ contract.InitialInput) (contract.RunState, error) {
	return contract.RunState{SchemaVersion: 1, Status: contract.RunActive, Data: map[string]any{"clue": content.Definitions[0].Content["exit"], "choice": ""}}, nil
}
func (m mysteryFixture) ValidateState(_ contract.Content, state contract.RunState) error {
	clue, _ := state.Data["clue"].(string)
	if state.SchemaVersion != 1 || (clue != "east" && clue != "west") {
		return fmt.Errorf("invalid fixture state")
	}
	return nil
}
func (m mysteryFixture) Preview(_ contract.Content, _ contract.RunState, _ string) ([]contract.ActionPreview, error) {
	return []contract.ActionPreview{{Action: m.Actions()[0], Available: true}}, nil
}
func (m mysteryFixture) Execute(_ contract.Content, state contract.RunState, request contract.RuntimeRequest) (contract.Transition, error) {
	state.Data["choice"] = request.Arguments["exit"]
	state.Status, state.OutcomeKey = contract.RunFailed, "lost"
	if state.Data["choice"] == state.Data["clue"] {
		state.Status, state.OutcomeKey = contract.RunCompleted, "escaped"
	}
	kind := "mystery.resolved"
	if m.writeForbidden {
		state.Data["clue"] = "forged"
		state.Data["clue"] = "west"
	}
	if m.wrongEvent {
		kind = "account.grant"
	}
	return contract.Transition{State: state, Events: []contract.RuntimeEvent{{Kind: kind, Visibility: contract.VisibilityPlayer, Data: map[string]any{"choice": state.Data["choice"]}}}}, nil
}
func (m mysteryFixture) Project(_ contract.Content, state contract.RunState, _ string) (map[string]any, error) {
	return map[string]any{"clue": state.Data["clue"], "choice": state.Data["choice"]}, nil
}
func mysteryContent(exit string) contract.Content {
	return contract.Content{Definitions: []contract.Definition{{Kind: "mystery.case", Key: "test", Content: map[string]any{"exit": exit}}}, Outcomes: []contract.Outcome{{Key: "escaped", Enabled: true}, {Key: "lost", Enabled: true}}}
}

func TestIndependentModuleVariantsAndTerminalPaths(t *testing.T) {
	for _, exit := range []string{"east", "west"} {
		content := mysteryContent(exit)
		state, err := Initialize(mysteryFixture{}, content, contract.InitialInput{Random: NewRandom(7, "birth")})
		if err != nil {
			t.Fatal(err)
		}
		for _, choice := range []string{"east", "west"} {
			transition, err := Execute(mysteryFixture{}, content, state, contract.RuntimeRequest{ActionKey: "mystery.choose", Actor: "player", Arguments: map[string]any{"exit": choice}, Random: NewRandom(7, "action")})
			if err != nil {
				t.Fatal(err)
			}
			if transition.State.Revision != 1 || (transition.State.Status == contract.RunCompleted) != (choice == exit) {
				t.Fatalf("variant %s / choice %s: %+v", exit, choice, transition)
			}
			previews, err := Preview(mysteryFixture{}, content, transition.State, "player")
			if err != nil || len(previews) != 0 {
				t.Fatalf("terminal action remained available: %+v %v", previews, err)
			}
		}
		if state.Data["choice"] != "" || state.Revision != 0 {
			t.Fatal("execution mutated the input snapshot")
		}
	}
}

func TestKernelRejectsUndeclaredWritesEventsAndArguments(t *testing.T) {
	content := mysteryContent("east")
	state, _ := Initialize(mysteryFixture{}, content, contract.InitialInput{})
	request := contract.RuntimeRequest{ActionKey: "mystery.choose", Actor: "player", Arguments: map[string]any{"exit": "east"}, Random: NewRandom(7, "action")}
	for _, module := range []mysteryFixture{{writeForbidden: true}, {wrongEvent: true}} {
		if _, err := Execute(module, content, state, request); err == nil {
			t.Fatal("undeclared authority accepted")
		}
		if state.Data["clue"] != "east" || state.Data["choice"] != "" {
			t.Fatal("rejected transition escaped the snapshot")
		}
	}
	request.Arguments["reward_points"] = 999
	_, err := Execute(mysteryFixture{}, content, state, request)
	var violation *contract.RuleViolation
	if !errors.As(err, &violation) || violation.Code != "argument.unknown" {
		t.Fatalf("forged argument error: %v", err)
	}
}

func TestRandomIsRepeatablePurposeBoundedAndScopeBound(t *testing.T) {
	first, second := NewRandom(91, "run-1/action-2"), NewRandom(91, "run-1/action-2")
	for _, purpose := range []string{"search", "weather", "injury"} {
		a, err := first.Intn(purpose, 100)
		if err != nil {
			t.Fatal(err)
		}
		b, err := second.Intn(purpose, 100)
		if err != nil || a != b || a < 0 || a >= 100 {
			t.Fatalf("random replay failed: %d %d %v", a, b, err)
		}
	}
	if _, err := first.Intn("search", 100); err == nil {
		t.Fatal("reused random purpose accepted")
	}
	if _, err := first.Intn("invalid", 0); err == nil {
		t.Fatal("zero random bound accepted")
	}
}

type tracedFixture struct {
	mysteryFixture
	nullWrite bool
}

func (m tracedFixture) Execute(content contract.Content, state contract.RunState, request contract.RuntimeRequest) (contract.Transition, error) {
	if _, err := request.Random.Intn("resolved", 100); err != nil {
		return contract.Transition{}, err
	}
	transition, err := m.mysteryFixture.Execute(content, state, request)
	if m.nullWrite {
		transition.State.Data["outside"] = nil
	}
	transition.State.RandomEvidence = []contract.RandomDraw{{Purpose: "forged", Upper: 100, Value: 99}}
	return transition, err
}

type outOfRangeRandom struct{}

func (outOfRangeRandom) Intn(_ string, upper int64) (int64, error) { return upper, nil }
func TestKernelOwnsRandomEvidenceAndRejectsNullInsertion(t *testing.T) {
	content := mysteryContent("east")
	state, _ := Initialize(mysteryFixture{}, content, contract.InitialInput{})
	request := contract.RuntimeRequest{ActionKey: "mystery.choose", Actor: "player", Arguments: map[string]any{"exit": "east"}, Random: NewRandom(91, "trace")}
	result, err := Execute(tracedFixture{}, content, state, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.State.RandomEvidence) != 1 || result.State.RandomEvidence[0].Purpose != "resolved" {
		t.Fatal("module supplied its own random evidence")
	}
	request.Random = outOfRangeRandom{}
	if _, err := Execute(tracedFixture{}, content, state, request); err == nil {
		t.Fatal("out-of-range injected source accepted")
	}
	request.Random = NewRandom(91, "trace")
	if _, err := Execute(tracedFixture{nullWrite: true}, content, state, request); err == nil {
		t.Fatal("undeclared null insertion accepted")
	}
}

type mutatingValidator struct{ mysteryFixture }

func (m mutatingValidator) ValidateState(content contract.Content, state contract.RunState) error {
	content.Definitions[0].Content["exit"] = "west"
	state.Data["choice"] = "west"
	return m.mysteryFixture.ValidateState(content, state)
}
func TestValidationCannotMutateCallerContentOrState(t *testing.T) {
	content := mysteryContent("east")
	state, _ := Initialize(mysteryFixture{}, content, contract.InitialInput{})
	if err := ValidateState(mutatingValidator{}, content, state); err != nil {
		t.Fatal(err)
	}
	if content.Definitions[0].Content["exit"] != "east" || state.Data["choice"] != "" {
		t.Fatal("validator changed shared input")
	}
}
