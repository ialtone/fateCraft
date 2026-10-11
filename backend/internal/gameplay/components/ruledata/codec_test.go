package ruledata

import (
	"strings"
	"testing"

	"github.com/ialtone/fatecraft/internal/gameplay/contract"
)

func TestConfigBoundsIncludeHiddenLabelsAndFieldPaths(t *testing.T) {
	c := contract.Content{Definitions: []contract.Definition{{Kind: "test.rules", Content: map[string]any{"locked": []any{map[string]any{"name": strings.Repeat("秘", 257)}}}}}}
	if _, err := Singleton[map[string]any](c, "test.rules"); err == nil || !strings.Contains(err.Error(), "test.rules.locked[0].name") {
		t.Fatalf("hidden label limit was not checked with its path: %v", err)
	}
	c.Definitions[0].Content["locked"] = []any{map[string]any{"name": strings.Repeat("秘", 256)}}
	if _, err := Singleton[map[string]any](c, "test.rules"); err != nil {
		t.Fatal(err)
	}
}
