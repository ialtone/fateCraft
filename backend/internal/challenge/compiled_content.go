package challenge

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/gameplay"
	"github.com/ialtone/fatecraft/internal/platformcapability"
	"github.com/ialtone/fatecraft/internal/storage"
	"github.com/ialtone/fatecraft/internal/tokenizer"
)

type publicVersionProjection struct {
	icon            string
	tags            []string
	features        []string
	quickStartGuide string
	personas        []AssetOption
	outcomes        []OutcomeView
	rewards         []RewardView
	runtime         PublicRuntime
}

// compiledVersionContent is private so callers cannot mutate its indexes.
type compiledVersionContent struct {
	document     *VersionDocument
	contentHash  string
	manifest     Manifest
	contract     gameplay.RuntimeContract
	assetsByID   map[uuid.UUID]storage.ChallengeAsset
	assetsByKey  map[string]storage.ChallengeAsset
	characters   map[uuid.UUID]storage.Character
	personas     map[uuid.UUID]storage.Persona
	scenarios    map[uuid.UUID]storage.Scenario
	prompt       storage.PromptPreset
	worldBooks   []storage.WorldBook
	worldEntries []storage.WorldBookEntry
	inquiryRules []storage.InquiryRuleSource
	definitions  map[string]storage.GameplayDefinition
	public       publicVersionProjection
}

func compileVersionContent(doc *VersionDocument) (*compiledVersionContent, int64, error) {
	if doc == nil {
		return nil, 0, ErrInvalidChallenge
	}
	contentHash, err := ContentHashDocument(doc)
	if err != nil {
		return nil, 0, err
	}
	if doc.Version.ContentHash == "" || contentHash != doc.Version.ContentHash {
		return nil, 0, fmt.Errorf("%w: 挑战版本内容哈希不匹配", ErrInvalidChallenge)
	}
	manifest, err := decodeManifest(doc.Version.Manifest)
	if err != nil {
		return nil, 0, err
	}
	contract, err := gameplay.DecodeRuntime(doc.Runtime)
	if err != nil {
		return nil, 0, err
	}
	compiled := &compiledVersionContent{
		document: doc, contentHash: contentHash, manifest: manifest, contract: *contract,
		assetsByID:  make(map[uuid.UUID]storage.ChallengeAsset, len(doc.Assets)),
		assetsByKey: make(map[string]storage.ChallengeAsset, len(doc.Assets)),
		characters:  make(map[uuid.UUID]storage.Character), personas: make(map[uuid.UUID]storage.Persona),
		scenarios:   make(map[uuid.UUID]storage.Scenario),
		definitions: make(map[string]storage.GameplayDefinition, len(doc.Definitions)),
	}
	for _, definition := range doc.Definitions {
		compiled.definitions[definition.Kind+":"+definition.Key] = definition
	}
	for _, asset := range doc.Assets {
		compiled.assetsByID[asset.ID] = asset
		compiled.assetsByKey[asset.Kind+":"+asset.Slug] = asset
		switch asset.Kind {
		case storage.ChallengeAssetCharacter:
			var value storage.Character
			if err := json.Unmarshal(asset.Content, &value); err != nil {
				return nil, 0, fmt.Errorf("解析 Character %s: %w", asset.Slug, err)
			}
			compiled.characters[asset.ID] = value
		case storage.ChallengeAssetPersona:
			var value storage.Persona
			if err := json.Unmarshal(asset.Content, &value); err != nil {
				return nil, 0, fmt.Errorf("解析 Persona %s: %w", asset.Slug, err)
			}
			compiled.personas[asset.ID] = value
		case storage.ChallengeAssetScenario:
			var value storage.Scenario
			if err := json.Unmarshal(asset.Content, &value); err != nil {
				return nil, 0, fmt.Errorf("解析 Scenario %s: %w", asset.Slug, err)
			}
			compiled.scenarios[asset.ID] = value
		}
	}
	promptAsset, ok := compiled.assetsByKey[storage.ChallengeAssetPrompt+":"+manifest.DefaultConfig.RecommendedPrompt]
	if !ok {
		return nil, 0, fmt.Errorf("%w: 缺少 Prompt", ErrInvalidChallenge)
	}
	if err := json.Unmarshal(promptAsset.Content, &compiled.prompt); err != nil {
		return nil, 0, fmt.Errorf("解析 Prompt %s: %w", promptAsset.Slug, err)
	}
	sort.SliceStable(compiled.prompt.Segments, func(i, j int) bool {
		return compiled.prompt.Segments[i].Position < compiled.prompt.Segments[j].Position
	})
	for _, slug := range manifest.DefaultConfig.RequiredWorldBooks {
		asset, ok := compiled.assetsByKey[storage.ChallengeAssetWorldBook+":"+slug]
		if !ok {
			return nil, 0, fmt.Errorf("%w: 缺少世界书 %s", ErrInvalidChallenge, slug)
		}
		var book storage.WorldBook
		if err := json.Unmarshal(asset.Content, &book); err != nil {
			return nil, 0, fmt.Errorf("解析世界书 %s: %w", slug, err)
		}
		compiled.worldBooks = append(compiled.worldBooks, book)
		for _, entry := range book.Entries {
			if entry.Enabled {
				entry.Keywords = normalizeWorldbookKeywords(entry.Keywords)
				if entry.TokenCount == nil {
					tokens := tokenizer.NewHeuristic().Count(entry.Title + "\n" + entry.Content)
					entry.TokenCount = &tokens
				}
				compiled.worldEntries = append(compiled.worldEntries, entry)
			}
		}
	}
	compiled.inquiryRules = storage.VisibleInquiryRules(compiled.worldBooks)
	compiled.public, err = buildPublicVersionProjection(compiled)
	if err != nil {
		return nil, 0, err
	}
	estimated, err := json.Marshal(doc)
	if err != nil {
		return nil, 0, err
	}
	return compiled, int64(len(estimated)) * 2, nil
}

