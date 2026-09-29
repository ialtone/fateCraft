package challenge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/campaignreward"
	"github.com/ialtone/fatecraft/internal/gameplay"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/craftingrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/effectrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/encounterrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/factionrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/gatheringrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/marketrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/worldrules"
	"github.com/ialtone/fatecraft/internal/operation"
	"github.com/ialtone/fatecraft/internal/platformcapability"
	"github.com/ialtone/fatecraft/internal/requirement"
	"github.com/ialtone/fatecraft/internal/storage"
)

var (
	ErrInvalidChallenge       = errors.New("挑战内容无效")
	ErrNotPublished           = errors.New("挑战尚未发布")
	ErrInvalidSelection       = errors.New("所选内容不属于该挑战")
	ErrSettlementDenied       = errors.New("挑战结算条件未满足")
	ErrConversationEnded      = errors.New("挑战对话已结束")
	ErrEntryRequirementNotMet = errors.New("挑战前置条件未满足")
)

type Service struct {
	repo         *storage.ChallengeRepository
	registry     *gameplay.Registry
	versionCache *versionContentCache
}

func NewService(repo *storage.ChallengeRepository, registries ...*gameplay.Registry) *Service {
	return NewServiceWithCache(repo, CacheConfig{}, registries...)
}

func NewServiceWithCache(repo *storage.ChallengeRepository, cacheConfig CacheConfig, registries ...*gameplay.Registry) *Service {
	registry := gameplay.NewCoreRegistry()
	if len(registries) > 0 && registries[0] != nil {
		registry = registries[0]
	}
	return &Service{
		repo:         repo,
		registry:     registry,
		versionCache: newVersionContentCache(cacheConfig),
	}
}

func (s *Service) Registry() *gameplay.Registry { return s.registry }

// SlugForVersion returns the stable challenge slug owning a published or
// retired version. It is used when forking a historical conversation onto the
// pack's current version.
func (s *Service) SlugForVersion(ctx context.Context, versionID uuid.UUID) (string, error) {
	version, err := s.repo.GetVersion(ctx, versionID)
	if err != nil {
		return "", err
	}
	pack, err := s.repo.GetPack(ctx, version.ChallengePackID)
	if err != nil {
		return "", err
	}
	return pack.Slug, nil
}

// Asset returns a version-pinned asset. Callers should still validate that
// the asset belongs to the source conversation's version before using its
// slug for a new selection.
func (s *Service) Asset(ctx context.Context, id uuid.UUID) (*storage.ChallengeAsset, error) {
	return s.repo.GetAsset(ctx, id)
}

// IsCurrentVersion reports whether a version is the pack's currently
// published version. Retired versions remain readable for historical
// conversations but cannot accept new narrative turns.
func (s *Service) IsCurrentVersion(ctx context.Context, versionID uuid.UUID) (bool, error) {
	version, err := s.repo.GetVersion(ctx, versionID)
	if err != nil {
		return false, err
	}
	pack, err := s.repo.GetPack(ctx, version.ChallengePackID)
	if err != nil {
		return false, err
	}
	return version.Status == storage.ChallengeVersionPublished &&
		pack.Status == storage.ChallengePackActive && pack.CurrentVersionID != nil &&
		*pack.CurrentVersionID == versionID, nil
}

func (s *Service) CreatePack(ctx context.Context, creatorID uuid.UUID, in CreatePackInput) (*storage.ChallengePack, error) {
	in.Slug = strings.TrimSpace(in.Slug)
	in.Name = strings.TrimSpace(in.Name)
	if in.Slug == "" || in.Name == "" || !validSlug(in.Slug) {
		return nil, fmt.Errorf("%w: slug 或名称无效", ErrInvalidChallenge)
	}
	now := time.Now()
	pack := &storage.ChallengePack{
		ID: uuid.New(), Slug: in.Slug, Name: in.Name, Description: strings.TrimSpace(in.Description),
		Status: storage.ChallengePackHidden, CreatedBy: &creatorID, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repo.CreatePack(ctx, pack); err != nil {
		return nil, err
	}
	return pack, nil
}

func (s *Service) ListPublished(ctx context.Context, playerIDs ...uuid.UUID) ([]PublicChallenge, error) {
	packs, err := s.repo.ListPacks(ctx, true)
	if err != nil {
		return nil, err
	}
	out := make([]PublicChallenge, 0, len(packs))
	for index := range packs {
		item, err := s.publicChallenge(ctx, &packs[index], playerIDs...)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, nil
}

func (s *Service) GetPublished(ctx context.Context, slug string, playerIDs ...uuid.UUID) (*PublicChallenge, error) {
	pack, err := s.repo.GetPackBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	if pack.Status != storage.ChallengePackActive || pack.CurrentVersionID == nil {
		return nil, ErrNotPublished
	}
	return s.publicChallenge(ctx, pack, playerIDs...)
}

func (s *Service) publicChallenge(ctx context.Context, pack *storage.ChallengePack, playerIDs ...uuid.UUID) (*PublicChallenge, error) {
	if pack.CurrentVersionID == nil {
		return nil, ErrNotPublished
	}
	compiled, err := s.compiledVersion(ctx, *pack.CurrentVersionID)
	if err != nil {
		return nil, err
	}
	if compiled.document.Version.Status != storage.ChallengeVersionPublished {
		return nil, ErrNotPublished
	}
	manifest := compiled.manifest
	projection := compiled.public
	available := true
	unavailableReason := ""
	if len(manifest.EntryRequirement) > 0 && len(playerIDs) > 0 {
		if err := s.checkEntryRequirement(ctx, playerIDs[0], manifest.EntryRequirement); err != nil {
			available = false
			unavailableReason = "尚未满足挑战前置条件"
		}
	}
	return &PublicChallenge{
		ID: pack.ID, Slug: pack.Slug, Name: pack.Name, Description: pack.Description,
		Version: compiled.document.Version.Version, Icon: projection.icon, Tags: append([]string(nil), projection.tags...),
		Features: append([]string(nil), projection.features...), QuickStartGuide: projection.quickStartGuide,
		Personas: append([]AssetOption(nil), projection.personas...), Outcomes: append([]OutcomeView(nil), projection.outcomes...),
		RewardOverview: append([]RewardView(nil), projection.rewards...), Runtime: clonePublicRuntime(projection.runtime),
		Available: available, UnavailableReason: unavailableReason,
	}, nil
}

func (s *Service) ResolveSelection(ctx context.Context, challengeSlug, personaSlug string, playerIDs ...uuid.UUID) (*Selection, error) {
	pack, err := s.repo.GetPackBySlug(ctx, challengeSlug)
	if err != nil {
		return nil, err
	}
	if pack.Status != storage.ChallengePackActive || pack.CurrentVersionID == nil {
		return nil, ErrNotPublished
	}
	compiled, err := s.compiledVersion(ctx, *pack.CurrentVersionID)
	if err != nil {
		return nil, err
	}
	doc := compiled.document
	if doc.Version.Status != storage.ChallengeVersionPublished || doc.Version.ContentHash == "" {
		return nil, ErrNotPublished
	}
	manifest := compiled.manifest
	if len(manifest.EntryRequirement) > 0 {
		if len(playerIDs) == 0 {
			return nil, ErrEntryRequirementNotMet
		}
		if err := s.checkEntryRequirement(ctx, playerIDs[0], manifest.EntryRequirement); err != nil {
			return nil, ErrEntryRequirementNotMet
		}
	}
	selectedPersona := strings.TrimSpace(personaSlug)
	if selectedPersona == "" {
		selectedPersona = manifest.DefaultConfig.RecommendedPersona
	}
	if allowed := stringSet(manifest.DefaultConfig.AllowedPersonas); len(allowed) > 0 && !allowed[selectedPersona] {
		return nil, ErrInvalidSelection
	}
	selection := &Selection{
		ChallengeName: pack.Name,
		Version:       doc.Version,
		Runtime:       doc.Runtime,
		Contract:      compiled.contract,
	}
	for kind, slug := range map[string]string{
		storage.ChallengeAssetCharacter: manifest.DefaultConfig.RecommendedCharacter,
		storage.ChallengeAssetPersona:   selectedPersona,
		storage.ChallengeAssetScenario:  manifest.DefaultConfig.RecommendedScenario,
	} {
		if slug == "" {
			continue
		}
		asset, ok := compiled.assetsByKey[kind+":"+slug]
		if !ok {
			return nil, ErrInvalidSelection
		}
		switch kind {
		case storage.ChallengeAssetCharacter:
			value := asset
			selection.Character = &value
		case storage.ChallengeAssetPersona:
			value := asset
			selection.Persona = &value
		case storage.ChallengeAssetScenario:
			value := asset
			selection.Scenario = &value
		}
	}
	return selection, nil
}

func (s *Service) checkEntryRequirement(ctx context.Context, playerID uuid.UUID, raw map[string]any) error {
	rule, err := requirement.Decode(raw)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidChallenge, err)
	}
	return requirement.NewEvaluator(s.repo.Rewards()).Check(ctx, playerID, rule)
}

func (s *Service) Bundle(ctx context.Context, conv *storage.Conversation) (*Bundle, error) {
	if conv == nil {
		return nil, ErrInvalidChallenge
	}
	compiled, err := s.compiledVersion(ctx, conv.ChallengeVersionID)
	if err != nil {
		return nil, err
	}
	doc := compiled.document
	versionUsable := doc.Version.Status == storage.ChallengeVersionPublished ||
		doc.Version.Status == storage.ChallengeVersionRetired
	if !versionUsable || compiled.contentHash != conv.ChallengeHash {
		return nil, fmt.Errorf("%w: 对话绑定的挑战版本不可用或内容校验失败", ErrInvalidChallenge)
	}
	bundle := &Bundle{
		Version: doc.Version, Runtime: doc.Runtime, Contract: compiled.contract,
		Manifest: cloneManifest(compiled.manifest), Prompt: clonePrompt(&compiled.prompt),
		WorldEntries: cloneWorldEntries(compiled.worldEntries),
	}
	selectedAsset := func(id uuid.UUID, kind string) (storage.ChallengeAsset, error) {
		asset, ok := compiled.assetsByID[id]
		if !ok || asset.ChallengeVersionID != doc.Version.ID || asset.Kind != kind {
			return storage.ChallengeAsset{}, ErrInvalidSelection
		}
		return asset, nil
	}
	if conv.CharacterAssetID != nil {
		asset, err := selectedAsset(*conv.CharacterAssetID, storage.ChallengeAssetCharacter)
		if err != nil {
			return nil, err
		}
		value, ok := compiled.characters[asset.ID]
		if !ok {
			return nil, ErrInvalidSelection
		}
		value.Tags = append([]string(nil), value.Tags...)
		bundle.Character = &value
	}
	if conv.PersonaAssetID != nil {
		asset, err := selectedAsset(*conv.PersonaAssetID, storage.ChallengeAssetPersona)
		if err != nil {
			return nil, err
		}
		value, ok := compiled.personas[asset.ID]
		if !ok {
			return nil, ErrInvalidSelection
		}
		bundle.Persona = &value
	}
	if conv.ScenarioAssetID != nil {
		asset, err := selectedAsset(*conv.ScenarioAssetID, storage.ChallengeAssetScenario)
		if err != nil {
			return nil, err
		}
		value, ok := compiled.scenarios[asset.ID]
		if !ok {
			return nil, ErrInvalidSelection
		}
		bundle.Scenario = &value
	}
	return bundle, nil
}

func (s *Service) RuntimeForConversation(ctx context.Context, convID uuid.UUID) (*gameplay.RuntimeContract, error) {
	conv, err := s.repo.Conversation(ctx, convID)
	if err != nil {
		return nil, err
	}
	compiled, err := s.compiledVersion(ctx, conv.ChallengeVersionID)
	if err != nil {
		return nil, err
	}
	usable := compiled.document.Version.Status == storage.ChallengeVersionPublished ||
		compiled.document.Version.Status == storage.ChallengeVersionRetired
	if !usable || conv.ChallengeHash == "" || compiled.contentHash != conv.ChallengeHash {
		return nil, fmt.Errorf("%w: 对话绑定的挑战版本不可用或内容校验失败", ErrInvalidChallenge)
	}
	return &compiled.contract, nil
}

