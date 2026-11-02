package campaignactions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ialtone/fatecraft/internal/storage"
)

func readCultivationDefinitions(t *testing.T) []storage.GameplayDefinition {
	t.Helper()
	root := filepath.Join("..", "..", "..", "..", "data", "preset_packs", "mortal-path")
	files, err := filepath.Glob(filepath.Join(root, "definitions", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	files = append([]string{filepath.Join(root, "definitions.json")}, files...)
	all := []storage.GameplayDefinition{}
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		var rows []storage.GameplayDefinition
		if err := json.Unmarshal(raw, &rows); err != nil {
			t.Fatal(err)
		}
		all = append(all, rows...)
	}
	return all
}
