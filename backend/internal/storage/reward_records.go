package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const MaxRewardQuantity int64 = 9007199254740991 // Exact in both Go and JavaScript JSON numbers.

var (
	ErrRewardRequestConflict = errors.New("同一请求标识对应不同内容")
	ErrRewardDisabled        = errors.New("奖励资产已停用")
	ErrRewardState           = errors.New("奖励状态不允许此操作")
	ErrRewardQuantity        = errors.New("奖励数量无效或超出安全整数范围")
	ErrRewardReversal        = errors.New("该奖励不支持冲正")
	ErrRewardAdminRequired   = errors.New("需要管理员权限")
)

// RewardRecord is the durable outcome of one award, including legitimate skips.
// Operational fields are deliberately excluded from the player JSON response.
type RewardRecord struct {
	ID                 uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	PlayerID           uuid.UUID  `gorm:"type:uuid" json:"-"`
	RewardDefinitionID uuid.UUID  `gorm:"type:uuid" json:"reward_definition_id"`
	SourceType         string     `json:"source_type"`
	SourceID           string     `json:"source_id"`
	SourceKey          string     `json:"-"`
	Snapshot           JSONMap    `gorm:"type:jsonb" json:"snapshot"`
	Metadata           JSONMap    `gorm:"type:jsonb" json:"metadata"`
	Quantity           int64      `json:"quantity"`
	DeliveryMode       string     `json:"delivery_mode"`
	Status             string     `json:"status"`
	SkipReason         string     `json:"skip_reason"`
	LedgerID           *uuid.UUID `gorm:"type:uuid" json:"ledger_id,omitempty"`
	ReversalLedgerID   *uuid.UUID `gorm:"type:uuid" json:"reversal_ledger_id,omitempty"`
	BalanceAfter       int64      `json:"balance_after"`
	ActorID            *uuid.UUID `gorm:"type:uuid" json:"-"`
	RequestID          *uuid.UUID `gorm:"type:uuid" json:"-"`
	RequestHash        string     `json:"-"`
	Purpose            string     `json:"purpose"`
	Reason             string     `json:"-"`
	PublicNote         string     `json:"public_note"`
	CancelledReason    string     `json:"cancelled_reason"`
	CreatedAt          time.Time  `json:"created_at"`
	CompletedAt        *time.Time `json:"completed_at,omitempty"`
}

type AdminRewardRecord struct {
	RewardRecord
	UserID         uuid.UUID  `json:"user_id"`
	OperatorID     *uuid.UUID `json:"operator_id,omitempty"`
	GrantRequestID *uuid.UUID `json:"request_id,omitempty"`
	InternalReason string     `json:"reason"`
}

func (r RewardRecord) AdminView() AdminRewardRecord {
	return AdminRewardRecord{r, r.PlayerID, r.ActorID, r.RequestID, r.Reason}
}

type AdminRewardGrant struct {
	RequestID          uuid.UUID `json:"request_id"`
	RewardDefinitionID uuid.UUID `json:"reward_definition_id"`
	Quantity           int64     `json:"quantity"`
	Purpose            string    `json:"purpose"`
	DeliveryMode       string    `json:"delivery_mode"`
	Reason             string    `json:"reason"`
	PublicNote         string    `json:"public_note"`
}

func rewardSnapshot(def RewardDefinition) JSONMap {
	return JSONMap{"key": def.Key, "name": def.Name, "kind": def.Kind, "unit": def.Unit, "icon": def.Icon,
		"description": def.Description, "stackable": def.Stackable, "consumable": def.Consumable}
}

func (r *RewardRepository) lockRewardKey(ctx context.Context, key string) error {
	return r.db.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "reward:"+key).Error
}

