package storage

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/identity"
	"github.com/ialtone/fatecraft/internal/testdb"
)

func TestPostgresDetailedLogScopePersistenceAndValidation(t *testing.T) {
	db := testdb.Open(t, "log_scope")
	actor, target, missing := uuid.New(), uuid.New(), uuid.New()
	if err := db.Exec(`INSERT INTO users(id,username,display_name) VALUES(?,'scope-admin','Scope Admin'),(?,'scope-player','Scope Player')`, actor, target).Error; err != nil {
		t.Fatal(err)
	}
	ctx := identity.WithPlayerID(context.Background(), actor.String())
	repo := NewSystemSettingsRepo(db)
	settings, err := repo.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	settings.DetailedLLMLoggingEnabled, settings.DetailedLLMLoggingScope, settings.DetailedLLMLoggingTargetID = true, "user", &target
	if err := repo.Save(ctx, settings); err != nil {
		t.Fatal(err)
	}
	stored, err := NewSystemSettingsRepo(db).Get(ctx)
	if err != nil || stored.DetailedLLMLoggingTargetID == nil || *stored.DetailedLLMLoggingTargetID != target || !stored.CaptureDetailedLLMLog(target.String(), "") {
		t.Fatalf("scope not persisted: %v", err)
	}
	var audit AdminAuditLog
	if err := db.Where("action=?", "system_settings_change").First(&audit).Error; err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(audit.Metadata)
	for _, required := range []string{"detailed_llm_logging_scope", "detailed_llm_logging_target_id", target.String()} {
		if !strings.Contains(string(encoded), required) {
			t.Fatalf("missing audited scope: %s", encoded)
		}
	}
	if strings.Contains(string(encoded), "scope-player") {
		t.Fatal("scope audit must contain only target ID")
	}
	settings.DetailedLLMLoggingTargetID = &missing
	if err := repo.Save(ctx, settings); !errors.Is(err, ErrDetailedLLMLoggingTarget) {
		t.Fatalf("unknown user accepted: %v", err)
	}
	settings.DetailedLLMLoggingScope, settings.DetailedLLMLoggingTargetID = "conversation", &target
	if err := repo.Save(ctx, settings); !errors.Is(err, ErrDetailedLLMLoggingTarget) {
		t.Fatalf("user ID accepted as conversation: %v", err)
	}
	settings, err = repo.Get(ctx)
	if err != nil || *settings.DetailedLLMLoggingTargetID != target || settings.DetailedLLMLoggingScope != "user" {
		t.Fatal("rejected save changed cache")
	}
	if err := db.Exec("DELETE FROM users WHERE id=?", target).Error; err != nil {
		t.Fatal(err)
	}
	settings.DetailedLLMLoggingEnabled = false
	if err := repo.Save(ctx, settings); err != nil {
		t.Fatalf("cannot turn off deleted target: %v", err)
	}
	settings.DetailedLLMLoggingEnabled = true
	if err := repo.Save(ctx, settings); !errors.Is(err, ErrDetailedLLMLoggingTarget) {
		t.Fatalf("deleted target re-enabled: %v", err)
	}
	settings.DetailedLLMLoggingScope, settings.DetailedLLMLoggingTargetID = "all", nil
	if err := repo.Save(ctx, settings); err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		`UPDATE system_settings SET detailed_llm_logging_scope='invalid' WHERE id=1`,
		`UPDATE system_settings SET detailed_llm_logging_scope='user',detailed_llm_logging_target_id=NULL WHERE id=1`,
		`UPDATE system_settings SET detailed_llm_logging_scope='conversation',detailed_llm_logging_target_id='00000000-0000-0000-0000-000000000000' WHERE id=1`,
		`UPDATE system_settings SET detailed_llm_logging_target_id='00000000-0000-0000-0000-000000000001' WHERE id=1`,
	} {
		if err := db.Exec(sql).Error; err == nil {
			t.Fatalf("invalid scope bypassed baseline constraint: %s", sql)
		}
	}
}
