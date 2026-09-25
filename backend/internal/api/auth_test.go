package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"

	authservice "github.com/ialtone/fatecraft/internal/auth"
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
		{name: "拥有者可以继续", role: storage.UserRoleOwner, wantStatus: http.StatusOK},
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

// 首次安装只保留空库创建 owner 的 /setup/initialize，已删除的兼容接口不能再注册。
func TestRouterDoesNotExposeSetupComplete(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	(&Server{}).Router(router)
	var hasInitialize bool
	for _, route := range router.Routes() {
		if route.Path == "/api/setup/complete" {
			t.Fatalf("legacy route is still registered: %s %s", route.Method, route.Path)
		}
		if route.Method == http.MethodPost && route.Path == "/api/setup/initialize" {
			hasInitialize = true
		}
	}
	if !hasInitialize {
		t.Fatal("setup initialize route is missing")
	}
}

func TestHandleAdminMutationErrorStatuses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		err        error
		wantStatus int
	}{
		{err: storage.ErrOwnerRequired, wantStatus: http.StatusForbidden},
		{err: storage.ErrLastOwner, wantStatus: http.StatusBadRequest},
		{err: storage.ErrTargetUserDisabled, wantStatus: http.StatusBadRequest},
		{err: storage.ErrInvalidUserRole, wantStatus: http.StatusBadRequest},
		{err: fmt.Errorf("包装: %w", storage.ErrNotFound), wantStatus: http.StatusNotFound},
	} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPatch, "/api/admin/users/x/role", nil)
		(&Server{}).handleAdminMutationError(ctx, test.err)
		if recorder.Code != test.wantStatus {
			t.Errorf("%v: status = %d, want %d", test.err, recorder.Code, test.wantStatus)
		}
	}
}

func TestAuthFailMapsAccountErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		err        error
		wantStatus int
	}{
		{err: storage.ErrUsernameInUse, wantStatus: http.StatusConflict},
		{err: storage.ErrEmailInUse, wantStatus: http.StatusConflict},
		{err: authservice.ErrEmailInUse, wantStatus: http.StatusConflict},
		{err: fmt.Errorf("%w：不能包含 @", authservice.ErrInvalidUsername), wantStatus: http.StatusBadRequest},
		{err: storage.ErrUserNotBindable, wantStatus: http.StatusBadRequest},
		{err: storage.ErrIdentityAlreadyBound, wantStatus: http.StatusBadRequest},
		{err: authservice.ErrInvalidCredentials, wantStatus: http.StatusUnauthorized},
	} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/auth/register", nil)
		authFail(ctx, test.err)
		if recorder.Code != test.wantStatus {
			t.Errorf("%v: status = %d, want %d", test.err, recorder.Code, test.wantStatus)
		}
	}
}

func TestReturnToWithQuery(t *testing.T) {
	for _, test := range []struct {
		returnTo string
		key      string
		value    string
		wantPath string
		want     url.Values
	}{
		{returnTo: "/settings", key: linuxDOBindQuery, value: "ok", wantPath: "/settings", want: url.Values{linuxDOBindQuery: {"ok"}}},
		{
			returnTo: "/settings?tab=identity&linuxdo_bind=ok", key: linuxDOBindErrorQuery, value: "已取消 LinuxDo 授权",
			wantPath: "/settings", want: url.Values{"tab": {"identity"}, linuxDOBindErrorQuery: {"已取消 LinuxDo 授权"}},
		},
		// 回跳地址异常时回落到站内首页，不能被带到外站。
		{returnTo: "//evil.example/x", key: linuxDOBindQuery, value: "ok", wantPath: "/", want: url.Values{linuxDOBindQuery: {"ok"}}},
		{returnTo: "https://evil.example/x", key: linuxDOBindQuery, value: "ok", wantPath: "/", want: url.Values{linuxDOBindQuery: {"ok"}}},
	} {
		got, err := url.Parse(returnToWithQuery(test.returnTo, test.key, test.value))
		if err != nil {
			t.Fatal(err)
		}
		if got.Scheme != "" || got.Host != "" || got.Path != test.wantPath || got.Query().Encode() != test.want.Encode() {
			t.Errorf("returnToWithQuery(%q) = %q, want path %q query %q", test.returnTo, got.String(), test.wantPath, test.want.Encode())
		}
	}
}
