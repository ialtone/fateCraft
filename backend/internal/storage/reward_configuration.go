package storage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type RegistrationRewardRule struct {
	ID                 int64      `json:"id"`
	RewardDefinitionID uuid.UUID  `gorm:"type:uuid" json:"reward_definition_id"`
	Quantity           int64      `json:"quantity"`
	Enabled            bool       `json:"enabled"`
	ActorID            *uuid.UUID `gorm:"type:uuid" json:"actor_id,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
}

type RewardDefinitionImpact struct {
	Rules          []ChallengeRewardRule   `json:"rules"`
	Pending        int64                   `json:"pending"`
	Registration   *RegistrationRewardRule `json:"registration,omitempty"`
	ExchangeOffers []RewardExchangeOffer   `json:"exchange_offers"`
}

func (r *RewardRepository) RegistrationRule(ctx context.Context) (*RegistrationRewardRule, error) {
	var row RegistrationRewardRule
	err := r.db.WithContext(ctx).Order("id DESC").First(&row).Error
	return &row, asNotFound(err)
}

func (r *RewardRepository) DefinitionImpact(ctx context.Context, id uuid.UUID) (*RewardDefinitionImpact, error) {
	result := &RewardDefinitionImpact{Rules: []ChallengeRewardRule{}}
	result.ExchangeOffers = []RewardExchangeOffer{}
	if err := r.db.WithContext(ctx).Where("cost_definition_id = ? OR reward_definition_id = ?", id, id).Order("name,id").Find(&result.ExchangeOffers).Error; err != nil {
		return nil, err
	}
	if err := r.db.WithContext(ctx).Where("reward_definition_id = ?", id).Order("challenge_version_id, id").Find(&result.Rules).Error; err != nil {
		return nil, err
	}
	if err := r.db.WithContext(ctx).Model(&RewardRecord{}).Where("reward_definition_id = ? AND status = 'pending'", id).Count(&result.Pending).Error; err != nil {
		return nil, err
	}
	rule, err := r.RegistrationRule(ctx)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	if err == nil && rule.RewardDefinitionID == id {
		result.Registration = rule
	}
	return result, nil
}

func (r *RewardRepository) SaveRegistrationRule(ctx context.Context, actor uuid.UUID, wanted RegistrationRewardRule) (*RegistrationRewardRule, error) {
	err := r.WithTx(ctx, func(tx *RewardRepository) error {
		if err := tx.requireRewardAdmin(ctx, actor); err != nil {
			return err
		}
		if err := tx.lockRewardKey(ctx, "configuration"); err != nil {
			return err
		}
		def, err := tx.DefinitionByID(ctx, wanted.RewardDefinitionID)
		if err != nil {
			return err
		}
		if wanted.Quantity < 1 || wanted.Quantity > MaxRewardQuantity || !def.Stackable && wanted.Quantity != 1 {
			return ErrRewardQuantity
		}
		if wanted.Enabled && def.Disabled {
			return ErrRewardDisabled
		}
		previous, err := tx.RegistrationRule(ctx)
		if err != nil {
			return err
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
	rule, err := r.RegistrationRule(ctx)
	if err != nil {
		return err
	}
	if !rule.Enabled {
		return nil
	}
	def, err := r.DefinitionByID(ctx, rule.RewardDefinitionID)
	if err != nil {
		return err
	}
	if def.Disabled {
		return ErrRewardDisabled
	}
	_, err = r.GrantInCurrentTx(ctx, RewardMutation{PlayerID: player, RewardKey: def.Key, Quantity: rule.Quantity, SourceType: "user_registration",
		SourceID: player.String(), IdempotencyKey: "user_registration:" + player.String() + ":" + def.Key, Metadata: JSONMap{"rule_revision": rule.ID}})
	return err
}

func (r *RewardRepository) SaveDefinition(ctx context.Context, actor uuid.UUID, id uuid.UUID, wanted RewardDefinition, disableRegistration bool) (*RewardDefinition, error) {
	var result *RewardDefinition
	err := r.WithTx(ctx, func(tx *RewardRepository) error {
		if err := tx.requireRewardAdmin(ctx, actor); err != nil {
			return err
		}
		if err := tx.lockRewardKey(ctx, "configuration"); err != nil {
			return err
		}
		if id == uuid.Nil {
			wanted.ID = uuid.New()
			wanted.CreatedBy = &actor
			if err := tx.CreateDefinition(ctx, &wanted); err != nil {
				return err
			}
			result = &wanted
			return tx.rewardAudit(ctx, actor, nil, "reward_definition_create", uuid.NewString(), JSONMap{"after": wanted})
		}
		previous, err := tx.DefinitionByID(ctx, id)
		if err != nil {
			return err
		}
		if wanted.Disabled && !previous.Disabled {
			rule, err := tx.RegistrationRule(ctx)
			if err != nil {
				return err
			}
			if rule.Enabled && rule.RewardDefinitionID == id {
				if !disableRegistration {
					return fmt.Errorf("%w: 请确认同时停用注册赠送规则", ErrRewardState)
				}
				rule.ID = 0
				rule.Enabled = false
				rule.ActorID = &actor
				rule.CreatedAt = time.Now()
				if err := tx.db.Create(rule).Error; err != nil {
					return err
				}
				if err := tx.rewardAudit(ctx, actor, nil, "reward_registration_disable", uuid.NewString(), JSONMap{"after": rule}); err != nil {
					return err
				}
			}
		}
		result, err = tx.UpdateDefinition(ctx, id, &wanted)
		if err != nil {
			return err
		}
		return tx.rewardAudit(ctx, actor, nil, "reward_definition_update", uuid.NewString(), JSONMap{"before": previous, "after": result})
	})
	return result, err
}

// ValidateNewRuleReference runs at authoring/publishing time, not at settlement.
func (r *RewardRepository) ValidateNewRuleReference(ctx context.Context, id uuid.UUID) error {
	def, err := r.DefinitionByID(ctx, id)
	if err != nil {
		return err
	}
	if def.Disabled {
		return ErrRewardDisabled
	}
	return nil
}

func (r *RewardRepository) LockConfiguration(ctx context.Context) error {
	return r.lockRewardKey(ctx, "configuration")
}

func (r *RewardRepository) AuditBuiltinImport(ctx context.Context, actor uuid.UUID, slug string, changes JSONMap) error {
	if err := r.requireRewardAdmin(ctx, actor); err != nil {
		return err
	}
	changes["slug"] = slug
	return r.rewardAudit(ctx, actor, nil, "reward_builtin_import", uuid.NewString(), changes)
}
