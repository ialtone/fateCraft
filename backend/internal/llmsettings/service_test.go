package llmsettings

import (
	"context"
	"errors"
	"testing"

	"github.com/ialtone/fatecraft/internal/storage"
)

type fakeUsers struct{ settings *storage.LLMSettings }

func (f *fakeUsers) Get(context.Context, string) (*storage.LLMSettings, error) {
	if f.settings == nil {
		return &storage.LLMSettings{PlayerID: "player-1", BaseURL: "https://user.example/v1", Protocol: "responses", Model: "user-model", MaxContextTokens: 128000, MaxResponseTokens: 16384}, nil
	}
	v := *f.settings
	return &v, nil
}
func (f *fakeUsers) Save(_ context.Context, _ string, s *storage.LLMSettings) error {
	v := *s
	f.settings = &v
	return nil
}

type fakeSystem struct{ settings *storage.SystemSettings }

func (f *fakeSystem) Get(context.Context) (*storage.SystemSettings, error) {
	if f.settings == nil {
		return storage.DefaultSystemSettings(), nil
	}
	v := *f.settings
	return &v, nil
}
func (f *fakeSystem) Save(_ context.Context, s *storage.SystemSettings) error {
	v := *s
	f.settings = &v
	return nil
}

func TestUserKeyIsEncryptedAndNeverReturned(t *testing.T) {
	users := &fakeUsers{}
	system := &fakeSystem{}
	svc := New(users, system, "01234567890123456789012345678901")
	key := "user-secret"
	input := &storage.LLMSettings{BaseURL: "https://user.example/v1", Protocol: "responses", Model: "user-model", MaxContextTokens: 128000, MaxResponseTokens: 16384, TopP: 1}
	view, err := svc.UpdateUser(context.Background(), "player-1", input, &key, storage.LLMRouteServer)
	if err != nil {
		t.Fatal(err)
	}
	if view.HasAPIKey == false || view.APIKey != "" {
		t.Fatalf("unexpected user view: %+v", view)
	}
	if users.settings.APIKeyEncrypted == "" || users.settings.APIKeyEncrypted == key {
		t.Fatalf("key was not encrypted")
	}
	runtime, err := svc.Get(context.Background(), "player-1")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.APIKey != key {
		t.Fatalf("runtime key = %q, want %q", runtime.APIKey, key)
	}
}

func TestPlatformModeUsesGlobalKeyAndBlocksUserUpdate(t *testing.T) {
	users := &fakeUsers{}
	system := &fakeSystem{settings: &storage.SystemSettings{
		ID: 1, LLMAccessMode: AccessModePlatformManaged, PublicLLMBaseURL: "https://platform.example/v1",
		PublicLLMProtocol: "responses", PublicLLMModel: "platform-model", PublicLLMMaxContextTokens: 128000,
		PublicLLMMaxResponseTokens: 16384, PublicLLMTopP: 1, PublicLLMSettingsRevision: 1,
	}}
	svc := New(users, system, "01234567890123456789012345678901")
	key := "platform-secret"
	if _, err := svc.UpdatePlatform(context.Background(), system.settings, &key, false); err != nil {
		t.Fatal(err)
	}
	runtime, err := svc.Get(context.Background(), "player-1")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Source != SourcePlatform || runtime.APIKey != key || runtime.Editable {
		t.Fatalf("unexpected platform settings: %+v", runtime)
	}
	_, err = svc.UpdateUser(context.Background(), "player-1", &storage.LLMSettings{}, &key, storage.LLMRouteServer)
	if err != ErrUserSettingsDisabled {
		t.Fatalf("UpdateUser error = %v, want %v", err, ErrUserSettingsDisabled)
	}
}

func TestEffectiveRouteDefaultsToServer(t *testing.T) {
	svc := New(&fakeUsers{}, &fakeSystem{}, "01234567890123456789012345678901")
	route, err := svc.EffectiveRoute(context.Background(), "player-1")
	if err != nil {
		t.Fatal(err)
	}
	if route != storage.LLMRouteServer {
		t.Fatalf("effective route = %q, want server", route)
	}
}

