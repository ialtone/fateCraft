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
	"time"
)

type requestAuditCapture struct {
	items []RequestAudit
}

func (c *requestAuditCapture) write(_ context.Context, audit RequestAudit) error {
	c.items = append(c.items, audit)
	return nil
}

func streamServer(t *testing.T, lines []string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, line := range lines {
			fmt.Fprintf(w, "data: %s\n\n", line)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
}

func TestStreamReturnsErrTruncatedOnLengthFinish(t *testing.T) {
	server := streamServer(t, []string{
		`{"choices":[{"delta":{"content":"{\"narrative\":\"你"}}]}`,
		`{"choices":[{"delta":{"content":"缓步走入"},"finish_reason":"length"}]}`,
	})
	defer server.Close()

	var deltas []string
	full, err := NewClient().Stream(context.Background(), Params{BaseURL: server.URL, APIKey: "k", Model: "m", Protocol: ProtocolChatCompletions},
		[]Message{{Role: "user", Content: "hi"}}, func(delta string) { deltas = append(deltas, delta) })
	if !errors.Is(err, ErrTruncated) {
		t.Fatalf("err = %v, want ErrTruncated", err)
	}
	if full != `{"narrative":"你缓步走入` || len(deltas) != 2 {
		t.Fatalf("full = %q deltas = %v", full, deltas)
	}
}

func TestStreamCompletesNormally(t *testing.T) {
	server := streamServer(t, []string{
		`{"choices":[{"delta":{"content":"你好"}}]}`,
		`{"choices":[{"delta":{"content":"。"},"finish_reason":"stop"}]}`,
	})
	defer server.Close()

	full, err := NewClient().Stream(context.Background(), Params{BaseURL: server.URL, APIKey: "k", Model: "m", Protocol: ProtocolChatCompletions},
		[]Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil || full != "你好。" {
		t.Fatalf("full = %q err = %v", full, err)
	}
}

func TestStreamRejectsUnexpectedEOFWithoutTerminalEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"半份\"}}]}\n\n")
	}))
	defer server.Close()

	content, err := NewClient().Stream(context.Background(), Params{
		BaseURL: server.URL, APIKey: "k", Model: "m", Protocol: ProtocolChatCompletions,
		RetryPolicy: &RetryPolicy{MaxAttempts: 1},
	}, nil, nil)
	if !errors.Is(err, io.ErrUnexpectedEOF) || content != "半份" {
		t.Fatalf("content=%q err=%v, want unexpected EOF with diagnostic content", content, err)
	}
}

func TestStreamRejectsMalformedSSEJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {not-json}\n\n")
	}))
	defer server.Close()

	_, err := NewClient().Stream(context.Background(), Params{
		BaseURL: server.URL, APIKey: "k", Model: "m", Protocol: ProtocolChatCompletions,
		RetryPolicy: &RetryPolicy{MaxAttempts: 1},
	}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "流式 JSON 解析失败") {
		t.Fatalf("err=%v, want explicit stream parse failure", err)
	}
}

func TestTransportRetriesOnlyBeforeResponseDelivery(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, `{"error":"temporary"}`)
			return
		}
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	content, err := NewClient().Complete(context.Background(), Params{
		BaseURL: server.URL, APIKey: "k", Model: "m", Protocol: ProtocolChatCompletions,
		RetryPolicy: &RetryPolicy{MaxAttempts: 2, BaseDelay: 0, MaxDelay: 0, DisableJitter: true},
	}, nil)
	if err != nil || content != "ok" || attempts != 2 {
		t.Fatalf("content=%q err=%v attempts=%d, want one retry", content, err, attempts)
	}
}

func TestRetryAfterIsBoundedByMaxDelay(t *testing.T) {
	start := time.Now()
	err := waitRetry(context.Background(), RetryPolicy{MaxAttempts: 2, BaseDelay: 0, MaxDelay: 5 * time.Millisecond}, 1, "3600")
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("retry-after was not bounded: %s", elapsed)
	}
}

func TestTransportDoesNotRetryAfterSuccessfulStreamStarts(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"半份\"}}]}\n\n")
	}))
	defer server.Close()

	_, err := NewClient().Stream(context.Background(), Params{
		BaseURL: server.URL, APIKey: "k", Model: "m", Protocol: ProtocolChatCompletions,
		RetryPolicy: &RetryPolicy{MaxAttempts: 3, BaseDelay: 0, MaxDelay: 0, DisableJitter: true},
	}, nil, nil)
	if !errors.Is(err, io.ErrUnexpectedEOF) || attempts != 1 {
		t.Fatalf("err=%v attempts=%d, want parser failure without replay", err, attempts)
	}
}

