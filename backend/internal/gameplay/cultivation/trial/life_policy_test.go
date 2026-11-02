package trial

import (
	"fmt"
	"testing"

	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/kernel"
)

// This policy starts with actual birth supplies. Every herb, pill and spirit
// stone is earned, bought or crafted by a real action; no mature fixture is
// substituted. It is an offline balance baseline, not a narrative playtest.
func simulateLife(t *testing.T, c contract.Content, seed int64, persona, route string, prepared bool) (contract.RunState, int) {
	t.Helper()
	run, err := kernel.Initialize(Module{}, c, contract.InitialInput{PersonaKey: persona, Random: kernel.NewRandom(seed, "birth")})
	if err != nil {
		t.Fatal(err)
	}
	for decisions := 0; decisions < 400; decisions++ {
		if run.Status != contract.RunActive {
			return run, decisions
		}
		cfg, s, err := read(c, run)
		if err != nil {
			t.Fatal(err)
		}
		key, arg := "trial.activity", "herbal-commission"
		choose := func(k, a string) { key, arg = k, a }
		switch {
		case s.Tribulation.Active:
			if prepared && s.HP <= 60 && s.Stock["healing-pill"] > 0 {
				choose("trial.consume", "healing-pill")
			} else if prepared && (s.Injury >= 30 || s.Stock["stone"] < cfg.Routes.WaveCost+s.Tribulation.Failures) {
				choose("trial.tribulation_retreat", "")
			} else {
				choose("trial.tribulation_wave", "")
			}
		case s.HP < 90 || s.Injury > 0:
			choose("trial.rest", "")
		case s.Method == "":
			choose("trial.activity", "herbal-apprenticeship")
		case route == "affinity" && s.Realm >= 3 && !s.AffinityQualified:
			choose("trial.activity", "immortal-courier")
		case route == "affinity" && s.Realm >= 3 && s.Token:
			choose("trial.affinity_use", "")
		case route == "affinity" && s.Realm >= 3 && s.AffinityAttempts < cfg.Routes.AffinityAttempts:
			choose("trial.affinity_seek", "")
		case route == "affinity" && s.Realm >= 3:
			return run, decisions // exhausted opportunities; another route remains available
		case route == "rift" && s.Realm >= 4:
			if run.GameTime < s.CooldownUntil {
				choose("trial.wait", "")
			} else if !s.Rift {
				choose("trial.rift_search", "")
			} else if prepared && s.Stock["void-anchor"] == 0 {
				choose("trial.buy", "void-anchor")
			} else {
				choose("trial.rift_enter", "")
			}
		case route == "orthodox" && s.Realm >= 5 && s.Progress == 100:
			if run.GameTime < s.CooldownUntil {
				choose("trial.wait", "")
			} else if s.Stock["stone"] < 60 { /* provision by paid work */
			} else if s.Stock["ward"] == 0 {
				choose("trial.buy", "ward")
			} else if prepared && s.Stock["thunder-talisman"] < 3 {
				choose("trial.buy", "thunder-talisman")
			} else if prepared && s.Stock["healing-pill"] < 3 {
				choose("trial.buy", "healing-pill")
			} else {
				choose("trial.tribulation_begin", "")
			}
		case s.Progress < 100:
			choose("trial.practice", "")
		case s.Stock["breakthrough-pill"] == 0:
			if s.Stock["herb"] >= 2 && s.Stock["stone"] >= 1 {
				choose("trial.craft", "prepare-breakthrough")
			} else {
				choose("trial.buy", "breakthrough-pill")
			}
		default:
			choose("trial.breakthrough", "")
		}
		choices, _ := options(cfg, s, run, key)
		available := false
		for _, o := range choices {
			k, _ := o.plan.Arguments["key"].(string)
			// 缺口方案仅用于展示，策略必须视同不可用。
			if k == arg && o.plan.BlockedReason == "" {
				available = true
			}
		}
		if !available {
			if s.Tribulation.Active {
				choose("trial.tribulation_retreat", "")
			} else {
				choose("trial.activity", "herbal-commission")
			}
		}
		args := map[string]any{}
		if arg != "" {
			args["key"] = arg
		}
		tr, err := kernel.Execute(Module{}, c, run, contract.RuntimeRequest{ActionKey: key, Actor: "player", Arguments: args, Random: kernel.NewRandom(seed, fmt.Sprintf("decision-%d", decisions))})
		if err != nil {
			t.Fatalf("seed=%d route=%s decision=%d %s(%s): %v", seed, route, decisions, key, arg, err)
		}
		run = tr.State
	}
	t.Fatalf("policy stranded after 400 paid actions: seed=%d persona=%s route=%s", seed, persona, route)
	return run, 400
}

// Default CI gate: one birth→completion run (seed 0, orthodox, prepared).
// Multi-seed / multi-difficulty matrices live under //go:build slow.
func TestFromBirthShortestHappyPath(t *testing.T) {
	c, _ := fixture(t)
	run, n := simulateLife(t, c, 0, "sword-cultivator", "orthodox", true)
	if run.Status != contract.RunCompleted {
		t.Fatalf("shortest path did not complete: status=%s outcome=%s decisions=%d", run.Status, run.OutcomeKey, n)
	}
	t.Logf("completed in %d decisions; outcome=%s years=%d", n, run.OutcomeKey, int(run.GameTime/Year))
}
