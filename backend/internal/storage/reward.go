package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/platformcapability"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// MessageRewriteCreditKey is retained as a storage-facing alias for callers
// that seed or inspect the reward definition. The policy itself lives in the
// platformcapability package, not in a challenge package.
const MessageRewriteCreditKey = platformcapability.MessageRewriteRewardKey

var (
	ErrInsufficientReward       = errors.New("奖励余额不足")
	ErrRewardDefinitionConflict = errors.New("奖励定义冲突")
	ErrRewardDefinitionInUse    = errors.New("奖励定义已被挑战引用")
)

type PlayerRewardView struct {
	Definition    RewardDefinition `json:"definition"`
	Balance       int64            `json:"balance"`
	GrantedTotal  int64            `json:"granted_total"`
	ConsumedTotal int64            `json:"consumed_total"`
	UpdatedAt     time.Time        `json:"updated_at"`
}

// PlayerRewardLedgerView 是面向当前玩家的公开账本视图。它不暴露玩家 ID、
// 幂等键等内部字段，并附带奖励定义，前端无需猜测 UUID 对应的名称。
type PlayerRewardLedgerView struct {
	ID             uuid.UUID        `json:"id"`
	Definition     RewardDefinition `json:"definition"`
	Operation      string           `json:"operation"`
	QuantityDelta  int64            `json:"quantity_delta"`
	SourceType     string           `json:"source_type"`
	SourceID       string           `json:"source_id"`
	ChallengeRunID *uuid.UUID       `json:"challenge_run_id,omitempty"`
	Metadata       JSONMap          `json:"metadata"`
	CreatedAt      time.Time        `json:"created_at"`
}

type RewardMutation struct {
	PlayerID       uuid.UUID
	RewardKey      string
	Quantity       int64
	SourceType     string
	SourceID       string
	IdempotencyKey string
	ChallengeRunID *uuid.UUID
	Metadata       JSONMap
}

type RewardMutationResult struct {
	Definition RewardDefinition   `json:"definition"`
	Ledger     *RewardLedgerEntry `json:"ledger,omitempty"`
	Balance    int64              `json:"balance"`
	Applied    bool               `json:"applied"`
}

// RewardRefund describes a compensating grant for a previously applied
// consumptive mutation. Refunds are idempotent and are only applied when the
// original consume ledger entry exists, so a failed preflight cannot create
// free credits.
type RewardRefund struct {
	PlayerID              uuid.UUID
	RewardKey             string
	ConsumeIdempotencyKey string
	RefundIdempotencyKey  string
	SourceID              string
	Metadata              JSONMap
}

type RewardRepository struct{ db *gorm.DB }

func NewRewardRepository(db *gorm.DB) *RewardRepository {
	return &RewardRepository{db: db}
}

func (r *RewardRepository) WithTx(ctx context.Context, fn func(*RewardRepository) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(NewRewardRepository(tx))
	})
}

func (r *RewardRepository) ListDefinitions(ctx context.Context) ([]RewardDefinition, error) {
	var rows []RewardDefinition
	err := r.db.WithContext(ctx).Order("sort_order ASC, name ASC, key ASC").Find(&rows).Error
	return rows, err
}

func (r *RewardRepository) DefinitionByKey(ctx context.Context, key string) (*RewardDefinition, error) {
	var row RewardDefinition
	err := r.db.WithContext(ctx).First(&row, "key = ?", strings.TrimSpace(key)).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &row, err
}

