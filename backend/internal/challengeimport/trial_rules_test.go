package challengeimport

import (
	"github.com/ialtone/fatecraft/internal/challenge"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/kernel"
	"github.com/ialtone/fatecraft/internal/state"
	"github.com/ialtone/fatecraft/internal/storage"
	"path/filepath"
	"testing"
	"time"
)

func TestTrialPublicationRejectsBypassesAndBrokenOrigins(t *testing.T) {
	for _, variant := range []string{"old-engine", "free-roll", "model-writer", "missing-action", "missing-origin", "extra-fairy-chances", "missing-death", "paid-death", "unknown-rule"} {
		t.Run(variant, func(t *testing.T) {
			loaded, err := loadDirectory(filepath.Join("..", "..", "data", "preset_packs", "reincarnation-trials"), time.Unix(0, 0))
			if err != nil {
				t.Fatal(err)
			}
			runtime, err := storage.DecodeGameplayRuntime(loaded.document.Runtime)
			if err != nil {
				t.Fatal(err)
			}
			config := loaded.document.Definitions[0].Content
			switch variant {
			case "old-engine":
				runtime.Engine.Version = 1
			case "free-roll":
				runtime.Actions.Model = append(runtime.Actions.Model, "roll")
			case "model-writer":
				for i := range runtime.StateSchema.Fields {
					if runtime.StateSchema.Fields[i].Path == "gameplay" {
						runtime.StateSchema.Fields[i].UpdatePolicy = contract.UpdateModel
					}
				}
			case "missing-action":
				runtime.Actions.Domain = runtime.Actions.Domain[1:]
			case "missing-origin":
				config["births"] = config["births"].([]any)[1:]
			case "extra-fairy-chances":
				config["routes"].(map[string]any)["affinity_attempts"] = 100
			case "missing-death":
				loaded.document.Outcomes = loaded.document.Outcomes[:len(loaded.document.Outcomes)-1]
			case "paid-death":
				loaded.document.Outcomes[len(loaded.document.Outcomes)-1].RewardPoints = 100
			case "unknown-rule":
				config["auto_win"] = true
			}
			loaded.document.Runtime, err = storage.EncodeGameplayRuntime(*runtime)
			if err != nil {
				t.Fatal(err)
			}
			if err = challenge.ValidateDocument(&loaded.document, defaultRegistry()); err == nil {
				t.Fatal("unsafe trial version accepted")
			}
		})
	}
}

func TestTrialProtectedFactsAndModelSettlementCannotBeForged(t *testing.T) {
	loaded, err := loadDirectory(filepath.Join("..", "..", "data", "preset_packs", "reincarnation-trials"), time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	c := testModuleContent(t, loaded.document)
	registry := defaultRegistry()
	descriptor, err := registry.Module(c.Runtime.Engine)
	if err != nil {
		t.Fatal(err)
	}
	run, err := kernel.Initialize(descriptor.Runtime, c, contract.InitialInput{PersonaKey: "rogue-cultivator", Random: kernel.NewRandom(0, "validation")})
	if err != nil {
		t.Fatal(err)
	}
	patch, err := kernel.EnvelopePatch(descriptor.Runtime, c, kernel.Envelope{Engine: c.Runtime.Engine, Seed: "0", State: run})
	if err != nil {
		t.Fatal(err)
	}
	if err = state.ValidatePatch(c.Runtime.StateSchema, contract.SourceModule, patch); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"module_state", "gameplay", "gameplay.health", "gameplay.progress", "gameplay.outcome_key", "profile.remaining_lifespan", "inventory"} {
		if err = state.ValidatePatch(c.Runtime.StateSchema, contract.SourceAI, map[string]any{path: 100}); err == nil {
			t.Fatalf("model can write %s", path)
		}
	}
	// Successful-roll history cannot stand in for the module's protected outcome.
	for _, o := range loaded.document.Outcomes {
		if err = registry.Verify(o.VerifierKey, contract.Evidence{State: patch, SuccessfulRollKeys: map[string]bool{"cultivation.tribulation.9": true}}, o.VerifierConfig); err == nil {
			t.Fatalf("fresh birth qualifies for %s", o.OutcomeKey)
		}
	}
}
