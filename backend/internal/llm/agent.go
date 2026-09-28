package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ialtone/fatecraft/internal/logging"
)

const maxAgentToolArguments = 16 * 1024

type chatAgentFunction struct {
	Name        string         `json:"name"`
	Arguments   string         `json:"arguments,omitempty"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	Strict      bool           `json:"strict,omitempty"`
}

type chatAgentToolCall struct {
	Index    int               `json:"index,omitempty"`
	ID       string            `json:"id,omitempty"`
	Type     string            `json:"type,omitempty"`
	Function chatAgentFunction `json:"function"`
}

type chatAgentMessage struct {
	Role             string              `json:"role"`
	Content          string              `json:"content,omitempty"`
	ReasoningContent string              `json:"reasoning_content,omitempty"`
	Refusal          string              `json:"refusal,omitempty"`
	ToolCalls        []chatAgentToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string              `json:"tool_call_id,omitempty"`
	Name             string              `json:"name,omitempty"`
}

type chatAgentTool struct {
	Type     string            `json:"type"`
	Function chatAgentFunction `json:"function"`
}

type chatAgentRequest struct {
	Model             string             `json:"model"`
	Messages          []chatAgentMessage `json:"messages"`
	Temperature       float64            `json:"temperature"`
	TopP              float64            `json:"top_p"`
	MaxTokens         int                `json:"max_tokens,omitempty"`
	Stream            bool               `json:"stream"`
	ReasoningEffort   string             `json:"reasoning_effort,omitempty"`
	Tools             []chatAgentTool    `json:"tools,omitempty"`
	ParallelToolCalls *bool              `json:"parallel_tool_calls,omitempty"`
	StreamOptions     *chatStreamOptions `json:"stream_options,omitempty"`
}

type chatStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatAgentResponse struct {
	Choices []struct {
		Message      chatAgentMessage `json:"message"`
		FinishReason string           `json:"finish_reason"`
	} `json:"choices"`
	Usage agentTokenUsage `json:"usage"`
}

type chatAgentChunk struct {
	Choices []struct {
		Delta        chatAgentMessage `json:"delta"`
		FinishReason *string          `json:"finish_reason"`
	} `json:"choices"`
	Usage agentTokenUsage `json:"usage"`
}

type agentTokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	InputTokens      int `json:"input_tokens"`
	OutputTokens     int `json:"output_tokens"`
}

func (u agentTokenUsage) normalized() TokenUsage {
	input := u.InputTokens
	if input == 0 {
		input = u.PromptTokens
	}
	output := u.OutputTokens
	if output == 0 {
		output = u.CompletionTokens
	}
	return TokenUsage{InputTokens: input, OutputTokens: output}
}

type responsesAgentRequest struct {
	Model             string                `json:"model"`
	Input             []responsesAgentInput `json:"input"`
	Temperature       float64               `json:"temperature"`
	TopP              float64               `json:"top_p"`
	MaxOutputTokens   int                   `json:"max_output_tokens,omitempty"`
	Stream            bool                  `json:"stream"`
	Reasoning         *responsesReasoning   `json:"reasoning,omitempty"`
	Tools             []responsesAgentTool  `json:"tools,omitempty"`
	ParallelToolCalls *bool                 `json:"parallel_tool_calls,omitempty"`
}

type responsesAgentInput struct {
	ID        string                  `json:"id,omitempty"`
	Type      string                  `json:"type,omitempty"`
	Role      string                  `json:"role,omitempty"`
	Content   []responsesAgentContent `json:"content,omitempty"`
	CallID    string                  `json:"call_id,omitempty"`
	Name      string                  `json:"name,omitempty"`
	Arguments string                  `json:"arguments,omitempty"`
	Output    string                  `json:"output,omitempty"`
	Raw       json.RawMessage         `json:"-"`
}

// MarshalJSON passes provider-owned Responses output items through unchanged.
// Reasoning items are not representable as ordinary messages and DeepSeek
// requires their reasoning_text content to be returned on the next request.
func (item responsesAgentInput) MarshalJSON() ([]byte, error) {
	if len(item.Raw) > 0 {
		return item.Raw, nil
	}
	type wire responsesAgentInput
	return json.Marshal(wire(item))
}

type responsesAgentContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responsesAgentTool struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
	Strict      bool           `json:"strict,omitempty"`
}

type responsesAgentOutput struct {
	Type      string                   `json:"type"`
	ID        string                   `json:"id"`
	CallID    string                   `json:"call_id"`
	Name      string                   `json:"name"`
	Arguments string                   `json:"arguments"`
	Text      string                   `json:"text"`
	Content   []responsesOutputContent `json:"content"`
	Summary   []responsesOutputContent `json:"summary"`
	Raw       json.RawMessage          `json:"-"`
}

func (item *responsesAgentOutput) UnmarshalJSON(data []byte) error {
	type wire responsesAgentOutput
	var decoded wire
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*item = responsesAgentOutput(decoded)
	item.Raw = append(json.RawMessage(nil), data...)
	return nil
}

type responsesAgentResponse struct {
	Output            []responsesAgentOutput     `json:"output"`
	OutputText        json.RawMessage            `json:"output_text"`
	Status            string                     `json:"status"`
	IncompleteDetails *responsesIncompleteDetail `json:"incomplete_details"`
	Usage             agentTokenUsage            `json:"usage"`
}

type responsesAgentStreamEvent struct {
	Type              string                     `json:"type"`
	Delta             string                     `json:"delta"`
	Text              string                     `json:"text"`
	Arguments         string                     `json:"arguments"`
	CallID            string                     `json:"call_id"`
	Name              string                     `json:"name"`
	ItemID            string                     `json:"item_id"`
	OutputIndex       int                        `json:"output_index"`
	Item              *responsesAgentOutput      `json:"item"`
	Response          *responsesAgentResponse    `json:"response"`
	IncompleteDetails *responsesIncompleteDetail `json:"incomplete_details"`
	Error             *responsesEventError       `json:"error"`
}

type accumulatedAgentCall struct {
	index     int
	id        string
	itemID    string
	name      string
	arguments strings.Builder
	started   bool
	completed bool
}

// AgentStep performs exactly one logical model step. The transport may retry
// before a response body is delivered, but this method never changes the
// model or falls back to a different output protocol.
func (c *Client) AgentStep(
	ctx context.Context,
	p Params,
	messages []Message,
	tools []ToolDefinition,
	stream bool,
	onEvent AgentEventHandler,
) (AgentStepResult, error) {
	var err error
	p, err = PlanParams(p, RequestKindAgent)
	if err != nil {
		return AgentStepResult{}, err
	}
	if err := validateAgentTools(tools); err != nil {
		return AgentStepResult{}, err
	}
	replay, err := PrepareToolReplay(messages)
	if err != nil {
		return AgentStepResult{}, err
	}
	if len(replay.Repairs) > 0 {
		logToolReplayRepairs(p, replay.Repairs)
	}
	result, err := c.adapter(p).AgentStep(ctx, p, replay.Messages, tools, stream, onEvent)
	result.Content = result.CanonicalBlocks()
	return result, err
}

func logToolReplayRepairs(p Params, repairs []ReplayRepair) {
	if len(repairs) == 0 {
		return
	}
	kinds := make([]string, 0, len(repairs))
	for _, repair := range repairs {
		kinds = append(kinds, string(repair.Kind))
	}
	logging.Named("llm").Warn("模型工具调用回放已修复",
		zap.String("conversation_id", p.ConversationID),
		zap.String("attempt_id", p.AttemptID),
		zap.String("protocol", string(p.Protocol)),
		zap.String("model", p.Model),
		zap.Int("repair_count", len(repairs)),
		zap.Strings("repair_kinds", kinds),
	)
}

// projectAgentContent is the canonical output projection shared by both
// protocol adapters. Replay payloads remain opaque to callers.
func projectAgentContent(result AgentStepResult) []ContentBlock {
	content := make([]ContentBlock, 0, len(result.ToolCalls)+3)
	for _, raw := range result.ReasoningItems {
		if len(raw) == 0 || !json.Valid(raw) {
			continue
		}
		content = append(content, ContentBlock{Kind: ContentThinking, Replay: cloneJSON(raw)})
	}
	if result.ReasoningContent != "" && len(result.ReasoningItems) == 0 {
		content = append(content, ContentBlock{Kind: ContentThinking, Text: result.ReasoningContent})
	}
	if result.Text != "" {
		content = append(content, ContentBlock{Kind: ContentText, Text: result.Text})
	}
	if result.Refusal != "" {
		content = append(content, ContentBlock{Kind: ContentRefusal, Text: result.Refusal})
	}
	for i := range result.ToolCalls {
		call := result.ToolCalls[i]
		content = append(content, ContentBlock{Kind: ContentToolCall, ToolCall: &call})
	}
	return content
}

func validateAgentTools(tools []ToolDefinition) error {
	seen := make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		if strings.TrimSpace(tool.Name) == "" || tool.Name != strings.TrimSpace(tool.Name) || tool.Parameters == nil {
			return errors.New("Agent 工具定义无效")
		}
		if _, exists := seen[tool.Name]; exists {
			return fmt.Errorf("Agent 工具名重复: %s", tool.Name)
		}
		seen[tool.Name] = struct{}{}
	}
	return nil
}

func (c *Client) agentChat(ctx context.Context, p Params, messages []Message, tools []ToolDefinition, stream bool, onEvent AgentEventHandler) (result AgentStepResult, resultErr error) {
	body := chatAgentRequest{
		Model: p.Model, Messages: chatAgentMessages(messages), Temperature: p.Temperature,
		TopP: p.TopP, MaxTokens: p.MaxTokens, Stream: stream, ReasoningEffort: p.ReasoningEffort,
		Tools: chatAgentTools(tools, strictToolsAllowed(p)),
	}
	if stream && ResolveEndpointCapabilities(p.BaseURL, ProtocolChatCompletions).ChatStreamUsage {
		body.StreamOptions = &chatStreamOptions{IncludeUsage: true}
	}
	if len(tools) > 0 && parallelToolFlagAllowed(p) {
		parallel := false
		body.ParallelToolCalls = &parallel
	}
	requestBody, err := json.Marshal(body)
	if err != nil {
		return result, err
	}
	auditOn := c.shouldAudit(ctx)
	started := time.Now()
	resp, err := c.doRaw(ctx, p, "/chat/completions", requestBody, stream)
	if err != nil {
		c.writeAudit(ctx, auditOn, p, stream, requestBody, "", "", 0, "", false, false, err, started)
		return result, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := readAllBounded(resp.Body, maxHTTPErrorBytes)
		httpErr := &HTTPError{StatusCode: resp.StatusCode, Body: boundedHTTPErrorBody(raw)}
		c.writeAudit(ctx, auditOn, p, stream, requestBody, string(raw), "", resp.StatusCode, "", false, false, httpErr, started)
		return result, httpErr
	}
	var raw []byte
	var done bool
	if stream {
		var rawText string
		result, rawText, done, err = parseAgentChatStream(resp.Body, onEvent)
		raw = []byte(rawText)
	} else {
		raw, err = readAllBounded(resp.Body, maxLLMResponseBytes)
		if err == nil {
			result, err = parseAgentChatComplete(raw, onEvent)
		}
	}
	if err == nil && result.FinishReason == "length" {
		err = ErrTruncated
	}
	if err == nil && strings.TrimSpace(result.TextProjection()) == "" && len(result.ToolCallBlocks()) == 0 {
		err = errors.New("模型响应为空")
	}
	c.writeAudit(ctx, auditOn, p, stream, requestBody, string(raw), result.TextProjection(), resp.StatusCode, result.FinishReason, done, err == nil, err, started)
	return result, err
}

func chatAgentMessages(messages []Message) []chatAgentMessage {
	out := make([]chatAgentMessage, 0, len(messages))
	for _, message := range messages {
		item := chatAgentMessage{
			Role: message.Role, ToolCallID: message.ToolCallID, Name: message.Name,
		}
		for _, block := range message.CanonicalBlocks() {
			switch block.Kind {
			case ContentThinking:
				item.ReasoningContent += block.Text
			case ContentText, ContentRefusal:
				item.Content += block.Text
			case ContentToolCall:
				if block.ToolCall != nil {
					call := *block.ToolCall
					item.ToolCalls = append(item.ToolCalls, chatAgentToolCall{
						Index: len(item.ToolCalls), ID: call.ID, Type: "function",
						Function: chatAgentFunction{Name: call.Name, Arguments: call.Arguments},
					})
				}
			}
		}
		out = append(out, item)
	}
	return out
}

func chatAgentTools(tools []ToolDefinition, strict bool) []chatAgentTool {
	out := make([]chatAgentTool, 0, len(tools))
	for _, tool := range tools {
		out = append(out, chatAgentTool{Type: "function", Function: chatAgentFunction{
			Name: tool.Name, Description: tool.Description, Parameters: tool.Parameters, Strict: tool.Strict && strict,
		}})
	}
	return out
}

// parallelToolFlagAllowed / strictToolsAllowed:自定义兼容网关经常对未知字段
// 直接返回 400,因此这两个可选字段只发给当前协议明确支持的端点档位。
func parallelToolFlagAllowed(p Params) bool {
	return ResolveEndpointCapabilities(p.BaseURL, p.Protocol).ParallelToolFlag
}

func strictToolsAllowed(p Params) bool {
	return ResolveEndpointCapabilities(p.BaseURL, p.Protocol).StrictTools
}

func parseAgentChatComplete(raw []byte, onEvent AgentEventHandler) (AgentStepResult, error) {
	var response chatAgentResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return AgentStepResult{}, err
	}
	if len(response.Choices) == 0 {
		return AgentStepResult{}, errors.New("模型未返回任何 choices")
	}
	choice := response.Choices[0]
	result := AgentStepResult{
		Text: choice.Message.Content, Refusal: choice.Message.Refusal,
		ReasoningContent: choice.Message.ReasoningContent, FinishReason: choice.FinishReason,
		Usage: response.Usage.normalized(),
	}
	if choice.Message.ReasoningContent != "" {
		emitAgent(onEvent, AgentEvent{Kind: AgentEventReasoningDelta, Text: choice.Message.ReasoningContent})
	}
	if choice.Message.Content != "" {
		emitAgent(onEvent, AgentEvent{Kind: AgentEventTextDelta, Text: choice.Message.Content})
	}
	if choice.Message.Refusal != "" {
		emitAgent(onEvent, AgentEvent{Kind: AgentEventRefusal, Text: choice.Message.Refusal})
	}
	for _, call := range choice.Message.ToolCalls {
		if len(call.Function.Arguments) > maxAgentToolArguments {
			return result, errors.New("模型工具参数超过 16 KiB")
		}
		normalized := ToolCall{ID: call.ID, Name: call.Function.Name, Arguments: call.Function.Arguments}
		result.ToolCalls = append(result.ToolCalls, normalized)
		emitAgent(onEvent, AgentEvent{Kind: AgentEventToolCallStarted, CallID: normalized.ID, Name: normalized.Name})
		emitAgent(onEvent, AgentEvent{Kind: AgentEventToolCallCompleted, CallID: normalized.ID, Name: normalized.Name, Arguments: normalized.Arguments})
	}
	emitAgent(onEvent, AgentEvent{Kind: AgentEventStepCompleted, FinishReason: choice.FinishReason})
	return result, nil
}

func parseAgentChatStream(reader io.Reader, onEvent AgentEventHandler) (AgentStepResult, string, bool, error) {
	var result AgentStepResult
	raw := newAuditBuffer(maxLLMAuditBytes)
	var text strings.Builder
	calls := map[int]*accumulatedAgentCall{}
	done := false
	buf := bufio.NewReader(reader)
	for {
		line, readErr := readSSELine(buf)
		raw.Write([]byte(line))
		if raw.TooLarge() {
			return result, raw.String(), done, ErrLLMResponseTooLarge
		}
		if errors.Is(readErr, ErrLLMSSELineTooLarge) {
			return result, raw.String(), done, readErr
		}
		if data, ok := parseSSE(line); ok {
			if data == "[DONE]" {
				done = true
				break
			}
			var chunk chatAgentChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				return result, raw.String(), done, fmt.Errorf("流式 JSON 解析失败: %w", err)
			}
			if usage := chunk.Usage.normalized(); usage.InputTokens > 0 || usage.OutputTokens > 0 {
				result.Usage = usage
			}
			for _, choice := range chunk.Choices {
				if choice.Delta.ReasoningContent != "" {
					if len(result.ReasoningContent)+len(choice.Delta.ReasoningContent) > maxLLMResponseBytes {
						return result, raw.String(), done, ErrLLMResponseTooLarge
					}
					result.ReasoningContent += choice.Delta.ReasoningContent
					emitAgent(onEvent, AgentEvent{Kind: AgentEventReasoningDelta, Text: choice.Delta.ReasoningContent})
				}
				if choice.Delta.Content != "" {
					if text.Len()+len(choice.Delta.Content) > maxLLMResponseBytes {
						return result, raw.String(), done, ErrLLMResponseTooLarge
					}
					text.WriteString(choice.Delta.Content)
					emitAgent(onEvent, AgentEvent{Kind: AgentEventTextDelta, Text: choice.Delta.Content})
				}
				if choice.Delta.Refusal != "" {
					if len(result.Refusal)+len(choice.Delta.Refusal) > maxLLMResponseBytes {
						return result, raw.String(), done, ErrLLMResponseTooLarge
					}
					result.Refusal += choice.Delta.Refusal
					emitAgent(onEvent, AgentEvent{Kind: AgentEventRefusal, Text: choice.Delta.Refusal})
				}
				for _, delta := range choice.Delta.ToolCalls {
					call := calls[delta.Index]
					if call == nil {
						call = &accumulatedAgentCall{index: delta.Index}
						calls[delta.Index] = call
					}
					if delta.ID != "" {
						call.id = delta.ID
					}
					if delta.Function.Name != "" {
						call.name += delta.Function.Name
					}
					if !call.started {
						call.started = true
						emitAgent(onEvent, AgentEvent{Kind: AgentEventToolCallStarted, CallID: call.id, Name: call.name})
					}
					if delta.Function.Arguments != "" {
						if call.arguments.Len()+len(delta.Function.Arguments) > maxAgentToolArguments {
							return result, raw.String(), done, errors.New("模型工具参数超过 16 KiB")
						}
						call.arguments.WriteString(delta.Function.Arguments)
						emitAgent(onEvent, AgentEvent{Kind: AgentEventToolArguments, CallID: call.id, Text: delta.Function.Arguments})
					}
				}
				if choice.FinishReason != nil && *choice.FinishReason != "" {
					result.FinishReason = *choice.FinishReason
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return result, raw.String(), done, readErr
		}
	}
	result.Text = text.String()
	result.ToolCalls = materializeAgentCalls(calls, onEvent)
	// 已有 finish_reason 的干净 EOF 视为完整;只有 finish_reason 也缺失才算截断。
	if !done && result.FinishReason == "" {
		return result, raw.String(), done, io.ErrUnexpectedEOF
	}
	emitAgent(onEvent, AgentEvent{Kind: AgentEventStepCompleted, FinishReason: result.FinishReason})
	return result, raw.String(), true, nil
}

func (c *Client) agentResponses(ctx context.Context, p Params, messages []Message, tools []ToolDefinition, stream bool, onEvent AgentEventHandler) (result AgentStepResult, resultErr error) {
	body := responsesAgentRequest{
		Model: p.Model, Input: responsesAgentInputs(messages), Temperature: p.Temperature,
		TopP: p.TopP, MaxOutputTokens: p.MaxTokens, Stream: stream,
		Reasoning: responsesReasoningConfig(p.ReasoningEffort), Tools: responsesAgentTools(tools, strictToolsAllowed(p)),
	}
	if len(tools) > 0 && parallelToolFlagAllowed(p) {
		parallel := false
		body.ParallelToolCalls = &parallel
	}
	requestBody, err := json.Marshal(body)
	if err != nil {
		return result, err
	}
	auditOn := c.shouldAudit(ctx)
	started := time.Now()
	resp, err := c.doRaw(ctx, p, "/responses", requestBody, stream)
	if err != nil {
		c.writeAudit(ctx, auditOn, p, stream, requestBody, "", "", 0, "", false, false, err, started)
		return result, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := readAllBounded(resp.Body, maxHTTPErrorBytes)
		httpErr := &HTTPError{StatusCode: resp.StatusCode, Body: boundedHTTPErrorBody(raw)}
		c.writeAudit(ctx, auditOn, p, stream, requestBody, string(raw), "", resp.StatusCode, "", false, false, httpErr, started)
		return result, httpErr
	}
	var raw []byte
	var done bool
	if stream {
		var rawText string
		result, rawText, done, err = parseAgentResponsesStream(resp.Body, onEvent)
		raw = []byte(rawText)
	} else {
		raw, err = readAllBounded(resp.Body, maxLLMResponseBytes)
		if err == nil {
			result, err = parseAgentResponsesComplete(raw, onEvent)
		}
	}
	if err == nil && (result.FinishReason == "max_output_tokens" || result.FinishReason == "length") {
		err = ErrTruncated
	}
	if err == nil && strings.TrimSpace(result.TextProjection()) == "" && len(result.ToolCallBlocks()) == 0 {
		err = errors.New("模型响应为空")
	}
	c.writeAudit(ctx, auditOn, p, stream, requestBody, string(raw), result.TextProjection(), resp.StatusCode, result.FinishReason, done, err == nil, err, started)
	return result, err
}

func responsesAgentInputs(messages []Message) []responsesAgentInput {
	out := make([]responsesAgentInput, 0, len(messages)+4)
	for _, message := range messages {
		blocks := message.CanonicalBlocks()
		if message.Role == "tool" {
			out = append(out, responsesAgentInput{Type: "function_call_output", CallID: message.ToolCallID, Output: message.TextProjection()})
			continue
		}
		var text strings.Builder
		for _, block := range blocks {
			switch block.Kind {
			case ContentThinking:
				if len(block.Replay) > 0 && json.Valid(block.Replay) {
					out = append(out, responsesAgentInput{Raw: cloneJSON(block.Replay)})
				}
			case ContentText, ContentRefusal:
				text.WriteString(block.Text)
			case ContentToolCall:
				if block.ToolCall != nil {
					out = append(out, responsesAgentInput{ID: block.ToolCall.ItemID, Type: "function_call", CallID: block.ToolCall.ID, Name: block.ToolCall.Name, Arguments: block.ToolCall.Arguments})
				}
			}
		}
		if textString := text.String(); strings.TrimSpace(textString) != "" {
			out = append(out, responseTextInput(message.Role, textString))
		}
		if len(blocks) == 0 && strings.TrimSpace(message.Content) != "" {
			out = append(out, responseTextInput(message.Role, message.Content))
		}
	}
	return out
}

func responseTextInput(role, content string) responsesAgentInput {
	typeName := "input_text"
	if role == "assistant" {
		typeName = "output_text"
	}
	return responsesAgentInput{Type: "message", Role: role, Content: []responsesAgentContent{{Type: typeName, Text: content}}}
}

func responsesAgentTools(tools []ToolDefinition, strict bool) []responsesAgentTool {
	out := make([]responsesAgentTool, 0, len(tools))
	for _, tool := range tools {
		out = append(out, responsesAgentTool{
			Type: "function", Name: tool.Name, Description: tool.Description,
			Parameters: tool.Parameters, Strict: tool.Strict && strict,
		})
	}
	return out
}

func parseAgentResponsesComplete(raw []byte, onEvent AgentEventHandler) (AgentStepResult, error) {
	var response responsesAgentResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return AgentStepResult{}, err
	}
	result := AgentStepResult{FinishReason: responsesAgentFinishReason(response), Usage: response.Usage.normalized()}
	for _, item := range response.Output {
		switch item.Type {
		case "reasoning":
			appendReasoningItem(&result, item)
			if text := reasoningOutputText(item); text != "" {
				replay := cloneJSON(item.Raw)
				if len(replay) == 0 {
					replay, _ = json.Marshal(item)
				}
				result.Content = append(result.Content, ContentBlock{Kind: ContentThinking, Text: text, Replay: replay})
				emitAgent(onEvent, AgentEvent{Kind: AgentEventReasoningDelta, Text: text})
			}
		case "function_call":
			if len(item.Arguments) > maxAgentToolArguments {
				return result, errors.New("模型工具参数超过 16 KiB")
			}
			call := ToolCall{ID: item.CallID, ItemID: item.ID, Name: item.Name, Arguments: item.Arguments}
			result.ToolCalls = append(result.ToolCalls, call)
			result.Content = append(result.Content, ContentBlock{Kind: ContentToolCall, ToolCall: &call})
			emitAgent(onEvent, AgentEvent{Kind: AgentEventToolCallStarted, CallID: call.ID, Name: call.Name})
			emitAgent(onEvent, AgentEvent{Kind: AgentEventToolCallCompleted, CallID: call.ID, Name: call.Name, Arguments: call.Arguments})
		case "message":
			for _, content := range item.Content {
				switch content.Type {
				case "output_text":
					result.Text += content.Text
					result.Content = append(result.Content, ContentBlock{Kind: ContentText, Text: content.Text})
					emitAgent(onEvent, AgentEvent{Kind: AgentEventTextDelta, Text: content.Text})
				case "refusal":
					result.Refusal += content.Text
					result.Content = append(result.Content, ContentBlock{Kind: ContentRefusal, Text: content.Text})
					emitAgent(onEvent, AgentEvent{Kind: AgentEventRefusal, Text: content.Text})
				}
			}
		}
	}
	if result.Text == "" && len(result.ToolCalls) == 0 {
		result.Text = rawResponsesOutputText(response.OutputText)
		if result.Text != "" {
			result.Content = append(result.Content, ContentBlock{Kind: ContentText, Text: result.Text})
			emitAgent(onEvent, AgentEvent{Kind: AgentEventTextDelta, Text: result.Text})
		}
	}
	if response.Status == "failed" {
		return result, errors.New("模型响应失败")
	}
	if response.Status == "incomplete" && result.FinishReason == "" {
		result.FinishReason = "incomplete"
	}
	emitAgent(onEvent, AgentEvent{Kind: AgentEventStepCompleted, FinishReason: result.FinishReason})
	return result, nil
}

func parseAgentResponsesStream(reader io.Reader, onEvent AgentEventHandler) (AgentStepResult, string, bool, error) {
	var result AgentStepResult
	raw := newAuditBuffer(maxLLMAuditBytes)
	var text strings.Builder
	calls := map[string]*accumulatedAgentCall{}
	reasoningTexts := make(map[string]string)
	reasoningEmitted := make(map[string]bool)
	var reasoningTextOrder []string
	done := false
	// sawItemDone:网关省略 response.completed 但已宣告输出条目完成时,
	// 干净 EOF 同样按完整响应处理。
	sawItemDone := false
	eventName := ""
	buf := bufio.NewReader(reader)
	for {
		line, readErr := readSSELine(buf)
		raw.Write([]byte(line))
		if raw.TooLarge() {
			return result, raw.String(), done, ErrLLMResponseTooLarge
		}
		if errors.Is(readErr, ErrLLMSSELineTooLarge) {
			return result, raw.String(), done, readErr
		}
		if name := parseSSEEvent(line); name != "" {
			eventName = name
		}
		if data, ok := parseSSE(line); ok {
			if data == "[DONE]" {
				done = true
				break
			}
			var event responsesAgentStreamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				return result, raw.String(), done, fmt.Errorf("流式 JSON 解析失败: %w", err)
			}
			typ := event.Type
			if typ == "" {
				typ = eventName
			}
			switch typ {
			case "response.output_text.delta":
				if text.Len()+len(event.Delta) > maxLLMResponseBytes {
					return result, raw.String(), done, ErrLLMResponseTooLarge
				}
				text.WriteString(event.Delta)
				emitAgent(onEvent, AgentEvent{Kind: AgentEventTextDelta, Text: event.Delta})
			case "response.refusal.delta", "response.refusal.done":
				if len(result.Refusal)+len(event.Delta) > maxLLMResponseBytes {
					return result, raw.String(), done, ErrLLMResponseTooLarge
				}
				result.Refusal += event.Delta
				emitAgent(onEvent, AgentEvent{Kind: AgentEventRefusal, Text: event.Delta})
			case "response.reasoning_text.delta", "response.reasoning_summary_text.delta":
				if event.ItemID != "" {
					if _, exists := reasoningTexts[event.ItemID]; !exists {
						reasoningTextOrder = append(reasoningTextOrder, event.ItemID)
					}
					part := event.Delta
					if part == "" {
						part = event.Text
					}
					reasoningTexts[event.ItemID] += part
					if part != "" {
						reasoningEmitted[event.ItemID] = true
						emitAgent(onEvent, AgentEvent{Kind: AgentEventReasoningDelta, Text: part})
					}
				}
			case "response.reasoning_text.done", "response.reasoning_summary_text.done":
				if event.ItemID != "" {
					if _, exists := reasoningTexts[event.ItemID]; !exists {
						reasoningTextOrder = append(reasoningTextOrder, event.ItemID)
					}
					if event.Text != "" {
						reasoningTexts[event.ItemID] = event.Text
					} else if event.Delta != "" {
						reasoningTexts[event.ItemID] = event.Delta
					}
					if !reasoningEmitted[event.ItemID] {
						part := event.Text
						if part == "" {
							part = event.Delta
						}
						if part != "" {
							emitAgent(onEvent, AgentEvent{Kind: AgentEventReasoningDelta, Text: part})
							reasoningEmitted[event.ItemID] = true
						}
					}
				}
			case "response.output_item.added", "response.output_item.done":
				if typ == "response.output_item.done" {
					sawItemDone = true
				}
				if event.Item != nil && event.Item.Type == "reasoning" && typ == "response.output_item.done" {
					appendReasoningItem(&result, *event.Item)
					if event.Item.ID == "" || !reasoningEmitted[event.Item.ID] {
						if text := reasoningOutputText(*event.Item); text != "" {
							emitAgent(onEvent, AgentEvent{Kind: AgentEventReasoningDelta, Text: text})
							if event.Item.ID != "" {
								reasoningEmitted[event.Item.ID] = true
							}
						}
					}
				} else if event.Item != nil && event.Item.Type == "function_call" {
					if err := captureResponseFunctionCall(calls, *event.Item, event.OutputIndex, onEvent); err != nil {
						return result, raw.String(), done, err
					}
				} else if event.Item != nil && (event.Item.Type == "message" || event.Item.Type == "output_text") {
					if err := appendResponsesMessageText(&text, &result, *event.Item, onEvent); err != nil {
						return result, raw.String(), done, err
					}
				}
			case "response.function_call_arguments.delta", "response.function_call_arguments.done":
				key := responseCallKey(event.ItemID, event.CallID, event.OutputIndex)
				call := ensureResponseCall(calls, key, event.OutputIndex, event.ItemID, event.CallID, event.Name, onEvent)
				part := event.Delta
				if part == "" && call.arguments.Len() == 0 {
					part = event.Arguments
				}
				if part != "" {
					if call.arguments.Len()+len(part) > maxAgentToolArguments {
						return result, raw.String(), done, errors.New("模型工具参数超过 16 KiB")
					}
					call.arguments.WriteString(part)
					emitAgent(onEvent, AgentEvent{Kind: AgentEventToolArguments, CallID: call.id, Text: part})
				}
			case "response.completed":
				done = true
				if event.Response != nil {
					result.FinishReason = responsesAgentFinishReason(*event.Response)
					result.Usage = event.Response.Usage.normalized()
					for outputIndex, item := range event.Response.Output {
						appendReasoningItem(&result, item)
						if item.Type == "reasoning" && (item.ID == "" || !reasoningEmitted[item.ID]) {
							if text := reasoningOutputText(item); text != "" {
								emitAgent(onEvent, AgentEvent{Kind: AgentEventReasoningDelta, Text: text})
								if item.ID != "" {
									reasoningEmitted[item.ID] = true
								}
							}
						}
						if item.Type == "function_call" {
							if err := captureResponseFunctionCall(calls, item, outputIndex, onEvent); err != nil {
								return result, raw.String(), done, err
							}
						} else if item.Type == "message" || item.Type == "output_text" {
							if err := appendResponsesMessageText(&text, &result, item, onEvent); err != nil {
								return result, raw.String(), done, err
							}
						}
					}
					if err := appendResponsesText(&text, rawResponsesOutputText(event.Response.OutputText), onEvent); err != nil {
						return result, raw.String(), done, err
					}
				}
			case "response.incomplete":
				done = true
				if event.Response != nil {
					result.FinishReason = responsesAgentFinishReason(*event.Response)
					result.Usage = event.Response.Usage.normalized()
					for outputIndex, item := range event.Response.Output {
						appendReasoningItem(&result, item)
						if item.Type == "reasoning" && (item.ID == "" || !reasoningEmitted[item.ID]) {
							if text := reasoningOutputText(item); text != "" {
								emitAgent(onEvent, AgentEvent{Kind: AgentEventReasoningDelta, Text: text})
								if item.ID != "" {
									reasoningEmitted[item.ID] = true
								}
							}
						}
						if item.Type == "function_call" {
							if err := captureResponseFunctionCall(calls, item, outputIndex, onEvent); err != nil {
								return result, raw.String(), done, err
							}
						} else if item.Type == "message" || item.Type == "output_text" {
							if err := appendResponsesMessageText(&text, &result, item, onEvent); err != nil {
								return result, raw.String(), done, err
							}
						}
					}
					if err := appendResponsesText(&text, rawResponsesOutputText(event.Response.OutputText), onEvent); err != nil {
						return result, raw.String(), done, err
					}
				} else if event.IncompleteDetails != nil {
					result.FinishReason = event.IncompleteDetails.Reason
				}
			case "response.failed":
				if event.Error != nil && event.Error.Message != "" {
					return result, raw.String(), true, fmt.Errorf("模型响应失败: %s", event.Error.Message)
				}
				return result, raw.String(), true, errors.New("模型响应失败")
			}
			eventName = ""
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return result, raw.String(), done, readErr
		}
	}
	result.Text = text.String()
	result.ToolCalls = materializeResponseCalls(calls, onEvent)
	for _, itemID := range reasoningTextOrder {
		if mergeReasoningItemText(&result, itemID, reasoningTexts[itemID]) {
			continue
		}
		item, _ := json.Marshal(map[string]any{
			"type": "reasoning", "id": itemID, "status": "completed",
			"content": []map[string]string{{"type": "reasoning_text", "text": reasoningTexts[itemID]}},
		})
		result.ReasoningItems = append(result.ReasoningItems, item)
	}
	if !done && !sawItemDone {
		return result, raw.String(), done, io.ErrUnexpectedEOF
	}
	emitAgent(onEvent, AgentEvent{Kind: AgentEventStepCompleted, FinishReason: result.FinishReason})
	return result, raw.String(), true, nil
}

func responseCallKey(itemID, callID string, outputIndex int) string {
	if itemID != "" {
		return itemID
	}
	if callID != "" {
		return callID
	}
	return strconv.Itoa(outputIndex)
}

// appendResponsesText reconciles a terminal full-text projection with any
// deltas already received. A gateway may send the same prefix again in
// response.completed; only the missing suffix is emitted, never a duplicate.
func appendResponsesText(text *strings.Builder, value string, onEvent AgentEventHandler) error {
	if text == nil || value == "" {
		return nil
	}
	current := text.String()
	if current == value || strings.HasPrefix(current, value) {
		return nil
	}
	if strings.HasPrefix(value, current) {
		value = strings.TrimPrefix(value, current)
	}
	if value == "" {
		return nil
	}
	if text.Len()+len(value) > maxLLMResponseBytes {
		return ErrLLMResponseTooLarge
	}
	text.WriteString(value)
	emitAgent(onEvent, AgentEvent{Kind: AgentEventTextDelta, Text: value})
	return nil
}

func appendResponsesRefusal(result *AgentStepResult, value string, onEvent AgentEventHandler) {
	if result == nil || value == "" {
		return
	}
	current := result.Refusal
	if current == value || strings.HasPrefix(current, value) {
		return
	}
	if strings.HasPrefix(value, current) {
		value = strings.TrimPrefix(value, current)
	}
	if value == "" {
		return
	}
	result.Refusal += value
	emitAgent(onEvent, AgentEvent{Kind: AgentEventRefusal, Text: value})
}

func appendResponsesMessageText(text *strings.Builder, result *AgentStepResult, item responsesAgentOutput, onEvent AgentEventHandler) error {
	if item.Type != "message" && item.Type != "output_text" {
		return nil
	}
	if err := appendResponsesText(text, item.Text, onEvent); err != nil {
		return err
	}
	for _, content := range item.Content {
		switch content.Type {
		case "output_text", "text":
			if err := appendResponsesText(text, content.Text, onEvent); err != nil {
				return err
			}
		case "refusal":
			if len(result.Refusal)+len(content.Text) > maxLLMResponseBytes {
				return ErrLLMResponseTooLarge
			}
			appendResponsesRefusal(result, content.Text, onEvent)
		}
	}
	return nil
}

func captureResponseFunctionCall(calls map[string]*accumulatedAgentCall, item responsesAgentOutput, outputIndex int, onEvent AgentEventHandler) error {
	key := responseCallKey(item.ID, item.CallID, outputIndex)
	call := ensureResponseCall(calls, key, outputIndex, item.ID, item.CallID, item.Name, onEvent)
	if item.Arguments != "" && call.arguments.Len() == 0 {
		if len(item.Arguments) > maxAgentToolArguments {
			return errors.New("模型工具参数超过 16 KiB")
		}
		call.arguments.WriteString(item.Arguments)
	}
	return nil
}

func ensureResponseCall(calls map[string]*accumulatedAgentCall, key string, index int, itemID, callID, name string, onEvent AgentEventHandler) *accumulatedAgentCall {
	call := calls[key]
	if call == nil {
		call = &accumulatedAgentCall{index: index, id: callID, itemID: itemID, name: name, started: true}
		calls[key] = call
		emitAgent(onEvent, AgentEvent{Kind: AgentEventToolCallStarted, CallID: callID, Name: name})
		return call
	}
	if itemID != "" {
		call.itemID = itemID
	}
	if callID != "" {
		call.id = callID
	}
	if name != "" {
		call.name = name
	}
	return call
}

func materializeAgentCalls(values map[int]*accumulatedAgentCall, onEvent AgentEventHandler) []ToolCall {
	indexes := make([]int, 0, len(values))
	for index := range values {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	out := make([]ToolCall, 0, len(indexes))
	for _, index := range indexes {
		call := values[index]
		item := ToolCall{ID: call.id, Name: call.name, Arguments: call.arguments.String()}
		out = append(out, item)
		emitAgent(onEvent, AgentEvent{Kind: AgentEventToolCallCompleted, CallID: item.ID, Name: item.Name, Arguments: item.Arguments})
	}
	return out
}

func materializeResponseCalls(values map[string]*accumulatedAgentCall, onEvent AgentEventHandler) []ToolCall {
	calls := make([]*accumulatedAgentCall, 0, len(values))
	for _, call := range values {
		calls = append(calls, call)
	}
	sort.Slice(calls, func(i, j int) bool { return calls[i].index < calls[j].index })
	out := make([]ToolCall, 0, len(calls))
	for _, call := range calls {
		item := ToolCall{ID: call.id, ItemID: call.itemID, Name: call.name, Arguments: call.arguments.String()}
		out = append(out, item)
		emitAgent(onEvent, AgentEvent{Kind: AgentEventToolCallCompleted, CallID: item.ID, Name: item.Name, Arguments: item.Arguments})
	}
	return out
}

func responsesAgentFinishReason(response responsesAgentResponse) string {
	if response.IncompleteDetails != nil {
		return response.IncompleteDetails.Reason
	}
	if response.Status == "completed" {
		return "stop"
	}
	return response.Status
}

func rawResponsesOutputText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var values []string
	if json.Unmarshal(raw, &values) == nil {
		return strings.Join(values, "")
	}
	return ""
}

// reasoningOutputText extracts only provider-declared readable text from a
// reasoning output item. Upstream-owned fields such as encrypted_content are
// intentionally ignored; the raw item is still retained separately for
// protocol replay.
func reasoningOutputText(item responsesAgentOutput) string {
	if item.Type != "reasoning" {
		return ""
	}
	parts := make([]string, 0, len(item.Content)+len(item.Summary)+1)
	appendPart := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range parts {
			if existing == value {
				return
			}
		}
		parts = append(parts, value)
	}
	appendPart(item.Text)
	for _, content := range item.Content {
		appendPart(content.Text)
	}
	for _, summary := range item.Summary {
		appendPart(summary.Text)
	}
	return strings.Join(parts, "\n")
}

func cloneJSON(raw json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), raw...)
}

func appendReasoningItem(result *AgentStepResult, item responsesAgentOutput) {
	if result == nil || item.Type != "reasoning" {
		return
	}
	raw := item.Raw
	if len(raw) == 0 {
		raw, _ = json.Marshal(item)
	}
	if len(raw) == 0 || !json.Valid(raw) || hasReasoningItem(result.ReasoningItems, item.ID) {
		return
	}
	result.ReasoningItems = append(result.ReasoningItems, cloneJSON(raw))
}

func hasReasoningItem(items []json.RawMessage, id string) bool {
	if id == "" {
		return false
	}
	for _, raw := range items {
		var item struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &item) == nil && item.ID == id {
			return true
		}
	}
	return false
}

// mergeReasoningItemText completes a reasoning item emitted by a streaming
// gateway that puts the text in reasoning_text events but leaves the final
// output_item.done content array empty. Keeping the original object preserves
// provider-specific fields such as encrypted_content while making the replay
// payload acceptable to providers that require reasoning_text.
func mergeReasoningItemText(result *AgentStepResult, id, text string) bool {
	if result == nil || id == "" {
		return false
	}
	for index, raw := range result.ReasoningItems {
		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &object) != nil {
			continue
		}
		var itemID string
		if json.Unmarshal(object["id"], &itemID) != nil || itemID != id {
			continue
		}
		if strings.TrimSpace(text) != "" {
			var content []map[string]any
			if contentRaw, ok := object["content"]; ok && string(contentRaw) != "null" {
				_ = json.Unmarshal(contentRaw, &content)
			}
			hasText := false
			for _, part := range content {
				if partType, _ := part["type"].(string); partType == "reasoning_text" {
					if existing, _ := part["text"].(string); existing != "" {
						hasText = true
						break
					}
				}
			}
			if !hasText {
				content = append(content, map[string]any{"type": "reasoning_text", "text": text})
				encoded, err := json.Marshal(content)
				if err == nil {
					object["content"] = encoded
					if updated, err := json.Marshal(object); err == nil {
						result.ReasoningItems[index] = updated
					}
				}
			}
		}
		return true
	}
	return false
}

func emitAgent(handler AgentEventHandler, event AgentEvent) {
	if handler != nil {
		handler(event)
	}
}
