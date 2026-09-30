package logging

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestPrivateErrorsAndFields(t *testing.T) {
	secret := "synthetic-secret"
	for _, err := range []error{
		fmt.Errorf("save: %w", &pgconn.PgError{Code: "23505", ConstraintName: "unique_email", Message: secret, Detail: secret}),
		&url.Error{Op: "Post", URL: "https://user:" + secret + "@example.test/shared/" + secret + "?code=" + secret, Err: errors.New("connection refused")},
		errors.New(`password="synthetic-secret has spaces" authorization=Bearer synthetic-secret`),
	} {
		if got := ErrorText(err); strings.Contains(got, secret) {
			t.Fatalf("error leaked: %s", got)
		}
	}
	core, logs := observer.New(zapcore.DebugLevel)
	log := zap.New(safeCore{core}).With(zap.String("api_key", secret))
	log.Error("failure", zap.String("url", "https://user:"+secret+"@example.test?code="+secret), zap.Any("panic", secret))
	if got := fmt.Sprint(logs.All()[0].ContextMap()); strings.Contains(got, secret) {
		t.Fatalf("fields leaked: %s", got)
	}
}

func TestHTTPPrivacyTraceAndLevels(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	old := global.Swap(zap.New(safeCore{core}))
	t.Cleanup(func() { global.Store(old) })
	r := gin.New()
	r.Use(GinLogger(), GinRecovery())
	r.GET("/api/health", func(c *gin.Context) { c.Status(200) })
	r.GET("/api/auth/callback", func(c *gin.Context) { c.Status(400) })
	r.GET("/shared/:token", func(c *gin.Context) { panic("synthetic-secret") })
	r.GET("/forbidden", func(c *gin.Context) { c.Status(403) })
	for _, path := range []string{"/api/health", "/api/auth/callback?code=synthetic-secret&email=private@example.test", "/shared/synthetic-secret", "/unknown/synthetic-secret?token=synthetic-secret", "/forbidden"} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("X-Request-ID", "synthetic-secret")
		request.Header.Set("Authorization", "Bearer synthetic-secret")
		response := httptest.NewRecorder()
		r.ServeHTTP(response, request)
		trace := response.Header().Get("X-Request-ID")
		if _, err := uuid.Parse(trace); err != nil {
			t.Fatalf("trace = %q", trace)
		}
		if response.Code == 500 && !strings.Contains(response.Body.String(), trace) {
			t.Fatal("panic response lacks trace")
		}
	}
	for _, entry := range logs.All() {
		encoded := fmt.Sprint(entry.ContextMap())
		if strings.Contains(encoded, "synthetic-secret") || strings.Contains(encoded, "private@example.test") {
			t.Fatalf("HTTP leak: %s", encoded)
		}
		if entry.ContextMap()["path"] == "/api/health" {
			t.Fatal("successful health probe was logged")
		}
		if entry.ContextMap()["status"] == int64(400) && entry.Level != zapcore.InfoLevel {
			t.Fatal("ordinary rejection is too noisy")
		}
		if entry.ContextMap()["status"] == int64(403) && entry.Level != zapcore.WarnLevel {
			t.Fatal("security rejection not visible")
		}
	}
}

func TestSQLTracePreservesCorrelation(t *testing.T) {
	g, logs := observedGormLogger()
	ctx := WithFields(NewTrace(context.Background()), zap.String("attempt_id", "attempt"))
	g.Trace(ctx, time.Now(), func() (string, int64) { return "SELECT $1", 1 }, nil)
	fields := logs.All()[0].ContextMap()
	if fields["trace_id"] != TraceID(ctx) || fields["attempt_id"] != "attempt" {
		t.Fatalf("missing correlation: %v", fields)
	}
}
