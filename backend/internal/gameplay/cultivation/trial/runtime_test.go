package trial

import (
	"encoding/json"
	"fmt"
	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/kernel"
	"os"
	"reflect"
	"testing"
)

func fixture(t *testing.T) (contract.Content, contract.RunState) {
	t.Helper()
	raw, err := os.ReadFile("../../../../data/preset_packs/reincarnation-trials/definitions/trial-rules.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Content map[string]any `json:"content"`
	}
	if err = json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	c := contract.Content{Definitions: []contract.Definition{{Kind: DefinitionKind, Content: rows[0].Content}}}
	for _, k := range outcomeKeys {
		c.Outcomes = append(c.Outcomes, contract.Outcome{Key: k, Enabled: true})
	}
	if err = ValidateContent(c); err != nil {
		t.Fatal(err)
	}
	s, err := kernel.Initialize(Module{}, c, contract.InitialInput{PersonaKey: "rogue-cultivator", Random: kernel.NewRandom(5, "birth")})
	if err != nil {
		t.Fatal(err)
	}
	return c, s
}

type fixedRandom int64

func (r fixedRandom) Intn(_ string, upper int64) (int64, error) { return int64(r) % upper, nil }
func do(t *testing.T, c contract.Content, s contract.RunState, key, arg string, random int64) contract.RunState {
	t.Helper()
	args := map[string]any{}
	if arg != "" {
		args["key"] = arg
	}
	tr, err := kernel.Execute(Module{}, c, s, contract.RuntimeRequest{ActionKey: key, Actor: "player", Arguments: args, Random: fixedRandom(random)})
	if err != nil {
		t.Fatalf("%s %s: %v", key, arg, err)
	}
	return tr.State
}
func change(t *testing.T, run contract.RunState, edit func(*State)) contract.RunState {
	t.Helper()
	s, err := ruledata.Decode[State](run.Data["life"])
	if err != nil {
		t.Fatal(err)
	}
	edit(&s)
	raw, err := ruledata.Object(s)
	if err != nil {
		t.Fatal(err)
	}
	run.Data = map[string]any{"life": raw}
	return run
}
func ready(t *testing.T, c contract.Content, run contract.RunState, rank int) contract.RunState {
	run = change(t, run, func(s *State) {
		s.Realm = rank
		s.Progress = 100
		s.Method = "herbal-breath"
		s.Completed["herbal-apprenticeship"] = 1
		for _, k := range itemKeys {
			s.Stock[k] = 100
		}
	})
	if err := kernel.ValidateState(Module{}, c, run); err != nil {
		t.Fatal(err)
	}
	return run
}
func denied(t *testing.T, c contract.Content, s contract.RunState, key, actor string, args map[string]any) {
	t.Helper()
	before, _ := json.Marshal(s)
	_, err := kernel.Execute(Module{}, c, s, contract.RuntimeRequest{ActionKey: key, Actor: actor, Arguments: args, Random: fixedRandom(0)})
	if err == nil {
		t.Fatalf("accepted forbidden %s as %s", key, actor)
	}
	after, _ := json.Marshal(s)
	if string(before) != string(after) {
		t.Fatal("rejection changed caller state")
	}
}

