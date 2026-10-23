// Package llmoutbound owns the immutable HTTP exit configuration used by every
// server-side LLM request. It deliberately knows nothing about storage, users or
// the HTTP API; callers hand it a validated configuration and publish the
// resulting reusable transport snapshot.
package llmoutbound

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// ResponseHeaderTimeout matches the existing LLM streaming behaviour: wait
	// for headers, then let the caller context and idle watchdog govern the body.
	ResponseHeaderTimeout = 2 * time.Minute

	maxURLLength      = 2048
	maxUsernameLength = 256
	maxPasswordLength = 1024
	maxCAPEMLength    = 256 << 10
)

var (
	// ErrInvalidConfig is returned for proxy settings that cannot be used.
	ErrInvalidConfig = errors.New("LLM 出站代理配置无效")
	// ErrProxyDisabled is returned by connectivity tests before a proxy is enabled.
	ErrProxyDisabled = errors.New("LLM 出站代理未启用")
)

// Config is a validated plaintext proxy configuration. Password is only held in
// memory while building a transport snapshot and is never serialized.
type Config struct {
	Enabled  bool
	URL      string
	Username string
	Password string
	CAPEM    string
}

// Validate checks the complete config, including the already-merged password.
func Validate(c Config) error {
	if strings.ContainsAny(c.URL, "\r\n") {
		return fmt.Errorf("%w: 地址不能包含换行", ErrInvalidConfig)
	}
	rawURL := strings.TrimSpace(c.URL)
	if rawURL == "" {
		if c.Enabled {
			return fmt.Errorf("%w: 启用代理时地址不能为空", ErrInvalidConfig)
		}
	} else {
		if err := validateProxyURL(rawURL); err != nil {
			return err
		}
	}
	if len(c.Username) > maxUsernameLength {
		return fmt.Errorf("%w: 用户名不能超过 %d 个字符", ErrInvalidConfig, maxUsernameLength)
	}
	if len(c.Password) > maxPasswordLength {
		return fmt.Errorf("%w: 密码不能超过 %d 个字符", ErrInvalidConfig, maxPasswordLength)
	}
	if strings.ContainsAny(c.Username, "\r\n") {
		return fmt.Errorf("%w: 用户名不能包含换行", ErrInvalidConfig)
	}
	if strings.Contains(c.Username, ":") {
		return fmt.Errorf("%w: Basic Auth 用户名不能包含冒号", ErrInvalidConfig)
	}
	if strings.ContainsAny(c.Password, "\r\n") {
		return fmt.Errorf("%w: 密码不能包含换行", ErrInvalidConfig)
	}
	if c.CAPEM != "" {
		if err := validateCAPEM(c.CAPEM); err != nil {
			return err
		}
	}
	return nil
}

func validateProxyURL(raw string) error {
	if len(raw) > maxURLLength {
		return fmt.Errorf("%w: 地址不能超过 %d 个字符", ErrInvalidConfig, maxURLLength)
	}
	if strings.ContainsAny(raw, "\r\n") {
		return fmt.Errorf("%w: 地址不能包含换行", ErrInvalidConfig)
	}
	u, err := url.Parse(raw)
	if err != nil || u == nil {
		return fmt.Errorf("%w: 地址格式不正确", ErrInvalidConfig)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("%w: 只支持 http 或 https 代理", ErrInvalidConfig)
	}
	if u.User != nil {
		return fmt.Errorf("%w: 地址不能包含用户名或密码，请使用独立认证字段", ErrInvalidConfig)
	}
	if u.Host == "" || u.Hostname() == "" {
		return fmt.Errorf("%w: 地址必须包含主机名", ErrInvalidConfig)
	}
	port, portErr := strconv.Atoi(u.Port())
	if portErr != nil || port < 1 || port > 65535 {
		return fmt.Errorf("%w: 地址必须明确指定端口", ErrInvalidConfig)
	}
	if u.ForceQuery || u.RawQuery != "" || u.Fragment != "" || strings.Contains(raw, "#") {
		return fmt.Errorf("%w: 地址不能包含查询参数或片段", ErrInvalidConfig)
	}
	if u.Path != "" && u.Path != "/" {
		return fmt.Errorf("%w: 地址不能包含业务路径", ErrInvalidConfig)
	}
	return nil
}

