package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/ialtone/fatecraft/internal/identity"
	"github.com/ialtone/fatecraft/internal/llm"
	"github.com/ialtone/fatecraft/internal/llmsettings"
	"github.com/ialtone/fatecraft/internal/modelcapability"
	"github.com/ialtone/fatecraft/internal/storage"
	"github.com/ialtone/fatecraft/internal/testdb"
)

// clientRouteStubUsers / clientRouteStubSystem 实现 llmsettings 的仓储接口
// （接口方法全导出，跨包实现合法），让管理与用户接口测试无需数据库。
type clientRouteStubUsers struct{ stored *storage.LLMSettings }

func (s *clientRouteStubUsers) Get(context.Context, string) (*storage.LLMSettings, error) {
	v := *s.stored
	return &v, nil
}

func (s *clientRouteStubUsers) Save(_ context.Context, _ string, next *storage.LLMSettings) error {
	v := *next
	s.stored = &v
	return nil
}

type clientRouteStubSystem struct{ stored *storage.SystemSettings }

func (s *clientRouteStubSystem) Get(context.Context) (*storage.SystemSettings, error) {
	v := *s.stored
	return &v, nil
}

func (s *clientRouteStubSystem) Save(_ context.Context, next *storage.SystemSettings) error {
	v := *next
	s.stored = &v
	return nil
}

func validClientRouteUserSettings() *storage.LLMSettings {
	return &storage.LLMSettings{
		PlayerID: "player-1", BaseURL: "https://user.example/v1",
		Protocol: "responses", Model: "user-model", MaxContextTokens: 128000,
		MaxResponseTokens: 4096, Temperature: 0.8, TopP: 1,
		LLMRoute: storage.LLMRouteServer,
	}
}

func defaultClientRouteSystem() *storage.SystemSettings {
	settings := storage.DefaultSystemSettings()
	settings.ClientLLMRouteEnabled = false
	return settings
}

// fakeRelayRegistry 断言管理操作是否触发 Relay 断开。
type fakeRelayRegistry struct{ closeAllCalls int }

func (f *fakeRelayRegistry) CloseAll()                                           { f.closeAllCalls++ }
func (f *fakeRelayRegistry) ClosePlayer(string)                                  {}
func (f *fakeRelayRegistry) Serve(http.ResponseWriter, *http.Request, uuid.UUID) {}

