package llm

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
)

// Relay 请求经浏览器执行的错误哨兵。分类规则见 transport.go 的
// retryableTransportError：只有 ErrRelayUnavailable 可重试（响应体交付前）。
var (
	// ErrRelayUnavailable 没有可用的 Relay 连接，可重试。
	ErrRelayUnavailable = errors.New("客户端直连未就绪")
	// ErrRelayLost 连接在响应头之后断开，不可重试。
	ErrRelayLost = errors.New("客户端直连中断")
	// ErrRelayNetwork 浏览器 fetch 失败（含 CORS、DNS、混合内容），不可重试。
	ErrRelayNetwork = errors.New("浏览器无法直接访问模型服务")
	// ErrRelayNoKey 本设备未配置 Key，不可重试。
	ErrRelayNoKey = errors.New("本设备尚未保存 API Key")
)

// Relay 允许的服务端下发 endpoint 白名单。URL 与 header 全部由浏览器按本地
// 设置固定拼接，服务端只能指定这两个 endpoint 之一。
var RelayAllowedEndpoints = map[string]bool{
	"/chat/completions": true,
	"/responses":        true,
}

// RelayRequest 是服务端构建好的一次 HTTP 调用。Body 与直连完全相同，
// 客户端不得解析或改写，原样作为 fetch body。
type RelayRequest struct {
	Endpoint string // 仅允许 RelayAllowedEndpoints 中的值
	Body     []byte // 与直连完全相同的请求体
	Stream   bool
	Purpose  string // narrative / memory / character_inquiry / protocol_test
	// ConversationID 仅用于客户端展示与日志，可空。
	ConversationID uuid.UUID
}

// RelayTransport 执行一次 HTTP 调用并返回原始响应；Body 必须是原始字节流，
// 服务端继续承担全部协议解析与校验。
type RelayTransport interface {
	Do(ctx context.Context, req RelayRequest) (*http.Response, error)
}

// RelayErrorCode 把 Relay 错误映射为对用户展示的错误码（§4.9）。
func RelayErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrRelayUnavailable):
		return "llm_relay_unavailable"
	case errors.Is(err, ErrRelayLost):
		return "llm_relay_lost"
	case errors.Is(err, ErrRelayNetwork):
		return "llm_relay_network"
	case errors.Is(err, ErrRelayNoKey):
		return "llm_relay_no_key"
	default:
		return ""
	}
}
