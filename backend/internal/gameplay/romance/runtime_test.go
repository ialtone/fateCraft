package romance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/kernel"
)

func packContent(t *testing.T) contract.Content {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "data", "preset_packs", "xingang-lover", "definitions", "scenario.json"))
	if err != nil {
		t.Fatal(err)
	}
	var defs []contract.Definition
	if err := json.Unmarshal(raw, &defs); err != nil {
		t.Fatal(err)
	}
	outcomes := []contract.Outcome{}
	for _, key := range []string{"perfect_lover", "won_back", "together", "farewell_blessing", "unexpected_love", "more_than_friends", "rejected", "missed", "abandoned"} {
		outcomes = append(outcomes, contract.Outcome{Key: key, Enabled: true})
	}
	content := contract.Content{Definitions: defs, Outcomes: outcomes}
	if err := ValidateContent(content); err != nil {
		t.Fatal(err)
	}
	return content
}

func run(t *testing.T, content contract.Content, state contract.RunState, actor, key string, args map[string]any) (contract.RunState, error) {
	t.Helper()
	result, err := kernel.Execute(Module{}, content, state, contract.RuntimeRequest{ActionKey: "romance." + key, Actor: actor, Arguments: args, Random: kernel.NewRandom(7, fmt.Sprintf("romance/%d", state.Revision))})
	return result.State, err
}

func mustRun(t *testing.T, content contract.Content, state contract.RunState, key string, args map[string]any) contract.RunState {
	t.Helper()
	next, err := run(t, content, state, "player", key, args)
	if err != nil {
		t.Fatalf("%s: %v", key, err)
	}
	return next
}

func started(t *testing.T, lead string) (contract.Content, contract.RunState) {
	content := packContent(t)
	state, err := kernel.Initialize(Module{}, content, contract.InitialInput{})
	if err != nil {
		t.Fatal(err)
	}
	return content, mustRun(t, content, state, "start", map[string]any{"lead": lead})
}

func decodeState(t *testing.T, state contract.RunState) State {
	t.Helper()
	facts, err := ruledata.Decode[State](state.Data)
	if err != nil {
		t.Fatal(err)
	}
	return facts
}

func writeState(t *testing.T, state contract.RunState, facts State) contract.RunState {
	t.Helper()
	data, err := ruledata.Object(facts)
	if err != nil {
		t.Fatal(err)
	}
	state.Data = data
	return state
}

func openFinal(t *testing.T, state contract.RunState, options ...string) contract.RunState {
	t.Helper()
	facts := decodeState(t, state)
	if len(options) == 0 {
		options = []string{"confess", "stay", "bless", "side", "let_go"}
	}
	facts.Scene = &OpenScene{Key: "final", Options: options}
	return writeState(t, state, facts)
}

// readyLover bumps the calendar so StageDay stays valid while stage_day >= 7.
func readyLover(facts *State) {
	facts.Day = 20
	facts.Stage = 3
	facts.StageDay = 13
	facts.Slot = 0
	facts.Half = 0
}

func TestProjectionAlwaysCoversDeclaredFields(t *testing.T) {
	content := packContent(t)
	state, err := kernel.Initialize(Module{}, content, contract.InitialInput{})
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"before", "after"} {
		view, err := Module{}.Project(content, state, contract.AudiencePlayer)
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"day", "slot", "stage", "stage_rank", "facts", "caps", "outcome_key", "log", "lead", "her", "you", "money", "stamina", "messages", "rivals", "warning", "memory_hooks", "knot_clues", "birthday"} {
			if _, ok := view[key]; !ok {
				t.Fatalf("%s start: projection missing %s", phase, key)
			}
		}
		if _, ok := view["hidden"]; ok {
			t.Fatal("player projection leaked hidden profile")
		}
		if phase == "before" {
			state = mustRun(t, content, state, "start", map[string]any{"lead": "lin_wan"})
		}
	}
}

func TestStartActAndEarlyConfession(t *testing.T) {
	content, state := started(t, "lin_wan")
	before := decodeState(t, state)
	if before.Lead != "lin_wan" || before.Stage != 0 {
		t.Fatalf("unexpected start state %+v", before)
	}
	state = mustRun(t, content, state, "act", map[string]any{"activity": "coffee_meet"})
	after := decodeState(t, state)
	if after.Stats["money"] >= before.Stats["money"] || after.Slot == before.Slot && after.Day == before.Day {
		t.Fatal("activity did not spend money or advance time")
	}
	if _, err := run(t, content, state, "player", "act", map[string]any{"activity": "stargaze"}); err == nil {
		t.Fatal("stage-locked activity accepted")
	}
	state = mustRun(t, content, state, "confess", map[string]any{})
	facts := decodeState(t, state)
	if facts.Stage != 0 || facts.Flags["cooldown"] == 0 || !strings.Contains(lastHook(facts), "拒绝") {
		t.Fatalf("early confession not rejected: %+v", facts.Lists["memory_hooks"])
	}
}