// RuntimeForVersion 返回固定版本的运行时，并重新验证版本状态与完整内容哈希。
// Campaign 创建后只按该固定版本继续，不再解析挑战包的 current_version_id。
func (s *Service) RuntimeForVersion(ctx context.Context, versionID uuid.UUID, contentHash string) (*gameplay.RuntimeContract, error) {
	compiled, err := s.compiledVersion(ctx, versionID)
	if err != nil {
		return nil, err
	}
	doc := compiled.document
	usable := doc.Version.Status == storage.ChallengeVersionPublished ||
		doc.Version.Status == storage.ChallengeVersionRetired
	if !usable || contentHash == "" || compiled.contentHash != contentHash {
		return nil, fmt.Errorf("%w: 固定挑战版本不可用或内容校验失败", ErrInvalidChallenge)
	}
	return &compiled.contract, nil
}

func (s *Service) PublicRuntimeForConversation(ctx context.Context, convID uuid.UUID, playerID string) (*PublicRuntime, error) {
	conv, err := s.repo.Conversation(ctx, convID)
	if err != nil {
		return nil, err
	}
	if conv.PlayerID != playerID {
		return nil, storage.ErrNotFound
	}
	compiled, err := s.compiledVersion(ctx, conv.ChallengeVersionID)
	if err != nil {
		return nil, err
	}
	usable := compiled.document.Version.Status == storage.ChallengeVersionPublished ||
		compiled.document.Version.Status == storage.ChallengeVersionRetired
	if !usable || conv.ChallengeHash == "" || compiled.contentHash != conv.ChallengeHash {
		return nil, fmt.Errorf("%w: 对话绑定的挑战版本不可用或内容校验失败", ErrInvalidChallenge)
	}
	runtime := clonePublicRuntime(compiled.public.runtime)
	return &runtime, nil
}

func publicGameplayCapabilities(source map[string]gameplay.Capability) map[string]gameplay.Capability {
	result := make(map[string]gameplay.Capability, len(source))
	for key, capability := range source {
		if !platformcapability.IsReservedKey(key) {
			result[key] = capability
		}
	}
	return result
}

func (s *Service) ListAdmin(ctx context.Context) ([]AdminPack, error) {
	packs, err := s.repo.ListPacks(ctx, false)
	if err != nil {
		return nil, err
	}
	out := make([]AdminPack, 0, len(packs))
	for _, pack := range packs {
		versions, err := s.repo.ListVersions(ctx, pack.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, AdminPack{Pack: pack, Versions: versions})
	}
	return out, nil
}

func (s *Service) VersionDocument(ctx context.Context, versionID uuid.UUID) (*VersionDocument, error) {
	entry, err := s.versionContent(ctx, versionID)
	if err != nil {
		return nil, err
	}
	return cloneVersionDocument(entry.document)
}

func (s *Service) versionContent(ctx context.Context, versionID uuid.UUID) (*versionCacheEntry, error) {
	cached, flight, leader := s.versionCache.acquire(versionID)
	if cached != nil {
		return cached, nil
	}
	if !leader {
		return s.versionCache.wait(ctx, flight)
	}
	loadStarted := time.Now()
	version, err := s.repo.GetVersion(ctx, versionID)
	if err != nil {
		s.versionCache.finishLoad(versionID, flight, nil, err, time.Since(loadStarted), 0)
		return nil, err
	}
	runtime, err := s.repo.GetRuntime(ctx, versionID)
	if err != nil {
		s.versionCache.finishLoad(versionID, flight, nil, err, time.Since(loadStarted), 0)
		return nil, err
	}
	assets, err := s.repo.ListAssets(ctx, versionID)
	if err != nil {
		s.versionCache.finishLoad(versionID, flight, nil, err, time.Since(loadStarted), 0)
		return nil, err
	}
	definitions, err := s.repo.ListDefinitions(ctx, versionID)
	if err != nil {
		s.versionCache.finishLoad(versionID, flight, nil, err, time.Since(loadStarted), 0)
		return nil, err
	}
	outcomes, err := s.repo.ListOutcomeRules(ctx, versionID)
	if err != nil {
		s.versionCache.finishLoad(versionID, flight, nil, err, time.Since(loadStarted), 0)
		return nil, err
	}
	rules, err := s.repo.ListRewardRules(ctx, versionID)
	if err != nil {
		s.versionCache.finishLoad(versionID, flight, nil, err, time.Since(loadStarted), 0)
		return nil, err
	}
	doc := &VersionDocument{
		Version: *version, Runtime: *runtime, Assets: assets, Definitions: definitions,
		Outcomes: outcomes, RewardRules: rules,
	}
	entry := &versionCacheEntry{document: doc}
	compileDuration := time.Duration(0)
	if version.Status == storage.ChallengeVersionPublished || version.Status == storage.ChallengeVersionRetired {
		compileStarted := time.Now()
		compiled, estimatedBytes, compileErr := compileVersionContent(doc)
		compileDuration = time.Since(compileStarted)
		if compileErr != nil {
			s.versionCache.finishLoad(versionID, flight, nil, compileErr, time.Since(loadStarted), compileDuration)
			return nil, compileErr
		}
		entry.key = versionCacheKey{id: versionID, hash: compiled.contentHash}
		entry.compiled = compiled
		entry.bytes = estimatedBytes
	}
	s.versionCache.finishLoad(versionID, flight, entry, nil, time.Since(loadStarted), compileDuration)
	return entry, nil
}

func (s *Service) compiledVersion(ctx context.Context, versionID uuid.UUID) (*compiledVersionContent, error) {
	entry, err := s.versionContent(ctx, versionID)
	if err != nil {
		return nil, err
	}
	if entry.compiled == nil {
		return nil, ErrNotPublished
	}
	return entry.compiled, nil
}

func (s *Service) CacheStats() CacheStats {
	return s.versionCache.stats()
}

func (s *Service) SaveDraft(ctx context.Context, versionID uuid.UUID, in SaveVersionInput) (*VersionDocument, error) {
	if !json.Valid(in.Manifest) {
		return nil, fmt.Errorf("%w: manifest 不是合法 JSON", ErrInvalidChallenge)
	}
	if err := validateAssetDocuments(in.Assets); err != nil {
		return nil, err
	}
	if err := validateDefinitionDocuments(in.Definitions); err != nil {
		return nil, err
	}
	in.Runtime.Mode = gameplay.NormalizeRuntimeMode(in.Runtime.Mode)
	if in.Runtime.Mode == gameplay.RuntimeModeChallenge {
		in.Runtime.CampaignConfig = nil
	}
	in.Runtime.ChallengeVersionID = versionID
	for index := range in.RewardRules {
		definition, err := s.repo.RewardDefinition(ctx, in.RewardRules[index].RewardDefinitionID)
		if err != nil {
			return nil, fmt.Errorf("%w: 奖励定义不存在", ErrInvalidChallenge)
		}
		in.RewardRules[index].PublicSnapshot = rewardSnapshot(*definition)
		if definition.Disabled {
			return nil, fmt.Errorf("%w: 奖励已停用", ErrInvalidChallenge)
		}
	}
	if err := s.repo.ReplaceDraft(
		ctx, versionID, storage.JSONDoc(in.Manifest), in.Runtime, in.Assets, in.Definitions, in.Outcomes, in.RewardRules,
	); err != nil {
		return nil, err
	}
	return s.VersionDocument(ctx, versionID)
}

func (s *Service) Publish(ctx context.Context, versionID, publisherID uuid.UUID) (*VersionDocument, error) {
	err := s.repo.Publish(ctx, versionID, publisherID, func(
		pack storage.ChallengePack,
		version storage.ChallengeVersion,
		runtime storage.ChallengeRuntimeConfig,
		assets []storage.ChallengeAsset,
		definitions []storage.GameplayDefinition,
		outcomes []storage.ChallengeOutcomeRule,
		rules []storage.ChallengeRewardRule,
	) (string, error) {
		doc := &VersionDocument{
			Version: version, Runtime: runtime, Assets: assets, Definitions: definitions,
			Outcomes: outcomes, RewardRules: rules,
		}
		if err := s.ValidateDocument(ctx, doc); err != nil {
			return "", err
		}
		manifest, err := decodeManifest(version.Manifest)
		if err != nil {
			return "", err
		}
		if manifest.ID != pack.Slug || manifest.Name != pack.Name || manifest.Description != pack.Description {
			return "", fmt.Errorf("%w: manifest 的 id、name、description 必须与挑战包一致", ErrInvalidChallenge)
		}
		return ContentHashDocument(doc)
	})
	if err != nil {
		return nil, err
	}
	return s.VersionDocument(ctx, versionID)
}

// DeleteVersion 删除无任何对话或存档引用的版本（草稿、已发布、退役均可，
// 只要没被用过）。被引用时返回包装后的 storage.ErrVersionInUse。
func (s *Service) DeleteVersion(ctx context.Context, versionID uuid.UUID) error {
	return s.repo.DeleteVersionIfUnused(ctx, versionID)
}

