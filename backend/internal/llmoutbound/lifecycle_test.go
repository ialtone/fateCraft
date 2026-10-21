package llmoutbound

import (
	"context"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Both hops use TLS. The target must never receive either caller-supplied or
// configured proxy credentials through the tunnel.
func TestHTTPSProxyAndTargetKeepCredentialsOnProxyHop(t *testing.T) {
	var targetHits atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHits.Add(1)
		if got := r.Header.Get("Proxy-Authorization"); got != "" {
			t.Errorf("proxy credentials reached target: %q", got)
		}
		if r.Header.Get("Authorization") != "Bearer provider-key" {
			t.Error("provider auth missing")
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer target.Close()
	var connects atomic.Int32
	proxy := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			t.Error("expected CONNECT")
			w.WriteHeader(400)
			return
		}
		connects.Add(1)
		if r.Header.Get("Proxy-Authorization") != "Basic YWxpY2U6c2VjcmV0" {
			t.Error("missing proxy auth")
		}
		upstream, err := net.Dial("tcp", r.Host)
		if err != nil {
			t.Error(err)
			w.WriteHeader(502)
			return
		}
		client, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			_ = upstream.Close()
			return
		}
		defer client.Close()
		defer upstream.Close()
		_, _ = io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n")
		go func() { _, _ = io.Copy(upstream, client); _ = upstream.Close() }()
		_, _ = io.Copy(client, upstream)
	}))
	defer proxy.Close()
	ca := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: proxy.Certificate().Raw}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: target.Certificate().Raw})...)
	snapshot, err := Build(Config{Enabled: true, URL: proxy.URL, Username: "alice", Password: "secret", CAPEM: string(ca)}, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, target.URL, nil)
	req.Header.Set("Authorization", "Bearer provider-key")
	req.Header.Set("Proxy-Authorization", "caller-secret")
	resp, err := snapshot.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil || targetHits.Load() != 1 || connects.Load() != 1 {
		t.Fatalf("bad tunnel result: %v targets=%d connects=%d", err, targetHits.Load(), connects.Load())
	}
}

func TestPublishPreservesActiveStreamAndOldSnapshotRetries(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{}, 1)
	defer close(release)
	oldProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stream" {
			_, _ = io.WriteString(w, "first\n")
			w.(http.Flusher).Flush()
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		_, _ = io.WriteString(w, "old")
	}))
	defer oldProxy.Close()
	newProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "new") }))
	defer newProxy.Close()
	oldSnapshot, _ := Build(Config{Enabled: true, URL: oldProxy.URL}, 1)
	newSnapshot, _ := Build(Config{Enabled: true, URL: newProxy.URL}, 2)
	manager := NewManager()
	manager.Publish(oldSnapshot)
	defer oldSnapshot.Close()
	defer newSnapshot.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://model.invalid/stream", nil)
	response, err := oldSnapshot.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	<-started
	manager.Publish(newSnapshot)
	for _, tc := range []struct {
		snapshot *Snapshot
		want     string
	}{{manager.Snapshot(), "new"}, {oldSnapshot, "old"}} {
		r, _ := http.NewRequestWithContext(ctx, "GET", "http://model.invalid/next", nil)
		resp, err := tc.snapshot.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil || string(body) != tc.want {
			t.Fatalf("want %s, got %q %v", tc.want, body, err)
		}
	}
	release <- struct{}{}
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "first\nold" {
		t.Fatalf("old stream interrupted: %q %v", body, err)
	}
	_ = response.Body.Close()
	oldSnapshot.transport.mu.Lock()
	defer oldSnapshot.transport.mu.Unlock()
	if !oldSnapshot.transport.retired || oldSnapshot.transport.active != 0 {
		t.Fatalf("old pool not drained: retired=%v active=%d", oldSnapshot.transport.retired, oldSnapshot.transport.active)
	}
}

func TestConnectionUsesHEADWithoutCredentialsOrRedirectsAndMarksStale(t *testing.T) {
	manager := NewManager()
	var targetHits atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHits.Add(1)
		if r.Method != http.MethodHead || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("invalid connectivity request")
		}
		next, _ := Build(Config{}, 8)
		manager.Publish(next)
		w.Header().Set("Location", "https://redirect.invalid/")
		w.WriteHeader(http.StatusFound)
	}))
	defer target.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			w.WriteHeader(400)
			return
		}
		upstream, err := net.Dial("tcp", r.Host)
		if err != nil {
			w.WriteHeader(502)
			return
		}
		client, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			_ = upstream.Close()
			return
		}
		defer client.Close()
		defer upstream.Close()
		_, _ = io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n")
		go func() { _, _ = io.Copy(upstream, client); _ = upstream.Close() }()
		_, _ = io.Copy(client, upstream)
	}))
	defer proxy.Close()
	snapshot, err := Build(Config{Enabled: true, URL: proxy.URL, CAPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: target.Certificate().Raw}))}, 7)
	if err != nil {
		t.Fatal(err)
	}
	manager.Publish(snapshot)
	defer snapshot.Close()
	result, err := manager.TestConnection(t.Context(), target.URL)
	if err != nil || !result.OK || !result.Stale || result.Revision != 7 || result.TargetStatus != 302 || targetHits.Load() != 1 {
		t.Fatalf("unexpected result: %+v err=%v hits=%d", result, err, targetHits.Load())
	}
}

func TestOutboundErrorsDoNotExposeTransportWrappers(t *testing.T) {
	inner := &Error{Code: CodeProxyUnreachable, Err: errors.New("secret-proxy:8080")}
	wrapped := &net.OpError{Op: "proxyconnect", Net: "tcp", Addr: &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 8080}, Err: inner}
	if got := wrapTransportError(wrapped, true, "secret-proxy:8080").Error(); strings.Contains(got, "8080") || strings.Contains(got, "192.0.2.1") {
		t.Fatal(got)
	}
	var unavailable *Snapshot
	resp, err := unavailable.Client().Get("https://example.invalid")
	if resp != nil || err == nil {
		t.Fatal("missing snapshot must fail closed")
	}
}
