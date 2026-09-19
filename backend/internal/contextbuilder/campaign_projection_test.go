package contextbuilder

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFitCampaignProjectionCompactsUnavailableActions(t *testing.T) {
	projection := map[string]any{
		"id": "campaign", "status": "active", "revision": 3,
		"current_location_key": "start", "game_time": 1,
		"action_capabilities": []any{
			map[string]any{
				"key": "domain.available", "description": "available", "available": true, "revision": 3,
				"argument_schema": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{}},
			},
			map[string]any{
				"key": "domain.blocked", "description": "blocked", "available": false, "revision": 3,
				"argument_schema": map[string]any{"type": "object", "description": strings.Repeat("x", 4000)},
				"options":         []any{map[string]any{"large": strings.Repeat("y", 4000)}},
				"metadata":        map[string]any{"large": strings.Repeat("z", 4000)},
				"violations":      []any{map[string]any{"code": "action.blocked", "message": "blocked now"}},
			},
		},
	}
	fitted, err := fitCampaignProjection(projection, nil, 1200, runeCounter{})
	if err != nil {
		t.Fatal(err)
	}
	if !fitted.Compacted || fitted.OriginalTokens <= 1200 {
		t.Fatalf("projection was not compacted: %+v", fitted)
	}
	decoded := decodeCampaignProjection(t, fitted.Content)
	actions := decoded["action_capabilities"].([]any)
	if len(actions) != 2 {
		t.Fatalf("action count = %d", len(actions))
	}
	blocked := actions[1].(map[string]any)
	if blocked["key"] != "domain.blocked" || blocked["violations"] == nil {
		t.Fatalf("blocked action lost its public reason: %#v", blocked)
	}
	for _, key := range []string{"argument_schema", "options", "metadata", "revision"} {
		if _, exists := blocked[key]; exists {
			t.Fatalf("unavailable action retained %s: %#v", key, blocked)
		}
	}
}

func TestFitCampaignProjectionHonorsProviderTopicPriority(t *testing.T) {
	projection := map[string]any{
		"id": "campaign", "status": "active", "revision": 1,
		"high": map[string]any{"text": strings.Repeat("h", 300)},
		"low":  map[string]any{"text": strings.Repeat("l", 300)},
	}
	for budget := 300; budget < 1200; budget++ {
		fitted, err := fitCampaignProjection(projection, []string{"high", "low"}, budget, runeCounter{})
		if err != nil {
			continue
		}
		decoded := decodeCampaignProjection(t, fitted.Content)
		_, highIncluded := decoded["high"]
		_, lowIncluded := decoded["low"]
		if highIncluded && !lowIncluded {
			return
		}
	}
	t.Fatal("no constrained budget kept the high-priority topic before the low-priority topic")
}

func decodeCampaignProjection(t *testing.T, content string) map[string]any {
	t.Helper()
	content = strings.TrimPrefix(content, "<campaign_projection>\n")
	content = strings.TrimSuffix(content, "\n</campaign_projection>")
	var decoded map[string]any
	if err := json.Unmarshal([]byte(content), &decoded); err != nil {
		t.Fatalf("decode projection: %v", err)
	}
	return decoded
}