func (s *Service) CreateDraft(ctx context.Context, packID, creatorID uuid.UUID, versionName string) (*VersionDocument, error) {
	versionName = strings.TrimSpace(versionName)
	if versionName == "" {
		return nil, fmt.Errorf("%w: version 不能为空", ErrInvalidChallenge)
	}
	pack, err := s.repo.GetPack(ctx, packID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	version := &storage.ChallengeVersion{
		ID: uuid.New(), ChallengePackID: pack.ID, Version: versionName,
		Status: storage.ChallengeVersionDraft, Manifest: storage.JSONDoc("{}"),
		ContentHash: "", CreatedBy: &creatorID, CreatedAt: now, UpdatedAt: now,
	}
	runtime, err := defaultDraftRuntime(version.ID)
	if err != nil {
		return nil, err
	}
	var assets []storage.ChallengeAsset
	var definitions []storage.GameplayDefinition
	var outcomes []storage.ChallengeOutcomeRule
	var rules []storage.ChallengeRewardRule
	if pack.CurrentVersionID != nil {
		source, err := s.VersionDocument(ctx, *pack.CurrentVersionID)
		if err != nil {
			return nil, err
		}
		manifest, err := decodeManifest(source.Version.Manifest)
		if err != nil {
			return nil, err
		}
		manifest.Version = versionName
		manifest.Outcomes = nil
		manifest.RewardRules = nil
		manifestJSON, err := json.Marshal(manifest)
		if err != nil {
			return nil, err
		}
		version.Manifest = storage.JSONDoc(manifestJSON)
		runtime = source.Runtime
		runtime.ChallengeVersionID = version.ID
		runtime.CreatedAt, runtime.UpdatedAt = now, now
		assets = source.Assets
		definitions = source.Definitions
		outcomes = source.Outcomes
		rules = source.RewardRules
		outcomeIDs := map[uuid.UUID]uuid.UUID{}
		for index := range assets {
			assets[index].ID = uuid.New()
			assets[index].ChallengeVersionID = version.ID
			assets[index].CreatedAt, assets[index].UpdatedAt = now, now
		}
		for index := range definitions {
			definitions[index].ID = uuid.New()
			definitions[index].ChallengeVersionID = version.ID
			definitions[index].CreatedAt, definitions[index].UpdatedAt = now, now
		}
		for index := range outcomes {
			oldID := outcomes[index].ID
			outcomes[index].ID = uuid.New()
			outcomeIDs[oldID] = outcomes[index].ID
			outcomes[index].ChallengeVersionID = version.ID
			outcomes[index].CreatedAt, outcomes[index].UpdatedAt = now, now
		}
		for index := range rules {
			rules[index].ID = uuid.New()
			rules[index].ChallengeVersionID = version.ID
			rules[index].OutcomeRuleID = outcomeIDs[rules[index].OutcomeRuleID]
			rules[index].CreatedAt, rules[index].UpdatedAt = now, now
		}
	}
	if err := s.repo.CreateDraftWithContent(ctx, version, runtime, assets, definitions, outcomes, rules); err != nil {
		return nil, err
	}
	return s.VersionDocument(ctx, version.ID)
}

type settlementReader interface {
	GetRun(context.Context, uuid.UUID) (*storage.ChallengeRun, error)
	OutcomeRule(context.Context, uuid.UUID, string) (*storage.ChallengeOutcomeRule, error)
	RewardRulesForOutcome(context.Context, uuid.UUID) ([]storage.ChallengeRewardRule, error)
	RewardDefinition(context.Context, uuid.UUID) (*storage.RewardDefinition, error)
	SuccessfulRollKeys(context.Context, uuid.UUID) ([]string, error)
	GetRuntime(context.Context, uuid.UUID) (*storage.ChallengeRuntimeConfig, error)
}

type settlementEvaluation struct {
	outcome storage.ChallengeOutcomeRule
	grants  []storage.ChallengeRewardGrant
	detail  storage.JSONMap
}

func (s *Service) ValidateSettlement(ctx context.Context, in SettlementInput) error {
	return s.ValidateSettlementWithRepository(ctx, s.repo, in)
}

// ValidateSettlementWithRepository 用调用方提供的仓储（通常绑定在事务上）做结算预校验。
func (s *Service) ValidateSettlementWithRepository(ctx context.Context, repo *storage.ChallengeRepository, in SettlementInput) error {
	if repo == nil {
		return errors.New("结算校验仓库未配置")
	}
	if err := validateSettlementInput(in); err != nil {
		return err
	}
	conv, err := repo.Conversation(ctx, in.ConversationID)
	if err != nil {
		return err
	}
	_, err = s.evaluateSettlement(ctx, repo, conv, in)
	return err
}

func (s *Service) Settle(ctx context.Context, in SettlementInput) (*SettlementResult, error) {
	if err := validateSettlementInput(in); err != nil {
		return nil, err
	}
	if in.SettledAt.IsZero() {
		in.SettledAt = time.Now()
	}
	var settled storage.ChallengeRun
	var rewards []storage.ChallengeRunReward
	var settledOutcome OutcomeView
	var disabledReason string
	err := s.repo.WithTx(ctx, func(tx *storage.ChallengeRepository) error {
		var err error
		settled, rewards, settledOutcome, disabledReason, err = s.settleInRepo(ctx, tx, in)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &SettlementResult{Run: settled, Outcome: settledOutcome, Rewards: rewards, RewardsDisabledReason: disabledReason}, nil
}

// SettleWithRepository runs settlement on a repository bound to an existing
// transaction. It is the atomic path used by narrative tool execution.
func (s *Service) SettleWithRepository(ctx context.Context, repo *storage.ChallengeRepository, in SettlementInput) (*SettlementResult, error) {
	if repo == nil {
		return nil, errors.New("结算事务仓库未配置")
	}
	if err := validateSettlementInput(in); err != nil {
		return nil, err
	}
	settled, rewards, outcome, disabledReason, err := s.settleInRepo(ctx, repo, in)
	if err != nil {
		return nil, err
	}
	return &SettlementResult{Run: settled, Outcome: outcome, Rewards: rewards, RewardsDisabledReason: disabledReason}, nil
}

func (s *Service) settleInRepo(ctx context.Context, tx *storage.ChallengeRepository, in SettlementInput) (storage.ChallengeRun, []storage.ChallengeRunReward, OutcomeView, string, error) {
	if err := validateSettlementInput(in); err != nil {
		return storage.ChallengeRun{}, nil, OutcomeView{}, "", err
	}
	conv, err := tx.LockConversation(ctx, in.ConversationID)
	if err != nil {
		return storage.ChallengeRun{}, nil, OutcomeView{}, "", err
	}
	evaluation, err := s.evaluateSettlement(ctx, tx, conv, in)
	if err != nil {
		return storage.ChallengeRun{}, nil, OutcomeView{}, "", err
	}
	// client 路由：照常校验结局、写 challenge_runs 并结束对话，但跳过全部奖励授予。
	rewardsDisabledReason := ""
	if conv.LLMRoute == storage.LLMRouteClient {
		rewardsDisabledReason = storage.RewardsDisabledReasonClientRoute
		evaluation.detail[storage.RewardsDisabledReasonKey] = rewardsDisabledReason
	}
	settled := storage.ChallengeRun{
		ID: uuid.New(), ConversationID: conv.ID, PlayerID: in.PlayerID,
		ChallengeVersionID: conv.ChallengeVersionID,
		OutcomeRuleID:      evaluation.outcome.ID, OutcomeKey: evaluation.outcome.OutcomeKey,
		Category: evaluation.outcome.Category, TitleSnapshot: evaluation.outcome.Title,
		Summary: strings.TrimSpace(in.Summary), SettlementDetail: evaluation.detail,
		SettledAt: in.SettledAt,
	}
	if metadata, ok := operation.FromContext(ctx); ok {
		settled.TurnID = &metadata.TurnID
		settled.AttemptID = &metadata.AttemptID
	}
	settledOutcome := OutcomeView{
		Key: evaluation.outcome.OutcomeKey, Category: evaluation.outcome.Category,
		Title: evaluation.outcome.Title, Tone: evaluation.outcome.Tone,
	}
	rewards, err := tx.CompleteRun(ctx, conv, &settled, evaluation.grants)
	if err != nil {
		return storage.ChallengeRun{}, nil, OutcomeView{}, "", err
	}
	return settled, rewards, settledOutcome, rewardsDisabledReason, nil
}

func validateSettlementInput(in SettlementInput) error {
	if strings.TrimSpace(in.OutcomeKey) == "" {
		return fmt.Errorf("%w: outcome_key 不能为空", ErrSettlementDenied)
	}
	if in.PlayerID == uuid.Nil || in.ConversationID == uuid.Nil {
		return fmt.Errorf("%w: 结算身份或对话无效", ErrSettlementDenied)
	}
	return nil
}

func (s *Service) evaluateSettlement(
	ctx context.Context,
	repo settlementReader,
	conv *storage.Conversation,
	in SettlementInput,
) (*settlementEvaluation, error) {
	if conv.PlayerID != in.PlayerID.String() {
		return nil, storage.ErrNotFound
	}
	if conv.Status != storage.StatusActive {
		if _, err := repo.GetRun(ctx, conv.ID); err == nil {
			return nil, storage.ErrAlreadySettled
		} else if !errors.Is(err, storage.ErrNotFound) {
			return nil, err
		}
		return nil, ErrConversationEnded
	}
	outcome, err := repo.OutcomeRule(ctx, conv.ChallengeVersionID, in.OutcomeKey)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, fmt.Errorf("%w: 结局规则不存在", ErrSettlementDenied)
		}
		return nil, err
	}
	runtime, err := repo.GetRuntime(ctx, conv.ChallengeVersionID)
	if err != nil {
		return nil, err
	}
	contract, err := gameplay.DecodeRuntime(*runtime)
	if err != nil {
		return nil, err
	}
	successful, err := repo.SuccessfulRollKeys(ctx, conv.ID)
	if err != nil {
		return nil, err
	}
	rolls := make(map[string]bool, len(successful))
	for _, key := range successful {
		rolls[key] = true
	}
	evidence := gameplay.Evidence{
		State: in.TrustedState, SuccessfulRollKeys: rolls, TurnCount: in.TurnCount,
		Facts: map[string]any{"engine_key": contract.Engine.Key},
	}
	if err := s.registry.Verify(outcome.VerifierKey, evidence, outcome.VerifierConfig); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSettlementDenied, err)
	}

	rules, err := repo.RewardRulesForOutcome(ctx, outcome.ID)
	if err != nil {
		return nil, err
	}
	grants := make([]storage.ChallengeRewardGrant, 0, len(rules))
	for _, rule := range rules {
		amount, err := s.registry.Calculate(rule.CalculatorKey, evidence, rule.CalculatorConfig)
		if err != nil {
			return nil, fmt.Errorf("%w: 奖励计算失败: %v", ErrSettlementDenied, err)
		}
		// Zero is a durable skipped award; a negative calculator result is invalid.
		if amount < 0 {
			return nil, fmt.Errorf("%w: 奖励计算数量为负数", ErrSettlementDenied)
		}
		definition, err := repo.RewardDefinition(ctx, rule.RewardDefinitionID)
		if err != nil {
			return nil, err
		}
		grants = append(grants, storage.ChallengeRewardGrant{
			Definition: *definition, Quantity: amount, Snapshot: rule.PublicSnapshot,
		})
	}
	return &settlementEvaluation{
		outcome: *outcome, grants: grants,
		detail: storage.JSONMap{
			"engine_key": contract.Engine.Key, "outcome_key": outcome.OutcomeKey,
			"turn_count": in.TurnCount, "successful_roll_keys": successful,
		},
	}, nil
}

func (s *Service) ValidateDocument(ctx context.Context, doc *VersionDocument) error {
	if err := ValidateDocument(doc, s.registry); err != nil {
		return err
	}
	contract, err := gameplay.DecodeRuntime(doc.Runtime)
	if err != nil {
		return fmt.Errorf("%w: runtime 无效: %v", ErrInvalidChallenge, err)
	}
	manifest, err := decodeManifest(doc.Version.Manifest)
	if err != nil {
		return err
	}
	if len(manifest.EntryRequirement) > 0 {
		rule, err := requirement.Decode(manifest.EntryRequirement)
		if err != nil {
			return fmt.Errorf("%w: entry_requirement 无效: %v", ErrInvalidChallenge, err)
		}
		for _, rewardKey := range requirement.RewardKeys(rule) {
			if _, err := s.repo.Rewards().DefinitionByKey(ctx, rewardKey); err != nil {
				return fmt.Errorf("%w: 前置条件奖励不存在 %s", ErrInvalidChallenge, rewardKey)
			}
		}
	}
	for capabilityKey, capability := range contract.Actions.Capabilities {
		if capability.CostQuantity <= 0 {
			continue
		}
		definition, err := s.repo.Rewards().DefinitionByKey(ctx, capability.CostRewardKey)
		if err != nil {
			return fmt.Errorf("%w: 能力 %s 的消耗奖励不存在 %s", ErrInvalidChallenge, capabilityKey, capability.CostRewardKey)
		}
		if !definition.Consumable {
			return fmt.Errorf("%w: 能力 %s 引用的奖励不可消费 %s", ErrInvalidChallenge, capabilityKey, capability.CostRewardKey)
		}
	}
	for _, rule := range doc.RewardRules {
		definition, err := s.repo.RewardDefinition(ctx, rule.RewardDefinitionID)
		if err != nil {
			return fmt.Errorf("%w: 奖励定义不存在", ErrInvalidChallenge)
		}
		expected := rewardSnapshot(*definition)
		if definition.Disabled {
			return fmt.Errorf("%w: 奖励已停用", ErrInvalidChallenge)
		}
		for _, key := range []string{"key", "kind", "name", "description", "unit", "icon", "stackable", "consumable"} {
			if fmt.Sprint(rule.PublicSnapshot[key]) != fmt.Sprint(expected[key]) {
				return fmt.Errorf("%w: 奖励 %s 的 public_snapshot 已过期", ErrInvalidChallenge, definition.Key)
			}
		}
	}
	return nil
}

