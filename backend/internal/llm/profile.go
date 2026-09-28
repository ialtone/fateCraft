package llm

import (
	"fmt"
	"net/url"
	"strings"
)

// CapabilityStatus describes whether an endpoint/model feature is known to be
// supported. Unknown is deliberately distinct from unsupported: compatible
// gateways frequently proxy different models behind one endpoint.
type CapabilityStatus string

const (
	CapabilitySupported   CapabilityStatus = "supported"
	CapabilityUnsupported CapabilityStatus = "unsupported"
	CapabilityUnknown     CapabilityStatus = "unknown"
)

// EndpointCapabilities 只包含实际驱动请求行为的能力；描述性字段不放这里。
// 同一端点两种协议的能力可能不同（例如 DeepSeek），所以能力按“档位 × 协议”查询。
type EndpointCapabilities struct {
	ChatStreamUsage  bool // 是否发送 stream_options.include_usage（仅 Chat 流式）
	ParallelToolFlag bool // 是否发送 parallel_tool_calls: false
	StrictTools      bool // 工具定义是否携带 strict
	// JSONSchemaOutput 为 unsupported 时结构化请求提前拦截，错误文案用 JSONSchemaHint。
	JSONSchemaOutput CapabilityStatus
	JSONSchemaHint   string
}

// API identifies the wire protocol selected for a model target.
// It is deliberately resolved on the server; callers must not infer protocol
// behavior from a model name or implement endpoint-specific branches.
type API string

const (
	APIChatCompletions API = API(ProtocolChatCompletions)
	APIResponses       API = API(ProtocolResponses)
)

// CapabilityProfile describes the stable transport contract of an endpoint
// tier. Profiles contain protocol capability only. Gameplay and UI policy do
// not belong here.
//
// 档位只决定可选字段的取舍与确定会失败的请求的提前拦截，永不改写用户选择的协议。
type CapabilityProfile struct {
	ID         string
	ByProtocol map[Protocol]EndpointCapabilities
}

var capabilityProfiles = []CapabilityProfile{
	{ID: "openai", ByProtocol: map[Protocol]EndpointCapabilities{
		ProtocolChatCompletions: {ChatStreamUsage: true, ParallelToolFlag: true, StrictTools: true, JSONSchemaOutput: CapabilitySupported},
		ProtocolResponses:       {ParallelToolFlag: true, StrictTools: true, JSONSchemaOutput: CapabilitySupported},
	}},
	{ID: "deepseek", ByProtocol: map[Protocol]EndpointCapabilities{
		ProtocolChatCompletions: {ChatStreamUsage: true, ParallelToolFlag: true, JSONSchemaOutput: CapabilityUnsupported,
			JSONSchemaHint: "DeepSeek 的 Chat Completions 协议不支持 JSON Schema 结构化输出（记忆、角色问答需要），请在设置中把接口协议改为 Responses。"},
		ProtocolResponses: {JSONSchemaOutput: CapabilitySupported},
	}},
	{ID: "openrouter", ByProtocol: map[Protocol]EndpointCapabilities{
		ProtocolChatCompletions: {ChatStreamUsage: true, JSONSchemaOutput: CapabilityUnknown},
		ProtocolResponses:       {JSONSchemaOutput: CapabilityUnknown},
	}},
	{ID: "custom_openai_compatible", ByProtocol: map[Protocol]EndpointCapabilities{
		ProtocolChatCompletions: {JSONSchemaOutput: CapabilityUnknown},
		ProtocolResponses:       {JSONSchemaOutput: CapabilityUnknown},
	}},
}

// profileByID looks up a tier by ID. All IDs are internal constants; an
// unknown ID is a programming error and fails fast.
func profileByID(id string) CapabilityProfile {
	for _, profile := range capabilityProfiles {
		if profile.ID == id {
			return profile
		}
	}
	panic("unknown capability profile: " + id)
}

// ResolveProfile identifies the endpoint capability tier from the base URL
// alone. Only official domains are recognized; everything else — including
// unparsable and empty URLs — is treated as a custom OpenAI-compatible
// gateway that honors the manually selected protocol.
//
// Profiles are capability assumptions about an endpoint, not user options:
// there is no provider field for the user to set, and tiers never rewrite
// the user-selected protocol. The openai match is an
// exact host comparison on purpose: the openai tier assumes every capability
// is supported, so a loose match (e.g. openai-proxy.example.com) would send
// strict and parallel-tool flags to gateways that reject them. deepseek and
// openrouter match their official registrable domains by suffix so that
// third-party lookalike domains are not mistaken for official endpoints.
func ResolveProfile(baseURL string) CapabilityProfile {
	host := ""
	if parsed, err := url.Parse(strings.TrimSpace(baseURL)); err == nil {
		host = strings.ToLower(parsed.Hostname())
	}
	switch {
	case host == "api.openai.com":
		return profileByID("openai")
	case host == "deepseek.com" || strings.HasSuffix(host, ".deepseek.com"):
		return profileByID("deepseek")
	case host == "openrouter.ai" || strings.HasSuffix(host, ".openrouter.ai"):
		return profileByID("openrouter")
	default:
		return profileByID("custom_openai_compatible")
	}
}

// ResolveEndpointCapabilities returns the request capabilities for a
// (profile, protocol) pair. The protocol is normalized first; every profile
// carries both protocols, so a missing entry can only mean a programming
// error elsewhere — the zero value (send nothing, intercept nothing) is the
// safe fallback.
func ResolveEndpointCapabilities(baseURL string, protocol Protocol) EndpointCapabilities {
	profile := ResolveProfile(baseURL)
	if capabilities, ok := profile.ByProtocol[NormalizeProtocol(string(protocol))]; ok {
		return capabilities
	}
	return EndpointCapabilities{}
}

// EndpointURL builds the request URL for an endpoint path. DeepSeek 的
// Responses 不在 /v1 下：用户存的 base_url 通常是 Chat 时代的
// https://api.deepseek.com/v1，直接拼接会得到 /v1/responses 而 404；
// deepseek 档走 /responses 时去掉 /v1 后缀，让同一地址两种协议都可用。
func EndpointURL(baseURL, endpoint string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if endpoint == "/responses" && ResolveProfile(baseURL).ID == "deepseek" {
		base = strings.TrimSuffix(base, "/v1")
	}
	return base + endpoint
}

type RequestKind string

const (
	RequestKindText       RequestKind = "text"
	RequestKindAgent      RequestKind = "agent"
	RequestKindStructured RequestKind = "structured"
)

var ErrCapabilityUnsupported = fmt.Errorf("模型能力不支持请求")

// PlanParams applies transport capability rules at the backend boundary. It
// never rewrites the user-selected protocol; it only drops optional fields
// the endpoint would reject and intercepts structured requests the endpoint
// is known to fail, telling the user how to fix it.
func PlanParams(p Params, kind RequestKind) (Params, error) {
	p.Protocol = NormalizeProtocol(string(p.Protocol))
	if kind == RequestKindStructured {
		structured := (p.StructuredOutput != nil && p.StructuredOutput.Schema != nil) ||
			(p.ResponseFormat != nil && p.ResponseFormat.Type == "json_schema")
		if structured {
			capabilities := ResolveEndpointCapabilities(p.BaseURL, p.Protocol)
			if capabilities.JSONSchemaOutput == CapabilityUnsupported {
				return p, fmt.Errorf("%w: %s", ErrCapabilityUnsupported, capabilities.JSONSchemaHint)
			}
		}
	}
	return p, nil
}
