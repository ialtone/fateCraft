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
	"strings"
	"time"
)

type chatToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type structuredChatResponse struct {
	Choices []struct {
		Message struct {
			Content   string         `json:"content"`
			ToolCalls []chatToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

type structuredChatChunk struct {
	Usage   *agentTokenUsage `json:"usage"`
	Choices []struct {
		Delta struct {
			Content   string         `json:"content"`
			ToolCalls []chatToolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

type structuredResponsesItem struct {
	ID        string                   `json:"id"`
	Type      string                   `json:"type"`
	Name      string                   `json:"name"`
	Arguments string                   `json:"arguments"`
	Text      string                   `json:"text"`
	Content   []responsesOutputContent `json:"content"`
}

type structuredResponsesResponse struct {
	Usage             *agentTokenUsage           `json:"usage"`
	Output            []structuredResponsesItem  `json:"output"`
	OutputText        json.RawMessage            `json:"output_text"`
	Status            string                     `json:"status"`
	IncompleteDetails *responsesIncompleteDetail `json:"incomplete_details"`
}

type structuredResponsesEvent struct {
	Type              string                       `json:"type"`
	Delta             string                       `json:"delta"`
	Text              string                       `json:"text"`
	Arguments         string                       `json:"arguments"`
	ItemID            string                       `json:"item_id"`
	OutputIndex       int                          `json:"output_index"`
	Item              *structuredResponsesItem     `json:"item"`
	Response          *structuredResponsesResponse `json:"response"`
	IncompleteDetails *responsesIncompleteDetail   `json:"incomplete_details"`
	Error             *responsesEventError         `json:"error"`
}

type accumulatedToolCall struct {
	Name      string
	Arguments strings.Builder
}

// CompleteStructured executes one non-streaming strict request. It never
// falls back to a weaker format or replays a request after response delivery;
// safe transport retries are still handled before a response body arrives.
func (c *Client) CompleteStructured(ctx context.Context, p Params, messages []Message) (StructuredResult, error) {
	var err error
	p, err = PlanParams(p, RequestKindStructured)
	if err != nil {
		return StructuredResult{}, err
	}
	if err := p.StructuredOutput.Validate(); err != nil {
		return StructuredResult{}, err
	}
	return c.adapter(p).CompleteStructured(ctx, p, messages)
}

// StreamStructured executes one streaming strict JSON Schema request.
func (c *Client) StreamStructured(ctx context.Context, p Params, messages []Message, onDelta func(string)) (StructuredResult, error) {
	var err error
	p, err = PlanParams(p, RequestKindStructured)
	if err != nil {
		return StructuredResult{}, err
	}
	if err := p.StructuredOutput.Validate(); err != nil {
		return StructuredResult{}, err
	}
	return c.adapter(p).StreamStructured(ctx, p, messages, onDelta)
}

func (c *Client) callStructuredChat(ctx context.Context, p Params, messages []Message, stream bool, onDelta func(string)) (result StructuredResult, resultErr error) {
	body := map[string]any{
		"model": p.Model, "messages": chatAgentMessages(messages), "temperature": p.Temperature,
		"top_p": p.TopP, "stream": stream,
	}
	if p.MaxTokens > 0 {
		body["max_tokens"] = p.MaxTokens
	}
	applyChatStructured(body, p.StructuredOutput)
	requestBody, err := json.Marshal(body)
	if err != nil {
		return result, err
	}
	started := time.Now()
	ctx, auditOn := c.beginAudit(ctx, p)
	resp, err := c.doRaw(ctx, p, "/chat/completions", requestBody, stream)
	if err != nil {
		c.writeStructuredAudit(ctx, auditOn, p, stream, requestBody, "", result, 0, "", false, false, err, started)
		return result, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := readAllBounded(resp.Body, maxHTTPErrorBytes)
		httpErr := &HTTPError{StatusCode: resp.StatusCode, Body: boundedHTTPErrorBody(raw)}
		c.writeStructuredAudit(ctx, auditOn, p, stream, requestBody, string(raw), result, resp.StatusCode, "", false, false, httpErr, started)
		return result, httpErr
	}
	if stream {
		raw := newCaptureBuffer(auditOn)
		result, resultErr = parseStructuredChatStream(io.TeeReader(resp.Body, raw), p.StructuredOutput, onDelta, auditCall(ctx).setUsage)
		resultErr = validateDeliveredPayload(p.StructuredOutput, result, resultErr)
		c.writeStructuredAudit(ctx, auditOn, p, stream, requestBody, raw.String(), result, resp.StatusCode, "", result.receivedDone, true, resultErr, started)
		return result, resultErr
	} else {
		var raw []byte
		raw, result, resultErr = parseStructuredChatComplete(resp.Body, p.StructuredOutput)
		resultErr = validateDeliveredPayload(p.StructuredOutput, result, resultErr)
		c.writeStructuredAudit(ctx, auditOn, p, stream, requestBody, string(raw), result, resp.StatusCode, "", false, true, resultErr, started)
		return result, resultErr
	}
}

func applyChatStructured(body map[string]any, output *StructuredOutput) {
	body["response_format"] = &ResponseFormat{Type: "json_schema", JSONSchema: output.Schema}
}

func parseStructuredChatComplete(reader io.Reader, output *StructuredOutput) ([]byte, StructuredResult, error) {
	raw, err := readAllBounded(reader, maxLLMResponseBytes)
	if err != nil {
		return raw, StructuredResult{}, err
	}
	var response structuredChatResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return raw, StructuredResult{}, err
	}
	if len(response.Choices) == 0 {
		return raw, StructuredResult{}, errors.New("模型未返回任何 choices")
	}
	choice := response.Choices[0]
	if choice.FinishReason == "length" {
		return raw, StructuredResult{}, ErrTruncated
	}
	result, err := validateStructuredDelivery(output, choice.Message.Content, choice.Message.ToolCalls)
	return raw, result, err
}

func parseStructuredChatStream(reader io.Reader, output *StructuredOutput, onDelta func(string), usage ...func(*agentTokenUsage)) (StructuredResult, error) {
	var text strings.Builder
	calls := map[int]*accumulatedToolCall{}
	finishReason := ""
	receivedDone := false
	buf := bufio.NewReader(reader)
	totalBytes := 0
	for {
		line, readErr := readSSELine(buf)
		totalBytes += len(line)
		if totalBytes > maxLLMResponseBytes {
			return StructuredResult{}, ErrLLMResponseTooLarge
		}
		if errors.Is(readErr, ErrLLMSSELineTooLarge) {
			return StructuredResult{}, readErr
		}
		data, ok := parseSSE(line)
		if !ok {
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				return StructuredResult{}, readErr
			}
			continue
		}
		if data == "[DONE]" {
			receivedDone = true
			break
		}
		var chunk structuredChatChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return StructuredResult{}, fmt.Errorf("流式 JSON 解析失败: %w", err)
		}
		for _, save := range usage {
			save(chunk.Usage)
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				if text.Len()+len(choice.Delta.Content) > maxLLMResponseBytes {
					return StructuredResult{}, ErrLLMResponseTooLarge
				}
				text.WriteString(choice.Delta.Content)
				if onDelta != nil {
					onDelta(choice.Delta.Content)
				}
			}
			for _, delta := range choice.Delta.ToolCalls {
				call := calls[delta.Index]
				if call == nil {
					call = &accumulatedToolCall{}
					calls[delta.Index] = call
				}
				if delta.Function.Name != "" {
					call.Name += delta.Function.Name
				}
				if delta.Function.Arguments != "" {
					if call.Arguments.Len()+len(delta.Function.Arguments) > maxAgentToolArguments {
						return StructuredResult{}, errors.New("模型工具参数超过 16 KiB")
					}
					call.Arguments.WriteString(delta.Function.Arguments)
				}
			}
			if choice.FinishReason != nil {
				finishReason = *choice.FinishReason
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return StructuredResult{}, readErr
		}
	}
	// 已有 finish_reason 的干净 EOF 视为完整;只有 finish_reason 也缺失才算截断。
	if !receivedDone && finishReason == "" {
		return StructuredResult{}, fmt.Errorf("%w: 流未收到终止事件", io.ErrUnexpectedEOF)
	}
	receivedDone = true
	if finishReason == "length" {
		return StructuredResult{receivedDone: true}, ErrTruncated
	}
	result, err := validateStructuredDelivery(output, text.String(), materializeCalls(calls))
	result.receivedDone = receivedDone
	return result, err
}

func materializeCalls(values map[int]*accumulatedToolCall) []chatToolCall {
	keys := make([]int, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Ints(keys)
	out := make([]chatToolCall, 0, len(keys))
	for _, key := range keys {
		var call chatToolCall
		call.Index = key
		call.Function.Name = values[key].Name
		call.Function.Arguments = values[key].Arguments.String()
		out = append(out, call)
	}
	return out
}

func validateStructuredDelivery(output *StructuredOutput, text string, calls []chatToolCall) (StructuredResult, error) {
	result := StructuredResult{Mode: output.Mode, ToolCalls: len(calls)}
	if strings.TrimSpace(text) != "" {
		result.OutputTypes = append(result.OutputTypes, "output_text")
	}
	if len(calls) > 0 {
		result.OutputTypes = append(result.OutputTypes, "function_call")
	}
	if len(calls) > 0 {
		return result, NewStructuredError("structured_output.invalid_payload", "严格 Schema 模式返回了工具调用")
	}
	if strings.TrimSpace(text) == "" {
		return result, NewStructuredError("structured_output.invalid_payload", "严格 Schema 模式未返回 payload")
	}
	result.Payload = text
	if err := requireJSONObject(text); err != nil {
		return result, NewStructuredError("structured_output.invalid_payload", "严格 Schema payload 不是单个完整 JSON 对象")
	}
	return result, nil
}

func validateDeliveredPayload(output *StructuredOutput, result StructuredResult, deliveryErr error) error {
	if deliveryErr != nil || output == nil || output.PayloadValidator == nil {
		return deliveryErr
	}
	if err := output.PayloadValidator(result.Payload); err != nil {
		return &StructuredError{Code: "structured_output.invalid_payload", Err: err}
	}
	return nil
}

func requireJSONObject(raw string) error {
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	var value map[string]json.RawMessage
	if err := decoder.Decode(&value); err != nil || value == nil {
		return errors.New("payload is not a JSON object")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("payload has trailing content")
	}
	return nil
}

func rejectDuplicateJSONKeys(raw string) error {
	decoder := json.NewDecoder(strings.NewReader(raw))
	var walk func() error
	walk = func() error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]struct{}{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok {
					return errors.New("JSON object key is not a string")
				}
				if _, exists := seen[key]; exists {
					return fmt.Errorf("duplicate JSON field %q", key)
				}
				seen[key] = struct{}{}
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		case '[':
			for decoder.More() {
				if err := walk(); err != nil {
					return err
				}
			}
			_, err = decoder.Token()
			return err
		default:
			return errors.New("invalid JSON delimiter")
		}
	}
	return walk()
}

func (c *Client) callStructuredResponses(ctx context.Context, p Params, messages []Message, stream bool, onDelta func(string)) (result StructuredResult, resultErr error) {
	body := map[string]any{
		"model": p.Model, "input": responsesInput(messages), "temperature": p.Temperature,
		"top_p": p.TopP, "stream": stream,
	}
	if p.MaxTokens > 0 {
		body["max_output_tokens"] = p.MaxTokens
	}
	applyResponsesStructured(body, p.StructuredOutput)
	requestBody, err := json.Marshal(body)
	if err != nil {
		return result, err
	}
	started := time.Now()
	ctx, auditOn := c.beginAudit(ctx, p)
	resp, err := c.doRaw(ctx, p, "/responses", requestBody, stream)
	if err != nil {
		c.writeStructuredAudit(ctx, auditOn, p, stream, requestBody, "", result, 0, "", false, false, err, started)
		return result, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := readAllBounded(resp.Body, maxHTTPErrorBytes)
		httpErr := &HTTPError{StatusCode: resp.StatusCode, Body: boundedHTTPErrorBody(raw)}
		c.writeStructuredAudit(ctx, auditOn, p, stream, requestBody, string(raw), result, resp.StatusCode, "", false, false, httpErr, started)
		return result, httpErr
	}
	var raw string
	if stream {
		streamed := newCaptureBuffer(auditOn)
		result, resultErr = parseStructuredResponsesStream(io.TeeReader(resp.Body, streamed), p.StructuredOutput, onDelta, auditCall(ctx).setUsage)
		resultErr = validateDeliveredPayload(p.StructuredOutput, result, resultErr)
		raw = streamed.String()
	} else {
		var rawBytes []byte
		rawBytes, result, resultErr = parseStructuredResponsesComplete(resp.Body, p.StructuredOutput)
		resultErr = validateDeliveredPayload(p.StructuredOutput, result, resultErr)
		raw = string(rawBytes)
	}
	c.writeStructuredAudit(ctx, auditOn, p, stream, requestBody, raw, result, resp.StatusCode, "", stream, true, resultErr, started)
	return result, resultErr
}

func applyResponsesStructured(body map[string]any, output *StructuredOutput) {
	body["text"] = responsesText(&ResponseFormat{Type: "json_schema", JSONSchema: output.Schema})
}

func parseStructuredResponsesComplete(reader io.Reader, output *StructuredOutput) ([]byte, StructuredResult, error) {
	raw, err := readAllBounded(reader, maxLLMResponseBytes)
	if err != nil {
		return raw, StructuredResult{}, err
	}
	var response structuredResponsesResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return raw, StructuredResult{}, err
	}
	if response.Status == "incomplete" && response.IncompleteDetails != nil &&
		(response.IncompleteDetails.Reason == "max_output_tokens" || response.IncompleteDetails.Reason == "length") {
		return raw, StructuredResult{}, ErrTruncated
	}
	if response.Status == "failed" || response.Status == "incomplete" {
		return raw, StructuredResult{}, fmt.Errorf("模型响应未完成: %s", response.Status)
	}
	text, calls := extractResponsesDelivery(response)
	result, err := validateStructuredDelivery(output, text, calls)
	return raw, result, err
}

func extractResponsesDelivery(response structuredResponsesResponse) (string, []chatToolCall) {
	var text strings.Builder
	hasOutputText := false
	if len(response.OutputText) > 0 && string(response.OutputText) != "null" {
		var value string
		if json.Unmarshal(response.OutputText, &value) == nil {
			text.WriteString(value)
			hasOutputText = value != ""
		}
	}
	var calls []chatToolCall
	for index, item := range response.Output {
		switch item.Type {
		case "function_call":
			var call chatToolCall
			call.Index = index
			call.Function.Name = item.Name
			call.Function.Arguments = item.Arguments
			calls = append(calls, call)
		case "message", "output_text":
			if hasOutputText {
				continue
			}
			if item.Text != "" {
				text.WriteString(item.Text)
			}
			for _, content := range item.Content {
				if content.Text != "" {
					text.WriteString(content.Text)
				}
			}
		}
	}
	return text.String(), calls
}

func parseStructuredResponsesStream(reader io.Reader, output *StructuredOutput, onDelta func(string), usage ...func(*agentTokenUsage)) (StructuredResult, error) {
	var text strings.Builder
	calls := map[int]*accumulatedToolCall{}
	receivedDone := false
	sawOutputDone := false
	buf := bufio.NewReader(reader)
	totalBytes := 0
	eventName := ""
	for {
		line, readErr := readSSELine(buf)
		totalBytes += len(line)
		if totalBytes > maxLLMResponseBytes {
			return StructuredResult{}, ErrLLMResponseTooLarge
		}
		if errors.Is(readErr, ErrLLMSSELineTooLarge) {
			return StructuredResult{}, readErr
		}
		if name := parseSSEEvent(line); name != "" {
			eventName = name
			if readErr == io.EOF {
				break
			}
			continue
		}
		data, ok := parseSSE(line)
		if !ok {
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				return StructuredResult{}, readErr
			}
			continue
		}
		if data == "[DONE]" {
			// Responses streams use an explicit response terminal event. A
			// compatibility [DONE] marker alone is not enough to establish that
			// the response was completed.
			continue
		}
		var event structuredResponsesEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return StructuredResult{}, fmt.Errorf("流式 JSON 解析失败: %w", err)
		}
		typ := event.Type
		if typ == "" {
			typ = eventName
		}
		if event.Response != nil {
			for _, save := range usage {
				save(event.Response.Usage)
			}
		}
		switch typ {
		case "response.output_text.delta":
			if err := appendStructuredText(&text, event.Delta, onDelta); err != nil {
				return StructuredResult{}, err
			}
		case "response.output_text.done":
			sawOutputDone = true
			value := event.Text
			if value == "" {
				value = event.Delta
			}
			if err := appendStructuredText(&text, value, onDelta); err != nil {
				return StructuredResult{}, err
			}
		case "response.output_item.added", "response.output_item.done":
			if event.Item != nil && event.Item.Type == "function_call" {
				call := calls[event.OutputIndex]
				if call == nil {
					call = &accumulatedToolCall{}
					calls[event.OutputIndex] = call
				}
				if call.Name == "" {
					call.Name = event.Item.Name
				}
				if call.Arguments.Len() == 0 {
					if len(event.Item.Arguments) > maxAgentToolArguments {
						return StructuredResult{}, errors.New("模型工具参数超过 16 KiB")
					}
					call.Arguments.WriteString(event.Item.Arguments)
				}
			} else if event.Item != nil && (event.Item.Type == "message" || event.Item.Type == "output_text") {
				if err := appendStructuredText(&text, structuredItemText(*event.Item), onDelta); err != nil {
					return StructuredResult{}, err
				}
			}
		case "response.function_call_arguments.delta":
			call := calls[event.OutputIndex]
			if call == nil {
				call = &accumulatedToolCall{}
				calls[event.OutputIndex] = call
			}
			if call.Arguments.Len()+len(event.Delta) > maxAgentToolArguments {
				return StructuredResult{}, errors.New("模型工具参数超过 16 KiB")
			}
			call.Arguments.WriteString(event.Delta)
		case "response.function_call_arguments.done":
			call := calls[event.OutputIndex]
			if call == nil {
				call = &accumulatedToolCall{}
				calls[event.OutputIndex] = call
			}
			if call.Arguments.Len() == 0 {
				if len(event.Arguments) > maxAgentToolArguments {
					return StructuredResult{}, errors.New("模型工具参数超过 16 KiB")
				}
				call.Arguments.WriteString(event.Arguments)
			}
		case "response.completed":
			receivedDone = true
			if event.Response != nil {
				finalText, finalCalls := extractResponsesDelivery(*event.Response)
				if err := appendStructuredText(&text, finalText, onDelta); err != nil {
					return StructuredResult{}, err
				}
				for _, finalCall := range finalCalls {
					call := calls[finalCall.Index]
					if call == nil {
						call = &accumulatedToolCall{}
						calls[finalCall.Index] = call
					}
					if call.Name == "" {
						call.Name = finalCall.Function.Name
					}
					if call.Arguments.Len() == 0 {
						if len(finalCall.Function.Arguments) > maxAgentToolArguments {
							return StructuredResult{}, errors.New("模型工具参数超过 16 KiB")
						}
						call.Arguments.WriteString(finalCall.Function.Arguments)
					}
				}
			}
		case "response.failed", "response.incomplete":
			receivedDone = true
			if typ == "response.incomplete" {
				reason := ""
				if event.Response != nil && event.Response.IncompleteDetails != nil {
					reason = event.Response.IncompleteDetails.Reason
				} else if event.IncompleteDetails != nil {
					reason = event.IncompleteDetails.Reason
				}
				if reason == "max_output_tokens" || reason == "length" {
					return StructuredResult{receivedDone: true}, ErrTruncated
				}
			}
			if event.Error != nil && event.Error.Message != "" {
				return StructuredResult{receivedDone: true}, &providerMessageError{message: event.Error.Message}
			}
			return StructuredResult{receivedDone: true}, errors.New("模型响应未完成")
		}
		eventName = ""
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return StructuredResult{}, readErr
		}
	}
	// 网关省略 response.completed 但已宣告文本输出完成时,干净 EOF 视为完整。
	if !receivedDone && !sawOutputDone {
		return StructuredResult{}, fmt.Errorf("%w: 流未收到终止事件", io.ErrUnexpectedEOF)
	}
	result, err := validateStructuredDelivery(output, text.String(), materializeCalls(calls))
	result.receivedDone = true
	return result, err
}

