package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/logging"
)

const auditRequestLimit = 512 << 10
const auditResponseLimit = 512 << 10
const auditTextLimit = 256 << 10
const auditRecordLimit = 2 << 20

type AuditFieldInfo struct {
	OriginalBytes int64 `json:"original_bytes"`
	StoredBytes   int   `json:"stored_bytes"`
	Truncated     bool  `json:"truncated"`
	Invalid       bool  `json:"invalid"`
}

type callAuditKey struct{}
type callAudit struct {
	id            string
	started       time.Time
	attempts      atomic.Int32
	responseBytes atomic.Int64
	usage         *TokenUsage
}

func (c *Client) beginAudit(ctx context.Context, p Params) (context.Context, bool) {
	if logging.TraceID(ctx) == "" {
		ctx = logging.NewTrace(ctx)
	}
	call := &callAudit{id: uuid.NewString(), started: time.Now()}
	ctx = context.WithValue(ctx, callAuditKey{}, call)
	return ctx, c.shouldAudit(ctx, p)
}

func auditCall(ctx context.Context) *callAudit {
	value, _ := ctx.Value(callAuditKey{}).(*callAudit)
	return value
}

func (call *callAudit) setUsage(usage *agentTokenUsage) {
	if call == nil || usage == nil {
		return
	}
	value := usage.normalized()
	if value.InputTokens < 0 || value.OutputTokens < 0 {
		return
	}
	call.usage = &value
}

type auditCountingBody struct {
	io.ReadCloser
	call *callAudit
}

func (b *auditCountingBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.call.responseBytes.Add(int64(n))
	return n, err
}

func (c *Client) finishAudit(ctx context.Context, enabled bool, p Params, detail RequestAudit, callErr error) {
	if c.auditWriter == nil {
		return
	}
	detail.TraceID, detail.Protocol = logging.TraceID(ctx), string(p.Protocol)
	detail.ID, detail.StartedAt = uuid.NewString(), time.Now().Add(-time.Duration(detail.DurationMS)*time.Millisecond)
	if call := auditCall(ctx); call != nil {
		if !detail.Stream && detail.ResponseStatus == 200 {
			var response struct {
				Usage *agentTokenUsage `json:"usage"`
			}
			if json.Unmarshal([]byte(detail.RawResponse), &response) == nil {
				call.setUsage(response.Usage)
			}
		}
		detail.ID, detail.StartedAt, detail.TransportAttempts = call.id, call.started, int(call.attempts.Load())
		if call.usage != nil {
			input, output := call.usage.InputTokens, call.usage.OutputTokens
			detail.InputTokens, detail.OutputTokens = &input, &output
		}
	}
	detail.Error = redactAuditText(logging.ErrorText(callErr), p.APIKey)
	for field, limit := range map[*string]int{&detail.Model: 128, &detail.BaseURL: 512, &detail.FinishReason: 64, &detail.ContractName: 64, &detail.ContractVersion: 32, &detail.ContractFingerprint: 64} {
		*field = logging.SafePrefix(redactAuditText(*field, p.APIKey), limit)
	}
	if callErr != nil {
		detail.ErrorCode = "model_error"
		var httpErr *HTTPError
		switch {
		case errors.Is(callErr, context.Canceled):
			detail.ErrorCode = "cancelled"
		case errors.Is(callErr, context.DeadlineExceeded):
			detail.ErrorCode = "timeout"
		case errors.As(callErr, &httpErr):
			detail.ErrorCode = "upstream_http_error"
		case detail.StructuredErrorCode != "":
			detail.ErrorCode = detail.StructuredErrorCode
		}
	}
	detail.BodyStatus = "disabled"
	detail.BodyMetadata = map[string]AuditFieldInfo{}
	if enabled {
		detail.BodyStatus = "captured"
		var requestInfo, rawInfo, textInfo AuditFieldInfo
		detail.RequestBody, requestInfo = safeAuditJSON(detail.RequestBody, p.APIKey)
		detail.RawResponse, rawInfo = safeAuditText(detail.RawResponse, p.APIKey, auditResponseLimit)
		detail.ResponseText, textInfo = safeAuditText(detail.ResponseText, p.APIKey, auditTextLimit)
		if call := auditCall(ctx); call != nil && call.responseBytes.Load() > rawInfo.OriginalBytes {
			rawInfo.OriginalBytes = call.responseBytes.Load()
			rawInfo.Truncated = true
		}
		detail.BodyMetadata = map[string]AuditFieldInfo{"request": requestInfo, "raw_response": rawInfo, "response_text": textInfo}
		for _, info := range detail.BodyMetadata {
			detail.AuditTruncated = detail.AuditTruncated || info.Truncated
			if info.Truncated && detail.BodyStatus != "invalid" {
				detail.BodyStatus = "truncated"
			}
			if info.Invalid {
				detail.BodyStatus = "invalid"
			}
		}
	} else {
		detail.RequestBody, detail.RawResponse, detail.ResponseText = []byte(`{}`), "", ""
		detail.AuditTruncated = false
	}
	// Keep failure reporting in the bounded writer; never replay a model request.
	_ = c.auditWriter(context.WithoutCancel(ctx), detail)
}

