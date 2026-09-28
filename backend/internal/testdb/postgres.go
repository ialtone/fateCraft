// Package testdb contains the shared isolated PostgreSQL setup for integration tests.
package testdb

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/ialtone/fatecraft/internal/dbmigration"
)

// Open creates a temporary PostgreSQL schema and applies the same migrations used by production.
// Tests are skipped when FATECRAFT_TEST_DATABASE_URL is not configured.
func Open(t testing.TB, prefix string) *gorm.DB {
	t.Helper()
	db := OpenEmpty(t, prefix)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := dbmigration.Up(context.Background(), sqlDB); err != nil {
		t.Fatalf("initialize PostgreSQL schema: %v", err)
	}
	return db
}

// OpenEmpty creates an isolated PostgreSQL schema without applying migrations.
func OpenEmpty(t testing.TB, prefix string) *gorm.DB {
	t.Helper()
	databaseURL := os.Getenv("FATECRAFT_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("FATECRAFT_TEST_DATABASE_URL is not set")
	}

	baseDB, err := gorm.Open(postgres.Open(databaseURL), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	schemaName := "fatecraft_" + prefix + "_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := baseDB.Exec("CREATE SCHEMA " + schemaName).Error; err != nil {
		t.Fatal(err)
	}

	parsedURL, err := url.Parse(databaseURL)
	if err != nil || parsedURL.Scheme == "" {
		t.Fatalf("FATECRAFT_TEST_DATABASE_URL must be a PostgreSQL URL: %v", err)
	}
	query := parsedURL.Query()
	query.Set("search_path", schemaName)
	parsedURL.RawQuery = query.Encode()
	db, err := gorm.Open(postgres.Open(parsedURL.String()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = sqlDB.Close()
		if err := baseDB.Exec("DROP SCHEMA " + schemaName + " CASCADE").Error; err != nil {
			t.Errorf("drop isolated verification schema: %v", err)
		}
		if baseSQL, err := baseDB.DB(); err == nil {
			_ = baseSQL.Close()
		}
	})
	return db
}
