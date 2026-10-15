package orchestration

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ialtone/fatecraft/internal/llm"
	"github.com/ialtone/fatecraft/internal/tokenizer"
)

func TestEstimateAgentOutputExcludesStorageAndReplayOverhead(t *testing.T) {
	counter := tokenizer.NewHeuristic()
	result := llm.AgentStepResult{Content: []llm.ContentBlock{
		{Kind: llm.ContentThinking, Text: "思考", Replay: json.RawMessage(`{"encrypted_content":"` + strings.Repeat("x", 4000) + `"}`)},
		{Kind: llm.ContentText, Text: "叙事"},
		{Kind: llm.ContentRefusal, Text: "拒绝"},
		{Kind: llm.ContentToolCall, ToolCall: &llm.ToolCall{ID: strings.Repeat("id", 100), ItemID: "provider-item", Name: "advance", Arguments: `{"days":1}`}},
	}}
	want := counter.Count("思考叙事拒绝advance" + `{"days":1}`)
	if got := estimateAgentOutputTokens(counter, result); got != want {
		t.Fatalf("estimated tokens=%d, want %d excluding wrappers and replay", got, want)
	}
}

func TestEstimateEmptyAgentOutputIsZero(t *testing.T) {
	if got := estimateAgentOutputTokens(tokenizer.NewHeuristic(), llm.AgentStepResult{}); got != 0 {
		t.Fatalf("empty output tokens=%d", got)
	}
}
