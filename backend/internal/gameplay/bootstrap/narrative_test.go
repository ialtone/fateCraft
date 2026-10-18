package bootstrap_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ialtone/fatecraft/internal/gameplay/bootstrap"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/kernel"
)

func TestInstalledNarrativeCoreCoversEightGameplayTypes(t *testing.T) {
	registry := bootstrap.NewRegistry()
	for _, slug := range []string{"island-rescue", "mountain-inn", "night-blockade", "old-town-promise", "lost-chambers", "underground-watchtower", "reincarnation-trials", "qinglan-sect"} {
		t.Run(slug, func(t *testing.T) {
			root := filepath.Join("..", "..", "..", "data", "preset_packs", slug)
			raw, err := os.ReadFile(filepath.Join(root, "runtime", "runtime.json"))
			if err != nil {
				t.Fatal(err)
			}
			var runtime contract.RuntimeContract
			if err := json.Unmarshal(raw, &runtime); err != nil {
				t.Fatal(err)
			}
			descriptor, err := registry.Module(runtime.Engine)
			if err != nil {
				t.Fatal(err)
			}
			content := contract.Content{Runtime: &runtime}
			files, err := filepath.Glob(filepath.Join(root, "definitions", "*.json"))
			if err != nil {
				t.Fatal(err)
			}
			for _, file := range files {
				raw, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				var definitions []contract.Definition
				if err := json.Unmarshal(raw, &definitions); err != nil {
					t.Fatal(err)
				}
				content.Definitions = append(content.Definitions, definitions...)
			}
			personas, _ := filepath.Glob(filepath.Join(root, "personas", "*.json"))
			if len(personas) == 0 {
				t.Fatal("no sample persona")
			}
			raw, err = os.ReadFile(personas[0])
			if err != nil {
				t.Fatal(err)
			}
			var persona struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(raw, &persona); err != nil {
				t.Fatal(err)
			}
			packBytes, err := os.ReadFile(filepath.Join(root, "pack.json"))
			if err != nil {
				t.Fatal(err)
			}
			var pack struct {
				Outcomes []struct {
					Key          string `json:"outcome_key"`
					Category     string `json:"category"`
					RewardPoints int64  `json:"reward_points"`
				} `json:"outcomes"`
			}
			if err := json.Unmarshal(packBytes, &pack); err != nil {
				t.Fatal(err)
			}
			for _, o := range pack.Outcomes {
				content.Outcomes = append(content.Outcomes, contract.Outcome{Key: o.Key, Category: o.Category, RewardPoints: o.RewardPoints, Enabled: true})
			}
			hasSuccess := false
			for _, outcome := range pack.Outcomes {
				if outcome.Category == "success" {
					hasSuccess = true
					break
				}
			}
			if !hasSuccess {
				t.Fatal("pack has no success outcome")
			}
			endingActions := 0
			for _, action := range descriptor.Runtime.Actions() {
				if action.Ending {
					endingActions++
				}
			}
			if endingActions == 0 {
				t.Fatal("module has no ending action")
			}
			if runtime.Mode == contract.RuntimeModeCampaign && endingActions < 2 {
				t.Fatal("campaign module has no non-abandon ending action")
			}
			run, err := kernel.Initialize(descriptor.Runtime, content, contract.InitialInput{PersonaKey: persona.ID, Random: kernel.NewRandom(0, "narrative-core")})
			if err != nil {
				t.Fatal(err)
			}
			if descriptor.AbandonAction == "" {
				t.Fatal("missing exit declaration")
			}
			if _, err := kernel.Execute(descriptor.Runtime, content, run, contract.RuntimeRequest{Actor: "model", ActionKey: descriptor.AbandonAction, Arguments: map[string]any{}, Random: kernel.NewRandom(0, "exit-test")}); err == nil {
				t.Fatal("model executed player exit")
			}
			exit, err := kernel.Execute(descriptor.Runtime, content, run, contract.RuntimeRequest{Actor: "player", ActionKey: descriptor.AbandonAction, Arguments: map[string]any{}, Random: kernel.NewRandom(0, "exit-test")})
			if err != nil {
				t.Fatal(err)
			}
			if exit.State.Status == contract.RunActive || exit.State.OutcomeKey == "" || exit.State.GameTime != run.GameTime {
				t.Fatalf("invalid immediate exit: %+v", exit.State)
			}
			found := false
			for _, o := range pack.Outcomes {
				if o.Key == exit.State.OutcomeKey {
					found = o.Category == "neutral" && o.RewardPoints == 0
				}
			}
			if !found {
				t.Fatalf("exit %s lacks neutral zero-point outcome", exit.State.OutcomeKey)
			}
			view, err := kernel.Project(descriptor.Runtime, content, run, contract.AudienceModel)
			if err != nil {
				t.Fatal(err)
			}
			paths := contract.NarrativeFactPaths(descriptor.Runtime)
			if len(paths) == 0 {
				t.Fatal("installed module lacks a narrative core")
			}
			facts, err := contract.SelectNarrativeFacts(view, paths)
			if err != nil {
				t.Fatal(err)
			}
			for path, value := range facts {
				if missing, ok := value.(map[string]any); ok && missing["unavailable"] == true {
					t.Errorf("declared initial fact %s is unavailable", path)
				}
			}
		})
	}
}