func TestTeaserAsksIfSerious(t *testing.T) {
	content, state := started(t, "yao_xiaotao")
	state = mustRun(t, content, state, "confess", map[string]any{})
	facts := decodeState(t, state)
	if facts.Flags["serious"] == 0 || !strings.Contains(lastHook(facts), "你认真的？") {
		t.Fatal("姚小桃 early confession should answer 你认真的？")
	}
}

func TestAbandonIsNeutralTerminal(t *testing.T) {
	content, state := started(t, "su_tang")
	state = mustRun(t, content, state, "abandon", nil)
	if state.Status == contract.RunActive || state.OutcomeKey != "abandoned" {
		t.Fatalf("abandon not terminal: %v %s", state.Status, state.OutcomeKey)
	}
	if _, err := run(t, content, state, "player", "act", map[string]any{"activity": "coffee_meet"}); err == nil {
		t.Fatal("finished run accepted another action")
	}
}

func TestModelCannotStartOrAbandonAndStateValidation(t *testing.T) {
	content := packContent(t)
	state, _ := kernel.Initialize(Module{}, content, contract.InitialInput{})
	if _, err := run(t, content, state, "model", "start", map[string]any{"lead": "lin_wan"}); err == nil {
		t.Fatal("model chose lead")
	}
	if _, err := run(t, content, state, "player", "start", map[string]any{"lead": "nobody"}); err == nil {
		t.Fatal("unknown lead accepted")
	}
	broken := state
	broken.Data = map[string]any{}
	for key, value := range state.Data {
		broken.Data[key] = value
	}
	broken.Data["lead"] = "nobody"
	if err := (Module{}).ValidateState(content, broken); err == nil {
		t.Fatal("invalid lead state accepted")
	}
}

func TestPreviewRespectsKernelLimits(t *testing.T) {
	content, state := started(t, "ye_fei")
	previews, err := Module{}.Preview(content, state, "player")
	if err != nil {
		t.Fatal(err)
	}
	for _, preview := range previews {
		if preview.Action.Key == "romance.start" && preview.Available {
			t.Fatal("start still available after choosing")
		}
		for _, parameter := range preview.Action.Parameters {
			if len(parameter.Options) > 64 {
				t.Fatalf("%s has %d options", preview.Action.Key, len(parameter.Options))
			}
		}
	}
}

func TestPlayerReplyTopicsOnlyUnlocked(t *testing.T) {
	content, state := started(t, "lin_wan")
	facts := decodeState(t, state)
	facts.Likes = append(facts.Likes, "black_coffee")
	facts.Messages = []Message{{ID: 1, Kind: "share", Day: facts.Day, Slot: facts.Slot}}
	state = writeState(t, state, facts)

	player, err := Module{}.Preview(content, state, "player")
	if err != nil {
		t.Fatal(err)
	}
	model, err := Module{}.Preview(content, state, "model")
	if err != nil {
		t.Fatal(err)
	}
	var playerTopics, modelTopics []string
	for _, preview := range player {
		if preview.Action.Key == "romance.reply" {
			playerTopics = preview.Action.Parameters[2].Options
		}
	}
	for _, preview := range model {
		if preview.Action.Key == "romance.reply" {
			modelTopics = preview.Action.Parameters[2].Options
		}
	}
	if len(playerTopics) == 0 {
		t.Fatal("player should see unlocked topics")
	}
	if len(modelTopics) <= len(playerTopics) {
		t.Fatalf("model should see more topics than player: player=%d model=%d", len(playerTopics), len(modelTopics))
	}
	for _, key := range playerTopics {
		if !slices.Contains(facts.Likes, key) && !slices.Contains(facts.Dislikes, key) {
			t.Fatalf("player saw locked topic %s", key)
		}
	}
}

func TestExecuteRejectsMissingSceneAndMessage(t *testing.T) {
	content, state := started(t, "lin_wan")
	chooseReq := contract.RuntimeRequest{ActionKey: "romance.choose", Actor: "player", Arguments: map[string]any{"option": "confess"}, Random: kernel.NewRandom(1, "x")}
	if _, err := (Module{}).Execute(content, state, chooseReq); err == nil {
		t.Fatal("choose without scene should reject")
	}
	replyReq := contract.RuntimeRequest{ActionKey: "romance.reply", Actor: "player", Arguments: map[string]any{"message": "999", "quality": "ok", "topic": ""}, Random: kernel.NewRandom(1, "y")}
	if _, err := (Module{}).Execute(content, state, replyReq); err == nil {
		t.Fatal("reply without message should reject")
	}
}

