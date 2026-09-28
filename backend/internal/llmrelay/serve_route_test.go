package llmrelay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/storage"
)

// TestServeRejectsNonClientRoute 开关关闭时有效路由为 server，握手必须被拒（403）。
func TestServeRejectsNonClientRoute(t *testing.T) {
	for _, route := range []string{storage.LLMRouteServer, ""} {
		registry := NewRegistry(func(context.Context, string) (string, error) {
			return route, nil
		}, nil)
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/ws/llm-relay", nil)
		registry.Serve(recorder, req, uuid.New())
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("route %q: status = %d, want 403", route, recorder.Code)
		}
	}
}

// TestServeAllowsClientRoute 有效路由为 client 时握手不被路由准入拒绝：
// platform_managed + 开关开启 + 用户选 client 同样允许连接（v2 7.1）。
// httptest 下 WebSocket 升级本身不会成功，断言重点是“没有 403”。
func TestServeAllowsClientRoute(t *testing.T) {
	registry := NewRegistry(func(context.Context, string) (string, error) {
		return storage.LLMRouteClient, nil
	}, nil)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ws/llm-relay", nil)
	registry.Serve(recorder, req, uuid.New())
	if recorder.Code == http.StatusForbidden {
		t.Fatalf("client route was rejected with 403, want handshake to proceed")
	}
}