func redactAuditText(value, secret string) string {
	if secret != "" {
		value = strings.ReplaceAll(value, secret, "[redacted]")
	}
	return logging.SafeText(value)
}

func safeAuditText(value, secret string, limit int) (string, AuditFieldInfo) {
	info := AuditFieldInfo{OriginalBytes: int64(len(value))}
	info.Invalid = strings.ToValidUTF8(value, "�") != value || strings.ContainsRune(value, 0)
	clean, invalid := redactJSONText(value, secret)
	info.Invalid = info.Invalid || invalid
	info.Truncated = len(clean) > limit
	clean = logging.SafePrefix(clean, limit)
	info.StoredBytes = len(clean)
	return clean, info
}

// Decode JSON before filtering, so escaped credential keys/values cannot bypass
// redaction. SSE frames are treated individually without changing game parsing.
func redactJSONText(value, secret string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") && trimmed != "[DONE]" {
		var decoded any
		decoder := json.NewDecoder(strings.NewReader(value))
		decoder.UseNumber()
		if !json.Valid([]byte(value)) || decoder.Decode(&decoded) != nil {
			return "[invalid or incomplete JSON omitted]", true
		}
		invalid := invalidJSONText(decoded)
		encoded, _ := json.Marshal(redactValue(decoded, secret))
		return string(encoded), invalid
	}
	if strings.HasPrefix(trimmed, "data:") || strings.Contains(value, "\ndata:") {
		lines := strings.Split(value, "\n")
		invalid := false
		for i, line := range lines {
			if strings.HasPrefix(line, "data:") {
				payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				clean, bad := redactAuditText(payload, secret), false
				if strings.HasPrefix(payload, "{") || strings.HasPrefix(payload, "[") {
					clean, bad = redactJSONText(payload, secret)
				}
				lines[i] = "data: " + clean
				invalid = invalid || bad
			} else {
				lines[i] = redactAuditText(line, secret)
			}
		}
		return strings.Join(lines, "\n"), invalid
	}
	return redactAuditText(value, secret), false
}

func safeAuditJSON(value []byte, secret string) ([]byte, AuditFieldInfo) {
	info := AuditFieldInfo{OriginalBytes: int64(len(value)), Invalid: !utf8.Valid(value) || bytes.ContainsRune(value, 0)}
	var object map[string]any
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	valid := json.Valid(value) && decoder.Decode(&object) == nil && object != nil
	preview := "[invalid JSON omitted]"
	if valid {
		info.Invalid = info.Invalid || invalidJSONText(object)
		redactObject(object, secret)
		encoded, err := json.Marshal(object)
		if err == nil && len(encoded) <= auditRequestLimit {
			info.StoredBytes = len(encoded)
			return encoded, info
		}
		preview = string(encoded)
	}
	info.Invalid = info.Invalid || !valid
	preview = logging.SafePrefix(preview, auditRequestLimit/2)
	for {
		encoded, _ := json.Marshal(map[string]any{"_diagnostic": map[string]any{"preview": preview, "original_bytes": len(value), "invalid": info.Invalid, "truncated": true}})
		if len(encoded) <= auditRequestLimit {
			info.StoredBytes = len(encoded)
			info.Truncated = true
			return encoded, info
		}
		preview = logging.SafePrefix(preview, len(preview)/2)
	}
}

func invalidJSONText(value any) bool {
	switch v := value.(type) {
	case string:
		return !utf8.ValidString(v) || strings.ContainsRune(v, 0)
	case map[string]any:
		for key, item := range v {
			if invalidJSONText(key) || invalidJSONText(item) {
				return true
			}
		}
	case []any:
		for _, item := range v {
			if invalidJSONText(item) {
				return true
			}
		}
	}
	return false
}

func redactObject(object map[string]any, secret string) {
	for key, value := range object {
		cleanKey := redactAuditText(key, secret)
		if cleanKey != key {
			delete(object, key)
		}
		normalized := strings.ToLower(strings.ReplaceAll(key, "-", "_"))
		switch normalized {
		case "authorization", "cookie", "password", "api_key", "apikey", "client_secret", "access_token", "refresh_token", "token", "secret", "code", "state":
			object[cleanKey] = "[redacted]"
		default:
			object[cleanKey] = redactValue(value, secret)
		}
	}
}
func redactValue(value any, secret string) any {
	switch typed := value.(type) {
	case string:
		return redactAuditText(typed, secret)
	case map[string]any:
		redactObject(typed, secret)
	case []any:
		for i := range typed {
			typed[i] = redactValue(typed[i], secret)
		}
	}
	return value
}
