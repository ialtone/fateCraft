package api

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/ialtone/fatecraft/internal/llmoutbound"
	"github.com/ialtone/fatecraft/internal/llmproxy"
	"github.com/ialtone/fatecraft/internal/llmsettings"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ialtone/fatecraft/internal/storage"
)

func TestAdminSystemSettingsJSONHidesLLMProxyFields(t *testing.T) {
	settings := storage.DefaultSystemSettings()
	settings.LLMProxyEnabled = true
	settings.LLMProxyURL = "http://secret-proxy.internal:8080"
	settings.LLMProxyUsername = "secret-proxy-user"
	settings.LLMProxyPasswordEncrypted = "secret-ciphertext"
	settings.LLMProxyCAPEM = "-----BEGIN CERTIFICATE-----\nsecret-ca\n-----END CERTIFICATE-----"
	raw, err := json.Marshal(&adminSystemSettingsResponse{SystemSettings: settings})
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, secret := range []string{
		"llm_proxy", "secret-proxy.internal", "secret-proxy-user", "secret-ciphertext", "secret-ca",
	} {
		if strings.Contains(body, secret) {
			t.Fatalf("general admin settings leaked %q: %s", secret, body)
		}
	}
}

func TestBothModelListEndpointsUseOutboundProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, admin := range []bool{false, true} {
		t.Run(map[bool]string{false: "player", true: "owner"}[admin], func(t *testing.T) {
			hits := 0
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits++
				if r.URL.Host != "provider.invalid" || r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer provider-key" {
					t.Errorf("unexpected provider request %s %s", r.Method, r.URL)
				}
				if r.Header.Get("Proxy-Authorization") == "" {
					t.Error("missing proxy auth")
				}
				_, _ = w.Write([]byte(`{"data":[{"id":"proxy-model"}]}`))
			}))
			defer proxy.Close()
			manager := llmoutbound.NewManager()
			snapshot, err := llmoutbound.Build(llmoutbound.Config{Enabled: true, URL: proxy.URL, Username: "proxy-user", Password: "proxy-password"}, 1)
			if err != nil {
				t.Fatal(err)
			}
			defer snapshot.Close()
			manager.Publish(snapshot)
			server := &Server{outbound: manager}
			handler := server.fetchModels
			if admin {
				server.llmSettings = llmsettings.New(&clientRouteStubUsers{}, &clientRouteStubSystem{}, "")
				handler = server.fetchAdminPublicModels
			}
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/models", strings.NewReader(`{"base_url":"http://provider.invalid/v1","api_key":"provider-key"}`))
			ctx.Request.Header.Set("Content-Type", "application/json")
			handler(ctx)
			if recorder.Code != 200 || hits != 1 || !strings.Contains(recorder.Body.String(), "proxy-model") {
				t.Fatalf("status=%d hits=%d body=%s", recorder.Code, hits, recorder.Body.String())
			}
		})
	}
}

func TestLLMProxyHandlersRequireOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, role := range []string{storage.UserRoleUser, storage.UserRoleAdmin, storage.UserRoleOwner} {
		for _, method := range []string{http.MethodGet, http.MethodPatch, http.MethodPost} {
			t.Run(role+method, func(t *testing.T) {
				server := &Server{}
				router := gin.New()
				handler := server.getAdminLLMProxy
				if method == http.MethodPatch {
					handler = server.updateAdminLLMProxy
				}
				if method == http.MethodPost {
					handler = server.testAdminLLMProxy
				}
				router.Handle(method, "/proxy", stubAuthUser(&storage.User{Role: role}), server.requireOwner, handler)
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, httptest.NewRequest(method, "/proxy", strings.NewReader(`{}`)))
				want := http.StatusForbidden
				if role == storage.UserRoleOwner {
					want = http.StatusServiceUnavailable
				}
				if recorder.Code != want {
					t.Fatalf("status=%d want=%d body=%s", recorder.Code, want, recorder.Body.String())
				}
			})
		}
	}
}

func TestProxyPatchRequiresExplicitRevision(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// Invalid requests must be rejected before touching storage.
	server := &Server{llmProxy: llmproxy.New(nil, llmoutbound.NewManager(), "")}
	for _, body := range []string{`{}`, `{"expected_revision":null}`, `{"expected_revision":-1}`} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodPatch, "/proxy", strings.NewReader(body))
		ctx.Request.Header.Set("Content-Type", "application/json")
		server.updateAdminLLMProxy(ctx)
		if recorder.Code != 400 {
			t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
}
