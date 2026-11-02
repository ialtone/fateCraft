//go:build slow

package realtime

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/storage"
	"github.com/ialtone/fatecraft/internal/testdb"
	"gorm.io/gorm"
)

func TestPostgresActivityPublishesOnlyCommittedSnapshots(t *testing.T) {
	db := testdb.Open(t, "activity_notify")
	var schema string
	if err := db.Raw("SELECT current_schema()").Scan(&schema).Error; err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(os.Getenv("FATECRAFT_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	id, owner, pack, version := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	hash := strings.Repeat("0", 64)
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO users(id,username,display_name) VALUES (?,'activity','Activity')", []any{owner}},
		{"INSERT INTO challenge_packs(id,slug,name) VALUES (?,'activity','Activity')", []any{pack}},
		{"INSERT INTO challenge_versions(id,challenge_pack_id,version,status,manifest,content_hash,published_at) VALUES (?,?,'1.0.0','published','{}'::jsonb,?,CURRENT_TIMESTAMP)", []any{version, pack, hash}},
		{"UPDATE challenge_packs SET current_version_id=?,status='active' WHERE id=?", []any{version, pack}},
		{"INSERT INTO conversations(id,player_id,title,challenge_version_id,challenge_hash,llm_route) VALUES (?,?,'Activity',?,?,'server')", []any{id, owner, version, hash}},
	} {
		if err := db.Exec(stmt.sql, stmt.args...).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := storage.NewActivityRepo(db)
	hub := NewHub(nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop, err := StartActivityNotifications(ctx, parsed.String(), repo, hub)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	connection := newConn(nil, id)
	if !hub.register(id, connection) {
		t.Fatal("register failed")
	}
	defer hub.unregister(id, connection)
	rollback := errors.New("rollback")
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&storage.Conversation{}).Where("id=?", id).Update("is_processing", true).Error; err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	select {
	case event := <-connection.send:
		t.Fatalf("uncommitted event: %s", event)
	case <-time.After(100 * time.Millisecond):
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&storage.Conversation{}).Where("id=?", id).Update("is_processing", true).Error; err != nil {
			return err
		}
		return tx.Model(&storage.Conversation{}).Where("id=?", id).Update("status", storage.StatusEnded).Error
	}); err != nil {
		t.Fatal(err)
	}
	event := readEvent(t, connection)
	latest, err := repo.Snapshot(ctx, id, "")
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != TypeActivityUpdated || event.Snapshot == nil || event.ActivityVersion != latest.ActivityVersion ||
		event.Snapshot.ConversationStatus != storage.StatusEnded || !event.Snapshot.IsProcessing {
		t.Fatalf("wrong committed snapshot: %+v", event)
	}
	// A write that changes neither stable fields nor version must not wake clients.
	if err := db.Model(&storage.Conversation{}).Where("id=?", id).Update("title", "renamed").Error; err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-connection.send:
		t.Fatalf("unchanged activity notified: %s", event)
	case <-time.After(100 * time.Millisecond):
	}
}
