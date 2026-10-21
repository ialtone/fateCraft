package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ialtone/fatecraft/internal/llmoutbound"
)

// ErrTruncated reports that the provider stopped generating because the
// max-token limit was reached. Accumulated text remains diagnostic-only and
// must not be committed as the final assistant response.
var ErrTruncated = errors.New("模型输出因达到最大回复 token 上限被截断")

// HTTPError preserves the provider response needed for explicit diagnostics
// and operator-visible request logs. Its body is bounded before exposure.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("LLM 请求失败 (%d): %s", e.StatusCode, e.Body)
}

func (e *HTTPError) SafeLogMessage() string { return fmt.Sprintf("LLM HTTP %d", e.StatusCode) }

type providerMessageError struct{ message string }

func (e *providerMessageError) Error() string        { return "模型响应失败: " + e.message }
func (*providerMessageError) SafeLogMessage() string { return "模型响应报告失败" }

// Client 是一个支持 Chat Completions 和 Responses 两种 OpenAI 兼容协议的
// HTTP 客户端。协议由 Params.Protocol 选择，空值按 Responses 处理。
type Client struct {
	http         *http.Client
	outbound     *llmoutbound.Manager
	auditWriter  RequestAuditWriter
	auditEnabled func(context.Context, Params) bool
}

// responseHeaderTimeout 只约束"发出请求到收到响应头"的等待;模型排队或
// 冷启动很少超过这个值。流式正文由 streamIdleTimeout 逐块看护。
const responseHeaderTimeout = 2 * time.Minute

func NewClient() *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// LLM 专用客户端保持显式直连，不继承环境代理变量；生产装配会用
	// llmoutbound 快照覆盖它。
	transport.Proxy = nil
	transport.ResponseHeaderTimeout = responseHeaderTimeout
	// 不设置 http.Client.Timeout:它覆盖整个正文读取,会把长叙事/深度思考
	// 在固定时刻切断。总时长完全由调用方 ctx(attempt / 记忆 / 问答)控制。
	return &Client{http: &http.Client{Transport: transport}}
}

// SetOutbound injects the shared server-side exit manager. Every logical call
// captures one immutable snapshot before its first transport attempt, so all
// retries use the same proxy pool even if settings change mid-call.
func (c *Client) SetOutbound(manager *llmoutbound.Manager) {
	c.outbound = manager
}

// SetRequestAudit 安装调用摘要写入器。enabled 只控制是否采集详细正文。
// 由管理员开关和调用归属控制，在每次逻辑调用开始时固定采集决定。
func (c *Client) SetRequestAudit(writer RequestAuditWriter, enabled func(context.Context, Params) bool) {
	c.auditWriter = writer
	c.auditEnabled = enabled
}

// chatRequest 是发送给 /chat/completions 的请求体。
type chatRequest struct {
	Model           string             `json:"model"`
	Messages        []chatAgentMessage `json:"messages"`
	Temperature     float64            `json:"temperature"`
	TopP            float64            `json:"top_p"`
	MaxTokens       int                `json:"max_tokens,omitempty"`
	Stream          bool               `json:"stream"`
	ReasoningEffort string             `json:"reasoning_effort,omitempty"`
	ResponseFormat  *ResponseFormat    `json:"response_format,omitempty"`
}

