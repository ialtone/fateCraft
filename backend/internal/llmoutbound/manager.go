package llmoutbound

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// Error codes are stable, non-secret classifications for the admin UI and
	// logs. They never include a proxy address, credentials or raw CONNECT text.
	CodeProxyUnreachable  = "proxy_unreachable"
	CodeProxyAuthFailed   = "proxy_auth_failed"
	CodeTargetUnreachable = "target_unreachable"
	CodeTLSCertificate    = "tls_certificate_failed"
	CodeTimeout           = "timeout"
	CodeConfigInvalid     = "config_invalid"
	CodeOutboundFailed    = "outbound_error"
)

// Error wraps a low-level outbound failure. Error() and SafeLogMessage() are
// intentionally generic so user-facing paths and structured logs never expose
// proxy hostnames, URLs, credentials or CONNECT details.
type Error struct {
	Code string
	Err  error
}

func (e *Error) Error() string {
	switch e.Code {
	case CodeProxyUnreachable:
		return "LLM 出站代理不可达"
	case CodeProxyAuthFailed:
		return "LLM 出站代理认证失败"
	case CodeTargetUnreachable:
		return "模型服务目标不可达"
	case CodeTLSCertificate:
		return "LLM 出站 TLS 证书校验失败"
	case CodeTimeout:
		return "LLM 出站请求超时"
	case CodeConfigInvalid:
		return "LLM 出站代理配置无效"
	default:
		return "LLM 出站网络错误"
	}
}

func (e *Error) Unwrap() error { return e.Err }

func (e *Error) SafeLogMessage() string { return "llm_outbound " + e.Code }

// Manager publishes immutable snapshots. Publishing is cheap and safe to call
// from the settings save path; readers take one snapshot per logical call.
type Manager struct {
	current atomic.Pointer[Snapshot]
}

// NewManager starts in explicit direct mode at revision 0.
func NewManager() *Manager {
	m := &Manager{}
	direct, err := Build(Config{}, 0)
	if err == nil {
		m.current.Store(direct)
	}
	return m
}

func (m *Manager) Snapshot() *Snapshot {
	if m == nil {
		return nil
	}
	return m.current.Load()
}

// Publish swaps the active snapshot and retires the old pool. Active responses
// keep running; their idle connections are closed when they finish.
func (m *Manager) Publish(snapshot *Snapshot) {
	if m == nil || snapshot == nil {
		return
	}
	old := m.current.Swap(snapshot)
	if old != nil && old != snapshot {
		old.Close()
	}
}

// Snapshot is an immutable exit configuration. Its HTTP client has no overall
// timeout so long narrative streams are unaffected.
type Snapshot struct {
	revision  int64
	enabled   bool
	transport *managedTransport
	client    *http.Client
}

func (s *Snapshot) Revision() int64 {
	if s == nil {
		return 0
	}
	return s.revision
}

func (s *Snapshot) Enabled() bool {
	return s != nil && s.enabled
}

// WithRevision returns a shallow copy carrying the revision assigned by the
// successful database transaction. The transport pool is intentionally shared.
func (s *Snapshot) WithRevision(revision int64) *Snapshot {
	if s == nil {
		return nil
	}
	clone := *s
	clone.revision = revision
	return &clone
}

func (s *Snapshot) Client() *http.Client {
	if s == nil {
		return &http.Client{Transport: unavailableTransport{}}
	}
	return s.client
}

type unavailableTransport struct{}

func (unavailableTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, &Error{Code: CodeConfigInvalid}
}

// NewClient returns a client that shares the snapshot's connection pool but has
// its own total timeout and redirect policy. Model list and connectivity checks
// use this without imposing their timeout on narrative streams.
func (s *Snapshot) NewClient(timeout time.Duration, followRedirects bool) *http.Client {
	if s == nil {
		return s.Client()
	}
	client := &http.Client{Transport: s.transport}
	if timeout > 0 {
		client.Timeout = timeout
	}
	if !followRedirects {
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	return client
}

// Close retires the pool immediately and again after in-flight bodies end.
func (s *Snapshot) Close() {
	if s == nil || s.transport == nil {
		return
	}
	s.transport.Retire()
}

type managedTransport struct {
	base      *http.Transport
	guarded   *http.Transport // 直连且目标由用户提供时使用
	proxyAddr string
	proxied   bool

	mu      sync.Mutex
	active  int
	retired bool
}

func (t *managedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL == nil || (req.URL.Scheme != "http" && req.URL.Scheme != "https") {
		return nil, &Error{Code: CodeConfigInvalid}
	}
	// Only the transport may attach proxy credentials to its proxy hop. Never
	// let a caller-supplied header reach the model through a CONNECT tunnel.
	req = req.Clone(req.Context())
	req.Header.Del("Proxy-Authorization")
	t.mu.Lock()
	t.active++
	t.mu.Unlock()

	transport := t.base
	if t.guarded != nil && userSuppliedTarget(req.Context()) {
		transport = t.guarded
	}
	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.finish()
		return nil, wrapTransportError(err, t.proxied, t.proxyAddr)
	}
	if t.proxied && resp.StatusCode == http.StatusProxyAuthRequired {
		_ = resp.Body.Close()
		t.finish()
		return nil, &Error{Code: CodeProxyAuthFailed}
	}
	if resp.Body == nil {
		t.finish()
		return resp, nil
	}
	resp.Body = &trackedBody{ReadCloser: resp.Body, finish: t.finish, sanitize: func(err error) error { return wrapTransportError(err, t.proxied, t.proxyAddr) }}
	return resp, nil
}

