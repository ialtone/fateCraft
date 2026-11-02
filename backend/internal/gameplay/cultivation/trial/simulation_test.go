//go:build slow

package trial

import (
	"fmt"
	"sort"
	"testing"

	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
)

func TestFromBirthStrategiesAcrossSeedsAndDifficulty(t *testing.T) {
	base, initial := fixture(t)
	cfg, _, _ := read(base, initial)
	for _, difficulty := range []int64{-10, 0, 10} {
		for _, route := range []string{"affinity", "rift", "orthodox"} {
			t.Run(fmt.Sprintf("difficulty_%d/%s", difficulty, route), func(t *testing.T) {
				cfg.Difficulty = difficulty
				raw, err := ruledata.Object(cfg)
				if err != nil {
					t.Fatal(err)
				}
				c := base
				c.Definitions = []contract.Definition{{Kind: DefinitionKind, Content: raw}}
				count, steps, years := 0, []int{}, []int{}
				for seed := int64(0); seed < 14; seed++ {
					run, n := simulateLife(t, c, seed, cfg.Births[seed%int64(len(cfg.Births))].Key, route, true)
					if run.Status == contract.RunCompleted {
						count++
						steps = append(steps, n)
						years = append(years, int(run.GameTime/Year))
					}
					if run.Status == contract.RunActive && route != "affinity" {
						t.Fatal("nonterminal route")
					}
				}
				if count == 0 {
					t.Fatal("no reachable completion in tested seeds")
				}
				sort.Ints(steps)
				sort.Ints(years)
				t.Logf("completed %d/14; successful decisions min/median/max=%d/%d/%d; median elapsed years=%d", count, steps[0], steps[len(steps)/2], steps[len(steps)-1], years[len(years)/2])
			})
		}
	}
}

func TestPreparationImprovesWholeTribulationRoute(t *testing.T) {
	c, _ := fixture(t)
	prepared, unprepared := 0, 0
	for seed := int64(20); seed < 40; seed++ {
		a, _ := simulateLife(t, c, seed, "sword-cultivator", "orthodox", true)
		b, _ := simulateLife(t, c, seed, "sword-cultivator", "orthodox", false)
		if a.Status == contract.RunCompleted {
			prepared++
		}
		if b.Status == contract.RunCompleted {
			unprepared++
		}
	}
	t.Logf("whole-route success: prepared=%d/20 unprepared=%d/20", prepared, unprepared)
	if prepared <= unprepared {
		t.Fatal("preparation/retreat strategy does not improve whole-route survival")
	}
}