func (r *RewardRepository) DefinitionByID(ctx context.Context, id uuid.UUID) (*RewardDefinition, error) {
	var row RewardDefinition
	err := r.db.WithContext(ctx).First(&row, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &row, err
}

func (r *RewardRepository) CreateDefinition(ctx context.Context, row *RewardDefinition) error {
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	now := time.Now()
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	row.UpdatedAt = now
	if row.Metadata == nil {
		row.Metadata = JSONMap{}
	}
	return r.db.WithContext(ctx).Create(row).Error
}

// UpsertDefinitionExact is used by repository-maintained challenge imports.
// Stable semantics must match; presentation fields are refreshed explicitly.
func (r *RewardRepository) UpsertDefinitionExact(ctx context.Context, wanted *RewardDefinition) (*RewardDefinition, error) {
	existing, err := r.DefinitionByKey(ctx, wanted.Key)
	if errors.Is(err, ErrNotFound) {
		if err := r.CreateDefinition(ctx, wanted); err != nil {
			return nil, err
		}
		return wanted, nil
	}
	if err != nil {
		return nil, err
	}
	if existing.Kind != wanted.Kind || existing.Stackable != wanted.Stackable || existing.Consumable != wanted.Consumable {
		return nil, fmt.Errorf("%w: %s", ErrRewardDefinitionConflict, wanted.Key)
	}
	updates := map[string]any{
		"name": wanted.Name, "description": wanted.Description, "unit": wanted.Unit,
		"icon": wanted.Icon, "featured": wanted.Featured, "sort_order": wanted.SortOrder,
		"metadata": wanted.Metadata, "updated_at": time.Now(),
	}
	if err := r.db.WithContext(ctx).Model(&RewardDefinition{}).Where("id = ?", existing.ID).Updates(updates).Error; err != nil {
		return nil, err
	}
	return r.DefinitionByID(ctx, existing.ID)
}

func (r *RewardRepository) UpdateDefinition(ctx context.Context, id uuid.UUID, wanted *RewardDefinition) (*RewardDefinition, error) {
	current, err := r.DefinitionByID(ctx, id)
	if err != nil {
		return nil, err
	}
	var references int64
	if err := r.db.WithContext(ctx).Model(&ChallengeRewardRule{}).
		Where("reward_definition_id = ?", id).Count(&references).Error; err != nil {
		return nil, err
	}
	if references > 0 && (wanted.Key != current.Key || wanted.Kind != current.Kind ||
		wanted.Stackable != current.Stackable || wanted.Consumable != current.Consumable) {
		return nil, ErrRewardDefinitionInUse
	}
	updates := map[string]any{
		"key": wanted.Key, "kind": wanted.Kind, "name": wanted.Name,
		"description": wanted.Description, "unit": wanted.Unit, "icon": wanted.Icon,
		"stackable": wanted.Stackable, "consumable": wanted.Consumable,
		"featured": wanted.Featured, "sort_order": wanted.SortOrder,
		"metadata": wanted.Metadata, "updated_at": time.Now(),
	}
	if err := r.db.WithContext(ctx).Model(&RewardDefinition{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return nil, err
	}
	return r.DefinitionByID(ctx, id)
}

func (r *RewardRepository) ListPlayerRewards(ctx context.Context, playerID uuid.UUID) ([]PlayerRewardView, error) {
	type row struct {
		RewardDefinition
		Balance       int64     `gorm:"column:balance"`
		GrantedTotal  int64     `gorm:"column:granted_total"`
		ConsumedTotal int64     `gorm:"column:consumed_total"`
		BalanceTime   time.Time `gorm:"column:balance_updated_at"`
	}
	var rows []row
	err := r.db.WithContext(ctx).Table("reward_definitions AS d").
		Select("d.*, COALESCE(b.balance, 0) AS balance, COALESCE(b.granted_total, 0) AS granted_total, COALESCE(b.consumed_total, 0) AS consumed_total, COALESCE(b.updated_at, d.updated_at) AS balance_updated_at").
		Joins("LEFT JOIN player_reward_balances AS b ON b.reward_definition_id = d.id AND b.player_id = ?", playerID).
		Where("COALESCE(b.balance, 0) > 0 OR d.featured = TRUE").
		Order("d.sort_order ASC, d.name ASC, d.key ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]PlayerRewardView, 0, len(rows))
	for _, item := range rows {
		out = append(out, PlayerRewardView{
			Definition: item.RewardDefinition, Balance: item.Balance,
			GrantedTotal: item.GrantedTotal, ConsumedTotal: item.ConsumedTotal,
			UpdatedAt: item.BalanceTime,
		})
	}
	return out, nil
}

func (r *RewardRepository) ListLedger(ctx context.Context, playerID uuid.UUID, limit int) ([]PlayerRewardLedgerView, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	type row struct {
		RewardLedgerEntry
		RewardKey         string    `gorm:"column:reward_key"`
		RewardKind        string    `gorm:"column:reward_kind"`
		RewardName        string    `gorm:"column:reward_name"`
		RewardDescription string    `gorm:"column:reward_description"`
		RewardUnit        string    `gorm:"column:reward_unit"`
		RewardIcon        string    `gorm:"column:reward_icon"`
		RewardStackable   bool      `gorm:"column:reward_stackable"`
		RewardConsumable  bool      `gorm:"column:reward_consumable"`
		RewardFeatured    bool      `gorm:"column:reward_featured"`
		RewardSortOrder   int       `gorm:"column:reward_sort_order"`
		RewardMetadata    JSONMap   `gorm:"column:reward_metadata"`
		RewardCreatedAt   time.Time `gorm:"column:reward_created_at"`
		RewardUpdatedAt   time.Time `gorm:"column:reward_updated_at"`
	}
	var rows []row
	err := r.db.WithContext(ctx).Table("reward_ledger AS e").
		Select(`e.*,
			d.key AS reward_key, d.kind AS reward_kind, d.name AS reward_name,
			d.description AS reward_description, d.unit AS reward_unit, d.icon AS reward_icon,
			d.stackable AS reward_stackable, d.consumable AS reward_consumable,
			d.featured AS reward_featured, d.sort_order AS reward_sort_order,
			d.metadata AS reward_metadata, d.created_at AS reward_created_at,
			d.updated_at AS reward_updated_at`).
		Joins("JOIN reward_definitions AS d ON d.id = e.reward_definition_id").
		Where("e.player_id = ?", playerID).
		Order("e.created_at DESC, e.id DESC").Limit(limit).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]PlayerRewardLedgerView, 0, len(rows))
	for _, item := range rows {
		out = append(out, PlayerRewardLedgerView{
			ID: item.ID,
			Definition: RewardDefinition{
				ID: item.RewardDefinitionID, Key: item.RewardKey, Kind: item.RewardKind,
				Name: item.RewardName, Description: item.RewardDescription,
				Unit: item.RewardUnit, Icon: item.RewardIcon,
				Stackable: item.RewardStackable, Consumable: item.RewardConsumable,
				Featured: item.RewardFeatured, SortOrder: item.RewardSortOrder,
				Metadata: item.RewardMetadata, CreatedAt: item.RewardCreatedAt, UpdatedAt: item.RewardUpdatedAt,
			},
			Operation: item.Operation, QuantityDelta: item.QuantityDelta,
			SourceType: item.SourceType, SourceID: item.SourceID,
			ChallengeRunID: item.ChallengeRunID, Metadata: item.RewardLedgerEntry.Metadata,
			CreatedAt: item.RewardLedgerEntry.CreatedAt,
		})
	}
	return out, nil
}

func (r *RewardRepository) HasBalance(ctx context.Context, playerID uuid.UUID, rewardKey string, quantity int64) (bool, error) {
	if quantity <= 0 {
		return true, nil
	}
	var count int64
	err := r.db.WithContext(ctx).Table("player_reward_balances AS b").
		Joins("JOIN reward_definitions AS d ON d.id = b.reward_definition_id").
		Where("b.player_id = ? AND d.key = ? AND b.balance >= ?", playerID, rewardKey, quantity).
		Count(&count).Error
	return count > 0, err
}

func (r *RewardRepository) Grant(ctx context.Context, in RewardMutation) (*RewardMutationResult, error) {
	var result *RewardMutationResult
	err := r.WithTx(ctx, func(tx *RewardRepository) error {
		var err error
		result, err = tx.GrantInCurrentTx(ctx, in)
		return err
	})
	return result, err
}

func (r *RewardRepository) Consume(ctx context.Context, in RewardMutation) (*RewardMutationResult, error) {
	var result *RewardMutationResult
	err := r.WithTx(ctx, func(tx *RewardRepository) error {
		var err error
		result, err = tx.ConsumeInCurrentTx(ctx, in)
		return err
	})
	return result, err
}

// RefundConsumed atomically verifies the original consume and appends one
// compensating grant. Calling it repeatedly with the same refund key is safe.
// A missing consume is a no-op because the operation may have failed before
// the reservation transaction committed.
func (r *RewardRepository) RefundConsumed(ctx context.Context, in RewardRefund) (*RewardMutationResult, error) {
	var result *RewardMutationResult
	err := r.WithTx(ctx, func(tx *RewardRepository) error {
		if in.PlayerID == uuid.Nil || strings.TrimSpace(in.RewardKey) == "" ||
			strings.TrimSpace(in.ConsumeIdempotencyKey) == "" || strings.TrimSpace(in.RefundIdempotencyKey) == "" ||
			strings.TrimSpace(in.SourceID) == "" {
			return fmt.Errorf("奖励返还参数不完整")
		}
		var consumed RewardLedgerEntry
		err := tx.db.WithContext(ctx).Where("player_id = ? AND operation = ? AND idempotency_key = ? AND quantity_delta < 0",
			in.PlayerID, "consume", in.ConsumeIdempotencyKey).First(&consumed).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if in.RewardKey != "" {
			definition, definitionErr := tx.DefinitionByKey(ctx, in.RewardKey)
			if definitionErr != nil {
				return definitionErr
			}
			if definition.ID != consumed.RewardDefinitionID {
				return fmt.Errorf("奖励返还目标与原消费不匹配")
			}
		}
		metadata := in.Metadata
		if metadata == nil {
			metadata = JSONMap{}
		}
		metadata["refund_of"] = in.ConsumeIdempotencyKey
		result, err = tx.GrantInCurrentTx(ctx, RewardMutation{
			PlayerID: in.PlayerID, RewardKey: in.RewardKey, Quantity: -consumed.QuantityDelta,
			SourceType: "message_rewrite_refund", SourceID: in.SourceID,
			IdempotencyKey: in.RefundIdempotencyKey, Metadata: metadata,
		})
		return err
	})
	return result, err
}

func (r *RewardRepository) GrantInCurrentTx(ctx context.Context, in RewardMutation) (*RewardMutationResult, error) {
	if in.Quantity <= 0 {
		return nil, fmt.Errorf("授予数量必须大于 0")
	}
	return r.mutateCurrentTx(ctx, in, "grant")
}

func (r *RewardRepository) ConsumeInCurrentTx(ctx context.Context, in RewardMutation) (*RewardMutationResult, error) {
	if in.Quantity <= 0 {
		return nil, fmt.Errorf("消费数量必须大于 0")
	}
	return r.mutateCurrentTx(ctx, in, "consume")
}

func (r *RewardRepository) mutateCurrentTx(ctx context.Context, in RewardMutation, operation string) (*RewardMutationResult, error) {
	if in.PlayerID == uuid.Nil || strings.TrimSpace(in.RewardKey) == "" ||
		strings.TrimSpace(in.IdempotencyKey) == "" || strings.TrimSpace(in.SourceType) == "" ||
		strings.TrimSpace(in.SourceID) == "" {
		return nil, fmt.Errorf("奖励变更参数不完整")
	}
	definition, err := r.DefinitionByKey(ctx, in.RewardKey)
	if err != nil {
		return nil, err
	}
	if operation == "consume" && !definition.Consumable {
		return nil, fmt.Errorf("%w: %s 不可消费", ErrInsufficientReward, definition.Key)
	}

	var existing RewardLedgerEntry
	err = r.db.WithContext(ctx).First(&existing, "idempotency_key = ?", in.IdempotencyKey).Error
	if err == nil {
		balance, balanceErr := r.lockBalance(ctx, in.PlayerID, definition.ID)
		if balanceErr != nil {
			return nil, balanceErr
		}
		return &RewardMutationResult{Definition: *definition, Ledger: &existing, Balance: balance.Balance}, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&PlayerRewardBalance{
		PlayerID: in.PlayerID, RewardDefinitionID: definition.ID, UpdatedAt: time.Now(),
	}).Error; err != nil {
		return nil, err
	}
	balance, err := r.lockBalance(ctx, in.PlayerID, definition.ID)
	if err != nil {
		return nil, err
	}
	delta := in.Quantity
	if operation == "consume" {
		delta = -in.Quantity
		if balance.Balance < in.Quantity {
			return nil, ErrInsufficientReward
		}
	} else if !definition.Stackable && balance.Balance > 0 {
		return &RewardMutationResult{Definition: *definition, Balance: balance.Balance, Applied: false}, nil
	}

	balance.Balance += delta
	if operation == "grant" {
		balance.GrantedTotal += in.Quantity
	} else {
		balance.ConsumedTotal += in.Quantity
	}
	balance.UpdatedAt = time.Now()
	if err := r.db.WithContext(ctx).Save(balance).Error; err != nil {
		return nil, err
	}
	metadata := in.Metadata
	if metadata == nil {
		metadata = JSONMap{}
	}
	entry := &RewardLedgerEntry{
		ID: uuid.New(), PlayerID: in.PlayerID, RewardDefinitionID: definition.ID,
		Operation: operation, QuantityDelta: delta, SourceType: in.SourceType,
		SourceID: in.SourceID, ChallengeRunID: in.ChallengeRunID,
		IdempotencyKey: in.IdempotencyKey, Metadata: metadata, CreatedAt: time.Now(),
	}
	if err := r.db.WithContext(ctx).Create(entry).Error; err != nil {
		return nil, err
	}
	return &RewardMutationResult{
		Definition: *definition, Ledger: entry, Balance: balance.Balance, Applied: true,
	}, nil
}

func (r *RewardRepository) lockBalance(ctx context.Context, playerID, definitionID uuid.UUID) (*PlayerRewardBalance, error) {
	var row PlayerRewardBalance
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&row, "player_id = ? AND reward_definition_id = ?", playerID, definitionID).Error
	return &row, err
}
