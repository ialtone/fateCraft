package llm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func TestAuditJSONAlwaysStorableAndRedacted(t *testing.T) {
	large, _ := json.Marshal(map[string]any{"api_key": "synthetic-secret with spaces", "messages": []any{map[string]any{"text": strings.Repeat("中文\n<", 600000)}}})
	for _, input := range [][]byte{nil, []byte(`{"broken":"synthetic-secret`), []byte(`{"text":"中\u0000文","\u0000":"value"}`), []byte("{\"text\":\"\xff\"}"), large} {
		body, info := safeAuditJSON(input, "synthetic-secret")
		if !json.Valid(body) || !utf8.Valid(body) || len(body) > auditRequestLimit {
			t.Fatalf("invalid preview: len=%d info=%+v", len(body), info)
		}
		var decoded any
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Fatal(err)
		}
		if invalidJSONText(decoded) || strings.Contains(string(body), "synthetic-secret") {
			t.Fatal("preview leaked credentials or contains PostgreSQL-incompatible NUL")
		}
		if info.StoredBytes != len(body) || info.OriginalBytes != int64(len(input)) {
			t.Fatalf("incorrect field sizes: %+v", info)
		}
	}
	_, info := safeAuditJSON([]byte(`{"text":"中\u0000文"}`), "")
	if !info.Invalid {
		t.Fatal("NUL replacement must be visible")
	}
	input := []byte(`{"text":"` + strings.Repeat("a", auditRequestLimit-11) + `"}`)
	body, info := safeAuditJSON(input, "")
	if len(body) != auditRequestLimit || info.Truncated {
		t.Fatalf("exact-limit request incorrectly truncated: %d %+v", len(body), info)
	}
}

func TestAuditTextBoundaryAndDisabledCapture(t *testing.T) {
	for _, raw := range []string{`{"pass\u0077ord":"synthetic-secret value","value":"\u0073ynthetic-secret"}`, "data: {\"api_key\":\"synthetic-secret\"}\n\ndata: [DONE]\n", "data:" + strings.Repeat("data:", 10000)} {
		clean, _ := safeAuditText(raw, "synthetic-secret", auditResponseLimit)
		if strings.Contains(clean, "synthetic-secret") {
			t.Fatal("escaped JSON/SSE credentials leaked")
		}
	}
	value, info := safeAuditText(strings.Repeat("中", 20)+"\x00\xff", "", 59)
	if !utf8.ValidString(value) || strings.ContainsRune(value, 0) || len(value) > 59 || !info.Invalid || !info.Truncated {
		t.Fatalf("unsafe text %q %+v", value, info)
	}
	buffer := newCaptureBuffer(false)
	_, _ = buffer.Write([]byte(strings.Repeat("x", 2<<20)))
	if buffer.String() != "" || cap(buffer.data) != 0 {
		t.Fatal("disabled audit collected raw stream")
	}
}

func TestAuditQueueBudgetsIncludeInflightAndRelease(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	w := NewAsyncRequestAuditWriter(func(context.Context, RequestAudit) error {
		once.Do(func() { close(started) })
		<-release
		return errors.New("write failed")
	}, 128)
	a := RequestAudit{RequestBody: []byte(`{}`), SettingsSource: strings.Repeat("x", 4096)}
	w.status.ByteLimit = 2 * auditSize(a)
	if err := w.Write(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := w.Write(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if err := w.Write(context.Background(), a); err == nil {
		t.Fatal("byte budget not enforced")
	}
	if status := w.Status(); status.Pending != 2 || status.Bytes != 2*auditSize(a) || status.Dropped["byte_limit"] != 1 {
		t.Fatalf("status=%+v", status)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := w.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("close error=%v", err)
	}
	if w.Status().Unflushed != 2 {
		t.Fatal("timeout missing unflushed count")
	}
	close(release)
	if err := w.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status := w.Status(); status.Pending != 0 || status.Bytes != 0 || status.Failed != 2 {
		t.Fatalf("budget leak: %+v", status)
	}
}

func TestAuditQueueConcurrentCloseAndPanic(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	w := NewAsyncRequestAuditWriter(func(context.Context, RequestAudit) error {
		once.Do(func() { close(started) })
		panic("synthetic-secret")
	}, 128)
	if err := w.Write(context.Background(), RequestAudit{}); err != nil {
		t.Fatal(err)
	}
	<-started
	var group sync.WaitGroup
	for i := 0; i < 200; i++ {
		group.Add(1)
		go func() { defer group.Done(); _ = w.Write(context.Background(), RequestAudit{}) }()
	}
	group.Add(1)
	go func() { defer group.Done(); _ = w.Close(context.Background()) }()
	group.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := w.Close(ctx); err != nil {
		t.Fatal(err)
	}
	status := w.Status()
	var dropped uint64
	for _, n := range status.Dropped {
		dropped += n
	}
	if status.Pending != 0 || status.Bytes != 0 || status.Failed == 0 || status.Failed+dropped != 201 {
		t.Fatalf("lost accounting: %+v", status)
	}
}
