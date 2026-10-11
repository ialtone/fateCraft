package survival

import (
	"fmt"
	"testing"

	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/kernel"
)

func testConfig() Config {
	return Config{StartLocation: "beach", RescueLocation: "beach", RescueItem: "signal", RescueAfterMinutes: 120, DeadlineMinutes: 1440, HungerPerHour: 4, ThirstPerHour: 6, WeatherRiskPercent: 0, WeatherDamage: 10,
		InitialStock: map[string]int64{"water": 2, "fruit": 2},
		Resources:    []Resource{{Key: "water", Name: "饮水", ThirstRecovery: 40}, {Key: "fruit", Name: "野果", HungerRecovery: 30}, {Key: "wood", Name: "木材"}, {Key: "rope", Name: "藤绳"}, {Key: "signal", Name: "求援装置"}},
		Locations:    []Location{{Key: "beach", Name: "海滩", Connections: []string{"forest"}, Resources: map[string]int64{"water": 3, "fruit": 4}, TravelMinutes: 30, GatherMinutes: 30}, {Key: "forest", Name: "树林", Connections: []string{"beach"}, Resources: map[string]int64{"wood": 5, "rope": 2}, TravelMinutes: 30, GatherMinutes: 30}},
		Recipes:      []Recipe{{Key: "signal", Name: "求援装置", Inputs: map[string]int64{"wood": 2, "rope": 1}, Output: "signal", Quantity: 1, Minutes: 60}},
	}
}

func TestRescueRequiresSufficientFiniteMaterials(t *testing.T) {
	c := testConfig()
	c.Recipes[0].Inputs["wood"] = 6
	if err := validateConfig(c); err == nil {
		t.Fatal("one source incorrectly treated as unlimited materials")
	}
	c = testConfig()
	c.Resources = append(c.Resources, Resource{Key: "frame", Name: "支架"})
	c.Recipes = []Recipe{
		{Key: "rope", Name: "编绳", Inputs: map[string]int64{"wood": 1}, Output: "rope", Quantity: 2, Minutes: 10},
		{Key: "frame", Name: "支架", Inputs: map[string]int64{"rope": 1}, Output: "frame", Quantity: 1, Minutes: 10},
		{Key: "signal", Name: "求援", Inputs: map[string]int64{"frame": 1, "rope": 1}, Output: "signal", Quantity: 1, Minutes: 10},
	}
	c.Locations[1].Resources = map[string]int64{"wood": 1}
	if err := validateConfig(c); err != nil {
		t.Fatalf("shared batch surplus discarded: %v", err)
	}
}

func TestInterruptedCraftingDoesNotCreateRescueEvidence(t *testing.T) {
	c := testConfig()
	c.InitialStock["wood"] = 2
	c.InitialStock["rope"] = 1
	content := testContent(t, c)
	s, err := kernel.Initialize(Module{}, content, contract.InitialInput{})
	if err != nil {
		t.Fatal(err)
	}
	s.GameTime = c.DeadlineMinutes - 1
	s = act(t, content, s, "craft", map[string]any{"recipe": "signal"})
	facts, _ := ruledata.Decode[State](s.Data)
	if s.OutcomeKey != "timeout" || facts.Crafted["signal"] != 0 || facts.Stock["signal"] != 0 || facts.Stock["wood"] != 0 {
		t.Fatal("unfinished crafting created output or refunded inputs")
	}
}
func testContent(t *testing.T, config Config) contract.Content {
	t.Helper()
	data, err := ruledata.Object(config)
	if err != nil {
		t.Fatal(err)
	}
	content := contract.Content{Definitions: []contract.Definition{{Kind: DefinitionKind, Key: "island", Content: data}}, Outcomes: []contract.Outcome{{Key: "rescued", Enabled: true}, {Key: "perished", Enabled: true}, {Key: "timeout", Enabled: true}, {Key: "abandoned", Enabled: true}}}
	if err := ValidateContent(content); err != nil {
		t.Fatal(err)
	}
	return content
}
func act(t *testing.T, content contract.Content, state contract.RunState, key string, args map[string]any) contract.RunState {
	t.Helper()
	result, err := kernel.Execute(Module{}, content, state, contract.RuntimeRequest{ActionKey: "survival." + key, Actor: "player", Arguments: args, Random: kernel.NewRandom(91, fmt.Sprintf("island/%d", state.Revision))})
	if err != nil {
		t.Fatalf("%s: %v", key, err)
	}
	return result.State
}

