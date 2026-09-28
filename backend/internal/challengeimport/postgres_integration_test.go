package challengeimport

import (
	"context"
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
	if !first.Created || first.Version != "3.6.2" || first.ContentHash == "" {
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

	// Force the stored snapshot hash to differ so the explicit overwrite path
	// must remove the unused published version and republish repository content.
	if err := db.Model(&storage.ChallengeVersion{}).Where("id = ?", firstVersionID).
		Update("content_hash", strings.Repeat("0", 64)).Error; err != nil {
		t.Fatal(err)
	}
	overwritten, err := importer.OverwriteBuiltin(ctx, "mortal-path")
	if err != nil {
		t.Fatalf("overwrite mortal-path: %v", err)
	}
	if !overwritten.Created || !overwritten.Overwritten || overwritten.ContentHash != first.ContentHash {
		t.Fatalf("unexpected overwrite result: %+v", overwritten)
	}
	pack, err = repository.GetPackBySlug(ctx, "mortal-path")
	if err != nil {
		t.Fatal(err)
	}
	if pack.CurrentVersionID == nil || *pack.CurrentVersionID == firstVersionID {
		t.Fatalf("overwrite did not replace the published snapshot: %+v", pack)
	}
	assertMortalPathPublishedContent(t, ctx, repository, *pack.CurrentVersionID)
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
	if version.Status != storage.ChallengeVersionPublished || version.Version != "3.6.2" {
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
	rules, err := repository.ListRewardRules(ctx, versionID)
	if err != nil {
		t.Fatal(err)
	}
	reward, err := repository.Rewards().DefinitionByKey(ctx, "mortal-path.world-seal")
	if err != nil {
		t.Fatal(err)
	}
	lawEssence, err := repository.Rewards().DefinitionByKey(ctx, "mortal-path.law-essence")
	if err != nil {
		t.Fatal(err)
	}
	spiritEssence, err := repository.Rewards().DefinitionByKey(ctx, "mortal-path.spirit-essence")
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 225 || len(outcomes) != 2 || len(rules) != 6 || reward.Kind != "badge" ||
		!lawEssence.Stackable || !lawEssence.Consumable || !spiritEssence.Stackable || !spiritEssence.Consumable {
		t.Fatalf(
			"incomplete mortal-path publication: definitions=%d outcomes=%d rules=%d rewards=%+v/%+v/%+v",
			len(definitions), len(outcomes), len(rules), reward, lawEssence, spiritEssence,
		)
	}
}
