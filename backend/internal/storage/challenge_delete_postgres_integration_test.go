package storage

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func insertDeletableVersion(t *testing.T, db *gorm.DB, packID, versionID uuid.UUID, version, status string) {
	t.Helper()
	if err := db.Exec(`INSERT INTO challenge_packs (id, slug, name) VALUES (?, ?, ?)`,
		packID, "delete-test-"+strings.ReplaceAll(versionID.String()[:8], "-", ""), "Delete Test").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO challenge_versions
		(id, challenge_pack_id, version, status, manifest, content_hash, published_at)
		VALUES (?, ?, ?, ?, '{}'::jsonb,
			CASE WHEN ? = 'draft' THEN '' ELSE '` + strings.Repeat("c", 64) + `' END,
			CASE WHEN ? = 'draft' THEN NULL ELSE CURRENT_TIMESTAMP END)`,
		versionID, packID, version, status, status, status).Error; err != nil {
		t.Fatal(err)
	}
}

func TestPostgresDeleteVersionIfUnused(t *testing.T) {
	db := openGenerationTestDB(t)
	ctx := context.Background()
	repository := NewChallengeRepository(db)

	t.Run("unused draft is removed", func(t *testing.T) {
		packID, versionID := uuid.New(), uuid.New()
		insertDeletableVersion(t, db, packID, versionID, "9.9.9", ChallengeVersionDraft)
		if err := repository.DeleteVersionIfUnused(ctx, versionID); err != nil {
			t.Fatal(err)
		}
		var count int64
		if err := db.Model(&ChallengeVersion{}).Where("id = ?", versionID).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatal("version row was not deleted")
		}
	})

	t.Run("version referenced by conversation is kept", func(t *testing.T) {
		packID, versionID := uuid.New(), uuid.New()
		insertDeletableVersion(t, db, packID, versionID, "9.9.8", ChallengeVersionPublished)
		playerID := uuid.New()
		if err := db.Exec(`INSERT INTO users (id, username, display_name) VALUES (?, 'deleteref', 'Delete Ref')`,
			playerID).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(`INSERT INTO conversations
			(id, player_id, title, status, challenge_version_id, challenge_hash, llm_route)
			VALUES (?, ?, 'delete-guard', 'active', ?, ?, 'server')`,
			uuid.New(), playerID, versionID, strings.Repeat("b", 64)).Error; err != nil {
			t.Fatal(err)
		}
		if err := repository.DeleteVersionIfUnused(ctx, versionID); !errors.Is(err, ErrVersionInUse) {
			t.Fatalf("expected ErrVersionInUse, got %v", err)
		}
		var count int64
		if err := db.Model(&ChallengeVersion{}).Where("id = ?", versionID).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatal("referenced version must not be deleted")
		}
	})

	t.Run("deleting current version detaches pack", func(t *testing.T) {
		packID, versionID := uuid.New(), uuid.New()
		insertDeletableVersion(t, db, packID, versionID, "9.9.7", ChallengeVersionPublished)
		if err := db.Exec(`UPDATE challenge_packs SET current_version_id = ?, status = ?
			WHERE id = ?`, versionID, ChallengePackActive, packID).Error; err != nil {
			t.Fatal(err)
		}
		if err := repository.DeleteVersionIfUnused(ctx, versionID); err != nil {
			t.Fatal(err)
		}
		pack, err := repository.GetPack(ctx, packID)
		if err != nil {
			t.Fatal(err)
		}
		if pack.CurrentVersionID != nil {
			t.Fatal("pack current version should be detached")
		}
		if pack.Status != ChallengePackHidden {
			t.Fatalf("pack status = %q, want %q", pack.Status, ChallengePackHidden)
		}
	})
}