func TestUpdateAdminSystemSettingsRejectsClientRouteSwitchForNonOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPatch, "/api/admin/settings",
		strings.NewReader(`{"client_llm_route_enabled":true}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set(authUserKey, &storage.User{Role: storage.UserRoleAdmin})

	server.updateAdminSystemSettings(ctx)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
}

func TestUpdateLLMSettingsRejectsClientRouteWhenDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	users := &clientRouteStubUsers{stored: validClientRouteUserSettings()}
	system := &clientRouteStubSystem{stored: defaultClientRouteSystem()}
	server := &Server{
		llmSettings: llmsettings.New(users, system, ""),
		identity:    identity.NewContext(),
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPatch, "/api/settings/llm",
		strings.NewReader(`{"llm_route":"client"}`))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req.WithContext(identity.WithPlayerID(req.Context(), "player-1"))

	server.updateLLMSettings(ctx)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "llm_client_route_disabled") {
		t.Fatalf("body %q does not carry llm_client_route_disabled code", body)
	}
	if users.stored.LLMRoute != storage.LLMRouteServer {
		t.Fatalf("stored route was rewritten to %q", users.stored.LLMRoute)
	}
}

func platformOpenClientRouteSystem() *storage.SystemSettings {
	settings := storage.DefaultSystemSettings()
	settings.LLMAccessMode = llmsettings.AccessModePlatformManaged
	settings.ClientLLMRouteEnabled = true
	settings.PublicLLMBaseURL = "https://platform.example/v1"
	settings.PublicLLMModel = "platform-model"
	return settings
}

// TestUpdateLLMSettingsPlatformServerKeepsPersonal 覆盖 7.4：platform_managed +
// 开 + server 只提交 llm_route 时成功（个人配置为空也不触发校验），且用户行
// 里不会出现平台 base_url 或模型。
func TestUpdateLLMSettingsPlatformServerKeepsPersonal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	users := &clientRouteStubUsers{stored: &storage.LLMSettings{
		PlayerID: "player-1", BaseURL: "https://user.example/v1",
		Protocol: "responses", Model: "user-model", MaxContextTokens: 128000,
		MaxResponseTokens: 4096, Temperature: 0.8, TopP: 1,
		LLMRoute: storage.LLMRouteClient, APIKeyEncrypted: "enc-old",
	}}
	system := &clientRouteStubSystem{stored: platformOpenClientRouteSystem()}
	server := &Server{
		llmSettings: llmsettings.New(users, system, "01234567890123456789012345678901"),
		identity:    identity.NewContext(),
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPatch, "/api/settings/llm",
		strings.NewReader(`{"llm_route":"server"}`))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req.WithContext(identity.WithPlayerID(req.Context(), "player-1"))

	server.updateLLMSettings(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if users.stored.LLMRoute != storage.LLMRouteServer {
		t.Fatalf("stored route = %q, want server", users.stored.LLMRoute)
	}
	// 个人配置字段与 Key 原样保留，请求体未带配置也不引入平台值。
	if users.stored.BaseURL != "https://user.example/v1" || users.stored.Model != "user-model" {
		t.Fatalf("personal config was overwritten: %+v", users.stored)
	}
	if users.stored.APIKeyEncrypted != "enc-old" {
		t.Fatalf("server key was not preserved: %q", users.stored.APIKeyEncrypted)
	}
}

// TestUpdateLLMSettingsPlatformClientSavesPersonal 覆盖 7.4：platform_managed +
// 开 + client 保存个人配置，合并基准是个人配置（用户行不会混入平台值）。
func TestUpdateLLMSettingsPlatformClientSavesPersonal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	users := &clientRouteStubUsers{stored: validClientRouteUserSettings()}
	system := &clientRouteStubSystem{stored: platformOpenClientRouteSystem()}
	server := &Server{
		llmSettings: llmsettings.New(users, system, "01234567890123456789012345678901"),
		identity:    identity.NewContext(),
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPatch, "/api/settings/llm",
		strings.NewReader(`{"llm_route":"client","base_url":"https://user.example/v1","model":"user-model","max_context_tokens":128000,"max_response_tokens":4096}`))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req.WithContext(identity.WithPlayerID(req.Context(), "player-1"))

	server.updateLLMSettings(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if users.stored.LLMRoute != storage.LLMRouteClient {
		t.Fatalf("stored route = %q, want client", users.stored.LLMRoute)
	}
	if users.stored.BaseURL == "https://platform.example/v1" || users.stored.Model == "platform-model" {
		t.Fatalf("platform config leaked into user row: %+v", users.stored)
	}
	if users.stored.BaseURL != "https://user.example/v1" || users.stored.Model != "user-model" {
		t.Fatalf("personal config was not saved: %+v", users.stored)
	}
}

// openClientRouteTestDB 在独立 schema 上运行最新 migration，与 storage 包的
// PostgreSQL 测试隔离。无 FATECRAFT_TEST_DATABASE_URL 时跳过。
func openClientRouteTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testdb.Open(t, "client_route_verify")
}

func TestUpdateAdminSystemSettingsClosesRelaysWhenClientRouteDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openClientRouteTestDB(t)
	ctx := context.Background()
	systemRepo := storage.NewSystemSettingsRepo(db)
	// 开关先打开，再由接口关闭，断言 CloseAll 与持久化。
	open := storage.DefaultSystemSettings()
	open.ClientLLMRouteEnabled = true
	if err := systemRepo.Save(ctx, open); err != nil {
		t.Fatalf("seed system settings: %v", err)
	}
	relays := &fakeRelayRegistry{}
	server := &Server{system: systemRepo, llmSettings: llmsettings.New(nil, systemRepo, ""), relays: relays}

	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = httptest.NewRequest(http.MethodPatch, "/api/admin/settings",
		strings.NewReader(`{"client_llm_route_enabled":false}`))
	ginCtx.Request.Header.Set("Content-Type", "application/json")
	owner := &storage.User{ID: uuid.New(), Username: "route-owner", DisplayName: "Owner", Role: storage.UserRoleOwner, Status: storage.UserStatusActive}
	if err := db.Create(owner).Error; err != nil {
		t.Fatal(err)
	}
	ginCtx.Set(authUserKey, owner)

	server.updateAdminSystemSettings(ginCtx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if relays.closeAllCalls != 1 {
		t.Fatalf("CloseAll calls = %d, want 1", relays.closeAllCalls)
	}
	persisted, err := systemRepo.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.ClientLLMRouteEnabled {
		t.Fatal("client_llm_route_enabled was not persisted as false")
	}
	if !strings.Contains(recorder.Body.String(), `"client_llm_route_enabled":false`) {
		t.Fatalf("response does not carry the switch: %s", recorder.Body.String())
	}
}

// TestUpdateAdminSystemSettingsAccessModeSwitchKeepsRelays 覆盖 7.4：切换公共
// LLM 不改变任何用户的有效路由，因此不调用 CloseAll，只有关闭开关时调用。
func TestUpdateAdminSystemSettingsAccessModeSwitchKeepsRelays(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openClientRouteTestDB(t)
	ctx := context.Background()
	systemRepo := storage.NewSystemSettingsRepo(db)
	seed := storage.DefaultSystemSettings()
	seed.LLMAccessMode = llmsettings.AccessModePlatformManaged
	seed.ClientLLMRouteEnabled = true
	seed.PublicLLMBaseURL = "https://platform.example/v1"
	seed.PublicLLMProtocol = "responses"
	seed.PublicLLMModel = "platform-model"
	seed.PublicLLMAPIKeyEncrypted = "enc-seed"
	seed.PublicLLMMaxContextTokens = 128000
	seed.PublicLLMMaxResponseTokens = 16384
	if err := systemRepo.Save(ctx, seed); err != nil {
		t.Fatalf("seed system settings: %v", err)
	}
	relays := &fakeRelayRegistry{}
	server := &Server{system: systemRepo, llmSettings: llmsettings.New(nil, systemRepo, ""), relays: relays}

	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = httptest.NewRequest(http.MethodPatch, "/api/admin/settings",
		strings.NewReader(`{"llm_access_mode":"user_managed"}`))
	ginCtx.Request.Header.Set("Content-Type", "application/json")
	owner := &storage.User{ID: uuid.New(), Username: "route-owner", DisplayName: "Owner", Role: storage.UserRoleOwner, Status: storage.UserStatusActive}
	if err := db.Create(owner).Error; err != nil {
		t.Fatal(err)
	}
	ginCtx.Set(authUserKey, owner)

	server.updateAdminSystemSettings(ginCtx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if relays.closeAllCalls != 0 {
		t.Fatalf("CloseAll calls = %d, want 0 (access mode switch must not drop relays)", relays.closeAllCalls)
	}
}

// stubAuthUser 模拟 requireAuth：注入用户与玩家身份，供 owner 门禁测试使用。
func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func stubAuthUser(user *storage.User) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set(authUserKey, user)
		c.Request = c.Request.WithContext(identity.WithPlayerID(c.Request.Context(), user.ID.String()))
		c.Next()
	}
}

// TestCheckAdminPublicCapabilityUsesPlatformConfig 覆盖 7.4：owner 本人在
// platform_managed 下选了客户端直连时，公共配置诊断仍使用平台配置、不经 Relay。
func TestCheckAdminPublicCapabilityUsesPlatformConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	personalHits := 0
	personalSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		personalHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer personalSrv.Close()
	platformSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"platform stub"}}`))
	}))
	defer platformSrv.Close()

	ownerID := uuid.New()
	users := &clientRouteStubUsers{stored: &storage.LLMSettings{
		PlayerID: ownerID.String(), BaseURL: personalSrv.URL,
		Protocol: "responses", Model: "user-model", MaxContextTokens: 128000,
		MaxResponseTokens: 4096, TopP: 1, LLMRoute: storage.LLMRouteClient,
	}}
	system := &clientRouteStubSystem{stored: &storage.SystemSettings{
		ID: 1, LLMAccessMode: llmsettings.AccessModePlatformManaged, ClientLLMRouteEnabled: true,
		PublicLLMBaseURL: platformSrv.URL, PublicLLMProtocol: "responses",
		PublicLLMModel: "platform-model", PublicLLMMaxContextTokens: 128000,
		PublicLLMMaxResponseTokens: 4096, PublicLLMTopP: 1, PublicLLMSettingsRevision: 7,
	}}
	svc := llmsettings.New(users, system, "01234567890123456789012345678901")
	platformKey := "platform-secret"
	if _, err := svc.UpdatePlatform(context.Background(), system.stored, &platformKey, false); err != nil {
		t.Fatalf("seed platform key: %v", err)
	}
	server := &Server{
		capabilities: modelcapability.New(llm.NewClient(), ""),
		llmSettings:  svc,
		identity:     identity.NewContext(),
	}
	engine := gin.New()
	engine.POST("/t", stubAuthUser(&storage.User{ID: ownerID, Role: storage.UserRoleOwner}),
		server.requireOwner, server.checkAdminPublicCapability)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/t", strings.NewReader(`{"role":"memory"}`))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	// 诊断用的是平台模型（个人配置的 user-model 会暴露复用用户接口的问题）。
	if !strings.Contains(body, `"model":"platform-model"`) {
		t.Fatalf("diagnostic did not use platform config: %s", body)
	}
	// UpdatePlatform 在写入平台 Key 时递增了公共 revision，断言携带的是当前值。
	if want := `"settings_revision":` + itoa(system.stored.PublicLLMSettingsRevision); !strings.Contains(body, want) {
		t.Fatalf("diagnostic did not carry public revision %s: %s", want, body)
	}
	if personalHits != 0 {
		t.Fatalf("personal endpoint was hit %d times, want 0 (no relay/personal use)", personalHits)
	}
}