func ValidateDocument(doc *VersionDocument, registries ...*gameplay.Registry) error {
	registry := gameplay.NewCoreRegistry()
	if len(registries) > 0 && registries[0] != nil {
		registry = registries[0]
	}
	manifest, err := decodeManifest(doc.Version.Manifest)
	if err != nil {
		return err
	}
	if manifest.ID == "" || manifest.Name == "" || manifest.Version == "" {
		return fmt.Errorf("%w: manifest 缺少 id、name 或 version", ErrInvalidChallenge)
	}
	if len(manifest.EntryRequirement) > 0 {
		if _, err := requirement.Decode(manifest.EntryRequirement); err != nil {
			return fmt.Errorf("%w: entry_requirement 无效: %v", ErrInvalidChallenge, err)
		}
	}
	if manifest.DefaultConfig.RecommendedCharacter == "" ||
		manifest.DefaultConfig.RecommendedPersona == "" ||
		manifest.DefaultConfig.RecommendedScenario == "" ||
		manifest.DefaultConfig.RecommendedPrompt == "" {
		return fmt.Errorf("%w: manifest 缺少默认 Character、Persona、Scenario 或 Prompt", ErrInvalidChallenge)
	}
	if manifest.Version != doc.Version.Version {
		return fmt.Errorf("%w: manifest.version 必须与版本记录一致", ErrInvalidChallenge)
	}
	contract, err := gameplay.DecodeRuntime(doc.Runtime)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidChallenge, err)
	}
	if err := registry.ValidateRuntime(contract); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidChallenge, err)
	}
	// Platform-owned capabilities must never be smuggled into a challenge
	// package. Keeping this rejection at the document boundary prevents future
	// content authors from accidentally reintroducing product authorization in
	// a Runtime JSON file.
	for key := range contract.Actions.Capabilities {
		if platformcapability.IsReservedKey(key) {
			return fmt.Errorf("%w: runtime 不得声明平台能力 %s", ErrInvalidChallenge, key)
		}
	}
	for _, key := range append(append([]string{}, contract.Actions.Player...), contract.Actions.Model...) {
		if platformcapability.IsReservedKey(key) {
			return fmt.Errorf("%w: runtime 动作列表不得声明平台能力 %s", ErrInvalidChallenge, platformcapability.MessageRewriteKey)
		}
	}
	if err := validateAssetDocuments(doc.Assets); err != nil {
		return err
	}
	if err := validateDefinitionDocuments(doc.Definitions); err != nil {
		return err
	}
	if gameplay.NormalizeRuntimeMode(contract.Mode) == gameplay.RuntimeModeCampaign {
		if err := validateCampaignDefinitionReferences(contract, doc.Definitions, registry.CampaignRequiresRealm(contract)); err != nil {
			return err
		}
	}
	assets := make(map[string]storage.ChallengeAsset, len(doc.Assets))
	for _, asset := range doc.Assets {
		assets[asset.Kind+":"+asset.Slug] = asset
	}
	required := map[string][]string{
		storage.ChallengeAssetCharacter: {manifest.DefaultConfig.RecommendedCharacter},
		storage.ChallengeAssetPersona:   {manifest.DefaultConfig.RecommendedPersona},
		storage.ChallengeAssetScenario:  {manifest.DefaultConfig.RecommendedScenario},
		storage.ChallengeAssetPrompt:    {manifest.DefaultConfig.RecommendedPrompt},
		storage.ChallengeAssetWorldBook: manifest.DefaultConfig.RequiredWorldBooks,
	}
	for kind, slugs := range required {
		for _, slug := range slugs {
			if slug != "" {
				if _, ok := assets[kind+":"+slug]; !ok {
					return fmt.Errorf("%w: manifest 引用了不存在的 %s/%s", ErrInvalidChallenge, kind, slug)
				}
			}
		}
	}
	for _, slug := range manifest.DefaultConfig.AllowedPersonas {
		if _, ok := assets[storage.ChallengeAssetPersona+":"+slug]; !ok {
			return fmt.Errorf("%w: allowed_personas 引用了不存在的 %s", ErrInvalidChallenge, slug)
		}
	}
	if allowed := stringSet(manifest.DefaultConfig.AllowedPersonas); len(allowed) > 0 &&
		!allowed[manifest.DefaultConfig.RecommendedPersona] {
		return fmt.Errorf("%w: recommended_persona 必须包含在 allowed_personas 中", ErrInvalidChallenge)
	}
	promptAsset := assets[storage.ChallengeAssetPrompt+":"+manifest.DefaultConfig.RecommendedPrompt]
	var prompt storage.PromptPreset
	if err := json.Unmarshal(promptAsset.Content, &prompt); err != nil {
		return fmt.Errorf("%w: Prompt 解析失败", ErrInvalidChallenge)
	}
	core := map[string]bool{"system_master": false, "agent_tool_loop": false}
	if gameplay.NormalizeRuntimeMode(contract.Mode) == gameplay.RuntimeModeChallenge {
		core["settlement_security"] = false
	}
	for _, segment := range prompt.Segments {
		if _, needed := core[segment.Key]; needed && segment.Enabled && segment.Required &&
			segment.Role == storage.RoleSystem && strings.TrimSpace(segment.Content) != "" {
			core[segment.Key] = true
		}
	}
	for key, found := range core {
		if !found {
			return fmt.Errorf("%w: Prompt 缺少必选核心分段 %s", ErrInvalidChallenge, key)
		}
	}
	for _, slug := range manifest.DefaultConfig.RequiredWorldBooks {
		asset := assets[storage.ChallengeAssetWorldBook+":"+slug]
		var book storage.WorldBook
		if err := json.Unmarshal(asset.Content, &book); err != nil {
			return fmt.Errorf("%w: 世界书 %s 解析失败", ErrInvalidChallenge, slug)
		}
		seenEntries := map[string]bool{}
		for _, entry := range book.Entries {
			if entry.ID == "" || seenEntries[entry.ID] || strings.TrimSpace(entry.Content) == "" ||
				entry.Priority < 0 || (entry.TokenCount != nil && *entry.TokenCount < 0) ||
				(!entry.Constant && len(entry.Keywords) == 0) {
				return fmt.Errorf("%w: 世界书 %s 包含无效或重复条目", ErrInvalidChallenge, slug)
			}
			seenEntries[entry.ID] = true
		}
	}
	if gameplay.NormalizeRuntimeMode(contract.Mode) == gameplay.RuntimeModeChallenge && len(doc.Outcomes) == 0 {
		return fmt.Errorf("%w: 至少需要一个结局", ErrInvalidChallenge)
	}
	outcomes := map[uuid.UUID]storage.ChallengeOutcomeRule{}
	outcomeKeys := map[string]bool{}
	enabledOutcomeKeys := map[string]bool{}
	for _, outcome := range doc.Outcomes {
		if outcome.ID == uuid.Nil || strings.TrimSpace(outcome.OutcomeKey) == "" ||
			outcomeKeys[outcome.OutcomeKey] || strings.TrimSpace(outcome.Title) == "" {
			return fmt.Errorf("%w: outcome_key、ID 或标题无效", ErrInvalidChallenge)
		}
		if outcome.Category != "success" && outcome.Category != "failure" && outcome.Category != "neutral" {
			return fmt.Errorf("%w: 结局 %s 的 category 无效", ErrInvalidChallenge, outcome.OutcomeKey)
		}
		if outcome.Tone != "success" && outcome.Tone != "error" && outcome.Tone != "warning" && outcome.Tone != "info" {
			return fmt.Errorf("%w: 结局 %s 的 tone 无效", ErrInvalidChallenge, outcome.OutcomeKey)
		}
		if err := registry.ValidateVerifier(contract, outcome.VerifierKey, outcome.VerifierConfig); err != nil {
			return fmt.Errorf("%w: 结局 %s: %v", ErrInvalidChallenge, outcome.OutcomeKey, err)
		}
		if !strings.Contains(string(promptAsset.Content), outcome.OutcomeKey) {
			return fmt.Errorf("%w: Prompt 未引用结局 %s", ErrInvalidChallenge, outcome.OutcomeKey)
		}
		outcomeKeys[outcome.OutcomeKey] = true
		enabledOutcomeKeys[outcome.OutcomeKey] = outcome.Enabled
		outcomes[outcome.ID] = outcome
	}
	seenRewardBindings := map[string]bool{}
	seenCampaignExtractionSources := map[string]bool{}
	for _, rule := range doc.RewardRules {
		outcome, ok := outcomes[rule.OutcomeRuleID]
		if !ok || rule.RewardDefinitionID == uuid.Nil {
			return fmt.Errorf("%w: 奖励规则引用了无效结局或奖励定义", ErrInvalidChallenge)
		}
		binding := rule.OutcomeRuleID.String() + ":" + rule.RewardDefinitionID.String()
		if seenRewardBindings[binding] {
			return fmt.Errorf("%w: 结局 %s 存在重复奖励", ErrInvalidChallenge, outcome.OutcomeKey)
		}
		if err := registry.ValidateCalculator(contract, rule.CalculatorKey, rule.CalculatorConfig); err != nil {
			return fmt.Errorf("%w: 结局 %s 的奖励计算无效: %v", ErrInvalidChallenge, outcome.OutcomeKey, err)
		}
		if rule.CalculatorKey == campaignreward.ItemStackCalculatorKey {
			if gameplay.NormalizeRuntimeMode(contract.Mode) != gameplay.RuntimeModeCampaign {
				return fmt.Errorf("%w: 局内资产提取计算器只能用于 Campaign", ErrInvalidChallenge)
			}
			extraction, _ := campaignreward.ParseItemStackRule(map[string]any(rule.CalculatorConfig))
			var source *storage.GameplayDefinition
			for index := range doc.Definitions {
				definition := &doc.Definitions[index]
				if definition.Kind == storage.DefinitionItem && definition.Key == extraction.ItemKey {
					source = definition
					break
				}
			}
			stackable, _ := sourceContentValue(source, "stackable").(bool)
			if source == nil || !stackable {
				return fmt.Errorf("%w: Campaign 提取奖励必须引用可堆叠 item %s", ErrInvalidChallenge, extraction.ItemKey)
			}
			rewardStackable, _ := rule.PublicSnapshot["stackable"].(bool)
			if !rewardStackable {
				return fmt.Errorf("%w: Campaign 局内资产只能提取为可叠加的账号奖励", ErrInvalidChallenge)
			}
			sourceBinding := outcome.OutcomeKey + ":" + extraction.ItemKey + ":" + extraction.QualityKey
			if rule.Enabled && seenCampaignExtractionSources[sourceBinding] {
				return fmt.Errorf("%w: Campaign 结局 %s 不能重复提取同一局内来源 %s/%s",
					ErrInvalidChallenge, outcome.OutcomeKey, extraction.ItemKey, extraction.QualityKey)
			}
			if rule.Enabled {
				seenCampaignExtractionSources[sourceBinding] = true
			}
		}
		if stringFromSnapshot(rule.PublicSnapshot, "key") == "" ||
			stringFromSnapshot(rule.PublicSnapshot, "name") == "" ||
			stringFromSnapshot(rule.PublicSnapshot, "kind") == "" {
			return fmt.Errorf("%w: 结局 %s 的奖励快照不完整", ErrInvalidChallenge, outcome.OutcomeKey)
		}
		seenRewardBindings[binding] = true
	}
	if gameplay.NormalizeRuntimeMode(contract.Mode) == gameplay.RuntimeModeCampaign {
		campaignOutcomes := map[string]bool{}
		for _, definition := range doc.Definitions {
			if definition.Kind != storage.DefinitionQuest {
				continue
			}
			quest, _ := worldrules.ParseQuest(map[string]any(definition.Content))
			if quest.CampaignOutcomeKey == "" {
				continue
			}
			if !enabledOutcomeKeys[quest.CampaignOutcomeKey] {
				return fmt.Errorf("%w: quest:%s 引用了未知或未启用的 Campaign 结局 %s", ErrInvalidChallenge, definition.Key, quest.CampaignOutcomeKey)
			}
			campaignOutcomes[quest.CampaignOutcomeKey] = true
		}
		for _, rule := range doc.RewardRules {
			outcome := outcomes[rule.OutcomeRuleID]
			if !rule.Enabled {
				continue
			}
			if !campaignOutcomes[outcome.OutcomeKey] {
				return fmt.Errorf("%w: Campaign 奖励引用了没有终局任务的结局 %s", ErrInvalidChallenge, outcome.OutcomeKey)
			}
			if rule.CalculatorKey != "core.fixed" && rule.CalculatorKey != campaignreward.ItemStackCalculatorKey {
				return fmt.Errorf("%w: Campaign 结局 %s 使用了不受支持的账号奖励计算器", ErrInvalidChallenge, outcome.OutcomeKey)
			}
		}
	}
	return nil
}

func sourceContentValue(definition *storage.GameplayDefinition, key string) any {
	if definition == nil {
		return nil
	}
	return definition.Content[key]
}

func validateAssetDocuments(assets []storage.ChallengeAsset) error {
	validKinds := map[string]bool{
		storage.ChallengeAssetCharacter: true, storage.ChallengeAssetPersona: true,
		storage.ChallengeAssetScenario: true, storage.ChallengeAssetPrompt: true,
		storage.ChallengeAssetWorldBook: true,
	}
	seen := map[string]bool{}
	for _, asset := range assets {
		if !validKinds[asset.Kind] || asset.Slug == "" || asset.Name == "" || !json.Valid(asset.Content) {
			return fmt.Errorf("%w: 资源 kind、slug、name 或 content 无效", ErrInvalidChallenge)
		}
		key := asset.Kind + ":" + asset.Slug
		if seen[key] {
			return fmt.Errorf("%w: 资源重复 %s", ErrInvalidChallenge, key)
		}
		seen[key] = true
		var identity struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(asset.Content, &identity); err != nil || identity.ID != asset.Slug {
			return fmt.Errorf("%w: %s 的 content.id 必须等于 slug", ErrInvalidChallenge, key)
		}
	}
	return nil
}

