package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	shareapp "github.com/ialtone/fatecraft/internal/share"
	"github.com/ialtone/fatecraft/internal/storage"
)

// 公开举报的身份只取 ClientIP：不信任任何代理（TRUSTED_PROXIES 为空）时，
// 更换 User-Agent 或伪造 X-Forwarded-For 都不能换出新的限流桶。
func TestPublicShareReportIdentityIgnoresSpoofableHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	if err := router.SetTrustedProxies(nil); err != nil {
		t.Fatal(err)
	}
	server := &Server{shares: shareapp.New(storage.NewShareRepository(nil), strings.Repeat("k", 32))}
	router.POST("/api/public/conversation-shares/:token/reports", server.reportPublicConversationShare)

	// 每个来源每小时最多 5 次举报；前 5 次因 token 无效返回 404，第 6 次必须被限流。
	for i := 0; i < 6; i++ {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/public/conversation-shares/invalid-token/reports", strings.NewReader(`{"reason":"abuse"}`))
		request.Header.Set("Content-Type", "application/json")
		request.RemoteAddr = "203.0.113.7:40000"
		request.Header.Set("User-Agent", fmt.Sprintf("spoofed-agent-%d", i))
		request.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i))
		request.Header.Set("X-Real-IP", fmt.Sprintf("192.0.2.%d", i))
		router.ServeHTTP(recorder, request)
		want := http.StatusNotFound
		if i == 5 {
			want = http.StatusTooManyRequests
		}
		if recorder.Code != want {
			t.Fatalf("request %d: status = %d, want %d; body=%s", i+1, recorder.Code, want, recorder.Body.String())
		}
	}
}
