package storage

import (
	"context"
	"encoding/json"
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
	ErrRevisionConflict = errors.New("内容或管理状态已变化，请刷新后重试")
	ErrVersionBlocked   = errors.New("该玩法版本已暂停运行")
)

type ChallengeRepository struct{ db *gorm.DB }

type PublicationValidator func(
	ChallengePack,
	ChallengeVersion,
	ChallengeRuntimeConfig,
	[]ChallengeAsset,
	[]GameplayDefinition,
	[]ChallengeOutcomeRule,
) (string, error)

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

// SetPackEnabled controls new-game availability without changing snapshots or saves.
func (r *ChallengeRepository) SetPackEnabled(ctx context.Context, id uuid.UUID, enabled bool) (*ChallengePack, error) {
	var pack *ChallengePack
	err := r.WithTx(ctx, func(tx *ChallengeRepository) error {
		var err error
		pack, err = tx.LockPack(ctx, id)
		if err != nil {
			return err
		}
		if pack.CurrentVersionID == nil {
			return ErrPublishedVersion
		}
		version, err := tx.GetVersion(ctx, *pack.CurrentVersionID)
		if err != nil {
			return err
		}
		if version.Status != ChallengeVersionPublished {
			return ErrPublishedVersion
		}
		pack.Status = ChallengePackHidden
		if enabled {
			pack.Status = ChallengePackActive
		}
		pack.UpdatedAt = time.Now()
		pack.ManagementRevision++
		return tx.SavePack(ctx, pack)
	})
	return pack, err
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

// DeleteVersionIfUnused protects all saved games, reward and import/audit references.
func (r *ChallengeRepository) DeleteVersionIfUnused(ctx context.Context, versionID uuid.UUID) error {
	return r.WithTx(ctx, func(tx *ChallengeRepository) error {
		version, err := tx.GetVersion(ctx, versionID)
		if err != nil {
			return err
		}
		pack, err := tx.LockPack(ctx, version.ChallengePackID)
		if err != nil {
			return err
		}
		if pack.CurrentVersionID != nil && *pack.CurrentVersionID == versionID {
			return ErrVersionInUse
		}
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
		if err := tx.db.WithContext(ctx).Model(&RewardLedgerEntry{}).Where("metadata->>'challenge_version_id' = ?", versionID.String()).Count(&references).Error; err != nil {
			return err
		}
		if references > 0 {
			return ErrVersionInUse
		}
		if err := tx.db.WithContext(ctx).Model(&GameplayImport{}).Where("result_version_id=?", versionID).Count(&references).Error; err != nil {
			return err
		}
		if references > 0 {
			return ErrVersionInUse
		}
		if err := tx.db.WithContext(ctx).Model(&AdminAuditLog{}).Where("metadata->>'version_id'=?", versionID.String()).Count(&references).Error; err != nil {
			return err
		}
		if references > 0 {
			return ErrVersionInUse
		}
		return tx.db.WithContext(ctx).Delete(&ChallengeVersion{}, "id = ?", versionID).Error
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

func (r *ChallengeRepository) ReplaceDraft(
	ctx context.Context,
	versionID uuid.UUID,
	manifest JSONDoc,
	runtime ChallengeRuntimeConfig,
	assets []ChallengeAsset,
	definitions []GameplayDefinition,
	outcomes []ChallengeOutcomeRule,
) error {
	return r.WithTx(ctx, func(tx *ChallengeRepository) error {
		return tx.replaceDraft(ctx, versionID, manifest, runtime, assets, definitions, outcomes)
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
		Updates(map[string]any{"manifest": manifest, "content_hash": "", "revision": version.Revision + 1, "updated_at": now}).Error; err != nil {
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
	return nil
}

func (r *ChallengeRepository) CreateDraftWithContent(
	ctx context.Context,
	version *ChallengeVersion,
	runtime ChallengeRuntimeConfig,
	assets []ChallengeAsset,
	definitions []GameplayDefinition,
	outcomes []ChallengeOutcomeRule,
) error {
	return r.WithTx(ctx, func(tx *ChallengeRepository) error {
		if err := tx.CreateVersion(ctx, version); err != nil {
			return err
		}
		return tx.replaceDraft(ctx, version.ID, version.Manifest, runtime, assets, definitions, outcomes)
	})
}

func (r *ChallengeRepository) Publish(ctx context.Context, versionID, publisherID uuid.UUID, validate PublicationValidator) error {
	return r.PublishOptions(ctx, versionID, publisherID, 0, true, validate)
}

func (r *ChallengeRepository) PublishOptions(ctx context.Context, versionID, publisherID uuid.UUID, expected int64, selectCurrent bool, validate PublicationValidator) error {
	return r.WithTx(ctx, func(tx *ChallengeRepository) error {
		if err := tx.Rewards().LockConfiguration(ctx); err != nil {
			return err
		}
		peek, err := tx.GetVersion(ctx, versionID)
		if err != nil {
			return err
		}
		pack, err := tx.LockPack(ctx, peek.ChallengePackID)
		if err != nil {
			return err
		}
		version, err := tx.LockVersion(ctx, versionID)
		if err != nil {
			return err
		}
		if version.Status != ChallengeVersionDraft {
			return ErrPublishedVersion
		}
		if expected > 0 && version.Revision != expected {
			return ErrRevisionConflict
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
		contentHash, err := validate(*pack, *version, *runtime, assets, definitions, outcomes)
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
		if err := tx.AuditGameplay(ctx, publisherID, "gameplay_publish", map[string]any{"pack_id": pack.ID, "version_id": versionID, "content_hash": contentHash, "select_current": selectCurrent}); err != nil {
			return err
		}
		if !selectCurrent {
			pack.ManagementRevision++
			return tx.SavePack(ctx, pack)
		}
		if pack.CurrentVersionID != nil && *pack.CurrentVersionID != version.ID {
			if err := tx.retireVersion(ctx, *pack.CurrentVersionID, now); err != nil {
				return err
			}
		}
		status := pack.Status
		if pack.Source == "builtin" && pack.CurrentVersionID == nil {
			status = ChallengePackActive
		}
		var manifest struct {
			Name        string
			Description string
		}
		if err := json.Unmarshal(version.Manifest, &manifest); err != nil {
			return err
		}
		if manifest.Name == "" {
			manifest.Name = pack.Name
			manifest.Description = pack.Description
		}
		return tx.db.WithContext(ctx).Model(&ChallengePack{}).
			Where("id = ?", version.ChallengePackID).
			Updates(map[string]any{
				"current_version_id": version.ID, "name": manifest.Name, "description": manifest.Description, "status": status, "management_revision": pack.ManagementRevision + 1, "updated_at": now,
			}).Error
	})
}

// retireVersion changes the new-game lifecycle only; existing games stay pinned.
func (r *ChallengeRepository) retireVersion(ctx context.Context, versionID uuid.UUID, archivedAt time.Time) error {
	return r.db.WithContext(ctx).Model(&ChallengeVersion{}).
		Where("id = ? AND status = ?", versionID, ChallengeVersionPublished).
		Updates(map[string]any{
			"status":     ChallengeVersionRetired,
			"updated_at": archivedAt,
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
	points int64,
) ([]ChallengeRunReward, error) {
	if points < 0 || points > MaxRewardQuantity {
		return nil, ErrRewardQuantity
	}
	var existing ChallengeRun
	err := r.db.WithContext(ctx).First(&existing, "conversation_id = ?", conv.ID).Error
	if err == nil {
		return nil, ErrAlreadySettled
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	playerID, err := uuid.Parse(conv.PlayerID)
	if err != nil {
		return nil, fmt.Errorf("玩家 ID 无效: %w", err)
	}
	if conv.LLMRoute != LLMRouteClient && points > 0 {
		capped, err := endingRewardCapped(ctx, r.db, playerID, run.ChallengeVersionID, run.OutcomeKey, conv.ID.String())
		if err != nil {
			return nil, err
		}
		if capped && run.SettlementDetail != nil {
			run.SettlementDetail["reward_points"] = 0
		}
		if capped {
			points = 0 // 结局照常结算，只是不再发积分
		}
	}
	if err := r.db.WithContext(ctx).Create(run).Error; err != nil {
		return nil, err
	}
	rewards := []ChallengeRunReward{}
	if conv.LLMRoute != LLMRouteClient && points > 0 {
		result, err := r.Rewards().GrantInCurrentTx(ctx, RewardMutation{PlayerID: playerID, RewardKey: PointsKey, Quantity: points, SourceType: "challenge_run", SourceID: conv.ID.String(), IdempotencyKey: "challenge_run:" + conv.ID.String(), ChallengeRunID: &run.ID, Metadata: JSONMap{"outcome_key": run.OutcomeKey, "challenge_version_id": run.ChallengeVersionID}})
		if err != nil {
			return nil, err
		}
		asset := RewardAssetFor(PointsKey)
		row := ChallengeRunReward{ChallengeRunID: run.ID, LedgerID: result.Ledger.ID, Quantity: points, RewardKeySnapshot: asset.Key, NameSnapshot: asset.Name, UnitSnapshot: asset.Unit, IconSnapshot: asset.Icon, BalanceAfter: result.Balance}
		if err := r.db.Create(&row).Error; err != nil {
			return nil, err
		}
		rewards = append(rewards, row)
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

// endingRewardWindow 内同一玩家、同一玩法包（含所有版本）、同一结局只发一次积分，
// 挑战结算与长期存档结局共用，防止重复通关刷取奖励。
const endingRewardWindow = 24 * time.Hour

// endingRewardCapped 必须在结算事务内调用；sourceID 为本次结算来源，自身的幂等重放不计入。
func endingRewardCapped(ctx context.Context, tx *gorm.DB, playerID, versionID uuid.UUID, outcomeKey, sourceID string) (bool, error) {
	db := tx.WithContext(ctx)
	if err := db.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "ending_reward:"+playerID.String()+":"+outcomeKey).Error; err != nil {
		return false, err
	}
	var granted int64
	err := db.Raw(`SELECT count(*) FROM reward_ledger rl
		JOIN challenge_versions v ON v.id::text = rl.metadata->>'challenge_version_id'
		WHERE rl.player_id = ? AND rl.operation = 'grant' AND rl.source_type IN ('campaign_outcome', 'challenge_run')
		AND rl.source_id <> ? AND rl.metadata->>'outcome_key' = ? AND rl.created_at > ?
		AND v.challenge_pack_id = (SELECT challenge_pack_id FROM challenge_versions WHERE id = ?)`,
		playerID, sourceID, outcomeKey, time.Now().Add(-endingRewardWindow), versionID).Scan(&granted).Error
	return granted > 0, err
}
