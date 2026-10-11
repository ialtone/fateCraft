package challenge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/kernel"
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
	campaignModules CampaignModuleExecutor
	repo            *storage.ChallengeRepository
	registry        *gameplay.Registry
	versionCache    *versionContentCache
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
		(pack.Status == storage.ChallengePackActive || pack.Status == storage.ChallengePackHidden) && pack.CurrentVersionID != nil &&
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

func (s *Service) SetEnabled(ctx context.Context, packID uuid.UUID, enabled bool) (*storage.ChallengePack, error) {
	pack, err := s.repo.SetPackEnabled(ctx, packID, enabled)
	if errors.Is(err, storage.ErrPublishedVersion) {
		return nil, fmt.Errorf("%w: 请先发布有效版本，再设置是否启用", ErrInvalidChallenge)
	}
	return pack, err
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
	doc := &VersionDocument{
		Version: *version, Runtime: *runtime, Assets: assets, Definitions: definitions,
		Outcomes: outcomes,
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
	var manifest Manifest
	if err := json.Unmarshal(in.Manifest, &manifest); err != nil {
		return nil, fmt.Errorf("%w: manifest 无效: %v", ErrInvalidChallenge, err)
	}
	for _, outcome := range in.Outcomes {
		if outcome.RewardPoints < 0 || outcome.RewardPoints > storage.MaxRewardQuantity {
			return nil, fmt.Errorf("%w: 通关积分无效", ErrInvalidChallenge)
		}
	}
	if err := validateAssetDocuments(in.Assets); err != nil {
		return nil, err
	}
	contract, err := storage.DecodeGameplayRuntime(in.Runtime)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidChallenge, err)
	}
	for i := range in.Definitions {
		if in.Definitions[i].Tags == nil {
			in.Definitions[i].Tags = []string{}
		}
	}
	if err := s.registry.ValidateDefinitions(moduleContent(contract, &VersionDocument{Definitions: in.Definitions})); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidChallenge, err)
	}
	in.Runtime.Mode = gameplay.NormalizeRuntimeMode(in.Runtime.Mode)
	if in.Runtime.Mode == gameplay.RuntimeModeChallenge {
		in.Runtime.CampaignConfig = nil
	}
	in.Runtime.ChallengeVersionID = versionID
	if err := s.repo.ReplaceDraft(
		ctx, versionID, storage.JSONDoc(in.Manifest), in.Runtime, in.Assets, in.Definitions, in.Outcomes,
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
	) (string, error) {
		doc := &VersionDocument{
			Version: version, Runtime: runtime, Assets: assets, Definitions: definitions,
			Outcomes: outcomes,
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
			outcomes[index].ID = uuid.New()
			outcomes[index].ChallengeVersionID = version.ID
			outcomes[index].CreatedAt, outcomes[index].UpdatedAt = now, now
		}
	}
	if err := s.repo.CreateDraftWithContent(ctx, version, runtime, assets, definitions, outcomes); err != nil {
		return nil, err
	}
	return s.VersionDocument(ctx, version.ID)
}

type settlementReader interface {
	GetRun(context.Context, uuid.UUID) (*storage.ChallengeRun, error)
	OutcomeRule(context.Context, uuid.UUID, string) (*storage.ChallengeOutcomeRule, error)
	SuccessfulRollKeys(context.Context, uuid.UUID) ([]string, error)
	GetRuntime(context.Context, uuid.UUID) (*storage.ChallengeRuntimeConfig, error)
}

