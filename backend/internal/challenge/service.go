package challenge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/storage"
)

var (
	ErrInvalidChallenge  = errors.New("挑战内容无效")
	ErrNotPublished      = errors.New("挑战尚未发布")
	ErrInvalidSelection  = errors.New("所选内容不属于该挑战")
	ErrSettlementDenied  = errors.New("挑战结算条件未满足")
	ErrConversationEnded = errors.New("挑战对话已结束")
)

type Service struct {
	repo *storage.ChallengeRepository
}

func NewService(repo *storage.ChallengeRepository) *Service {
	return &Service{repo: repo}
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

func (s *Service) ListPublished(ctx context.Context) ([]PublicChallenge, error) {
	packs, err := s.repo.ListPacks(ctx, true)
	if err != nil {
		return nil, err
	}
	out := make([]PublicChallenge, 0, len(packs))
	for _, pack := range packs {
		item, err := s.publicChallenge(ctx, &pack)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, nil
}

func (s *Service) GetPublished(ctx context.Context, slug string) (*PublicChallenge, error) {
	pack, err := s.repo.GetPackBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	if pack.Status != storage.ChallengePackActive || pack.CurrentVersionID == nil {
		return nil, ErrNotPublished
	}
	return s.publicChallenge(ctx, pack)
}

func (s *Service) publicChallenge(ctx context.Context, pack *storage.ChallengePack) (*PublicChallenge, error) {
	if pack.CurrentVersionID == nil {
		return nil, ErrNotPublished
	}
	doc, err := s.VersionDocument(ctx, *pack.CurrentVersionID)
	if err != nil {
		return nil, err
	}
	version := &doc.Version
	if version.Status != storage.ChallengeVersionPublished {
		return nil, ErrNotPublished
	}
	actualHash, err := ContentHash(version.Manifest, doc.Assets, doc.RewardRules)
	if err != nil || version.ContentHash == "" || actualHash != version.ContentHash {
		return nil, fmt.Errorf("%w: 已发布挑战内容校验失败", ErrInvalidChallenge)
	}
	manifest, err := decodeManifest(version.Manifest)
	if err != nil {
		return nil, err
	}
	assets := doc.Assets
	allowed := stringSet(manifest.DefaultConfig.AllowedPersonas)
	personas := make([]AssetOption, 0)
	for _, asset := range assets {
		if asset.Kind != storage.ChallengeAssetPersona {
			continue
		}
		if len(allowed) > 0 && !allowed[asset.Slug] {
			continue
		}
		var persona storage.Persona
		if err := json.Unmarshal(asset.Content, &persona); err != nil {
			return nil, fmt.Errorf("解析 Persona %s: %w", asset.Slug, err)
		}
		personas = append(personas, AssetOption{
			ID: asset.ID, Slug: asset.Slug, Name: asset.Name, Description: persona.Description,
		})
	}
	sort.Slice(personas, func(i, j int) bool { return personas[i].Name < personas[j].Name })
	rules := doc.RewardRules
	rewardViews := make([]RewardView, 0, len(rules))
	for _, rule := range rules {
		if !rule.Enabled || rule.EndingType != "good" {
			continue
		}
		rewardViews = append(rewardViews, RewardView{
			RewardKey: rule.RewardKey, EndingTitle: rule.EndingTitle,
			Multiplier: formatMultiplier(numberAsInt64(rule.Calculation["multiplier_bps"])),
		})
	}
	return &PublicChallenge{
		ID: pack.ID, Slug: pack.Slug, Name: pack.Name, Description: pack.Description,
		Version: version.Version, Icon: manifest.Icon, Tags: manifest.Tags,
		Features: manifest.Features, QuickStartGuide: manifest.QuickStartGuide,
		Personas: personas, RewardOverview: rewardViews,
	}, nil
}

func (s *Service) ResolveSelection(ctx context.Context, challengeSlug, personaSlug string) (*Selection, error) {
	pack, err := s.repo.GetPackBySlug(ctx, challengeSlug)
	if err != nil {
		return nil, err
	}
	if pack.Status != storage.ChallengePackActive || pack.CurrentVersionID == nil {
		return nil, ErrNotPublished
	}
	doc, err := s.VersionDocument(ctx, *pack.CurrentVersionID)
	if err != nil {
		return nil, err
	}
	version := &doc.Version
	if version.Status != storage.ChallengeVersionPublished || version.ContentHash == "" {
		return nil, ErrNotPublished
	}
	actualHash, err := ContentHash(version.Manifest, doc.Assets, doc.RewardRules)
	if err != nil || actualHash != version.ContentHash {
		return nil, fmt.Errorf("%w: 已发布挑战内容校验失败", ErrInvalidChallenge)
	}
	manifest, err := decodeManifest(version.Manifest)
	if err != nil {
		return nil, err
	}
	selectedPersona := personaSlug
	if selectedPersona == "" {
		selectedPersona = manifest.DefaultConfig.RecommendedPersona
	}
	if allowed := stringSet(manifest.DefaultConfig.AllowedPersonas); len(allowed) > 0 && !allowed[selectedPersona] {
		return nil, ErrInvalidSelection
	}
	selection := &Selection{Version: *version}
	assetByKey := make(map[string]*storage.ChallengeAsset, len(doc.Assets))
	for i := range doc.Assets {
		asset := &doc.Assets[i]
		assetByKey[asset.Kind+":"+asset.Slug] = asset
	}
	for kind, slug := range map[string]string{
		storage.ChallengeAssetCharacter: manifest.DefaultConfig.RecommendedCharacter,
		storage.ChallengeAssetPersona:   selectedPersona,
		storage.ChallengeAssetScenario:  manifest.DefaultConfig.RecommendedScenario,
	} {
		if slug == "" {
			continue
		}
		asset, ok := assetByKey[kind+":"+slug]
		if !ok {
			return nil, ErrInvalidSelection
		}
		switch kind {
		case storage.ChallengeAssetCharacter:
			selection.Character = asset
		case storage.ChallengeAssetPersona:
			selection.Persona = asset
		case storage.ChallengeAssetScenario:
			selection.Scenario = asset
		}
	}
	return selection, nil
}

func (s *Service) Bundle(ctx context.Context, conv *storage.Conversation) (*Bundle, error) {
	doc, err := s.VersionDocument(ctx, conv.ChallengeVersionID)
	if err != nil {
		return nil, err
	}
	version := &doc.Version
	actualHash, err := ContentHash(version.Manifest, doc.Assets, doc.RewardRules)
	if err != nil {
		return nil, err
	}
	versionUsable := version.Status == storage.ChallengeVersionPublished || version.Status == storage.ChallengeVersionRetired
	if !versionUsable || version.ContentHash != conv.ChallengeHash || actualHash != version.ContentHash {
		return nil, fmt.Errorf("%w: 对话绑定的挑战版本不可用或内容校验失败", ErrInvalidChallenge)
	}
	manifest, err := decodeManifest(version.Manifest)
	if err != nil {
		return nil, err
	}
	bundle := &Bundle{Version: *version, Manifest: manifest}
	byID := make(map[uuid.UUID]storage.ChallengeAsset, len(doc.Assets))
	byKey := make(map[string]storage.ChallengeAsset, len(doc.Assets))
	for _, asset := range doc.Assets {
		byID[asset.ID] = asset
		byKey[asset.Kind+":"+asset.Slug] = asset
	}
	selectedAsset := func(id uuid.UUID, kind string) (storage.ChallengeAsset, error) {
		asset, ok := byID[id]
		if !ok || asset.ChallengeVersionID != version.ID || asset.Kind != kind {
			return storage.ChallengeAsset{}, ErrInvalidSelection
		}
		return asset, nil
	}
	if conv.CharacterAssetID != nil {
		asset, err := selectedAsset(*conv.CharacterAssetID, storage.ChallengeAssetCharacter)
		if err != nil {
			return nil, err
		}
		var value storage.Character
		if err := json.Unmarshal(asset.Content, &value); err != nil {
			return nil, err
		}
		bundle.Character = &value
	}
	if conv.PersonaAssetID != nil {
		asset, err := selectedAsset(*conv.PersonaAssetID, storage.ChallengeAssetPersona)
		if err != nil {
			return nil, err
		}
		var value storage.Persona
		if err := json.Unmarshal(asset.Content, &value); err != nil {
			return nil, err
		}
		bundle.Persona = &value
	}
	if conv.ScenarioAssetID != nil {
		asset, err := selectedAsset(*conv.ScenarioAssetID, storage.ChallengeAssetScenario)
		if err != nil {
			return nil, err
		}
		var value storage.Scenario
		if err := json.Unmarshal(asset.Content, &value); err != nil {
			return nil, err
		}
		bundle.Scenario = &value
	}
	promptAsset, ok := byKey[storage.ChallengeAssetPrompt+":"+manifest.DefaultConfig.RecommendedPrompt]
	if !ok {
		return nil, fmt.Errorf("%w: 缺少 Prompt", ErrInvalidChallenge)
	}
	var prompt storage.PromptPreset
	if err := json.Unmarshal(promptAsset.Content, &prompt); err != nil {
		return nil, err
	}
	bundle.Prompt = &prompt
	for _, slug := range manifest.DefaultConfig.RequiredWorldBooks {
		asset, ok := byKey[storage.ChallengeAssetWorldBook+":"+slug]
		if !ok {
			return nil, fmt.Errorf("%w: 缺少世界书 %s", ErrInvalidChallenge, slug)
		}
		var book storage.WorldBook
		if err := json.Unmarshal(asset.Content, &book); err != nil {
			return nil, err
		}
		bundle.WorldBooks = append(bundle.WorldBooks, book)
	}
	return bundle, nil
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
	version, err := s.repo.GetVersion(ctx, versionID)
	if err != nil {
		return nil, err
	}
	assets, err := s.repo.ListAssets(ctx, versionID)
	if err != nil {
		return nil, err
	}
	rules, err := s.repo.ListRewardRules(ctx, versionID)
	if err != nil {
		return nil, err
	}
	return &VersionDocument{Version: *version, Assets: assets, RewardRules: rules}, nil
}

func (s *Service) SaveDraft(ctx context.Context, versionID uuid.UUID, in SaveVersionInput) (*VersionDocument, error) {
	if !json.Valid(in.Manifest) {
		return nil, fmt.Errorf("%w: manifest 不是合法 JSON", ErrInvalidChallenge)
	}
	if err := validateAssetDocuments(in.Assets); err != nil {
		return nil, err
	}
	if err := s.repo.ReplaceDraft(ctx, versionID, storage.JSONDoc(in.Manifest), in.Assets, in.RewardRules); err != nil {
		return nil, err
	}
	return s.VersionDocument(ctx, versionID)
}

func (s *Service) Publish(ctx context.Context, versionID, publisherID uuid.UUID) (*VersionDocument, error) {
	err := s.repo.Publish(ctx, versionID, publisherID, func(
		pack storage.ChallengePack,
		version storage.ChallengeVersion,
		assets []storage.ChallengeAsset,
		rules []storage.ChallengeRewardRule,
	) (string, error) {
		doc := &VersionDocument{Version: version, Assets: assets, RewardRules: rules}
		if err := ValidateDocument(doc); err != nil {
			return "", err
		}
		manifest, err := decodeManifest(version.Manifest)
		if err != nil {
			return "", err
		}
		if manifest.ID != pack.Slug || manifest.Name != pack.Name || manifest.Description != pack.Description {
			return "", fmt.Errorf("%w: manifest 的 id、name、description 必须与挑战包一致", ErrInvalidChallenge)
		}
		return ContentHash(version.Manifest, assets, rules)
	})
	if err != nil {
		return nil, err
	}
	return s.VersionDocument(ctx, versionID)
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
	var assets []storage.ChallengeAsset
	var rules []storage.ChallengeRewardRule
	if pack.CurrentVersionID != nil {
		source, err := s.repo.GetVersion(ctx, *pack.CurrentVersionID)
		if err != nil {
			return nil, err
		}
		manifest, err := decodeManifest(source.Manifest)
		if err != nil {
			return nil, err
		}
		manifest.Version = versionName
		manifest.RewardRules = nil
		manifestJSON, err := json.Marshal(manifest)
		if err != nil {
			return nil, err
		}
		version.Manifest = storage.JSONDoc(manifestJSON)
		assets, err = s.repo.ListAssets(ctx, source.ID)
		if err != nil {
			return nil, err
		}
		rules, err = s.repo.ListRewardRules(ctx, source.ID)
		if err != nil {
			return nil, err
		}
		for i := range assets {
			assets[i].ID = uuid.New()
			assets[i].ChallengeVersionID = version.ID
			assets[i].CreatedAt = now
			assets[i].UpdatedAt = now
		}
		for i := range rules {
			rules[i].ID = uuid.New()
			rules[i].ChallengeVersionID = version.ID
			rules[i].CreatedAt = now
			rules[i].UpdatedAt = now
		}
	}
	if err := s.repo.CreateDraftWithContent(ctx, version, assets, rules); err != nil {
		return nil, err
	}
	return s.VersionDocument(ctx, version.ID)
}

type settlementReader interface {
	GetRun(context.Context, uuid.UUID) (*storage.ChallengeRun, error)
	RewardRule(context.Context, uuid.UUID, string) (*storage.ChallengeRewardRule, error)
	SuccessfulRollTypes(context.Context, uuid.UUID, []string) ([]string, error)
}

type settlementEvaluation struct {
	rewardAmount int64
	rewardKey    *string
	detail       storage.JSONMap
}

// ValidateSettlement 在模型回复落库前做非锁定预校验。Settle 会在事务锁内重做
// 完全相同的资格检查，因此这里既阻止无效叙事落库，也不依赖预校验避免竞态。
func (s *Service) ValidateSettlement(ctx context.Context, in SettlementInput) error {
	if err := validateSettlementInput(in); err != nil {
		return err
	}
	conv, err := s.repo.Conversation(ctx, in.ConversationID)
	if err != nil {
		return err
	}
	_, err = evaluateSettlement(ctx, s.repo, conv, in)
	return err
}

// Settle 校验版本化奖励规则，并将流水、账号统计和对话结束原子落库。
func (s *Service) Settle(ctx context.Context, in SettlementInput) (*SettlementResult, error) {
	if err := validateSettlementInput(in); err != nil {
		return nil, err
	}
	if in.SettledAt.IsZero() {
		in.SettledAt = time.Now()
	}
	var settled storage.ChallengeRun
	err := s.repo.WithTx(ctx, func(tx *storage.ChallengeRepository) error {
		conv, err := tx.LockConversation(ctx, in.ConversationID)
		if err != nil {
			return err
		}
		evaluation, err := evaluateSettlement(ctx, tx, conv, in)
		if err != nil {
			return err
		}

		settled = storage.ChallengeRun{
			ID: uuid.New(), ConversationID: conv.ID, PlayerID: in.PlayerID,
			ChallengeVersionID: conv.ChallengeVersionID, Result: in.EndingType,
			RewardKey: evaluation.rewardKey, RewardAmount: evaluation.rewardAmount,
			SettlementDetail: evaluation.detail, SettledAt: in.SettledAt,
		}
		return tx.CompleteRun(ctx, conv, &settled)
	})
	if err != nil {
		return nil, err
	}
	return &SettlementResult{Run: settled}, nil
}

func validateSettlementInput(in SettlementInput) error {
	if in.EndingType != "good" && in.EndingType != "bad" && in.EndingType != "neutral" {
		return fmt.Errorf("%w: ending_type 无效", ErrSettlementDenied)
	}
	return nil
}

func evaluateSettlement(ctx context.Context, repo settlementReader, conv *storage.Conversation, in SettlementInput) (*settlementEvaluation, error) {
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

	evaluation := &settlementEvaluation{detail: storage.JSONMap{
		"ending_title": in.EndingTitle, "base_reward": maxInt64(in.BaseReward, 0),
		"character_name": in.CharacterName, "cultivation_level": in.CultivationLevel,
		"survival_turns": in.SurvivalTurns,
	}}
	if in.EndingType != "good" {
		if strings.TrimSpace(in.RewardKey) != "" {
			return nil, fmt.Errorf("%w: 非成功结局不能申请奖励", ErrSettlementDenied)
		}
		return evaluation, nil
	}
	if strings.TrimSpace(in.RewardKey) == "" {
		return nil, fmt.Errorf("%w: 成功结局缺少 reward_key", ErrSettlementDenied)
	}

	rule, err := repo.RewardRule(ctx, conv.ChallengeVersionID, in.RewardKey)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, fmt.Errorf("%w: 奖励规则不存在", ErrSettlementDenied)
		}
		return nil, err
	}
	if rule.EndingType != in.EndingType || rule.EndingTitle != in.EndingTitle {
		return nil, fmt.Errorf("%w: 结局与奖励规则不匹配", ErrSettlementDenied)
	}

	minimumRank, err := requiredCultivationRank(rule.Conditions)
	if err != nil {
		return nil, fmt.Errorf("%w: 奖励规则的境界条件无效", ErrSettlementDenied)
	}
	actualRank := cultivationRank(in.CultivationLevel)
	if actualRank < minimumRank {
		return nil, fmt.Errorf("%w: 当前境界 %q 未达到路线要求", ErrSettlementDenied, in.CultivationLevel)
	}
	requiredRolls, err := requiredSuccessfulRolls(rule.Conditions)
	if err != nil {
		return nil, fmt.Errorf("%w: 奖励规则的判定条件无效", ErrSettlementDenied)
	}
	foundRolls, err := repo.SuccessfulRollTypes(ctx, conv.ID, requiredRolls)
	if err != nil {
		return nil, err
	}
	missing := missingRequiredRolls(requiredRolls, foundRolls)
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: 尚未通过服务端判定：%s", ErrSettlementDenied, strings.Join(missing, "、"))
	}

	bps := numberAsInt64(rule.Calculation["multiplier_bps"])
	base := maxInt64(in.BaseReward, 0)
	if bps <= 0 || (base > 0 && bps > math.MaxInt64/base) {
		return nil, fmt.Errorf("%w: 奖励计算参数无效", ErrSettlementDenied)
	}
	evaluation.rewardAmount = base * bps / 10000
	if rule.MaxReward != nil && evaluation.rewardAmount > *rule.MaxReward {
		evaluation.rewardAmount = *rule.MaxReward
	}
	key := rule.RewardKey
	evaluation.rewardKey = &key
	evaluation.detail["minimum_cultivation_rank"] = minimumRank
	evaluation.detail["actual_cultivation_rank"] = actualRank
	evaluation.detail["required_successful_rolls"] = requiredRolls
	evaluation.detail["successful_required_rolls"] = foundRolls
	evaluation.detail["multiplier_bps"] = bps
	return evaluation, nil
}

