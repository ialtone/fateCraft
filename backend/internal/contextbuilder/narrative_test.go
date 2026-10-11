package contextbuilder

import (
	"errors"
	"github.com/ialtone/fatecraft/internal/storage"
	"strings"
	"testing"
)

func TestNarrativeFactsSurviveProjectionCompaction(t *testing.T) {
	projection := map[string]any{"revision": 5, "status": "active", "narrative_facts": map[string]any{"realm": "凡人", "progress": 52}, "gameplay": map[string]any{"inventory": strings.Repeat("库存详情", 4000)}}
	result, err := Build(Input{MaxContextTokens: 2000, ReservedResponseTokens: 256, CampaignProjection: projection, CampaignProjectionPriority: []string{"gameplay"}, History: []storage.ChatMessage{{Role: "assistant", Content: "历史错误描述：练气一层"}}}, runeCounter{})
	if err != nil {
		t.Fatal(err)
	}
	found, compacted := false, false
	historyIndex, factsIndex := -1, -1
	for index, message := range result.Messages {
		if strings.Contains(message.Content, "历史错误描述") {
			historyIndex = index
		}
		if strings.Contains(message.Content, "<narrative_facts>") {
			factsIndex = index
			found = strings.Contains(message.Content, `"realm":"凡人"`) && strings.Contains(message.Content, `"progress":52`)
		}
		if strings.Contains(message.Content, "<campaign_projection>") {
			compacted = strings.Contains(message.Content, "projection_control")
		}
	}
	if !found || !compacted || historyIndex < 0 || factsIndex <= historyIndex {
		t.Fatal("mandatory facts lost during compaction")
	}
	projection["narrative_facts"] = map[string]any{"realm": strings.Repeat("x", 10000)}
	if _, err := Build(Input{MaxContextTokens: 2000, ReservedResponseTokens: 256, CampaignProjection: projection}, runeCounter{}); !errors.Is(err, ErrRequiredOverflow) {
		t.Fatalf("oversized core should reject, got %v", err)
	}
}