func validateDefinitionDocuments(definitions []storage.GameplayDefinition) error {
	validKinds := map[string]bool{
		storage.DefinitionRealm: true, storage.DefinitionTechnique: true,
		storage.DefinitionItem: true, storage.DefinitionEquipment: true,
		storage.DefinitionRecipe: true, storage.DefinitionMarket: true,
		storage.DefinitionLocation: true, storage.DefinitionFaction: true,
		storage.DefinitionNPCArchetype: true, storage.DefinitionEncounter: true,
		storage.DefinitionWorldEvent: true, storage.DefinitionQuest: true,
	}
	seen := map[string]bool{}
	for index := range definitions {
		definition := &definitions[index]
		if definition.Tags == nil {
			definition.Tags = make([]string, 0)
		}
		identity := definition.Kind + ":" + definition.Key
		if !validKinds[definition.Kind] || !stableDefinitionKey(definition.Key) ||
			strings.TrimSpace(definition.Name) == "" || len([]rune(definition.Name)) > 128 ||
			definition.SortOrder < 0 || definition.Content == nil {
			return fmt.Errorf("%w: 玩法定义 %s 的 kind、key、name、content 或 sort_order 无效", ErrInvalidChallenge, identity)
		}
		if seen[identity] {
			return fmt.Errorf("%w: 玩法定义重复 %s", ErrInvalidChallenge, identity)
		}
		seen[identity] = true
		switch definition.Visibility {
		case storage.CampaignVisibilityPlayer, storage.CampaignVisibilityModel, storage.CampaignVisibilityInternal:
		default:
			return fmt.Errorf("%w: 玩法定义 %s 的 visibility 无效", ErrInvalidChallenge, identity)
		}
		tags := map[string]bool{}
		for _, tag := range definition.Tags {
			if strings.TrimSpace(tag) == "" || tag != strings.TrimSpace(tag) || tags[tag] {
				return fmt.Errorf("%w: 玩法定义 %s 包含空白或重复 tag", ErrInvalidChallenge, identity)
			}
			tags[tag] = true
		}
		if err := validateDefinitionShape(*definition); err != nil {
			return fmt.Errorf("%w: 玩法定义 %s 无效: %v", ErrInvalidChallenge, identity, err)
		}
	}
	return nil
}

func validateDefinitionShape(definition storage.GameplayDefinition) error {
	content := definition.Content
	switch definition.Kind {
	case storage.DefinitionRealm:
		if _, ok := positiveDefinitionInt(content["cultivation_required"]); !ok {
			return fmt.Errorf("cultivation_required 必须是正整数")
		}
		if rank, ok := nonNegativeDefinitionInt(content["rank"]); !ok || rank > 1000 {
			return fmt.Errorf("rank 必须是 0 到 1000 的整数")
		}
		if raw, exists := content["next_realm_key"]; exists && raw != nil {
			next, ok := raw.(string)
			if !ok || !stableDefinitionKey(next) || next == definition.Key {
				return fmt.Errorf("next_realm_key 必须是其他境界的稳定 key")
			}
		}
		if _, ok := positiveDefinitionInt(content["lifespan_days"]); !ok {
			return fmt.Errorf("lifespan_days 必须是正整数")
		}
		if stages, ok := stringList(content["stages"]); !ok || len(stages) == 0 {
			return fmt.Errorf("stages 必须是非空稳定 key 数组")
		}
		for _, field := range []string{"max_health", "max_mana", "max_spiritual_sense"} {
			if raw, exists := content[field]; exists {
				if value, ok := positiveDefinitionInt(raw); !ok || value > 1000000000 {
					return fmt.Errorf("%s 必须是 1 到 1000000000 的整数", field)
				}
			}
		}
	case storage.DefinitionTechnique:
		category, _ := content["category"].(string)
		if !map[string]bool{"cultivation": true, "body": true, "movement": true, "spell": true, "secret": true, "craft": true}[category] {
			return fmt.Errorf("category 无效")
		}
		layers, layersOK := positiveDefinitionInt(content["max_layer"])
		if !layersOK || layers > 100 {
			return fmt.Errorf("max_layer 必须是 1 到 100 的整数")
		}
		if _, ok := positiveDefinitionInt(content["practice_minutes"]); !ok {
			return fmt.Errorf("practice_minutes 必须是正整数")
		}
		slots, ok := stringList(content["allowed_slots"])
		if !ok || len(slots) == 0 {
			return fmt.Errorf("allowed_slots 必须是非空稳定 key 数组")
		}
		if raw, exists := content["mastery_per_layer"]; exists {
			if value, valid := positiveDefinitionInt(raw); !valid || value > 10000 {
				return fmt.Errorf("mastery_per_layer 必须是 1 到 10000 的整数")
			}
		}
		starting, _ := content["starting_known"].(bool)
		pool, hasPool := content["starting_pool"]
		poolKey, poolOK := pool.(string)
		if hasPool && (!poolOK || !stableDefinitionKey(poolKey)) {
			return fmt.Errorf("starting_pool 必须是稳定 key")
		}
		if starting && poolKey != "" {
			return fmt.Errorf("starting_known 与 starting_pool 不能同时声明")
		}
		if rawWeight, hasWeight := content["starting_weight"]; hasWeight {
			weight, valid := positiveDefinitionInt(rawWeight)
			if poolKey == "" || !valid || weight > 100000 {
				return fmt.Errorf("starting_weight 只允许用于 starting_pool 且必须是 1 到 100000 的整数")
			}
		}
		if slot, _ := content["starting_slot"].(string); slot != "" {
			if (!starting && poolKey == "") || !containsString(slots, slot) {
				return fmt.Errorf("starting_slot 只允许用于固定或候选池开局功法且必须属于 allowed_slots")
			}
		}
		if source, _ := content["source_item_key"].(string); source != "" && !stableDefinitionKey(source) {
			return fmt.Errorf("source_item_key 必须是稳定 key")
		}
		if raw, exists := content["layer_effects"]; exists {
			layerEffects, ok := raw.([]any)
			if !ok || len(layerEffects) > effectrules.MaxLayerEffects {
				return fmt.Errorf("layer_effects 必须是最多 %d 项的数组", effectrules.MaxLayerEffects)
			}
			seenLayers := map[int64]bool{}
			for _, rawLayer := range layerEffects {
				layer, ok := definitionObject(rawLayer)
				number, numberOK := positiveDefinitionInt(layer["layer"])
				_, effectsOK := effectrules.ParseEffects(layer["effects"])
				if !ok || len(layer) != 2 || !numberOK || number > layers || seenLayers[number] || !effectsOK {
					return fmt.Errorf("layer_effects 必须使用不重复且不超过 max_layer 的层数和受支持效果")
				}
				seenLayers[number] = true
			}
		}
	case storage.DefinitionItem:
		if _, ok := content["stackable"].(bool); !ok {
			return fmt.Errorf("stackable 必须是布尔值")
		}
		if raw, exists := content["model_grantable"]; exists {
			if _, ok := raw.(bool); !ok {
				return fmt.Errorf("model_grantable 必须是布尔值")
			}
		}
		if raw, exists := content["consumable"]; exists {
			if _, ok := raw.(bool); !ok {
				return fmt.Errorf("consumable 必须是布尔值")
			}
		}
		if raw, exists := content["effects"]; exists {
			effects, ok := definitionObject(raw)
			if !ok || len(effects) > 8 {
				return fmt.Errorf("effects 必须是最多 8 项的对象")
			}
			for key, rawAmount := range effects {
				amount, valid := signedDefinitionInt(rawAmount)
				if !map[string]bool{"health": true, "mana": true, "spiritual_sense": true, "cultivation_progress": true}[key] ||
					!valid || amount < -10000 || amount > 10000 {
					return fmt.Errorf("effects 包含不受支持或越界的效果")
				}
			}
		}
	case storage.DefinitionEquipment:
		if raw, exists := content["model_grantable"]; exists {
			if _, ok := raw.(bool); !ok {
				return fmt.Errorf("model_grantable 必须是布尔值")
			}
		}
		if slots, ok := stringList(content["allowed_slots"]); !ok || len(slots) == 0 {
			return fmt.Errorf("allowed_slots 必须是非空稳定 key 数组")
		}
		if _, ok := positiveDefinitionInt(content["durability_max"]); !ok {
			return fmt.Errorf("durability_max 必须是正整数")
		}
		if raw, exists := content["bonuses"]; exists {
			if _, ok := effectrules.ParseEffects(raw); !ok {
				return fmt.Errorf("bonuses 包含不受支持或越界的效果")
			}
		}
	case storage.DefinitionRecipe:
		if _, ok := craftingrules.Parse(map[string]any(content)); !ok {
			return fmt.Errorf("配方结构、数值边界或品质权重无效")
		}
	case storage.DefinitionMarket:
		market, ok := marketrules.Parse(map[string]any(content))
		if !ok {
			return fmt.Errorf("市场结构、库存、补货或价格边界无效")
		}
		// 最坏情况（声望 +10000、满库存买入、零库存卖出）下卖价必须不高于买价，
		// 否则市场可被零时间无限套利。见 marketrules.NoArbitrage。
		if !marketrules.NoArbitrage(market) {
			return fmt.Errorf("市场存在套利空间：最坏情况下卖价不低于买价")
		}
	case storage.DefinitionEncounter:
		if _, ok := encounterrules.Parse(map[string]any(content)); !ok {
			return fmt.Errorf("遭遇地点、对手数值、回合规则或奖励结构无效")
		}
	case storage.DefinitionLocation:
		nodeType, ok := content["node_type"].(string)
		if !map[string]bool{"world": true, "realm": true, "region": true, "location": true, "scene": true}[nodeType] {
			return fmt.Errorf("node_type 必须是 world、realm、region、location 或 scene")
		}
		if parentKey, _ := content["parent_key"].(string); parentKey != "" {
			if !stableDefinitionKey(parentKey) || parentKey == definition.Key {
				return fmt.Errorf("parent_key 必须是不同于自身的稳定 key")
			}
		}
		connections, ok := content["connections"].([]any)
		if !ok {
			return fmt.Errorf("connections 必须是数组")
		}
		seen := map[string]bool{}
		for _, raw := range connections {
			connection, ok := raw.(map[string]any)
			key, _ := connection["location_key"].(string)
			if !ok || !stableDefinitionKey(key) || seen[key] {
				return fmt.Errorf("connections 包含无效或重复 location_key")
			}
			if _, ok := positiveDefinitionInt(connection["travel_minutes"]); !ok {
				return fmt.Errorf("connection.travel_minutes 必须是正整数")
			}
			seen[key] = true
		}
		if (nodeType == "world" || nodeType == "realm" || nodeType == "region") && len(connections) > 0 {
			return fmt.Errorf("world、realm 与 region 容器节点不能声明旅行连接")
		}
		if raw, exists := content["entry_requirements"]; exists {
			if _, ok := factionrules.EntryRequirements(map[string]any{"entry_requirements": raw}); !ok {
				return fmt.Errorf("entry_requirements 必须由不重复的势力权限或 min_realm_rank 组成，且最多 %d 项", factionrules.MaxEntryRequirements)
			}
		}
		if raw, exists := content["initial_state"]; exists {
			if nodeType != "region" {
				return fmt.Errorf("initial_state 只允许用于 region 节点")
			}
			if _, ok := worldrules.ParseRegionInitialState(raw); !ok {
				return fmt.Errorf("initial_state 的区域数值或 faction_power 无效")
			}
		}
		if raw, exists := content["gathering"]; exists {
			if nodeType == "realm" || nodeType == "region" {
				return fmt.Errorf("gathering 只允许用于可进入的 location 或 scene")
			}
			if _, ok := gatheringrules.Parse(raw); !ok {
				return fmt.Errorf("gathering 的时间、资源条目、权重或数量无效")
			}
		}
	case storage.DefinitionFaction:
		if raw, exists := content["initial_reputation"]; exists {
			reputation, ok := signedDefinitionInt(raw)
			if !ok || reputation < factionrules.MinimumReputation || reputation > factionrules.MaximumReputation {
				return fmt.Errorf("initial_reputation 必须是 %d 到 %d 的整数", factionrules.MinimumReputation, factionrules.MaximumReputation)
			}
		}
		if raw, exists := content["permission_thresholds"]; exists {
			thresholds, ok := definitionObject(raw)
			if !ok || len(thresholds) > 32 {
				return fmt.Errorf("permission_thresholds 必须是最多 32 项的对象")
			}
			for permissionKey, rawThreshold := range thresholds {
				threshold, valid := signedDefinitionInt(rawThreshold)
				if !stableDefinitionKey(permissionKey) || !valid ||
					threshold < factionrules.MinimumReputation || threshold > factionrules.MaximumReputation {
					return fmt.Errorf("permission_thresholds 的 key 必须稳定，阈值必须在 %d 到 %d 之间", factionrules.MinimumReputation, factionrules.MaximumReputation)
				}
			}
		}
		if raw, exists := content["position_thresholds"]; exists {
			thresholds, ok := definitionObject(raw)
			if !ok || len(thresholds) > 32 {
				return fmt.Errorf("position_thresholds 必须是最多 32 项的对象")
			}
			for positionKey, rawThreshold := range thresholds {
				threshold, valid := nonNegativeDefinitionInt(rawThreshold)
				if !stableDefinitionKey(positionKey) || !valid || threshold > factionrules.MaximumContribution {
					return fmt.Errorf("position_thresholds 的 key 必须稳定，阈值必须在 0 到 %d 之间", factionrules.MaximumContribution)
				}
			}
		}
	case storage.DefinitionNPCArchetype:
		initial, _ := content["initial"].(bool)
		spawnable, _ := content["spawnable"].(bool)
		if raw, exists := content["initial"]; exists {
			if _, ok := raw.(bool); !ok {
				return fmt.Errorf("initial 必须是布尔值")
			}
		}
		if raw, exists := content["spawnable"]; exists {
			if _, ok := raw.(bool); !ok {
				return fmt.Errorf("spawnable 必须是布尔值")
			}
		}
		if initial && spawnable {
			return fmt.Errorf("initial 与 spawnable 不能同时为 true")
		}
		if initial {
			for _, key := range []string{"instance_key", "location_key"} {
				value, _ := content[key].(string)
				if !stableDefinitionKey(value) {
					return fmt.Errorf("初始 NPC 的 %s 必须是稳定 key", key)
				}
			}
		} else if value, _ := content["instance_key"].(string); value != "" && !stableDefinitionKey(value) {
			return fmt.Errorf("instance_key 必须是稳定 key")
		}
		if realmKey, _ := content["realm_key"].(string); realmKey != "" && !stableDefinitionKey(realmKey) {
			return fmt.Errorf("realm_key 必须为空或稳定 key")
		}
		for _, key := range []string{"stage_key", "faction_key"} {
			if value, _ := content[key].(string); value != "" && !stableDefinitionKey(value) {
				return fmt.Errorf("%s 必须是稳定 key", key)
			}
		}
		for _, key := range []string{"public_profile", "private_state", "goals"} {
			if raw, exists := content[key]; exists {
				if _, ok := raw.(map[string]any); !ok {
					return fmt.Errorf("%s 必须是对象", key)
				}
			}
		}
		if spawnable {
			locations, ok := stringList(content["allowed_locations"])
			if !ok || len(locations) == 0 {
				return fmt.Errorf("可实例化 NPC 的 allowed_locations 必须是非空稳定 key 数组")
			}
			if maximum, ok := positiveDefinitionInt(content["max_instances"]); !ok || maximum > 100 {
				return fmt.Errorf("可实例化 NPC 的 max_instances 必须是 1 到 100 的整数")
			}
		}
	case storage.DefinitionWorldEvent:
		if _, ok := worldrules.ParseWorldEvent(map[string]any(content)); !ok {
			return fmt.Errorf("世界事件结构、调度、变体权重或效果边界无效")
		}
	case storage.DefinitionQuest:
		if _, ok := worldrules.ParseQuest(map[string]any(content)); !ok {
			return fmt.Errorf("任务类型、状态、截止时间、目标或奖励结构无效")
		}
	}
	return nil
}

