package storage

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrPublishedVersion = errors.New("已发布的挑战版本不可修改")
	ErrAlreadySettled   = errors.New("该挑战已经结算")
)

type ChallengeRepository struct{ db *gorm.DB }

type PublicationValidator func(ChallengePack, ChallengeVersion, []ChallengeAsset, []ChallengeRewardRule) (string, error)

func NewChallengeRepository(db *gorm.DB) *ChallengeRepository {
	return &ChallengeRepository{db: db}
}

func (r *ChallengeRepository) WithTx(ctx context.Context, fn func(*ChallengeRepository) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&ChallengeRepository{db: tx})
	})
}

// LockBootstrap serializes the empty-database bootstrap across server instances.
// The transaction-scoped advisory lock is released automatically on commit or rollback.
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
	err := query.Order("created_at ASC").Find(&packs).Error
	return packs, err
}

func (r *ChallengeRepository) GetPack(ctx context.Context, id uuid.UUID) (*ChallengePack, error) {
	var pack ChallengePack
	err := r.db.WithContext(ctx).First(&pack, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &pack, err
}

func (r *ChallengeRepository) GetPackBySlug(ctx context.Context, slug string) (*ChallengePack, error) {
	var pack ChallengePack
	err := r.db.WithContext(ctx).First(&pack, "slug = ?", slug).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &pack, err
}

func (r *ChallengeRepository) CreatePack(ctx context.Context, pack *ChallengePack) error {
	return r.db.WithContext(ctx).Create(pack).Error
}

func (r *ChallengeRepository) SavePack(ctx context.Context, pack *ChallengePack) error {
	return r.db.WithContext(ctx).Save(pack).Error
}

func (r *ChallengeRepository) GetVersion(ctx context.Context, id uuid.UUID) (*ChallengeVersion, error) {
	var version ChallengeVersion
	err := r.db.WithContext(ctx).First(&version, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &version, err
}

func (r *ChallengeRepository) LockVersion(ctx context.Context, id uuid.UUID) (*ChallengeVersion, error) {
	var version ChallengeVersion
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&version, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &version, err
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
	var versions []ChallengeVersion
	err := r.db.WithContext(ctx).
		Where("challenge_pack_id = ?", packID).
		Order("created_at DESC").Find(&versions).Error
	return versions, err
}

func (r *ChallengeRepository) CreateVersion(ctx context.Context, version *ChallengeVersion) error {
	return r.db.WithContext(ctx).Create(version).Error
}

func (r *ChallengeRepository) ListAssets(ctx context.Context, versionID uuid.UUID) ([]ChallengeAsset, error) {
	var assets []ChallengeAsset
	err := r.db.WithContext(ctx).
		Where("challenge_version_id = ?", versionID).
		Order("kind ASC, sort_order ASC, slug ASC").Find(&assets).Error
	return assets, err
}

func (r *ChallengeRepository) GetAsset(ctx context.Context, id uuid.UUID) (*ChallengeAsset, error) {
	var asset ChallengeAsset
	err := r.db.WithContext(ctx).First(&asset, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &asset, err
}

func (r *ChallengeRepository) AssetBySlug(ctx context.Context, versionID uuid.UUID, kind, slug string) (*ChallengeAsset, error) {
	var asset ChallengeAsset
	err := r.db.WithContext(ctx).
		Where("challenge_version_id = ? AND kind = ? AND slug = ?", versionID, kind, slug).
		First(&asset).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &asset, err
}

func (r *ChallengeRepository) ListRewardRules(ctx context.Context, versionID uuid.UUID) ([]ChallengeRewardRule, error) {
	var rules []ChallengeRewardRule
	err := r.db.WithContext(ctx).
		Where("challenge_version_id = ?", versionID).
		Order("reward_key ASC").Find(&rules).Error
	return rules, err
}

func (r *ChallengeRepository) RewardRule(ctx context.Context, versionID uuid.UUID, key string) (*ChallengeRewardRule, error) {
	var rule ChallengeRewardRule
	err := r.db.WithContext(ctx).
		Where("challenge_version_id = ? AND reward_key = ? AND enabled = ?", versionID, key, true).
		First(&rule).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &rule, err
}

func (r *ChallengeRepository) ReplaceDraft(
	ctx context.Context,
	versionID uuid.UUID,
	manifest JSONDoc,
	assets []ChallengeAsset,
	rules []ChallengeRewardRule,
) error {
	return r.WithTx(ctx, func(tx *ChallengeRepository) error {
		return tx.replaceDraft(ctx, versionID, manifest, assets, rules)
	})
}

func (r *ChallengeRepository) replaceDraft(
	ctx context.Context,
	versionID uuid.UUID,
	manifest JSONDoc,
	assets []ChallengeAsset,
	rules []ChallengeRewardRule,
) error {
	version, err := r.LockVersion(ctx, versionID)
	if err != nil {
		return err
	}
	if version.Status != ChallengeVersionDraft {
		return ErrPublishedVersion
	}
	if err := r.db.WithContext(ctx).Model(&ChallengeVersion{}).
		Where("id = ?", versionID).
		Updates(map[string]any{"manifest": manifest, "content_hash": "", "updated_at": time.Now()}).Error; err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Where("challenge_version_id = ?", versionID).Delete(&ChallengeAsset{}).Error; err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Where("challenge_version_id = ?", versionID).Delete(&ChallengeRewardRule{}).Error; err != nil {
		return err
	}
	now := time.Now()
	for i := range assets {
		assets[i].ChallengeVersionID = versionID
		if assets[i].ID == uuid.Nil {
			assets[i].ID = uuid.New()
		}
		if assets[i].CreatedAt.IsZero() {
			assets[i].CreatedAt = now
		}
		assets[i].UpdatedAt = now
		if err := r.db.WithContext(ctx).Create(&assets[i]).Error; err != nil {
			return err
		}
	}
	for i := range rules {
		rules[i].ChallengeVersionID = versionID
		if rules[i].ID == uuid.Nil {
			rules[i].ID = uuid.New()
		}
		if rules[i].CreatedAt.IsZero() {
			rules[i].CreatedAt = now
		}
		rules[i].UpdatedAt = now
		if err := r.db.WithContext(ctx).Create(&rules[i]).Error; err != nil {
			return err
		}
	}
	return nil
}

// CreateDraftWithContent 原子创建一个草稿及其完整内容。
func (r *ChallengeRepository) CreateDraftWithContent(
	ctx context.Context,
	version *ChallengeVersion,
	assets []ChallengeAsset,
	rules []ChallengeRewardRule,
) error {
	return r.WithTx(ctx, func(tx *ChallengeRepository) error {
		if err := tx.CreateVersion(ctx, version); err != nil {
			return err
		}
		return tx.replaceDraft(ctx, version.ID, version.Manifest, assets, rules)
	})
}

// Publish locks the draft and validates the exact content snapshot that is
// published. ReplaceDraft uses the same row lock, so save and publish cannot
// interleave and leave a stale content hash behind.
func (r *ChallengeRepository) Publish(ctx context.Context, versionID, publisherID uuid.UUID, validate PublicationValidator) error {
	return r.WithTx(ctx, func(tx *ChallengeRepository) error {
		version, err := tx.LockVersion(ctx, versionID)
		if err != nil {
			return err
		}
		if version.Status != ChallengeVersionDraft {
			return ErrPublishedVersion
		}
		pack, err := tx.GetPack(ctx, version.ChallengePackID)
		if err != nil {
			return err
		}
		assets, err := tx.ListAssets(ctx, versionID)
		if err != nil {
			return err
		}
		rules, err := tx.ListRewardRules(ctx, versionID)
		if err != nil {
			return err
		}
		contentHash, err := validate(*pack, *version, assets, rules)
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
		return tx.db.WithContext(ctx).Model(&ChallengePack{}).
			Where("id = ?", version.ChallengePackID).
			Updates(map[string]any{
				"current_version_id": version.ID, "status": ChallengePackActive, "updated_at": now,
			}).Error
	})
}

func (r *ChallengeRepository) GetRun(ctx context.Context, convID uuid.UUID) (*ChallengeRun, error) {
	var run ChallengeRun
	err := r.db.WithContext(ctx).First(&run, "conversation_id = ?", convID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &run, err
}

// Conversation 返回待结算对话但不加锁，供保存模型回复前的资格预校验使用。
// 最终 Settle 仍会在事务中通过 LockConversation 重做同一套校验。
func (r *ChallengeRepository) Conversation(ctx context.Context, convID uuid.UUID) (*Conversation, error) {
	var conv Conversation
	err := r.db.WithContext(ctx).First(&conv, "id = ?", convID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &conv, err
}

// SuccessfulRollTypes 返回 required 中已经成功过的精确判定类型（去重）。
func (r *ChallengeRepository) SuccessfulRollTypes(ctx context.Context, convID uuid.UUID, required []string) ([]string, error) {
	if len(required) == 0 {
		return []string{}, nil
	}
	var found []string
	err := r.db.WithContext(ctx).Model(&RollHistoryEntry{}).
		Distinct("roll_type").
		Where("conversation_id = ? AND success = ? AND roll_type IN ?", convID, true, required).
		Pluck("roll_type", &found).Error
	return found, err
}

// LockConversation 锁定待结算对话，确保同一对话只能完成一次结算。
func (r *ChallengeRepository) LockConversation(ctx context.Context, convID uuid.UUID) (*Conversation, error) {
	var conv Conversation
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&conv, "id = ?", convID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &conv, err
}

// CompleteRun 在当前事务中写入结算流水、更新账号统计并结束对话。
func (r *ChallengeRepository) CompleteRun(ctx context.Context, conv *Conversation, run *ChallengeRun) error {
	var existing ChallengeRun
	err := r.db.WithContext(ctx).First(&existing, "conversation_id = ?", conv.ID).Error
	if err == nil {
		return ErrAlreadySettled
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	playerID := conv.PlayerID
	account := Account{PlayerID: playerID}
	if err := r.db.WithContext(ctx).Where("player_id = ?", playerID).FirstOrCreate(&account, Account{
		PlayerID: playerID, RewindCount: 10, TotalRewindsEarned: 10,
	}).Error; err != nil {
		return err
	}

	accountUpdates := map[string]any{"updated_at": run.SettledAt}
	switch run.Result {
	case "good":
		accountUpdates["total_spirit_stones"] = gorm.Expr("total_spirit_stones + ?", run.RewardAmount)
		accountUpdates["successful_trials"] = gorm.Expr("successful_trials + 1")
	case "bad":
		accountUpdates["failed_trials"] = gorm.Expr("failed_trials + 1")
	}
	if err := r.db.WithContext(ctx).Model(&Account{}).
		Where("player_id = ?", playerID).Updates(accountUpdates).Error; err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Create(run).Error; err != nil {
		return err
	}
	return r.db.WithContext(ctx).Model(&Conversation{}).Where("id = ?", conv.ID).Updates(map[string]any{
		"status": StatusEnded, "ended_at": run.SettledAt, "is_processing": false, "updated_at": run.SettledAt,
	}).Error
}
