package llm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// scriptedRelayFunc 是测试替身：按脚本返回原始响应字节，不触碰网络。
// chunk 刻意用小块交付，验证服务端重组。
type scriptedRelayFunc struct {
	gotEndpoint string
	gotBody     []byte
	gotStream   bool
	calls       int
	do          func(ctx context.Context, req RelayRequest) (*http.Response, error)
}

func (s *scriptedRelayFunc) Do(ctx context.Context, req RelayRequest) (*http.Response, error) {
	return s.do(ctx, req)
}

func serveBytes(status int, contentType string, body []byte, chunkSize int) *http.Response {
	if chunkSize <= 0 || chunkSize > len(body) {
		chunkSize = len(body)
	}
	pr, pw := io.Pipe()
	go func() {
		for offset := 0; offset < len(body); {
			end := offset + chunkSize
			if end > len(body) {
				end = len(body)
			}
			if _, err := pw.Write(body[offset:end]); err != nil {
				return
			}
			offset = end
		}
		_ = pw.Close()
	}()
	header := http.Header{}
	if contentType != "" {
		header.Set("Content-Type", contentType)
	}
	return &http.Response{
		StatusCode: status, Header: header, Body: pr, ContentLength: -1,
	}
}

// captureWriter 记录服务端实际写出的响应字节。
type captureWriter struct {
	http.ResponseWriter
	buffer bytes.Buffer
}

func (w *captureWriter) Write(data []byte) (int, error) {
	w.buffer.Write(data)
	return w.ResponseWriter.Write(data)
}

func agentStepOutcome(result AgentStepResult, events []AgentEvent) (string, []AgentEventKind) {
	kinds := make([]AgentEventKind, 0, len(events))
	for _, event := range events {
		kinds = append(kinds, event.Kind)
	}
	summary := fmt.Sprintf("text=%q finish=%q tools=%d usage=%+v", result.Text, result.FinishReason, len(result.ToolCalls), result.Usage)
	for _, call := range result.ToolCalls {
		summary += fmt.Sprintf(" call(%s %s %s)", call.ID, call.Name, call.Arguments)
	}
	return summary, kinds
}

// TestRelayEquivalenceWithDirectTransport 同一份 fixture 分别经 httptest 直连
// 和假 Relay 喂入，断言 AgentStepResult、事件序列与审计记录完全一致，
// 且 Relay 收到的 endpoint 与请求体与直连收到的完全相同。
func TestRelayEquivalenceWithDirectTransport(t *testing.T) {
	for _, protocol := range []Protocol{ProtocolChatCompletions, ProtocolResponses} {
		for _, stream := range []bool{false, true} {
			for _, lane := range []string{"text", "tool"} {
				name := fmt.Sprintf("%s/stream=%v/%s", protocol, stream, lane)
				t.Run(name, func(t *testing.T) {
					var directBody []byte
					var directPath string
					var directRaw bytes.Buffer
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						directPath = r.URL.Path
						raw, err := io.ReadAll(r.Body)
						if err != nil {
							t.Fatal(err)
						}
						directBody = raw
						capture := &captureWriter{ResponseWriter: w}
						writeAgentFixture(capture, protocol, stream, lane)
						directRaw.Write(capture.buffer.Bytes())
					}))
					defer server.Close()

					run := func(relay RelayTransport) (AgentStepResult, []AgentEvent, RequestAudit) {
						capture := &requestAuditCapture{}
						client := NewClient()
						client.SetRequestAudit(capture.write, func(context.Context) bool { return true })
						var events []AgentEvent
						result, err := client.AgentStep(context.Background(), Params{
							BaseURL: server.URL, APIKey: "test", Model: "agent-model",
							Protocol: protocol, Provider: "openai", Relay: relay,
						}, []Message{{Role: "user", Content: "continue"}}, testAgentTools(), stream, func(event AgentEvent) {
							events = append(events, event)
						})
						if err != nil {
							t.Fatal(err)
						}
						if len(capture.items) != 1 {
							t.Fatalf("audit count = %d, want 1", len(capture.items))
						}
						return result, events, capture.items[0]
					}

					directResult, directEvents, directAudit := run(nil)
					directSummary, directKinds := agentStepOutcome(directResult, directEvents)

					contentType := ""
					if stream {
						contentType = "text/event-stream"
					}
					relay := &scriptedRelayFunc{}
					relay.do = func(_ context.Context, req RelayRequest) (*http.Response, error) {
						relay.gotEndpoint = req.Endpoint
						relay.gotBody = append([]byte(nil), req.Body...)
						relay.gotStream = req.Stream
						relay.calls++
						return serveBytes(http.StatusOK, contentType, directRaw.Bytes(), 7), nil
					}
					relayResult, relayEvents, relayAudit := run(relay)
					relaySummary, relayKinds := agentStepOutcome(relayResult, relayEvents)

					if relaySummary != directSummary {
						t.Fatalf("relay result = %s, want %s", relaySummary, directSummary)
					}
					if !reflect.DeepEqual(relayKinds, directKinds) {
						t.Fatalf("relay events = %v, want %v", relayKinds, directKinds)
					}
					if string(relayAudit.RequestBody) != string(directAudit.RequestBody) ||
						relayAudit.RawResponse != directAudit.RawResponse ||
						relayAudit.ResponseText != directAudit.ResponseText ||
						relayAudit.Success != directAudit.Success ||
						relayAudit.TransportSuccess != directAudit.TransportSuccess ||
						relayAudit.FinishReason != directAudit.FinishReason ||
						relayAudit.ResponseStatus != directAudit.ResponseStatus {
						t.Fatalf("relay audit differs:\nrelay=%+v\ndirect=%+v", relayAudit, directAudit)
					}
					wantEndpoint := "/chat/completions"
					if protocol == ProtocolResponses {
						wantEndpoint = "/responses"
					}
					if relay.gotEndpoint != wantEndpoint || relay.gotEndpoint != directPath {
						t.Fatalf("relay endpoint = %q, direct path = %q, want %q", relay.gotEndpoint, directPath, wantEndpoint)
					}
					if string(relay.gotBody) != string(directBody) {
						t.Fatalf("relay request body differs from direct request body")
					}
					if relay.gotStream != stream {
						t.Fatalf("relay stream = %v, want %v", relay.gotStream, stream)
					}
				})
			}
		}
	}
}
