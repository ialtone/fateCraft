package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"time"
)

type AdminRewardGrant struct {
	RequestID  uuid.UUID `json:"request_id"`
	RewardKey  string    `json:"reward_key"`
	Quantity   int64     `json:"quantity"`
	Reason     string    `json:"reason"`
	PublicNote string    `json:"public_note"`
}

func (r *RewardRepository) requireRewardAdmin(ctx context.Context, actorID uuid.UUID) error {
	var actor User
	if err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).First(&actor, "id = ?", actorID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrRewardAdminRequired
		}
		return err
	}
	if !actor.IsAdmin() || actor.Status != UserStatusActive {
		return ErrRewardAdminRequired
	}
	return nil
}
func (r *RewardRepository) rewardAudit(ctx context.Context, actor uuid.UUID, player *uuid.UUID, action, request string, metadata JSONMap) error {
	return r.db.WithContext(ctx).Create(&AdminAuditLog{ID: uuid.New(), ActorUserID: &actor, TargetUserID: player, Action: action, RequestID: request, Result: "success", Metadata: metadata, CreatedAt: time.Now()}).Error
}
func requestFingerprint(value any) string {
	b, _ := json.Marshal(value)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func (r *RewardRepository) GrantByRequest(ctx context.Context, request uuid.UUID) (*RewardLedgerEntry, error) {
	var row RewardLedgerEntry
	err := r.ledgerQuery(ctx).Where("reward_ledger.request_id = ?", request).First(&row).Error
	return &row, asNotFound(err)
}
func (r *RewardRepository) AdminGrant(ctx context.Context, actor, player uuid.UUID, in AdminRewardGrant) (*RewardLedgerEntry, error) {
	if !ValidRewardKey(in.RewardKey) {
		return nil, ErrRewardKey
	}
	if in.Quantity < 1 || in.Quantity > MaxRewardQuantity {
		return nil, ErrRewardQuantity
	}
	if in.RequestID == uuid.Nil || strings.TrimSpace(in.Reason) == "" || len(in.Reason) > 2000 || len(in.PublicNote) > 2000 {
		return nil, ErrRewardState
	}
	fingerprint := requestFingerprint(struct {
		Player uuid.UUID
		Input  AdminRewardGrant
	}{player, in})
	var result *RewardLedgerEntry
	err := r.WithTx(ctx, func(tx *RewardRepository) error {
		// 按稳定顺序一次锁定操作者和收款人，也覆盖自己给自己发放的情况。
		var users []User
		if err := tx.db.WithContext(ctx).Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).Where("id IN ?", []uuid.UUID{actor, player}).Order("id ASC").Find(&users).Error; err != nil {
			return err
		}
		var operator, target *User
		for i := range users {
			if users[i].ID == actor {
				operator = &users[i]
			}
			if users[i].ID == player {
				target = &users[i]
			}
		}
		if operator == nil || !operator.IsAdmin() || operator.Status != UserStatusActive {
			return ErrRewardAdminRequired
		}
		if target == nil {
			return ErrNotFound
		}
		if err := tx.lockRewardKey(ctx, "request:"+in.RequestID.String()); err != nil {
			return err
		}
		existing, err := tx.GrantByRequest(ctx, in.RequestID)
		if err == nil {
			if existing.RequestHash != fingerprint {
				return ErrRewardRequestConflict
			}
			result = existing
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		if target.Status != UserStatusActive {
			return ErrTargetUserDisabled
		}
		grant, err := tx.GrantInCurrentTx(ctx, RewardMutation{PlayerID: player, RewardKey: in.RewardKey, Quantity: in.Quantity, SourceType: "admin_grant", SourceID: in.RequestID.String(), IdempotencyKey: "admin_grant:" + in.RequestID.String(), ActorID: &actor, RequestID: &in.RequestID, RequestHash: fingerprint, Reason: in.Reason, PublicNote: in.PublicNote})
		if err != nil {
			return err
		}
		result = grant.Ledger
		return tx.rewardAudit(ctx, actor, &player, "reward_grant", in.RequestID.String(), JSONMap{"ledger_id": result.ID, "reward_key": in.RewardKey, "quantity": in.Quantity})
	})
	return result, err
}
func (r *RewardRepository) ReverseAdminGrant(ctx context.Context, actor, id, request uuid.UUID, reason string) (*RewardLedgerEntry, error) {
	if request == uuid.Nil || strings.TrimSpace(reason) == "" || len(reason) > 2000 {
		return nil, ErrRewardState
	}
	fingerprint := requestFingerprint(struct {
		Entry  uuid.UUID
		Reason string
	}{id, reason})
	var result *RewardLedgerEntry
	err := r.WithTx(ctx, func(tx *RewardRepository) error {
		if err := tx.requireRewardAdmin(ctx, actor); err != nil {
			return err
		}
		if err := tx.lockRewardKey(ctx, "request:"+request.String()); err != nil {
			return err
		}
		previous, err := tx.GrantByRequest(ctx, request)
		if err == nil {
			if previous.RequestHash != fingerprint {
				return ErrRewardRequestConflict
			}
			result = previous
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		var original RewardLedgerEntry
		if err := tx.db.Clauses(clause.Locking{Strength: "UPDATE"}).First(&original, "id = ?", id).Error; err != nil {
			return asNotFound(err)
		}
		if original.SourceType != "admin_grant" || original.Operation != "grant" {
			return ErrRewardReversal
		}
		var count int64
		if err := tx.db.Model(&RewardLedgerEntry{}).Where("related_entry_id = ? AND operation = 'reverse'", id).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrRewardState
		}
		reversed, err := tx.mutateCurrentTx(ctx, RewardMutation{PlayerID: original.PlayerID, RewardKey: original.RewardKey, Quantity: original.QuantityDelta, SourceType: "admin_reversal", SourceID: id.String(), IdempotencyKey: "reverse:" + id.String(), RelatedEntryID: &id, ActorID: &actor, RequestID: &request, RequestHash: fingerprint, Reason: reason, PublicNote: "撤回管理员发放"}, "reverse")
		if err != nil {
			return err
		}
		result = reversed.Ledger
		return tx.rewardAudit(ctx, actor, &original.PlayerID, "reward_reverse", request.String(), JSONMap{"ledger_id": id, "reversal_id": result.ID})
	})
	return result, err
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
