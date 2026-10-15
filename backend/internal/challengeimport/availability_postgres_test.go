package challengeimport

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/challenge"
	"github.com/ialtone/fatecraft/internal/storage"
	"github.com/ialtone/fatecraft/internal/testdb"
)

func TestPostgresBootstrapAndAvailabilityPreserveAdminChoices(t *testing.T) {
	db := testdb.Open(t, "builtin_availability")
	ctx := context.Background()
	repo := storage.NewChallengeRepository(db)
	importer := New(repo, filepath.Join("..", "..", "data"))
	if _, err := importer.ImportBuiltin(ctx, "mortal-path"); err != nil {
		t.Fatal(err)
	}
	pack, err := repo.GetPackBySlug(ctx, "mortal-path")
	if err != nil {
		t.Fatal(err)
	}
	original := *pack.CurrentVersionID
	service := challenge.NewService(repo, defaultRegistry())
	if _, err := service.SetEnabled(ctx, pack.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&storage.ChallengePack{}).Where("id = ?", pack.ID).Update("name", "管理员自定义名称").Error; err != nil {
		t.Fatal(err)
	}
	result, err := importer.BootstrapBuiltins(ctx)
	if err != nil || !result.Initialized || len(result.Imports) != 8 {
		t.Fatalf("missing packs not imported: %+v %v", result, err)
	}
	pack, err = repo.GetPack(ctx, pack.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pack.Status != storage.ChallengePackHidden || pack.Name != "管理员自定义名称" || *pack.CurrentVersionID != original {
		t.Fatalf("bootstrap changed existing content: %+v", pack)
	}
	result, err = importer.BootstrapBuiltins(ctx)
	if err != nil || result.Initialized || len(result.Imports) != 0 {
		t.Fatalf("restart must be idempotent: %+v %v", result, err)
	}
	public, err := service.ListPublished(ctx)
	if err != nil || len(public) != 8 {
		t.Fatalf("disabled pack visible: %d %v", len(public), err)
	}
	if _, err := service.GetPublished(ctx, pack.Slug); !errors.Is(err, challenge.ErrNotPublished) {
		t.Fatalf("disabled detail accessible: %v", err)
	}
	if _, err := service.ResolveSelection(ctx, pack.Slug, ""); !errors.Is(err, challenge.ErrNotPublished) {
		t.Fatalf("disabled new-game selection allowed: %v", err)
	}
	if current, err := service.IsCurrentVersion(ctx, original); err != nil || !current {
		t.Fatalf("disabling must not block existing save turns: %v %v", current, err)
	}
	infos, err := importer.ListBuiltins(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range infos {
		if info.Slug == pack.Slug && (info.Enabled || info.PackID == nil || *info.PackID != pack.ID) {
			t.Fatalf("admin status wrong: %+v", info)
		}
	}
	// A normal new-version publication must preserve the disabled entry.
	admin := uuid.New()
	if err := db.Exec("INSERT INTO users (id, username, display_name) VALUES (?, 'availability-admin', 'Admin')", admin).Error; err != nil {
		t.Fatal(err)
	}
	draft, err := service.CreateDraft(ctx, pack.ID, admin, "3.7.1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Publish(ctx, draft.Version.ID, admin); err != nil {
		t.Fatal(err)
	}
	pack, err = repo.GetPack(ctx, pack.ID)
	if err != nil || pack.Status != storage.ChallengePackHidden {
		t.Fatalf("publication re-enabled pack: %+v %v", pack, err)
	}
	if _, err := service.SetEnabled(ctx, pack.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetPublished(ctx, pack.Slug); err != nil {
		t.Fatalf("re-enabled pack inaccessible: %v", err)
	}
}

func TestPostgresBuiltinOverwritePreservesDisabledEntry(t *testing.T) {
	db := testdb.Open(t, "builtin_disabled_overwrite")
	ctx := context.Background()
	repo := storage.NewChallengeRepository(db)
	importer := New(repo, filepath.Join("..", "..", "data"))
	if _, err := importer.ImportBuiltin(ctx, "mortal-path"); err != nil {
		t.Fatal(err)
	}
	pack, err := repo.GetPackBySlug(ctx, "mortal-path")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetPackEnabled(ctx, pack.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&storage.ChallengeVersion{}).Where("id = ?", *pack.CurrentVersionID).Update("content_hash", strings.Repeat("0", 64)).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := importer.OverwriteBuiltin(ctx, pack.Slug); !errors.Is(err, storage.ErrPublishedVersion) {
		t.Fatal(err)
	}
	pack, err = repo.GetPack(ctx, pack.ID)
	if err != nil || pack.Status != storage.ChallengePackHidden {
		t.Fatalf("overwrite re-enabled pack: %+v %v", pack, err)
	}
}
