package llm

import (
	"fmt"
	"net/url"
	"strings"
)

// CapabilityStatus describes whether a provider/model feature is known to be
// supported. Unknown is deliberately distinct from unsupported: compatible
// gateways frequently proxy different models behind one endpoint.
type CapabilityStatus string

const (
	CapabilitySupported   CapabilityStatus = "supported"
	CapabilityUnsupported CapabilityStatus = "unsupported"
	CapabilityUnknown     CapabilityStatus = "unknown"
)

type FeatureCapability struct {
	Status CapabilityStatus `json:"status"`
	Reason string           `json:"reason,omitempty"`
}

// ModelCapabilities is the server-owned request capability matrix. It
// contains transport facts only; gameplay policy and UI labels do not belong
// here.
type ModelCapabilities struct {
	Provider                string            `json:"provider"`
	Model                   string            `json:"model"`
	Protocols               []Protocol        `json:"protocols"`
	Streaming               FeatureCapability `json:"streaming"`
	TerminalEventRequired   bool              `json:"terminal_event_required"`
	Reasoning               FeatureCapability `json:"reasoning"`
	AllowedReasoningEfforts []string          `json:"allowed_reasoning_efforts,omitempty"`
	Tools                   FeatureCapability `json:"tools"`
	ParallelToolCalls       FeatureCapability `json:"parallel_tool_calls"`
	StreamedToolArguments   FeatureCapability `json:"streamed_tool_arguments"`
	StructuredJSONSchema    FeatureCapability `json:"structured_json_schema"`
	StructuredJSONObject    FeatureCapability `json:"structured_json_object"`
	ImageInput              FeatureCapability `json:"image_input"`
	Temperature             FeatureCapability `json:"temperature"`
	TopP                    FeatureCapability `json:"top_p"`
	MaxTokensParameter      string            `json:"max_tokens_parameter,omitempty"`
	ContextWindowKnown      bool              `json:"context_window_known"`
}

// API identifies the wire protocol selected for a model target.
// It is deliberately resolved on the server; callers must not infer protocol
// behavior from a model name or implement provider-specific branches.
type API string

const (
	APIChatCompletions API = API(ProtocolChatCompletions)
	APIResponses       API = API(ProtocolResponses)
)

// ProviderProfile describes the stable transport contract of a provider.
// Profiles contain protocol capability only. Gameplay and UI policy do not
// belong here.
type ProviderProfile struct {
	ID               string            `json:"id"`
	DefaultAPI       Protocol          `json:"default_api"`
	AllowedAPIs      []Protocol        `json:"allowed_apis"`
	ThinkingFormat   string            `json:"thinking_format,omitempty"`
	NativeJSONSchema bool              `json:"native_json_schema"`
	Capabilities     ModelCapabilities `json:"capabilities"`
}

var providerProfiles = []ProviderProfile{
	{ID: "deepseek", DefaultAPI: ProtocolChatCompletions, AllowedAPIs: []Protocol{ProtocolChatCompletions}, ThinkingFormat: "deepseek", NativeJSONSchema: false,
		Capabilities: ModelCapabilities{Protocols: []Protocol{ProtocolChatCompletions}, Streaming: supported("Chat Completions SSE"), TerminalEventRequired: true,
			Reasoning: supported("DeepSeek reasoning_content"), AllowedReasoningEfforts: []string{"low", "medium", "high"}, Tools: supported("function calling"), ParallelToolCalls: unsupported("为保证工具顺序，运行时关闭并行工具"), StreamedToolArguments: supported("Chat Completions tool-call delta"),
			StructuredJSONSchema: unsupported("DeepSeek 兼容接口不保证严格 JSON Schema"), StructuredJSONObject: supported("response_format json_object"), ImageInput: unknown("需按具体模型确认"), Temperature: supported("temperature"), TopP: supported("top_p"), MaxTokensParameter: "max_tokens"}},
	{ID: "openai", DefaultAPI: ProtocolResponses, AllowedAPIs: []Protocol{ProtocolResponses, ProtocolChatCompletions}, ThinkingFormat: "openai", NativeJSONSchema: true,
		Capabilities: ModelCapabilities{Protocols: []Protocol{ProtocolResponses, ProtocolChatCompletions}, Streaming: supported("Responses/Chat SSE"), TerminalEventRequired: true,
			Reasoning: supported("reasoning effort"), AllowedReasoningEfforts: []string{"minimal", "low", "medium", "high"}, Tools: supported("function calling"), ParallelToolCalls: supported("provider default"), StreamedToolArguments: supported("Responses function-call delta"),
			StructuredJSONSchema: supported("native structured outputs"), StructuredJSONObject: supported("JSON object output"), ImageInput: supported("model dependent"), Temperature: supported("temperature"), TopP: supported("top_p"), MaxTokensParameter: "protocol-dependent"}},
	{ID: "openrouter", DefaultAPI: ProtocolChatCompletions, AllowedAPIs: []Protocol{ProtocolChatCompletions, ProtocolResponses}, ThinkingFormat: "openrouter", NativeJSONSchema: false,
		Capabilities: ModelCapabilities{Protocols: []Protocol{ProtocolChatCompletions, ProtocolResponses}, Streaming: supported("gateway SSE"), TerminalEventRequired: true,
			Reasoning: unknown("由路由到的具体模型决定"), Tools: unknown("由路由到的具体模型决定"), ParallelToolCalls: unknown("由路由到的具体模型决定"), StreamedToolArguments: unknown("由路由到的具体模型决定"),
			StructuredJSONSchema: unknown("由路由到的具体模型决定"), StructuredJSONObject: unknown("由路由到的具体模型决定"), ImageInput: unknown("由路由到的具体模型决定"), Temperature: unknown("由路由到的具体模型决定"), TopP: unknown("由路由到的具体模型决定"), MaxTokensParameter: "protocol-dependent"}},
}

