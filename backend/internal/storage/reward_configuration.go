package storage

import (
	"context"
	"github.com/google/uuid"
	"gorm.io/gorm/clause"
	"time"
)

type RegistrationRewardRule struct {
	ID             int64      `json:"id"`
	Points         int64      `json:"points"`
	RewriteCredits int64      `json:"rewrite_credits"`
	ActorID        *uuid.UUID `gorm:"type:uuid" json:"actor_id,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

func (r *RewardRepository) RegistrationRule(ctx context.Context) (*RegistrationRewardRule, error) {
	var row RegistrationRewardRule
	err := r.db.WithContext(ctx).Order("id DESC").First(&row).Error
	return &row, asNotFound(err)
}
func (r *RewardRepository) SaveRegistrationRule(ctx context.Context, actor uuid.UUID, wanted RegistrationRewardRule) (*RegistrationRewardRule, error) {
	if wanted.Points < 0 || wanted.Points > MaxRewardQuantity || wanted.RewriteCredits < 0 || wanted.RewriteCredits > MaxRewardQuantity {
		return nil, ErrRewardQuantity
	}
	err := r.WithTx(ctx, func(tx *RewardRepository) error {
		if err := tx.LockConfiguration(ctx); err != nil {
			return err
		}
		if err := tx.requireRewardAdmin(ctx, actor); err != nil {
			return err
		}
		previous, err := tx.RegistrationRule(ctx)
		if err != nil {
			return err
		}
		if wanted.ID != previous.ID {
			return ErrRewardRequestConflict
		}
		wanted.ID = 0
		wanted.ActorID = &actor
		wanted.CreatedAt = time.Now()
		if err := tx.db.Create(&wanted).Error; err != nil {
			return err
		}
		return tx.rewardAudit(ctx, actor, nil, "reward_registration_rule", uuid.NewString(), JSONMap{"before": previous, "after": wanted})
	})
	return &wanted, err
}
func (r *RewardRepository) GrantRegistrationInCurrentTx(ctx context.Context, player uuid.UUID) error {
	if err := r.db.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock_shared(hashtextextended('reward:configuration', 0))").Error; err != nil {
		return err
	}
	var user User
	if err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).First(&user, "id = ?", player).Error; err != nil {
		return asNotFound(err)
	}
	if user.RegistrationRewardRevision != nil {
		return nil
	}
	rule, err := r.RegistrationRule(ctx)
	if err != nil {
		return err
	}
	for _, item := range []struct {
		key      string
		quantity int64
	}{{MessageRewriteCreditKey, rule.RewriteCredits}, {PointsKey, rule.Points}} {
		if item.quantity == 0 {
			continue
		}
		_, err := r.GrantInCurrentTx(ctx, RewardMutation{PlayerID: player, RewardKey: item.key, Quantity: item.quantity, SourceType: "user_registration", SourceID: player.String(), IdempotencyKey: "user_registration:" + player.String() + ":" + item.key, Metadata: JSONMap{"rule_revision": rule.ID}})
		if err != nil {
			return err
		}
	}
	// 即使两项都是 0，也保存本次注册使用的规则；后续重试不能套用新规则。
	return r.db.WithContext(ctx).Model(&User{}).Where("id = ?", player).Update("registration_reward_revision", rule.ID).Error
}
