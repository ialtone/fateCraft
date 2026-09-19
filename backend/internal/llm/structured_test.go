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

func TestStructuredTransportMatrix(t *testing.T) {
	payload := `{"value":"ok"}`
	for _, protocol := range []Protocol{ProtocolChatCompletions, ProtocolResponses} {
		for _, stream := range []bool{false, true} {
			name := fmt.Sprintf("%s/stream=%v", protocol, stream)
			t.Run(name, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if protocol == ProtocolResponses && r.URL.Path != "/responses" || protocol == ProtocolChatCompletions && r.URL.Path != "/chat/completions" {
						t.Errorf("path = %s", r.URL.Path)
					}
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					assertJSONSchemaRequest(t, body, protocol)
					w.Header().Set("Content-Type", map[bool]string{true: "text/event-stream", false: "application/json"}[stream])
					writeJSONSchemaFixture(w, protocol, stream, payload)
				}))
				defer server.Close()
				client := NewClient()
				params := Params{
					BaseURL: server.URL, APIKey: "test", Model: "model", Protocol: protocol, MaxTokens: 128,
					StructuredOutput: testStructuredOutput(),
				}
				var result StructuredResult
				var err error
				var deltas strings.Builder
				if stream {
					result, err = client.StreamStructured(context.Background(), params, nil, func(delta string) { deltas.WriteString(delta) })
				} else {
					result, err = client.CompleteStructured(context.Background(), params, nil)
				}
				if err != nil {
					t.Fatal(err)
				}
				if result.Payload != payload {
					t.Fatalf("payload = %q", result.Payload)
				}
				if stream && deltas.String() != payload {
					t.Fatalf("deltas = %q", deltas.String())
				}
			})
		}
	}
}

func testStructuredOutput() *StructuredOutput {
	return &StructuredOutput{Mode: OutputModeNativeJSONSchema, Schema: &JSONSchema{Name: "test_contract", Strict: true, Schema: map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"value"},
		"properties": map[string]any{"value": map[string]any{"type": "string"}},
	}}}
}

func assertJSONSchemaRequest(t *testing.T, body map[string]any, protocol Protocol) {
	t.Helper()
	if _, exists := body["tools"]; exists {
		t.Fatal("JSON Schema request unexpectedly contains tools")
	}
	if protocol == ProtocolChatCompletions {
		format := body["response_format"].(map[string]any)
		if format["type"] != "json_schema" || format["json_schema"].(map[string]any)["strict"] != true {
			t.Fatalf("chat response_format = %#v", format)
		}
		return
	}
	format := body["text"].(map[string]any)["format"].(map[string]any)
	if format["type"] != "json_schema" || format["strict"] != true {
		t.Fatalf("responses text.format = %#v", format)
	}
}

func writeJSONSchemaFixture(w http.ResponseWriter, protocol Protocol, stream bool, payload string) {
	if protocol == ProtocolChatCompletions {
		if stream {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", payload)
			return
		}
		fmt.Fprintf(w, `{"choices":[{"message":{"content":%q},"finish_reason":"stop"}]}`, payload)
		return
	}
	if stream {
		fmt.Fprintf(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":%q}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[]}}\n\n", payload)
		return
	}
	fmt.Fprintf(w, `{"status":"completed","output_text":%q,"output":[]}`, payload)
}

func TestJSONSchemaDeliveryRejectsToolCall(t *testing.T) {
	var call chatToolCall
	call.Function.Name = "unexpected"
	call.Function.Arguments = `{}`
	_, err := validateStructuredDelivery(testStructuredOutput(), "", []chatToolCall{call})
	if StructuredErrorCode(err) != "structured_output.invalid_payload" {
		t.Fatalf("error = %v", err)
	}
}

func TestStructuredChatStreamRejectsMalformedJSON(t *testing.T) {
	_, err := parseStructuredChatStream(strings.NewReader("data: {not-json}\n\n"), testStructuredOutput(), nil)
	if err == nil || !strings.Contains(err.Error(), "流式 JSON 解析失败") {
		t.Fatalf("err = %v", err)
	}
}

func TestStructuredChatStreamRequiresDoneMarkerOrFinishReason(t *testing.T) {
	// 既无 [DONE] 也无 finish_reason：流被截断。
	_, err := parseStructuredChatStream(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"), testStructuredOutput(), nil)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err = %v", err)
	}
	// 有 finish_reason 的干净 EOF 视为完整；后续按 Schema 校验 payload。
	_, err = parseStructuredChatStream(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"},\"finish_reason\":\"stop\"}]}\n\n"), testStructuredOutput(), nil)
	if errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("finish_reason without [DONE] must not be treated as truncated: %v", err)
	}
}

func TestStructuredResponsesStreamRejectsMalformedJSON(t *testing.T) {
	_, err := parseStructuredResponsesStream(strings.NewReader("data: {not-json}\n\n"), testStructuredOutput(), nil)
	if err == nil || !strings.Contains(err.Error(), "流式 JSON 解析失败") {
		t.Fatalf("err = %v", err)
	}
}

func TestStructuredResponsesStreamRequiresTerminalEvent(t *testing.T) {
	_, err := parseStructuredResponsesStream(strings.NewReader("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"), testStructuredOutput(), nil)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err = %v", err)
	}
}

func TestStructuredResponsesDoneMarkerAloneIsNotTerminal(t *testing.T) {
	_, err := parseStructuredResponsesStream(strings.NewReader("data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\ndata: [DONE]\n\n"), testStructuredOutput(), nil)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err = %v", err)
	}
}

func TestStructuredResponsesStreamReportsTruncation(t *testing.T) {
	sse := "event: response.incomplete\ndata: {\"type\":\"response.incomplete\",\"response\":{\"status\":\"incomplete\",\"incomplete_details\":{\"reason\":\"max_output_tokens\"}}}\n\n"
	_, err := parseStructuredResponsesStream(strings.NewReader(sse), testStructuredOutput(), nil)
	if !errors.Is(err, ErrTruncated) {
		t.Fatalf("err = %v, want ErrTruncated", err)
	}
}

func TestStructuredResponsesStreamMergesTerminalTextProjection(t *testing.T) {
	delta := `{"value":"前`
	final := `{"value":"前后"}`
	sse := fmt.Sprintf("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":%q}\n\n"+
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output_text\":%q}}\n\n", delta, final)
	var deltas strings.Builder
	result, err := parseStructuredResponsesStream(strings.NewReader(sse), testStructuredOutput(), func(delta string) {
		deltas.WriteString(delta)
	})
	if err != nil || result.Payload != `{"value":"前后"}` {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if deltas.String() != `{"value":"前后"}` {
		t.Fatalf("deltas=%q", deltas.String())
	}
}
