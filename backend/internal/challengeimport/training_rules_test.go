package challengeimport

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ialtone/fatecraft/internal/challenge"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/trainingrules"
	"github.com/ialtone/fatecraft/internal/storage"
)

func TestTrainingPublicationRejectsInvalidOrUnusablePlans(t *testing.T) {
	for _, variant := range []string{"missing-action", "missing-combat-action", "invalid-mode", "early-technique", "unknown-material", "region-as-location", "negative-cost", "unbounded-days", "free-retreat", "rest-grants-growth"} {
		t.Run(variant, func(t *testing.T) {
			loaded, err := loadDirectory(filepath.Join("..", "..", "data", "preset_packs", "mortal-path"), time.Unix(0, 0))
			if err != nil {
				t.Fatal(err)
			}
			doc := &loaded.document
			var plan *storage.GameplayDefinition
			for i := range doc.Definitions {
				if doc.Definitions[i].Kind == trainingrules.DefinitionKind && doc.Definitions[i].Key == "retreat-mortal" {
					plan = &doc.Definitions[i]
					break
				}
			}
			if plan == nil {
				t.Fatal("missing built-in retreat")
			}
			switch variant {
			case "missing-action", "missing-combat-action":
				c := testModuleContent(t, *doc)
				key := trainingrules.ActionRetreat
				if variant == "missing-combat-action" {
					key = "encounter.act"
				}
				c.Runtime.Campaign.ActionAllowlist = slices.DeleteFunc(c.Runtime.Campaign.ActionAllowlist, func(k string) bool { return k == key })
				doc.Runtime, err = storage.EncodeGameplayRuntime(*c.Runtime)
				if err != nil {
					t.Fatal(err)
				}
			case "invalid-mode":
				plan.Content["mode"] = "free-grant"
			case "early-technique":
				plan.Content["technique_keys"] = []any{"great-unity-scripture"}
			case "unknown-material":
				plan.Content["materials"] = []any{map[string]any{"item_key": "unobtainable", "quantity": 1}}
			case "region-as-location":
				plan.Content["location_keys"] = []any{"mortal-realm"}
			case "negative-cost":
				plan.Content["mana_cost"] = -1
			case "unbounded-days":
				plan.Content["days"] = 3651
			case "free-retreat":
				plan.Content["materials"] = []any{}
			case "rest-grants-growth":
				plan.Content["mode"] = "recovery"
			}
			if err := challenge.ValidateDocument(doc, defaultRegistry()); err == nil {
				t.Fatal("invalid training content published")
			}
		})
	}
}
