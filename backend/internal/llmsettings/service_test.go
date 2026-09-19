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
func (f *fakeUsers) Save(_ context.Context, _ string, s *storage.LLMSettings) error { v := *s; f.settings = &v; return nil }

type fakeSystem struct{ settings *storage.SystemSettings }

func (f *fakeSystem) Get(context.Context) (*storage.SystemSettings, error) {
	if f.settings == nil { return storage.DefaultSystemSettings(), nil }
	v := *f.settings
	return &v, nil
}
func (f *fakeSystem) Save(_ context.Context, s *storage.SystemSettings) error { v := *s; f.settings = &v; return nil }

func TestUserKeyIsEncryptedAndNeverReturned(t *testing.T) {
	users := &fakeUsers{}
	system := &fakeSystem{}
	svc := New(users, system, "01234567890123456789012345678901")
	key := "user-secret"
	input := &storage.LLMSettings{BaseURL: "https://user.example/v1", Protocol: "responses", Model: "user-model", MaxContextTokens: 128000, MaxResponseTokens: 16384, TopP: 1}
	view, err := svc.UpdateUser(context.Background(), "player-1", input, &key)
	if err != nil { t.Fatal(err) }
	if view.HasAPIKey == false || view.APIKey != "" { t.Fatalf("unexpected user view: %+v", view) }
	if users.settings.APIKeyEncrypted == "" || users.settings.APIKeyEncrypted == key { t.Fatalf("key was not encrypted") }
	runtime, err := svc.Get(context.Background(), "player-1")
	if err != nil { t.Fatal(err) }
	if runtime.APIKey != key { t.Fatalf("runtime key = %q, want %q", runtime.APIKey, key) }
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
	if _, err := svc.UpdatePlatform(context.Background(), system.settings, &key, false); err != nil { t.Fatal(err) }
	runtime, err := svc.Get(context.Background(), "player-1")
	if err != nil { t.Fatal(err) }
	if runtime.Source != SourcePlatform || runtime.APIKey != key || runtime.Editable { t.Fatalf("unexpected platform settings: %+v", runtime) }
	_, err = svc.UpdateUser(context.Background(), "player-1", &storage.LLMSettings{}, &key)
	if err != ErrUserSettingsDisabled { t.Fatalf("UpdateUser error = %v, want %v", err, ErrUserSettingsDisabled) }
}
