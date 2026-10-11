package contextbuilder

import (
	"strings"
	"testing"
)

func TestLargeModuleListDoesNotHideAllCurrentFacts(t *testing.T) {
	projection := map[string]any{"revision": 3, "gameplay": map[string]any{"goal": "返回营地完成救援", "health": 72, "inventory": []any{strings.Repeat("大量已知物资", 1000)}}}
	fitted, err := fitCampaignProjection(projection, []string{"gameplay"}, 1500, runeCounter{})
	if err != nil {
		t.Fatal(err)
	}
	result := decodeCampaignProjection(t, fitted.Content)
	view, ok := result["gameplay"].(map[string]any)
	if !ok || view["goal"] != "返回营地完成救援" || view["health"] != float64(72) {
		t.Fatalf("lost all module facts: %v", result)
	}
	if _, exists := view["inventory"]; exists {
		t.Fatal("oversized inventory bypassed budget")
	}
	control := result["projection_control"].(map[string]any)
	fields := control["omitted_fields"].([]any)
	if len(fields) != 1 || fields[0] != "gameplay.inventory" {
		t.Fatalf("partial module state was not identified: %v", control)
	}
	if len(projection["gameplay"].(map[string]any)) != 3 {
		t.Fatal("fitting mutated original projection")
	}
}
