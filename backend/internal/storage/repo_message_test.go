package storage

import (
	"encoding/json"
	"testing"
)

func TestNormalizeChatMessageFillsRequiredContentBlocks(t *testing.T) {
	message := &ChatMessage{Content: "继续", Role: RoleUser}
	if err := normalizeChatMessage(message); err != nil {
		t.Fatalf("normalizeChatMessage returned error: %v", err)
	}
	if message.Kind != "text" {
		t.Fatalf("kind = %q, want text", message.Kind)
	}
	if message.TokenizerVersion != ChatTokenizerVersion {
		t.Fatalf("tokenizer version = %q, want %q", message.TokenizerVersion, ChatTokenizerVersion)
	}
	var blocks []struct {
		Kind string `json:"kind"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(message.ContentBlocks, &blocks); err != nil {
		t.Fatalf("content_blocks is not JSON: %v", err)
	}
	if len(blocks) != 1 || blocks[0].Kind != "text" || blocks[0].Text != "继续" {
		t.Fatalf("content_blocks = %s, want one text block", message.ContentBlocks)
	}
}

func TestNormalizeChatMessageKeepsToolCallWithoutSyntheticText(t *testing.T) {
	message := &ChatMessage{Kind: "tool_call", Content: "provider payload"}
	if err := normalizeChatMessage(message); err != nil {
		t.Fatalf("normalizeChatMessage returned error: %v", err)
	}
	if string(message.ContentBlocks) != "[]" {
		t.Fatalf("content_blocks = %s, want []", message.ContentBlocks)
	}
}

func TestNormalizeChatMessageRejectsInvalidContentBlockKinds(t *testing.T) {
	message := &ChatMessage{ContentBlocks: JSONDoc(`[{"kind":"unknown"}]`)}
	if err := normalizeChatMessage(message); err == nil {
		t.Fatal("normalizeChatMessage accepted an invalid content block kind")
	}
}