type settlementEvaluation struct {
	outcome storage.ChallengeOutcomeRule
	points  int64
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
		evaluation.points = 0
		rewardsDisabledReason = storage.RewardsDisabledReasonClientRoute
		evaluation.detail[storage.RewardsDisabledReasonKey] = rewardsDisabledReason
		evaluation.detail["reward_points"] = int64(0)
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
		RewardPoints: evaluation.points,
		Key:          evaluation.outcome.OutcomeKey, Category: evaluation.outcome.Category,
		Title: evaluation.outcome.Title, Tone: evaluation.outcome.Tone,
	}
	rewards, err := tx.CompleteRun(ctx, conv, &settled, evaluation.points)
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
	contract, err := storage.DecodeGameplayRuntime(*runtime)
	if err != nil {
		return nil, err
	}
	module, err := s.registry.Module(contract.Engine)
	if err != nil {
		return nil, err
	}
	if module.Runtime != nil {
		envelope, err := kernel.ReadEnvelope(gameplay.Content{Runtime: contract}, in.TrustedState)
		if err != nil || envelope.State.Status == gameplay.RunActive || envelope.State.OutcomeKey != in.OutcomeKey || envelope.State.Revision < 1 {
			return nil, fmt.Errorf("%w: 尚未达成此模块结局", ErrSettlementDenied)
		}
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

	if outcome.RewardPoints < 0 || outcome.RewardPoints > storage.MaxRewardQuantity {
		return nil, fmt.Errorf("%w: 积分数量无效", ErrSettlementDenied)
	}
	return &settlementEvaluation{
		outcome: *outcome, points: outcome.RewardPoints,
		detail: storage.JSONMap{
			"engine_key": contract.Engine.Key, "outcome_key": outcome.OutcomeKey,
			"turn_count": in.TurnCount, "successful_roll_keys": successful, "reward_points": outcome.RewardPoints,
		},
	}, nil
}

func (s *Service) ValidateDocument(ctx context.Context, doc *VersionDocument) error {
	return ValidateDocument(doc, s.registry)
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
	contract, err := storage.DecodeGameplayRuntime(doc.Runtime)
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
	for i := range doc.Definitions {
		if doc.Definitions[i].Tags == nil {
			doc.Definitions[i].Tags = []string{}
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
	outcomeKeys := map[string]bool{}

	for _, outcome := range doc.Outcomes {
		if outcome.RewardPoints < 0 || outcome.RewardPoints > storage.MaxRewardQuantity {
			return fmt.Errorf("%w: 通关积分无效", ErrInvalidChallenge)
		}
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

	}
	if err := registry.ValidateContent(moduleContent(contract, doc)); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidChallenge, err)
	}
	personas := slices.Clone(manifest.DefaultConfig.AllowedPersonas)
	if len(personas) == 0 {
		for _, asset := range doc.Assets {
			if asset.Kind == storage.ChallengeAssetPersona {
				personas = append(personas, asset.Slug)
			}
		}
	}
	if err := validateInitialModule(registry, moduleContent(contract, doc), personas...); err != nil {
		return fmt.Errorf("%w: 模块开局验证: %v", ErrInvalidChallenge, err)
	}
	return nil
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

func ContentHashDocument(doc *VersionDocument) (string, error) {
	type hashAsset struct {
		Kind      string          `json:"kind"`
		Slug      string          `json:"slug"`
		Name      string          `json:"name"`
		Content   json.RawMessage `json:"content"`
		SortOrder int             `json:"sort_order"`
	}
	type hashOutcome struct {
		RewardPoints   int64          `json:"reward_points"`
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
	outcomes := append([]storage.ChallengeOutcomeRule(nil), doc.Outcomes...)
	sort.Slice(outcomes, func(i, j int) bool { return outcomes[i].OutcomeKey < outcomes[j].OutcomeKey })
	for _, outcome := range outcomes {
		payload.Outcomes = append(payload.Outcomes, hashOutcome{
			Key: outcome.OutcomeKey, Category: outcome.Category, Title: outcome.Title,
			Tone: outcome.Tone, VerifierKey: outcome.VerifierKey,
			VerifierConfig: outcome.VerifierConfig, Enabled: outcome.Enabled, RewardPoints: outcome.RewardPoints,
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
	runtime, err := storage.EncodeGameplayRuntime(contract)
	if err != nil {
		return storage.ChallengeRuntimeConfig{}, err
	}
	now := time.Now()
	runtime.ChallengeVersionID = versionID
	runtime.CreatedAt = now
	runtime.UpdatedAt = now
	return runtime, nil
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
