package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAgentTransportMatrix(t *testing.T) {
	for _, protocol := range []Protocol{ProtocolChatCompletions, ProtocolResponses} {
		for _, stream := range []bool{false, true} {
			for _, lane := range []string{"text", "tool"} {
				name := fmt.Sprintf("%s/stream=%v/%s", protocol, stream, lane)
				t.Run(name, func(t *testing.T) {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						wantPath := "/chat/completions"
						if protocol == ProtocolResponses {
							wantPath = "/responses"
						}
						if r.URL.Path != wantPath {
							t.Errorf("path = %q, want %q", r.URL.Path, wantPath)
						}
						var body map[string]any
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Fatal(err)
						}
						assertAgentRequest(t, body, protocol, stream)
						writeAgentFixture(w, protocol, stream, lane)
					}))
					defer server.Close()

					var events []AgentEvent
					// 测试端点是本地 httptest 地址，按兼容网关处理：不发
					// parallel_tool_calls、stream_options 与 strict。
					result, err := NewClient().AgentStep(context.Background(), Params{
						BaseURL: server.URL, APIKey: "test", Model: "agent-model", Protocol: protocol,
					}, []Message{{Role: "user", Content: "continue"}}, testAgentTools(), stream, func(event AgentEvent) {
						events = append(events, event)
					})
					if err != nil {
						t.Fatal(err)
					}
					if lane == "text" {
						if result.Text != "叙事完成" || len(result.ToolCalls) != 0 {
							t.Fatalf("result = %+v", result)
						}
						assertAgentEventKinds(t, events, AgentEventTextDelta, AgentEventStepCompleted)
						return
					}
					if result.Text != "" || len(result.ToolCalls) != 1 || result.ToolCalls[0].ID != "call-1" ||
						result.ToolCalls[0].Name != "fc_test" || result.ToolCalls[0].Arguments != `{"value":"ok"}` {
						t.Fatalf("result = %+v", result)
					}
					if events[0].Kind != AgentEventToolCallStarted || events[len(events)-1].Kind != AgentEventStepCompleted {
						t.Fatalf("events = %+v", events)
					}
				})
			}
		}
	}
}

func TestAgentStreamPreservesTextAndToolArrivalOrder(t *testing.T) {
	tests := []struct {
		name string
		sse  string
		want []AgentEventKind
	}{
		{
			name: "text before tool",
			sse: "data: {\"choices\":[{\"delta\":{\"content\":\"draft\"}}]}\n\n" +
				"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"function\":{\"name\":\"fc_test\",\"arguments\":\"{}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n" +
				"data: [DONE]\n\n",
			want: []AgentEventKind{AgentEventTextDelta, AgentEventToolCallStarted, AgentEventToolArguments, AgentEventToolCallCompleted, AgentEventStepCompleted},
		},
		{
			name: "tool before text",
			sse: "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"function\":{\"name\":\"fc_test\",\"arguments\":\"{}\"}}]}}]}\n\n" +
				"data: {\"choices\":[{\"delta\":{\"content\":\"draft\"},\"finish_reason\":\"tool_calls\"}]}\n\n" +
				"data: [DONE]\n\n",
			want: []AgentEventKind{AgentEventToolCallStarted, AgentEventToolArguments, AgentEventTextDelta, AgentEventToolCallCompleted, AgentEventStepCompleted},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var events []AgentEvent
			result, _, done, err := parseAgentChatStream(strings.NewReader(tt.sse), func(event AgentEvent) {
				events = append(events, event)
			})
			if err != nil || !done || result.Text != "draft" || len(result.ToolCalls) != 1 {
				t.Fatalf("result=%+v done=%v err=%v", result, done, err)
			}
			if len(events) != len(tt.want) {
				t.Fatalf("events = %+v", events)
			}
			for index, kind := range tt.want {
				if events[index].Kind != kind {
					t.Fatalf("event %d = %s, want %s", index, events[index].Kind, kind)
				}
			}
		})
	}
}

func TestChatReasoningArrivesBeforeText(t *testing.T) {
	sse := "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"先检查\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"叙事\"}}]}\n\n" +
		"data: [DONE]\n\n"
	var events []AgentEvent
	result, _, done, err := parseAgentChatStream(strings.NewReader(sse), func(event AgentEvent) {
		events = append(events, event)
	})
	if err != nil || !done || result.ReasoningContent != "先检查" || result.Text != "叙事" {
		t.Fatalf("result=%+v done=%v err=%v", result, done, err)
	}
	want := []AgentEventKind{AgentEventReasoningDelta, AgentEventTextDelta, AgentEventStepCompleted}
	assertAgentEventKinds(t, events, want...)
}