// streamChunk 是 Chat Completions 流式响应中单个 SSE 数据块的结构。
type streamChunk struct {
	Usage   *agentTokenUsage `json:"usage"`
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// completionResp 是 Chat Completions 非流式响应的结构。
type completionResp struct {
	Usage   *agentTokenUsage `json:"usage"`
	Choices []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
}

// responsesRequest 是 OpenAI Responses API 的请求体。
type responsesRequest struct {
	Model           string                  `json:"model"`
	Input           []responsesInputMessage `json:"input"`
	Temperature     float64                 `json:"temperature"`
	TopP            float64                 `json:"top_p"`
	MaxOutputTokens int                     `json:"max_output_tokens,omitempty"`
	Stream          bool                    `json:"stream"`
	Reasoning       *responsesReasoning     `json:"reasoning,omitempty"`
	Text            *responsesTextConfig    `json:"text,omitempty"`
}

// responsesReasoning 是 Responses 协议的思考配置；仅在用户设置了思考强度时发送。
type responsesReasoning struct {
	Effort string `json:"effort,omitempty"`
}

// responsesReasoningConfig 把可选思考强度转换为请求字段，空值返回 nil 以完全省略。
func responsesReasoningConfig(effort string) *responsesReasoning {
	if effort == "" {
		return nil
	}
	return &responsesReasoning{Effort: effort}
}

type responsesInputMessage struct {
	Role    string                  `json:"role"`
	Content []responsesInputContent `json:"content"`
	Raw     json.RawMessage         `json:"-"`
}

func (item responsesInputMessage) MarshalJSON() ([]byte, error) {
	if len(item.Raw) > 0 {
		return item.Raw, nil
	}
	type wire responsesInputMessage
	return json.Marshal(wire(item))
}

type responsesInputContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responsesTextConfig struct {
	Format *responsesFormat `json:"format,omitempty"`
}

type responsesFormat struct {
	Type        string         `json:"type"`
	Name        string         `json:"name,omitempty"`
	Description string         `json:"description,omitempty"`
	Schema      map[string]any `json:"schema,omitempty"`
	Strict      *bool          `json:"strict,omitempty"`
}

// responsesResponse is shared by non-streaming responses and terminal stream
// events. OutputText is raw because a few compatible gateways return an array
// of strings even though the official API returns a single string.
type responsesResponse struct {
	Usage             *agentTokenUsage           `json:"usage"`
	Output            []responsesOutputItem      `json:"output"`
	OutputText        json.RawMessage            `json:"output_text"`
	Status            string                     `json:"status"`
	IncompleteDetails *responsesIncompleteDetail `json:"incomplete_details"`
}

type responsesOutputItem struct {
	Type    string                   `json:"type"`
	Role    string                   `json:"role"`
	Text    string                   `json:"text"`
	Content []responsesOutputContent `json:"content"`
}

type responsesOutputContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responsesIncompleteDetail struct {
	Reason string `json:"reason"`
}

type responsesStreamEvent struct {
	Type              string                     `json:"type"`
	Delta             string                     `json:"delta"`
	Response          *responsesResponse         `json:"response"`
	IncompleteDetails *responsesIncompleteDetail `json:"incomplete_details"`
	Error             *responsesEventError       `json:"error"`
}

type responsesEventError struct {
	Message string `json:"message"`
}

// Stream 以流式方式请求模型，每个增量通过 onDelta 回调返回，最终返回完整文本。
// ctx 取消时请求随之中止。
func (c *Client) Stream(ctx context.Context, p Params, messages []Message, onDelta func(string)) (string, error) {
	var err error
	p, err = PlanParams(p, RequestKindText)
	if err != nil {
		return "", err
	}
	return c.adapter(p).Stream(ctx, p, messages, onDelta)
}

func (c *Client) streamChat(ctx context.Context, p Params, messages []Message, onDelta func(string)) (string, error) {
	body := chatRequest{
		Model:           p.Model,
		Messages:        chatAgentMessages(messages),
		Temperature:     p.Temperature,
		TopP:            p.TopP,
		MaxTokens:       p.MaxTokens,
		Stream:          true,
		ReasoningEffort: p.ReasoningEffort,
		ResponseFormat:  p.ResponseFormat,
	}
	requestBody, marshalErr := json.Marshal(body)
	ctx, auditOn := c.beginAudit(ctx, p)
	started := time.Now()
	if marshalErr != nil {
		c.writeAudit(ctx, auditOn, p, true, nil, "", "", 0, "", false, false, marshalErr, started)
		return "", marshalErr
	}
	resp, err := c.doRaw(ctx, p, "/chat/completions", requestBody, true)
	if err != nil {
		c.writeAudit(ctx, auditOn, p, true, requestBody, "", "", 0, "", false, false, err, started)
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		responseBody, _ := readAllBounded(resp.Body, maxHTTPErrorBytes)
		httpErr := &HTTPError{StatusCode: resp.StatusCode, Body: boundedHTTPErrorBody(responseBody)}
		c.writeAudit(ctx, auditOn, p, true, requestBody, string(responseBody), "", resp.StatusCode, "", false, false, httpErr, started)
		return "", httpErr
	}

	var full strings.Builder
	raw := newCaptureBuffer(auditOn)
	finishReason := ""
	receivedDone := false
	reader := bufio.NewReader(resp.Body)
	for {
		line, readErr := readSSELine(reader)
		raw.Write([]byte(line))
		if raw.TooLarge() {
			err := ErrLLMResponseTooLarge
			c.writeAudit(ctx, auditOn, p, true, requestBody, raw.String(), full.String(), resp.StatusCode, finishReason, receivedDone, false, err, started)
			return full.String(), err
		}
		if errors.Is(readErr, ErrLLMSSELineTooLarge) {
			c.writeAudit(ctx, auditOn, p, true, requestBody, raw.String(), full.String(), resp.StatusCode, finishReason, receivedDone, false, readErr, started)
			return full.String(), readErr
		}
		if len(line) > 0 {
			data, ok := parseSSE(line)
			if ok {
				if data == "[DONE]" {
					receivedDone = true
					break
				}
				var chunk streamChunk
				if err := json.Unmarshal([]byte(data), &chunk); err != nil {
					err = fmt.Errorf("流式 JSON 解析失败: %w", err)
					c.writeAudit(ctx, auditOn, p, true, requestBody, raw.String(), full.String(), resp.StatusCode, finishReason, receivedDone, false, err, started)
					return full.String(), err
				}
				auditCall(ctx).setUsage(chunk.Usage)
				for _, ch := range chunk.Choices {
					if ch.Delta.Content != "" {
						if full.Len()+len(ch.Delta.Content) > maxLLMResponseBytes {
							err := ErrLLMResponseTooLarge
							c.writeAudit(ctx, auditOn, p, true, requestBody, raw.String(), full.String(), resp.StatusCode, finishReason, receivedDone, false, err, started)
							return full.String(), err
						}
						full.WriteString(ch.Delta.Content)
						if onDelta != nil {
							onDelta(ch.Delta.Content)
						}
					}
					if ch.FinishReason != nil && *ch.FinishReason != "" {
						finishReason = *ch.FinishReason
					}
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			c.writeAudit(ctx, auditOn, p, true, requestBody, raw.String(), full.String(), resp.StatusCode, finishReason, receivedDone, false, readErr, started)
			return full.String(), readErr
		}
	}
	// 部分兼容网关在 finish_reason 之后直接关流、不发 [DONE]。已经拿到
	// finish_reason 的干净 EOF 视为完整;只有既无终止标记也无 finish_reason
	// 才判为流被截断。
	if !receivedDone && finishReason == "" {
		err := fmt.Errorf("%w: 流未收到终止事件", io.ErrUnexpectedEOF)
		c.writeAudit(ctx, auditOn, p, true, requestBody, raw.String(), full.String(), resp.StatusCode, finishReason, false, false, err, started)
		return full.String(), err
	}
	if finishReason == "length" {
		c.writeAudit(ctx, auditOn, p, true, requestBody, raw.String(), full.String(), resp.StatusCode, finishReason, receivedDone, false, ErrTruncated, started)
		return full.String(), ErrTruncated
	}
	c.writeAudit(ctx, auditOn, p, true, requestBody, raw.String(), full.String(), resp.StatusCode, finishReason, receivedDone, true, nil, started)
	return full.String(), nil
}

// Complete 以非流式方式请求模型并返回完整文本。
func (c *Client) Complete(ctx context.Context, p Params, messages []Message) (string, error) {
	var err error
	p, err = PlanParams(p, RequestKindText)
	if err != nil {
		return "", err
	}
	return c.adapter(p).Complete(ctx, p, messages)
}

func (c *Client) completeChat(ctx context.Context, p Params, messages []Message) (string, error) {
	body := chatRequest{
		Model:           p.Model,
		Messages:        chatAgentMessages(messages),
		Temperature:     p.Temperature,
		TopP:            p.TopP,
		MaxTokens:       p.MaxTokens,
		Stream:          false,
		ReasoningEffort: p.ReasoningEffort,
		ResponseFormat:  p.ResponseFormat,
	}
	requestBody, marshalErr := json.Marshal(body)
	ctx, auditOn := c.beginAudit(ctx, p)
	started := time.Now()
	if marshalErr != nil {
		c.writeAudit(ctx, auditOn, p, false, nil, "", "", 0, "", false, false, marshalErr, started)
		return "", marshalErr
	}
	resp, err := c.doRaw(ctx, p, "/chat/completions", requestBody, false)
	if err != nil {
		c.writeAudit(ctx, auditOn, p, false, requestBody, "", "", 0, "", false, false, err, started)
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		responseBody, _ := readAllBounded(resp.Body, maxHTTPErrorBytes)
		httpErr := &HTTPError{StatusCode: resp.StatusCode, Body: boundedHTTPErrorBody(responseBody)}
		c.writeAudit(ctx, auditOn, p, false, requestBody, string(responseBody), "", resp.StatusCode, "", false, false, httpErr, started)
		return "", httpErr
	}
	responseBody, readErr := readAllBounded(resp.Body, maxLLMResponseBytes)
	if readErr != nil {
		c.writeAudit(ctx, auditOn, p, false, requestBody, string(responseBody), "", resp.StatusCode, "", false, false, readErr, started)
		return "", readErr
	}
	var out completionResp
	if err := json.Unmarshal(responseBody, &out); err != nil {
		c.writeAudit(ctx, auditOn, p, false, requestBody, string(responseBody), "", resp.StatusCode, "", false, false, err, started)
		return "", err
	}
	if len(out.Choices) == 0 {
		err := fmt.Errorf("模型未返回任何 choices")
		c.writeAudit(ctx, auditOn, p, false, requestBody, string(responseBody), "", resp.StatusCode, "", false, false, err, started)
		return "", err
	}
	if out.Choices[0].FinishReason == "length" {
		c.writeAudit(ctx, auditOn, p, false, requestBody, string(responseBody), out.Choices[0].Message.Content, resp.StatusCode, out.Choices[0].FinishReason, false, false, ErrTruncated, started)
		return out.Choices[0].Message.Content, ErrTruncated
	}
	c.writeAudit(ctx, auditOn, p, false, requestBody, string(responseBody), out.Choices[0].Message.Content, resp.StatusCode, out.Choices[0].FinishReason, false, true, nil, started)
	return out.Choices[0].Message.Content, nil
}

func (c *Client) streamResponses(ctx context.Context, p Params, messages []Message, onDelta func(string)) (string, error) {
	body := responsesRequest{
		Model:           p.Model,
		Input:           responsesInput(messages),
		Temperature:     p.Temperature,
		TopP:            p.TopP,
		MaxOutputTokens: p.MaxTokens,
		Stream:          true,
		Reasoning:       responsesReasoningConfig(p.ReasoningEffort),
		Text:            responsesText(p.ResponseFormat),
	}
	requestBody, marshalErr := json.Marshal(body)
	ctx, auditOn := c.beginAudit(ctx, p)
	started := time.Now()
	if marshalErr != nil {
		c.writeAudit(ctx, auditOn, p, true, nil, "", "", 0, "", false, false, marshalErr, started)
		return "", marshalErr
	}
	resp, err := c.doRaw(ctx, p, "/responses", requestBody, true)
	if err != nil {
		c.writeAudit(ctx, auditOn, p, true, requestBody, "", "", 0, "", false, false, err, started)
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		responseBody, _ := readAllBounded(resp.Body, maxHTTPErrorBytes)
		httpErr := &HTTPError{StatusCode: resp.StatusCode, Body: boundedHTTPErrorBody(responseBody)}
		c.writeAudit(ctx, auditOn, p, true, requestBody, string(responseBody), "", resp.StatusCode, "", false, false, httpErr, started)
		return "", httpErr
	}

	var full strings.Builder
	raw := newCaptureBuffer(auditOn)
	var finalResponse *responsesResponse
	finishReason := ""
	receivedDone := false
	var streamErr error
	eventName := ""
	// sawOutputDone 记录是否收到过任意 *.done 事件:网关若省略 response.completed
	// 但已经宣告输出条目完成,干净 EOF 同样视为完整响应。
	sawOutputDone := false
	reader := bufio.NewReader(resp.Body)
	appendDelta := func(delta string) {
		if delta == "" || streamErr != nil {
			return
		}
		if full.Len()+len(delta) > maxLLMResponseBytes {
			streamErr = ErrLLMResponseTooLarge
			return
		}
		full.WriteString(delta)
		if onDelta != nil {
			onDelta(delta)
		}
	}
	for {
		line, readErr := readSSELine(reader)
		raw.Write([]byte(line))
		if raw.TooLarge() {
			streamErr = ErrLLMResponseTooLarge
			break
		}
		if errors.Is(readErr, ErrLLMSSELineTooLarge) {
			streamErr = readErr
			break
		}
		if name := parseSSEEvent(line); name != "" {
			eventName = name
		}
		if data, ok := parseSSE(line); ok {
			if data == "[DONE]" {
				receivedDone = true
				break
			}
			var event responsesStreamEvent
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				streamErr = fmt.Errorf("流式 JSON 解析失败: %w", err)
				break
			}
			if event.Response != nil {
				auditCall(ctx).setUsage(event.Response.Usage)
			}
			{
				typ := event.Type
				if typ == "" {
					typ = eventName
				}
				switch typ {
				case "response.output_text.delta":
					appendDelta(event.Delta)
				case "response.output_text.done", "response.output_item.done", "response.content_part.done":
					sawOutputDone = true
				case "response.completed":
					receivedDone = true
					finalResponse = event.Response
				case "response.incomplete":
					receivedDone = true
					finalResponse = event.Response
					if finalResponse != nil && finalResponse.IncompleteDetails != nil {
						finishReason = finalResponse.IncompleteDetails.Reason
					} else if event.IncompleteDetails != nil {
						finishReason = event.IncompleteDetails.Reason
					}
				case "response.failed":
					receivedDone = true
					if event.Error != nil && event.Error.Message != "" {
						streamErr = &providerMessageError{message: event.Error.Message}
					} else {
						streamErr = errors.New("模型响应失败")
					}
				}
				// Some compatible gateways omit the event type but still emit delta.
				if typ == "" {
					appendDelta(event.Delta)
				}
			}
			eventName = ""
		}
		if streamErr != nil {
			break
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			streamErr = readErr
			break
		}
	}
	if streamErr == nil && !receivedDone && !sawOutputDone {
		streamErr = fmt.Errorf("%w: 流未收到终止事件", io.ErrUnexpectedEOF)
	}
	if finalResponse != nil && full.Len() == 0 {
		appendDelta(responseOutputText(*finalResponse))
	}
	if streamErr != nil {
		c.writeAudit(ctx, auditOn, p, true, requestBody, raw.String(), full.String(), resp.StatusCode, finishReason, receivedDone, false, streamErr, started)
		return full.String(), streamErr
	}
	if finishReason == "max_output_tokens" || finishReason == "length" {
		c.writeAudit(ctx, auditOn, p, true, requestBody, raw.String(), full.String(), resp.StatusCode, finishReason, receivedDone, false, ErrTruncated, started)
		return full.String(), ErrTruncated
	}
	if finishReason != "" {
		err := fmt.Errorf("模型响应未完成: %s", finishReason)
		c.writeAudit(ctx, auditOn, p, true, requestBody, raw.String(), full.String(), resp.StatusCode, finishReason, receivedDone, false, err, started)
		return full.String(), err
	}
	if finalResponse != nil && finalResponse.Status == "incomplete" {
		err := fmt.Errorf("模型响应未完成")
		if finalResponse.IncompleteDetails != nil && finalResponse.IncompleteDetails.Reason != "" {
			err = fmt.Errorf("模型响应未完成: %s", finalResponse.IncompleteDetails.Reason)
		}
		c.writeAudit(ctx, auditOn, p, true, requestBody, raw.String(), full.String(), resp.StatusCode, finishReason, receivedDone, false, err, started)
		return full.String(), err
	}
	c.writeAudit(ctx, auditOn, p, true, requestBody, raw.String(), full.String(), resp.StatusCode, finishReason, receivedDone, true, nil, started)
	return full.String(), nil
}

func (c *Client) completeResponses(ctx context.Context, p Params, messages []Message) (string, error) {
	body := responsesRequest{
		Model:           p.Model,
		Input:           responsesInput(messages),
		Temperature:     p.Temperature,
		TopP:            p.TopP,
		MaxOutputTokens: p.MaxTokens,
		Stream:          false,
		Reasoning:       responsesReasoningConfig(p.ReasoningEffort),
		Text:            responsesText(p.ResponseFormat),
	}
	requestBody, marshalErr := json.Marshal(body)
	ctx, auditOn := c.beginAudit(ctx, p)
	started := time.Now()
	if marshalErr != nil {
		c.writeAudit(ctx, auditOn, p, false, nil, "", "", 0, "", false, false, marshalErr, started)
		return "", marshalErr
	}
	resp, err := c.doRaw(ctx, p, "/responses", requestBody, false)
	if err != nil {
		c.writeAudit(ctx, auditOn, p, false, requestBody, "", "", 0, "", false, false, err, started)
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		responseBody, _ := readAllBounded(resp.Body, maxHTTPErrorBytes)
		httpErr := &HTTPError{StatusCode: resp.StatusCode, Body: boundedHTTPErrorBody(responseBody)}
		c.writeAudit(ctx, auditOn, p, false, requestBody, string(responseBody), "", resp.StatusCode, "", false, false, httpErr, started)
		return "", httpErr
	}
	responseBody, readErr := readAllBounded(resp.Body, maxLLMResponseBytes)
	if readErr != nil {
		c.writeAudit(ctx, auditOn, p, false, requestBody, string(responseBody), "", resp.StatusCode, "", false, false, readErr, started)
		return "", readErr
	}
	var out responsesResponse
	if err := json.Unmarshal(responseBody, &out); err != nil {
		c.writeAudit(ctx, auditOn, p, false, requestBody, string(responseBody), "", resp.StatusCode, "", false, false, err, started)
		return "", err
	}
	content := responseOutputText(out)
	finishReason := responsesIncompleteReason(out)
	if finishReason == "max_output_tokens" || finishReason == "length" {
		c.writeAudit(ctx, auditOn, p, false, requestBody, string(responseBody), content, resp.StatusCode, finishReason, false, false, ErrTruncated, started)
		return content, ErrTruncated
	}
	if out.Status == "failed" {
		err := errors.New("模型响应失败")
		c.writeAudit(ctx, auditOn, p, false, requestBody, string(responseBody), content, resp.StatusCode, "failed", false, false, err, started)
		return content, err
	}
	if out.Status == "incomplete" {
		err := errors.New("模型响应未完成")
		if finishReason != "" {
			err = fmt.Errorf("模型响应未完成: %s", finishReason)
		}
		c.writeAudit(ctx, auditOn, p, false, requestBody, string(responseBody), content, resp.StatusCode, finishReason, false, false, err, started)
		return content, err
	}
	if content == "" {
		err := fmt.Errorf("模型未返回任何 output 文本")
		c.writeAudit(ctx, auditOn, p, false, requestBody, string(responseBody), "", resp.StatusCode, finishReason, false, false, err, started)
		return "", err
	}
	c.writeAudit(ctx, auditOn, p, false, requestBody, string(responseBody), content, resp.StatusCode, finishReason, false, true, nil, started)
	return content, nil
}

func responsesInput(messages []Message) []responsesInputMessage {
	input := make([]responsesInputMessage, 0, len(messages)+4)
	for _, message := range messages {
		var text strings.Builder
		for _, block := range message.CanonicalBlocks() {
			if block.Kind == ContentThinking && len(block.Replay) > 0 && json.Valid(block.Replay) {
				input = append(input, responsesInputMessage{Raw: append(json.RawMessage(nil), block.Replay...)})
				continue
			}
			if block.Kind == ContentText || block.Kind == ContentRefusal {
				text.WriteString(block.Text)
			}
		}
		value := text.String()
		if value == "" {
			value = message.Content
		}
		input = append(input, responsesInputMessage{Role: message.Role, Content: []responsesInputContent{{Type: "input_text", Text: value}}})
	}
	return input
}

func responsesText(format *ResponseFormat) *responsesTextConfig {
	if format == nil {
		return nil
	}
	out := &responsesFormat{Type: format.Type}
	if format.Type == "json_schema" && format.JSONSchema != nil {
		strict := format.JSONSchema.Strict
		out.Name = format.JSONSchema.Name
		out.Description = format.JSONSchema.Description
		out.Schema = format.JSONSchema.Schema
		out.Strict = &strict
	}
	return &responsesTextConfig{Format: out}
}

func responseOutputText(response responsesResponse) string {
	if len(response.OutputText) > 0 && string(response.OutputText) != "null" {
		var text string
		if json.Unmarshal(response.OutputText, &text) == nil && text != "" {
			return text
		}
		var texts []string
		if json.Unmarshal(response.OutputText, &texts) == nil {
			return strings.Join(texts, "")
		}
	}
	var out strings.Builder
	for _, item := range response.Output {
		if item.Text != "" {
			out.WriteString(item.Text)
		}
		for _, content := range item.Content {
			if content.Text != "" {
				out.WriteString(content.Text)
			}
		}
	}
	return out.String()
}

func responsesIncompleteReason(response responsesResponse) string {
	if response.IncompleteDetails == nil {
		return ""
	}
	return response.IncompleteDetails.Reason
}