func TestPlatformModeEffectiveRouteIsServer(t *testing.T) {
	// v2：有效路由只由开关和用户保存值决定（7.1 表格），接入模式不再强制 server。
	off := &storage.SystemSettings{LLMAccessMode: AccessModePlatformManaged, ClientLLMRouteEnabled: false}
	on := &storage.SystemSettings{LLMAccessMode: AccessModePlatformManaged, ClientLLMRouteEnabled: true}
	users := &fakeUsers{settings: &storage.LLMSettings{PlayerID: "player-1", LLMRoute: storage.LLMRouteClient}}
	for _, test := range []struct {
		name   string
		system *storage.SystemSettings
		want   string
	}{
		{"platform closed ignores stored client", off, storage.LLMRouteServer},
		{"platform open keeps stored client", on, storage.LLMRouteClient},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc := New(users, &fakeSystem{settings: test.system}, "01234567890123456789012345678901")
			route, err := svc.EffectiveRoute(context.Background(), "player-1")
			if err != nil {
				t.Fatal(err)
			}
			if route != test.want {
				t.Fatalf("effective route = %q, want %q", route, test.want)
			}
		})
	}
}

// systemWithClientRoute 返回带平台开关取值的全局设置（默认关闭，需 owner 开启）。
func systemWithClientRoute(enabled bool) *storage.SystemSettings {
	settings := storage.DefaultSystemSettings()
	settings.ClientLLMRouteEnabled = enabled
	return settings
}

func TestSwitchToClientClearsServerKey(t *testing.T) {
	users := &fakeUsers{}
	system := &fakeSystem{settings: systemWithClientRoute(true)}
	svc := New(users, system, "01234567890123456789012345678901")
	key := "user-secret"
	input := &storage.LLMSettings{BaseURL: "https://user.example/v1", Protocol: "responses", Model: "user-model", MaxContextTokens: 128000, MaxResponseTokens: 16384, TopP: 1}
	if _, err := svc.UpdateUser(context.Background(), "player-1", input, &key, storage.LLMRouteServer); err != nil {
		t.Fatal(err)
	}
	if users.settings.APIKeyEncrypted == "" {
		t.Fatal("server key was not saved")
	}
	// 切到 client：忽略传入 Key，清除服务端 Key，不报错。
	other := "should-be-ignored"
	view, err := svc.UpdateUser(context.Background(), "player-1", &storage.LLMSettings{BaseURL: "https://user.example/v1"}, &other, storage.LLMRouteClient)
	if err != nil {
		t.Fatal(err)
	}
	if users.settings.APIKeyEncrypted != "" {
		t.Fatalf("server key was not cleared: %q", users.settings.APIKeyEncrypted)
	}
	if view.LLMRoute != storage.LLMRouteClient {
		t.Fatalf("view route = %q, want client", view.LLMRoute)
	}
	runtime, err := svc.Get(context.Background(), "player-1")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.APIKey != "" || runtime.Source != SourceClient || runtime.LLMRoute != storage.LLMRouteClient {
		t.Fatalf("unexpected client runtime: %+v", runtime)
	}
	route, err := svc.EffectiveRoute(context.Background(), "player-1")
	if err != nil {
		t.Fatal(err)
	}
	if route != storage.LLMRouteClient {
		t.Fatalf("effective route = %q, want client", route)
	}
}

func TestUpdateUserRejectsInvalidRoute(t *testing.T) {
	svc := New(&fakeUsers{}, &fakeSystem{}, "01234567890123456789012345678901")
	key := "user-secret"
	if _, err := svc.UpdateUser(context.Background(), "player-1", &storage.LLMSettings{}, &key, "relay"); err != ErrInvalidLLMRoute {
		t.Fatalf("UpdateUser error = %v, want %v", err, ErrInvalidLLMRoute)
	}
}

