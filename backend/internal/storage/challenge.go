package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrPublishedVersion = errors.New("已发布的挑战版本不可修改")
	ErrVersionInUse     = errors.New("挑战版本已被存档或对话使用，无法覆盖")
	ErrAlreadySettled   = errors.New("该挑战已经结算")
)

type ChallengeRepository struct{ db *gorm.DB }

type PublicationValidator func(
	ChallengePack,
	ChallengeVersion,
	ChallengeRuntimeConfig,
	[]ChallengeAsset,
	[]GameplayDefinition,
	[]ChallengeOutcomeRule,
	[]ChallengeRewardRule,
) (string, error)

type ChallengeRewardGrant struct {
	Definition RewardDefinition
	Quantity   int64
	Snapshot   JSONMap
}

func NewChallengeRepository(db *gorm.DB) *ChallengeRepository {
	return &ChallengeRepository{db: db}
}

func (r *ChallengeRepository) WithTx(ctx context.Context, fn func(*ChallengeRepository) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(NewChallengeRepository(tx))
	})
}

func (r *ChallengeRepository) Rewards() *RewardRepository {
	return NewRewardRepository(r.db)
}

func (r *ChallengeRepository) LockBootstrap(ctx context.Context) error {
	return r.db.WithContext(ctx).
		Exec("SELECT pg_advisory_xact_lock(hashtext('fatecraft:challenge-bootstrap'))").Error
}

func (r *ChallengeRepository) CountPacks(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&ChallengePack{}).Count(&count).Error
	return count, err
}

func (r *ChallengeRepository) ListPacks(ctx context.Context, publishedOnly bool) ([]ChallengePack, error) {
	query := r.db.WithContext(ctx)
	if publishedOnly {
		query = query.Where("status = ? AND current_version_id IS NOT NULL", ChallengePackActive)
	}
	var packs []ChallengePack
	err := query.Order("created_at ASC, id ASC").Limit(100).Find(&packs).Error
	return packs, err
}

