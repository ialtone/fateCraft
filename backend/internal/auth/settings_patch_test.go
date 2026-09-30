package auth

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/storage"
)

func TestAuthSettingsPatchKeepsOtherTabsAndCredentials(t *testing.T) {
	initial := storage.DefaultAuthSettings()
	initial.EmailAuthEnabled = true
	initial.EmailRegistrationEnabled = true
	initial.EmailVerificationEnabled = false
	initial.LinuxDOLoginEnabled = true
	initial.SMTPHost = "smtp.old.example"
	initial.SMTPPasswordEncrypted = "unchanged-ciphertext"
	repo := &fakeRepository{settings: initial}
	svc := NewService(repo, Config{})
	ctx, owner := context.Background(), uuid.New()
	expected := *initial
	expected.SMTPHost = "smtp.new.example"
	if _, err := svc.UpdateAdminSettings(ctx, owner, UpdateAuthSettingsInput{SMTPHost: authSettingPointer(" smtp.new.example ")}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*repo.settings, expected) {
		t.Fatal("SMTP patch changed login, verification, or credential settings")
	}
	expected.LinuxDOLoginEnabled = false
	if _, err := svc.UpdateAdminSettings(ctx, owner, UpdateAuthSettingsInput{LinuxDOLoginEnabled: authSettingPointer(false)}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*repo.settings, expected) {
		t.Fatal("explicit false was lost or login patch changed mail settings")
	}
	if _, err := svc.UpdateAdminSettings(ctx, owner, UpdateAuthSettingsInput{VerificationTemplate: authSettingPointer("missing code placeholder")}); !errors.Is(err, ErrInvalidAuthSettings) {
		t.Fatalf("invalid mail template accepted: %v", err)
	}
	if !reflect.DeepEqual(*repo.settings, expected) {
		t.Fatal("invalid patch changed persisted settings")
	}
	if _, err := svc.UpdateAdminSettings(ctx, owner, UpdateAuthSettingsInput{SMTPPort: authSettingPointer(0)}); !errors.Is(err, ErrInvalidAuthSettings) {
		t.Fatalf("explicit invalid port accepted: %v", err)
	}
}
