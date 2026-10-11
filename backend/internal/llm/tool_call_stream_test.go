package llm

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func toolStream(events ...map[string]any) string {
	var stream strings.Builder
	for _, event := range events {
		raw, _ := json.Marshal(event)
		stream.WriteString("data: " + string(raw) + "\n\n")
	}
	return stream.String()
}

func responseToolEvent(kind string, index int, itemID, callID, name, arguments string) map[string]any {
	if kind == "response.output_item.added" || kind == "response.output_item.done" {
		return map[string]any{"type": kind, "output_index": index, "item": map[string]any{"type": "function_call", "id": itemID, "call_id": callID, "name": name, "arguments": arguments}}
	}
	field := "delta"
	if kind == "response.function_call_arguments.done" {
		field = "arguments"
	}
	return map[string]any{"type": kind, "output_index": index, "item_id": itemID, "call_id": callID, "name": name, field: arguments}
}

func responseFinished(items ...map[string]any) map[string]any {
	return map[string]any{"type": "response.completed", "response": map[string]any{"status": "completed", "output": items}}
}

func TestResponsesStreamMergesChangedItemIDAndDelayedCallID(t *testing.T) {
	for _, earlyID := range []string{"call-1", ""} {
		sse := toolStream(
			responseToolEvent("response.output_item.added", 0, "early-item", earlyID, "fc_test", ""),
			responseToolEvent("response.function_call_arguments.delta", 0, "early-item", earlyID, "", `{"value":`),
			responseToolEvent("response.function_call_arguments.delta", 0, "early-item", "call-1", "", `"ok"}`),
			responseToolEvent("response.function_call_arguments.done", 0, "early-item", "call-1", "", `{"value":"ok"}`),
			responseToolEvent("response.output_item.done", 0, "early-item", "call-1", "fc_test", `{"value":"ok"}`),
			responseFinished(map[string]any{"type": "function_call", "id": "changed-final-item", "call_id": "call-1", "name": "fc_test", "arguments": `{"value":"ok"}`}),
		)
		started := 0
		result, _, done, err := parseAgentResponsesStream(strings.NewReader(sse), func(e AgentEvent) {
			if e.Kind == AgentEventToolCallStarted {
				started++
			}
		}, true)
		if err != nil || !done || len(result.ToolCalls) != 1 || started != 1 {
			t.Fatalf("early ID %q: calls=%+v starts=%d err=%v", earlyID, result.ToolCalls, started, err)
		}
		call := result.ToolCalls[0]
		if call.ID != "call-1" || call.Arguments != `{"value":"ok"}` || call.Name != "fc_test" {
			t.Fatalf("call=%+v", call)
		}
	}
}

func TestResponsesStreamRejectsConflictingToolProjections(t *testing.T) {
	for _, tc := range []struct {
		name  string
		final map[string]any
	}{
		{"name", responseToolEvent("response.output_item.done", 0, "item-1", "call-1", "fc_other", `{}`)},
		{"arguments", responseToolEvent("response.function_call_arguments.done", 0, "item-1", "call-1", "fc_test", `{"changed":true}`)},
		{"identity", responseToolEvent("response.output_item.done", 0, "item-1", "different-call", "fc_test", `{}`)},
		{"duplicate ID on another output", responseToolEvent("response.output_item.added", 1, "item-2", "call-1", "fc_test", `{}`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sse := toolStream(responseToolEvent("response.output_item.added", 0, "item-1", "call-1", "fc_test", `{}`), tc.final, responseFinished())
			_, _, _, err := parseAgentResponsesStream(strings.NewReader(sse), nil, false)
			if !errors.Is(err, ErrToolCallConflict) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestResponsesStreamKeepsInterleavedCallsSeparateAndOrdered(t *testing.T) {
	sse := toolStream(
		responseToolEvent("response.output_item.added", 1, "item-b", "call-b", "fc_b", ""),
		responseToolEvent("response.output_item.added", 0, "item-a", "", "fc_a", ""),
		responseToolEvent("response.function_call_arguments.delta", 1, "item-b", "call-b", "", `{"b":2}`),
		responseToolEvent("response.function_call_arguments.delta", 0, "item-a", "call-a", "", `{"a":1}`),
		responseFinished(map[string]any{"type": "function_call", "id": "final-a", "call_id": "call-a", "name": "fc_a", "arguments": `{"a":1}`}, map[string]any{"type": "function_call", "id": "final-b", "call_id": "call-b", "name": "fc_b", "arguments": `{"b":2}`}),
	)
	result, _, _, err := parseAgentResponsesStream(strings.NewReader(sse), nil, false)
	if err != nil || len(result.ToolCalls) != 2 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if result.ToolCalls[0].ID != "call-a" || result.ToolCalls[1].ID != "call-b" || result.ToolCalls[0].Arguments != `{"a":1}` || result.ToolCalls[1].Arguments != `{"b":2}` {
		t.Fatalf("calls=%+v", result.ToolCalls)
	}
}

func chatToolDelta(index int, id, name, args string) map[string]any {
	return map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"tool_calls": []any{map[string]any{"index": index, "id": id, "function": map[string]any{"name": name, "arguments": args}}}}}}}
}