func (r *ChallengeRepository) GetPack(ctx context.Context, id uuid.UUID) (*ChallengePack, error) {
	var row ChallengePack
	err := r.db.WithContext(ctx).First(&row, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &row, err
}

// LockPack serializes publication of versions belonging to the same
// challenge. Without this lock two concurrent publishes can both observe the
// same old current_version_id and retire/update inconsistent versions.
func (r *ChallengeRepository) LockPack(ctx context.Context, id uuid.UUID) (*ChallengePack, error) {
	var row ChallengePack
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&row, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &row, err
}

func (r *ChallengeRepository) GetPackBySlug(ctx context.Context, slug string) (*ChallengePack, error) {
	var row ChallengePack
	err := r.db.WithContext(ctx).First(&row, "slug = ?", slug).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &row, err
}

func (r *ChallengeRepository) CreatePack(ctx context.Context, row *ChallengePack) error {
	return r.db.WithContext(ctx).Create(row).Error
}

func (r *ChallengeRepository) SavePack(ctx context.Context, row *ChallengePack) error {
	return r.db.WithContext(ctx).Save(row).Error
}

func (r *ChallengeRepository) GetVersion(ctx context.Context, id uuid.UUID) (*ChallengeVersion, error) {
	var row ChallengeVersion
	err := r.db.WithContext(ctx).First(&row, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &row, err
}

func (r *ChallengeRepository) LockVersion(ctx context.Context, id uuid.UUID) (*ChallengeVersion, error) {
	var row ChallengeVersion
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&row, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &row, err
}

func (r *ChallengeRepository) VersionByNumber(ctx context.Context, packID uuid.UUID, version string) (*ChallengeVersion, error) {
	var row ChallengeVersion
	err := r.db.WithContext(ctx).
		Where("challenge_pack_id = ? AND version = ?", packID, version).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &row, err
}

func (r *ChallengeRepository) ListVersions(ctx context.Context, packID uuid.UUID) ([]ChallengeVersion, error) {
	var rows []ChallengeVersion
	err := r.db.WithContext(ctx).Where("challenge_pack_id = ?", packID).
		Order("created_at DESC, id DESC").Find(&rows).Error
	return rows, err
}

func (r *ChallengeRepository) CreateVersion(ctx context.Context, row *ChallengeVersion) error {
	return r.db.WithContext(ctx).Create(row).Error
}

// DeleteVersionIfUnused removes a version so repository-maintained built-in
// content can recreate the same version number. Existing conversations keep
// published snapshots immutable and therefore block this operation.
func (r *ChallengeRepository) DeleteVersionIfUnused(ctx context.Context, versionID uuid.UUID) error {
	return r.WithTx(ctx, func(tx *ChallengeRepository) error {
		if _, err := tx.LockVersion(ctx, versionID); err != nil {
			return err
		}
		var references int64
		if err := tx.db.WithContext(ctx).Model(&Conversation{}).
			Where("challenge_version_id = ?", versionID).Count(&references).Error; err != nil {
			return err
		}
		if references > 0 {
			return ErrVersionInUse
		}
		if err := tx.db.WithContext(ctx).Model(&Campaign{}).
			Where("challenge_version_id = ?", versionID).Count(&references).Error; err != nil {
			return err
		}
		if references > 0 {
			return ErrVersionInUse
		}
		now := time.Now()
		if err := tx.db.WithContext(ctx).Model(&ChallengePack{}).
			Where("current_version_id = ?", versionID).
			Updates(map[string]any{
				"current_version_id": nil,
				"status":             ChallengePackHidden,
				"updated_at":         now,
			}).Error; err != nil {
			return err
		}
		return tx.db.WithContext(ctx).Delete(&ChallengeVersion{}, "id = ?", versionID).Error
	})
}

// ActivatePublishedVersion switches the pack entry to an already published
// snapshot. It is used when an explicit built-in overwrite selects repository
// content that is already present but is not the current version.
func (r *ChallengeRepository) ActivatePublishedVersion(ctx context.Context, packID, versionID uuid.UUID) error {
	return r.WithTx(ctx, func(tx *ChallengeRepository) error {
		version, err := tx.LockVersion(ctx, versionID)
		if err != nil {
			return err
		}
		if version.ChallengePackID != packID {
			return ErrNotFound
		}
		if version.Status != ChallengeVersionPublished {
			return ErrPublishedVersion
		}
		result := tx.db.WithContext(ctx).Model(&ChallengePack{}).
			Where("id = ?", packID).
			Updates(map[string]any{
				"current_version_id": versionID,
				"status":             ChallengePackActive,
				"updated_at":         time.Now(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrNotFound
		}
		return nil
	})
}

func (r *ChallengeRepository) GetRuntime(ctx context.Context, versionID uuid.UUID) (*ChallengeRuntimeConfig, error) {
	var row ChallengeRuntimeConfig
	err := r.db.WithContext(ctx).First(&row, "challenge_version_id = ?", versionID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &row, err
}

func (r *ChallengeRepository) ListAssets(ctx context.Context, versionID uuid.UUID) ([]ChallengeAsset, error) {
	var rows []ChallengeAsset
	err := r.db.WithContext(ctx).Where("challenge_version_id = ?", versionID).
		Order("kind ASC, sort_order ASC, slug ASC").Find(&rows).Error
	return rows, err
}

func (r *ChallengeRepository) ListDefinitions(ctx context.Context, versionID uuid.UUID) ([]GameplayDefinition, error) {
	var rows []GameplayDefinition
	err := r.db.WithContext(ctx).Where("challenge_version_id = ?", versionID).
		Order("kind ASC, sort_order ASC, key ASC").Find(&rows).Error
	return rows, err
}

func (r *ChallengeRepository) GetAsset(ctx context.Context, id uuid.UUID) (*ChallengeAsset, error) {
	var row ChallengeAsset
	err := r.db.WithContext(ctx).First(&row, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &row, err
}

func (r *ChallengeRepository) AssetBySlug(ctx context.Context, versionID uuid.UUID, kind, slug string) (*ChallengeAsset, error) {
	var row ChallengeAsset
	err := r.db.WithContext(ctx).
		Where("challenge_version_id = ? AND kind = ? AND slug = ?", versionID, kind, slug).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &row, err
}

func (r *ChallengeRepository) ListOutcomeRules(ctx context.Context, versionID uuid.UUID) ([]ChallengeOutcomeRule, error) {
	var rows []ChallengeOutcomeRule
	err := r.db.WithContext(ctx).Where("challenge_version_id = ?", versionID).
		Order("outcome_key ASC").Find(&rows).Error
	return rows, err
}

func (r *ChallengeRepository) OutcomeRule(ctx context.Context, versionID uuid.UUID, key string) (*ChallengeOutcomeRule, error) {
	var row ChallengeOutcomeRule
	err := r.db.WithContext(ctx).
		Where("challenge_version_id = ? AND outcome_key = ? AND enabled = ?", versionID, key, true).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &row, err
}

func (r *ChallengeRepository) ListRewardRules(ctx context.Context, versionID uuid.UUID) ([]ChallengeRewardRule, error) {
	var rows []ChallengeRewardRule
	err := r.db.WithContext(ctx).Where("challenge_version_id = ?", versionID).
		Order("outcome_rule_id ASC, reward_definition_id ASC").Find(&rows).Error
	return rows, err
}

func (r *ChallengeRepository) RewardRulesForOutcome(ctx context.Context, outcomeID uuid.UUID) ([]ChallengeRewardRule, error) {
	var rows []ChallengeRewardRule
	err := r.db.WithContext(ctx).
		Where("outcome_rule_id = ? AND enabled = ?", outcomeID, true).
		Order("reward_definition_id ASC").Find(&rows).Error
	return rows, err
}

func (r *ChallengeRepository) RewardDefinition(ctx context.Context, id uuid.UUID) (*RewardDefinition, error) {
	return r.Rewards().DefinitionByID(ctx, id)
}

func (r *ChallengeRepository) ReplaceDraft(
	ctx context.Context,
	versionID uuid.UUID,
	manifest JSONDoc,
	runtime ChallengeRuntimeConfig,
	assets []ChallengeAsset,
	definitions []GameplayDefinition,
	outcomes []ChallengeOutcomeRule,
	rules []ChallengeRewardRule,
) error {
	return r.WithTx(ctx, func(tx *ChallengeRepository) error {
		return tx.replaceDraft(ctx, versionID, manifest, runtime, assets, definitions, outcomes, rules)
	})
}

func (r *ChallengeRepository) replaceDraft(
	ctx context.Context,
	versionID uuid.UUID,
	manifest JSONDoc,
	runtime ChallengeRuntimeConfig,
	assets []ChallengeAsset,
	definitions []GameplayDefinition,
	outcomes []ChallengeOutcomeRule,
	rules []ChallengeRewardRule,
) error {
	version, err := r.LockVersion(ctx, versionID)
	if err != nil {
		return err
	}
	if version.Status != ChallengeVersionDraft {
		return ErrPublishedVersion
	}
	now := time.Now()
	if err := r.db.WithContext(ctx).Model(&ChallengeVersion{}).Where("id = ?", versionID).
		Updates(map[string]any{"manifest": manifest, "content_hash": "", "updated_at": now}).Error; err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Where("challenge_version_id = ?", versionID).Delete(&ChallengeRewardRule{}).Error; err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Where("challenge_version_id = ?", versionID).Delete(&ChallengeOutcomeRule{}).Error; err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Where("challenge_version_id = ?", versionID).Delete(&ChallengeAsset{}).Error; err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Where("challenge_version_id = ?", versionID).Delete(&GameplayDefinition{}).Error; err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Where("challenge_version_id = ?", versionID).Delete(&ChallengeRuntimeConfig{}).Error; err != nil {
		return err
	}

	runtime.ChallengeVersionID = versionID
	runtime.CreatedAt = now
	runtime.UpdatedAt = now
	if err := r.db.WithContext(ctx).Create(&runtime).Error; err != nil {
		return err
	}
	for index := range assets {
		assets[index].ChallengeVersionID = versionID
		if assets[index].ID == uuid.Nil {
			assets[index].ID = uuid.New()
		}
		if assets[index].CreatedAt.IsZero() {
			assets[index].CreatedAt = now
		}
		assets[index].UpdatedAt = now
		if err := r.db.WithContext(ctx).Create(&assets[index]).Error; err != nil {
			return err
		}
	}
	for index := range definitions {
		definitions[index].ChallengeVersionID = versionID
		if definitions[index].Tags == nil {
			definitions[index].Tags = make([]string, 0)
		}
		if definitions[index].ID == uuid.Nil {
			definitions[index].ID = uuid.New()
		}
		if definitions[index].CreatedAt.IsZero() {
			definitions[index].CreatedAt = now
		}
		definitions[index].UpdatedAt = now
		if err := r.db.WithContext(ctx).Create(&definitions[index]).Error; err != nil {
			return err
		}
	}
	for index := range outcomes {
		outcomes[index].ChallengeVersionID = versionID
		if outcomes[index].ID == uuid.Nil {
			outcomes[index].ID = uuid.New()
		}
		if outcomes[index].CreatedAt.IsZero() {
			outcomes[index].CreatedAt = now
		}
		outcomes[index].UpdatedAt = now
		if err := r.db.WithContext(ctx).Create(&outcomes[index]).Error; err != nil {
			return err
		}
	}
	for index := range rules {
		rules[index].ChallengeVersionID = versionID
		if rules[index].ID == uuid.Nil {
			rules[index].ID = uuid.New()
		}
		if rules[index].CreatedAt.IsZero() {
			rules[index].CreatedAt = now
		}
		rules[index].UpdatedAt = now
		if err := r.db.WithContext(ctx).Create(&rules[index]).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *ChallengeRepository) CreateDraftWithContent(
	ctx context.Context,
	version *ChallengeVersion,
	runtime ChallengeRuntimeConfig,
	assets []ChallengeAsset,
	definitions []GameplayDefinition,
	outcomes []ChallengeOutcomeRule,
	rules []ChallengeRewardRule,
) error {
	return r.WithTx(ctx, func(tx *ChallengeRepository) error {
		if err := tx.CreateVersion(ctx, version); err != nil {
			return err
		}
		return tx.replaceDraft(ctx, version.ID, version.Manifest, runtime, assets, definitions, outcomes, rules)
	})
}

func (r *ChallengeRepository) Publish(ctx context.Context, versionID, publisherID uuid.UUID, validate PublicationValidator) error {
	return r.WithTx(ctx, func(tx *ChallengeRepository) error {
		if err := tx.Rewards().LockConfiguration(ctx); err != nil {
			return err
		}
		version, err := tx.LockVersion(ctx, versionID)
		if err != nil {
			return err
		}
		if version.Status != ChallengeVersionDraft {
			return ErrPublishedVersion
		}
		pack, err := tx.LockPack(ctx, version.ChallengePackID)
		if err != nil {
			return err
		}
		runtime, err := tx.GetRuntime(ctx, versionID)
		if err != nil {
			return err
		}
		assets, err := tx.ListAssets(ctx, versionID)
		if err != nil {
			return err
		}
		definitions, err := tx.ListDefinitions(ctx, versionID)
		if err != nil {
			return err
		}
		outcomes, err := tx.ListOutcomeRules(ctx, versionID)
		if err != nil {
			return err
		}
		rules, err := tx.ListRewardRules(ctx, versionID)
		if err != nil {
			return err
		}
		for _, rule := range rules {
			if err := tx.Rewards().ValidateNewRuleReference(ctx, rule.RewardDefinitionID); err != nil {
				return err
			}
		}
		contentHash, err := validate(*pack, *version, *runtime, assets, definitions, outcomes, rules)
		if err != nil {
			return err
		}
		now := time.Now()
		var publisher *uuid.UUID
		if publisherID != uuid.Nil {
			publisher = &publisherID
		}
		result := tx.db.WithContext(ctx).Model(&ChallengeVersion{}).
			Where("id = ? AND status = ?", version.ID, ChallengeVersionDraft).
			Updates(map[string]any{
				"status": ChallengeVersionPublished, "content_hash": contentHash,
				"published_by": publisher, "published_at": now, "updated_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrPublishedVersion
		}
		// 发布同一玩法的新版本后，旧版本必须退役。普通对话不再允许
		// 继续生成，但保留全部消息和状态用于浏览；长期 Campaign 存档
		// 仍由其自身生命周期管理，不在这里强制中断领域动作。
		if pack.CurrentVersionID != nil && *pack.CurrentVersionID != version.ID {
			if err := tx.retireVersionAndArchiveConversations(ctx, *pack.CurrentVersionID, now); err != nil {
				return err
			}
		}
		return tx.db.WithContext(ctx).Model(&ChallengePack{}).
			Where("id = ?", version.ChallengePackID).
			Updates(map[string]any{
				"current_version_id": version.ID, "status": ChallengePackActive, "updated_at": now,
			}).Error
	})
}

// retireVersionAndArchiveConversations freezes a superseded version without
// deleting it. The foreign keys intentionally keep old snapshots addressable
// for read-only replay and audit. Only ordinary conversations are archived;
// Campaigns are independent long-lived saves and remain pinned to their
// original version until their own lifecycle ends.
func (r *ChallengeRepository) retireVersionAndArchiveConversations(ctx context.Context, versionID uuid.UUID, archivedAt time.Time) error {
	if err := r.db.WithContext(ctx).Model(&ChallengeVersion{}).
		Where("id = ? AND status = ?", versionID, ChallengeVersionPublished).
		Updates(map[string]any{
			"status":     ChallengeVersionRetired,
			"updated_at": archivedAt,
		}).Error; err != nil {
		return err
	}
	return r.db.WithContext(ctx).Model(&Conversation{}).
		Where("challenge_version_id = ? AND campaign_id IS NULL AND status = ?", versionID, StatusActive).
		Updates(map[string]any{
			"status":      StatusArchived,
			"archived_at": archivedAt,
			"updated_at":  archivedAt,
		}).Error
}

func (r *ChallengeRepository) GetRun(ctx context.Context, convID uuid.UUID) (*ChallengeRun, error) {
	var row ChallengeRun
	err := r.db.WithContext(ctx).First(&row, "conversation_id = ?", convID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &row, err
}

func (r *ChallengeRepository) ListRunRewards(ctx context.Context, runID uuid.UUID) ([]ChallengeRunReward, error) {
	var rows []ChallengeRunReward
	err := r.db.WithContext(ctx).Where("challenge_run_id = ?", runID).
		Order("reward_key_snapshot ASC").Find(&rows).Error
	return rows, err
}

func (r *ChallengeRepository) Conversation(ctx context.Context, convID uuid.UUID) (*Conversation, error) {
	var row Conversation
	err := r.db.WithContext(ctx).First(&row, "id = ?", convID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &row, err
}

func (r *ChallengeRepository) SuccessfulRollKeys(ctx context.Context, convID uuid.UUID) ([]string, error) {
	var found []string
	// 只统计当前分支仍然有效的判定；重新生成或改写作废的旧分支判定不能作为结算证据。
	err := r.db.WithContext(ctx).Model(&RollHistoryEntry{}).Distinct("roll_key").
		Where("conversation_id = ? AND success = ? AND invalidated_at IS NULL", convID, true).
		Pluck("roll_key", &found).Error
	return found, err
}

func (r *ChallengeRepository) LockConversation(ctx context.Context, convID uuid.UUID) (*Conversation, error) {
	var row Conversation
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&row, "id = ?", convID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &row, err
}

// CompleteRun 在结算事务内调用：此时对话已加行锁，所有可信校验均已通过。
// 它只把对话标记为 ended，不触碰 is_processing：忙碌投影只能由持有生成租约的
// Worker 在租约结束时释放，结算后同一 attempt 还会继续写收尾叙事。
func (r *ChallengeRepository) CompleteRun(
	ctx context.Context,
	conv *Conversation,
	run *ChallengeRun,
	grants []ChallengeRewardGrant,
) ([]ChallengeRunReward, error) {
	var existing ChallengeRun
	err := r.db.WithContext(ctx).First(&existing, "conversation_id = ?", conv.ID).Error
	if err == nil {
		return nil, ErrAlreadySettled
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err := r.db.WithContext(ctx).Create(run).Error; err != nil {
		return nil, err
	}

	playerID, err := uuid.Parse(conv.PlayerID)
	if err != nil {
		return nil, fmt.Errorf("玩家 ID 无效: %w", err)
	}
	rewards := make([]ChallengeRunReward, 0, len(grants))
	// client 路由：结局校验、challenge_runs 写入与对话结束照常，只跳过奖励授予。
	// 路由在事务内直接取自已加行锁的对话行，不在调用方判断，保证所有入口一致。
	if conv.LLMRoute != LLMRouteClient {
		ids := make([]uuid.UUID, 0, len(grants))
		for _, grant := range grants {
			ids = append(ids, grant.Definition.ID)
		}
		if err := r.Rewards().LockBalances(ctx, playerID, ids); err != nil {
			return nil, err
		}
		for _, grant := range grants {
			result, err := r.Rewards().GrantInCurrentTx(ctx, RewardMutation{
				PlayerID: playerID, RewardKey: grant.Definition.Key, Quantity: grant.Quantity,
				SourceType: "challenge_run", SourceID: run.ID.String(),
				IdempotencyKey: "challenge_run:" + run.ID.String() + ":" + grant.Definition.Key,
				ChallengeRunID: &run.ID, Metadata: JSONMap{"outcome_key": run.OutcomeKey, "challenge_version_id": run.ChallengeVersionID.String(), "reward_snapshot": grant.Snapshot},
			})
			if err != nil {
				return nil, err
			}
			if !result.Applied || result.Ledger == nil {
				continue
			}
			snapshot := grant.Snapshot
			row := ChallengeRunReward{
				ChallengeRunID: run.ID, RewardDefinitionID: grant.Definition.ID,
				LedgerID: result.Ledger.ID, Quantity: grant.Quantity,
				RewardKeySnapshot: grant.Definition.Key,
				NameSnapshot:      stringFromMap(snapshot, "name", grant.Definition.Name),
				UnitSnapshot:      stringFromMap(snapshot, "unit", grant.Definition.Unit),
				IconSnapshot:      stringFromMap(snapshot, "icon", grant.Definition.Icon),
				BalanceAfter:      result.Balance,
			}
			if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
				return nil, err
			}
			rewards = append(rewards, row)
		}
	}
	if err := r.db.WithContext(ctx).Model(&Conversation{}).Where("id = ?", conv.ID).
		Updates(map[string]any{
			"status": StatusEnded, "ended_at": run.SettledAt, "updated_at": run.SettledAt,
		}).Error; err != nil {
		return nil, err
	}
	return rewards, nil
}

func stringFromMap(values JSONMap, key, fallback string) string {
	if value, ok := values[key].(string); ok && value != "" {
		return value
	}
	return fallback
}
