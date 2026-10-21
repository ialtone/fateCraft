package llm

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ialtone/fatecraft/internal/llmoutbound"
)

func TestTransportRetriesKeepOriginalExitAndNextCallUsesNewExit(t *testing.T) {
	manager := llmoutbound.NewManager()
	var oldHits, newHits atomic.Int32
	newProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { newHits.Add(1); _, _ = io.WriteString(w, "new") }))
	defer newProxy.Close()
	next, err := llmoutbound.Build(llmoutbound.Config{Enabled: true, URL: newProxy.URL}, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	oldProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if oldHits.Add(1) == 1 {
			manager.Publish(next)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, "old")
	}))
	defer oldProxy.Close()
	first, err := llmoutbound.Build(llmoutbound.Config{Enabled: true, URL: oldProxy.URL}, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	manager.Publish(first)
	client := NewClient()
	client.SetOutbound(manager)
	p := Params{BaseURL: "http://provider.invalid", APIKey: "k", Model: "m", RetryPolicy: &RetryPolicy{MaxAttempts: 2}}
	for _, want := range []string{"old", "new"} {
		resp, err := client.doRaw(t.Context(), p, "/responses", []byte(`{}`), false)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil || string(body) != want {
			t.Fatalf("want %s, body=%q err=%v", want, body, err)
		}
	}
	if oldHits.Load() != 2 || newHits.Load() != 1 {
		t.Fatalf("retries switched exit: old=%d new=%d", oldHits.Load(), newHits.Load())
	}
}

func TestAgentProtocolsAndStreamingUseConfiguredProxy(t *testing.T) {
	for _, protocol := range []Protocol{ProtocolChatCompletions, ProtocolResponses} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", protocol, stream), func(t *testing.T) {
				var hits atomic.Int32
				proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					hits.Add(1)
					if r.URL.Host != "provider.invalid" {
						t.Errorf("proxy destination %s", r.URL)
					}
					writeAgentFixture(w, protocol, stream, "text")
				}))
				defer proxy.Close()
				snapshot, err := llmoutbound.Build(llmoutbound.Config{Enabled: true, URL: proxy.URL}, 1)
				if err != nil {
					t.Fatal(err)
				}
				defer snapshot.Close()
				manager := llmoutbound.NewManager()
				manager.Publish(snapshot)
				client := NewClient()
				client.SetOutbound(manager)
				result, err := client.AgentStep(t.Context(), Params{BaseURL: "http://provider.invalid", APIKey: "k", Model: "m", Protocol: protocol}, []Message{{Role: "user", Content: "continue"}}, testAgentTools(), stream, nil)
				if err != nil || result.Text != "叙事完成" || hits.Load() != 1 {
					t.Fatalf("result=%+v err=%v hits=%d", result, err, hits.Load())
				}
			})
		}
	}
}

func TestProxyAuthenticationFailureIsNotRetriedOrLeaked(t *testing.T) {
	var hits atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusProxyAuthRequired)
		_, _ = io.WriteString(w, "secret CONNECT diagnostic")
	}))
	defer proxy.Close()
	snapshot, err := llmoutbound.Build(llmoutbound.Config{Enabled: true, URL: proxy.URL, Username: "secret-user", Password: "secret-password"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	manager := llmoutbound.NewManager()
	manager.Publish(snapshot)
	client := NewClient()
	client.SetOutbound(manager)
	_, err = client.doRaw(t.Context(), Params{BaseURL: "http://provider.invalid", APIKey: "k", RetryPolicy: &RetryPolicy{MaxAttempts: 3}}, "/responses", nil, false)
	if err == nil || hits.Load() != 1 {
		t.Fatalf("auth failure retried: hits=%d error=%v", hits.Load(), err)
	}
	for _, secret := range []string{proxy.URL, "secret-user", "secret-password", "secret CONNECT diagnostic"} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("leaked %q: %v", secret, err)
		}
	}
}