// TestResolveRouteCoversAdminSwitchTable 覆盖重构文档 7.1 表格的全部 6 行。
func TestResolveRouteCoversAdminSwitchTable(t *testing.T) {
	platformOff := &storage.SystemSettings{LLMAccessMode: AccessModePlatformManaged, ClientLLMRouteEnabled: false}
	platformOn := &storage.SystemSettings{LLMAccessMode: AccessModePlatformManaged, ClientLLMRouteEnabled: true}
	userOff := &storage.SystemSettings{LLMAccessMode: AccessModeUserManaged, ClientLLMRouteEnabled: false}
	userOn := &storage.SystemSettings{LLMAccessMode: AccessModeUserManaged, ClientLLMRouteEnabled: true}
	for _, test := range []struct {
		name   string
		global *storage.SystemSettings
		stored string
		want   string
	}{
		{"platform closed ignores stored client", platformOff, storage.LLMRouteClient, storage.LLMRouteServer},
		{"platform closed ignores stored server", platformOff, storage.LLMRouteServer, storage.LLMRouteServer},
		{"platform open keeps stored server", platformOn, storage.LLMRouteServer, storage.LLMRouteServer},
		{"platform open keeps stored client", platformOn, storage.LLMRouteClient, storage.LLMRouteClient},
		{"user closed ignores stored client", userOff, storage.LLMRouteClient, storage.LLMRouteServer},
		{"user closed ignores stored server", userOff, storage.LLMRouteServer, storage.LLMRouteServer},
		{"user open keeps stored server", userOn, storage.LLMRouteServer, storage.LLMRouteServer},
		{"user open keeps stored client", userOn, storage.LLMRouteClient, storage.LLMRouteClient},
		{"open defaults empty to server", userOn, "", storage.LLMRouteServer},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := resolveRoute(test.global, test.stored); got != test.want {
				t.Fatalf("resolveRoute = %q, want %q", got, test.want)
			}
		})
	}
}

func TestUpdateUserRejectsClientRouteWhenDisabled(t *testing.T) {
	users := &fakeUsers{}
	system := &fakeSystem{settings: systemWithClientRoute(false)}
	svc := New(users, system, "01234567890123456789012345678901")
	other := "should-be-rejected"
	if _, err := svc.UpdateUser(context.Background(), "player-1",
		&storage.LLMSettings{BaseURL: "https://user.example/v1"}, &other, storage.LLMRouteClient); !errors.Is(err, ErrClientRouteDisabled) {
		t.Fatalf("UpdateUser error = %v, want ErrClientRouteDisabled", err)
	}
}

// TestStoredClientIgnoredWhenDisabledAndRestoredWhenEnabled 验证：关闭开关不改写
// 用户保存值（只是忽略），重新开启后自动恢复，无需批量写库。
func TestStoredClientIgnoredWhenDisabledAndRestoredWhenEnabled(t *testing.T) {
	users := &fakeUsers{settings: &storage.LLMSettings{
		PlayerID: "player-1", BaseURL: "https://user.example/v1", Protocol: "responses",
		Model: "user-model", MaxContextTokens: 128000, MaxResponseTokens: 16384,
		LLMRoute: storage.LLMRouteClient,
	}}
	system := &fakeSystem{settings: systemWithClientRoute(false)}
	svc := New(users, system, "01234567890123456789012345678901")

	runtime, err := svc.Get(context.Background(), "player-1")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.LLMRoute != storage.LLMRouteServer || runtime.HasAPIKey || runtime.ClientRouteEnabled {
		t.Fatalf("disabled runtime = %+v, want server/no-key/disabled", runtime)
	}
	if route, err := svc.EffectiveRoute(context.Background(), "player-1"); err != nil || route != storage.LLMRouteServer {
		t.Fatalf("disabled effective route = %q err = %v, want server", route, err)
	}
	// 用户保存值未被改写。
	if users.settings.LLMRoute != storage.LLMRouteClient {
		t.Fatalf("stored route was rewritten to %q, want client kept", users.settings.LLMRoute)
	}

	// 重新开启：自动恢复为 client。
	system.settings.ClientLLMRouteEnabled = true
	runtime, err = svc.Get(context.Background(), "player-1")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.LLMRoute != storage.LLMRouteClient || !runtime.ClientRouteEnabled {
		t.Fatalf("restored runtime = %+v, want client/enabled", runtime)
	}
	if route, err := svc.EffectiveRoute(context.Background(), "player-1"); err != nil || route != storage.LLMRouteClient {
		t.Fatalf("restored effective route = %q err = %v, want client", route, err)
	}
}

