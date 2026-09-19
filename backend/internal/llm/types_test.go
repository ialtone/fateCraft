package llm

import "testing"

func TestNormalizeProtocolDefaultsToResponses(t *testing.T) {
	for _, value := range []string{"", "unknown"} {
		if got := NormalizeProtocol(value); got != ProtocolResponses {
			t.Fatalf("NormalizeProtocol(%q) = %q, want %q", value, got, ProtocolResponses)
		}
	}
	if got := NormalizeProtocol(string(ProtocolChatCompletions)); got != ProtocolChatCompletions {
		t.Fatalf("explicit Chat Completions normalized to %q", got)
	}
}

func TestCanonicalBlocksAreAuthoritative(t *testing.T) {
	message := Message{Role: "assistant", Blocks: []ContentBlock{
		{Kind: ContentThinking, Text: "思考"},
		{Kind: ContentToolCall, ToolCall: &ToolCall{ID: "c1", Name: "fc_test", Arguments: "{}"}},
	}, Content: "不应使用"}
	if got := message.TextProjection(); got != "" {
		t.Fatalf("text projection = %q", got)
	}
	calls := message.ToolCallBlocks()
	if len(calls) != 1 || calls[0].ID != "c1" {
		t.Fatalf("tool calls = %#v", calls)
	}
}
