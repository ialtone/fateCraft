package storage

import (
	"encoding/json"
	"strings"
)

// ReasoningText is retained as a small pure helper for callers that already
// have a provider response in the old split representation. New persistence
// code uses ReasoningTextBlocks below.
func ReasoningText(content string, items JSONDoc) string {
	if strings.TrimSpace(content) != "" {
		return content
	}
	if len(items) == 0 || string(items) == "null" {
		return ""
	}
	var rawItems []json.RawMessage
	if json.Unmarshal(items, &rawItems) != nil {
		return ""
	}
	parts := make([]string, 0, len(rawItems))
	for _, raw := range rawItems {
		var item struct {
			Type    string `json:"type"`
			Text    string `json:"text"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			Summary []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"summary"`
		}
		if json.Unmarshal(raw, &item) != nil || item.Type != "reasoning" {
			continue
		}
		appendReasoningTextPart(&parts, item.Text)
		for _, part := range item.Content {
			if isReadableReasoningPart(part.Type) {
				appendReasoningTextPart(&parts, part.Text)
			}
		}
		for _, part := range item.Summary {
			if isReadableReasoningPart(part.Type) {
				appendReasoningTextPart(&parts, part.Text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

// ReasoningTextBlocks extracts the safe, player-facing thinking projection from
// the canonical ordered content_blocks JSON. Provider-owned replay payloads
// are inspected only for readable summary text; opaque fields are never
// returned to the player.
func ReasoningTextBlocks(blocks JSONDoc) string {
	if len(blocks) == 0 || string(blocks) == "null" || string(blocks) == "[]" {
		return ""
	}
	var values []struct {
		Kind   string          `json:"kind"`
		Text   string          `json:"text"`
		Replay json.RawMessage `json:"replay"`
	}
	if json.Unmarshal(blocks, &values) != nil {
		return ""
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		if value.Kind != "thinking" {
			continue
		}
		appendReasoningTextPart(&parts, value.Text)
		if len(value.Replay) > 0 {
			appendReasoningTextPart(&parts, reasoningReplayText(value.Replay))
		}
	}
	return strings.Join(parts, "\n")
}

func reasoningReplayText(raw json.RawMessage) string {
	var item struct {
		Type    string `json:"type"`
		Text    string `json:"text"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Summary []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"summary"`
	}
	if json.Unmarshal(raw, &item) != nil || item.Type != "reasoning" {
		return ""
	}
	parts := make([]string, 0, len(item.Content)+len(item.Summary)+1)
	appendPart := func(kind, text string) {
		if isReadableReasoningPart(kind) {
			appendReasoningTextPart(&parts, text)
		}
	}
	appendPart("output_text", item.Text)
	for _, part := range item.Content {
		appendPart(part.Type, part.Text)
	}
	for _, part := range item.Summary {
		appendPart(part.Type, part.Text)
	}
	return strings.Join(parts, "\n")
}

func isReadableReasoningPart(kind string) bool {
	return kind == "reasoning_text" || kind == "summary_text" || kind == "output_text"
}

func appendReasoningTextPart(parts *[]string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	for _, existing := range *parts {
		if existing == value {
			return
		}
	}
	*parts = append(*parts, value)
}