func TestPlatformViewDisablesClientRoute(t *testing.T) {
	// v2：UserView 基于 Personal，platform_managed 下返回用户个人配置，不含
	// 平台 base_url 与模型；Editable 表示个人配置是否可编辑。
	system := &fakeSystem{settings: &storage.SystemSettings{
		ID: 1, LLMAccessMode: AccessModePlatformManaged, ClientLLMRouteEnabled: true,
		PublicLLMBaseURL:  "https://platform.example/v1",
		PublicLLMProtocol: "responses", PublicLLMModel: "platform-model", PublicLLMMaxContextTokens: 128000,
		PublicLLMMaxResponseTokens: 16384, PublicLLMTopP: 1, PublicLLMSettingsRevision: 1,
	}}
	users := &fakeUsers{settings: &storage.LLMSettings{
		PlayerID: "player-1", BaseURL: "https://user.example/v1", Protocol: "responses",
		Model: "user-model", MaxContextTokens: 128000, MaxResponseTokens: 16384,
		LLMRoute: storage.LLMRouteServer,
	}}
	svc := New(users, system, "01234567890123456789012345678901")
	view, err := svc.UserView(context.Background(), "player-1")
	if err != nil {
		t.Fatal(err)
	}
	if view.BaseURL != "https://user.example/v1" || view.Model != "user-model" {
		t.Fatalf("platform view leaks platform config: %+v", view)
	}
	if view.LLMRoute != storage.LLMRouteServer || view.Source != SourcePlatform || !view.Editable || !view.ClientRouteEnabled {
		t.Fatalf("platform open view = %+v, want server/platform/editable/enabled", view)
	}
	// 开关关闭：个人配置不可编辑。
	system.settings.ClientLLMRouteEnabled = false
	view, err = svc.UserView(context.Background(), "player-1")
	if err != nil {
		t.Fatal(err)
	}
	if view.Editable || view.ClientRouteEnabled {
		t.Fatalf("platform closed view = %+v, want not-editable/disabled", view)
	}
}

// TestGetPlatformClientRouteReturnsPersonal 覆盖 7.4：platform_managed + 开 +
// client 返回个人配置，Source=client，AccessMode 保持 platform_managed；平台
// 配置不完整也不报错。
func TestGetPlatformClientRouteReturnsPersonal(t *testing.T) {
	system := &fakeSystem{settings: &storage.SystemSettings{
		ID: 1, LLMAccessMode: AccessModePlatformManaged, ClientLLMRouteEnabled: true,
	}}
	users := &fakeUsers{settings: &storage.LLMSettings{
		PlayerID: "player-1", BaseURL: "https://user.example/v1", Protocol: "responses",
		Model: "user-model", MaxContextTokens: 128000, MaxResponseTokens: 16384,
		LLMRoute: storage.LLMRouteClient,
	}}
	svc := New(users, system, "01234567890123456789012345678901")
	runtime, err := svc.Get(context.Background(), "player-1")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Source != SourceClient || runtime.AccessMode != AccessModePlatformManaged {
		t.Fatalf("runtime source/access = %q/%q, want client/platform_managed", runtime.Source, runtime.AccessMode)
	}
	if runtime.BaseURL != "https://user.example/v1" || runtime.Model != "user-model" {
		t.Fatalf("runtime is not personal config: %+v", runtime)
	}
	if runtime.APIKey != "" || runtime.HasAPIKey {
		t.Fatalf("client runtime must not carry a key: %+v", runtime)
	}
	// 同组合的 server 返回平台配置。
	users.settings.LLMRoute = storage.LLMRouteServer
	system.settings.PublicLLMBaseURL = "https://platform.example/v1"
	system.settings.PublicLLMProtocol = "responses"
	system.settings.PublicLLMModel = "platform-model"
	system.settings.PublicLLMAPIKeyEncrypted = "platform-secret"
	system.settings.PublicLLMMaxContextTokens = 128000
	system.settings.PublicLLMMaxResponseTokens = 16384
	system.settings.PublicLLMTopP = 1
	system.settings.PublicLLMSettingsRevision = 1
	key := "platform-secret"
	if _, err := svc.UpdatePlatform(context.Background(), system.settings, &key, false); err != nil {
		t.Fatal(err)
	}
	runtime, err = svc.Get(context.Background(), "player-1")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Source != SourcePlatform || runtime.BaseURL != "https://platform.example/v1" {
		t.Fatalf("platform server runtime = %+v, want platform config", runtime)
	}
	if !runtime.ClientRouteEnabled {
		t.Fatalf("platform server runtime must carry client_route_enabled=true: %+v", runtime)
	}
}