func TestAgentEdgeCases(t *testing.T) {
	t.Run("multiple tools are preserved for coordinator rejection", func(t *testing.T) {
		raw := []byte(`{"choices":[{"message":{"tool_calls":[{"id":"a","function":{"name":"fc_test","arguments":"{}"}},{"id":"b","function":{"name":"fc_test","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
		result, err := parseAgentChatComplete(raw, nil)
		if err != nil || len(result.ToolCalls) != 2 {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	})

	t.Run("stream eof", func(t *testing.T) {
		_, _, done, err := parseAgentChatStream(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"), nil)
		if done || !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("done=%v err=%v", done, err)
		}
	})

	t.Run("stream malformed json", func(t *testing.T) {
		_, _, done, err := parseAgentChatStream(strings.NewReader("data: {not-json}\n\ndata: [DONE]\n\n"), nil)
		if done || err == nil || !strings.Contains(err.Error(), "流式 JSON 解析失败") {
			t.Fatalf("done=%v err=%v", done, err)
		}
	})

	t.Run("stream finish reason without done marker is accepted", func(t *testing.T) {
		// 部分兼容网关在 finish_reason 之后直接关流、不发 [DONE]；
		// 已拿到 finish_reason 的干净 EOF 必须视为完整响应。
		result, _, done, err := parseAgentChatStream(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"完整\"},\"finish_reason\":\"stop\"}]}\n\n"), nil)
		if !done || err != nil || result.Text != "完整" || result.FinishReason != "stop" {
			t.Fatalf("done=%v err=%v result=%+v", done, err, result)
		}
	})

	t.Run("custom gateway does not receive optional tool controls", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if _, exists := body["parallel_tool_calls"]; exists {
				t.Errorf("custom gateway must not receive parallel_tool_calls: %#v", body)
			}
			if _, exists := body["stream_options"]; exists {
				t.Errorf("custom gateway must not receive stream_options: %#v", body)
			}
			tools := body["tools"].([]any)
			function := tools[0].(map[string]any)["function"].(map[string]any)
			if _, exists := function["strict"]; exists {
				t.Errorf("custom gateway must not receive strict: %#v", function)
			}
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		}))
		defer server.Close()
		tools := testAgentTools()
		tools[0].Strict = true
		if _, err := NewClient().AgentStep(context.Background(), Params{
			BaseURL: server.URL, APIKey: "test", Model: "m", Protocol: ProtocolChatCompletions,
		}, []Message{{Role: "user", Content: "hi"}}, tools, true, nil); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("empty response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `{"choices":[{"message":{},"finish_reason":"stop"}]}`)
		}))
		defer server.Close()
		_, err := NewClient().AgentStep(context.Background(), Params{BaseURL: server.URL, APIKey: "test", Model: "m", Protocol: ProtocolChatCompletions}, nil, nil, false, nil)
		if err == nil || !strings.Contains(err.Error(), "为空") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("truncated response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"message","content":[{"type":"output_text","text":"partial"}]}]}`)
		}))
		defer server.Close()
		result, err := NewClient().AgentStep(context.Background(), Params{
			BaseURL: server.URL, APIKey: "test", Model: "m", Protocol: ProtocolResponses,
		}, nil, nil, false, nil)
		if !errors.Is(err, ErrTruncated) || result.Text != "partial" {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	})
}