func (r *RewardRepository) requireRewardAdmin(ctx context.Context, actorID uuid.UUID) error {
	var actor User
	if err := r.db.WithContext(ctx).First(&actor, "id = ?", actorID).Error; err != nil {
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
	return r.db.WithContext(ctx).Create(&AdminAuditLog{ID: uuid.New(), ActorUserID: &actor, TargetUserID: player,
		Action: action, RequestID: request, Result: "success", Metadata: metadata, CreatedAt: time.Now()}).Error
}

func (r *RewardRepository) Record(ctx context.Context, playerID *uuid.UUID, id uuid.UUID) (*RewardRecord, error) {
	var row RewardRecord
	query := r.db.WithContext(ctx).Where("id = ?", id)
	if playerID != nil {
		query = query.Where("player_id = ?", *playerID)
	}
	err := query.First(&row).Error
	return &row, asNotFound(err)
}

func (r *RewardRepository) GrantByRequest(ctx context.Context, requestID uuid.UUID) (*RewardRecord, error) {
	var row RewardRecord
	err := r.db.WithContext(ctx).First(&row, "request_id = ?", requestID).Error
	return &row, asNotFound(err)
}

// AwardInCurrentTx must run in the source transaction, including domain deductions.
func (r *RewardRepository) AwardInCurrentTx(ctx context.Context, in RewardMutation) (*RewardMutationResult, error) {
	if in.Quantity < 0 || in.Quantity > MaxRewardQuantity {
		return nil, ErrRewardQuantity
	}
	if err := r.lockRewardKey(ctx, in.IdempotencyKey); err != nil {
		return nil, err
	}
	def, err := r.DefinitionByKey(ctx, in.RewardKey)
	if err != nil {
		return nil, err
	}
	identity := fmt.Sprintf("source:%s:%s:%s:%s", in.PlayerID, in.SourceType, in.SourceID, def.ID)
	if err := r.lockRewardKey(ctx, identity); err != nil {
		return nil, err
	}
	var existing RewardRecord
	err = r.db.WithContext(ctx).Where("source_key = ? OR (player_id = ? AND source_type = ? AND source_id = ? AND reward_definition_id = ?)", in.IdempotencyKey, in.PlayerID, in.SourceType, in.SourceID, def.ID).First(&existing).Error
	if err == nil {
		if existing.PlayerID != in.PlayerID || existing.RewardDefinitionID != def.ID || existing.Quantity != in.Quantity || existing.SourceType != in.SourceType || existing.SourceID != in.SourceID {
			return nil, ErrRewardRequestConflict
		}
		return r.recordMutationResult(ctx, existing, *def)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if in.PlayerID == uuid.Nil || in.IdempotencyKey == "" || in.SourceType == "" || in.SourceID == "" {
		return nil, ErrRewardState
	}
	meta := in.Metadata
	if meta == nil {
		meta = JSONMap{}
	}
	snapshot := rewardSnapshot(*def)
	if value, ok := meta["reward_snapshot"].(JSONMap); ok && len(value) > 0 {
		for key, field := range value {
			snapshot[key] = field
		}
	}
	if value, ok := meta["reward_snapshot"].(map[string]any); ok && len(value) > 0 {
		for key, field := range value {
			snapshot[key] = field
		}
	}
	row := RewardRecord{ID: uuid.New(), PlayerID: in.PlayerID, RewardDefinitionID: def.ID, SourceType: in.SourceType,
		SourceID: in.SourceID, SourceKey: in.IdempotencyKey, Quantity: in.Quantity, Snapshot: snapshot, Metadata: meta,
		DeliveryMode: "direct", CreatedAt: time.Now()}
	result, err := r.fulfillRecord(ctx, &row, *def, in.ChallengeRunID)
	if err != nil {
		return nil, err
	}
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return nil, err
	}
	return result, nil
}

func (r *RewardRepository) recordMutationResult(ctx context.Context, row RewardRecord, def RewardDefinition) (*RewardMutationResult, error) {
	result := &RewardMutationResult{Definition: def, Balance: row.BalanceAfter}
	if row.LedgerID != nil {
		var entry RewardLedgerEntry
		if err := r.db.WithContext(ctx).First(&entry, "id = ?", *row.LedgerID).Error; err != nil {
			return nil, err
		}
		result.Ledger = &entry
	}
	return result, nil
}

func (r *RewardRepository) fulfillRecord(ctx context.Context, row *RewardRecord, def RewardDefinition, runID *uuid.UUID) (*RewardMutationResult, error) {
	now := time.Now()
	row.CompletedAt = &now
	if row.Quantity == 0 {
		row.Status, row.SkipReason = "skipped", "zero_quantity"
		return &RewardMutationResult{Definition: def}, nil
	}
	metadata := JSONMap{}
	for key, value := range row.Metadata {
		metadata[key] = value
	}
	metadata["reward_snapshot"], metadata["record_id"] = row.Snapshot, row.ID.String()
	result, err := r.mutateCurrentTx(ctx, RewardMutation{PlayerID: row.PlayerID, RewardKey: def.Key, Quantity: row.Quantity,
		SourceType: row.SourceType, SourceID: row.SourceID, IdempotencyKey: row.SourceKey, ChallengeRunID: runID,
		Metadata: metadata}, "grant")
	if err != nil {
		return nil, err
	}
	row.BalanceAfter = result.Balance
	row.Status = "granted"
	if result.Ledger == nil {
		row.Status, row.SkipReason = "skipped", "already_owned"
	} else {
		row.LedgerID = &result.Ledger.ID
	}
	return result, nil
}

func (r *RewardRepository) AdminGrant(ctx context.Context, actorID, playerID uuid.UUID, in AdminRewardGrant) (*RewardRecord, error) {
	if in.RequestID == uuid.Nil || in.RewardDefinitionID == uuid.Nil || in.Quantity <= 0 || in.Quantity > MaxRewardQuantity {
		return nil, ErrRewardQuantity
	}
	if (in.Purpose != "gift" && in.Purpose != "compensation") || (in.DeliveryMode != "direct" && in.DeliveryMode != "claim") || strings.TrimSpace(in.Reason) == "" || len(in.Reason) > 2000 || len(in.PublicNote) > 2000 {
		return nil, ErrRewardState
	}
	encoded, _ := json.Marshal(struct {
		Player uuid.UUID
		Grant  AdminRewardGrant
	}{playerID, in})
	hash := sha256.Sum256(encoded)
	fingerprint := hex.EncodeToString(hash[:])
	var row RewardRecord
	err := r.WithTx(ctx, func(tx *RewardRepository) error {
		if err := tx.requireRewardAdmin(ctx, actorID); err != nil {
			return err
		}
		if err := tx.lockRewardKey(ctx, "request:"+in.RequestID.String()); err != nil {
			return err
		}
		err := tx.db.WithContext(ctx).First(&row, "request_id = ?", in.RequestID).Error
		if err == nil {
			if row.RequestHash != fingerprint {
				return ErrRewardRequestConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var auditCount int64
		if err := tx.db.Model(&AdminAuditLog{}).Where("request_id = ? AND action LIKE 'reward_%'", in.RequestID.String()).Count(&auditCount).Error; err != nil {
			return err
		}
		if auditCount > 0 {
			return ErrRewardRequestConflict
		}
		if err := tx.db.Exec("SELECT pg_advisory_xact_lock_shared(hashtextextended('reward:configuration', 0))").Error; err != nil {
			return err
		}
		var target User
		if err := tx.db.Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).First(&target, "id = ?", playerID).Error; err != nil {
			return asNotFound(err)
		}
		if target.Status != UserStatusActive {
			return ErrTargetUserDisabled
		}
		def, err := tx.DefinitionByID(ctx, in.RewardDefinitionID)
		if err != nil {
			return err
		}
		if def.Disabled {
			return ErrRewardDisabled
		}
		if !def.Stackable && in.Quantity != 1 {
			return ErrRewardQuantity
		}
		row = RewardRecord{ID: uuid.New(), PlayerID: playerID, RewardDefinitionID: def.ID, SourceType: "admin_grant",
			SourceID: in.RequestID.String(), SourceKey: "admin:" + in.RequestID.String(), Snapshot: rewardSnapshot(*def), Metadata: JSONMap{},
			Quantity: in.Quantity, DeliveryMode: in.DeliveryMode, Status: "pending", ActorID: &actorID, RequestID: &in.RequestID, RequestHash: fingerprint,
			Purpose: in.Purpose, Reason: in.Reason, PublicNote: in.PublicNote, CreatedAt: time.Now()}
		if in.DeliveryMode == "direct" {
			if _, err := tx.fulfillRecord(ctx, &row, *def, nil); err != nil {
				return err
			}
		}
		if err := tx.db.Create(&row).Error; err != nil {
			return err
		}
		return tx.rewardAudit(ctx, actorID, &playerID, "reward_grant", in.RequestID.String(), JSONMap{"record_id": row.ID, "asset_id": def.ID, "quantity": in.Quantity, "purpose": in.Purpose, "delivery_mode": in.DeliveryMode, "reason": in.Reason, "status": row.Status, "balance_after": row.BalanceAfter})
	})
	return &row, err
}

func (r *RewardRepository) ClaimReward(ctx context.Context, playerID, recordID uuid.UUID) (*RewardRecord, error) {
	var row RewardRecord
	err := r.WithTx(ctx, func(tx *RewardRepository) error {
		if err := tx.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ? AND player_id = ?", recordID, playerID).Error; err != nil {
			return asNotFound(err)
		}
		if row.Status == "cancelled" {
			return ErrRewardState
		}
		if row.Status != "pending" {
			return nil
		}
		var target User
		if err := tx.db.Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).First(&target, "id = ?", playerID).Error; err != nil {
			return asNotFound(err)
		}
		if target.Status != UserStatusActive {
			return ErrTargetUserDisabled
		}
		def, err := tx.DefinitionByID(ctx, row.RewardDefinitionID)
		if err != nil {
			return err
		}
		if _, err := tx.fulfillRecord(ctx, &row, *def, nil); err != nil {
			return err
		}
		return tx.db.Save(&row).Error
	})
	return &row, err
}