// ResolveProfile resolves a known provider from the endpoint host. Unknown
// endpoints are treated as custom OpenAI-compatible targets and retain the
// explicitly configured protocol.
func ResolveProfile(provider, baseURL string) ProviderProfile {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "deepseek" {
		return providerProfiles[0]
	}
	if provider == "openrouter" {
		return providerProfiles[2]
	}
	if provider == "openai" {
		return providerProfiles[1]
	}
	host := ""
	if parsed, err := url.Parse(strings.TrimSpace(baseURL)); err == nil {
		host = strings.ToLower(parsed.Hostname())
	}
	switch {
	case strings.Contains(host, "deepseek"):
		return providerProfiles[0]
	case strings.Contains(host, "openrouter"):
		return providerProfiles[2]
	case strings.Contains(host, "openai"):
		return providerProfiles[1]
	default:
		return ProviderProfile{ID: "custom_openai_compatible", DefaultAPI: ProtocolResponses, AllowedAPIs: []Protocol{ProtocolChatCompletions, ProtocolResponses},
			Capabilities: ModelCapabilities{Protocols: []Protocol{ProtocolChatCompletions, ProtocolResponses}, Streaming: unknown("自定义兼容网关"), TerminalEventRequired: true,
				Reasoning: unknown("自定义兼容网关"), Tools: unknown("自定义兼容网关"), ParallelToolCalls: unknown("自定义兼容网关"), StreamedToolArguments: unknown("自定义兼容网关"),
				StructuredJSONSchema: unknown("自定义兼容网关"), StructuredJSONObject: unknown("自定义兼容网关"), ImageInput: unknown("自定义兼容网关"), Temperature: unknown("自定义兼容网关"), TopP: unknown("自定义兼容网关"), MaxTokensParameter: "protocol-dependent"}}
	}
}

func supported(reason string) FeatureCapability {
	return FeatureCapability{Status: CapabilitySupported, Reason: reason}
}
func unsupported(reason string) FeatureCapability {
	return FeatureCapability{Status: CapabilityUnsupported, Reason: reason}
}
func unknown(reason string) FeatureCapability {
	return FeatureCapability{Status: CapabilityUnknown, Reason: reason}
}

// ResolveModelCapabilities returns a copy so callers cannot mutate the global
// provider defaults. Model-specific overrides can be added here later without
// leaking model-name branches into protocol adapters or the frontend.
func ResolveModelCapabilities(provider, baseURL, model string) ModelCapabilities {
	profile := ResolveProfile(provider, baseURL)
	capabilities := profile.Capabilities
	capabilities.Provider = profile.ID
	capabilities.Model = strings.TrimSpace(model)
	capabilities.Protocols = append([]Protocol(nil), capabilities.Protocols...)
	capabilities.AllowedReasoningEfforts = append([]string(nil), capabilities.AllowedReasoningEfforts...)
	return capabilities
}

type RequestKind string

const (
	RequestKindText       RequestKind = "text"
	RequestKindAgent      RequestKind = "agent"
	RequestKindStructured RequestKind = "structured"
)

var ErrCapabilityUnsupported = fmt.Errorf("模型能力不支持请求")

// PlanParams applies transport capability rules at the backend boundary. It
// only removes optional parameters when a capability is explicitly marked
// unsupported; unknown capabilities remain untouched for custom gateways.
func PlanParams(p Params, kind RequestKind) (Params, error) {
	p.Protocol = ResolveProtocolForTarget(p.Provider, p.BaseURL, string(p.Protocol))
	capabilities := ResolveModelCapabilities(p.Provider, p.BaseURL, p.Model)
	if capabilities.Reasoning.Status == CapabilityUnsupported {
		p.ReasoningEffort = ""
	}
	if kind == RequestKindStructured {
		if p.StructuredOutput != nil && p.StructuredOutput.Schema != nil && capabilities.StructuredJSONSchema.Status == CapabilityUnsupported {
			return p, fmt.Errorf("%w: %s", ErrCapabilityUnsupported, capabilities.StructuredJSONSchema.Reason)
		}
		if p.ResponseFormat != nil && p.ResponseFormat.Type == "json_schema" && capabilities.StructuredJSONSchema.Status == CapabilityUnsupported {
			return p, fmt.Errorf("%w: %s", ErrCapabilityUnsupported, capabilities.StructuredJSONSchema.Reason)
		}
	}
	_ = kind // reserved for future per-request capability planning
	return p, nil
}

// ResolveProtocol applies the provider profile. Known providers use their
// stable default and capability set; custom endpoints honor explicit config.
// DeepSeek is intentionally Chat Completions-only because its thinking/tool
// replay contract is not compatible with the generic Responses path.
func ResolveProtocolForTarget(provider, baseURL, requested string) Protocol {
	profile := ResolveProfile(provider, baseURL)
	if profile.ID != "custom_openai_compatible" {
		for _, allowed := range profile.AllowedAPIs {
			if Protocol(strings.TrimSpace(requested)) == allowed {
				return allowed
			}
		}
		return profile.DefaultAPI
	}
	return NormalizeProtocol(requested)
}

// ResolveProtocol is kept as a small convenience for custom callers that do
// not yet carry a provider id. New request paths should use
// ResolveProtocolForTarget so the persisted Provider Profile is authoritative.
func ResolveProtocol(baseURL, requested string) Protocol {
	return ResolveProtocolForTarget("", baseURL, requested)
}

// Profiles returns a copy suitable for API metadata responses.
func Profiles() []ProviderProfile {
	out := make([]ProviderProfile, len(providerProfiles))
	copy(out, providerProfiles)
	return out
}