func validateCampaignDefinitionReferences(
	contract *gameplay.RuntimeContract,
	definitions []storage.GameplayDefinition,
	requireRealmOption ...bool,
) error {
	requireRealm := true
	if len(requireRealmOption) > 0 {
		requireRealm = requireRealmOption[0]
	}
	catalog := map[string]storage.GameplayDefinition{}
	for _, definition := range definitions {
		catalog[definition.Kind+":"+definition.Key] = definition
	}
	if _, ok := catalog[storage.DefinitionLocation+":"+contract.Campaign.InitialLocationKey]; !ok {
		return fmt.Errorf("%w: campaign.initial_location_key 未在 location 定义中声明", ErrInvalidChallenge)
	}
	if initial := catalog[storage.DefinitionLocation+":"+contract.Campaign.InitialLocationKey]; !travelableLocationType(definitionLocationNodeType(initial.Content)) {
		return fmt.Errorf("%w: campaign.initial_location_key 必须指向 location 或 scene", ErrInvalidChallenge)
	}
	if len(definitions) == 0 {
		return fmt.Errorf("%w: campaign 模式必须提供玩法定义", ErrInvalidChallenge)
	}
	realmCount := 0
	realmRanks := map[int64]string{}
	startingTechniqueSlots := map[string]string{}
	startingTechniquePools := map[string]int{}
	initialNPCKeys := map[string]string{}
	initialNPCDefinitions := map[string]storage.GameplayDefinition{}
	quests := map[string]worldrules.Quest{}
	questUnlockSources := map[string][]string{}
	for _, definition := range definitions {
		if definition.Kind == storage.DefinitionNPCArchetype {
			if initial, _ := definition.Content["initial"].(bool); initial {
				if key, _ := definition.Content["instance_key"].(string); key != "" {
					initialNPCDefinitions[key] = definition
				}
			}
		}
		if definition.Kind == storage.DefinitionQuest {
			if parsed, ok := worldrules.ParseQuest(map[string]any(definition.Content)); ok {
				quests[definition.Key] = parsed
			}
		}
	}
	for _, definition := range definitions {
		switch definition.Kind {
		case storage.DefinitionRealm:
			realmCount++
			rank, rankOK := nonNegativeDefinitionInt(definition.Content["rank"])
			if !rankOK {
				return fmt.Errorf("%w: realm:%s 的 rank 无效", ErrInvalidChallenge, definition.Key)
			}
			if previous, exists := realmRanks[rank]; exists {
				return fmt.Errorf("%w: realm:%s 与 realm:%s 使用了相同 rank %d", ErrInvalidChallenge, definition.Key, previous, rank)
			}
			realmRanks[rank] = definition.Key
			if next, _ := definition.Content["next_realm_key"].(string); next != "" {
				nextDefinition, ok := catalog[storage.DefinitionRealm+":"+next]
				if !ok {
					return fmt.Errorf("%w: realm:%s 引用了未知 next_realm_key %s", ErrInvalidChallenge, definition.Key, next)
				}
				nextRank, nextRankOK := nonNegativeDefinitionInt(nextDefinition.Content["rank"])
				if !nextRankOK || nextRank <= rank {
					return fmt.Errorf("%w: realm:%s 的 next_realm_key %s 必须指向更高 rank", ErrInvalidChallenge, definition.Key, next)
				}
			}
		case storage.DefinitionTechnique:
			if source, _ := definition.Content["source_item_key"].(string); source != "" {
				if _, ok := catalog[storage.DefinitionItem+":"+source]; !ok {
					return fmt.Errorf("%w: technique:%s 引用了未知 source_item_key %s", ErrInvalidChallenge, definition.Key, source)
				}
			}
			starting, _ := definition.Content["starting_known"].(bool)
			pool, _ := definition.Content["starting_pool"].(string)
			if pool != "" {
				startingTechniquePools[pool]++
			}
			if starting || pool != "" {
				if slot, _ := definition.Content["starting_slot"].(string); slot != "" {
					owner := "fixed:" + definition.Key
					if pool != "" {
						owner = "pool:" + pool
					}
					if previous, exists := startingTechniqueSlots[slot]; exists && previous != owner {
						return fmt.Errorf("%w: technique:%s 与 %s 可能使用相同初始槽位 %s", ErrInvalidChallenge, definition.Key, previous, slot)
					}
					startingTechniqueSlots[slot] = owner
				}
			}
		case storage.DefinitionRecipe:
			recipe, _ := craftingrules.Parse(map[string]any(definition.Content))
			if recipe.SourceItemKey != "" {
				if err := requireStackableDefinition(catalog, recipe.SourceItemKey, "recipe:"+definition.Key+" 的 source_item_key"); err != nil {
					return err
				}
			}
			for _, ingredient := range recipe.Inputs {
				for _, option := range ingredient.Options {
					if err := requireStackableDefinition(catalog, option.ItemKey, "recipe:"+definition.Key+" 的材料"); err != nil {
						return err
					}
				}
			}
			for _, equipmentKey := range recipe.RequiredEquipmentKeys {
				if _, ok := catalog[storage.DefinitionEquipment+":"+equipmentKey]; !ok {
					return fmt.Errorf("%w: recipe:%s 引用了未知生产器具 %s", ErrInvalidChallenge, definition.Key, equipmentKey)
				}
			}
			if recipe.TechniqueKey != "" {
				technique, ok := catalog[storage.DefinitionTechnique+":"+recipe.TechniqueKey]
				category, _ := technique.Content["category"].(string)
				if !ok || category != "craft" {
					return fmt.Errorf("%w: recipe:%s 的 technique_key 必须引用 craft 类功法", ErrInvalidChallenge, definition.Key)
				}
			}
			for _, locationKey := range recipe.AllowedLocationKeys {
				location, ok := catalog[storage.DefinitionLocation+":"+locationKey]
				if !ok || !travelableLocationType(definitionLocationNodeType(location.Content)) {
					return fmt.Errorf("%w: recipe:%s 引用了未知或不可使用的地点 %s", ErrInvalidChallenge, definition.Key, locationKey)
				}
			}
			for _, products := range [][]craftingrules.Product{recipe.Outputs, recipe.FailureOutputs} {
				for _, product := range products {
					if err := requireProductDefinition(catalog, product.Kind, product.Key, "recipe:"+definition.Key); err != nil {
						return err
					}
				}
			}
		case storage.DefinitionMarket:
			market, _ := marketrules.Parse(map[string]any(definition.Content))
			location, ok := catalog[storage.DefinitionLocation+":"+market.LocationKey]
			if !ok || !travelableLocationType(definitionLocationNodeType(location.Content)) {
				return fmt.Errorf("%w: market:%s 引用了未知或不可进入的 location_key", ErrInvalidChallenge, definition.Key)
			}
			if err := requireStackableDefinition(catalog, market.CurrencyItemKey, "market:"+definition.Key+" 的 currency_item_key"); err != nil {
				return err
			}
			if market.FactionKey != "" {
				if _, ok := catalog[storage.DefinitionFaction+":"+market.FactionKey]; !ok {
					return fmt.Errorf("%w: market:%s 引用了未知 faction_key", ErrInvalidChallenge, definition.Key)
				}
			}
			for _, entry := range market.Entries {
				if err := requireProductDefinition(catalog, entry.Kind, entry.Key, "market:"+definition.Key); err != nil {
					return err
				}
			}
		case storage.DefinitionLocation:
			if parentKey, _ := definition.Content["parent_key"].(string); parentKey != "" {
				if _, ok := catalog[storage.DefinitionLocation+":"+parentKey]; !ok {
					return fmt.Errorf("%w: location:%s 引用了未知 parent_key %s", ErrInvalidChallenge, definition.Key, parentKey)
				}
			}
			connections, _ := definition.Content["connections"].([]any)
			for _, raw := range connections {
				connection := raw.(map[string]any)
				key := connection["location_key"].(string)
				target, ok := catalog[storage.DefinitionLocation+":"+key]
				if !ok {
					return fmt.Errorf("%w: location:%s 引用了未知地点 %s", ErrInvalidChallenge, definition.Key, key)
				}
				if !travelableLocationType(definitionLocationNodeType(target.Content)) {
					return fmt.Errorf("%w: location:%s 的连接指向不可旅行的容器节点 %s", ErrInvalidChallenge, definition.Key, key)
				}
			}
			requirements, _ := definition.Content["entry_requirements"].([]any)
			for _, raw := range requirements {
				requirement, _ := definitionObject(raw)
				if _, realmRequirement := requirement["min_realm_rank"]; realmRequirement {
					if !requireRealm {
						return fmt.Errorf("%w: 通用 Campaign 的 location:%s 不能声明 min_realm_rank", ErrInvalidChallenge, definition.Key)
					}
					continue
				}
				factionKey, _ := requirement["faction_key"].(string)
				permissionKey, _ := requirement["permission_key"].(string)
				faction, ok := catalog[storage.DefinitionFaction+":"+factionKey]
				if !ok {
					return fmt.Errorf("%w: location:%s 引用了未知 faction_key %s", ErrInvalidChallenge, definition.Key, factionKey)
				}
				thresholds, _ := definitionObject(faction.Content["permission_thresholds"])
				if _, ok := thresholds[permissionKey]; !ok {
					return fmt.Errorf("%w: location:%s 引用了 faction:%s 未声明的 permission_key %s", ErrInvalidChallenge, definition.Key, factionKey, permissionKey)
				}
			}
			if nodeType := definitionLocationNodeType(definition.Content); nodeType == "region" {
				initial, _ := worldrules.ParseRegionInitialState(definition.Content["initial_state"])
				for factionKey := range initial.FactionPower {
					if _, ok := catalog[storage.DefinitionFaction+":"+factionKey]; !ok {
						return fmt.Errorf("%w: location:%s 的 initial_state 引用了未知 faction %s", ErrInvalidChallenge, definition.Key, factionKey)
					}
				}
			}
			if raw, exists := definition.Content["gathering"]; exists {
				table, _ := gatheringrules.Parse(raw)
				for _, entry := range table.Entries {
					if err := requireStackableDefinition(catalog, entry.ItemKey, "location:"+definition.Key+" 的 gathering 资源"); err != nil {
						return err
					}
				}
			}
		case storage.DefinitionNPCArchetype:
			realmKey, _ := definition.Content["realm_key"].(string)
			if realmKey != "" {
				if _, ok := catalog[storage.DefinitionRealm+":"+realmKey]; ok {
					// Valid optional progression reference.
				} else {
					return fmt.Errorf("%w: npc_archetype:%s 引用了未知 realm_key", ErrInvalidChallenge, definition.Key)
				}
			} else if requireRealm {
				return fmt.Errorf("%w: npc_archetype:%s 引用了未知 realm_key", ErrInvalidChallenge, definition.Key)
			}
			// stage_key 必须属于其 realm 声明的 stages：跨境界的 stage
			//（例如练气期 archetype 配筑基层的 early/middle/late）一律拒绝。
			if stageKey, _ := definition.Content["stage_key"].(string); stageKey != "" && realmKey != "" {
				realm := catalog[storage.DefinitionRealm+":"+realmKey]
				stages, ok := stringList(realm.Content["stages"])
				if !ok || !containsString(stages, stageKey) {
					return fmt.Errorf("%w: npc_archetype:%s 的 stage_key %s 不属于 realm:%s 的 stages", ErrInvalidChallenge, definition.Key, stageKey, realmKey)
				}
			}
			if factionKey, _ := definition.Content["faction_key"].(string); factionKey != "" {
				if _, ok := catalog[storage.DefinitionFaction+":"+factionKey]; !ok {
					return fmt.Errorf("%w: npc_archetype:%s 引用了未知 faction_key", ErrInvalidChallenge, definition.Key)
				}
			}
			if initial, _ := definition.Content["initial"].(bool); initial {
				locationKey, _ := definition.Content["location_key"].(string)
				if _, ok := catalog[storage.DefinitionLocation+":"+locationKey]; !ok {
					return fmt.Errorf("%w: npc_archetype:%s 引用了未知 location_key", ErrInvalidChallenge, definition.Key)
				}
				instanceKey, _ := definition.Content["instance_key"].(string)
				if previous, exists := initialNPCKeys[instanceKey]; exists {
					return fmt.Errorf("%w: npc_archetype:%s 与 npc_archetype:%s 使用了相同 instance_key %s", ErrInvalidChallenge, definition.Key, previous, instanceKey)
				}
				initialNPCKeys[instanceKey] = definition.Key
			}
			if spawnable, _ := definition.Content["spawnable"].(bool); spawnable {
				locations, _ := stringList(definition.Content["allowed_locations"])
				for _, locationKey := range locations {
					if _, ok := catalog[storage.DefinitionLocation+":"+locationKey]; !ok {
						return fmt.Errorf("%w: npc_archetype:%s 引用了未知 allowed_location %s", ErrInvalidChallenge, definition.Key, locationKey)
					}
				}
			}
		case storage.DefinitionEncounter:
			encounter, _ := encounterrules.Parse(map[string]any(definition.Content))
			location, ok := catalog[storage.DefinitionLocation+":"+encounter.LocationKey]
			if !ok || !travelableLocationType(definitionLocationNodeType(location.Content)) {
				return fmt.Errorf("%w: encounter:%s 引用了未知或不可进入的 location_key", ErrInvalidChallenge, definition.Key)
			}
			for _, reward := range encounter.Rewards {
				if err := requireStackableDefinition(catalog, reward.ItemKey, "encounter:"+definition.Key+" 的奖励"); err != nil {
					return err
				}
			}
		case storage.DefinitionWorldEvent:
			event, _ := worldrules.ParseWorldEvent(map[string]any(definition.Content))
			region, ok := catalog[storage.DefinitionLocation+":"+event.RegionKey]
			if !ok || definitionLocationNodeType(region.Content) != "region" {
				return fmt.Errorf("%w: world_event:%s 的 region_key 必须引用 region 节点", ErrInvalidChallenge, definition.Key)
			}
			for _, variant := range event.Variants {
				for factionKey := range variant.Effects.FactionPowerDeltas {
					if _, ok := catalog[storage.DefinitionFaction+":"+factionKey]; !ok {
						return fmt.Errorf("%w: world_event:%s 引用了未知 faction %s", ErrInvalidChallenge, definition.Key, factionKey)
					}
				}
				for marketKey := range variant.Effects.MarketPriceModifierDeltasBPS {
					if _, ok := catalog[storage.DefinitionMarket+":"+marketKey]; !ok {
						return fmt.Errorf("%w: world_event:%s 引用了未知 market %s", ErrInvalidChallenge, definition.Key, marketKey)
					}
				}
				for npcKey := range variant.Effects.NPCGoalDeltas {
					if _, ok := initialNPCDefinitions[npcKey]; !ok {
						return fmt.Errorf("%w: world_event:%s 引用了未知初始 NPC 实例 %s", ErrInvalidChallenge, definition.Key, npcKey)
					}
				}
			}
		case storage.DefinitionQuest:
			quest, _ := worldrules.ParseQuest(map[string]any(definition.Content))
			for _, key := range quest.UnlockQuestKeys {
				target, ok := quests[key]
				if !ok {
					return fmt.Errorf("%w: quest:%s 解锁了未知 quest %s", ErrInvalidChallenge, definition.Key, key)
				}
				if key == definition.Key || target.Initial {
					return fmt.Errorf("%w: quest:%s 只能解锁其他非初始任务，当前为 %s", ErrInvalidChallenge, definition.Key, key)
				}
				questUnlockSources[key] = append(questUnlockSources[key], definition.Key)
			}
			for _, locationKey := range quest.AcceptLocationKeys {
				location, ok := catalog[storage.DefinitionLocation+":"+locationKey]
				if !ok || !travelableLocationType(definitionLocationNodeType(location.Content)) {
					return fmt.Errorf("%w: quest:%s 引用了未知或不可进入的接受地点 %s", ErrInvalidChallenge, definition.Key, locationKey)
				}
			}
			for _, objective := range quest.Objectives {
				switch objective.Type {
				case "item":
					if err := requireStackableDefinition(catalog, objective.ItemKey, "quest:"+definition.Key+" 的目标"); err != nil {
						return err
					}
				case "encounter_win":
					if _, ok := catalog[storage.DefinitionEncounter+":"+objective.EncounterKey]; !ok {
						return fmt.Errorf("%w: quest:%s 引用了未知 encounter %s", ErrInvalidChallenge, definition.Key, objective.EncounterKey)
					}
				case "location":
					if _, ok := catalog[storage.DefinitionLocation+":"+objective.LocationKey]; !ok {
						return fmt.Errorf("%w: quest:%s 引用了未知 location %s", ErrInvalidChallenge, definition.Key, objective.LocationKey)
					}
				case "faction_reputation":
					if _, ok := catalog[storage.DefinitionFaction+":"+objective.FactionKey]; !ok {
						return fmt.Errorf("%w: quest:%s 引用了未知 faction %s", ErrInvalidChallenge, definition.Key, objective.FactionKey)
					}
				case "npc_relationship":
					if _, ok := initialNPCDefinitions[objective.NPCKey]; !ok {
						return fmt.Errorf("%w: quest:%s 引用了未知初始 NPC 实例 %s", ErrInvalidChallenge, definition.Key, objective.NPCKey)
					}
				case "region_state":
					region, ok := catalog[storage.DefinitionLocation+":"+objective.RegionKey]
					if !ok || definitionLocationNodeType(region.Content) != "region" {
						return fmt.Errorf("%w: quest:%s 引用了未知 region %s", ErrInvalidChallenge, definition.Key, objective.RegionKey)
					}
				}
			}
			for _, reward := range quest.Rewards {
				if reward.Type == "item" {
					if err := requireStackableDefinition(catalog, reward.ItemKey, "quest:"+definition.Key+" 的奖励"); err != nil {
						return err
					}
				} else if _, ok := catalog[storage.DefinitionFaction+":"+reward.FactionKey]; !ok {
					return fmt.Errorf("%w: quest:%s 的奖励引用了未知 faction %s", ErrInvalidChallenge, definition.Key, reward.FactionKey)
				}
			}
			for _, key := range quest.Associations.RegionKeys {
				region, ok := catalog[storage.DefinitionLocation+":"+key]
				if !ok || definitionLocationNodeType(region.Content) != "region" {
					return fmt.Errorf("%w: quest:%s 的 associations 引用了未知 region %s", ErrInvalidChallenge, definition.Key, key)
				}
			}
			for _, key := range quest.Associations.FactionKeys {
				if _, ok := catalog[storage.DefinitionFaction+":"+key]; !ok {
					return fmt.Errorf("%w: quest:%s 的 associations 引用了未知 faction %s", ErrInvalidChallenge, definition.Key, key)
				}
			}
			for _, key := range quest.Associations.NPCKeys {
				if _, ok := initialNPCDefinitions[key]; !ok {
					return fmt.Errorf("%w: quest:%s 的 associations 引用了未知初始 NPC %s", ErrInvalidChallenge, definition.Key, key)
				}
			}
			for _, key := range quest.Associations.WorldEventKeys {
				if _, ok := catalog[storage.DefinitionWorldEvent+":"+key]; !ok {
					return fmt.Errorf("%w: quest:%s 的 associations 引用了未知 world_event %s", ErrInvalidChallenge, definition.Key, key)
				}
			}
		}
	}
	for pool, count := range startingTechniquePools {
		if count < 2 {
			return fmt.Errorf("%w: starting_pool %s 至少需要两个候选功法", ErrInvalidChallenge, pool)
		}
	}
	if requireRealm && realmCount == 0 {
		return fmt.Errorf("%w: campaign 模式至少需要一个 realm 定义", ErrInvalidChallenge)
	}
	for key, quest := range quests {
		if !quest.Initial && len(questUnlockSources[key]) == 0 {
			return fmt.Errorf("%w: 非初始 quest:%s 没有任何前置任务可以解锁它", ErrInvalidChallenge, key)
		}
	}
	if err := validateQuestUnlockGraph(quests); err != nil {
		return err
	}
	if err := validateLocationHierarchy(catalog, definitions); err != nil {
		return err
	}
	return nil
}

