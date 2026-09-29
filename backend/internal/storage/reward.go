package storage

import (
	"context"
	"errors"
	"fmt"
	"sort"
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
	ErrRewardDefinitionInUse    = errors.New("奖励资产已被使用，不能改变 Key、类型、堆叠或消费语义")
)

type PlayerRewardView struct {
	RefundedTotal int64            `json:"refunded_total"`
	ReversedTotal int64            `json:"reversed_total"`
	Definition    RewardDefinition `json:"definition"`
	Balance       int64            `json:"balance"`
	GrantedTotal  int64            `json:"granted_total"`
	ConsumedTotal int64            `json:"consumed_total"`
	UpdatedAt     time.Time        `json:"updated_at"`
}

// PlayerRewardLedgerView 是面向当前玩家的公开账本视图。它不暴露玩家 ID、
// 幂等键等内部字段，并附带奖励定义，前端无需猜测 UUID 对应的名称。
type PlayerRewardLedgerView struct {
	RelatedEntryID *uuid.UUID       `json:"related_entry_id,omitempty"`
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
	RelatedEntryID *uuid.UUID
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
	var otherReferences int64
	if err := r.db.WithContext(ctx).Raw(`SELECT
		(SELECT count(*) FROM reward_records WHERE reward_definition_id = ?) +
		(SELECT count(*) FROM reward_ledger WHERE reward_definition_id = ?) +
		(SELECT count(*) FROM player_reward_balances WHERE reward_definition_id = ?) +
		(SELECT count(*) FROM registration_reward_rules WHERE reward_definition_id = ?) +
		(SELECT count(*) FROM reward_exchange_offers WHERE cost_definition_id = ? OR reward_definition_id = ?) +
		(SELECT count(*) FROM reward_exchange_revisions WHERE snapshot->>'cost_definition_id' = ? OR snapshot->>'reward_definition_id' = ?)`, id, id, id, id, id, id, id.String(), id.String()).Scan(&otherReferences).Error; err != nil {
		return nil, err
	}
	if (references+otherReferences > 0 || current.Key == MessageRewriteCreditKey) && (wanted.Key != current.Key || wanted.Kind != current.Kind ||
		wanted.Stackable != current.Stackable || wanted.Consumable != current.Consumable) {
		return nil, ErrRewardDefinitionInUse
	}
	updates := map[string]any{
		"key": wanted.Key, "kind": wanted.Kind, "name": wanted.Name,
		"disabled":    wanted.Disabled,
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
		RefundedTotal int64     `gorm:"column:refunded_total"`
		ReversedTotal int64     `gorm:"column:reversed_total"`
		BalanceTime   time.Time `gorm:"column:balance_updated_at"`
	}
	var rows []row
	err := r.db.WithContext(ctx).Table("reward_definitions AS d").
		Select("d.*, COALESCE(b.balance, 0) AS balance, COALESCE(b.granted_total, 0) AS granted_total, COALESCE(b.consumed_total, 0) AS consumed_total, COALESCE(b.refunded_total, 0) AS refunded_total, COALESCE(b.reversed_total, 0) AS reversed_total, COALESCE(b.updated_at, d.updated_at) AS balance_updated_at").
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
			UpdatedAt:     item.BalanceTime,
			RefundedTotal: item.RefundedTotal, ReversedTotal: item.ReversedTotal,
		})
	}
	return out, nil
}

type RewardPageCursor struct {
	Time         time.Time
	ID           uuid.UUID
	DefinitionID *uuid.UUID
}

func (r *RewardRepository) ListLedger(ctx context.Context, playerID uuid.UUID, limit int, cursors ...RewardPageCursor) ([]PlayerRewardLedgerView, error) {
	if limit <= 0 || limit > 201 {
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
	q := r.db.WithContext(ctx).Table("reward_ledger AS e")
	if len(cursors) > 0 {
		cursor := cursors[0]
		if !cursor.Time.IsZero() {
			q = q.Where("(e.created_at, e.id) < (?, ?)", cursor.Time, cursor.ID)
		}
		if cursor.DefinitionID != nil {
			q = q.Where("e.reward_definition_id = ?", *cursor.DefinitionID)
		}
	}
	err := q.
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
			ChallengeRunID: item.ChallengeRunID, Metadata: publicRewardMetadata(item.RewardLedgerEntry.Metadata),
			CreatedAt:      item.RewardLedgerEntry.CreatedAt,
			RelatedEntryID: item.RelatedEntryID,
		})
		if snapshot, ok := item.RewardLedgerEntry.Metadata["reward_snapshot"].(map[string]any); ok {
			view := &out[len(out)-1].Definition
			view.Name = stringFromMap(JSONMap(snapshot), "name", view.Name)
			view.Description = stringFromMap(JSONMap(snapshot), "description", view.Description)
			view.Unit = stringFromMap(JSONMap(snapshot), "unit", view.Unit)
			view.Icon = stringFromMap(JSONMap(snapshot), "icon", view.Icon)
		}
	}
	return out, nil
}

