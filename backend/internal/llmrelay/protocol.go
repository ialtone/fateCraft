package llmrelay

import (
	"github.com/ialtone/fatecraft/internal/llm"
)

// Relay 协议（/ws/llm-relay，JSON 文本帧）。body 为请求体 JSON 字符串，
// 客户端不得解析或改写，原样作为 fetch body。

const (
	msgRelayRequest = "relay.request"
	msgRelayCancel  = "relay.cancel"

	msgRelayHead  = "relay.response.head"
	msgRelayChunk = "relay.response.chunk"
	msgRelayEnd   = "relay.response.end"
	msgRelayError = "relay.response.error"
)

// 服务端 → 客户端：发起请求。
type serverRequestFrame struct {
	Type           string `json:"type"`
	RelayID        string `json:"relay_id"`
	Endpoint       string `json:"endpoint"`
	Stream         bool   `json:"stream"`
	Purpose        string `json:"purpose"`
	ConversationID string `json:"conversation_id,omitempty"`
	Body           string `json:"body"`
}

// 服务端 → 客户端：取消请求。
type serverCancelFrame struct {
	Type    string `json:"type"`
	RelayID string `json:"relay_id"`
}

// 客户端 → 服务端各帧的公共头。
type clientFrameHeader struct {
	Type    string `json:"type"`
	RelayID string `json:"relay_id"`
}

// 收到上游响应头。
type clientHeadFrame struct {
	Type        string `json:"type"`
	RelayID     string `json:"relay_id"`
	Status      int    `json:"status"`
	ContentType string `json:"content_type,omitempty"`
	RetryAfter  string `json:"retry_after,omitempty"`
}

// 原始响应文本片段，用 TextDecoder stream 模式解码，每帧 ≤ 64KiB。
type clientChunkFrame struct {
	Type    string `json:"type"`
	RelayID string `json:"relay_id"`
	Seq     int    `json:"seq"`
	Data    string `json:"data"`
}

// 正常结束。Seq 等于已发送的 chunk 总数（无 chunk 时为 0）。
type clientEndFrame struct {
	Type    string `json:"type"`
	RelayID string `json:"relay_id"`
	Seq     int    `json:"seq"`
}

// 失败。code 取值：network | no_key | aborted | invalid_request。
type clientErrorFrame struct {
	Type    string `json:"type"`
	RelayID string `json:"relay_id"`
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

// 客户端错误码到服务端哨兵的映射（响应头到达之前）。
func preHeadError(code string) error {
	switch code {
	case "network":
		return llm.ErrRelayNetwork
	case "no_key":
		return llm.ErrRelayNoKey
	default:
		return llm.ErrRelayLost
	}
}