// TestUpdateUserPlatformManagedTable 覆盖 7.2.1 第 6 条表格的全部 6 行。
func TestUpdateUserPlatformManagedTable(t *testing.T) {
	newSvc := func(mode string, enabled bool, stored *storage.LLMSettings) (*Service, *fakeUsers) {
		users := &fakeUsers{settings: stored}
		system := &fakeSystem{settings: &storage.SystemSettings{ID: 1, LLMAccessMode: mode, ClientLLMRouteEnabled: enabled}}
		return New(users, system, "01234567890123456789012345678901"), users
	}
	personal := func(route string) *storage.LLMSettings {
		return &storage.LLMSettings{
			PlayerID: "player-1", BaseURL: "https://user.example/v1",
			Protocol: "responses", Model: "user-model", MaxContextTokens: 128000,
			MaxResponseTokens: 16384, TopP: 1, APIKeyEncrypted: "enc-old",
			LLMRoute: route,
		}
	}
	next := &storage.LLMSettings{
		BaseURL: "https://user.example/v1", Protocol: "responses",
		Model: "user-model", MaxContextTokens: 128000, MaxResponseTokens: 16384, TopP: 1,
	}
	key := "user-secret"

	t.Run("platform closed rejects any route", func(t *testing.T) {
		svc, _ := newSvc(AccessModePlatformManaged, false, personal(storage.LLMRouteServer))
		if _, err := svc.UpdateUser(context.Background(), "player-1", next, &key, storage.LLMRouteServer); !errors.Is(err, ErrUserSettingsDisabled) {
			t.Fatalf("UpdateUser error = %v, want ErrUserSettingsDisabled", err)
		}
	})

	t.Run("platform open server keeps personal config and key", func(t *testing.T) {
		svc, users := newSvc(AccessModePlatformManaged, true, personal(storage.LLMRouteClient))
		evil := &storage.LLMSettings{BaseURL: "https://evil.example/v1", Protocol: "responses", Model: "evil-model"}
		view, err := svc.UpdateUser(context.Background(), "player-1", evil, &key, storage.LLMRouteServer)
		if err != nil {
			t.Fatal(err)
		}
		if users.settings.LLMRoute != storage.LLMRouteServer {
			t.Fatalf("stored route = %q, want server", users.settings.LLMRoute)
		}
		if users.settings.BaseURL != "https://user.example/v1" || users.settings.Model != "user-model" {
			t.Fatalf("personal config was overwritten: %+v", users.settings)
		}
		if users.settings.APIKeyEncrypted != "enc-old" {
			t.Fatalf("server key was not preserved: %q", users.settings.APIKeyEncrypted)
		}
		if view.LLMRoute != storage.LLMRouteServer {
			t.Fatalf("view route = %q, want server", view.LLMRoute)
		}
	})

	t.Run("platform open client saves personal config", func(t *testing.T) {
		svc, users := newSvc(AccessModePlatformManaged, true, personal(storage.LLMRouteServer))
		other := "should-be-ignored"
		if _, err := svc.UpdateUser(context.Background(), "player-1", next, &other, storage.LLMRouteClient); err != nil {
			t.Fatal(err)
		}
		if users.settings.LLMRoute != storage.LLMRouteClient || users.settings.APIKeyEncrypted != "" {
			t.Fatalf("stored = %+v, want client without server key", users.settings)
		}
	})

	t.Run("user closed client is rejected", func(t *testing.T) {
		svc, _ := newSvc(AccessModeUserManaged, false, personal(storage.LLMRouteServer))
		other := "should-be-rejected"
		if _, err := svc.UpdateUser(context.Background(), "player-1", next, &other, storage.LLMRouteClient); !errors.Is(err, ErrClientRouteDisabled) {
			t.Fatalf("UpdateUser error = %v, want ErrClientRouteDisabled", err)
		}
	})

	t.Run("user server requires key", func(t *testing.T) {
		svc, _ := newSvc(AccessModeUserManaged, true, &storage.LLMSettings{PlayerID: "player-1", LLMRoute: storage.LLMRouteServer})
		if _, err := svc.UpdateUser(context.Background(), "player-1", next, nil, storage.LLMRouteServer); !errors.Is(err, ErrMissingAPIKey) {
			t.Fatalf("UpdateUser error = %v, want ErrMissingAPIKey", err)
		}
	})

	t.Run("user open client saves personal config", func(t *testing.T) {
		svc, users := newSvc(AccessModeUserManaged, true, personal(storage.LLMRouteServer))
		other := "should-be-ignored"
		if _, err := svc.UpdateUser(context.Background(), "player-1", next, &other, storage.LLMRouteClient); err != nil {
			t.Fatal(err)
		}
		if users.settings.LLMRoute != storage.LLMRouteClient {
			t.Fatalf("stored route = %q, want client", users.settings.LLMRoute)
		}
	})
}
