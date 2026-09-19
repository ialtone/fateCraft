package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequestBodyLimitSupportsExactRouteOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RequestBodyLimit(16, BodyLimitOverride{
		Method: http.MethodPut, Path: "/large/:id", MaxBytes: 32,
	}))
	read := func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.Status(http.StatusRequestEntityTooLarge)
			return
		}
		c.String(http.StatusOK, "%d", len(body))
	}
	router.POST("/ordinary", read)
	router.PUT("/large/:id", read)

	ordinary := httptest.NewRecorder()
	router.ServeHTTP(ordinary, httptest.NewRequest(http.MethodPost, "/ordinary", strings.NewReader(strings.Repeat("x", 20))))
	if ordinary.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("ordinary status=%d body=%s", ordinary.Code, ordinary.Body.String())
	}

	large := httptest.NewRecorder()
	router.ServeHTTP(large, httptest.NewRequest(http.MethodPut, "/large/1", strings.NewReader(strings.Repeat("x", 20))))
	if large.Code != http.StatusOK || large.Body.String() != "20" {
		t.Fatalf("override status=%d body=%s", large.Code, large.Body.String())
	}
}
