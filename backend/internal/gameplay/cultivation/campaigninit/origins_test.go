package campaigninit

import (
	"encoding/json"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/originrules"
	"github.com/ialtone/fatecraft/internal/storage"
	"os"
	"path/filepath"
	"testing"
)

func TestAllBuiltinPersonasHaveDistinctValidatedStartingSupplies(t *testing.T) {
	base := "../../../../data/preset_packs/mortal-path/"
	var definitions []storage.GameplayDefinition
	files, err := filepath.Glob(filepath.Join(base, "definitions", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var rows []storage.GameplayDefinition
		if err = json.Unmarshal(raw, &rows); err != nil {
			t.Fatal(err)
		}
		definitions = append(definitions, rows...)
	}
	keys := []string{"herb-apprentice", "qinglan-handyman", "old-ferry-hand", "lin-clan-sideline", "yamen-runner", "wandering-cultivator"}
	c := contract.Content{PersonaKeys: keys}
	for _, d := range definitions {
		c.Definitions = append(c.Definitions, contract.Definition{Kind: d.Kind, Key: d.Key, Content: d.Content})
	}
	if err := originrules.Validate(c); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, key := range keys {
		o, err := selectOrigin(definitions, key)
		if err != nil || o == nil {
			t.Fatalf("%s: %v", key, err)
		}
		raw, _ := json.Marshal(o.Items)
		if seen[string(raw)] {
			t.Fatal("identical persona supplies")
		}
		seen[string(raw)] = true
	}
	if _, err := selectOrigin(definitions, "invented"); err == nil {
		t.Fatal("unknown persona silently fell back")
	}
	c.PersonaKeys = append(c.PersonaKeys, "invented")
	if err := originrules.Validate(c); err == nil {
		t.Fatal("publication accepted missing persona mapping")
	}
	c.PersonaKeys = keys
	for i, d := range c.Definitions {
		if d.Kind == originrules.DefinitionKind {
			rows := d.Content["origins"].([]any)
			rows[0].(map[string]any)["items"] = map[string]any{"manual.basic-breathing": 1}
			c.Definitions[i] = d
		}
	}
	if err := originrules.Validate(c); err == nil {
		t.Fatal("origin can hand out non-starter progression items")
	}
}
