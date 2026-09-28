package dbmigration

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/ialtone/fatecraft/migrations"
)

// Open creates the database/sql connection used exclusively by migration commands.
func Open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres for migrations: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping postgres for migrations: %w", err)
	}
	return db, nil
}

func provider(db *sql.DB) (*goose.Provider, error) {
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return nil, fmt.Errorf("create postgres migration locker: %w", err)
	}
	p, err := goose.NewProvider(
		goose.DialectPostgres,
		db,
		migrations.FS,
		goose.WithSessionLocker(locker),
	)
	if err != nil {
		return nil, fmt.Errorf("create goose provider: %w", err)
	}
	return p, nil
}

// Up applies all pending migrations.
func Up(ctx context.Context, db *sql.DB) error {
	p, err := provider(db)
	if err != nil {
		return err
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("apply database migrations: %w", err)
	}
	return nil
}

// Down rolls back the latest migration. Production callers should not expose this operation.
func Down(ctx context.Context, db *sql.DB) error {
	p, err := provider(db)
	if err != nil {
		return err
	}
	if _, err := p.Down(ctx); err != nil {
		return fmt.Errorf("roll back database migration: %w", err)
	}
	return nil
}

type MigrationStatus struct {
	Version int64
	State   string
}

// Status returns the current state of every embedded migration.
func Status(ctx context.Context, db *sql.DB) ([]MigrationStatus, error) {
	p, err := provider(db)
	if err != nil {
		return nil, err
	}
	status, err := p.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("read database migration status: %w", err)
	}
	result := make([]MigrationStatus, 0, len(status))
	for _, item := range status {
		result = append(result, MigrationStatus{
			Version: item.Source.Version,
			State:   fmt.Sprint(item.State),
		})
	}
	return result, nil
}

// Version returns the highest applied migration version.
func Version(ctx context.Context, db *sql.DB) (int64, error) {
	p, err := provider(db)
	if err != nil {
		return 0, err
	}
	version, err := p.GetDBVersion(ctx)
	if err != nil {
		return 0, fmt.Errorf("read database migration version: %w", err)
	}
	return version, nil
}
