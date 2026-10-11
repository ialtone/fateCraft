package challengeimport

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ialtone/fatecraft/internal/challenge"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/ascensionrules"
	"github.com/ialtone/fatecraft/internal/storage"
)

func TestCultivationPublicationRejectsMissingOrCircularFinalGates(t *testing.T) {
	for _, variant := range []string{"missing-config", "missing-action", "early-realm", "ending-as-prerequisite", "trial-as-prerequisite", "model-granted-material", "unprotected-ending", "old-engine", "old-layer-scale"} {
		t.Run(variant, func(t *testing.T) {
			loaded, err := loadDirectory(filepath.Join("..", "..", "data", "preset_packs", "mortal-path"), time.Unix(0, 0))
			if err != nil {
				t.Fatal(err)
			}
			doc := &loaded.document
			var config *storage.GameplayDefinition
			for i := range doc.Definitions {
				if doc.Definitions[i].Kind == ascensionrules.DefinitionKind {
					config = &doc.Definitions[i]
				}
			}
			if config == nil {
				t.Fatal("new content lacks ascension rules")
			}
			switch variant {
			case "missing-config":
				doc.Definitions = slices.DeleteFunc(doc.Definitions, func(d storage.GameplayDefinition) bool { return d.Kind == ascensionrules.DefinitionKind })
			case "missing-action":
				c := testModuleContent(t, *doc)
				c.Runtime.Campaign.ActionAllowlist = slices.DeleteFunc(c.Runtime.Campaign.ActionAllowlist, func(k string) bool { return k == ascensionrules.ActionBegin })
				doc.Runtime, err = storage.EncodeGameplayRuntime(*c.Runtime)
				if err != nil {
					t.Fatal(err)
				}
			case "early-realm":
				config.Content["realm_key"] = "void-refining"
			case "ending-as-prerequisite":
				config.Content["quest_groups"] = []any{[]any{"warden-ending"}}
			case "trial-as-prerequisite":
				for i := range doc.Definitions {
					if doc.Definitions[i].Key == "hunt-abyssal-isle" {
						doc.Definitions[i].Content["objectives"] = []any{map[string]any{"key": "trial", "type": "encounter_win", "encounter_key": "final-ascension-tribulation", "quantity": float64(1)}}
					}
				}
			case "model-granted-material":
				for i := range doc.Definitions {
					if doc.Definitions[i].Kind == storage.DefinitionItem && doc.Definitions[i].Key == "law-fragment" {
						doc.Definitions[i].Content["model_grantable"] = true
					}
				}
			case "unprotected-ending":
				config.Content["outcome_keys"] = []any{"world_warden"}
			case "old-engine":
				doc.Runtime.EngineVersion = 1
			case "old-layer-scale":
				for i := range doc.Definitions {
					if doc.Definitions[i].Kind == storage.DefinitionTechnique {
						doc.Definitions[i].Content["mastery_per_layer"] = float64(1000)
						break
					}
				}
			}
			if err := challenge.ValidateDocument(doc, defaultRegistry()); err == nil {
				t.Fatal("invalid cultivation rules published")
			} else if variant == "old-layer-scale" && !strings.Contains(err.Error(), "mastery_per_layer") {
				t.Fatalf("unexpected validation: %v", err)
			}
		})
	}
}