func TestAgentParsersCaptureUsage(t *testing.T) {
	t.Run("chat complete", func(t *testing.T) {
		raw := []byte(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":120,"completion_tokens":30}}`)
		result, err := parseAgentChatComplete(raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		if result.Usage.InputTokens != 120 || result.Usage.OutputTokens != 30 {
			t.Fatalf("usage = %#v", result.Usage)
		}
	})

	t.Run("chat stream", func(t *testing.T) {
		sse := "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\n" +
			"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":80,\"completion_tokens\":20}}\n\n" +
			"data: [DONE]\n\n"
		result, _, _, err := parseAgentChatStream(strings.NewReader(sse), nil)
		if err != nil {
			t.Fatal(err)
		}
		if result.Usage.InputTokens != 80 || result.Usage.OutputTokens != 20 {
			t.Fatalf("usage = %#v", result.Usage)
		}
	})

	t.Run("responses stream", func(t *testing.T) {
		sse := "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output_text\":\"ok\",\"usage\":{\"input_tokens\":90,\"output_tokens\":25}}}\n\n"
		result, _, _, err := parseAgentResponsesStream(strings.NewReader(sse), nil)
		if err != nil {
			t.Fatal(err)
		}
		if result.Usage.InputTokens != 90 || result.Usage.OutputTokens != 25 {
			t.Fatalf("usage = %#v", result.Usage)
		}
	})
}

func TestResponsesReasoningItemsRoundTrip(t *testing.T) {
	raw := []byte(`{"status":"completed","output":[{"type":"reasoning","id":"rs-1","status":"completed","content":[{"type":"reasoning_text","text":"先检查状态"}]},{"type":"function_call","id":"item-1","call_id":"call-1","name":"fc_test","arguments":"{}"}]}`)
	result, err := parseAgentResponsesComplete(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.ReasoningItems) != 1 {
		t.Fatalf("reasoning items = %d, want 1", len(result.ReasoningItems))
	}
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].ItemID != "item-1" {
		t.Fatalf("tool call item id = %#v, want item-1", result.ToolCalls)
	}
	inputs := responsesAgentInputs([]Message{
		{Role: "assistant", ReasoningItems: result.ReasoningItems, ToolCalls: result.ToolCalls},
		{Role: "tool", ToolCallID: "call-1", Content: `{"ok":true}`},
	})
	encoded, err := json.Marshal(inputs)
	if err != nil {
		t.Fatal(err)
	}
	var values []map[string]any
	if err := json.Unmarshal(encoded, &values); err != nil {
		t.Fatal(err)
	}
	if len(values) != 3 || values[0]["type"] != "reasoning" || values[1]["type"] != "function_call" || values[1]["id"] != "item-1" || values[2]["type"] != "function_call_output" {
		t.Fatalf("inputs = %#v", values)
	}
	content, ok := values[0]["content"].([]any)
	if !ok || len(content) != 1 || content[0].(map[string]any)["type"] != "reasoning_text" {
		t.Fatalf("reasoning input = %#v", values[0])
	}
}

func TestResponsesCompleteEmitsReasoningSummary(t *testing.T) {
	raw := []byte(`{"status":"completed","output":[{"type":"reasoning","id":"rs-1","summary":[{"type":"summary_text","text":"检查状态"}]},{"type":"message","content":[{"type":"output_text","text":"叙事"}]}]}`)
	var events []AgentEvent
	result, err := parseAgentResponsesComplete(raw, func(event AgentEvent) {
		events = append(events, event)
	})
	if err != nil || result.Text != "叙事" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	want := []AgentEventKind{AgentEventReasoningDelta, AgentEventTextDelta, AgentEventStepCompleted}
	assertAgentEventKinds(t, events, want...)
}

func TestResponsesStreamCapturesReasoningItem(t *testing.T) {
	sse := "event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"reasoning\",\"id\":\"rs-1\",\"status\":\"in_progress\",\"content\":[]}}\n\n" +
		"event: response.reasoning_text.done\ndata: {\"type\":\"response.reasoning_text.done\",\"item_id\":\"rs-1\",\"text\":\"检查完成\"}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"reasoning\",\"id\":\"rs-1\",\"status\":\"completed\",\"content\":[{\"type\":\"reasoning_text\",\"text\":\"检查完成\"}]}}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":1,\"item\":{\"type\":\"function_call\",\"id\":\"item-1\",\"call_id\":\"call-1\",\"name\":\"fc_test\",\"arguments\":\"{}\"}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n"
	result, _, done, err := parseAgentResponsesStream(strings.NewReader(sse), nil)
	if err != nil || !done {
		t.Fatalf("result=%+v done=%v err=%v", result, done, err)
	}
	if len(result.ReasoningItems) != 1 || len(result.ToolCalls) != 1 {
		t.Fatalf("result = %+v", result)
	}
	if result.ToolCalls[0].ItemID != "item-1" {
		t.Fatalf("tool call item id = %q, want item-1", result.ToolCalls[0].ItemID)
	}
	if !strings.Contains(string(result.ReasoningItems[0]), "reasoning_text") {
		t.Fatalf("reasoning item = %s", result.ReasoningItems[0])
	}
}

func TestResponsesReasoningDeltaArrivesBeforeText(t *testing.T) {
	sse := "event: response.reasoning_text.delta\ndata: {\"type\":\"response.reasoning_text.delta\",\"item_id\":\"rs-1\",\"delta\":\"先检查\"}\n\n" +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"叙事\"}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n"
	var events []AgentEvent
	result, _, done, err := parseAgentResponsesStream(strings.NewReader(sse), func(event AgentEvent) {
		events = append(events, event)
	})
	if err != nil || !done || result.Text != "叙事" {
		t.Fatalf("result=%+v done=%v err=%v", result, done, err)
	}
	want := []AgentEventKind{AgentEventReasoningDelta, AgentEventTextDelta, AgentEventStepCompleted}
	assertAgentEventKinds(t, events, want...)
}

func TestResponsesStreamUsesTerminalMessageWhenNoTextDelta(t *testing.T) {
	sse := "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"终止正文\"}]}]}}\n\n"
	var events []AgentEvent
	result, _, done, err := parseAgentResponsesStream(strings.NewReader(sse), func(event AgentEvent) {
		events = append(events, event)
	})
	if err != nil || !done || result.Text != "终止正文" {
		t.Fatalf("result=%+v done=%v err=%v", result, done, err)
	}
	want := []AgentEventKind{AgentEventTextDelta, AgentEventStepCompleted}
	assertAgentEventKinds(t, events, want...)
}

func TestResponsesStreamRejectsMalformedJSON(t *testing.T) {
	result, _, done, err := parseAgentResponsesStream(strings.NewReader("data: {not-json}\n\n"), nil)
	if done || err == nil || !strings.Contains(err.Error(), "流式 JSON 解析失败") {
		t.Fatalf("result=%+v done=%v err=%v", result, done, err)
	}
}

func TestResponsesStreamRequiresTerminalEvent(t *testing.T) {
	result, _, done, err := parseAgentResponsesStream(strings.NewReader("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"), nil)
	if done || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("result=%+v done=%v err=%v", result, done, err)
	}
}

