package llmoutbound

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"strconv"
	"syscall"
	"time"
)

// ErrPrivateTarget 表示用户提供的模型地址解析到了内网、回环或保留地址。
var ErrPrivateTarget = errors.New("模型服务地址指向内网、回环或保留地址，已拒绝访问")

type userTargetKey struct{}

// WithUserSuppliedTarget 标记请求的 base_url 来自用户；直连时在拨号阶段校验
// 解析后的 IP（防 DNS rebinding）。LLM_ALLOW_PRIVATE_TARGETS=true 可关闭校验。
func WithUserSuppliedTarget(ctx context.Context) context.Context {
	return context.WithValue(ctx, userTargetKey{}, true)
}

func userSuppliedTarget(ctx context.Context) bool {
	v, _ := ctx.Value(userTargetKey{}).(bool)
	return v
}

var aliyunMetadata = net.ParseIP("100.100.100.200")

func forbiddenTarget(ip net.IP) bool {
	return ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.Equal(aliyunMetadata)
}

func guardControl(_, address string, _ syscall.RawConn) error {
	if allow, _ := strconv.ParseBool(os.Getenv("LLM_ALLOW_PRIVATE_TARGETS")); allow {
		return nil
	}
	host, _, _ := net.SplitHostPort(address)
	if forbiddenTarget(net.ParseIP(host)) {
		return ErrPrivateTarget
	}
	return nil
}

// guardedTransport 克隆直连 transport，并在拨号时执行目标地址策略。
// 经运营方代理出站时由代理负责出口策略，不使用该 transport。
func guardedTransport(base *http.Transport) *http.Transport {
	guarded := base.Clone()
	guarded.DialContext = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second, Control: guardControl}).DialContext
	return guarded
}
