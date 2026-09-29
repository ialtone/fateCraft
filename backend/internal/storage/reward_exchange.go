package storage

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrExchangeChanged     = errors.New("兑换商品已变更，请刷新后重新确认")
	ErrExchangeUnavailable = errors.New("兑换商品当前不可用")
	ErrExchangeOwned       = errors.New("已经拥有该奖励，本次未扣除积分")
)

type RewardExchangeOffer struct {
	ID                 uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	Name               string    `json:"name"`
	Description        string    `json:"description"`
	CostDefinitionID   uuid.UUID `gorm:"type:uuid" json:"cost_definition_id"`
	CostQuantity       int64     `json:"cost_quantity"`
	RewardDefinitionID uuid.UUID `gorm:"type:uuid" json:"reward_definition_id"`
	RewardQuantity     int64     `json:"reward_quantity"`
	Enabled            bool      `json:"enabled"`
	Revision           int64     `json:"revision"`
	SortOrder          int       `json:"sort_order"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type RewardExchangeOfferView struct {
	RewardExchangeOffer
	CostAsset   JSONMap `json:"cost_asset"`
	RewardAsset JSONMap `json:"reward_asset"`
	Available   bool    `json:"available"`
}

type RewardExchangeRevision struct {
	OfferID   uuid.UUID `gorm:"type:uuid;primaryKey" json:"offer_id"`
	Revision  int64     `gorm:"primaryKey" json:"revision"`
	Snapshot  JSONMap   `gorm:"type:jsonb" json:"snapshot"`
	ActorID   uuid.UUID `gorm:"type:uuid" json:"actor_id"`
	CreatedAt time.Time `json:"created_at"`
}

type RewardExchangeRequest struct {
	RequestID uuid.UUID `json:"request_id"`
	OfferID   uuid.UUID `json:"offer_id"`
	Revision  int64     `json:"revision"`
	Units     int64     `json:"units"`
}

type RewardExchangeOrder struct {
	ID             uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	RequestID      uuid.UUID `gorm:"type:uuid" json:"request_id"`
	PlayerID       uuid.UUID `gorm:"type:uuid" json:"user_id"`
	OfferID        uuid.UUID `gorm:"type:uuid" json:"offer_id"`
	OfferRevision  int64     `json:"offer_revision"`
	Units          int64     `json:"units"`
	CostQuantity   int64     `json:"cost_quantity"`
	RewardQuantity int64     `json:"reward_quantity"`
	Snapshot       JSONMap   `gorm:"type:jsonb" json:"snapshot"`
	CostLedgerID   uuid.UUID `gorm:"type:uuid" json:"cost_ledger_id"`
	RewardRecordID uuid.UUID `gorm:"type:uuid" json:"reward_record_id"`
	CreatedAt      time.Time `json:"created_at"`
}

func (r *RewardRepository) ListExchangeOffers(ctx context.Context, admin bool) ([]RewardExchangeOfferView, error) {
	rows := []RewardExchangeOffer{}
	q := r.db.WithContext(ctx)
	if !admin {
		q = q.Where("enabled = true")
	}
	if err := q.Order("sort_order ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	defs, err := r.ListDefinitions(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[uuid.UUID]RewardDefinition{}
	for _, def := range defs {
		byID[def.ID] = def
	}
	views := make([]RewardExchangeOfferView, 0, len(rows))
	for _, row := range rows {
		cost, costOK := byID[row.CostDefinitionID]
		reward, rewardOK := byID[row.RewardDefinitionID]
		available := costOK && rewardOK && row.Enabled && !cost.Disabled && !reward.Disabled
		if admin || available {
			views = append(views, RewardExchangeOfferView{row, rewardSnapshot(cost), rewardSnapshot(reward), available})
		}
	}
	return views, nil
}

func (r *RewardRepository) SaveExchangeOffer(ctx context.Context, actor uuid.UUID, id uuid.UUID, wanted RewardExchangeOffer) (*RewardExchangeOffer, error) {
	if strings.TrimSpace(wanted.Name) == "" || len(wanted.Name) > 128 || len(wanted.Description) > 4000 || wanted.CostDefinitionID == wanted.RewardDefinitionID {
		return nil, ErrExchangeUnavailable
	}
	if wanted.CostQuantity < 1 || wanted.CostQuantity > MaxRewardQuantity || wanted.RewardQuantity < 1 || wanted.RewardQuantity > MaxRewardQuantity {
		return nil, ErrRewardQuantity
	}
	err := r.WithTx(ctx, func(tx *RewardRepository) error {
		if err := tx.requireRewardAdmin(ctx, actor); err != nil {
			return err
		}
		if err := tx.LockConfiguration(ctx); err != nil {
			return err
		}
		cost, err := tx.DefinitionByID(ctx, wanted.CostDefinitionID)
		if err != nil {
			return err
		}
		reward, err := tx.DefinitionByID(ctx, wanted.RewardDefinitionID)
		if err != nil {
			return err
		}
		if !cost.Stackable || !cost.Consumable || cost.Kind != "currency" && cost.Kind != "credit" {
			return ErrExchangeUnavailable
		}
		if !reward.Stackable && wanted.RewardQuantity != 1 {
			return ErrRewardQuantity
		}
		if wanted.Enabled && (cost.Disabled || reward.Disabled) {
			return ErrRewardDisabled
		}
		before := RewardExchangeOffer{}
		if id != uuid.Nil {
			if err := tx.db.Clauses(clause.Locking{Strength: "UPDATE"}).First(&before, "id = ?", id).Error; err != nil {
				return asNotFound(err)
			}
			if before.Revision != wanted.Revision {
				return ErrExchangeChanged
			}
			wanted.ID = id
			wanted.CreatedAt = before.CreatedAt
			wanted.Revision = before.Revision + 1
		} else {
			wanted.ID = uuid.New()
			wanted.CreatedAt = time.Now()
			wanted.Revision = 1
		}
		wanted.UpdatedAt = time.Now()
		if err := tx.db.Save(&wanted).Error; err != nil {
			return err
		}
		encoded, err := json.Marshal(wanted)
		if err != nil {
			return err
		}
		snapshot := JSONMap{}
		if err := json.Unmarshal(encoded, &snapshot); err != nil {
			return err
		}
		if err := tx.db.Create(&RewardExchangeRevision{OfferID: wanted.ID, Revision: wanted.Revision, Snapshot: snapshot, ActorID: actor, CreatedAt: wanted.UpdatedAt}).Error; err != nil {
			return err
		}
		return tx.rewardAudit(ctx, actor, nil, "reward_exchange_offer", uuid.NewString(), JSONMap{"before": before, "after": wanted})
	})
	return &wanted, err
}

// Exchange uses the same account ledger as gameplay grants and platform consumption.
func (r *RewardRepository) Exchange(ctx context.Context, player uuid.UUID, in RewardExchangeRequest) (*RewardExchangeOrder, error) {
	if in.RequestID == uuid.Nil || in.OfferID == uuid.Nil || in.Revision < 1 || in.Units < 1 || in.Units > MaxRewardQuantity {
		return nil, ErrRewardQuantity
	}
	var order RewardExchangeOrder
	err := r.WithTx(ctx, func(tx *RewardRepository) error {
		if err := tx.lockRewardKey(ctx, "exchange:"+in.RequestID.String()); err != nil {
			return err
		}
		err := tx.db.First(&order, "request_id = ?", in.RequestID).Error
		if err == nil {
			if order.PlayerID != player || order.OfferID != in.OfferID || order.OfferRevision != in.Revision || order.Units != in.Units {
				return ErrRewardRequestConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.db.WithContext(ctx).Exec("SELECT pg_advisory_xact_lock_shared(hashtextextended('reward:configuration', 0))").Error; err != nil {
			return err
		}
		var user User
		if err := tx.db.Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).First(&user, "id = ?", player).Error; err != nil {
			return asNotFound(err)
		}
		if user.Status != UserStatusActive {
			return ErrTargetUserDisabled
		}
		var offer RewardExchangeOffer
		if err := tx.db.First(&offer, "id = ?", in.OfferID).Error; err != nil {
			return asNotFound(err)
		}
		if !offer.Enabled {
			return ErrExchangeUnavailable
		}
		if offer.Revision != in.Revision {
			return ErrExchangeChanged
		}
		cost, err := tx.DefinitionByID(ctx, offer.CostDefinitionID)
		if err != nil {
			return err
		}
		reward, err := tx.DefinitionByID(ctx, offer.RewardDefinitionID)
		if err != nil {
			return err
		}
		if cost.Disabled || reward.Disabled {
			return ErrRewardDisabled
		}
		if !cost.Stackable || !cost.Consumable || cost.ID == reward.ID {
			return ErrExchangeUnavailable
		}
		if in.Units > MaxRewardQuantity/offer.CostQuantity || in.Units > MaxRewardQuantity/offer.RewardQuantity || !reward.Stackable && in.Units != 1 {
			return ErrRewardQuantity
		}
		ids := []uuid.UUID{cost.ID, reward.ID}
		sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
		for _, id := range ids {
			if err := tx.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&PlayerRewardBalance{PlayerID: player, RewardDefinitionID: id, UpdatedAt: time.Now()}).Error; err != nil {
				return err
			}
		}
		balances := []PlayerRewardBalance{}
		if err := tx.db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("player_id = ? AND reward_definition_id IN ?", player, ids).Order("reward_definition_id ASC").Find(&balances).Error; err != nil {
			return err
		}
		for _, balance := range balances {
			if balance.RewardDefinitionID == reward.ID && !reward.Stackable && balance.Balance > 0 {
				return ErrExchangeOwned
			}
		}
		order = RewardExchangeOrder{ID: uuid.New(), RequestID: in.RequestID, PlayerID: player, OfferID: offer.ID, OfferRevision: offer.Revision, Units: in.Units, CostQuantity: offer.CostQuantity * in.Units, RewardQuantity: offer.RewardQuantity * in.Units,
			Snapshot: JSONMap{"name": offer.Name, "description": offer.Description, "cost_asset": rewardSnapshot(*cost), "reward_asset": rewardSnapshot(*reward), "cost_per_unit": offer.CostQuantity, "reward_per_unit": offer.RewardQuantity}, CreatedAt: time.Now()}
		paid, err := tx.ConsumeInCurrentTx(ctx, RewardMutation{PlayerID: player, RewardKey: cost.Key, Quantity: order.CostQuantity, SourceType: "reward_exchange", SourceID: order.ID.String(), IdempotencyKey: "exchange:cost:" + order.ID.String(), Metadata: JSONMap{"reward_snapshot": rewardSnapshot(*cost)}})
		if err != nil {
			return err
		}
		awardKey := "exchange:reward:" + order.ID.String()
		awarded, err := tx.AwardInCurrentTx(ctx, RewardMutation{PlayerID: player, RewardKey: reward.Key, Quantity: order.RewardQuantity, SourceType: "reward_exchange", SourceID: order.ID.String(), IdempotencyKey: awardKey, Metadata: JSONMap{"offer_id": offer.ID, "offer_revision": offer.Revision}})
		if err != nil {
			return err
		}
		if awarded.Ledger == nil || paid.Ledger == nil {
			return ErrExchangeOwned
		}
		var record RewardRecord
		if err := tx.db.First(&record, "source_key = ?", awardKey).Error; err != nil {
			return err
		}
		order.CostLedgerID = paid.Ledger.ID
		order.RewardRecordID = record.ID
		return tx.db.Create(&order).Error
	})
	return &order, err
}

func (r *RewardRepository) ExchangeByRequest(ctx context.Context, player uuid.UUID, id uuid.UUID) (*RewardExchangeOrder, error) {
	var row RewardExchangeOrder
	err := r.db.WithContext(ctx).First(&row, "player_id = ? AND request_id = ?", player, id).Error
	return &row, asNotFound(err)
}

func (r *RewardRepository) ExchangeOrders(ctx context.Context, player *uuid.UUID, cursor RewardPageCursor, limit int) ([]RewardExchangeOrder, error) {
	if limit < 1 || limit > 201 {
		limit = 51
	}
	q := r.db.WithContext(ctx)
	if player != nil {
		q = q.Where("player_id = ?", *player)
	}
	if !cursor.Time.IsZero() {
		q = q.Where("(created_at,id)<(?,?)", cursor.Time, cursor.ID)
	}
	rows := []RewardExchangeOrder{}
	err := q.Order("created_at DESC,id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *RewardRepository) ExchangeRevisions(ctx context.Context, id uuid.UUID) ([]RewardExchangeRevision, error) {
	rows := []RewardExchangeRevision{}
	err := r.db.WithContext(ctx).Where("offer_id = ?", id).Order("revision DESC").Find(&rows).Error
	return rows, err
}
