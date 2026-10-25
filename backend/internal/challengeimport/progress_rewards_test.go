package challengeimport

import (
	"path/filepath"
	"testing"
	"time"

	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/progressrules"
	"github.com/ialtone/fatecraft/internal/gameplay/kernel"
)

func TestBundledReincarnationStageRewards(t *testing.T) {
	loaded, err := loadDirectory(filepath.Join("..", "..", "data", "preset_packs", "reincarnation-trials"), time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	content := testModuleContent(t, loaded.document)
	rules, found, err := gameplay.ParseProgressRewards(content)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("bundled stage rewards missing")
	}
	if len(rules.Stages) != 3 || rules.TotalPoints() != 75 || rules.StageCap != 75 {
		t.Fatalf("unexpected stage budget: stages=%d total=%d cap=%d", len(rules.Stages), rules.TotalPoints(), rules.StageCap)
	}
	for _, stage := range rules.Stages {
		if stage.Points != 25 {
			t.Fatalf("stage %s points=%d", stage.Key, stage.Points)
		}
	}
	if len(rules.OutcomeKeys) != 3 {
		t.Fatalf("stage outcome keys = %#v", rules.OutcomeKeys)
	}
	// Modules must never see platform definitions; the trial singleton stays
	// exactly one definition.
	if len(content.Definitions) != 1 {
		t.Fatalf("module definitions = %d, want 1", len(content.Definitions))
	}
}

func TestEveryBundledPackHasFinalSettlementStages(t *testing.T) {
	cases := []struct {
		slug     string
		points   []int64
		outcomes []string
	}{
		{"island-rescue", []int64{25, 25, 25}, []string{"rescued"}},
		{"lost-chambers", []int64{25, 25, 25}, []string{"recovered"}},
		{"night-blockade", []int64{30, 30, 40}, []string{"escaped"}},
		{"underground-watchtower", []int64{30, 30, 40}, []string{"cleared"}},
		{"old-town-promise", []int64{15, 15, 20}, []string{"reunited", "honest_distance"}},
		{"mountain-inn", []int64{50, 50, 50}, []string{"prosperous"}},
		{"qinglan-sect", []int64{75, 75, 100}, []string{"rebuilt"}},
		{"reincarnation-trials", []int64{25, 25, 25}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.slug, func(t *testing.T) {
			loaded, err := loadDirectory(filepath.Join("..", "..", "data", "preset_packs", tc.slug), time.Unix(0, 0))
			if err != nil {
				t.Fatal(err)
			}
			content := testModuleContent(t, loaded.document)
			rules, found, err := gameplay.ParseProgressRewards(content)
			if err != nil || !found {
				t.Fatalf("stage rules: found=%v err=%v", found, err)
			}
			if len(rules.Stages) != len(tc.points) {
				t.Fatalf("stages=%d want=%d", len(rules.Stages), len(tc.points))
			}
			var total int64
			for index, stage := range rules.Stages {
				if stage.Points != tc.points[index] {
					t.Fatalf("stage %s points=%d want=%d", stage.Key, stage.Points, tc.points[index])
				}
				total += stage.Points
			}
			if rules.StageCap != total {
				t.Fatalf("stage cap=%d total=%d", rules.StageCap, total)
			}
			registry := defaultRegistry()
			descriptor, err := registry.Module(content.Runtime.Engine)
			if err != nil {
				t.Fatal(err)
			}
			initial, err := kernel.Initialize(descriptor.Runtime, content, gameplay.InitialInput{PersonaKey: loaded.manifest.DefaultConfig.RecommendedPersona, Random: kernel.NewRandom(37, "stages")})
			if err != nil {
				t.Fatal(err)
			}
			projection, err := kernel.Project(descriptor.Runtime, content, initial, gameplay.AudiencePlayer)
			if err != nil {
				t.Fatal(err)
			}
			for _, stage := range rules.Stages {
				if err := registry.Verify(stage.VerifierKey, gameplay.Evidence{State: map[string]any{"gameplay": projection}}, stage.VerifierConfig); err == nil {
					t.Fatalf("stage %s is already satisfied at game creation", stage.Key)
				}
			}
			if tc.outcomes != nil {
				if len(rules.OutcomeKeys) != len(tc.outcomes) {
					t.Fatalf("outcome keys=%v", rules.OutcomeKeys)
				}
				for index, key := range tc.outcomes {
					if rules.OutcomeKeys[index] != key {
						t.Fatalf("outcome keys=%v want=%v", rules.OutcomeKeys, tc.outcomes)
					}
				}
			}
		})
	}
}

func TestBundledMortalPathStillSettlesCultivationStages(t *testing.T) {
	loaded, err := loadDirectory(filepath.Join("..", "..", "data", "preset_packs", "mortal-path"), time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	for _, definition := range loaded.document.Definitions {
		if definition.Kind != progressrules.DefinitionKind {
			continue
		}
		rules, err := progressrules.Parse(definition.Content)
		if err != nil {
			t.Fatal(err)
		}
		if len(rules.Stages) != 3 {
			t.Fatalf("cultivation stages=%d", len(rules.Stages))
		}
		var total int64
		for _, stage := range rules.Stages {
			total += stage.Points
		}
		if total != 300 {
			t.Fatalf("cultivation stage points=%d", total)
		}
		return
	}
	t.Fatal("mortal-path stage rules missing")
}