func ValidateDocument(doc *VersionDocument) error {
	manifest, err := decodeManifest(doc.Version.Manifest)
	if err != nil {
		return err
	}
	if manifest.ID == "" || manifest.Name == "" || manifest.Version == "" {
		return fmt.Errorf("%w: manifest 缺少 id、name 或 version", ErrInvalidChallenge)
	}
	if manifest.DefaultConfig.RecommendedCharacter == "" || manifest.DefaultConfig.RecommendedPersona == "" ||
		manifest.DefaultConfig.RecommendedScenario == "" || manifest.DefaultConfig.RecommendedPrompt == "" {
		return fmt.Errorf("%w: manifest 缺少默认 Character、Persona、Scenario 或 Prompt", ErrInvalidChallenge)
	}
	if manifest.Version != doc.Version.Version {
		return fmt.Errorf("%w: manifest.version 必须与版本记录一致", ErrInvalidChallenge)
	}
	if err := validateAssetDocuments(doc.Assets); err != nil {
		return err
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
			if slug == "" {
				continue
			}
			if _, ok := assets[kind+":"+slug]; !ok {
				return fmt.Errorf("%w: manifest 引用了不存在的 %s/%s", ErrInvalidChallenge, kind, slug)
			}
		}
	}
	for _, slug := range manifest.DefaultConfig.AllowedPersonas {
		if _, ok := assets[storage.ChallengeAssetPersona+":"+slug]; !ok {
			return fmt.Errorf("%w: allowed_personas 引用了不存在的 %s", ErrInvalidChallenge, slug)
		}
	}
	if allowed := stringSet(manifest.DefaultConfig.AllowedPersonas); len(allowed) > 0 && !allowed[manifest.DefaultConfig.RecommendedPersona] {
		return fmt.Errorf("%w: recommended_persona 必须包含在 allowed_personas 中", ErrInvalidChallenge)
	}
	promptAsset, ok := assets[storage.ChallengeAssetPrompt+":"+manifest.DefaultConfig.RecommendedPrompt]
	if !ok {
		return fmt.Errorf("%w: 缺少默认 Prompt", ErrInvalidChallenge)
	}
	var prompt storage.PromptPreset
	if err := json.Unmarshal(promptAsset.Content, &prompt); err != nil {
		return fmt.Errorf("%w: Prompt 解析失败", ErrInvalidChallenge)
	}
	core := map[string]bool{"system_master": false, "settlement_security": false, "output_schema": false}
	for _, segment := range prompt.Segments {
		if _, required := core[segment.Key]; required && segment.Enabled && segment.Required && segment.Role == storage.RoleSystem && strings.TrimSpace(segment.Content) != "" {
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
			if entry.ID == "" || seenEntries[entry.ID] || strings.TrimSpace(entry.Content) == "" || entry.Priority < 0 ||
				(entry.TokenCount != nil && *entry.TokenCount < 0) || (!entry.Constant && len(entry.Keywords) == 0) {
				return fmt.Errorf("%w: 世界书 %s 包含无效或重复条目", ErrInvalidChallenge, slug)
			}
			seenEntries[entry.ID] = true
		}
	}
	if len(doc.RewardRules) == 0 {
		return fmt.Errorf("%w: 至少需要一条奖励规则", ErrInvalidChallenge)
	}
	seenRewards := map[string]bool{}
	for _, rule := range doc.RewardRules {
		if strings.TrimSpace(rule.RewardKey) == "" || seenRewards[rule.RewardKey] {
			return fmt.Errorf("%w: reward_key 为空或重复", ErrInvalidChallenge)
		}
		if rule.EndingType != "good" || strings.TrimSpace(rule.EndingTitle) == "" {
			return fmt.Errorf("%w: %s 必须绑定有效的成功结局", ErrInvalidChallenge, rule.RewardKey)
		}
		seenRewards[rule.RewardKey] = true
		bps := numberAsInt64(rule.Calculation["multiplier_bps"])
		if bps <= 0 || bps > 1000000 {
			return fmt.Errorf("%w: %s 缺少有效 multiplier_bps", ErrInvalidChallenge, rule.RewardKey)
		}
		if len(rule.Conditions) != 2 {
			return fmt.Errorf("%w: %s 的 conditions 只允许 min_cultivation_rank 和 required_successful_rolls", ErrInvalidChallenge, rule.RewardKey)
		}
		if _, err := requiredCultivationRank(rule.Conditions); err != nil {
			return fmt.Errorf("%w: %s 的 min_cultivation_rank 无效", ErrInvalidChallenge, rule.RewardKey)
		}
		if _, err := requiredSuccessfulRolls(rule.Conditions); err != nil {
			return fmt.Errorf("%w: %s 的 required_successful_rolls 无效: %v", ErrInvalidChallenge, rule.RewardKey, err)
		}
		if rule.MaxReward != nil && *rule.MaxReward < 0 {
			return fmt.Errorf("%w: %s 的 max_reward 不能为负数", ErrInvalidChallenge, rule.RewardKey)
		}
		if rule.EndingType == "good" && !strings.Contains(string(promptAsset.Content), rule.RewardKey) {
			return fmt.Errorf("%w: Prompt 未引用奖励 %s", ErrInvalidChallenge, rule.RewardKey)
		}
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

func ContentHash(manifest storage.JSONDoc, assets []storage.ChallengeAsset, rules []storage.ChallengeRewardRule) (string, error) {
	type hashAsset struct {
		Kind      string          `json:"kind"`
		Slug      string          `json:"slug"`
		Name      string          `json:"name"`
		Content   json.RawMessage `json:"content"`
		SortOrder int             `json:"sort_order"`
	}
	type hashRule struct {
		RewardKey   string         `json:"reward_key"`
		EndingType  string         `json:"ending_type"`
		EndingTitle string         `json:"ending_title"`
		Conditions  map[string]any `json:"conditions"`
		Calculation map[string]any `json:"calculation"`
		MaxReward   *int64         `json:"max_reward,omitempty"`
		Enabled     bool           `json:"enabled"`
	}
	assets = append([]storage.ChallengeAsset(nil), assets...)
	rules = append([]storage.ChallengeRewardRule(nil), rules...)
	sort.Slice(assets, func(i, j int) bool {
		return assets[i].Kind+":"+assets[i].Slug < assets[j].Kind+":"+assets[j].Slug
	})
	sort.Slice(rules, func(i, j int) bool { return rules[i].RewardKey < rules[j].RewardKey })
	canonicalManifest, err := canonicalJSON(manifest)
	if err != nil {
		return "", err
	}
	payload := struct {
		Manifest json.RawMessage `json:"manifest"`
		Assets   []hashAsset     `json:"assets"`
		Rules    []hashRule      `json:"rules"`
	}{Manifest: canonicalManifest}
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
	for _, rule := range rules {
		payload.Rules = append(payload.Rules, hashRule{
			RewardKey: rule.RewardKey, EndingType: rule.EndingType, EndingTitle: rule.EndingTitle,
			Conditions: rule.Conditions, Calculation: rule.Calculation,
			MaxReward: rule.MaxReward, Enabled: rule.Enabled,
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

func validSlug(value string) bool {
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return true
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
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

func requiredCultivationRank(conditions map[string]any) (int, error) {
	value, ok := conditions["min_cultivation_rank"]
	if !ok {
		return 0, errors.New("缺少字段")
	}
	rank, ok := exactInt64(value)
	if !ok || rank < 0 || rank > 8 {
		return 0, errors.New("必须是 0 到 8 的整数")
	}
	return int(rank), nil
}

func requiredSuccessfulRolls(conditions map[string]any) ([]string, error) {
	value, ok := conditions["required_successful_rolls"]
	if !ok {
		return nil, errors.New("缺少字段")
	}
	var values []string
	switch list := value.(type) {
	case []string:
		values = append(values, list...)
	case []any:
		for _, item := range list {
			text, ok := item.(string)
			if !ok {
				return nil, errors.New("数组元素必须是字符串")
			}
			values = append(values, text)
		}
	default:
		return nil, errors.New("必须是字符串数组")
	}
	seen := make(map[string]bool, len(values))
	for i, value := range values {
		values[i] = strings.TrimSpace(value)
		if values[i] == "" {
			return nil, errors.New("不能包含空判定类型")
		}
		if seen[values[i]] {
			return nil, fmt.Errorf("判定类型重复: %s", values[i])
		}
		seen[values[i]] = true
	}
	return values, nil
}

func missingRequiredRolls(required, successful []string) []string {
	found := stringSet(successful)
	missing := make([]string, 0)
	for _, rollType := range required {
		if !found[rollType] {
			missing = append(missing, rollType)
		}
	}
	return missing
}

func cultivationRank(level string) int {
	normalized := strings.NewReplacer(" ", "", "\t", "", "\r", "", "\n", "").Replace(level)
	realms := []struct {
		rank  int
		names []string
	}{
		{8, []string{"大乘"}},
		{7, []string{"合体"}},
		{6, []string{"炼虚", "煉虛"}},
		{5, []string{"化神"}},
		{4, []string{"元婴", "元嬰"}},
		{3, []string{"结丹", "結丹", "金丹"}},
		{2, []string{"筑基", "築基"}},
		{1, []string{"练气", "練氣", "炼气", "煉氣"}},
		{0, []string{"凡人", "凡俗"}},
	}
	for _, realm := range realms {
		for _, name := range realm.names {
			if strings.HasPrefix(normalized, name) {
				return realm.rank
			}
		}
	}
	return -1
}

func exactInt64(value any) (int64, bool) {
	switch number := value.(type) {
	case int:
		return int64(number), true
	case int64:
		return number, true
	case float64:
		if math.Trunc(number) != number || number < math.MinInt64 || number > math.MaxInt64 {
			return 0, false
		}
		return int64(number), true
	case json.Number:
		result, err := number.Int64()
		return result, err == nil
	default:
		return 0, false
	}
}

func numberAsInt64(value any) int64 {
	switch number := value.(type) {
	case int:
		return int64(number)
	case int64:
		return number
	case float64:
		return int64(number)
	case json.Number:
		result, _ := number.Int64()
		return result
	default:
		return 0
	}
}

func formatMultiplier(bps int64) string {
	if bps == 0 {
		return ""
	}
	whole, fraction := bps/10000, (bps%10000)/1000
	if fraction == 0 {
		return fmt.Sprintf("%dx", whole)
	}
	return fmt.Sprintf("%d.%dx", whole, fraction)
}