func TestStreamDetailedAuditKeepsRawSSEWithoutSecrets(t *testing.T) {
	server := streamServer(t, []string{
		`{"choices":[{"delta":{"content":"{\"narrative\":\"你好\",\"state_update\":{}}"},"finish_reason":"stop"}]}`,
	})
	defer server.Close()
	capture := &requestAuditCapture{}
	client := NewClient()
	client.SetRequestAudit(capture.write, func(context.Context) bool { return true })
	params := Params{
		BaseURL: server.URL, APIKey: "secret-key", Model: "audit-model", Protocol: ProtocolChatCompletions, MaxTokens: 100,
		ConversationID: "conversation", MessageID: "message",
	}
	content, err := client.Stream(context.Background(), params, []Message{{Role: "user", Content: "测试"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(capture.items) != 1 {
		t.Fatalf("audit count = %d, want 1", len(capture.items))
	}
	audit := capture.items[0]
	if content != `{"narrative":"你好","state_update":{}}` || audit.ResponseText != content ||
		!audit.ReceivedDone || !audit.Success || !audit.TransportSuccess || audit.FinishReason != "stop" ||
		!strings.Contains(audit.RawResponse, "data:") {
		t.Fatalf("unexpected audit: %+v content=%q", audit, content)
	}
	if strings.Contains(string(audit.RequestBody), "secret-key") || strings.Contains(audit.RawResponse, "secret-key") {
		t.Fatal("API key leaked into detailed request log")
	}
	var request map[string]any
	if err := json.Unmarshal(audit.RequestBody, &request); err != nil || request["model"] != "audit-model" {
		t.Fatalf("request body = %s, err = %v", audit.RequestBody, err)
	}
}

func TestStreamAuditTruncationDoesNotTruncateParsedResponse(t *testing.T) {
	chunk := strings.Repeat("x", 300<<10)
	lines := make([]string, 8)
	for i := range lines {
		lines[i] = fmt.Sprintf(`{"choices":[{"delta":{"content":%q},"finish_reason":null}]}`, chunk)
	}
	server := streamServer(t, lines)
	defer server.Close()

	capture := &requestAuditCapture{}
	client := NewClient()
	client.SetRequestAudit(capture.write, func(context.Context) bool { return true })
	content, err := client.Stream(context.Background(), Params{
		BaseURL: server.URL, APIKey: "k", Model: "audit-model", Protocol: ProtocolChatCompletions,
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) != len(chunk)*len(lines) {
		t.Fatalf("parsed content len=%d, want %d", len(content), len(chunk)*len(lines))
	}
	if len(capture.items) != 1 {
		t.Fatalf("audit count=%d, want 1", len(capture.items))
	}
	audit := capture.items[0]
	if !audit.Success || !audit.AuditTruncated {
		t.Fatalf("audit success=%v truncated=%v", audit.Success, audit.AuditTruncated)
	}
	if len(audit.RawResponse) != maxLLMAuditBytes || len(audit.ResponseText) != maxLLMAuditBytes {
		t.Fatalf("audit bounds: raw=%d text=%d", len(audit.RawResponse), len(audit.ResponseText))
	}
}

func TestDetailedAuditDisabledCreatesNoRecord(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"完整内容"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	capture := &requestAuditCapture{}
	client := NewClient()
	client.SetRequestAudit(capture.write, func(context.Context) bool { return false })
	if _, err := client.Complete(context.Background(), Params{BaseURL: server.URL, APIKey: "k", Model: "m", Protocol: ProtocolChatCompletions}, nil); err != nil {
		t.Fatal(err)
	}
	if len(capture.items) != 0 {
		t.Fatalf("disabled audit wrote %d records", len(capture.items))
	}
}

func TestSafeAuditURLRemovesSensitiveParts(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "userinfo query and fragment",
			raw:  "https://admin:secret@example.com/v1?api_key=secret#debug",
			want: "https://example.com/v1",
		},
		{
			name: "ordinary base URL",
			raw:  "https://example.com/v1",
			want: "https://example.com/v1",
		},
		{
			name: "invalid URL is omitted",
			raw:  "%zz?api_key=secret",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := safeAuditURL(tt.raw); got != tt.want {
				t.Fatalf("safeAuditURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestCompleteReturnsErrTruncatedOnLengthFinish(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"半份内容"},"finish_reason":"length"}]}`)
	}))
	defer server.Close()

	content, err := NewClient().Complete(context.Background(), Params{BaseURL: server.URL, APIKey: "k", Model: "m", Protocol: ProtocolChatCompletions},
		[]Message{{Role: "user", Content: "hi"}})
	if !errors.Is(err, ErrTruncated) {
		t.Fatalf("err = %v, want ErrTruncated", err)
	}
	if content != "半份内容" {
		t.Fatalf("content = %q", content)
	}
}

func TestCompleteNormalFinish(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"完整内容"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	content, err := NewClient().Complete(context.Background(), Params{BaseURL: server.URL, APIKey: "k", Model: "m", Protocol: ProtocolChatCompletions},
		[]Message{{Role: "user", Content: "hi"}})
	if err != nil || content != "完整内容" {
		t.Fatalf("content = %q err = %v", content, err)
	}
}

func TestResponsesCompleteMapsRequestAndOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Errorf("path = %q, want /responses", r.URL.Path)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		if request["model"] != "responses-model" || request["max_output_tokens"] != float64(321) {
			t.Errorf("request = %#v", request)
		}
		input, ok := request["input"].([]any)
		if !ok || len(input) != 2 {
			t.Errorf("input = %#v", request["input"])
		}
		text, ok := request["text"].(map[string]any)
		if !ok || text["format"] == nil {
			t.Errorf("text format = %#v", request["text"])
		}
		fmt.Fprint(w, `{"status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Responses 内容"}]}]}`)
	}))
	defer server.Close()

	content, err := NewClient().Complete(context.Background(), Params{
		BaseURL: server.URL, APIKey: "k", Model: "responses-model", Protocol: ProtocolResponses,
		MaxTokens: 321, ResponseFormat: &ResponseFormat{Type: "json_schema", JSONSchema: &JSONSchema{
			Name: "envelope", Schema: map[string]any{"type": "object"}, Strict: true,
		}},
	}, []Message{{Role: "system", Content: "规则"}, {Role: "user", Content: "问题"}})
	if err != nil || content != "Responses 内容" {
		t.Fatalf("content = %q err = %v", content, err)
	}
}