func validateCAPEM(pem string) error {
	if len(pem) > maxCAPEMLength {
		return fmt.Errorf("%w: CA 证书不能超过 %d 字节", ErrInvalidConfig, maxCAPEMLength)
	}
	if strings.Contains(strings.ToUpper(pem), "PRIVATE KEY") {
		return fmt.Errorf("%w: 这里只能填写 CA 证书，不能包含私钥", ErrInvalidConfig)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(pem)) {
		return fmt.Errorf("%w: CA 证书不是有效的 PEM 证书", ErrInvalidConfig)
	}
	return nil
}

// Build creates an independent reusable snapshot. It performs no network I/O;
// an unreachable proxy is a runtime error, not a save-time error.
func Build(c Config, revision int64) (*Snapshot, error) {
	if err := Validate(c); err != nil {
		return nil, err
	}
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok || base == nil {
		return nil, errors.New("HTTP 默认 Transport 不可用")
	}
	transport := base.Clone()
	// Explicit direct means exactly direct: never inherit HTTP_PROXY/HTTPS_PROXY/
	// NO_PROXY for LLM traffic. Non-LLM clients keep their existing behaviour.
	transport.Proxy = nil
	transport.ResponseHeaderTimeout = ResponseHeaderTimeout
	transport.MaxResponseHeaderBytes = 1 << 20

	// The extra CA belongs to the LLM client only. It applies to both the proxy
	// hop and the model target, and never changes the process-wide root pool.
	if c.CAPEM != "" {
		roots, err := x509.SystemCertPool()
		if err != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM([]byte(c.CAPEM)) {
			return nil, fmt.Errorf("%w: CA 证书不是有效的 PEM 证书", ErrInvalidConfig)
		}
		tlsConfig := transport.TLSClientConfig
		if tlsConfig == nil {
			tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		} else {
			tlsConfig = tlsConfig.Clone()
		}
		tlsConfig.RootCAs = roots
		transport.TLSClientConfig = tlsConfig
	}

	proxyAddr := ""
	if c.Enabled {
		proxyURL, err := url.Parse(strings.TrimSpace(c.URL))
		if err != nil {
			return nil, fmt.Errorf("%w: 地址格式不正确", ErrInvalidConfig)
		}
		proxyAddr = proxyURL.Host
		if c.Username != "" || c.Password != "" {
			proxyURL.User = url.UserPassword(c.Username, c.Password)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
		// At this stage the TCP destination is unambiguously the proxy. Matching
		// net.OpError.Addr to a hostname is unreliable after DNS resolution.
		dial := transport.DialContext
		if dial == nil {
			dial = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext
		}
		transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
			conn, err := dial(ctx, network, address)
			if err != nil {
				code := CodeProxyUnreachable
				var timeout net.Error
				if errors.As(err, &timeout) && timeout.Timeout() {
					code = CodeTimeout
				}
				return nil, &Error{Code: code, Err: err}
			}
			return conn, nil
		}
		transport.OnProxyConnectResponse = func(_ context.Context, _ *url.URL, _ *http.Request, response *http.Response) error {
			if response.StatusCode == http.StatusProxyAuthRequired {
				return &Error{Code: CodeProxyAuthFailed}
			}
			if response.StatusCode != http.StatusOK {
				return &Error{Code: CodeOutboundFailed}
			}
			return nil
		}
	}
	managed := &managedTransport{base: transport, proxyAddr: proxyAddr, proxied: c.Enabled}
	if !c.Enabled {
		managed.guarded = guardedTransport(transport)
	}
	return &Snapshot{
		revision:  revision,
		enabled:   c.Enabled,
		transport: managed,
		client:    &http.Client{Transport: managed},
	}, nil
}

// CAPEMFingerprint returns a stable fingerprint for audit/display purposes. It
// never reveals the certificate content.
func CAPEMFingerprint(pem string) string {
	if pem == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(pem))
	return hex.EncodeToString(sum[:])
}