func TestResponsesStreamCompletesPartialTextFromTerminalProjection(t *testing.T) {
	sse := "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"前半\"}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output_text\":\"前半后半\"}}\n\n"
	var events []AgentEvent
	result, _, done, err := parseAgentResponsesStream(strings.NewReader(sse), func(event AgentEvent) {
		events = append(events, event)
	})
	if err != nil || !done || result.Text != "前半后半" {
		t.Fatalf("result=%+v done=%v err=%v", result, done, err)
	}
	want := []AgentEventKind{AgentEventTextDelta, AgentEventTextDelta, AgentEventStepCompleted}
	assertAgentEventKinds(t, events, want...)
	if events[1].Text != "后半" {
		t.Fatalf("terminal delta = %q, want 后半", events[1].Text)
	}
}

func TestResponsesStreamCapturesTerminalFunctionCall(t *testing.T) {
	sse := "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"function_call\",\"id\":\"item-1\",\"call_id\":\"call-1\",\"name\":\"fc_test\",\"arguments\":\"{\\\"value\\\":\\\"ok\\\"}\"}]}}\n\n"
	var events []AgentEvent
	result, _, done, err := parseAgentResponsesStream(strings.NewReader(sse), func(event AgentEvent) {
		events = append(events, event)
	})
	if err != nil || !done || len(result.ToolCalls) != 1 {
		t.Fatalf("result=%+v done=%v err=%v", result, done, err)
	}
	call := result.ToolCalls[0]
	if call.ID != "call-1" || call.Name != "fc_test" || call.Arguments != `{"value":"ok"}` {
		t.Fatalf("tool call = %+v", call)
	}
	want := []AgentEventKind{AgentEventToolCallStarted, AgentEventToolCallCompleted, AgentEventStepCompleted}
	assertAgentEventKinds(t, events, want...)
}

func TestResponsesStreamMergesReasoningDeltaIntoEmptyFinalItem(t *testing.T) {
	sse := "event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"reasoning\",\"id\":\"rs-1\",\"status\":\"in_progress\",\"content\":[]}}\n\n" +
		"event: response.reasoning_text.delta\ndata: {\"type\":\"response.reasoning_text.delta\",\"item_id\":\"rs-1\",\"delta\":\"检查\"}\n\n" +
		"event: response.reasoning_text.done\ndata: {\"type\":\"response.reasoning_text.done\",\"item_id\":\"rs-1\",\"text\":\"检查完成\"}\n\n" +
		"event: response.output_item.done\ndata: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"reasoning\",\"id\":\"rs-1\",\"status\":\"completed\",\"content\":[],\"encrypted_content\":\"opaque\"}}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n"
	result, _, done, err := parseAgentResponsesStream(strings.NewReader(sse), nil)
	if err != nil || !done {
		t.Fatalf("result=%+v done=%v err=%v", result, done, err)
	}
	if len(result.ReasoningItems) != 1 {
		t.Fatalf("reasoning items = %d", len(result.ReasoningItems))
	}
	var item map[string]any
	if err := json.Unmarshal(result.ReasoningItems[0], &item); err != nil {
		t.Fatal(err)
	}
	if item["encrypted_content"] != "opaque" {
		t.Fatalf("provider field was lost: %#v", item)
	}
	content, ok := item["content"].([]any)
	if !ok || len(content) != 1 || content[0].(map[string]any)["text"] != "检查完成" {
		t.Fatalf("reasoning content = %#v", item["content"])
	}
}

