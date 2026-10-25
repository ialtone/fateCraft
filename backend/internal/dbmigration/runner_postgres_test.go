package dbmigration_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ialtone/fatecraft/internal/dbmigration"
	"github.com/ialtone/fatecraft/internal/testdb"
	"github.com/ialtone/fatecraft/migrations"
	"github.com/pressly/goose/v3"
)

func TestBaselineMigrationAndFailedMigrationRollback(t *testing.T) {
	db := testdb.OpenEmpty(t, "migration_verify")
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := dbmigration.Up(ctx, sqlDB); err != nil {
		t.Fatalf("first migration run: %v", err)
	}
	if err := dbmigration.Up(ctx, sqlDB); err != nil {
		t.Fatalf("second migration run: %v", err)
	}

	statuses, err := dbmigration.Status(ctx, sqlDB)
	if err != nil {
		t.Fatalf("read migration status: %v", err)
	}
	if len(statuses) != 1 || statuses[0].Version != 1 || statuses[0].State != "applied" {
		t.Fatalf("migration status = %+v, want version 1 applied", statuses)
	}
	version, err := dbmigration.Version(ctx, sqlDB)
	if err != nil {
		t.Fatalf("read migration version: %v", err)
	}
	if version != 1 {
		t.Fatalf("migration version = %d, want 1", version)
	}

	assertBaselineObjects(t, sqlDB)
	assertBaselineDownRejected(t, ctx, sqlDB)
	assertFailedMigrationRollsBack(t, ctx, sqlDB)
}

func TestConcurrentBaselineMigration(t *testing.T) {
	db := testdb.OpenEmpty(t, "migration_concurrent")
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			// Each call creates a different provider, so only the database lock
			// can serialize the migration-table creation and baseline DDL.
			results <- dbmigration.Up(ctx, sqlDB)
		}()
	}
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Errorf("concurrent migration: %v", err)
		}
	}
	var applied int
	if err := sqlDB.QueryRowContext(ctx, `SELECT count(*) FROM goose_db_version WHERE version_id=1 AND is_applied`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Fatalf("baseline version records = %d, want 1", applied)
	}
	assertBaselineObjects(t, sqlDB)
}

func assertBaselineDownRejected(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	// Exercise the SQL guard directly, bypassing our CLI's command rejection.
	p, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Down(ctx); err == nil || !strings.Contains(err.Error(), "baseline 00001 is irreversible") {
		t.Fatalf("baseline Down = %v, want explicit rejection", err)
	}
	version, err := dbmigration.Version(ctx, db)
	if err != nil || version != 1 {
		t.Fatalf("version after rejected Down = %d, %v; want 1", version, err)
	}
	assertBaselineObjects(t, db)
	if err := dbmigration.Up(ctx, db); err != nil {
		t.Fatalf("Up after rejected Down: %v", err)
	}
}

func assertBaselineObjects(t *testing.T, db interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) {
	t.Helper()
	ctx := context.Background()
	var functions, triggers, indexes, seedRows, rewardTables, registrationSeeds, rewardTriggers int
	var limitColumn, limitCheck, limitSnapshot, stageRewardGuards int
	queries := []struct {
		name   string
		query  string
		target *int
		want   int
	}{
		{"functions", `SELECT count(*) FROM pg_proc WHERE pronamespace = current_schema()::regnamespace AND proname IN ('reject_reward_ledger_mutation', 'reject_campaign_event_mutation', 'reject_campaign_action_commit_mutation', 'reject_llm_route_change')`, &functions, 4},
		{"triggers", `SELECT count(*) FROM pg_trigger WHERE NOT tgisinternal AND tgrelid IN ('reward_ledger'::regclass, 'campaign_events'::regclass, 'campaign_action_commits'::regclass, 'conversations'::regclass, 'campaigns'::regclass)`, &triggers, 12},
		{"indexes", `SELECT count(*) FROM pg_indexes WHERE schemaname = current_schema() AND indexname IN ('idx_users_username_lower', 'idx_users_single_owner', 'idx_challenge_versions_pack', 'idx_reward_ledger_player', 'idx_chat_messages_active_turn_role')`, &indexes, 5},
		{"seed rows", `SELECT count(*) FROM reward_exchange_offers WHERE id = '00000000-0000-0000-0000-000000000001'`, &seedRows, 1},
		{"reward tables with comments", `SELECT count(*) FROM pg_class WHERE relnamespace=current_schema()::regnamespace AND relname IN ('registration_reward_rules','reward_exchange_offers','reward_exchange_revisions','reward_exchange_orders') AND obj_description(oid,'pg_class') IS NOT NULL`, &rewardTables, 4},
		{"registration rule seed", `SELECT count(*) FROM registration_reward_rules WHERE points=0 AND rewrite_credits=10`, &registrationSeeds, 1},
		{"immutable reward objects", `SELECT count(*) FROM pg_trigger WHERE NOT tgisinternal AND tgrelid IN ('registration_reward_rules'::regclass,'reward_exchange_orders'::regclass,'reward_exchange_revisions'::regclass)`, &rewardTriggers, 3},
		{"exchange limit column", `SELECT count(*) FROM information_schema.columns WHERE table_schema=current_schema() AND table_name='reward_exchange_offers' AND column_name='max_redemptions_per_user' AND column_default='0' AND is_nullable='NO'`, &limitColumn, 1},
		{"exchange limit check", `SELECT count(*) FROM pg_constraint WHERE conrelid='reward_exchange_offers'::regclass AND contype='c' AND pg_get_constraintdef(oid) LIKE '%max_redemptions_per_user%'`, &limitCheck, 1},
		{"exchange limit seed snapshot", `SELECT count(*) FROM reward_exchange_revisions WHERE offer_id='00000000-0000-0000-0000-000000000001' AND revision=1 AND snapshot->>'max_redemptions_per_user'='0'`, &limitSnapshot, 1},
		{"challenge stage reward guards", `SELECT count(*) FROM pg_trigger WHERE NOT tgisinternal AND tgrelid='challenge_stage_rewards'::regclass AND tgname IN ('challenge_stage_reward_immutable','gameplay_write_guard')`, &stageRewardGuards, 2},
	}
	for _, check := range queries {
		if err := db.QueryRowContext(ctx, check.query).Scan(check.target); err != nil {
			t.Fatalf("query %s: %v", check.name, err)
		}
		if *check.target != check.want {
			t.Fatalf("%s count = %d, want %d", check.name, *check.target, check.want)
		}
	}
}

func assertFailedMigrationRollsBack(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	baseline, err := migrations.FS.ReadFile("00001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	brokenFS := fstest.MapFS{
		"00001_init.sql": {Data: baseline},
		"00002_broken.sql": {Data: []byte(`-- +goose Up
CREATE TABLE goose_failure_probe (id INTEGER PRIMARY KEY);
INSERT INTO goose_failure_probe (id) VALUES (1);
SELECT fatecraft_intentionally_missing_function();
`)},
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, brokenFS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err == nil {
		t.Fatal("broken migration unexpectedly succeeded")
	}

	var tableExists bool
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('goose_failure_probe') IS NOT NULL`).Scan(&tableExists); err != nil {
		t.Fatal(err)
	}
	if tableExists {
		t.Fatal("failed migration left goose_failure_probe behind")
	}
	version, err := dbmigration.Version(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if version != 1 {
		t.Fatalf("version after failed migration = %d, want 1", version)
	}
}
