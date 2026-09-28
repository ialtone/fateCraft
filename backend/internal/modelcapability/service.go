package modelcapability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/llm"
	"github.com/ialtone/fatecraft/internal/storage"
)

type Role string

const (
	RoleNarrative Role = "narrative"
	RoleMemory    Role = "memory"
	RoleInquiry   Role = "character_inquiry"
)

type DiagnosticResult struct {
	Role      Role      `json:"role"`
	Feature   string    `json:"feature"`
	Status    string    `json:"status"`
	Protocol  string    `json:"protocol"`
	Model     string    `json:"model"`
	Stream    bool      `json:"stream"`
	ErrorCode string    `json:"error_code,omitempty"`
	Summary   string    `json:"summary,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

type Service struct {
	client *llm.Client
	// relayFor 返回该玩家的 Relay 传输；client 路由的诊断经它执行。
	relayFor func(playerID string) llm.RelayTransport
}

var errDiagnosticInconclusive = errors.New("模型诊断结果不确定")

// New keeps an optional legacy argument for callers compiled against the old
// constructor; capability checks never use an environment-key fallback.
func New(client *llm.Client, _ ...string) *Service {
	return &Service{client: client}
}

// SetRelayTransport 注入 Relay 传输构造。
func (s *Service) SetRelayTransport(forTransport func(playerID string) llm.RelayTransport) {
	s.relayFor = forTransport
}

// Check performs one explicit, side-effect-free diagnostic. It never retries,
// persists health scores or changes production request behavior.
func (s *Service) Check(ctx context.Context, playerID uuid.UUID, settings *storage.LLMSettings, role Role) DiagnosticResult {
	if s == nil || s.client == nil || settings == nil {
		return DiagnosticResult{
			Role: role, Status: "unavailable", ErrorCode: "diagnostic_unavailable",
			Summary: "模型诊断服务未配置", CheckedAt: time.Now(),
		}
	}
	result := DiagnosticResult{
		Role: role, Protocol: string(llm.NormalizeProtocol(settings.Protocol)),
		Model: modelForRole(settings, role), Stream: role == RoleNarrative && settings.StreamEnabled,
		Status: "unavailable", CheckedAt: time.Now(),
	}
	var err error
	if role == RoleNarrative {
		result.Feature = "agent_tools"
		err = s.checkAgentTools(ctx, playerID, settings, result.Model)
	} else {
		result.Feature = "json_schema"
		err = s.checkJSONSchema(ctx, playerID, settings, result.Model, role)
	}
	if err == nil {
		result.Status = "supported"
		return result
	}
	result.ErrorCode, result.Status = classifyDiagnosticError(err, result.Feature)
	result.Summary = bounded(err.Error())
	return result
}

func (s *Service) checkAgentTools(ctx context.Context, playerID uuid.UUID, settings *storage.LLMSettings, model string) error {
	nonce := uuid.NewString()
	tool := llm.ToolDefinition{
		Name: "fc_diagnostic_echo", Description: "Echo a diagnostic nonce without side effects.",
		Parameters: map[string]any{
			"type": "object", "additionalProperties": false, "required": []string{"nonce"},
			"properties": map[string]any{"nonce": map[string]any{"type": "string", "const": nonce}},
		},
	}
	params := s.params(playerID, settings, model, RoleNarrative)
	params.MaxTokens = settings.MaxResponseTokens
	first, err := s.client.AgentStep(ctx, params, []llm.Message{
		{Role: storage.RoleSystem, Content: "Call the supplied diagnostic tool exactly once. Do not answer with text."},
		{Role: storage.RoleUser, Content: "Echo diagnostic nonce " + nonce + "."},
	}, []llm.ToolDefinition{tool}, settings.StreamEnabled, nil)
	if err != nil {
		return err
	}
	if strings.TrimSpace(first.TextProjection()) != "" || len(first.ToolCallBlocks()) != 1 {
		return fmt.Errorf("%w: 模型未按诊断指令返回唯一工具调用", errDiagnosticInconclusive)
	}
	call := first.ToolCallBlocks()[0]
	if call.Name != tool.Name || strings.TrimSpace(call.ID) == "" {
		return fmt.Errorf("%w: 模型返回了错误的诊断工具或缺少 call_id", errDiagnosticInconclusive)
	}
	var arguments struct {
		Nonce string `json:"nonce"`
	}
	if err := json.Unmarshal([]byte(call.Arguments), &arguments); err != nil || arguments.Nonce != nonce {
		return fmt.Errorf("%w: 诊断工具参数未保留随机 nonce", errDiagnosticInconclusive)
	}
	second, err := s.client.AgentStep(ctx, params, []llm.Message{
		{Role: storage.RoleSystem, Content: "After receiving the tool result, reply with exactly TOOL_LOOP_OK as plain text and do not call another tool."},
		{Role: storage.RoleUser, Content: "Run the diagnostic tool loop."},
		{Role: storage.RoleAssistant, ToolCalls: []llm.ToolCall{call}},
		{Role: storage.RoleTool, ToolCallID: call.ID, Name: call.Name, Content: `{"ok":true,"nonce":"` + nonce + `"}`},
	}, []llm.ToolDefinition{tool}, settings.StreamEnabled, nil)
	if err != nil {
		return err
	}
	if len(second.ToolCallBlocks()) != 0 || strings.TrimSpace(second.TextProjection()) == "" {
		return fmt.Errorf("%w: 模型未在工具结果后返回普通文本", errDiagnosticInconclusive)
	}
	return nil
}

func (s *Service) checkJSONSchema(ctx context.Context, playerID uuid.UUID, settings *storage.LLMSettings, model string, role Role) error {
	nonce := uuid.NewString()
	schema := &llm.JSONSchema{Name: "fatecraft_json_schema_diagnostic", Strict: true, Schema: map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"nonce"},
		"properties": map[string]any{"nonce": map[string]any{"type": "string", "const": nonce}},
	}}
	params := s.params(playerID, settings, model, role)
	params.MaxTokens = settings.MaxResponseTokens
	params.ContractName = schema.Name
	params.ContractVersion = "1"
	params.ContractFingerprint = llm.SchemaFingerprint(schema)
	params.StructuredOutput = &llm.StructuredOutput{
		Mode: llm.OutputModeNativeJSONSchema, Schema: schema,
		PayloadValidator: func(raw string) error { return validateNonce(raw, nonce) },
	}
	_, err := s.client.CompleteStructured(ctx, params, []llm.Message{
		{Role: storage.RoleSystem, Content: "Return the exact object required by the JSON Schema."},
		{Role: storage.RoleUser, Content: fmt.Sprintf("%s diagnostic nonce: %s", role, nonce)},
	})
	return err
}

func (s *Service) params(playerID uuid.UUID, settings *storage.LLMSettings, model string, role Role) llm.Params {
	apiKey := settings.APIKey
	return llm.Params{
		BaseURL: settings.BaseURL, APIKey: apiKey, Model: model,
		Protocol: llm.NormalizeProtocol(settings.Protocol), Temperature: 0, TopP: 1,
		ReasoningEffort: effortForRole(settings, role),
		Relay:           s.relayForRoute(playerID, settings),
		// A diagnostic is an explicit one-shot probe. The production transport
		// retry policy must not turn a button click into multiple provider calls.
		RetryPolicy: &llm.RetryPolicy{MaxAttempts: 1},
		Purpose:     "protocol_test", SettingsRevision: settings.SettingsRevision, SettingsSource: settings.Source,
	}
}

// relayForRoute 返回 client 路由设置对应的 Relay 传输，否则返回 nil。
func (s *Service) relayForRoute(playerID uuid.UUID, settings *storage.LLMSettings) llm.RelayTransport {
	if settings.LLMRoute != storage.LLMRouteClient || s.relayFor == nil {
		return nil
	}
	return s.relayFor(playerID.String())
}

// effortForRole 返回角色实际使用的思考强度；记忆和问答留空时继承叙事设置。
func effortForRole(settings *storage.LLMSettings, role Role) string {
	if settings == nil {
		return ""
	}
	switch role {
	case RoleMemory:
		if settings.MemoryReasoningEffort != "" {
			return settings.MemoryReasoningEffort
		}
	case RoleInquiry:
		if settings.InquiryReasoningEffort != "" {
			return settings.InquiryReasoningEffort
		}
	}
	return settings.ReasoningEffort
}

func modelForRole(settings *storage.LLMSettings, role Role) string {
	if settings == nil {
		return ""
	}
	switch role {
	case RoleMemory:
		if strings.TrimSpace(settings.MemoryModel) != "" {
			return settings.MemoryModel
		}
	case RoleInquiry:
		if strings.TrimSpace(settings.InquiryModel) != "" {
			return settings.InquiryModel
		}
	}
	return settings.Model
}

func validateNonce(raw, nonce string) error {
	var value struct {
		Nonce string `json:"nonce"`
	}
	if err := json.Unmarshal([]byte(raw), &value); err != nil || value.Nonce != nonce {
		return errors.New("结构化诊断未返回预期 nonce")
	}
	return nil
}

func classifyDiagnosticError(err error, feature string) (string, string) {
	if errors.Is(err, errDiagnosticInconclusive) {
		return "diagnostic_inconclusive", "inconclusive"
	}
	var httpErr *llm.HTTPError
	if errors.As(err, &httpErr) {
		if (httpErr.StatusCode == http.StatusBadRequest || httpErr.StatusCode == http.StatusUnprocessableEntity) &&
			explicitlyRejectsFeature(httpErr.Body, feature) {
			return "feature_unsupported", "unsupported"
		}
		if httpErr.StatusCode == http.StatusBadRequest || httpErr.StatusCode == http.StatusUnprocessableEntity {
			return "diagnostic_inconclusive", "inconclusive"
		}
		return "provider_unavailable", "unavailable"
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "request_timeout", "unavailable"
	}
	return "diagnostic_inconclusive", "inconclusive"
}

func explicitlyRejectsFeature(body, feature string) bool {
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Param   string `json:"param"`
		} `json:"error"`
	}
	_ = json.Unmarshal([]byte(body), &envelope)
	details := strings.ToLower(strings.Join([]string{
		envelope.Error.Code, envelope.Error.Message, envelope.Error.Param, body,
	}, " "))
	if !containsAny(details,
		"not supported", "isnot supported", "unsupported", "not available", "unavailable",
	) {
		return false
	}
	switch feature {
	case "agent_tools":
		// Rejecting a control parameter does not prove that function calling is
		// unavailable. Fatecraft deliberately omits these optional controls.
		if strings.Contains(details, "tool_choice") || strings.Contains(details, "parallel_tool_calls") {
			return false
		}
		return containsAny(details, "function calling", "function_call", "tool calling", "tool calls", "tools")
	case "json_schema":
		return containsAny(details, "json_schema", "json schema", "response_format", "text.format")
	default:
		return false
	}
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

func bounded(value string) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > 512 {
		return string(runes[:512])
	}
	return value
}
