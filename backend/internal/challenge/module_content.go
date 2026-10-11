package challenge

import (
	"encoding/json"
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
		content.Definitions = append(content.Definitions, gameplay.Definition{
			Kind: d.Kind, Key: d.Key, Name: d.Name, Content: map[string]any(d.Content),
			Tags: d.Tags, Visibility: d.Visibility, SortOrder: d.SortOrder,
		})
	}
	for _, o := range document.Outcomes {
		content.Outcomes = append(content.Outcomes, gameplay.Outcome{Key: o.OutcomeKey, Enabled: o.Enabled, Category: o.Category, RewardPoints: o.RewardPoints})
	}
	return content
}