func publicRewardMetadata(metadata JSONMap) JSONMap {
	result := JSONMap{}
	for _, key := range []string{"reward_snapshot", "record_id", "outcome_key", "challenge_version_id", "extraction", "offer_id", "offer_revision"} {
		if value, ok := metadata[key]; ok {
			result[key] = value
		}
	}
	return result
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
		err := tx.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("player_id = ? AND operation = ? AND idempotency_key = ? AND quantity_delta < 0",
			in.PlayerID, "consume", in.ConsumeIdempotencyKey).First(&consumed).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if consumed.SourceType == "reward_exchange" {
			return ErrRewardReversal
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
		if snapshot, ok := consumed.Metadata["reward_snapshot"]; ok {
			metadata["reward_snapshot"] = snapshot
		}
		result, err = tx.mutateCurrentTx(ctx, RewardMutation{
			PlayerID: in.PlayerID, RewardKey: in.RewardKey, Quantity: -consumed.QuantityDelta,
			SourceType: "message_rewrite_refund", SourceID: in.SourceID,
			IdempotencyKey: "refund:" + consumed.ID.String(), RelatedEntryID: &consumed.ID, Metadata: metadata,
		}, "refund")
		return err
	})
	return result, err
}

func (r *RewardRepository) GrantInCurrentTx(ctx context.Context, in RewardMutation) (*RewardMutationResult, error) {
	return r.AwardInCurrentTx(ctx, in)
}

func (r *RewardRepository) ConsumeInCurrentTx(ctx context.Context, in RewardMutation) (*RewardMutationResult, error) {
	if in.Quantity <= 0 {
		return nil, fmt.Errorf("消费数量必须大于 0")
	}
	return r.mutateCurrentTx(ctx, in, "consume")
}

func (r *RewardRepository) mutateCurrentTx(ctx context.Context, in RewardMutation, operation string) (*RewardMutationResult, error) {
	if in.Quantity <= 0 || in.Quantity > MaxRewardQuantity {
		return nil, ErrRewardQuantity
	}
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
	if operation == "grant" && !definition.Stackable && in.Quantity != 1 {
		return nil, ErrRewardQuantity
	}
	if err := r.lockRewardKey(ctx, "ledger:"+in.IdempotencyKey); err != nil {
		return nil, err
	}

	var existing RewardLedgerEntry
	err = r.db.WithContext(ctx).First(&existing, "idempotency_key = ?", in.IdempotencyKey).Error
	if err == nil {
		if existing.PlayerID != in.PlayerID || existing.RewardDefinitionID != definition.ID || existing.Operation != operation || (existing.QuantityDelta != in.Quantity && existing.QuantityDelta != -in.Quantity) || existing.SourceType != in.SourceType || existing.SourceID != in.SourceID {
			return nil, ErrRewardRequestConflict
		}
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
	if operation == "consume" || operation == "reverse" {
		delta = -in.Quantity
		if balance.Balance < in.Quantity {
			return nil, ErrInsufficientReward
		}
	} else if operation == "grant" && !definition.Stackable && balance.Balance > 0 {
		return &RewardMutationResult{Definition: *definition, Balance: balance.Balance, Applied: false}, nil
	}

	if delta > 0 && balance.Balance > MaxRewardQuantity-delta {
		return nil, ErrRewardQuantity
	}
	if balance.GrantedTotal > MaxRewardQuantity-in.Quantity && operation == "grant" || balance.ConsumedTotal > MaxRewardQuantity-in.Quantity && operation == "consume" || balance.RefundedTotal > MaxRewardQuantity-in.Quantity && operation == "refund" || balance.ReversedTotal > MaxRewardQuantity-in.Quantity && operation == "reverse" {
		return nil, ErrRewardQuantity
	}
	balance.Balance += delta
	if operation == "grant" {
		balance.GrantedTotal += in.Quantity
	} else if operation == "consume" {
		balance.ConsumedTotal += in.Quantity
	} else if operation == "refund" {
		balance.RefundedTotal += in.Quantity
	} else if operation == "reverse" {
		balance.ReversedTotal += in.Quantity
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
		RelatedEntryID: in.RelatedEntryID,
	}
	if _, ok := entry.Metadata["reward_snapshot"]; !ok {
		entry.Metadata["reward_snapshot"] = rewardSnapshot(*definition)
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

// LockBalances orders every multi-asset grant the same way as exchange payments.
func (r *RewardRepository) LockBalances(ctx context.Context, playerID uuid.UUID, ids []uuid.UUID) error {
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	for _, id := range ids {
		if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&PlayerRewardBalance{PlayerID: playerID, RewardDefinitionID: id, UpdatedAt: time.Now()}).Error; err != nil {
			return err
		}
	}
	if len(ids) == 0 {
		return nil
	}
	var rows []PlayerRewardBalance
	return r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("player_id = ? AND reward_definition_id IN ?", playerID, ids).Order("reward_definition_id ASC").Find(&rows).Error
}
