package storage

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func TestPostgresConcurrentChallengePublishKeepsOneCurrentVersion(t *testing.T) {
	db := openGenerationTestDB(t)
	ctx := context.Background()
	packID, firstID, secondID := uuid.New(), uuid.New(), uuid.New()
	if err := db.Exec(`INSERT INTO challenge_packs (id, slug, name) VALUES (?, 'publish-test', 'Publish Test')`, packID).Error; err != nil {
		t.Fatal(err)
	}
	for index, versionID := range []uuid.UUID{firstID, secondID} {
		if err := db.Exec(`INSERT INTO challenge_versions
			(id, challenge_pack_id, version, status, manifest)
			VALUES (?, ?, ?, 'draft', '{}'::jsonb)`, versionID, packID, "1.0."+string(rune('0'+index))).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Exec(`INSERT INTO challenge_runtime_configs
			(challenge_version_id, engine_key, engine_version, mode, state_schema, action_config, settlement_config, presentation)
			VALUES (?, 'generic', 1, 'challenge', '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb)`, versionID).Error; err != nil {
			t.Fatal(err)
		}
	}

	repository := NewChallengeRepository(db)
	validator := func(ChallengePack, ChallengeVersion, ChallengeRuntimeConfig, []ChallengeAsset, []GameplayDefinition, []ChallengeOutcomeRule) (string, error) {
		return strings.Repeat("a", 64), nil
	}
	var wg sync.WaitGroup
	errorsByVersion := make(chan error, 2)
	for _, versionID := range []uuid.UUID{firstID, secondID} {
		wg.Add(1)
		go func(id uuid.UUID) {
			defer wg.Done()
			errorsByVersion <- repository.Publish(ctx, id, uuid.Nil, validator)
		}(versionID)
	}
	wg.Wait()
	close(errorsByVersion)
	for err := range errorsByVersion {
		if err != nil {
			t.Fatalf("concurrent publish failed: %v", err)
		}
	}

	var versions []ChallengeVersion
	if err := db.Where("id IN ?", []uuid.UUID{firstID, secondID}).Find(&versions).Error; err != nil {
		t.Fatal(err)
	}
	statusCounts := map[string]int{}
	var publishedID uuid.UUID
	for _, version := range versions {
		statusCounts[version.Status]++
		if version.Status == ChallengeVersionPublished {
			publishedID = version.ID
		}
	}
	if statusCounts[ChallengeVersionPublished] != 1 || statusCounts[ChallengeVersionRetired] != 1 {
		t.Fatalf("version statuses=%v", statusCounts)
	}
	pack, err := repository.GetPack(ctx, packID)
	if err != nil || pack.CurrentVersionID == nil || *pack.CurrentVersionID != publishedID {
		t.Fatalf("pack current version=%+v published=%s err=%v", pack.CurrentVersionID, publishedID, err)
	}
}