func TestChatStreamRepeatingIdentityDoesNotDuplicateNameOrArguments(t *testing.T) {
	sse := toolStream(chatToolDelta(0, "call-a", "fc_a", `{"a":`), chatToolDelta(1, "call-b", "fc_b", `{}`), chatToolDelta(0, "call-a", "fc_a", `1}`)) + "data: [DONE]\n\n"
	result, _, _, err := parseAgentChatStream(strings.NewReader(sse), nil, false)
	if err != nil || len(result.ToolCalls) != 2 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if result.ToolCalls[0].Name != "fc_a" || result.ToolCalls[0].Arguments != `{"a":1}` || result.ToolCalls[1].ID != "call-b" {
		t.Fatalf("calls=%+v", result.ToolCalls)
	}
}

func TestChatStreamRejectsChangedIdentityOrNameAtSameIndex(t *testing.T) {
	for _, delta := range []map[string]any{chatToolDelta(0, "changed", "fc_a", ""), chatToolDelta(0, "call-a", "fc_other", "")} {
		sse := toolStream(chatToolDelta(0, "call-a", "fc_a", `{}`), delta) + "data: [DONE]\n\n"
		_, _, _, err := parseAgentChatStream(strings.NewReader(sse), nil, false)
		if !errors.Is(err, ErrToolCallConflict) {
			t.Fatalf("err=%v", err)
		}
	}
}

func TestToolArgumentsFinalProjectionReconcilesWithoutOverwritingConflicts(t *testing.T) {
	for _, tc := range []struct {
		initial, final string
		conflict       bool
	}{
		{`{"a":`, `{"a":1}`, false},
		{`{"a":1,"b":2}`, `{"b":2, "a":1}`, false},
		{`{"a":1}`, `{"a":2}`, true},
		{`{"a":`, `{"b":2}`, true},
	} {
		call := &accumulatedAgentCall{}
		call.arguments.WriteString(tc.initial)
		err := reconcileToolArguments(call, tc.final)
		if tc.conflict {
			if !errors.Is(err, ErrToolCallConflict) || call.arguments.String() != tc.initial {
				t.Fatalf("conflict overwrote arguments: %q %v", call.arguments.String(), err)
			}
		} else if err != nil || call.arguments.String() != tc.final {
			t.Fatalf("final projection=%q err=%v", call.arguments.String(), err)
		}
	}
}

func TestResponsesDoesNotInventMissingCallID(t *testing.T) {
	sse := toolStream(responseToolEvent("response.output_item.done", 0, "item-1", "", "fc_test", `{}`), responseFinished())
	result, _, _, err := parseAgentResponsesStream(strings.NewReader(sse), nil, false)
	if err != nil || len(result.ToolCalls) != 1 || result.ToolCalls[0].ID != "" {
		t.Fatalf("missing call ID replaced: %+v %v", result, err)
	}
	// The orchestration batch validator must reject this missing identity;
	// item ID or output position must never become an execution/idempotency ID.
}
