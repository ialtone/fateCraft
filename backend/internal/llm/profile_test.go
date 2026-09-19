package llm

import (
	"errors"
	"testing"
)

func TestResolveProtocolForTarget(t *testing.T) {
	tests := []struct {
		name, provider, baseURL, requested string
		want                               Protocol
	}{
		{name: "deepseek never uses responses", provider: "deepseek", baseURL: "https://api.deepseek.com", requested: string(ProtocolResponses), want: ProtocolChatCompletions},
		{name: "deepseek host is detected", baseURL: "https://api.deepseek.com/v1", requested: string(ProtocolResponses), want: ProtocolChatCompletions},
		{name: "openai honors explicit chat", provider: "openai", baseURL: "https://api.openai.com/v1", requested: string(ProtocolChatCompletions), want: ProtocolChatCompletions},
		{name: "custom honors explicit responses", provider: "custom", baseURL: "http://localhost:9999/v1", requested: string(ProtocolResponses), want: ProtocolResponses},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveProtocolForTarget(tt.provider, tt.baseURL, tt.requested); got != tt.want {
				t.Fatalf("ResolveProtocolForTarget() = %q, want %q", got, tt.want)
			}
		})
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

func TestResolveModelCapabilitiesKeepsProviderFactsServerSide(t *testing.T) {
	capabilities := ResolveModelCapabilities("deepseek", "https://api.deepseek.com", "deepseek-chat")
	if capabilities.Provider != "deepseek" || capabilities.Model != "deepseek-chat" {
		t.Fatalf("identity = %#v", capabilities)
	}
	if capabilities.StructuredJSONSchema.Status != CapabilityUnsupported || capabilities.Tools.Status != CapabilitySupported {
		t.Fatalf("capabilities = %#v", capabilities)
	}
	if capabilities.MaxTokensParameter != "max_tokens" {
		t.Fatalf("max token parameter = %q", capabilities.MaxTokensParameter)
	}
}

func TestPlanParamsRejectsKnownUnsupportedStructuredSchema(t *testing.T) {
	_, err := PlanParams(Params{
		Provider: "deepseek", BaseURL: "https://api.deepseek.com", Model: "deepseek-chat",
		Protocol:       ProtocolChatCompletions,
		ResponseFormat: &ResponseFormat{Type: "json_schema", JSONSchema: &JSONSchema{Name: "x", Schema: map[string]any{"type": "object"}, Strict: true}},
	}, RequestKindStructured)
	if err == nil || !errors.Is(err, ErrCapabilityUnsupported) {
		t.Fatalf("err=%v, want capability rejection", err)
	}
}