func TestChatReasoningContentRoundTrip(t *testing.T) {
	raw := []byte(`{"choices":[{"message":{"role":"assistant","reasoning_content":"先检查状态","tool_calls":[{"id":"call-1","type":"function","function":{"name":"fc_test","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
	result, err := parseAgentChatComplete(raw, nil)
	if err != nil || result.ReasoningContent != "先检查状态" || len(result.ToolCalls) != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	encoded, err := json.Marshal(chatAgentMessages([]Message{{
		Role: "assistant", ReasoningContent: result.ReasoningContent, ToolCalls: result.ToolCalls,
	}}))
	if err != nil || !strings.Contains(string(encoded), `"reasoning_content":"先检查状态"`) {
		t.Fatalf("messages=%s err=%v", encoded, err)
	}
}

func testAgentTools() []ToolDefinition {
	return []ToolDefinition{{
		Name: "fc_test", Description: "test", Strict: true,
		Parameters: map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"value"},
			"properties": map[string]any{"value": map[string]any{"type": "string"}},
		},
	}}
}

func assertAgentRequest(t *testing.T, body map[string]any, protocol Protocol, stream bool) {
	t.Helper()
	if body["model"] != "agent-model" || body["stream"] != stream {
		t.Fatalf("request = %#v", body)
	}
	tools, ok := body["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v", body["tools"])
	}
	// 测试端点是本地地址，按兼容网关处理：可选工具控制字段一律不发。
	if _, exists := body["parallel_tool_calls"]; exists {
		t.Fatalf("%s must not receive parallel_tool_calls: %#v", protocol, body)
	}
	if _, exists := body["stream_options"]; exists {
		t.Fatalf("%s must not receive stream_options: %#v", protocol, body)
	}
	if _, exists := body["tool_choice"]; exists {
		t.Fatalf("tool_choice should use provider default auto semantics: %#v", body["tool_choice"])
	}
}

func writeAgentFixture(w http.ResponseWriter, protocol Protocol, stream bool, lane string) {
	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
	}
	if protocol == ProtocolChatCompletions {
		if !stream && lane == "text" {
			fmt.Fprint(w, `{"choices":[{"message":{"content":"叙事完成"},"finish_reason":"stop"}]}`)
			return
		}
		if !stream {
			fmt.Fprint(w, `{"choices":[{"message":{"tool_calls":[{"id":"call-1","type":"function","function":{"name":"fc_test","arguments":"{\"value\":\"ok\"}"}}]},"finish_reason":"tool_calls"}]}`)
			return
		}
		if lane == "text" {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"叙事完成\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"function\":{\"name\":\"fc_test\",\"arguments\":\"{\\\"value\\\":\\\"ok\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
		return
	}
	if !stream && lane == "text" {
		fmt.Fprint(w, `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"叙事完成"}]}]}`)
		return
	}
	if !stream {
		fmt.Fprint(w, `{"status":"completed","output":[{"type":"function_call","id":"item-1","call_id":"call-1","name":"fc_test","arguments":"{\"value\":\"ok\"}"}]}`)
		return
	}
	if lane == "text" {
		fmt.Fprint(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"叙事完成\"}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n")
		return
	}
	fmt.Fprint(w, "event: response.output_item.added\ndata: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"function_call\",\"id\":\"item-1\",\"call_id\":\"call-1\",\"name\":\"fc_test\"}}\n\nevent: response.function_call_arguments.delta\ndata: {\"type\":\"response.function_call_arguments.delta\",\"output_index\":0,\"item_id\":\"item-1\",\"call_id\":\"call-1\",\"delta\":\"{\\\"value\\\":\\\"ok\\\"}\"}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n")
}

func assertAgentEventKinds(t *testing.T, events []AgentEvent, want ...AgentEventKind) {
	t.Helper()
	if len(events) != len(want) {
		t.Fatalf("events = %+v", events)
	}
	for index, kind := range want {
		if events[index].Kind != kind {
			t.Fatalf("event %d = %s, want %s", index, events[index].Kind, kind)
		}
	}
}
