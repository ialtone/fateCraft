package logging

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lib/pq"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var urlPattern = regexp.MustCompile(`(?i)\b(?:https?|postgres(?:ql)?)://[^\s<>"']+`)
var credentialPattern = regexp.MustCompile(`(?i)(password|passwd|api[_-]?key|authorization|cookie|code|state|token|secret)(["']?\s*[:=]\s*["']?)[^\s,"';&]+`)
var quotedCredentialPattern = regexp.MustCompile(`(?i)(password|passwd|api[_-]?key|authorization|cookie|code|state|token|secret)(["']?\s*[:=]\s*)(?:"(?:[^"\\]|\\.)*"|'[^']*')`)
var bearerPattern = regexp.MustCompile(`(?i)\bBearer\s+[^\s,"']+`)
var sharePattern = regexp.MustCompile(`/(?:s|shared|api/public/conversation-shares)/[^\s/?#"']+`)

func SafeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "[invalid-url]"
	}
	u.User, u.RawQuery, u.Fragment = nil, "", ""
	u.Path = safeRequestPath(u.Path)
	u.RawPath = ""
	return u.String()
}

func SafeText(value string) string {
	value = strings.ReplaceAll(strings.ToValidUTF8(value, "�"), "\x00", "�")
	value = urlPattern.ReplaceAllStringFunc(value, SafeURL)
	value = sharePattern.ReplaceAllStringFunc(value, safeRequestPath)
	value = bearerPattern.ReplaceAllString(value, "Bearer [redacted]")
	value = quotedCredentialPattern.ReplaceAllString(value, "${1}${2}\"[redacted]\"")
	return credentialPattern.ReplaceAllString(value, "${1}${2}[redacted]")
}

func SafePrefix(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	value = strings.ReplaceAll(strings.ToValidUTF8(value, "�"), "\x00", "�")
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

// ErrorText never serializes database detail values or provider response bodies.
func ErrorText(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "context canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "context deadline exceeded"
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return "postgres " + pg.Code + " constraint=" + pg.ConstraintName
	}
	var legacy *pq.Error
	if errors.As(err, &legacy) {
		return "postgres " + string(legacy.Code) + " constraint=" + legacy.Constraint
	}
	var safe interface{ SafeLogMessage() string }
	if errors.As(err, &safe) {
		return safe.SafeLogMessage()
	}
	var u *url.Error
	if errors.As(err, &u) {
		return u.Op + " " + SafeURL(u.URL) + ": " + ErrorText(u.Err)
	}
	return SafePrefix(SafeText(err.Error()), 2048)
}

type safeCore struct{ zapcore.Core }

func (c safeCore) With(fields []zap.Field) zapcore.Core {
	return safeCore{c.Core.With(safeFields(fields))}
}
func (c safeCore) Check(entry zapcore.Entry, checked *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(entry.Level) {
		return checked.AddCore(entry, c)
	}
	return checked
}
func (c safeCore) Write(entry zapcore.Entry, fields []zap.Field) error {
	entry.Message = SafeText(entry.Message)
	return c.Core.Write(entry, safeFields(fields))
}
func safeFields(fields []zap.Field) []zap.Field {
	out := append([]zap.Field(nil), fields...)
	for i, field := range out {
		switch {
		case sensitiveKey(field.Key):
			out[i] = zap.String(field.Key, "[redacted]")
		case field.Type == zapcore.ErrorType:
			if err, ok := field.Interface.(error); ok {
				out[i] = zap.String(field.Key, ErrorText(err))
			}
		case field.Key == "panic":
			out[i] = zap.String("panic_type", fmt.Sprintf("%T", field.Interface))
		case field.Type == zapcore.StringType && field.Key != "stack":
			out[i] = zap.String(field.Key, SafeText(field.String))
		}
	}
	return out
}

func sensitiveKey(key string) bool {
	switch strings.ToLower(strings.ReplaceAll(key, "-", "_")) {
	case "authorization", "cookie", "password", "passwd", "api_key", "apikey", "code", "state", "token", "access_token", "refresh_token", "client_secret", "secret":
		return true
	}
	return false
}
