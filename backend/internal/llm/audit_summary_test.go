package llm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/ialtone/fatecraft/internal/logging"
)

type auditRoundTripper func(*http.Request) (*http.Response, error)

func (f auditRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRetriesShareOneSummaryAndKeepUsageWithoutBodies(t *testing.T) {
	client := NewClient()
	ctx := logging.NewTrace(context.Background())
	var ids []string
	client.http.Transport = auditRoundTripper(func(r *http.Request) (*http.Response, error) {
		call := auditCall(r.Context())
		ids = append(ids, call.id)
		status := 503
		body := "synthetic-secret provider failure"
		if len(ids) == 3 {
			status = 200
			body = "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: {\"usage\":{\"prompt_tokens\":21,\"completion_tokens\":3},\"choices\":[]}\n\ndata: [DONE]\n\n"
		}
		return &http.Response{StatusCode: status, Status: fmt.Sprint(status), Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	capture := &requestAuditCapture{}
	client.SetRequestAudit(capture.write, func(context.Context, Params) bool { return false })
	text, err := client.Stream(ctx, Params{Protocol: ProtocolChatCompletions, BaseURL: "https://example.test/v1", APIKey: "synthetic-secret", Model: "m", RetryPolicy: &RetryPolicy{MaxAttempts: 3, DisableJitter: true}}, nil, nil)
	if err != nil || text != "ok" || len(capture.items) != 1 {
		t.Fatalf("call=%q err=%v summaries=%d", text, err, len(capture.items))
	}
	a := capture.items[0]
	if len(ids) != 3 || ids[0] != ids[1] || ids[0] != ids[2] || a.ID != ids[0] || a.TraceID != logging.TraceID(ctx) || a.TransportAttempts != 3 {
		t.Fatal("logical call identity changed during retry")
	}
	if a.InputTokens == nil || *a.InputTokens != 21 || a.OutputTokens == nil || *a.OutputTokens != 3 || a.BodyStatus != "disabled" || a.RawResponse != "" || a.ResponseText != "" || string(a.RequestBody) != "{}" {
		t.Fatalf("invalid summary: %+v", a)
	}
}

func TestMaximumDetailedRecordFitsRecordBudget(t *testing.T) {
	a := RequestAudit{RequestBody: make([]byte, auditRequestLimit), RawResponse: strings.Repeat("x", auditResponseLimit), ResponseText: strings.Repeat("x", auditTextLimit), BodyMetadata: map[string]AuditFieldInfo{"request": {}, "raw_response": {}, "response_text": {}}}
	if size := auditSize(a); size > auditRecordLimit {
		t.Fatalf("supported body limits exceed record limit: %d", size)
	}
	a.SettingsSource = strings.Repeat("x", auditRecordLimit)
	w := NewAsyncRequestAuditWriter(func(context.Context, RequestAudit) error { return nil }, 2)
	defer w.Close(context.Background())
	if err := w.Write(context.Background(), a); err == nil || w.Status().Dropped["record_limit"] != 1 {
		t.Fatal("metadata bypassed record limit")
	}
}

func BenchmarkAuditSummary(b *testing.B) {
	c := NewClient()
	var size int64
	c.SetRequestAudit(func(_ context.Context, a RequestAudit) error { size = auditSize(a); return nil }, func(context.Context, Params) bool { return false })
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ctx, _ := c.beginAudit(context.Background(), Params{})
		c.finishAudit(ctx, false, Params{Model: "benchmark"}, RequestAudit{Model: "benchmark", Success: true}, nil)
	}
	b.ReportMetric(float64(size), "estimated-bytes/record")
}