func normalizeWorldbookKeywords(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func buildPublicVersionProjection(compiled *compiledVersionContent) (publicVersionProjection, error) {
	manifest := compiled.manifest
	projection := publicVersionProjection{
		icon: manifest.Icon, tags: append([]string(nil), manifest.Tags...),
		features: append([]string(nil), manifest.Features...), quickStartGuide: manifest.QuickStartGuide,
	}
	allowed := stringSet(manifest.DefaultConfig.AllowedPersonas)
	for _, asset := range compiled.document.Assets {
		if asset.Kind != storage.ChallengeAssetPersona || (len(allowed) > 0 && !allowed[asset.Slug]) {
			continue
		}
		persona, ok := compiled.personas[asset.ID]
		if !ok {
			return projection, fmt.Errorf("%w: 缺少 Persona %s", ErrInvalidChallenge, asset.Slug)
		}
		projection.personas = append(projection.personas, AssetOption{ID: asset.ID, Slug: asset.Slug, Name: asset.Name, Description: persona.Description})
	}
	sort.Slice(projection.personas, func(i, j int) bool { return projection.personas[i].Name < projection.personas[j].Name })
	for _, outcome := range compiled.document.Outcomes {
		if outcome.Enabled {
			projection.outcomes = append(projection.outcomes, OutcomeView{Key: outcome.OutcomeKey, Category: outcome.Category, Title: outcome.Title, Tone: outcome.Tone, RewardPoints: outcome.RewardPoints})
			if outcome.RewardPoints > 0 {
				projection.rewards = append(projection.rewards, RewardView{Points: outcome.RewardPoints, OutcomeKey: outcome.OutcomeKey, OutcomeTitle: outcome.Title})
			}
		}
	}
	projection.runtime = PublicRuntime{
		EngineKey: compiled.contract.Engine.Key, Mode: gameplay.NormalizeRuntimeMode(compiled.contract.Mode),
		Presentation:         compiled.contract.Presentation,
		Capabilities:         publicGameplayCapabilities(compiled.contract.Actions.Capabilities),
		PlatformCapabilities: platformcapability.PublicCapabilities(), Outcomes: append([]OutcomeView(nil), projection.outcomes...),
	}
	return projection, nil
}

func cloneVersionDocument(source *VersionDocument) (*VersionDocument, error) {
	encoded, err := json.Marshal(source)
	if err != nil {
		return nil, err
	}
	var clone VersionDocument
	if err := json.Unmarshal(encoded, &clone); err != nil {
		return nil, err
	}
	return &clone, nil
}

func cloneManifest(source Manifest) Manifest {
	clone := source
	clone.Tags = append([]string(nil), source.Tags...)
	clone.Features = append([]string(nil), source.Features...)
	clone.Dependencies = append([]string(nil), source.Dependencies...)
	clone.DefaultConfig.RequiredWorldBooks = append([]string(nil), source.DefaultConfig.RequiredWorldBooks...)
	clone.DefaultConfig.AllowedPersonas = append([]string(nil), source.DefaultConfig.AllowedPersonas...)
	clone.Outcomes = append([]OutcomeSpec(nil), source.Outcomes...)
	return clone
}

func clonePrompt(source *storage.PromptPreset) *storage.PromptPreset {
	if source == nil {
		return nil
	}
	clone := *source
	clone.Segments = append([]storage.PromptSegment(nil), source.Segments...)
	return &clone
}

func cloneWorldEntries(source []storage.WorldBookEntry) []storage.WorldBookEntry {
	clone := make([]storage.WorldBookEntry, len(source))
	for index, entry := range source {
		clone[index] = entry
		clone[index].Keywords = append([]string(nil), entry.Keywords...)
	}
	return clone
}

func clonePublicRuntime(source PublicRuntime) PublicRuntime {
	clone := source
	clone.Capabilities = publicGameplayCapabilities(source.Capabilities)
	clone.PlatformCapabilities = platformcapability.PublicCapabilities()
	clone.Outcomes = append([]OutcomeView(nil), source.Outcomes...)
	return clone
}

// InquiryRules implements storage.InquiryRuleProvider using the same compiled
// worldbook source as narrative context assembly.
func (s *Service) InquiryRules(ctx context.Context, versionID uuid.UUID) ([]storage.InquiryRuleSource, error) {
	compiled, err := s.compiledVersion(ctx, versionID)
	if err != nil {
		return nil, err
	}
	return append([]storage.InquiryRuleSource(nil), compiled.inquiryRules...), nil
}

// DefinitionsForVersion implements storage.GameplayDefinitionProvider. The
// campaign hash is checked before immutable definitions cross the transaction
// boundary.
func (s *Service) DefinitionsForVersion(ctx context.Context, versionID uuid.UUID, contentHash string) (map[string]storage.GameplayDefinition, error) {
	compiled, err := s.compiledVersion(ctx, versionID)
	if err != nil {
		return nil, err
	}
	if contentHash == "" || compiled.contentHash != contentHash {
		return nil, fmt.Errorf("%w: 固定挑战版本不可用或内容校验失败", ErrInvalidChallenge)
	}
	return cloneGameplayDefinitions(compiled.definitions), nil
}

func cloneGameplayDefinitions(source map[string]storage.GameplayDefinition) map[string]storage.GameplayDefinition {
	clone := make(map[string]storage.GameplayDefinition, len(source))
	for key, definition := range source {
		definition.Tags = append([]string(nil), definition.Tags...)
		definition.Content = cloneDefinitionJSONMap(definition.Content)
		clone[key] = definition
	}
	return clone
}

func cloneDefinitionJSONMap(source storage.JSONMap) storage.JSONMap {
	if source == nil {
		return nil
	}
	clone := make(storage.JSONMap, len(source))
	for key, value := range source {
		clone[key] = cloneDefinitionJSONValue(value)
	}
	return clone
}

func cloneDefinitionJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		clone := make(map[string]any, len(typed))
		for key, child := range typed {
			clone[key] = cloneDefinitionJSONValue(child)
		}
		return clone
	case []any:
		clone := make([]any, len(typed))
		for index, child := range typed {
			clone[index] = cloneDefinitionJSONValue(child)
		}
		return clone
	case []string:
		return append([]string(nil), typed...)
	default:
		return value
	}
}