func (t *managedTransport) Retire() {
	t.mu.Lock()
	t.retired = true
	t.closeIdle()
	t.mu.Unlock()
}

func (t *managedTransport) closeIdle() {
	t.base.CloseIdleConnections()
	if t.guarded != nil {
		t.guarded.CloseIdleConnections()
	}
}

func (t *managedTransport) finish() {
	t.mu.Lock()
	if t.active > 0 {
		t.active--
	}
	if t.retired && t.active == 0 {
		t.closeIdle()
	}
	t.mu.Unlock()
}

type trackedBody struct {
	io.ReadCloser
	once     sync.Once
	finish   func()
	sanitize func(error) error
}

func (b *trackedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err == nil || errors.Is(err, io.EOF) {
		return n, err
	}
	return n, b.sanitize(err)
}

func (b *trackedBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(b.finish)
	return b.sanitize(err)
}

func wrapTransportError(err error, proxied bool, proxyAddr string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	var outbound *Error
	if errors.As(err, &outbound) {
		return outbound
	}
	code := classifyError(err, proxied, proxyAddr)
	return &Error{Code: code, Err: err}
}

func classifyError(err error, proxied bool, proxyAddr string) string {
	if isTLSCertificateError(err) {
		return CodeTLSCertificate
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return CodeTimeout
	}
	var dialErr *net.OpError
	if !proxied && errors.As(err, &dialErr) && dialErr.Op == "dial" {
		return CodeTargetUnreachable
	}
	if proxied {
		var opErr *net.OpError
		if errors.As(err, &opErr) && opErr.Addr != nil && strings.EqualFold(opErr.Addr.String(), proxyAddr) {
			return CodeProxyUnreachable
		}
		var urlErr *url.Error
		if errors.As(err, &urlErr) && strings.Contains(strings.ToLower(urlErr.Op), "proxy") {
			return CodeProxyUnreachable
		}
		return CodeOutboundFailed
	}
	return CodeOutboundFailed
}

func isTLSCertificateError(err error) bool {
	var unknownAuthority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var record tls.RecordHeaderError
	return errors.As(err, &unknownAuthority) || errors.As(err, &hostname) ||
		errors.As(err, &invalid) || errors.As(err, &record)
}

// TestResult is the structured outcome of a saved-proxy connectivity check.
type TestResult struct {
	OK           bool
	Code         string
	Message      string
	TargetStatus int
	Elapsed      time.Duration
	CheckedAt    time.Time
	Revision     int64
	Stale        bool
}

// TestConnection performs one credential-free HEAD through the current saved
// snapshot. It does not follow redirects, retry, or send provider credentials.
func (m *Manager) TestConnection(ctx context.Context, target string) (TestResult, error) {
	snapshot := m.Snapshot()
	result := TestResult{CheckedAt: time.Now().UTC()}
	if snapshot == nil {
		result.Code = CodeOutboundFailed
		result.Message = "LLM 出站配置不可用"
		return result, nil
	}
	result.Revision = snapshot.Revision()
	if !snapshot.Enabled() {
		return result, ErrProxyDisabled
	}
	targetURL, err := validateTestURL(target)
	if err != nil {
		return result, err
	}
	testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(testCtx, http.MethodHead, targetURL.String(), nil)
	if err != nil {
		return result, fmt.Errorf("%w: 检测地址格式不正确", ErrInvalidConfig)
	}
	// Close this check's connection afterward. Reusing a connection from this
	// same immutable snapshot still exercises the saved proxy path.
	req.Close = true
	// Deliberately no Authorization, Cookie, prompt or proxy credentials here.
	started := time.Now()
	resp, err := snapshot.NewClient(0, false).Do(req)
	result.Elapsed = time.Since(started)
	result.CheckedAt = time.Now().UTC()
	current := m.Snapshot()
	result.Stale = current != nil && current.Revision() != result.Revision
	if err != nil {
		var outbound *Error
		if errors.As(err, &outbound) {
			result.Code = outbound.Code
			result.Message = outbound.Error()
		} else if errors.Is(err, context.DeadlineExceeded) || errors.Is(testCtx.Err(), context.DeadlineExceeded) {
			result.Code = CodeTimeout
			result.Message = "连通性检测超时"
		} else {
			result.Code = CodeOutboundFailed
			result.Message = "出站网络错误"
		}
		return result, nil
	}
	defer resp.Body.Close()
	result.OK = true
	result.TargetStatus = resp.StatusCode
	result.Message = "网络通路可达"
	return result, nil
}

func validateTestURL(raw string) (*url.URL, error) {
	if strings.ContainsAny(raw, "\r\n") {
		return nil, fmt.Errorf("%w: 检测地址不能包含换行", ErrInvalidConfig)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("%w: 检测地址不能为空", ErrInvalidConfig)
	}
	if len(raw) > maxURLLength || strings.ContainsAny(raw, "\r\n") {
		return nil, fmt.Errorf("%w: 检测地址无效", ErrInvalidConfig)
	}
	u, err := url.Parse(raw)
	if err != nil || u == nil || strings.ToLower(u.Scheme) != "https" || u.Hostname() == "" {
		return nil, fmt.Errorf("%w: 检测地址必须是有效的 HTTPS 地址", ErrInvalidConfig)
	}
	if u.User != nil || u.ForceQuery || u.RawQuery != "" || u.Fragment != "" || strings.Contains(raw, "#") {
		return nil, fmt.Errorf("%w: 检测地址不能包含凭据、查询参数或片段", ErrInvalidConfig)
	}
	return u, nil
}
