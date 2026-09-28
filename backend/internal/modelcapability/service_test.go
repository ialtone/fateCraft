package modelcapability

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/llm"
	"github.com/ialtone/fatecraft/internal/storage"
)

func TestAgentDiagnosticUsesProductionRequestShape(t *testing.T) {
	for _, protocol := range []llm.Protocol{llm.ProtocolChatCompletions, llm.ProtocolResponses} {
		t.Run(string(protocol), func(t *testing.T) {
			settings := testSettings("", protocol)
			settings.MaxResponseTokens = 2345
			step := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				step++
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if _, exists := body["tool_choice"]; exists {
					t.Fatalf("diagnostic must use provider default tool selection: %#v", body["tool_choice"])
				}
				maxTokensKey := "max_tokens"
				if protocol == llm.ProtocolResponses {
					maxTokensKey = "max_output_tokens"
				}
				if body[maxTokensKey] != float64(settings.MaxResponseTokens) {
					t.Fatalf("%s = %#v, want %d", maxTokensKey, body[maxTokensKey], settings.MaxResponseTokens)
				}
				// 测试端点是本地地址，按兼容网关处理：可选工具控制字段一律不发。
				if _, exists := body["parallel_tool_calls"]; exists {
					t.Fatalf("parallel_tool_calls = %#v, want absent", body["parallel_tool_calls"])
				}
				if step == 1 {
					nonce := diagnosticToolNonce(t, body, protocol)
					writeDiagnosticToolCall(w, protocol, nonce)
					return
				}
				writeDiagnosticText(w, protocol)
			}))
			defer server.Close()

			settings.BaseURL = server.URL
			result := New(llm.NewClient(), "").Check(t.Context(), uuid.New(), settings, RoleNarrative)
			if result.Status != "supported" || result.Feature != "agent_tools" || step != 2 {
				t.Fatalf("result=%+v steps=%d", result, step)
			}
		})
	}
}

func TestAgentDiagnosticBehaviorMismatchIsInconclusive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"I can help without a tool."},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()

	result := New(llm.NewClient(), "").Check(t.Context(), uuid.New(), testSettings(server.URL, llm.ProtocolChatCompletions), RoleNarrative)
	if result.Status != "inconclusive" || result.ErrorCode != "diagnostic_inconclusive" {
		t.Fatalf("result=%+v", result)
	}
}

func TestJSONSchemaDiagnosticUsesNativeSchema(t *testing.T) {
	for _, protocol := range []llm.Protocol{llm.ProtocolChatCompletions, llm.ProtocolResponses} {
		t.Run(string(protocol), func(t *testing.T) {
			settings := testSettings("", protocol)
			settings.MaxResponseTokens = 2345
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				maxTokensKey := "max_tokens"
				if protocol == llm.ProtocolResponses {
					maxTokensKey = "max_output_tokens"
				}
				if body[maxTokensKey] != float64(settings.MaxResponseTokens) {
					t.Fatalf("%s = %#v, want %d", maxTokensKey, body[maxTokensKey], settings.MaxResponseTokens)
				}
				nonce := diagnosticSchemaNonce(t, body, protocol)
				payload, _ := json.Marshal(map[string]string{"nonce": nonce})
				if protocol == llm.ProtocolResponses {
					fmt.Fprintf(w, `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":%q}]}]}`, payload)
					return
				}
				fmt.Fprintf(w, `{"choices":[{"message":{"content":%q},"finish_reason":"stop"}]}`, payload)
			}))
			defer server.Close()

			settings.BaseURL = server.URL
			result := New(llm.NewClient(), "").Check(t.Context(), uuid.New(), settings, RoleMemory)
			if result.Status != "supported" || result.Feature != "json_schema" {
				t.Fatalf("result=%+v", result)
			}
		})
	}
}

