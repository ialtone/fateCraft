package llmsettings

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ialtone/fatecraft/internal/storage"
)

func TestGameReadinessUsesEffectiveModelConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name      string
		change    func(*storage.LLMSettings, *storage.SystemSettings)
		wantRoute string
		wantErr   error
	}{
		{name: "configured personal server", wantRoute: storage.LLMRouteServer},
		{name: "missing personal key", change: func(u *storage.LLMSettings, _ *storage.SystemSettings) { u.APIKeyEncrypted = "" }, wantErr: ErrConfigurationIncomplete},
		{name: "missing model", change: func(u *storage.LLMSettings, _ *storage.SystemSettings) { u.Model = "  " }, wantErr: ErrConfigurationIncomplete},
		{name: "missing API address", change: func(u *storage.LLMSettings, _ *storage.SystemSettings) { u.BaseURL = " " }, wantErr: ErrConfigurationIncomplete},
		{name: "client key stays on device", change: func(u *storage.LLMSettings, _ *storage.SystemSettings) {
			u.LLMRoute = storage.LLMRouteClient
			u.APIKeyEncrypted = ""
		}, wantRoute: storage.LLMRouteClient},
		{name: "client still requires model", change: func(u *storage.LLMSettings, _ *storage.SystemSettings) {
			u.LLMRoute = storage.LLMRouteClient
			u.Model = ""
		}, wantErr: ErrConfigurationIncomplete},
		{name: "closed client needs server key", change: func(u *storage.LLMSettings, s *storage.SystemSettings) {
			u.LLMRoute = storage.LLMRouteClient
			u.APIKeyEncrypted = ""
			s.ClientLLMRouteEnabled = false
		}, wantErr: ErrConfigurationIncomplete},
		{name: "platform does not require personal config", change: func(u *storage.LLMSettings, s *storage.SystemSettings) {
			s.LLMAccessMode = AccessModePlatformManaged
			u.BaseURL = ""
			u.Model = ""
			u.APIKeyEncrypted = ""
		}, wantRoute: storage.LLMRouteServer},
		{name: "personal key cannot replace missing platform key", change: func(_ *storage.LLMSettings, s *storage.SystemSettings) {
			s.LLMAccessMode = AccessModePlatformManaged
			s.PublicLLMAPIKeyEncrypted = ""
		}, wantErr: ErrPlatformIncomplete},
		{name: "platform client still needs personal config", change: func(u *storage.LLMSettings, s *storage.SystemSettings) {
			s.LLMAccessMode = AccessModePlatformManaged
			u.LLMRoute = storage.LLMRouteClient
			u.Model = ""
		}, wantErr: ErrConfigurationIncomplete},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			user := &storage.LLMSettings{BaseURL: "https://user.example/v1", Model: "personal-model", LLMRoute: storage.LLMRouteServer}
			global := storage.DefaultSystemSettings()
			global.PublicLLMModel = "private-platform-model"
			svc := New(&fakeUsers{settings: user}, &fakeSystem{settings: global}, "01234567890123456789012345678901")
			ciphertext, err := svc.encrypt("test-api-secret")
			if err != nil {
				t.Fatal(err)
			}
			user.APIKeyEncrypted, global.PublicLLMAPIKeyEncrypted = ciphertext, ciphertext
			if tc.change != nil {
				tc.change(user, global)
			}
			view, err := svc.UserView(ctx, "player-1")
			if err != nil {
				t.Fatal(err)
			}
			if view.ConfigurationReady != (tc.wantErr == nil) {
				t.Fatalf("configuration readiness = %v", view.ConfigurationReady)
			}
			encoded, err := json.Marshal(view)
			if err != nil {
				t.Fatal(err)
			}
			for _, hidden := range []string{"test-api-secret", ciphertext, "private-platform-model"} {
				if strings.Contains(string(encoded), hidden) {
					t.Fatal("settings response exposed private model configuration")
				}
			}
			route, err := svc.ReadyRoute(ctx, "player-1")
			if !errors.Is(err, tc.wantErr) || route != tc.wantRoute {
				t.Fatalf("ReadyRoute = %q, %v; want %q, %v", route, err, tc.wantRoute, tc.wantErr)
			}
		})
	}
}

func TestGameReadinessRejectsUndecryptableServerKey(t *testing.T) {
	user := &fakeUsers{settings: &storage.LLMSettings{BaseURL: "https://user.example/v1", Model: "model", APIKeyEncrypted: "invalid-ciphertext"}}
	svc := New(user, &fakeSystem{}, "01234567890123456789012345678901")
	if route, err := svc.ReadyRoute(context.Background(), "player-1"); err == nil || route != "" {
		t.Fatal("unusable encrypted key allowed a game to start")
	}
}
