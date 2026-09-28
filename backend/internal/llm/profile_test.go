package llm

import (
	"errors"
	"strings"
	"testing"
)

func TestResolveProfileByBaseURL(t *testing.T) {
	tests := []struct {
		name, baseURL, want string
	}{
		{name: "openai exact host", baseURL: "https://api.openai.com/v1", want: "openai"},
		{name: "openai uppercase host", baseURL: "https://API.OPENAI.COM/v1", want: "openai"},
		{name: "openai lookalike subdomain is custom", baseURL: "https://openai-proxy.example.com/v1", want: "custom_openai_compatible"},
		{name: "openai suffix attack is custom", baseURL: "https://api.openai.com.evil.com/v1", want: "custom_openai_compatible"},
		{name: "deepseek official", baseURL: "https://api.deepseek.com", want: "deepseek"},
		{name: "deepseek with path", baseURL: "https://api.deepseek.com/v1", want: "deepseek"},
		{name: "deepseek lookalike domain is custom", baseURL: "https://mydeepseek-proxy.com/v1", want: "custom_openai_compatible"},
		{name: "openrouter official", baseURL: "https://openrouter.ai/api/v1", want: "openrouter"},
		{name: "openrouter subdomain", baseURL: "https://gateway.openrouter.ai/api/v1", want: "openrouter"},
		{name: "openrouter lookalike domain is custom", baseURL: "https://myopenrouter-proxy.com/v1", want: "custom_openai_compatible"},
		{name: "unknown gateway is custom", baseURL: "http://localhost:9999/v1", want: "custom_openai_compatible"},
		{name: "empty url is custom", baseURL: "", want: "custom_openai_compatible"},
		{name: "unparsable url is custom", baseURL: "http://[::1", want: "custom_openai_compatible"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveProfile(tt.baseURL); got.ID != tt.want {
				t.Fatalf("ResolveProfile(%q) = %q, want %q", tt.baseURL, got.ID, tt.want)
			}
		})
	}
}

func TestProtocolIsNeverRewritten(t *testing.T) {
	baseURLs := []string{
		"https://api.openai.com/v1",
		"https://api.deepseek.com/v1",
		"https://openrouter.ai/api/v1",
		"http://localhost:9999/v1",
	}
	for _, baseURL := range baseURLs {
		for _, requested := range []Protocol{ProtocolChatCompletions, ProtocolResponses} {
			if got := NormalizeProtocol(string(requested)); got != requested {
				t.Fatalf("NormalizeProtocol(%q) = %q, want %q", requested, got, requested)
			}
			planned, err := PlanParams(Params{BaseURL: baseURL, Protocol: requested}, RequestKindText)
			if err != nil {
				t.Fatalf("PlanParams(%q, %q) err = %v", baseURL, requested, err)
			}
			if planned.Protocol != requested {
				t.Fatalf("PlanParams(%q, %q) protocol = %q, want %q", baseURL, requested, planned.Protocol, requested)
			}
		}
	}
}

func TestProjectAgentContent(t *testing.T) {
	result := AgentStepResult{
		Text:      "前导文本",
		ToolCalls: []ToolCall{{ID: "call-1", Name: "fc_state_set_text", Arguments: `{}`}},
	}
	content := projectAgentContent(result)
	if len(content) != 2 || content[0].Kind != ContentText || content[1].Kind != ContentToolCall {
		t.Fatalf("unexpected ordered content: %#v", content)
	}
}