func TestCheckAdminPublicCapabilityRejectsNonOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := &Server{
		capabilities: modelcapability.New(llm.NewClient(), ""),
		llmSettings:  llmsettings.New(&clientRouteStubUsers{stored: validClientRouteUserSettings()}, &clientRouteStubSystem{stored: defaultClientRouteSystem()}, ""),
		identity:     identity.NewContext(),
	}
	engine := gin.New()
	engine.POST("/t", stubAuthUser(&storage.User{ID: uuid.New(), Role: storage.UserRoleAdmin}),
		server.requireOwner, server.checkAdminPublicCapability)

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/t", strings.NewReader(`{"role":"memory"}`))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
}

// TestGetLLMSettingsExposesResolvedProfile 覆盖文档 8.2.3：GET /settings/llm
// 返回的 resolved_profile 只含档位 id（协议完全由用户选择），且响应不再含 provider。
func TestGetLLMSettingsExposesResolvedProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stored := validClientRouteUserSettings()
	stored.BaseURL = "https://api.deepseek.com"
	users := &clientRouteStubUsers{stored: stored}
	system := &clientRouteStubSystem{stored: defaultClientRouteSystem()}
	server := &Server{
		llmSettings: llmsettings.New(users, system, "01234567890123456789012345678901"),
		identity:    identity.NewContext(),
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodGet, "/api/settings/llm", nil)
	ctx.Request = req.WithContext(identity.WithPlayerID(req.Context(), "player-1"))

	server.getLLMSettings(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	want := `"resolved_profile":{"id":"deepseek"}`
	if !strings.Contains(body, want) {
		t.Fatalf("body %s does not carry %s", body, want)
	}
	if strings.Contains(body, `"provider"`) {
		t.Fatalf("body %s still carries provider field", body)
	}
}

