package storage

import (
	"context"
	"testing"

	"github.com/ialtone/fatecraft/internal/testdb"
)

func TestPostgresAuthSettingsPersistsDisabledSwitches(t *testing.T) {
	db := testdb.Open(t, "auth_settings")
	repo := NewAuthRepository(db)
	ctx := context.Background()

	settings, err := repo.GetAuthSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	settings.EmailAuthEnabled = false
	settings.LinuxDOLoginEnabled = false
	settings.EmailVerificationEnabled = false
	if err := repo.SaveAuthSettings(ctx, settings); err != nil {
		t.Fatal(err)
	}

	stored, err := repo.GetAuthSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stored.EmailAuthEnabled || stored.LinuxDOLoginEnabled || stored.EmailVerificationEnabled {
		t.Fatalf("disabled switches were persisted as enabled: %+v", stored)
	}
}