func validateQuestUnlockGraph(quests map[string]worldrules.Quest) error {
	const (
		visiting = iota + 1
		visited
	)
	states := make(map[string]int, len(quests))
	var visit func(string) error
	visit = func(key string) error {
		switch states[key] {
		case visiting:
			return fmt.Errorf("%w: 任务解锁关系存在循环，涉及 quest:%s", ErrInvalidChallenge, key)
		case visited:
			return nil
		}
		states[key] = visiting
		for _, target := range quests[key].UnlockQuestKeys {
			if err := visit(target); err != nil {
				return err
			}
		}
		states[key] = visited
		return nil
	}
	for key := range quests {
		if err := visit(key); err != nil {
			return err
		}
	}
	return nil
}

func requireStackableDefinition(catalog map[string]storage.GameplayDefinition, itemKey, reference string) error {
	item, ok := catalog[storage.DefinitionItem+":"+itemKey]
	stackable, _ := item.Content["stackable"].(bool)
	if !ok || !stackable {
		return fmt.Errorf("%w: %s 必须引用可堆叠 item，当前为 %s", ErrInvalidChallenge, reference, itemKey)
	}
	return nil
}

func requireProductDefinition(catalog map[string]storage.GameplayDefinition, kind, key, reference string) error {
	switch kind {
	case storage.DefinitionItem:
		return requireStackableDefinition(catalog, key, reference+" 的产物")
	case storage.DefinitionEquipment:
		if _, ok := catalog[storage.DefinitionEquipment+":"+key]; !ok {
			return fmt.Errorf("%w: %s 引用了未知 equipment %s", ErrInvalidChallenge, reference, key)
		}
		return nil
	default:
		return fmt.Errorf("%w: %s 的产物 kind 无效", ErrInvalidChallenge, reference)
	}
}

