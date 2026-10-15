package challengeimport

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/storage"
	"github.com/ialtone/fatecraft/internal/testdb"
)

func TestPostgresBundledMortalPathImportAndOverwrite(t *testing.T) {
	db := testdb.Open(t, "pack_verify")

	ctx := context.Background()
	repository := storage.NewChallengeRepository(db)
	importer := New(repository, filepath.Join("..", "..", "data"))
	first, err := importer.ImportBuiltin(ctx, "mortal-path")
	if err != nil {
		t.Fatalf("import mortal-path: %v", err)
	}
	if !first.Created || first.Version != "1.0.0" || first.ContentHash == "" {
		t.Fatalf("unexpected first import result: %+v", first)
	}
	pack, err := repository.GetPackBySlug(ctx, "mortal-path")
	if err != nil {
		t.Fatal(err)
	}
	if pack.CurrentVersionID == nil || pack.Status != storage.ChallengePackActive {
		t.Fatalf("mortal-path was not activated: %+v", pack)
	}
	firstVersionID := *pack.CurrentVersionID
	assertMortalPathPublishedContent(t, ctx, repository, firstVersionID)

	// A changed published identity must never be replaced, even if unused.
	if err := db.Model(&storage.ChallengeVersion{}).Where("id = ?", firstVersionID).
		Update("content_hash", strings.Repeat("0", 64)).Error; err != nil {
		t.Fatal(err)
	}
	_, err = importer.OverwriteBuiltin(ctx, "mortal-path")
	if !errors.Is(err, storage.ErrPublishedVersion) {
		t.Fatalf("published replacement accepted: %v", err)
	}
	pack, err = repository.GetPackBySlug(ctx, "mortal-path")
	if err != nil {
		t.Fatal(err)
	}
	if pack.CurrentVersionID == nil || *pack.CurrentVersionID != firstVersionID {
		t.Fatalf("overwrite did not replace the published snapshot: %+v", pack)
	}
}

func assertMortalPathPublishedContent(
	t *testing.T,
	ctx context.Context,
	repository *storage.ChallengeRepository,
	versionID uuid.UUID,
) {
	t.Helper()
	version, err := repository.GetVersion(ctx, versionID)
	if err != nil {
		t.Fatal(err)
	}
	if version.Status != storage.ChallengeVersionPublished || version.Version != "1.0.0" {
		t.Fatalf("unexpected mortal-path version: %+v", version)
	}
	definitions, err := repository.ListDefinitions(ctx, versionID)
	if err != nil {
		t.Fatal(err)
	}
	outcomes, err := repository.ListOutcomeRules(ctx, versionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 270 {
		t.Fatalf("definitions = %d, want 270", len(definitions))
	}
	wantPoints := map[string]int64{
		"law_seeker": 700, "world_warden": 700,
		"abandoned": 0, "lifespan_exhausted": 0, "perished": 0,
	}
	if len(outcomes) != len(wantPoints) {
		t.Fatalf("outcomes = %d, want %d", len(outcomes), len(wantPoints))
	}
	for _, outcome := range outcomes {
		points, ok := wantPoints[outcome.OutcomeKey]
		if !ok || outcome.RewardPoints != points || !outcome.Enabled {
			t.Fatalf("unexpected published outcome: %+v", outcome)
		}
	}
}
