package llmoutbound

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestValidateRejectsUnsafeProxyURLs(t *testing.T) {
	bad := []string{
		"socks5://127.0.0.1:1080",
		"http://user:pass@127.0.0.1:8080",
		"http://127.0.0.1",
		"http://127.0.0.1:8080/path",
		"http://127.0.0.1:8080?x=1",
		"http://127.0.0.1:8080#fragment",
		"http://127.0.0.1:8080?",
		"http://127.0.0.1:8080#",
		"http://127.0.0.1:0",
		"http://127.0.0.1:65536",
		"http://127.0.0.1:8080\r\nX-Test: 1",
		"http://127.0.0.1:8080\n",
	}
	for _, raw := range bad {
		err := Validate(Config{Enabled: true, URL: raw})
		if !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("expected %q to be rejected, got %v", raw, err)
		}
	}
	if err := Validate(Config{Enabled: true, URL: "http://127.0.0.1:8080"}); err != nil {
		t.Fatalf("valid proxy rejected: %v", err)
	}
}

func TestValidateRejectsPrivateKeyPEM(t *testing.T) {
	err := Validate(Config{Enabled: true, URL: "http://127.0.0.1:8080", CAPEM: "-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----"})
	if !errors.Is(err, ErrInvalidConfig) || !strings.Contains(err.Error(), "私钥") {
		t.Fatalf("expected private key rejection, got %v", err)
	}
}

func TestDirectIgnoresEnvironmentProxy(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer target.Close()
	var proxyHits atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		proxyHits.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer proxy.Close()

	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("NO_PROXY", "")

	snapshot, err := Build(Config{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := snapshot.Client().Get(target.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if proxyHits.Load() != 0 {
		t.Fatalf("direct mode sent the request through the environment proxy %d times", proxyHits.Load())
	}
}

func TestProxyUsesBasicAuth(t *testing.T) {
	var auth atomic.Value
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth.Store(r.Header.Get("Proxy-Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer proxy.Close()

	snapshot, err := Build(Config{
		Enabled: true, URL: proxy.URL, Username: "proxy-user", Password: "p@ss word",
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := snapshot.Client().Get("http://example.invalid/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("proxy-user:p@ss word"))
	if got, _ := auth.Load().(string); got != want {
		t.Fatalf("unexpected proxy auth header %q", got)
	}
}

func TestUnreachableProxyIsClassifiedAsProxyFailure(t *testing.T) {
	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	proxyURL := closed.URL
	closed.Close()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	snapshot, err := Build(Config{Enabled: true, URL: proxyURL}, 1)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := snapshot.Client().Get(target.URL)
	if resp != nil {
		_ = resp.Body.Close()
	}
	var outbound *Error
	if !errors.As(err, &outbound) || outbound.Code != CodeProxyUnreachable {
		t.Fatalf("expected proxy unreachable classification, got %v", err)
	}
}

func TestHTTPSThroughProxyUsesConnectAuthAndCustomCA(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer target.Close()
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: target.Certificate().Raw})

	var connects atomic.Int32
	var auth atomic.Value
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "CONNECT required", http.StatusMethodNotAllowed)
			return
		}
		connects.Add(1)
		auth.Store(r.Header.Get("Proxy-Authorization"))
		upstream, err := net.Dial("tcp", r.Host)
		if err != nil {
			http.Error(w, "upstream unavailable", http.StatusBadGateway)
			return
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			_ = upstream.Close()
			http.Error(w, "hijack unavailable", http.StatusInternalServerError)
			return
		}
		client, _, err := hijacker.Hijack()
		if err != nil {
			_ = upstream.Close()
			return
		}
		_, _ = client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		go func() {
			_, _ = io.Copy(upstream, client)
			_ = upstream.Close()
		}()
		go func() {
			_, _ = io.Copy(client, upstream)
			_ = client.Close()
		}()
	}))
	defer proxy.Close()

	// A matching NO_PROXY entry must not bypass an explicitly configured proxy.
	t.Setenv("NO_PROXY", "127.0.0.1,localhost")
	snapshot, err := Build(Config{
		Enabled: true, URL: proxy.URL, Username: "proxy-user", Password: "proxy-secret",
		CAPEM: string(certPEM),
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := snapshot.Client().Get(target.URL)
	if err != nil {
		t.Fatalf("HTTPS through CONNECT proxy failed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected target status %d", resp.StatusCode)
	}
	if connects.Load() != 1 {
		t.Fatalf("expected exactly one CONNECT, got %d", connects.Load())
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("proxy-user:proxy-secret"))
	if got, _ := auth.Load().(string); got != want {
		t.Fatalf("unexpected CONNECT auth header %q", got)
	}
}

func TestConnectProxy407ReturnsAuthError(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			http.Error(w, "CONNECT required", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Proxy-Authenticate", `Basic realm="test"`)
		w.WriteHeader(http.StatusProxyAuthRequired)
	}))
	defer proxy.Close()

	snapshot, err := Build(Config{Enabled: true, URL: proxy.URL}, 1)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := snapshot.Client().Get("https://example.invalid/")
	if resp != nil {
		_ = resp.Body.Close()
	}
	var outbound *Error
	if !errors.As(err, &outbound) || outbound.Code != CodeProxyAuthFailed {
		t.Fatalf("expected CONNECT proxy auth error, got %v", err)
	}
}

func TestProxy407ReturnsAuthError(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusProxyAuthRequired)
	}))
	defer proxy.Close()

	snapshot, err := Build(Config{Enabled: true, URL: proxy.URL}, 1)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := snapshot.Client().Get("http://example.invalid/")
	if resp != nil {
		_ = resp.Body.Close()
	}
	var outbound *Error
	if !errors.As(err, &outbound) || outbound.Code != CodeProxyAuthFailed {
		t.Fatalf("expected proxy auth error, got %v", err)
	}
}

func TestCustomCAAppliesToLLMClientOnly(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer target.Close()
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: target.Certificate().Raw})
	if _, err := x509.ParseCertificate(target.Certificate().Raw); err != nil {
		t.Fatal(err)
	}

	withoutCA, err := Build(Config{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := withoutCA.Client().Get(target.URL)
	if resp != nil {
		_ = resp.Body.Close()
	}
	var outbound *Error
	if !errors.As(err, &outbound) || outbound.Code != CodeTLSCertificate {
		t.Fatalf("expected certificate failure without custom CA, got %v", err)
	}

	withCA, err := Build(Config{CAPEM: string(certPEM)}, 0)
	if err != nil {
		t.Fatal(err)
	}
	resp, err = withCA.Client().Get(target.URL)
	if err != nil {
		t.Fatalf("custom CA should trust the target: %v", err)
	}
	_ = resp.Body.Close()
}

func TestPublishRetiresOldSnapshot(t *testing.T) {
	manager := NewManager()
	first, err := Build(Config{}, 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build(Config{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	manager.Publish(first)
	manager.Publish(second)
	if manager.Snapshot().Revision() != 2 {
		t.Fatalf("unexpected current revision %d", manager.Snapshot().Revision())
	}
	if first.Revision() != 1 {
		t.Fatalf("old snapshot must remain immutable, got revision %d", first.Revision())
	}
}

func TestTestConnectionRejectsNonHTTPS(t *testing.T) {
	manager := NewManager()
	snapshot, err := Build(Config{Enabled: true, URL: "http://127.0.0.1:9999"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	manager.Publish(snapshot)
	if _, err := manager.TestConnection(t.Context(), "http://example.test/"); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected HTTPS validation error, got %v", err)
	}
}
