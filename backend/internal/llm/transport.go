package llm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/ialtone/fatecraft/internal/logging"
)

// transport owns HTTP construction, provider status errors, audit decoration
// and SSE framing. Protocol adapters only build payloads and parse events.
func (c *Client) doRaw(ctx context.Context, p Params, endpoint string, body []byte, stream bool) (*http.Response, error) {
	if p.Relay == nil && p.APIKey == "" {
		return nil, fmt.Errorf("未配置 LLM API Key")
	}
	requestURL := strings.TrimRight(p.BaseURL, "/") + endpoint
	policy := effectiveRetryPolicy(p.RetryPolicy)
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		resp, err := c.roundTrip(ctx, p, requestURL, endpoint, body, stream)
		if err == nil && !retryableStatus(resp.StatusCode) {
			// 响应头已到达;后续读取由空闲看门狗与调用方 ctx 共同约束,
			// 不再有覆盖整个流的固定总超时。
			resp.Body = newIdleBody(resp.Body, streamIdleTimeout)
			return resp, nil
		}
		if attempt == policy.MaxAttempts || !retryableTransportError(err, resp) {
			if err != nil {
				return nil, err
			}
			if resp != nil {
				resp.Body = newIdleBody(resp.Body, streamIdleTimeout)
			}
			return resp, nil
		}

		// A retryable status body belongs to this failed transport attempt. It
		// must be consumed and closed before the next request so connections can
		// be reused; the final attempt is returned untouched to the caller.
		retryAfter := ""
		if resp != nil {
			retryAfter = resp.Header.Get("Retry-After")
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		logging.Named("llm").Warn("模型请求将重试",
			zap.String("model", p.Model), zap.String("provider", p.Provider),
			zap.String("endpoint", endpoint), zap.Int("attempt", attempt),
			zap.Int("max_attempts", policy.MaxAttempts), zap.String("reason", retryReason(err, resp)),
		)
		if err := waitRetry(ctx, policy, attempt, retryAfter); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("模型请求重试耗尽")
}

// roundTrip 执行一次 HTTP 调用：Relay 为空时直连，
// 非空时把已构建的请求交给浏览器执行并返回原始响应。
// Relay 模式下不设置 Authorization（APIKey 本来为空）。
func (c *Client) roundTrip(ctx context.Context, p Params, requestURL, endpoint string, body []byte, stream bool) (*http.Response, error) {
	if p.Relay != nil {
		var conversationID uuid.UUID
		if parsed, err := uuid.Parse(p.ConversationID); err == nil {
			conversationID = parsed
		}
		return p.Relay.Do(ctx, RelayRequest{
			Endpoint: endpoint, Body: body, Stream: stream,
			Purpose: p.Purpose, ConversationID: conversationID,
		})
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	return c.http.Do(req)
}

func effectiveRetryPolicy(policy *RetryPolicy) RetryPolicy {
	if policy == nil {
		return RetryPolicy{MaxAttempts: 3, BaseDelay: 250 * time.Millisecond, MaxDelay: 4 * time.Second}
	}
	result := *policy
	if result.MaxAttempts <= 0 {
		result.MaxAttempts = 1
	}
	if result.BaseDelay < 0 {
		result.BaseDelay = 0
	}
	if result.MaxDelay <= 0 {
		result.MaxDelay = 4 * time.Second
	}
	if result.MaxDelay < result.BaseDelay {
		result.MaxDelay = result.BaseDelay
	}
	return result
}

func retryableStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func retryableTransportError(err error, resp *http.Response) bool {
	if resp != nil {
		return retryableStatus(resp.StatusCode)
	}
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	// Relay 错误分类：只有“无可用连接”可重试；head 之后断开、浏览器网络失败、
	// 本设备无 Key 都不可重试（重试也只会得到相同结果）。
	if errors.Is(err, ErrRelayUnavailable) {
		return true
	}
	if errors.Is(err, ErrRelayLost) || errors.Is(err, ErrRelayNetwork) || errors.Is(err, ErrRelayNoKey) {
		return false
	}
	return true
}

func retryReason(err error, resp *http.Response) string {
	if resp != nil {
		return resp.Status
	}
	if err != nil {
		return err.Error()
	}
	return "temporary_failure"
}

func waitRetry(ctx context.Context, policy RetryPolicy, attempt int, retryAfter string) error {
	delay := retryAfterDelay(retryAfter)
	if delay <= 0 {
		delay = policy.BaseDelay
		for i := 1; i < attempt; i++ {
			if delay >= policy.MaxDelay/2 {
				delay = policy.MaxDelay
				break
			}
			delay *= 2
		}
		if delay > policy.MaxDelay {
			delay = policy.MaxDelay
		}
		if !policy.DisableJitter && delay > 0 {
			// A bounded deterministic component avoids synchronized retries while
			// keeping tests and operator logs reproducible enough to reason about.
			jitter := time.Duration(time.Now().UnixNano() % int64(delay/4+1))
			delay += jitter
			if delay > policy.MaxDelay {
				delay = policy.MaxDelay
			}
		}
	}
	if delay > policy.MaxDelay {
		delay = policy.MaxDelay
	}
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func retryAfterDelay(value string) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		if delay := time.Until(when); delay > 0 {
			return delay
		}
	}
	return 0
}

func (c *Client) shouldAudit(ctx context.Context) bool {
	return c.auditWriter != nil && (c.auditEnabled == nil || c.auditEnabled(ctx))
}

func (c *Client) writeAudit(ctx context.Context, auditOn bool, p Params, stream bool, requestBody []byte, rawResponse, responseText string, status int, finishReason string, receivedDone, success bool, callErr error, started time.Time) {
	if !auditOn || c.auditWriter == nil {
		return
	}
	requestBody, requestTruncated := boundedAuditBytes(requestBody)
	rawResponse, rawTruncated := boundedAuditString(rawResponse)
	responseText, textTruncated := boundedAuditString(responseText)
	detail := RequestAudit{
		ConversationID: p.ConversationID, MessageID: p.MessageID,
		Purpose: p.Purpose, AttemptID: p.AttemptID, MemoryJobID: p.MemoryJobID, MemoryID: p.MemoryID,
		InquiryID: p.InquiryID, InquirySnapshotID: p.InquirySnapshotID,
		InquirySnapshotBranchEpoch: p.InquirySnapshotBranchEpoch, InquirySnapshotRevision: p.InquirySnapshotRevision,
		Model: p.Model, BaseURL: safeAuditURL(p.BaseURL), Stream: stream,
		RequestBody: requestBody, RawResponse: rawResponse, ResponseText: responseText,
		AuditTruncated: requestTruncated || rawTruncated || textTruncated,
		ResponseStatus: status, FinishReason: finishReason, ReceivedDone: receivedDone,
		Success: success, DurationMS: time.Since(started).Milliseconds(),
		SettingsRevision: p.SettingsRevision, SettingsSource: p.SettingsSource,
		// HTTP transport succeeded once a valid 2xx response was received;
		// parser/contract errors are represented separately by Success/Error.
		TransportSuccess: status >= http.StatusOK && status < http.StatusMultipleChoices,
	}
	if callErr != nil {
		detail.Error = callErr.Error()
	}
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := c.auditWriter(auditCtx, detail); err != nil {
		logging.Named("llm").Warn("详细模型请求日志写入失败", zap.Error(err))
	}
}

func safeAuditURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func boundedHTTPErrorBody(body []byte) string {
	const maxErrorBytes = 4096
	if len(body) > maxErrorBytes {
		body = body[:maxErrorBytes]
	}
	return strings.TrimSpace(string(body))
}

// parseSSE parses one SSE line and returns its data field.
func parseSSE(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, ":") {
		return "", false
	}
	if strings.HasPrefix(line, "data:") {
		return strings.TrimSpace(strings.TrimPrefix(line, "data:")), true
	}
	return "", false
}

func parseSSEEvent(line string) string {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "event:") {
		return strings.TrimSpace(strings.TrimPrefix(line, "event:"))
	}
	return ""
}
