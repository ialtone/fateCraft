// Package llm 是 OpenAI 兼容 Chat Completions/Responses 的 HTTP 客户端，
// 负责将普通文本、工具调用与 JSON Schema 输出归一化为协议无关事件。
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Protocol identifies the wire protocol used by an OpenAI-compatible provider.
// Responses is the default; Chat Completions remains available explicitly.
type Protocol string

const (
	ProtocolChatCompletions Protocol = "chat_completions"
	ProtocolResponses       Protocol = "responses"
)

// NormalizeProtocol treats an omitted or unknown value as Responses.
// API validation rejects unknown values before they are persisted; omitted
// values use the platform default.
// NormalizeProtocol 把用户输入归一为受支持协议，留空按 Responses 处理。
func NormalizeProtocol(value string) Protocol {
	if Protocol(strings.TrimSpace(value)) == ProtocolChatCompletions {
		return ProtocolChatCompletions
	}
	return ProtocolResponses
}

func IsValidProtocol(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed == "" || trimmed == string(ProtocolChatCompletions) || trimmed == string(ProtocolResponses)
}

// Message 是协议无关的模型消息。仅由具体 Adapter 投影为供应商 wire 格式。
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content,omitempty"`
	// Blocks is the canonical ordered representation.
	Blocks     []ContentBlock `json:"-"`
	ToolCalls  []ToolCall     `json:"-"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Name       string         `json:"name,omitempty"`
	// The following scalar fields are accepted only at the legacy construction
	// boundary; adapters never serialize Message directly.
	ReasoningContent string `json:"-"`
	// ReasoningItems contains raw Responses reasoning output items.
	ReasoningItems []json.RawMessage `json:"-"`
}

// CanonicalBlocks returns the ordered provider-neutral representation used by
// every adapter. Blocks is authoritative; the scalar fields are only accepted
// here as a narrow boundary projection for callers that construct messages
// directly (diagnostics and tests).
func (m Message) CanonicalBlocks() []ContentBlock {
	if len(m.Blocks) > 0 {
		return append([]ContentBlock(nil), m.Blocks...)
	}
	blocks := make([]ContentBlock, 0, 2+len(m.ToolCalls)+len(m.ReasoningItems))
	for _, raw := range m.ReasoningItems {
		if len(raw) > 0 && json.Valid(raw) {
			blocks = append(blocks, ContentBlock{Kind: ContentThinking, Replay: append(json.RawMessage(nil), raw...)})
		}
	}
	if m.ReasoningContent != "" {
		blocks = append(blocks, ContentBlock{Kind: ContentThinking, Text: m.ReasoningContent})
	}
	if m.Content != "" {
		blocks = append(blocks, ContentBlock{Kind: ContentText, Text: m.Content})
	}
	for index := range m.ToolCalls {
		call := m.ToolCalls[index]
		blocks = append(blocks, ContentBlock{Kind: ContentToolCall, ToolCall: &call})
	}
	return blocks
}

func (m Message) TextProjection() string {
	if len(m.Blocks) == 0 {
		return m.Content
	}
	var out strings.Builder
	for _, block := range m.Blocks {
		if block.Kind == ContentText || block.Kind == ContentRefusal {
			out.WriteString(block.Text)
		}
	}
	return out.String()
}

func (m Message) ToolCallBlocks() []ToolCall {
	blocks := m.CanonicalBlocks()
	out := make([]ToolCall, 0)
	for _, block := range blocks {
		if block.Kind == ContentToolCall && block.ToolCall != nil {
			out = append(out, *block.ToolCall)
		}
	}
	return out
}

// ContentKind is the provider-neutral ordered output block type.
type ContentKind string

const (
	ContentText     ContentKind = "text"
	ContentThinking ContentKind = "thinking"
	ContentToolCall ContentKind = "tool_call"
	ContentRefusal  ContentKind = "refusal"
)

// ContentBlock preserves the order in which a provider emitted thinking,
// text, refusal and tool-call blocks. Replay is opaque provider-owned data;
// only the selected protocol adapter may interpret it.
type ContentBlock struct {
	Kind     ContentKind     `json:"kind"`
	Text     string          `json:"text,omitempty"`
	ToolCall *ToolCall       `json:"tool_call,omitempty"`
	Replay   json.RawMessage `json:"replay,omitempty"`
}

// ToolCall and ToolDefinition are provider-neutral function-calling types.
// ItemID is populated only for Responses function calls so the original
// provider output item can be replayed on the next tool-loop request. Chat
// Completions leaves it empty; domain packages never consume it.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	ItemID    string `json:"item_id,omitempty"`
}

type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
	Strict      bool           `json:"strict"`
}

type AgentEventKind string

const (
	AgentEventReasoningDelta    AgentEventKind = "reasoning_delta"
	AgentEventTextDelta         AgentEventKind = "text_delta"
	AgentEventToolCallStarted   AgentEventKind = "tool_call_started"
	AgentEventToolArguments     AgentEventKind = "tool_arguments_delta"
	AgentEventToolCallCompleted AgentEventKind = "tool_call_completed"
	AgentEventRefusal           AgentEventKind = "refusal"
	AgentEventStepCompleted     AgentEventKind = "step_completed"
)

// AgentEvent is the normalized transport event emitted by both supported
// OpenAI-compatible protocols. It carries no application or database state.
type AgentEvent struct {
	Kind         AgentEventKind
	Text         string
	CallID       string
	Name         string
	Arguments    string
	FinishReason string
}

type AgentStepResult struct {
	// Content is the canonical ordered result. The legacy scalar fields remain
	// as projections for callers being migrated and must not be used to infer
	// output order.
	Content          []ContentBlock
	Text             string
	Refusal          string
	ToolCalls        []ToolCall
	FinishReason     string
	ReasoningContent string
	ReasoningItems   []json.RawMessage
	Usage            TokenUsage
}

// TokenUsage is the provider-reported usage for one model request. A zero
// value means the provider omitted usage and callers should use their local
// tokenizer estimate.
type TokenUsage struct {
	InputTokens  int
	OutputTokens int
}

// CanonicalBlocks returns the normalized ordered result. Scalar fields are only
// a construction boundary for older callers; runtime code should consume this
// projection and never branch on provider-specific fields.
func (r AgentStepResult) CanonicalBlocks() []ContentBlock {
	if len(r.Content) > 0 {
		return append([]ContentBlock(nil), r.Content...)
	}
	return projectAgentContent(r)
}

// ToolCallBlocks and TextProjection are the only output queries the Agent
// Runtime needs.
func (r AgentStepResult) ToolCallBlocks() []ToolCall {
	blocks := r.CanonicalBlocks()
	out := make([]ToolCall, 0)
	for _, block := range blocks {
		if block.Kind == ContentToolCall && block.ToolCall != nil {
			out = append(out, *block.ToolCall)
		}
	}
	return out
}

func (r AgentStepResult) TextProjection() string {
	blocks := r.CanonicalBlocks()
	var out strings.Builder
	for _, block := range blocks {
		if block.Kind == ContentText || block.Kind == ContentRefusal {
			out.WriteString(block.Text)
		}
	}
	return out.String()
}

type AgentEventHandler func(AgentEvent)

// Params 是一次请求的模型参数。
type Params struct {
	BaseURL     string
	Provider    string
	APIKey      string
	Model       string
	Protocol    Protocol
	Temperature float64
	TopP        float64
	MaxTokens   int
	// ReasoningEffort 为空时不发送思考强度字段，沿用供应商默认；非空时原样
	// 透传（各家取值不一，如 minimal/low/medium/high/ultra，不做枚举映射）。
	ReasoningEffort            string
	ResponseFormat             *ResponseFormat // 通用非 Agent 调用的原生格式参数
	StructuredOutput           *StructuredOutput
	SettingsRevision           int64
	SettingsSource             string
	ContractName               string
	ContractVersion            string
	ContractFingerprint        string
	ConversationID             string // 可选，仅用于详细请求日志关联
	MessageID                  string // 可选，仅用于详细请求日志关联
	Purpose                    string
	AttemptID                  string
	MemoryJobID                string
	MemoryID                   string
	InquiryID                  string
	InquirySnapshotID          string
	InquirySnapshotBranchEpoch int64
	InquirySnapshotRevision    int64
	// RetryPolicy controls safe transport retries before a response body is
	// delivered. Nil uses the client default; MaxAttempts=1 disables retries.
	RetryPolicy *RetryPolicy `json:"-"`
}

// RetryPolicy only applies while establishing a model HTTP response. Once a
// successful stream has been returned, parsers never replay the Agent step.
type RetryPolicy struct {
	MaxAttempts   int
	BaseDelay     time.Duration
	MaxDelay      time.Duration
	DisableJitter bool
}

// RequestAudit 是一次 LLM HTTP 调用的诊断快照。RequestBody 不包含 API Key
// 或 Authorization 头；RawResponse 保留上游原始响应，便于分析流式截断和协议漂移。
type RequestAudit struct {
	ConversationID             string
	MessageID                  string
	Purpose                    string
	AttemptID                  string
	MemoryJobID                string
	MemoryID                   string
	InquiryID                  string
	InquirySnapshotID          string
	InquirySnapshotBranchEpoch int64
	InquirySnapshotRevision    int64
	Model                      string
	BaseURL                    string
	Stream                     bool
	RequestBody                []byte
	RawResponse                string
	ResponseText               string
	AuditTruncated             bool
	ResponseStatus             int
	FinishReason               string
	ReceivedDone               bool
	Success                    bool
	Error                      string
	DurationMS                 int64
	SettingsRevision           int64
	SettingsSource             string
	OutputMode                 OutputMode
	ContractName               string
	ContractVersion            string
	ContractFingerprint        string
	TransportSuccess           bool
	StructuredSuccess          bool
	StructuredErrorCode        string
	OutputTypes                []string
}

// RequestAuditWriter 持久化详细请求日志。实现方必须自行决定存储位置和权限。
type RequestAuditWriter func(context.Context, RequestAudit) error

// ResponseFormat 是 OpenAI 兼容接口的原生格式参数。需要结构化交付的调用通过
// StructuredOutput 使用原生 JSON Schema，不能直接使用该字段降低契约要求。
type ResponseFormat struct {
	Type       string      `json:"type"`
	JSONSchema *JSONSchema `json:"json_schema,omitempty"`
}

// JSONSchema 定义 JSON Schema 约束（OpenAI Structured Outputs）。
type JSONSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Schema      map[string]any `json:"schema"`
	Strict      bool           `json:"strict"`
}

// OutputMode is a provider-enforced structured delivery mode. JSON object and
// prompt-only JSON are intentionally not represented here.
type OutputMode string

const (
	OutputModeNativeJSONSchema OutputMode = "native_json_schema"
)

type StructuredOutput struct {
	Mode   OutputMode
	Schema *JSONSchema
	// PayloadValidator runs after transport delivery and before the audit is
	// finalized. It lets each Agent apply its exact local contract without
	// teaching the HTTP client domain semantics.
	PayloadValidator func(string) error `json:"-"`
}

func (o *StructuredOutput) Validate() error {
	if o == nil || o.Schema == nil {
		return fmt.Errorf("严格输出契约不能为空")
	}
	if !o.Schema.Strict {
		return fmt.Errorf("严格输出契约必须启用 strict")
	}
	if o.Mode != OutputModeNativeJSONSchema {
		return fmt.Errorf("不支持的严格输出模式 %q", o.Mode)
	}
	return nil
}

type StructuredResult struct {
	Payload     string
	Mode        OutputMode
	OutputTypes []string
	ToolCalls   int
	// receivedDone is populated only by streaming parsers so audit records
	// can distinguish a valid terminal event from a parser/contract failure.
	receivedDone bool
}

type StructuredError struct {
	Code string
	Err  error
}

func (e *StructuredError) Error() string {
	if e == nil || e.Err == nil {
		return e.Code
	}
	return e.Err.Error()
}

func (e *StructuredError) Unwrap() error { return e.Err }

func NewStructuredError(code, message string) error {
	return &StructuredError{Code: code, Err: fmt.Errorf("%s", message)}
}

func StructuredErrorCode(err error) string {
	var target *StructuredError
	if errors.As(err, &target) {
		return target.Code
	}
	return ""
}
