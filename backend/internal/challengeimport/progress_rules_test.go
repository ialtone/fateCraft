package challengeimport

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ialtone/fatecraft/internal/challenge"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/progressrules"
)

func TestProgressRewardBudgetAndFactsAreValidatedAtPublication(t *testing.T) {
	for _, variant := range []string{"budget", "negative", "duplicate", "terminal-prerequisite", "unknown-encounter", "unreachable-rank", "missing-ending", "zero-stage"} {
		t.Run(variant, func(t *testing.T) {
			loaded, err := loadDirectory(filepath.Join("..", "..", "data", "preset_packs", "mortal-path"), time.Unix(0, 0))
			if err != nil {
				t.Fatal(err)
			}
			var config map[string]any
			for _, d := range loaded.document.Definitions {
				if d.Kind == progressrules.DefinitionKind {
					config = d.Content
				}
			}
			if config == nil {
				t.Fatal("missing stage rules")
			}
			stages := config["stages"].([]any)
			first := stages[0].(map[string]any)
			switch variant {
			case "budget":
				config["total_points"] = 1001
			case "negative":
				first["points"] = -1
			case "duplicate":
				stages[1].(map[string]any)["key"] = first["key"]
			case "terminal-prerequisite":
				first["quest_groups"] = []any{[]any{"warden-ending"}}
			case "unknown-encounter":
				first["encounter_keys"] = []any{"nonexistent"}
			case "unreachable-rank":
				first["min_realm_rank"] = 999
			case "missing-ending":
				config["outcome_keys"] = []any{"world_warden"}
			case "zero-stage":
				first["points"] = 0
				config["total_points"] = 900
			}
			err = challenge.ValidateDocument(&loaded.document, defaultRegistry())
			if variant == "zero-stage" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("invalid stage rules published")
			}
		})
	}
}