func TestResolveEndpointCapabilities(t *testing.T) {
	tests := []struct {
		name, baseURL string
		protocol      Protocol
		want          EndpointCapabilities
	}{
		{name: "openai chat", baseURL: "https://api.openai.com/v1", protocol: ProtocolChatCompletions,
			want: EndpointCapabilities{ChatStreamUsage: true, ParallelToolFlag: true, StrictTools: true, JSONSchemaOutput: CapabilitySupported}},
		{name: "openai responses", baseURL: "https://api.openai.com/v1", protocol: ProtocolResponses,
			want: EndpointCapabilities{ParallelToolFlag: true, StrictTools: true, JSONSchemaOutput: CapabilitySupported}},
		{name: "deepseek chat", baseURL: "https://api.deepseek.com/v1", protocol: ProtocolChatCompletions,
			want: EndpointCapabilities{ChatStreamUsage: true, ParallelToolFlag: true, JSONSchemaOutput: CapabilityUnsupported,
				JSONSchemaHint: "DeepSeek 的 Chat Completions 协议不支持 JSON Schema 结构化输出（记忆、角色问答需要），请在设置中把接口协议改为 Responses。"}},
		{name: "deepseek responses", baseURL: "https://api.deepseek.com/v1", protocol: ProtocolResponses,
			want: EndpointCapabilities{JSONSchemaOutput: CapabilitySupported}},
		{name: "openrouter chat", baseURL: "https://openrouter.ai/api/v1", protocol: ProtocolChatCompletions,
			want: EndpointCapabilities{ChatStreamUsage: true, JSONSchemaOutput: CapabilityUnknown}},
		{name: "openrouter responses", baseURL: "https://openrouter.ai/api/v1", protocol: ProtocolResponses,
			want: EndpointCapabilities{JSONSchemaOutput: CapabilityUnknown}},
		{name: "custom chat", baseURL: "http://localhost:9999/v1", protocol: ProtocolChatCompletions,
			want: EndpointCapabilities{JSONSchemaOutput: CapabilityUnknown}},
		{name: "custom responses", baseURL: "http://localhost:9999/v1", protocol: ProtocolResponses,
			want: EndpointCapabilities{JSONSchemaOutput: CapabilityUnknown}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveEndpointCapabilities(tt.baseURL, tt.protocol); got != tt.want {
				t.Fatalf("ResolveEndpointCapabilities() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestEndpointURL(t *testing.T) {
	tests := []struct {
		name, baseURL, endpoint, want string
	}{
		{name: "deepseek responses drops v1 suffix", baseURL: "https://api.deepseek.com/v1", endpoint: "/responses", want: "https://api.deepseek.com/responses"},
		{name: "deepseek responses without suffix", baseURL: "https://api.deepseek.com", endpoint: "/responses", want: "https://api.deepseek.com/responses"},
		{name: "deepseek chat keeps v1", baseURL: "https://api.deepseek.com/v1", endpoint: "/chat/completions", want: "https://api.deepseek.com/v1/chat/completions"},
		{name: "openai responses keeps v1", baseURL: "https://api.openai.com/v1", endpoint: "/responses", want: "https://api.openai.com/v1/responses"},
		{name: "custom responses untouched", baseURL: "http://localhost:9999/v1", endpoint: "/responses", want: "http://localhost:9999/v1/responses"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EndpointURL(tt.baseURL, tt.endpoint); got != tt.want {
				t.Fatalf("EndpointURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPlanParamsRejectsKnownUnsupportedStructuredSchema(t *testing.T) {
	_, err := PlanParams(Params{
		BaseURL: "https://api.deepseek.com", Model: "deepseek-chat",
		Protocol:       ProtocolChatCompletions,
		ResponseFormat: &ResponseFormat{Type: "json_schema", JSONSchema: &JSONSchema{Name: "x", Schema: map[string]any{"type": "object"}, Strict: true}},
	}, RequestKindStructured)
	if err == nil || !errors.Is(err, ErrCapabilityUnsupported) {
		t.Fatalf("err=%v, want capability rejection", err)
	}
	if !strings.Contains(err.Error(), "改为 Responses") {
		t.Fatalf("err=%v, want hint to switch to Responses", err)
	}
}

func TestPlanParamsAllowsSupportedStructuredSchema(t *testing.T) {
	structured := func() Params {
		return Params{
			BaseURL: "https://api.deepseek.com", Model: "deepseek-chat",
			StructuredOutput: &StructuredOutput{Mode: OutputModeNativeJSONSchema, Schema: &JSONSchema{Name: "x", Schema: map[string]any{"type": "object"}}},
		}
	}
	t.Run("deepseek responses", func(t *testing.T) {
		params := structured()
		params.Protocol = ProtocolResponses
		if _, err := PlanParams(params, RequestKindStructured); err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
	})
	for _, protocol := range []Protocol{ProtocolChatCompletions, ProtocolResponses} {
		t.Run("custom "+string(protocol), func(t *testing.T) {
			params := structured()
			params.BaseURL = "http://localhost:9999/v1"
			params.Protocol = protocol
			if _, err := PlanParams(params, RequestKindStructured); err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
		})
	}
}
