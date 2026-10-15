package storage

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"sort"
	"strings"
	"time"
)

var (
	ErrInsufficientReward    = errors.New("余额不足")
	ErrRewardRequestConflict = errors.New("同一请求标识对应不同内容")
	ErrRewardState           = errors.New("当前状态不允许此操作")
	ErrRewardQuantity        = errors.New("数量无效或超出安全整数范围")
	ErrRewardReversal        = errors.New("只能撤回原管理员发放，兑换不支持单边返还")
	ErrRewardAdminRequired   = errors.New("需要管理员权限")
	ErrRewardKey             = errors.New("仅支持积分和剧情改写次数")
)

type PlayerRewardView struct {
	Asset   RewardAsset `json:"asset"`
	Balance int64       `json:"balance"`
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
	ActorID        *uuid.UUID
	RequestID      *uuid.UUID
	RequestHash    string
	Reason         string
	PublicNote     string
}
type RewardMutationResult struct {
	Asset   RewardAsset        `json:"asset"`
	Ledger  *RewardLedgerEntry `json:"ledger,omitempty"`
	Balance int64              `json:"balance"`
	Applied bool               `json:"applied"`
}
type RewardRefund struct {
	PlayerID              uuid.UUID
	RewardKey             string
	ConsumeIdempotencyKey string
	Metadata              JSONMap
}
type RewardRepository struct{ db *gorm.DB }