// appendStructuredText reconciles a terminal full-text projection with
// deltas already delivered by a Responses-compatible gateway. It emits only
// the missing suffix, so a terminal echo cannot duplicate streamed JSON.
func appendStructuredText(text *strings.Builder, value string, onDelta func(string)) error {
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
	if onDelta != nil {
		onDelta(value)
	}
	return nil
}

func structuredItemText(item structuredResponsesItem) string {
	var text strings.Builder
	if item.Text != "" {
		text.WriteString(item.Text)
	}
	for _, content := range item.Content {
		if content.Text != "" {
			text.WriteString(content.Text)
		}
	}
	return text.String()
}

func (c *Client) writeStructuredAudit(ctx context.Context, auditOn bool, p Params, stream bool, requestBody []byte, raw string, result StructuredResult, status int, finishReason string, done, transport bool, callErr error, started time.Time) {
	if c.auditWriter == nil {
		return
	}
	detail := RequestAudit{
		ConversationID: p.ConversationID, MessageID: p.MessageID, Purpose: p.Purpose,
		AttemptID: p.AttemptID, MemoryJobID: p.MemoryJobID, MemoryID: p.MemoryID,
		InquiryID: p.InquiryID, InquirySnapshotID: p.InquirySnapshotID,
		InquirySnapshotBranchEpoch: p.InquirySnapshotBranchEpoch, InquirySnapshotRevision: p.InquirySnapshotRevision,
		Model: p.Model, BaseURL: safeAuditURL(p.BaseURL), Stream: stream, RequestBody: requestBody,
		RawResponse: raw, ResponseText: result.Payload, AuditTruncated: false,
		ResponseStatus: status, FinishReason: finishReason,
		ReceivedDone: done, Success: transport && callErr == nil, TransportSuccess: transport, StructuredSuccess: callErr == nil,
		DurationMS: time.Since(started).Milliseconds(), SettingsRevision: p.SettingsRevision, SettingsSource: p.SettingsSource,
		OutputMode: result.Mode, ContractName: p.ContractName,
		ContractVersion: p.ContractVersion, ContractFingerprint: p.ContractFingerprint,
		OutputTypes: result.OutputTypes, StructuredErrorCode: StructuredErrorCode(callErr),
	}
	if p.StructuredOutput != nil && detail.OutputMode == "" {
		detail.OutputMode = p.StructuredOutput.Mode
	}
	c.finishAudit(ctx, auditOn, p, detail, callErr)
}
