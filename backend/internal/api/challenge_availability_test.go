package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestChallengeAvailabilityRejectsMissingOrInvalidBoolean(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, body := range []string{`{}`, `{"enabled":null}`, `{"enabled":"false"}`, `{"enabled":0}`} {
		t.Run(body, func(t *testing.T) {
			writer := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(writer)
			ctx.Params = gin.Params{{Key: "id", Value: uuid.NewString()}}
			ctx.Request = httptest.NewRequest(http.MethodPut, "/api/admin/challenges/test/enabled", strings.NewReader(body))
			ctx.Request.Header.Set("Content-Type", "application/json")
			// A malformed toggle must stop before accessing the service/storage.
			(&Server{}).setAdminChallengeEnabled(ctx)
			if writer.Code != http.StatusBadRequest {
				t.Fatalf("status %d for %s", writer.Code, body)
			}
		})
	}
}