func TestSurvivalCraftedRescueAndConfigVariant(t *testing.T) {
	for _, woodCost := range []int64{2, 3} {
		config := testConfig()
		config.Recipes[0].Inputs["wood"] = woodCost
		content := testContent(t, config)
		state, err := kernel.Initialize(Module{}, content, contract.InitialInput{Random: kernel.NewRandom(91, "birth")})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := kernel.Execute(Module{}, content, state, contract.RuntimeRequest{ActionKey: "survival.rescue", Actor: "player", Random: kernel.NewRandom(91, "premature")}); err == nil {
			t.Fatal("rescue without crafting accepted")
		}
		state = act(t, content, state, "observe", nil)
		state = act(t, content, state, "travel", map[string]any{"destination": "forest"})
		for range woodCost {
			state = act(t, content, state, "gather", map[string]any{"resource": "wood"})
		}
		state = act(t, content, state, "gather", map[string]any{"resource": "rope"})
		state = act(t, content, state, "craft", map[string]any{"recipe": "signal"})
		state = act(t, content, state, "travel", map[string]any{"destination": "beach"})
		if _, err := kernel.Execute(Module{}, content, state, contract.RuntimeRequest{ActionKey: "survival.rescue", Actor: "model", Random: kernel.NewRandom(91, "model-rescue")}); err == nil {
			t.Fatal("model finalized player-only rescue")
		}
		state = act(t, content, state, "rescue", nil)
		facts, _ := ruledata.Decode[State](state.Data)
		if state.Status != contract.RunCompleted || state.OutcomeKey != "rescued" || facts.Stock["signal"] != 0 || facts.Remaining["forest"]["wood"] != 5-woodCost {
			t.Fatalf("rescue lost resource accounting: %+v %+v", state, facts)
		}
	}
}

func TestSurvivalFailurePathsAndInputSnapshot(t *testing.T) {
	config := testConfig()
	content := testContent(t, config)
	initial, err := kernel.Initialize(Module{}, content, contract.InitialInput{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = kernel.Execute(Module{}, content, initial, contract.RuntimeRequest{ActionKey: "survival.craft", Actor: "player", Arguments: map[string]any{"recipe": "signal"}, Random: kernel.NewRandom(91, "craft")})
	if err == nil {
		t.Fatal("free crafting accepted")
	}
	facts, _ := ruledata.Decode[State](initial.Data)
	if initial.GameTime != 0 || facts.Stock["water"] != 2 {
		t.Fatal("rejection mutated state")
	}
	state := initial
	for range 3 {
		if state.Status == contract.RunActive {
			state = act(t, content, state, "wait", map[string]any{"hours": int64(8)})
		}
	}
	if state.OutcomeKey != "perished" {
		t.Fatalf("starvation did not end run: %+v", state)
	}
	config.DeadlineMinutes = 240
	config.HungerPerHour = 1
	config.ThirstPerHour = 1
	content = testContent(t, config)
	state, err = kernel.Initialize(Module{}, content, contract.InitialInput{})
	if err != nil {
		t.Fatal(err)
	}
	state = act(t, content, state, "wait", map[string]any{"hours": int64(4)})
	if state.OutcomeKey != "timeout" {
		t.Fatalf("deadline did not end run: %+v", state)
	}
	config = testConfig()
	config.WeatherRiskPercent = 100
	config.WeatherDamage = 50
	content = testContent(t, config)
	state, _ = kernel.Initialize(Module{}, content, contract.InitialInput{})
	state = act(t, content, state, "observe", nil)
	state = act(t, content, state, "observe", nil)
	if state.OutcomeKey != "perished" {
		t.Fatal("weather damage was only narrative")
	}
}

func TestSurvivalRejectsUnreachableRescueAndLeaksNoHiddenResources(t *testing.T) {
	config := testConfig()
	config.Recipes[0].Inputs = map[string]int64{"signal": 1}
	if err := validateConfig(config); err == nil {
		t.Fatal("self-dependent rescue recipe accepted")
	}
	content := testContent(t, testConfig())
	state, _ := kernel.Initialize(Module{}, content, contract.InitialInput{})
	view, err := kernel.Project(Module{}, content, state, contract.AudiencePlayer)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := view["remaining"]; exists {
		t.Fatal("hidden resource stock leaked")
	}
	if locations := view["locations"].([]any); len(locations) != 1 {
		t.Fatal("undiscovered locations leaked")
	}
}

func TestHourlyThirstDoesNotChargeEarlierHoursOrRevive(t *testing.T) {
	c := testConfig()
	c.ThirstPerHour = 20
	content := testContent(t, c)
	run, _ := kernel.Initialize(Module{}, content, contract.InitialInput{})
	run = act(t, content, run, "wait", map[string]any{"hours": int64(5)})
	facts, _ := ruledata.Decode[State](run.Data)
	if facts.Health != 85 {
		t.Fatalf("charged hours before thirst reached 100: %d", facts.Health)
	}
	facts.Health = 10
	run.Data, _ = ruledata.Object(facts)
	run = act(t, content, run, "rest", map[string]any{"hours": int64(8)})
	if run.OutcomeKey != "perished" || run.GameTime != 360 {
		t.Fatalf("rest revived a dead survivor or advanced past death: %+v", run)
	}
	if len(run.RandomEvidence) != 0 {
		t.Fatal("weather continued after death")
	}
}