func TestBirthIsAuthoritativeAndPreviewNeverSpends(t *testing.T) {
	c, s := fixture(t)
	cfg, _, _ := read(c, s)
	seen := map[string]bool{}
	for _, b := range cfg.Births {
		r, err := kernel.Initialize(Module{}, c, contract.InitialInput{PersonaKey: b.Key, Random: kernel.NewRandom(5, "birth")})
		if err != nil {
			t.Fatal(err)
		}
		_, life, _ := read(c, r)
		if life.Realm != 0 || life.Method != "" || life.AgeAtBirth < 18*Year || life.AgeAtBirth > 25*Year || life.Token || life.Rift {
			t.Fatal("invalid birth")
		}
		seen[fmt.Sprint(life.Stock, life.Vigor, life.Insight, life.Luck)] = true
		before, _ := json.Marshal(r)
		for range 3 {
			for _, aud := range []string{"player", "model", "inquiry"} {
				if _, err = kernel.Project(Module{}, c, r, aud); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = kernel.Preview(Module{}, c, r, "player"); err != nil {
				t.Fatal(err)
			}
		}
		after, _ := json.Marshal(r)
		if string(before) != string(after) {
			t.Fatal("read advanced lifespan or random state")
		}
	}
	if len(seen) != len(cfg.Births) {
		t.Fatal("personas do not change actual birth")
	}
	if _, err := kernel.Initialize(Module{}, c, contract.InitialInput{PersonaKey: "invented", Random: fixedRandom(0)}); err == nil {
		t.Fatal("unknown birth accepted")
	}
	denied(t, c, s, "trial.practice", "player", nil)
	denied(t, c, s, "trial.activity", "model", map[string]any{"key": "herbal-commission", "stones": 999})
	denied(t, c, s, "trial.activity", "player", map[string]any{"key": "law-sanctum-watch"})
}

func TestLifespanEndsBeforeLootGrowthAndHealing(t *testing.T) {
	c, initial := fixture(t)
	for _, action := range []string{"trial.practice", "trial.breakthrough", "trial.activity", "trial.consume"} {
		run := ready(t, c, initial, 0)
		arg := ""
		run = change(t, run, func(s *State) {
			if action == "trial.practice" {
				s.Progress = 0
			}
			if action == "trial.consume" {
				s.HP = 10
			}
		})
		if action == "trial.activity" {
			arg = "herbal-commission"
		}
		if action == "trial.consume" {
			arg = "healing-pill"
		}
		cfg, before, _ := read(c, run)
		run.GameTime = (cfg.Realms[0].LifespanYears*Year - before.AgeAtBirth) - 5
		next := do(t, c, run, action, arg, 0)
		_, after, _ := read(c, next)
		if next.OutcomeKey != "lifespan_exhausted" || after.DeathCause != "lifespan" || lifeRemaining(cfg, after, next) != 0 || after.Progress != before.Progress || after.Realm != before.Realm || after.HP != before.HP || after.Stock["herb"] != before.Stock["herb"] {
			t.Fatalf("post-death effect: %+v", after)
		}
		denied(t, c, next, "trial.rest", "player", nil)
	}
	// Time is cumulative across realm changes, even well beyond 1900 years.
	run := ready(t, c, initial, 8)
	run.GameTime = 19999 * Year
	_, s, _ := read(c, run)
	run.GameTime -= s.AgeAtBirth
	if err := kernel.ValidateState(Module{}, c, run); err != nil {
		t.Fatal(err)
	}
	next := do(t, c, run, "trial.wait", "", 0)
	if next.OutcomeKey != "lifespan_exhausted" {
		t.Fatal("long-lived character cannot age to death")
	}
}

func TestAffinityHasLimitedAttemptsAndPlayerOwnedUse(t *testing.T) {
	c, run := fixture(t)
	run = ready(t, c, run, 3)
	denied(t, c, run, "trial.affinity_seek", "player", nil)
	run = do(t, c, run, "trial.activity", "immortal-courier", 0)
	failure := do(t, c, run, "trial.affinity_seek", "", 99)
	failure = do(t, c, failure, "trial.affinity_seek", "", 99)
	denied(t, c, failure, "trial.affinity_seek", "player", nil)
	_, s, _ := read(c, failure)
	if s.AffinityAttempts != 2 || s.Token {
		t.Fatal("fairy chance count wrong")
	}
	success := do(t, c, run, "trial.affinity_seek", "", 0)
	denied(t, c, success, "trial.affinity_use", "model", nil)
	success = do(t, c, success, "trial.affinity_use", "", 0)
	_, s, _ = read(c, success)
	if success.OutcomeKey != "affinity_ascension" || s.Token || !s.TokenUsed {
		t.Fatal("token not atomically consumed")
	}
}

func TestRiftFailureClosesDiscoveryAndSpendsPreparation(t *testing.T) {
	c, run := fixture(t)
	run = ready(t, c, run, 4)
	denied(t, c, run, "trial.rift_enter", "player", nil)
	run = do(t, c, run, "trial.rift_search", "", 0)
	denied(t, c, run, "trial.rift_enter", "model", nil)
	failure := do(t, c, run, "trial.rift_enter", "", 99)
	_, s, _ := read(c, failure)
	if s.Rift || s.HP != 75 || s.Injury != 20 || s.Stock["void-anchor"] != 99 || s.CooldownUntil <= failure.GameTime {
		t.Fatalf("rift failure missing consequences: %+v", s)
	}
	denied(t, c, failure, "trial.rift_enter", "player", nil)
	denied(t, c, failure, "trial.rift_search", "player", nil)
	success := do(t, c, run, "trial.rift_enter", "", 0)
	if success.OutcomeKey != "void_ascension" {
		t.Fatal("rift did not settle")
	}
}

func TestTribulationWavesCannotSkipOrStitchAcrossAttempts(t *testing.T) {
	c, run := fixture(t)
	run = ready(t, c, run, 5)
	denied(t, c, run, "trial.tribulation_begin", "model", nil)
	denied(t, c, run, "trial.tribulation_wave", "player", nil)
	run = do(t, c, run, "trial.tribulation_begin", "", 0)
	denied(t, c, run, "trial.tribulation_begin", "player", nil)
	denied(t, c, run, "trial.tribulation_wave", "player", map[string]any{"wave": 9})
	failed := do(t, c, run, "trial.tribulation_wave", "", 99)
	_, s, _ := read(c, failed)
	if s.Tribulation.Wave != 1 || s.Tribulation.Failures != 1 || s.Stock["thunder-talisman"] != 99 || s.HP >= 100 || s.Injury == 0 {
		t.Fatal("failed wave did not keep sequence and spend")
	}
	failed = do(t, c, failed, "trial.tribulation_wave", "", 0)
	_, s, _ = read(c, failed)
	if s.Tribulation.Wave != 2 || s.Stock["thunder-talisman"] != 99 {
		t.Fatal("retry double consumed phase protection")
	}
	retreated := do(t, c, failed, "trial.tribulation_retreat", "", 0)
	denied(t, c, retreated, "trial.tribulation_wave", "player", nil)
	denied(t, c, retreated, "trial.tribulation_begin", "player", nil)
	retreated = do(t, c, retreated, "trial.rest", "", 0)
	retreated = do(t, c, retreated, "trial.wait", "", 0)
	retreated = do(t, c, retreated, "trial.tribulation_begin", "", 0)
	_, s, _ = read(c, retreated)
	if s.Tribulation.Attempt != 2 || s.Tribulation.Wave != 1 || s.Tribulation.ProtectedPhase != 0 || s.Tribulation.Failures != 0 {
		t.Fatal("new attempt retained progress")
	}
	for wave := int64(1); wave <= 9; wave++ {
		retreated = do(t, c, retreated, "trial.tribulation_wave", "", 0)
		_, s, _ = read(c, retreated)
		if s.Tribulation.Wave != wave+1 {
			t.Fatal("out of sequence")
		}
	}
	if retreated.OutcomeKey != "orthodox_ascension" || s.Tribulation.Active || s.Stock["thunder-talisman"] != 96 {
		t.Fatal("nine-wave result or phase costs wrong")
	}
	denied(t, c, retreated, "trial.tribulation_wave", "player", nil)
}

func TestDamagePreparationRealmKarmaAndDeathCause(t *testing.T) {
	c, run := fixture(t)
	run = ready(t, c, run, 5)
	cfg, s, _ := read(c, run)
	s.Tribulation = Tribulation{Attempt: 1, Active: true, Wave: 7}
	unprepared := waveDamage(cfg, s, nil)
	s.Tribulation.Ward = true
	s.Tribulation.ProtectedPhase = 3
	if waveDamage(cfg, s, nil) >= unprepared {
		t.Fatal("preparation does not reduce damage")
	}
	base := waveChance(cfg, s)
	s.Realm = 8
	if waveChance(cfg, s) <= base || waveDamage(cfg, s, nil) >= unprepared {
		t.Fatal("higher realm does not improve risk")
	}
	base = waveChance(cfg, s)
	s.Karma = 20
	if waveChance(cfg, s) >= base {
		t.Fatal("karma does not affect risk")
	}
	run = do(t, c, run, "trial.tribulation_begin", "", 0)
	run = change(t, run, func(s *State) { s.HP = 1 })
	run = do(t, c, run, "trial.tribulation_wave", "", 99)
	_, s, _ = read(c, run)
	if run.OutcomeKey != "perished" || s.DeathCause != "tribulation" || s.HP != 0 || lifeRemaining(cfg, s, run) <= 0 {
		t.Fatal("death falsely converted to lifespan exhaustion")
	}
}

func TestConfigRejectsUnreachableOrFreeRules(t *testing.T) {
	c, run := fixture(t)
	cfg, _, _ := read(c, run)
	for name, edit := range map[string]func(*Config){"free practice": func(c *Config) { c.Realms[0].PracticeCost = 0 }, "infinite fairy": func(c *Config) { c.Routes.AffinityAttempts = 20 }, "free recipe": func(c *Config) { c.Recipes[0].Inputs = map[string]int64{} }, "missing birth": func(c *Config) { c.Births = nil }, "gift token": func(c *Config) { c.Births[0].Stock["fairy-token"] = 1 }, "unreachable method": func(c *Config) { c.Activities[1].MinRealm = 8 }, "no safe economy": func(c *Config) { c.Activities[0].Cost = 99 }} {
		t.Run(name, func(t *testing.T) {
			copy, err := ruledata.Decode[Config](cfg)
			if err != nil {
				t.Fatal(err)
			}
			edit(&copy)
			if validateConfig(copy) == nil {
				t.Fatal("invalid content accepted")
			}
		})
	}
	before, _ := json.Marshal(run)
	view, err := kernel.Project(Module{}, c, run, contract.AudienceInquiry)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := view["random_evidence"]; ok {
		t.Fatal("private randomness leaked")
	}
	after, _ := json.Marshal(run)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("projection mutated state")
	}
}

func TestAbandonDuringTribulationDoesNotMeanDeathOrConsumeTime(t *testing.T) {
	c, run := fixture(t)
	run = ready(t, c, run, 5)
	run = do(t, c, run, "trial.tribulation_begin", "", 0)
	denied(t, c, run, "trial.abandon", "model", nil)
	denied(t, c, run, "trial.pass_away", "player", nil)
	before := run.GameTime
	exited := do(t, c, run, "trial.abandon", "", 0)
	state, err := ruledata.Decode[State](exited.Data["life"])
	if err != nil {
		t.Fatal(err)
	}
	if exited.OutcomeKey != "abandoned" || exited.GameTime != before || state.HP != 100 || state.DeathCause != "" {
		t.Fatalf("incorrect exit: %+v / %+v", exited, state)
	}
}
