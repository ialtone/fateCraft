package challenge

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/storage"
)

func moduleContent(runtime *gameplay.RuntimeContract, document *VersionDocument) gameplay.Content {
	content := gameplay.Content{Runtime: runtime}
	var manifest Manifest
	if json.Unmarshal(document.Version.Manifest, &manifest) == nil {
		content.PersonaKeys = append(content.PersonaKeys, manifest.DefaultConfig.AllowedPersonas...)
	}
	if len(content.PersonaKeys) == 0 {
		for _, asset := range document.Assets {
			if asset.Kind == storage.ChallengeAssetPersona {
				content.PersonaKeys = append(content.PersonaKeys, asset.Slug)
			}
		}
	}
	for _, d := range document.Definitions {
		definition := gameplay.Definition{
			Kind: d.Kind, Key: d.Key, Name: d.Name, Content: map[string]any(d.Content),
			Tags: d.Tags, Visibility: d.Visibility, SortOrder: d.SortOrder,
		}
		if gameplay.IsPlatformDefinitionKind(d.Kind) {
			content.PlatformDefinitions = append(content.PlatformDefinitions, definition)
			continue
		}
		content.Definitions = append(content.Definitions, definition)
	}
	for _, o := range document.Outcomes {
		content.Outcomes = append(content.Outcomes, gameplay.Outcome{Key: o.OutcomeKey, Enabled: o.Enabled, Category: o.Category, RewardPoints: o.RewardPoints})
	}
	return content
}

// moduleContentForVersion reads the pinned runtime and definitions through the
// caller's repository (usually a transaction) without touching the global
// connection pool.
func moduleContentForVersion(ctx context.Context, repo *storage.ChallengeRepository, versionID uuid.UUID) (gameplay.Content, error) {
	runtime, err := repo.GetRuntime(ctx, versionID)
	if err != nil {
		return gameplay.Content{}, err
	}
	contract, err := storage.DecodeGameplayRuntime(*runtime)
	if err != nil {
		return gameplay.Content{}, err
	}
	definitions, err := repo.ListDefinitions(ctx, versionID)
	if err != nil {
		return gameplay.Content{}, err
	}
	return moduleContent(contract, &VersionDocument{Definitions: definitions}), nil
}
