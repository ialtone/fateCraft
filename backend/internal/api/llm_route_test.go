package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ialtone/fatecraft/internal/storage"
)

func TestFailMapsLLMRouteMismatchToConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, err := range []error{
		storage.ErrLLMRouteMismatch,
		fmt.Errorf("生成前检查: %w", storage.ErrLLMRouteMismatch),
	} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/conversations/x/messages", nil)
		fail(ctx, err)
		if recorder.Code != http.StatusConflict {
			t.Fatalf("%v: status = %d, want %d", err, recorder.Code, http.StatusConflict)
		}
		if body := recorder.Body.String(); body == "" || !strings.Contains(body, "llm_route_mismatch") {
			t.Fatalf("%v: body %q does not carry llm_route_mismatch code", err, body)
		}
	}
}