func NewRewardRepository(db *gorm.DB) *RewardRepository { return &RewardRepository{db: db} }
func (r *RewardRepository) WithTx(ctx context.Context, fn func(*RewardRepository) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return fn(NewRewardRepository(tx)) })
}
func (r *RewardRepository) lockRewardKey(ctx context.Context, key string) error {
	return r.db.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", "reward:"+key).Error
}
func (r *RewardRepository) ListPlayerRewards(ctx context.Context, player uuid.UUID) ([]PlayerRewardView, error) {
	var rows []PlayerRewardBalance
	if err := r.db.WithContext(ctx).Where("player_id = ?", player).Find(&rows).Error; err != nil {
		return nil, err
	}
	balances := map[string]int64{}
	for _, row := range rows {
		balances[row.RewardKey] = row.Balance
	}
	return []PlayerRewardView{{RewardAssetFor(PointsKey), balances[PointsKey]}, {RewardAssetFor(MessageRewriteCreditKey), balances[MessageRewriteCreditKey]}}, nil
}
func (r *RewardRepository) HasBalance(ctx context.Context, player uuid.UUID, key string, quantity int64) (bool, error) {
	if !ValidRewardKey(key) {
		return false, ErrRewardKey
	}
	if quantity < 0 || quantity > MaxRewardQuantity {
		return false, ErrRewardQuantity
	}
	if quantity == 0 {
		return true, nil
	}
	var count int64
	err := r.db.WithContext(ctx).Model(&PlayerRewardBalance{}).Where("player_id = ? AND reward_key = ? AND balance >= ?", player, key, quantity).Count(&count).Error
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
func (r *RewardRepository) GrantInCurrentTx(ctx context.Context, in RewardMutation) (*RewardMutationResult, error) {
	return r.mutateCurrentTx(ctx, in, "grant")
}
func (r *RewardRepository) ConsumeInCurrentTx(ctx context.Context, in RewardMutation) (*RewardMutationResult, error) {
	return r.mutateCurrentTx(ctx, in, "consume")
}

// 只返还实际发生的改写消费；关联原流水，不能从兑换订单单边退款。
func (r *RewardRepository) RefundConsumed(ctx context.Context, in RewardRefund) (*RewardMutationResult, error) {
	var result *RewardMutationResult
	err := r.WithTx(ctx, func(tx *RewardRepository) error {
		if in.PlayerID == uuid.Nil || !ValidRewardKey(in.RewardKey) || in.ConsumeIdempotencyKey == "" {
			return ErrRewardState
		}
		var original RewardLedgerEntry
		err := tx.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("player_id = ? AND operation = 'consume' AND idempotency_key = ?", in.PlayerID, in.ConsumeIdempotencyKey).First(&original).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if (original.SourceType != "message_rewrite" && original.SourceType != "message_regenerate") || original.RewardKey != in.RewardKey || in.RewardKey != MessageRewriteCreditKey {
			return ErrRewardReversal
		}
		result, err = tx.mutateCurrentTx(ctx, RewardMutation{PlayerID: in.PlayerID, RewardKey: in.RewardKey, Quantity: -original.QuantityDelta, SourceType: "message_rewrite_refund", SourceID: original.ID.String(), IdempotencyKey: "refund:" + original.ID.String(), RelatedEntryID: &original.ID, Metadata: in.Metadata}, "refund")
		return err
	})
	return result, err
}

func (r *RewardRepository) mutateCurrentTx(ctx context.Context, in RewardMutation, operation string) (*RewardMutationResult, error) {
	if !ValidRewardKey(in.RewardKey) {
		return nil, ErrRewardKey
	}
	if in.Quantity <= 0 || in.Quantity > MaxRewardQuantity {
		return nil, ErrRewardQuantity
	}
	if in.PlayerID == uuid.Nil || strings.TrimSpace(in.IdempotencyKey) == "" || strings.TrimSpace(in.SourceType) == "" || strings.TrimSpace(in.SourceID) == "" {
		return nil, ErrRewardState
	}
	delta := in.Quantity
	switch operation {
	case "consume", "reverse":
		delta = -delta
	case "grant", "refund":
	default:
		return nil, ErrRewardState
	}
	if (operation == "refund" || operation == "reverse") != (in.RelatedEntryID != nil) {
		return nil, ErrRewardState
	}
	source := fmt.Sprintf("source:%s:%s:%s:%s:%s", in.PlayerID, in.RewardKey, operation, in.SourceType, in.SourceID)
	if err := r.lockRewardKey(ctx, source); err != nil {
		return nil, err
	}
	if err := r.lockRewardKey(ctx, "ledger:"+in.IdempotencyKey); err != nil {
		return nil, err
	}
	var existing RewardLedgerEntry
	err := r.db.WithContext(ctx).Where("idempotency_key = ? OR (player_id = ? AND reward_key = ? AND operation = ? AND source_type = ? AND source_id = ?)", in.IdempotencyKey, in.PlayerID, in.RewardKey, operation, in.SourceType, in.SourceID).First(&existing).Error
	if err == nil {
		if existing.PlayerID != in.PlayerID || existing.RewardKey != in.RewardKey || existing.Operation != operation || existing.QuantityDelta != delta || existing.SourceType != in.SourceType || existing.SourceID != in.SourceID || existing.RequestHash != in.RequestHash || !sameUUID(existing.RelatedEntryID, in.RelatedEntryID) {
			return nil, ErrRewardRequestConflict
		}
		return &RewardMutationResult{Asset: RewardAssetFor(in.RewardKey), Ledger: &existing, Balance: existing.BalanceAfter}, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err := r.LockBalances(ctx, in.PlayerID, []string{in.RewardKey}); err != nil {
		return nil, err
	}
	var balance PlayerRewardBalance
	if err := r.db.Where("player_id = ? AND reward_key = ?", in.PlayerID, in.RewardKey).First(&balance).Error; err != nil {
		return nil, err
	}
	if delta < 0 && balance.Balance < in.Quantity {
		return nil, ErrInsufficientReward
	}
	if delta > 0 && balance.Balance > MaxRewardQuantity-delta {
		return nil, ErrRewardQuantity
	}
	var total *int64
	switch operation {
	case "grant":
		total = &balance.GrantedTotal
	case "consume":
		total = &balance.ConsumedTotal
	case "refund":
		total = &balance.RefundedTotal
	case "reverse":
		total = &balance.ReversedTotal
	}
	if *total > MaxRewardQuantity-in.Quantity {
		return nil, ErrRewardQuantity
	}
	*total += in.Quantity
	balance.Balance += delta
	balance.UpdatedAt = time.Now()
	if err := r.db.Save(&balance).Error; err != nil {
		return nil, err
	}
	if in.Metadata == nil {
		in.Metadata = JSONMap{}
	}
	if err := r.gameplayRewardSource(ctx, &in); err != nil {
		return nil, err
	}
	entry := RewardLedgerEntry{ID: uuid.New(), PlayerID: in.PlayerID, RewardKey: in.RewardKey, Operation: operation, QuantityDelta: delta, BalanceAfter: balance.Balance, SourceType: in.SourceType, SourceID: in.SourceID, ChallengeRunID: in.ChallengeRunID, IdempotencyKey: in.IdempotencyKey, RelatedEntryID: in.RelatedEntryID, Metadata: in.Metadata, ActorID: in.ActorID, RequestID: in.RequestID, RequestHash: in.RequestHash, Reason: in.Reason, PublicNote: in.PublicNote, CreatedAt: time.Now()}
	if err := r.db.Create(&entry).Error; err != nil {
		return nil, err
	}
	return &RewardMutationResult{Asset: RewardAssetFor(in.RewardKey), Ledger: &entry, Balance: balance.Balance, Applied: true}, nil
}
func (r *RewardRepository) LockBalances(ctx context.Context, player uuid.UUID, keys []string) error {
	keys = append([]string(nil), keys...)
	sort.Strings(keys)
	for _, key := range keys {
		if !ValidRewardKey(key) {
			return ErrRewardKey
		}
		if err := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&PlayerRewardBalance{PlayerID: player, RewardKey: key, UpdatedAt: time.Now()}).Error; err != nil {
			return err
		}
	}
	if len(keys) == 0 {
		return nil
	}
	var rows []PlayerRewardBalance
	return r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("player_id = ? AND reward_key IN ?", player, keys).Order("reward_key ASC").Find(&rows).Error
}

type RewardPageCursor struct {
	PackID      string
	VersionID   string
	Time        time.Time
	ID          uuid.UUID
	RewardKey   string
	SourceType  string
	CreatedFrom *time.Time
	CreatedTo   *time.Time
}
type AdminRewardLedgerView struct {
	RewardLedgerEntry
	UserID         uuid.UUID  `json:"user_id"`
	OperatorID     *uuid.UUID `json:"operator_id,omitempty"`
	AdminRequestID *uuid.UUID `json:"request_id,omitempty"`
	InternalReason string     `json:"reason"`
	SourceIdentity string     `json:"source_id"`
}

func (e RewardLedgerEntry) AdminView() AdminRewardLedgerView {
	e.GameplaySource = rewardGameplayProjection(e)
	return AdminRewardLedgerView{e, e.PlayerID, e.ActorID, e.RequestID, e.Reason, e.SourceID}
}
func (r *RewardRepository) ledgerQuery(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).Model(&RewardLedgerEntry{}).Select("reward_ledger.*, rev.id AS reversal_id").Joins("LEFT JOIN reward_ledger rev ON rev.related_entry_id = reward_ledger.id AND rev.operation = 'reverse'")
}
func (r *RewardRepository) ListLedger(ctx context.Context, player *uuid.UUID, limit int, cursor RewardPageCursor) ([]RewardLedgerEntry, error) {
	if limit < 1 || limit > 201 {
		limit = 51
	}
	q := r.ledgerQuery(ctx)
	if player != nil {
		q = q.Where("reward_ledger.player_id = ?", *player)
	}
	if cursor.RewardKey != "" {
		if !ValidRewardKey(cursor.RewardKey) {
			return nil, ErrRewardKey
		}
		q = q.Where("reward_ledger.reward_key = ?", cursor.RewardKey)
	}
	if cursor.SourceType != "" {
		q = q.Where("reward_ledger.source_type = ?", cursor.SourceType)
	}
	if cursor.PackID != "" {
		q = q.Where("reward_ledger.metadata->'gameplay_source'->>'pack_id' = ?", cursor.PackID)
	}
	if cursor.VersionID != "" {
		q = q.Where("reward_ledger.metadata->>'challenge_version_id' = ?", cursor.VersionID)
	}
	if cursor.CreatedFrom != nil {
		q = q.Where("reward_ledger.created_at >= ?", *cursor.CreatedFrom)
	}
	if cursor.CreatedTo != nil {
		q = q.Where("reward_ledger.created_at <= ?", *cursor.CreatedTo)
	}
	if !cursor.Time.IsZero() {
		q = q.Where("(reward_ledger.created_at,reward_ledger.id) < (?,?)", cursor.Time, cursor.ID)
	}
	rows := []RewardLedgerEntry{}
	err := q.Order("reward_ledger.created_at DESC,reward_ledger.id DESC").Limit(limit).Find(&rows).Error
	for i := range rows {
		rows[i].GameplaySource = rewardGameplayProjection(rows[i])
	}
	return rows, err
}
func (r *RewardRepository) LedgerEntry(ctx context.Context, id uuid.UUID) (*RewardLedgerEntry, error) {
	var row RewardLedgerEntry
	err := r.ledgerQuery(ctx).Where("reward_ledger.id = ?", id).First(&row).Error
	row.GameplaySource = rewardGameplayProjection(row)
	return &row, asNotFound(err)
}