func validateLocationHierarchy(catalog map[string]storage.GameplayDefinition, definitions []storage.GameplayDefinition) error {
	ranks := map[string]int{"world": 0, "realm": 0, "region": 1, "location": 2, "scene": 3}
	for _, definition := range definitions {
		if definition.Kind != storage.DefinitionLocation {
			continue
		}
		childType := definitionLocationNodeType(definition.Content)
		parentKey, _ := definition.Content["parent_key"].(string)
		if (childType == "world" || childType == "realm") && parentKey != "" {
			return fmt.Errorf("%w: location:%s 的根节点不能声明父节点", ErrInvalidChallenge, definition.Key)
		}
		if childType != "world" && childType != "realm" && parentKey == "" {
			return fmt.Errorf("%w: location:%s 的 %s 节点必须声明父节点", ErrInvalidChallenge, definition.Key, childType)
		}
		if parentKey != "" {
			parent := catalog[storage.DefinitionLocation+":"+parentKey]
			parentType := definitionLocationNodeType(parent.Content)
			if ranks[parentType]+1 != ranks[childType] {
				return fmt.Errorf("%w: location:%s 的 %s 父节点不能是 %s", ErrInvalidChallenge, definition.Key, childType, parentType)
			}
		}
		seen := map[string]bool{}
		key := definition.Key
		for depth := 1; key != ""; depth++ {
			if seen[key] {
				return fmt.Errorf("%w: location:%s 的 parent_key 形成循环", ErrInvalidChallenge, definition.Key)
			}
			if depth > 4 {
				return fmt.Errorf("%w: location:%s 的层级深度超过 4", ErrInvalidChallenge, definition.Key)
			}
			seen[key] = true
			current, ok := catalog[storage.DefinitionLocation+":"+key]
			if !ok {
				break
			}
			key, _ = current.Content["parent_key"].(string)
		}
	}
	return nil
}

func definitionLocationNodeType(content storage.JSONMap) string {
	if value, ok := content["node_type"].(string); ok && value != "" {
		return value
	}
	return "location"
}

func travelableLocationType(nodeType string) bool {
	return nodeType == "location" || nodeType == "scene"
}

func stableDefinitionKey(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || len(value) > 128 {
		return false
	}
	for index, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') ||
			index > 0 && (char == '.' || char == '_' || char == '-') {
			continue
		}
		return false
	}
	return true
}

func stringList(value any) ([]string, bool) {
	raw, ok := value.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(raw))
	seen := map[string]bool{}
	for _, item := range raw {
		value, ok := item.(string)
		if !ok || !stableDefinitionKey(value) || seen[value] {
			return nil, false
		}
		seen[value] = true
		out = append(out, value)
	}
	return out, true
}

func definitionObject(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	case storage.JSONMap:
		return map[string]any(typed), true
	default:
		return nil, false
	}
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func positiveDefinitionInt(value any) (int64, bool) {
	number, ok := nonNegativeDefinitionInt(value)
	return number, ok && number > 0
}

func nonNegativeDefinitionInt(value any) (int64, bool) {
	number, ok := signedDefinitionInt(value)
	return number, ok && number >= 0
}

func signedDefinitionInt(value any) (int64, bool) {
	switch number := value.(type) {
	case int:
		return int64(number), true
	case int64:
		return number, true
	case float64:
		return int64(number), number == float64(int64(number))
	default:
		return 0, false
	}
}

func ContentHashDocument(doc *VersionDocument) (string, error) {
	type hashAsset struct {
		Kind      string          `json:"kind"`
		Slug      string          `json:"slug"`
		Name      string          `json:"name"`
		Content   json.RawMessage `json:"content"`
		SortOrder int             `json:"sort_order"`
	}
	type hashOutcome struct {
		Key            string         `json:"key"`
		Category       string         `json:"category"`
		Title          string         `json:"title"`
		Tone           string         `json:"tone"`
		VerifierKey    string         `json:"verifier_key"`
		VerifierConfig map[string]any `json:"verifier_config"`
		Enabled        bool           `json:"enabled"`
	}
	type hashDefinition struct {
		Kind       string         `json:"kind"`
		Key        string         `json:"key"`
		Name       string         `json:"name"`
		Content    map[string]any `json:"content"`
		Tags       []string       `json:"tags"`
		SearchText string         `json:"search_text"`
		Visibility string         `json:"visibility"`
		SortOrder  int            `json:"sort_order"`
	}
	type hashReward struct {
		OutcomeKey       string         `json:"outcome_key"`
		RewardKey        string         `json:"reward_key"`
		CalculatorKey    string         `json:"calculator_key"`
		CalculatorConfig map[string]any `json:"calculator_config"`
		PublicSnapshot   map[string]any `json:"public_snapshot"`
		Enabled          bool           `json:"enabled"`
	}
	canonicalManifest, err := canonicalJSON(doc.Version.Manifest)
	if err != nil {
		return "", err
	}
	runtimePayload := map[string]any{
		"engine_key": doc.Runtime.EngineKey, "engine_version": doc.Runtime.EngineVersion,
		"mode": gameplay.NormalizeRuntimeMode(doc.Runtime.Mode),
	}
	for key, raw := range map[string]storage.JSONDoc{
		"state_schema": doc.Runtime.StateSchema, "action_config": doc.Runtime.ActionConfig,
		"settlement_config": doc.Runtime.SettlementConfig, "presentation": doc.Runtime.Presentation,
	} {
		var value any
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			return "", err
		}
		runtimePayload[key] = value
	}
	if doc.Runtime.CampaignConfig != nil && len(*doc.Runtime.CampaignConfig) > 0 && string(*doc.Runtime.CampaignConfig) != "null" {
		var value any
		decoder := json.NewDecoder(strings.NewReader(string(*doc.Runtime.CampaignConfig)))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			return "", err
		}
		runtimePayload["campaign_config"] = value
	}
	payload := struct {
		Manifest    json.RawMessage  `json:"manifest"`
		Runtime     map[string]any   `json:"runtime"`
		Assets      []hashAsset      `json:"assets"`
		Definitions []hashDefinition `json:"definitions"`
		Outcomes    []hashOutcome    `json:"outcomes"`
		Rewards     []hashReward     `json:"rewards"`
	}{Manifest: canonicalManifest, Runtime: runtimePayload}

	assets := append([]storage.ChallengeAsset(nil), doc.Assets...)
	sort.Slice(assets, func(i, j int) bool {
		return assets[i].Kind+":"+assets[i].Slug < assets[j].Kind+":"+assets[j].Slug
	})
	for _, asset := range assets {
		content, err := canonicalJSON(asset.Content)
		if err != nil {
			return "", err
		}
		payload.Assets = append(payload.Assets, hashAsset{
			Kind: asset.Kind, Slug: asset.Slug, Name: asset.Name,
			Content: content, SortOrder: asset.SortOrder,
		})
	}
	definitions := append([]storage.GameplayDefinition(nil), doc.Definitions...)
	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].Kind+":"+definitions[i].Key < definitions[j].Kind+":"+definitions[j].Key
	})
	for _, definition := range definitions {
		payload.Definitions = append(payload.Definitions, hashDefinition{
			Kind: definition.Kind, Key: definition.Key, Name: definition.Name,
			Content: definition.Content, Tags: []string(definition.Tags), SearchText: definition.SearchText,
			Visibility: definition.Visibility, SortOrder: definition.SortOrder,
		})
	}
	outcomeByID := map[uuid.UUID]string{}
	outcomes := append([]storage.ChallengeOutcomeRule(nil), doc.Outcomes...)
	sort.Slice(outcomes, func(i, j int) bool { return outcomes[i].OutcomeKey < outcomes[j].OutcomeKey })
	for _, outcome := range outcomes {
		outcomeByID[outcome.ID] = outcome.OutcomeKey
		payload.Outcomes = append(payload.Outcomes, hashOutcome{
			Key: outcome.OutcomeKey, Category: outcome.Category, Title: outcome.Title,
			Tone: outcome.Tone, VerifierKey: outcome.VerifierKey,
			VerifierConfig: outcome.VerifierConfig, Enabled: outcome.Enabled,
		})
	}
	rules := append([]storage.ChallengeRewardRule(nil), doc.RewardRules...)
	sort.Slice(rules, func(i, j int) bool {
		left := outcomeByID[rules[i].OutcomeRuleID] + ":" + stringFromSnapshot(rules[i].PublicSnapshot, "key")
		right := outcomeByID[rules[j].OutcomeRuleID] + ":" + stringFromSnapshot(rules[j].PublicSnapshot, "key")
		return left < right
	})
	for _, rule := range rules {
		payload.Rewards = append(payload.Rewards, hashReward{
			OutcomeKey:    outcomeByID[rule.OutcomeRuleID],
			RewardKey:     stringFromSnapshot(rule.PublicSnapshot, "key"),
			CalculatorKey: rule.CalculatorKey, CalculatorConfig: rule.CalculatorConfig,
			PublicSnapshot: rule.PublicSnapshot, Enabled: rule.Enabled,
		})
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func canonicalJSON(raw []byte) (json.RawMessage, error) {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(canonical), nil
}

func defaultDraftRuntime(versionID uuid.UUID) (storage.ChallengeRuntimeConfig, error) {
	contract := gameplay.RuntimeContract{
		Engine: gameplay.EngineRef{Key: "core.generic", Version: 1},
		Mode:   gameplay.RuntimeModeChallenge,
		StateSchema: gameplay.StateSchema{
			SchemaVersion: 1,
			Fields: []gameplay.StateField{{
				Path: "profile", Type: "object", Default: map[string]any{},
				Visibility: gameplay.VisibilityPlayer, UpdatePolicy: gameplay.UpdateModel,
				AllowChildren: true,
			}},
		},
		Actions: gameplay.ActionConfig{
			Model:        []string{"update_state", "roll", "complete_challenge"},
			Player:       []string{"send_message"},
			Capabilities: map[string]gameplay.Capability{},
		},
		Settlement: gameplay.SettlementConfig{Handler: "core.challenge_completion"},
		Presentation: gameplay.Presentation{
			StatePanelTitle: "状态", StateSections: []gameplay.StateSection{},
			Completion: gameplay.CompletionPresentation{
				ReplayLabel: "再来一次", BackLabel: "返回挑战",
			},
		},
	}
	runtime, err := gameplay.EncodeRuntime(contract)
	if err != nil {
		return storage.ChallengeRuntimeConfig{}, err
	}
	now := time.Now()
	runtime.ChallengeVersionID = versionID
	runtime.CreatedAt = now
	runtime.UpdatedAt = now
	return runtime, nil
}

func rewardSnapshot(definition storage.RewardDefinition) storage.JSONMap {
	return storage.JSONMap{
		"key": definition.Key, "kind": definition.Kind, "name": definition.Name,
		"description": definition.Description, "unit": definition.Unit, "icon": definition.Icon,
		"stackable": definition.Stackable, "consumable": definition.Consumable,
	}
}

func stringFromSnapshot(snapshot storage.JSONMap, key string) string {
	value, _ := snapshot[key].(string)
	return value
}

func decodeManifest(raw storage.JSONDoc) (Manifest, error) {
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return manifest, fmt.Errorf("%w: manifest 解析失败: %v", ErrInvalidChallenge, err)
	}
	return manifest, nil
}

func stringSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}

func validSlug(value string) bool {
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return value != ""
}
