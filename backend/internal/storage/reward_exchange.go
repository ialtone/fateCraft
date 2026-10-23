package storage

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrExchangeChanged      = errors.New("兑换商品已变更，请刷新后重新确认")
	ErrExchangeUnavailable  = errors.New("兑换商品当前不可用")
	ErrExchangeLimitReached = errors.New("本次兑换份数超过该商品每人剩余额度")
	ErrExchangeStock        = errors.New("库存不足，请刷新后重试")
)

const (
	ExchangeKindCDK    = "cdk"
	ExchangeKindReward = "reward"
)

type RewardExchangeOffer struct {
	ID                    uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	Name                  string    `json:"name"`
	Description           string    `json:"description"`
	Kind                  string    `json:"kind"`
	Tutorial              string    `json:"tutorial"`
	CostQuantity          int64     `json:"cost_quantity"`
	RewardQuantity        int64     `json:"reward_quantity"`
	MaxRedemptionsPerUser int64     `json:"max_redemptions_per_user"`
	Enabled               bool      `json:"enabled"`
	Revision              int64     `json:"revision"`
	SortOrder             int       `json:"sort_order"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

type RewardExchangeRevision struct {
	OfferID   uuid.UUID  `gorm:"type:uuid;primaryKey" json:"offer_id"`
	Revision  int64      `gorm:"primaryKey" json:"revision"`
	Snapshot  JSONMap    `gorm:"type:jsonb" json:"snapshot"`
	ActorID   *uuid.UUID `gorm:"type:uuid" json:"actor_id"`
	CreatedAt time.Time  `json:"created_at"`
}

type RewardExchangeRequest struct {
	RequestID uuid.UUID `json:"request_id"`
	OfferID   uuid.UUID `json:"offer_id"`
	Revision  int64     `json:"revision"`
	Units     int64     `json:"units"`
}

type RewardExchangeOrder struct {
	ID             uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	RequestID      uuid.UUID  `gorm:"type:uuid" json:"request_id"`
	PlayerID       uuid.UUID  `gorm:"type:uuid" json:"user_id"`
	OfferID        uuid.UUID  `gorm:"type:uuid" json:"offer_id"`
	OfferRevision  int64      `json:"offer_revision"`
	Units          int64      `json:"units"`
	CostQuantity   int64      `json:"cost_quantity"`
	RewardQuantity int64      `json:"reward_quantity"`
	Snapshot       JSONMap    `gorm:"type:jsonb" json:"snapshot"`
	CostLedgerID   uuid.UUID  `gorm:"type:uuid" json:"cost_ledger_id"`
	RewardLedgerID *uuid.UUID `gorm:"type:uuid" json:"reward_ledger_id"`
	CreatedAt      time.Time  `json:"created_at"`
	TicketStatus   string     `gorm:"->;-:migration" json:"ticket_status"`
	Username       string     `gorm:"->;-:migration" json:"username,omitempty"`
}

func (r *RewardRepository) ListExchangeOffers(ctx context.Context, admin bool) ([]RewardExchangeOffer, error) {
	rows := []RewardExchangeOffer{}
	q := r.db.WithContext(ctx)
	if !admin {
		q = q.Where("enabled = true")
	}
	err := q.Order("sort_order ASC,id ASC").Find(&rows).Error
	return rows, err
}

// ExchangeUsage 返回玩家在每个商品上仍有效的已兑换份数；退款作废的份数不计入，额度随之退还。
func (r *RewardRepository) ExchangeUsage(ctx context.Context, player uuid.UUID) (map[uuid.UUID]int64, error) {
	rows := []struct {
		OfferID uuid.UUID
		Units   int64
	}{}
	err := r.db.WithContext(ctx).Raw(
		`SELECT offer_id, count(*) AS units FROM reward_exchange_stock WHERE player_id = ? AND status = 'redeemed' GROUP BY offer_id`,
		player,
	).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	usage := make(map[uuid.UUID]int64, len(rows))
	for _, row := range rows {
		usage[row.OfferID] = row.Units
	}
	return usage, nil
}

// exchangeRedeemed 在事务内统计玩家对某商品仍有效的已兑换份数。
func (r *RewardRepository) exchangeRedeemed(ctx context.Context, player, offer uuid.UUID) (int64, error) {
	var redeemed int64
	err := r.db.WithContext(ctx).Raw(
		`SELECT count(*) FROM reward_exchange_stock WHERE player_id = ? AND offer_id = ? AND status = 'redeemed'`, player, offer,
	).Scan(&redeemed).Error
	return redeemed, err
}

func (r *RewardRepository) SaveExchangeOffer(ctx context.Context, actor uuid.UUID, id uuid.UUID, wanted RewardExchangeOffer) (*RewardExchangeOffer, error) {
	if strings.TrimSpace(wanted.Name) == "" || len(wanted.Name) > 128 || len(wanted.Description) > 4000 || len(wanted.Tutorial) > 20000 || (wanted.Kind != "" && wanted.Kind != ExchangeKindCDK && wanted.Kind != ExchangeKindReward) || wanted.SortOrder < -2147483648 || wanted.SortOrder > 2147483647 {
		return nil, ErrExchangeUnavailable
	}
	if wanted.CostQuantity < 1 || wanted.CostQuantity > MaxRewardQuantity || wanted.RewardQuantity < 1 || wanted.RewardQuantity > MaxRewardQuantity {
		return nil, ErrRewardQuantity
	}
	if wanted.MaxRedemptionsPerUser < 0 || wanted.MaxRedemptionsPerUser > MaxRewardQuantity {
		return nil, ErrRewardQuantity
	}
	err := r.WithTx(ctx, func(tx *RewardRepository) error {
		if err := tx.LockConfiguration(ctx); err != nil {
			return err
		}
		if err := tx.requireRewardAdmin(ctx, actor); err != nil {
			return err
		}
		before := RewardExchangeOffer{}
		if id != uuid.Nil {
			if err := tx.db.Clauses(clause.Locking{Strength: "UPDATE"}).First(&before, "id = ?", id).Error; err != nil {
				return asNotFound(err)
			}
			if before.Revision != wanted.Revision {
				return ErrExchangeChanged
			}
			wanted.Kind = before.Kind // 商品类型决定库存形态，创建后不可修改
			wanted.ID = id
			wanted.CreatedAt = before.CreatedAt
			wanted.Revision = before.Revision + 1
		} else {
			wanted.ID = uuid.New()
			wanted.CreatedAt = time.Now()
			if wanted.Kind == "" {
				wanted.Kind = ExchangeKindReward
			}
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
		if err := tx.db.Create(&RewardExchangeRevision{OfferID: wanted.ID, Revision: wanted.Revision, Snapshot: snapshot, ActorID: &actor, CreatedAt: wanted.UpdatedAt}).Error; err != nil {
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
		if offer.CostQuantity < 1 || offer.RewardQuantity < 1 || in.Units > MaxRewardQuantity/offer.CostQuantity || in.Units > MaxRewardQuantity/offer.RewardQuantity {
			return ErrRewardQuantity
		}
		if offer.MaxRedemptionsPerUser < 0 || offer.MaxRedemptionsPerUser > MaxRewardQuantity {
			return ErrExchangeUnavailable
		}
		if offer.MaxRedemptionsPerUser > 0 {
			// 玩家行锁已覆盖同一账号的并发兑换，这里读到的是该账号最新已提交的成功订单。
			redeemed, err := tx.exchangeRedeemed(ctx, player, offer.ID)
			if err != nil {
				return err
			}
			if redeemed < 0 || redeemed > offer.MaxRedemptionsPerUser || in.Units > offer.MaxRedemptionsPerUser-redeemed {
				return ErrExchangeLimitReached
			}
		}
		if err := tx.LockBalances(ctx, player, []string{PointsKey, MessageRewriteCreditKey}); err != nil {
			return err
		}
		// 并发兑换跳过他人已锁定的份额；不足 n 份整单拒绝，不部分成交。
		var stock []RewardExchangeStock
		if err := tx.db.Raw(`SELECT id, code FROM reward_exchange_stock WHERE offer_id = ? AND status = 'available' ORDER BY created_at, id LIMIT ? FOR UPDATE SKIP LOCKED`, offer.ID, in.Units).Scan(&stock).Error; err != nil {
			return err
		}
		if int64(len(stock)) < in.Units {
			return ErrExchangeStock
		}
		ids, codes := make([]uuid.UUID, len(stock)), []string{}
		for i, row := range stock {
			ids[i] = row.ID
			if offer.Kind == ExchangeKindCDK {
				codes = append(codes, row.Code)
			}
		}
		order = RewardExchangeOrder{ID: uuid.New(), RequestID: in.RequestID, PlayerID: player, OfferID: offer.ID, OfferRevision: offer.Revision, Units: in.Units, CostQuantity: offer.CostQuantity * in.Units, RewardQuantity: offer.RewardQuantity * in.Units,
			Snapshot: JSONMap{"name": offer.Name, "description": offer.Description, "kind": offer.Kind, "tutorial": offer.Tutorial, "codes": codes, "cost_per_unit": offer.CostQuantity, "reward_per_unit": offer.RewardQuantity}, CreatedAt: time.Now()}
		paid, err := tx.ConsumeInCurrentTx(ctx, RewardMutation{PlayerID: player, RewardKey: PointsKey, Quantity: order.CostQuantity, SourceType: "reward_exchange", SourceID: order.ID.String(), IdempotencyKey: "exchange:cost:" + order.ID.String()})
		if err != nil {
			return err
		}
		order.CostLedgerID = paid.Ledger.ID
		if offer.Kind == ExchangeKindReward {
			awarded, err := tx.GrantInCurrentTx(ctx, RewardMutation{PlayerID: player, RewardKey: MessageRewriteCreditKey, Quantity: order.RewardQuantity, SourceType: "reward_exchange", SourceID: order.ID.String(), IdempotencyKey: "exchange:reward:" + order.ID.String(), Metadata: JSONMap{"offer_id": offer.ID, "offer_revision": offer.Revision}})
			if err != nil {
				return err
			}
			order.RewardLedgerID = &awarded.Ledger.ID
		}
		if err := tx.db.Create(&order).Error; err != nil {
			return err
		}
		return tx.db.Exec(`UPDATE reward_exchange_stock SET status = 'redeemed', order_id = ?, player_id = ?, updated_at = now() WHERE id IN ?`, order.ID, player, ids).Error
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
	err := q.Select("reward_exchange_orders.*, COALESCE((SELECT status FROM reward_exchange_tickets t WHERE t.order_id = reward_exchange_orders.id), '') AS ticket_status, " + usernameOf("reward_exchange_orders.player_id")).Order("created_at DESC,id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *RewardRepository) ExchangeRevisions(ctx context.Context, id uuid.UUID) ([]RewardExchangeRevision, error) {
	rows := []RewardExchangeRevision{}
	err := r.db.WithContext(ctx).Where("offer_id = ?", id).Order("revision DESC").Find(&rows).Error
	return rows, err
}
