package llmsettings

import (
	"context"
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
	system := &fakeSystem{settings: &storage.SystemSettings{ID: 1, LLMAccessMode: AccessModePlatformManaged}}
	svc := New(&fakeUsers{}, system, "01234567890123456789012345678901")
	route, err := svc.EffectiveRoute(context.Background(), "player-1")
	if err != nil {
		t.Fatal(err)
	}
	if route != storage.LLMRouteServer {
		t.Fatalf("effective route = %q, want server", route)
	}
}

func TestSwitchToClientClearsServerKey(t *testing.T) {
	users := &fakeUsers{}
	system := &fakeSystem{}
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