func TestDiagnosticOnlyMarksExplicitFeatureRejectionUnsupported(t *testing.T) {
	tests := []struct {
		name    string
		feature string
		body    string
		want    string
	}{
		{
			name: "function calling rejected", feature: "agent_tools", want: "unsupported",
			body: `{"error":{"code":"unsupported_feature","message":"Function calling is not supported for this model.","param":"tools"}}`,
		},
		{
			name: "tool choice rejected", feature: "agent_tools", want: "inconclusive",
			body: `{"error":{"code":"invalid_request_error","message":"Thinking mode does not support this tool_choice","param":"tool_choice"}}`,
		},
		{
			name: "json schema rejected", feature: "json_schema", want: "unsupported",
			body: `{"error":{"code":"responses_feature_not_supported","message":"text.format type json_schema is not supported","param":"text.format"}}`,
		},
		{
			name: "unrelated bad request", feature: "agent_tools", want: "inconclusive",
			body: `{"error":{"code":"invalid_request_error","message":"temperature is not supported"}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, status := classifyDiagnosticError(&llm.HTTPError{StatusCode: http.StatusBadRequest, Body: test.body}, test.feature)
			if status != test.want {
				t.Fatalf("status=%s want=%s", status, test.want)
			}
		})
	}
}

func TestEffortForRoleInheritsNarrativeSetting(t *testing.T) {
	settings := &storage.LLMSettings{
		ReasoningEffort:        "high",
		MemoryReasoningEffort:  "",
		InquiryReasoningEffort: "low",
	}

	if got := effortForRole(settings, RoleNarrative); got != "high" {
		t.Fatalf("narrative effort = %q", got)
	}
	if got := effortForRole(settings, RoleMemory); got != "high" {
		t.Fatalf("memory effort = %q", got)
	}
	if got := effortForRole(settings, RoleInquiry); got != "low" {
		t.Fatalf("inquiry effort = %q", got)
	}
	settings.ReasoningEffort = ""
	if got := effortForRole(settings, RoleMemory); got != "" {
		t.Fatalf("memory effort without narrative fallback = %q", got)
	}
}

func TestDiagnosticParamsDisableTransportRetries(t *testing.T) {
	params := New(llm.NewClient(), "").params(uuid.New(), testSettings("http://example.invalid", llm.ProtocolResponses), "model", RoleNarrative)
	if params.RetryPolicy == nil || params.RetryPolicy.MaxAttempts != 1 {
		t.Fatalf("diagnostic retry policy = %#v, want one attempt", params.RetryPolicy)
	}
}

// This opt-in test exercises the exact capability service against a real
// OpenAI-compatible endpoint without ever storing or printing its API key.
func TestLiveEndpointCapabilities(t *testing.T) {
	baseURL := strings.TrimSpace(os.Getenv("FATECRAFT_LIVE_LLM_BASE_URL"))
	apiKey := strings.TrimSpace(os.Getenv("FATECRAFT_LIVE_LLM_API_KEY"))
	model := strings.TrimSpace(os.Getenv("FATECRAFT_LIVE_LLM_MODEL"))
	if baseURL == "" || apiKey == "" || model == "" {
		t.Skip("live LLM environment is not configured")
	}
	protocol := llm.NormalizeProtocol(os.Getenv("FATECRAFT_LIVE_LLM_PROTOCOL"))
	settings := testSettings(baseURL, protocol)
	settings.APIKey = apiKey
	settings.Model = model
	settings.MemoryModel = model
	settings.StreamEnabled = true
	service := New(llm.NewClient(), "")
	for _, role := range []Role{RoleNarrative, RoleMemory} {
		result := service.Check(t.Context(), uuid.New(), settings, role)
		if result.Status != "supported" {
			t.Fatalf("role=%s result=%+v", role, result)
		}
	}
}

func testSettings(baseURL string, protocol llm.Protocol) *storage.LLMSettings {
	return &storage.LLMSettings{
		BaseURL: baseURL, APIKey: "test-key", Model: "test-model", Protocol: string(protocol),
		MaxResponseTokens: 1024, TopP: 1,
	}
}

func diagnosticToolNonce(t *testing.T, body map[string]any, protocol llm.Protocol) string {
	t.Helper()
	tool := body["tools"].([]any)[0].(map[string]any)
	if protocol == llm.ProtocolChatCompletions {
		tool = tool["function"].(map[string]any)
	}
	parameters := tool["parameters"].(map[string]any)
	properties := parameters["properties"].(map[string]any)
	nonce := properties["nonce"].(map[string]any)["const"].(string)
	return nonce
}

func diagnosticSchemaNonce(t *testing.T, body map[string]any, protocol llm.Protocol) string {
	t.Helper()
	var format map[string]any
	if protocol == llm.ProtocolResponses {
		format = body["text"].(map[string]any)["format"].(map[string]any)
	} else {
		format = body["response_format"].(map[string]any)["json_schema"].(map[string]any)
	}
	if format["type"] != nil && format["type"] != "json_schema" {
		t.Fatalf("format type=%#v", format["type"])
	}
	properties := format["schema"].(map[string]any)["properties"].(map[string]any)
	return properties["nonce"].(map[string]any)["const"].(string)
}

func writeDiagnosticToolCall(w http.ResponseWriter, protocol llm.Protocol, nonce string) {
	arguments, _ := json.Marshal(map[string]string{"nonce": nonce})
	if protocol == llm.ProtocolResponses {
		fmt.Fprintf(w, `{"status":"completed","output":[{"type":"function_call","id":"item-1","call_id":"call-1","name":"fc_diagnostic_echo","arguments":%q}]}`, arguments)
		return
	}
	fmt.Fprintf(w, `{"choices":[{"message":{"tool_calls":[{"id":"call-1","type":"function","function":{"name":"fc_diagnostic_echo","arguments":%q}}]},"finish_reason":"tool_calls"}]}`, arguments)
}

func writeDiagnosticText(w http.ResponseWriter, protocol llm.Protocol) {
	if protocol == llm.ProtocolResponses {
		fmt.Fprint(w, `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"TOOL_LOOP_OK"}]}]}`)
		return
	}
	fmt.Fprint(w, `{"choices":[{"message":{"content":"TOOL_LOOP_OK"},"finish_reason":"stop"}]}`)
}