// TestUpdateLLMSettingsIgnoresUnknownField 覆盖文档 1.4：请求体带已删除的
// provider 字段时保存成功，该字段被忽略（Gin 忽略未知字段）。
func TestUpdateLLMSettingsIgnoresUnknownField(t *testing.T) {
	gin.SetMode(gin.TestMode)
	users := &clientRouteStubUsers{stored: validClientRouteUserSettings()}
	system := &clientRouteStubSystem{stored: defaultClientRouteSystem()}
	server := &Server{
		llmSettings: llmsettings.New(users, system, "01234567890123456789012345678901"),
		identity:    identity.NewContext(),
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPatch, "/api/settings/llm",
		strings.NewReader(`{"provider":"deepseek","base_url":"https://api.deepseek.com","api_key":"test-secret"}`))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req.WithContext(identity.WithPlayerID(req.Context(), "player-1"))

	server.updateLLMSettings(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if users.stored.BaseURL != "https://api.deepseek.com" {
		t.Fatalf("stored base_url = %q, want deepseek endpoint", users.stored.BaseURL)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"resolved_profile":{"id":"deepseek"`) {
		t.Fatalf("body %s does not carry deepseek resolved_profile", body)
	}
	if strings.Contains(body, `"provider"`) {
		t.Fatalf("body %s still carries provider field", body)
	}
}
