package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func replayToolCall(id, name, args string) Message {
	return Message{Role: "assistant", Blocks: []ContentBlock{{
		Kind:     ContentToolCall,
		ToolCall: &ToolCall{ID: id, Name: name, Arguments: args},
	}}}
}

func replayToolResult(id, content string) Message {
	return Message{Role: "tool", ToolCallID: id, Content: content}
}

func TestPrepareToolReplayKeepsCompletePair(t *testing.T) {
	result, err := PrepareToolReplay([]Message{
		replayToolCall("call-1", "fc_test", `{}`),
		replayToolResult("call-1", `{"ok":true}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 2 || len(result.Repairs) != 0 {
		t.Fatalf("messages=%d repairs=%+v", len(result.Messages), result.Repairs)
	}
}

func TestPrepareToolReplaySynthesizesMissingOutputBeforeNextTurn(t *testing.T) {
	result, err := PrepareToolReplay([]Message{
		replayToolCall("call-1", "fc_test", `{}`),
		{Role: "user", Content: "继续"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 3 || len(result.Repairs) != 1 || result.Repairs[0].Kind != ReplayRepairMissingOutput {
		t.Fatalf("messages=%+v repairs=%+v", result.Messages, result.Repairs)
	}
	if result.Messages[1].Role != "tool" || result.Messages[1].ToolCallID != "call-1" ||
		!strings.Contains(result.Messages[1].Content, "未执行") {
		t.Fatalf("synthetic result=%+v", result.Messages[1])
	}
}

func TestPrepareToolReplaySynthesizesOnlyMissingCalls(t *testing.T) {
	result, err := PrepareToolReplay([]Message{
		Message{Role: "assistant", Blocks: []ContentBlock{{
			Kind: ContentToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "fc_one", Arguments: `{}`},
		}, {
			Kind: ContentToolCall, ToolCall: &ToolCall{ID: "call-2", Name: "fc_two", Arguments: `{}`},
		}}},
		replayToolResult("call-2", `{"ok":true}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 3 || len(result.Repairs) != 1 || result.Repairs[0].CallID != "call-1" {
		t.Fatalf("messages=%+v repairs=%+v", result.Messages, result.Repairs)
	}
	if result.Messages[1].ToolCallID != "call-2" || result.Messages[2].ToolCallID != "call-1" {
		t.Fatalf("tool result order=%q,%q", result.Messages[1].ToolCallID, result.Messages[2].ToolCallID)
	}
}

func TestPrepareToolReplayKeepsResultsAcrossEmptyToolPlaceholders(t *testing.T) {
	result, err := PrepareToolReplay([]Message{
		Message{Role: "assistant", Blocks: []ContentBlock{
			{Kind: ContentToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "fc_one", Arguments: `{}`}},
			{Kind: ContentToolCall, ToolCall: &ToolCall{ID: "call-2", Name: "fc_two", Arguments: `{}`}},
		}},
		replayToolResult("call-1", `{"ok":true}`),
		{Role: "assistant"}, // second invocation's storage placeholder
		replayToolResult("call-2", `{"ok":true}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 3 || len(result.Repairs) != 0 {
		t.Fatalf("messages=%+v repairs=%+v", result.Messages, result.Repairs)
	}
	if result.Messages[1].ToolCallID != "call-1" || result.Messages[2].ToolCallID != "call-2" {
		t.Fatalf("tool result order=%q,%q", result.Messages[1].ToolCallID, result.Messages[2].ToolCallID)
	}
}

func TestPrepareToolReplayDropsOrphanAndRepairsEmptyOutput(t *testing.T) {
	result, err := PrepareToolReplay([]Message{
		replayToolResult("missing", `{"ok":true}`),
		replayToolCall("call-1", "fc_test", `{}`),
		replayToolResult("call-1", ""),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 2 || len(result.Repairs) != 2 {
		t.Fatalf("messages=%+v repairs=%+v", result.Messages, result.Repairs)
	}
	if result.Messages[0].ToolCallID != "" || result.Messages[1].ToolCallID != "call-1" || !strings.Contains(result.Messages[1].Content, "返回为空") {
		t.Fatalf("messages=%+v", result.Messages)
	}
	if result.Repairs[0].Kind != ReplayRepairOrphanOutput || result.Repairs[1].Kind != ReplayRepairEmptyOutput {
		t.Fatalf("repairs=%+v", result.Repairs)
	}
}

func TestPrepareToolReplayRejectsDuplicateOrInvalidCallID(t *testing.T) {
	tests := []struct {
		name string
		in   []Message
	}{
		{
			name: "empty id",
			in:   []Message{replayToolCall("", "fc_test", `{}`)},
		},
		{
			name: "duplicate id",
			in: []Message{
				{Role: "assistant", Blocks: []ContentBlock{
					{Kind: ContentToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "fc_test", Arguments: `{}`}},
					{Kind: ContentToolCall, ToolCall: &ToolCall{ID: "call-1", Name: "fc_test", Arguments: `{"other":true}`}},
				}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := PrepareToolReplay(test.in); err == nil {
				t.Fatal("expected replay validation error")
			}
		})
	}
}

func TestPrepareToolReplayAllowsCompletedShortIDReuseAcrossTurns(t *testing.T) {
	result, err := PrepareToolReplay([]Message{
		replayToolCall("call-0", "fc_test", `{}`),
		replayToolResult("call-0", `{"ok":true}`),
		replayToolCall("call-0", "fc_test", `{}`),
		replayToolResult("call-0", `{"ok":true}`),
	})
	if err != nil || len(result.Messages) != 4 || len(result.Repairs) != 0 {
		t.Fatalf("messages=%+v repairs=%+v err=%v", result.Messages, result.Repairs, err)
	}
}

func TestAgentStepResponsesRepairsMissingOutputBeforeRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Input []map[string]any `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		var calls, outputs int
		for _, item := range body.Input {
			switch item["type"] {
			case "function_call":
				calls++
			case "function_call_output":
				outputs++
				if item["call_id"] != "call-1" || item["output"] == "" {
					t.Errorf("invalid synthetic output: %#v", item)
				}
			}
		}
		if calls != 1 || outputs != 1 {
			t.Errorf("input=%#v calls=%d outputs=%d", body.Input, calls, outputs)
		}
		fmt.Fprint(w, `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`)
	}))
	defer server.Close()

	_, err := NewClient().AgentStep(context.Background(), Params{
		BaseURL: server.URL, APIKey: "test", Model: "m", Protocol: ProtocolResponses,
	}, []Message{
		replayToolCall("call-1", "fc_test", `{}`),
		{Role: "user", Content: "继续"},
	}, []ToolDefinition{{Name: "fc_test", Parameters: map[string]any{"type": "object"}}}, false, nil)
	if err != nil {
		t.Fatal(err)
	}
}