func TestEndingPerfectLover(t *testing.T) {
	content, state := started(t, "lin_wan")
	facts := decodeState(t, state)
	readyLover(&facts)
	facts.Stats["affection"] = 95
	facts.Stats["trust"] = 90
	facts.Stats["spark"] = 70
	facts.Stats["mood"] = 1
	facts.Stats["jealousy"] = 0
	facts.Flags["knot"] = 1
	state = openFinal(t, writeState(t, state, facts), "confess", "bless", "let_go")
	state = mustRun(t, content, state, "choose", map[string]any{"option": "confess"})
	if state.OutcomeKey != "perfect_lover" {
		t.Fatalf("want perfect_lover, got %s status=%v", state.OutcomeKey, state.Status)
	}
}

func TestEndingWonBack(t *testing.T) {
	content, state := started(t, "lin_wan")
	facts := decodeState(t, state)
	readyLover(&facts)
	facts.Stats["affection"] = 75
	facts.Stats["trust"] = 70
	facts.Stats["spark"] = 60
	facts.Stats["mood"] = 1
	facts.Stats["jealousy"] = 0
	// insight is a sum of revealed likes/dislikes (+ clues); unlock enough tags so confess_ready can pass.
	facts.Likes = []string{"black_coffee", "old_film", "keyboard", "code_together", "listened", "cat"}
	facts.Dislikes = []string{"perfunctory", "late", "public_tease", "touch_pc"}
	facts.Flags["won_back"] = 1
	facts.Flags["knot"] = 0
	state = openFinal(t, writeState(t, state, facts), "confess", "bless", "let_go")
	state = mustRun(t, content, state, "choose", map[string]any{"option": "confess"})
	if state.OutcomeKey != "won_back" {
		t.Fatalf("want won_back, got %s log=%v", state.OutcomeKey, decodeState(t, state).Log)
	}
}

func TestEndingFarewellBlessing(t *testing.T) {
	content, state := started(t, "su_tang")
	facts := decodeState(t, state)
	facts.Stage = 2
	facts.Stats["affection"] = 60
	facts.Stats["trust"] = 65
	facts.Flags["reached_ambiguous"] = 1
	facts.Flags["warning"] = facts.Day
	state = openFinal(t, writeState(t, state, facts), "bless", "let_go")
	state = mustRun(t, content, state, "choose", map[string]any{"option": "bless"})
	if state.OutcomeKey != "farewell_blessing" {
		t.Fatalf("want farewell_blessing, got %s", state.OutcomeKey)
	}
}

func TestEndingUnexpectedLove(t *testing.T) {
	content, state := started(t, "ye_fei")
	facts := decodeState(t, state)
	readyLover(&facts)
	facts.Stats["affection"] = 40
	facts.Stats["side"] = 80
	facts.Stats["trust"] = 50
	state = openFinal(t, writeState(t, state, facts), "side", "bless", "let_go")
	state = mustRun(t, content, state, "choose", map[string]any{"option": "side"})
	if state.OutcomeKey != "unexpected_love" {
		t.Fatalf("want unexpected_love, got %s", state.OutcomeKey)
	}
}

func TestUnknownPlaceholderKeptInFill(t *testing.T) {
	content, state := started(t, "lin_wan")
	g, err := load(content, &state)
	if err != nil {
		t.Fatal(err)
	}
	got := g.fill("她喜欢{stat.affecion}")
	if got != "她喜欢{stat.affecion}" {
		t.Fatalf("unknown placeholder should stay visible, got %q", got)
	}
}

func TestResolveRefActAliases(t *testing.T) {
	content := packContent(t)
	cfg, err := ruledata.Singleton[Config](content, DefinitionKind)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"act", "act.halves", "day", "stage", "stage.min_days"} {
		if _, ok := resolveRef(&cfg, name); !ok {
			t.Fatalf("%s should be known by resolveRef", name)
		}
	}
	if _, ok := resolveRef(&cfg, "act.unknown"); ok {
		t.Fatal("act.unknown should not be known")
	}
	v := &validator{c: &cfg}
	v.ref("test", "act")
	v.ref("test", "act.halves")
	if len(v.errs) != 0 {
		t.Fatalf("validator should accept act aliases, got %v", v.errs)
	}
	v.ref("test", "act.unknown")
	if len(v.errs) == 0 {
		t.Fatal("validator should reject act.unknown")
	}
}

func TestProjectionSceneAbsentIsNil(t *testing.T) {
	content := packContent(t)
	state, err := kernel.Initialize(Module{}, content, contract.InitialInput{})
	if err != nil {
		t.Fatal(err)
	}
	view, err := Module{}.Project(content, state, contract.AudiencePlayer)
	if err != nil {
		t.Fatal(err)
	}
	scene, ok := view["scene"]
	if !ok {
		t.Fatal("projection should include scene key")
	}
	if scene != nil {
		t.Fatalf("absent scene should be nil, got %#v", scene)
	}
}

func lastHook(s State) string {
	hooks := s.Lists["memory_hooks"]
	if len(hooks) == 0 {
		return ""
	}
	return hooks[len(hooks)-1].Text
}
