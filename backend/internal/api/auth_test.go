package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ialtone/fatecraft/internal/storage"
)

func TestRequireAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		role       string
		wantStatus int
	}{
		{name: "普通用户被拒绝", role: storage.UserRoleUser, wantStatus: http.StatusForbidden},
		{name: "管理员可以继续", role: storage.UserRoleAdmin, wantStatus: http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			server := &Server{}
			router.POST("/resource",
				func(c *gin.Context) {
					c.Set(authUserKey, &storage.User{Role: tt.role})
					c.Next()
				},
				server.requireAdmin,
				func(c *gin.Context) { c.Status(http.StatusOK) },
			)

			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/resource", nil)
			router.ServeHTTP(response, request)
			if response.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", response.Code, tt.wantStatus, response.Body.String())
			}
		})
	}
}