func TestResponsesCompleteReplaysReasoningItems(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		input := request["input"].([]any)
		if len(input) != 2 || input[0].(map[string]any)["type"] != "reasoning" {
			t.Fatalf("input = %#v", input)
		}
		fmt.Fprint(w, `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"ok"}]}]}`)
	}))
	defer server.Close()

	content, err := NewClient().Complete(context.Background(), Params{
		BaseURL: server.URL, APIKey: "k", Model: "m", Protocol: ProtocolResponses,
	}, []Message{
		{Role: "assistant", ReasoningItems: []json.RawMessage{json.RawMessage(`{"type":"reasoning","id":"rs-1","content":[{"type":"reasoning_text","text":"检查"}]}`)}, Content: "previous"},
	})
	if err != nil || content != "ok" {
		t.Fatalf("content = %q err = %v", content, err)
	}
}

func TestResponsesStreamParsesDeltaEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Errorf("path = %q, want /responses", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"你\"}\n\n")
		fmt.Fprint(w, "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"好\"}\n\n")
		fmt.Fprint(w, "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n")
	}))
	defer server.Close()

	var deltas []string
	content, err := NewClient().Stream(context.Background(), Params{
		BaseURL: server.URL, APIKey: "k", Model: "responses-model", Protocol: ProtocolResponses,
	}, []Message{{Role: "user", Content: "hi"}}, func(delta string) { deltas = append(deltas, delta) })
	if err != nil || content != "你好" || len(deltas) != 2 {
		t.Fatalf("content = %q deltas = %#v err = %v", content, deltas, err)
	}
}

func TestResponsesCompleteReturnsErrTruncatedOnMaxOutputTokens(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"output":[{"type":"message","content":[{"type":"output_text","text":"半份"}]}]}`)
	}))
	defer server.Close()

	content, err := NewClient().Complete(context.Background(), Params{
		BaseURL: server.URL, APIKey: "k", Model: "responses-model", Protocol: ProtocolResponses,
	}, []Message{{Role: "user", Content: "hi"}})
	if !errors.Is(err, ErrTruncated) || content != "半份" {
		t.Fatalf("content = %q err = %v", content, err)
	}
}