// ManageReward serializes cancellation and reversal with claims on the record row.
func (r *RewardRepository) ManageReward(ctx context.Context, actorID, recordID uuid.UUID, action, reason string, requestID uuid.UUID) (*RewardRecord, error) {
	if strings.TrimSpace(reason) == "" || len(reason) > 2000 || requestID == uuid.Nil {
		return nil, ErrRewardState
	}
	var row RewardRecord
	err := r.WithTx(ctx, func(tx *RewardRepository) error {
		if err := tx.requireRewardAdmin(ctx, actorID); err != nil {
			return err
		}
		if err := tx.lockRewardKey(ctx, "request:"+requestID.String()); err != nil {
			return err
		}
		var audit AdminAuditLog
		err := tx.db.Where("request_id = ? AND action LIKE 'reward_%'", requestID.String()).First(&audit).Error
		if err == nil {
			if audit.Action != "reward_"+action || fmt.Sprint(audit.Metadata["record_id"]) != recordID.String() || audit.Metadata["reason"] != reason {
				return ErrRewardRequestConflict
			}
			previous, e := tx.Record(ctx, nil, recordID)
			if e == nil {
				row = *previous
			}
			return e
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.db.Clauses(clause.Locking{Strength: "UPDATE"}).First(&row, "id = ?", recordID).Error; err != nil {
			return asNotFound(err)
		}
		switch action {
		case "cancel":
			if row.Status != "pending" {
				return ErrRewardState
			}
			now := time.Now()
			row.Status = "cancelled"
			row.CancelledReason = reason
			row.CompletedAt = &now
		case "reverse":
			if row.Status != "granted" || row.ReversalLedgerID != nil || row.LedgerID == nil {
				return ErrRewardState
			}
			def, err := tx.DefinitionByID(ctx, row.RewardDefinitionID)
			if err != nil {
				return err
			}
			if row.SourceType == "reward_exchange" || !def.Stackable || (def.Kind != "currency" && def.Kind != "credit") {
				return ErrRewardReversal
			}
			result, err := tx.reverseGrant(ctx, row, *def, requestID)
			if err != nil {
				return err
			}
			row.ReversalLedgerID = &result.Ledger.ID
		case "note":
		default:
			return ErrRewardState
		}
		if err := tx.db.Save(&row).Error; err != nil {
			return err
		}
		return tx.rewardAudit(ctx, actorID, &row.PlayerID, "reward_"+action, requestID.String(), JSONMap{"record_id": row.ID, "reason": reason, "quantity": row.Quantity, "asset_id": row.RewardDefinitionID})
	})
	return &row, err
}

func (r *RewardRepository) reverseGrant(ctx context.Context, row RewardRecord, def RewardDefinition, requestID uuid.UUID) (*RewardMutationResult, error) {
	var original RewardLedgerEntry
	if err := r.db.Clauses(clause.Locking{Strength: "UPDATE"}).First(&original, "id = ? AND operation = 'grant'", *row.LedgerID).Error; err != nil {
		return nil, err
	}
	return r.mutateCurrentTx(ctx, RewardMutation{PlayerID: row.PlayerID, RewardKey: def.Key, Quantity: original.QuantityDelta,
		SourceType: "admin_reversal", SourceID: row.ID.String(), IdempotencyKey: "reverse:" + original.ID.String(), RelatedEntryID: &original.ID,
		Metadata: JSONMap{"record_id": row.ID.String(), "request_id": requestID.String(), "reward_snapshot": row.Snapshot}}, "reverse")
}

type RewardRecordFilter struct {
	PlayerID     *uuid.UUID
	DefinitionID *uuid.UUID
	Status       string
	SourceType   string
	SourceID     string
	Purpose      string
	ActorID      *uuid.UUID
	BeforeTime   *time.Time
	BeforeID     uuid.UUID
	CreatedFrom  *time.Time
	CreatedTo    *time.Time
	Limit        int
}

func (r *RewardRepository) ListRecords(ctx context.Context, f RewardRecordFilter) ([]RewardRecord, error) {
	q := r.db.WithContext(ctx).Model(&RewardRecord{})
	if f.CreatedFrom != nil {
		q = q.Where("created_at >= ?", *f.CreatedFrom)
	}
	if f.CreatedTo != nil {
		q = q.Where("created_at <= ?", *f.CreatedTo)
	}
	if f.PlayerID != nil {
		q = q.Where("player_id = ?", *f.PlayerID)
	}
	if f.DefinitionID != nil {
		q = q.Where("reward_definition_id = ?", *f.DefinitionID)
	}
	if f.Status == "reversed" {
		q = q.Where("reversal_ledger_id IS NOT NULL")
	} else if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.SourceType != "" {
		q = q.Where("source_type = ?", f.SourceType)
	}
	if f.SourceID != "" {
		q = q.Where("source_id = ?", f.SourceID)
	}
	if f.Purpose != "" {
		q = q.Where("purpose = ?", f.Purpose)
	}
	if f.ActorID != nil {
		q = q.Where("actor_id = ?", *f.ActorID)
	}
	if f.BeforeTime != nil {
		q = q.Where("(created_at, id) < (?, ?)", *f.BeforeTime, f.BeforeID)
	}
	if f.Limit < 1 || f.Limit > 201 {
		f.Limit = 51
	}
	rows := []RewardRecord{}
	err := q.Order("created_at DESC, id DESC").Limit(f.Limit).Find(&rows).Error
	return rows, err
}

func (r *RewardRepository) RecordAudits(ctx context.Context, id uuid.UUID) ([]AdminAuditLog, error) {
	rows := []AdminAuditLog{}
	err := r.db.WithContext(ctx).Where("metadata ->> 'record_id' = ?", id.String()).Order("created_at ASC, id ASC").Find(&rows).Error
	return rows, err
}
