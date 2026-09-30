package storage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/identity"
	"github.com/ialtone/fatecraft/internal/logging"
	"github.com/ialtone/fatecraft/internal/testdb"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestDiagnosticCursorPrecisionAndFilterBinding(t *testing.T) {
	db, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=localhost user=test dbname=test sslmode=disable"}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 9, 29, 12, 30, 0, 123456000, time.UTC)
	id := uuid.New()
	q := LogQuery{Limit: 1, Model: "same-model"}
	q.Cursor = q.next("llm", when, id)
	encoded, _ := base64.RawURLEncoding.DecodeString(q.Cursor)
	var cursor logCursor
	if json.Unmarshal(encoded, &cursor) != nil || !cursor.Time.Equal(when) {
		t.Fatal("cursor lost timestamp precision")
	}
	if _, err := q.query(context.Background(), db, "llm"); err != nil {
		t.Fatal(err)
	}
	q.Limit = 100
	if _, err := q.query(context.Background(), db, "llm"); err != nil {
		t.Fatal("page size should not invalidate cursor")
	}
	if _, err := q.query(context.Background(), db, "context"); !errors.Is(err, ErrLogQuery) {
		t.Fatal("wrong-kind cursor accepted")
	}
	q.Model = "other-model"
	if _, err := q.query(context.Background(), db, "llm"); !errors.Is(err, ErrLogQuery) {
		t.Fatal("filter change accepted")
	}
	q.Cursor = "invalid"
	if _, err := q.query(context.Background(), db, "llm"); !errors.Is(err, ErrLogQuery) {
		t.Fatal("malformed cursor accepted")
	}
}

func TestPostgresDiagnosticPagingAndCleanupIsolation(t *testing.T) {
	db := testdb.Open(t, "diagnostic_page")
	ctx := context.Background()
	repo := NewLLMRequestLogRepo(db)
	when := time.Now().UTC().AddDate(0, 0, -40).Truncate(time.Microsecond)
	ids := []uuid.UUID{}
	for i := 0; i < 5; i++ {
		row := &LLMRequestLog{ID: uuid.New(), Model: "test", CreatedAt: when, StartedAt: when, BodyStatus: "disabled"}
		if err := repo.Create(ctx, row); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, row.ID)
	}
	q := LogQuery{Limit: 2, Model: "test"}
	seen := map[uuid.UUID]bool{}
	for {
		rows, next, more, err := repo.List(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if seen[row.ID] {
				t.Fatal("duplicate page row")
			}
			seen[row.ID] = true
		}
		if !more {
			break
		}
		q.Cursor = next
	}
	if len(seen) != 5 {
		t.Fatalf("lost rows: %d", len(seen))
	}
	auditID := uuid.New()
	if err := db.Exec(`INSERT INTO admin_audit_logs(id,action,result,created_at) VALUES(?, 'system_settings_change','success',?)`, auditID, when).Error; err != nil {
		t.Fatal(err)
	}
	fresh := &LLMRequestLog{ID: uuid.New(), Model: "fresh", CreatedAt: time.Now(), StartedAt: time.Now()}
	if err := repo.Create(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	cleanup := NewDiagnosticLogRepo(db)
	cutoff := time.Now().AddDate(0, 0, -30)
	// A second connection must skip a locked expired record, leaving a visible backlog.
	lock := db.Begin()
	if lock.Error != nil {
		t.Fatal(lock.Error)
	}
	t.Cleanup(func() { lock.Rollback() })
	if err := lock.Exec(`SELECT id FROM llm_request_logs WHERE id=? FOR UPDATE`, ids[0]).Error; err != nil {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	result, err := cleanup.CleanupExpired(bounded, cutoff, 2, 1)
	if err != nil || result.LLMRequests != 2 || !result.HasMore || !result.LimitReached {
		t.Fatalf("first cleanup=%+v err=%v", result, err)
	}
	if err := lock.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	result, err = cleanup.CleanupExpired(ctx, cutoff, 2, 5)
	if err != nil || result.LLMRequests != 3 || result.HasMore {
		t.Fatalf("drain cleanup=%+v err=%v", result, err)
	}
	if _, err := repo.Get(ctx, fresh.ID); err != nil {
		t.Fatal("fresh diagnostic removed")
	}
	var n int64
	if err := db.Model(&AdminAuditLog{}).Where("id=?", auditID).Count(&n).Error; err != nil || n != 1 {
		t.Fatal("business audit removed")
	}
}

func TestPostgresConfigAuditRollbackConflictAndSecrets(t *testing.T) {
	db := testdb.Open(t, "config_audit")
	actor := uuid.New()
	if err := db.Exec(`INSERT INTO users(id,username,display_name) VALUES(?,'log-admin','Log Admin')`, actor).Error; err != nil {
		t.Fatal(err)
	}
	ctx := identity.WithPlayerID(logging.NewTrace(context.Background()), actor.String())
	repo, other := NewSystemSettingsRepo(db), NewSystemSettingsRepo(db)
	settings, err := repo.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := other.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	settings.DiagnosticLogRetentionDays = 31
	settings.PublicLLMAPIKeyEncrypted = "synthetic-secret"
	if err := repo.Save(ctx, settings); err != nil {
		t.Fatal(err)
	}
	stale.RollSystemEnabled = !stale.RollSystemEnabled
	if err := other.Save(ctx, stale); !errors.Is(err, ErrSettingsConflict) {
		t.Fatalf("stale write=%v", err)
	}
	refreshed, err := other.Get(ctx)
	if err != nil || refreshed.DiagnosticLogRetentionDays != 31 {
		t.Fatal("conflict failed to invalidate stale cache")
	}
	var audit AdminAuditLog
	if err := db.Where("action=?", "system_settings_change").First(&audit).Error; err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(audit.Metadata)
	if strings.Contains(string(encoded), "synthetic-secret") || audit.ActorUserID == nil || *audit.ActorUserID != actor || audit.TraceID == nil || audit.TraceID.String() != logging.TraceID(ctx) {
		t.Fatalf("unsafe/missing audit: %s", encoded)
	}
	// Force the audit insert to fail after the settings UPDATE inside its transaction.
	if err := db.Exec(`CREATE FUNCTION reject_config_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test audit failure'; END $$`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TRIGGER reject_config_audit BEFORE INSERT ON admin_audit_logs FOR EACH ROW EXECUTE FUNCTION reject_config_audit()`).Error; err != nil {
		t.Fatal(err)
	}
	settings.DiagnosticLogRetentionDays = 32
	if err := repo.Save(ctx, settings); err == nil {
		t.Fatal("audit failure did not abort configuration")
	}
	cached, err := repo.Get(ctx)
	if err != nil || cached.DiagnosticLogRetentionDays != 31 {
		t.Fatal("uncommitted settings entered cache")
	}
	var stored SystemSettings
	if err := db.First(&stored, "id=1").Error; err != nil || stored.DiagnosticLogRetentionDays != 31 {
		t.Fatal("settings did not roll back")
	}
	auth := NewAuthRepository(db)
	authSettings, err := auth.GetAuthSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	original := authSettings.InvitationRequired
	authSettings.InvitationRequired = !original
	if err := auth.SaveAuthSettings(ctx, authSettings); err == nil {
		t.Fatal("auth audit failure did not abort settings")
	}
	authSettings, err = auth.GetAuthSettings(ctx)
	if err != nil || authSettings.InvitationRequired != original {
		t.Fatal("auth settings did not roll back")
	}
}
