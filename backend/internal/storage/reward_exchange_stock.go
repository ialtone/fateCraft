package storage

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrTicketsDisabled = errors.New("当前未开放发起工单")
	ErrTicketState     = errors.New("工单状态已变化，请刷新后重试")
	ErrTicketKind      = errors.New("仅 CDK 商品订单可发起工单")
)

// ExchangeDuplicateError 列出导入时重复的 CDK，整批拒绝保存。
type ExchangeDuplicateError struct{ Codes []string }

func (e *ExchangeDuplicateError) Error() string { return "存在重复的 CDK，请删除后再保存" }

const maxStockImport = 5000

type RewardExchangeStock struct {
	ID        uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	OfferID   uuid.UUID  `gorm:"type:uuid" json:"offer_id"`
	Code      string     `json:"code"`
	Status    string     `json:"status"`
	OrderID   *uuid.UUID `gorm:"type:uuid" json:"order_id"`
	PlayerID  *uuid.UUID `gorm:"type:uuid" json:"user_id"`
	Username  string     `gorm:"->;-:migration" json:"username,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func (RewardExchangeStock) TableName() string { return "reward_exchange_stock" }

// ExchangeStockCount 总量 = 可用 + 已兑换；剩余 = 可用。
type ExchangeStockCount struct {
	Total     int64 `json:"stock_total"`
	Remaining int64 `json:"stock_remaining"`
}

func (r *RewardRepository) ExchangeStockCounts(ctx context.Context) (map[uuid.UUID]ExchangeStockCount, error) {
	rows := []struct {
		OfferID          uuid.UUID
		Total, Remaining int64
	}{}
	err := r.db.WithContext(ctx).Raw(`SELECT offer_id, count(*) FILTER (WHERE status IN ('available','redeemed')) AS total,
		count(*) FILTER (WHERE status = 'available') AS remaining FROM reward_exchange_stock GROUP BY offer_id`).Scan(&rows).Error
	counts := make(map[uuid.UUID]ExchangeStockCount, len(rows))
	for _, row := range rows {
		counts[row.OfferID] = ExchangeStockCount{Total: row.Total, Remaining: row.Remaining}
	}
	return counts, err
}

// AddExchangeStock 为 CDK 商品导入一行一个的 CDK，或为站内奖励商品新增 quantity 份。
// CDK 在本次粘贴内或与该商品已有 CDK（任意状态）重复时整批拒绝并返回重复项。
func (r *RewardRepository) AddExchangeStock(ctx context.Context, actor, offerID uuid.UUID, codes []string, quantity int) (int, error) {
	added := 0
	err := r.WithTx(ctx, func(tx *RewardRepository) error {
		if err := tx.requireRewardAdmin(ctx, actor); err != nil {
			return err
		}
		var offer RewardExchangeOffer
		if err := tx.db.Clauses(clause.Locking{Strength: "UPDATE"}).First(&offer, "id = ?", offerID).Error; err != nil {
			return asNotFound(err)
		}
		rows := []RewardExchangeStock{}
		now := time.Now()
		if offer.Kind == ExchangeKindCDK {
			seen, dup := map[string]bool{}, []string{}
			for _, code := range codes {
				if code = strings.TrimSpace(code); code == "" {
					continue
				}
				if len(code) > 512 {
					return ErrRewardQuantity
				}
				if seen[code] {
					dup = append(dup, code)
					continue
				}
				seen[code] = true
				rows = append(rows, RewardExchangeStock{ID: uuid.New(), OfferID: offerID, Code: code, Status: "available", CreatedAt: now, UpdatedAt: now})
			}
			existing := []string{}
			if len(rows) > 0 {
				keys := make([]string, 0, len(seen))
				for code := range seen {
					keys = append(keys, code)
				}
				if err := tx.db.Model(&RewardExchangeStock{}).Where("offer_id = ? AND code IN ?", offerID, keys).Pluck("code", &existing).Error; err != nil {
					return err
				}
			}
			if dup = append(dup, existing...); len(dup) > 0 {
				return &ExchangeDuplicateError{Codes: dup}
			}
		} else {
			for i := 0; i < quantity; i++ {
				rows = append(rows, RewardExchangeStock{ID: uuid.New(), OfferID: offerID, Status: "available", CreatedAt: now, UpdatedAt: now})
			}
		}
		if len(rows) == 0 || len(rows) > maxStockImport {
			return ErrRewardQuantity
		}
		if err := tx.db.CreateInBatches(rows, 500).Error; err != nil {
			return err
		}
		added = len(rows)
		return tx.rewardAudit(ctx, actor, nil, "reward_exchange_stock_add", uuid.NewString(), JSONMap{"offer_id": offerID, "count": added})
	})
	return added, err
}

func (r *RewardRepository) ListExchangeStock(ctx context.Context, offerID uuid.UUID, status, code string, limit, offset int) ([]RewardExchangeStock, error) {
	q := r.db.WithContext(ctx).Select("reward_exchange_stock.*, "+usernameOf("reward_exchange_stock.player_id")).Where("offer_id = ?", offerID)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if code = strings.TrimSpace(code); code != "" {
		q = q.Where("strpos(code, ?) > 0", code)
	}
	rows := []RewardExchangeStock{}
	err := q.Order("created_at DESC, id DESC").Limit(limit).Offset(offset).Find(&rows).Error
	return rows, err
}

// RemoveExchangeStock 只移除仍可用的份额；行锁与兑换事务互斥，已被兑换的记录不受影响。
func (r *RewardRepository) RemoveExchangeStock(ctx context.Context, actor, offerID uuid.UUID, ids []uuid.UUID) (int64, error) {
	var removed int64
	err := r.WithTx(ctx, func(tx *RewardRepository) error {
		if err := tx.requireRewardAdmin(ctx, actor); err != nil {
			return err
		}
		if len(ids) == 0 || len(ids) > maxStockImport {
			return ErrRewardQuantity
		}
		res := tx.db.Exec(`UPDATE reward_exchange_stock SET status = 'removed', updated_at = now() WHERE offer_id = ? AND id IN ? AND status = 'available'`, offerID, ids)
		if res.Error != nil {
			return res.Error
		}
		removed = res.RowsAffected
		return tx.rewardAudit(ctx, actor, nil, "reward_exchange_stock_remove", uuid.NewString(), JSONMap{"offer_id": offerID, "count": removed})
	})
	return removed, err
}

type RewardExchangeTicket struct {
	ID             uuid.UUID                   `gorm:"type:uuid;primaryKey" json:"id"`
	OrderID        uuid.UUID                   `gorm:"type:uuid" json:"order_id"`
	PlayerID       uuid.UUID                   `gorm:"type:uuid" json:"user_id"`
	Username       string                      `gorm:"->;-:migration" json:"username,omitempty"`
	Status         string                      `json:"status"`
	RefundLedgerID *uuid.UUID                  `gorm:"type:uuid" json:"refund_ledger_id"`
	CreatedAt      time.Time                   `json:"created_at"`
	UpdatedAt      time.Time                   `json:"updated_at"`
	Entries        []RewardExchangeTicketEntry `gorm:"foreignKey:TicketID" json:"entries"`
	Order          *RewardExchangeOrder        `gorm:"foreignKey:OrderID" json:"order,omitempty"`
}

type RewardExchangeTicketEntry struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	TicketID  uuid.UUID `gorm:"type:uuid" json:"ticket_id"`
	Kind      string    `json:"kind"`
	ActorID   uuid.UUID `gorm:"type:uuid" json:"actor_id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

func (r *RewardRepository) TicketsEnabled(ctx context.Context) (bool, error) {
	enabled := true
	err := r.db.WithContext(ctx).Raw("SELECT exchange_tickets_enabled FROM system_settings WHERE id = 1").Scan(&enabled).Error
	return enabled, err
}

func (r *RewardRepository) SetTicketsEnabled(ctx context.Context, actor uuid.UUID, enabled bool) error {
	return r.WithTx(ctx, func(tx *RewardRepository) error {
		if err := tx.requireRewardAdmin(ctx, actor); err != nil {
			return err
		}
		if err := tx.db.Exec("UPDATE system_settings SET exchange_tickets_enabled = ? WHERE id = 1", enabled).Error; err != nil {
			return err
		}
		return tx.rewardAudit(ctx, actor, nil, "reward_exchange_tickets_toggle", uuid.NewString(), JSONMap{"enabled": enabled})
	})
}

func (r *RewardRepository) lockTicketOrder(ctx context.Context, orderID uuid.UUID) (*RewardExchangeOrder, *RewardExchangeTicket, error) {
	var order RewardExchangeOrder
	if err := r.db.WithContext(ctx).First(&order, "id = ?", orderID).Error; err != nil {
		return nil, nil, asNotFound(err)
	}
	if err := r.lockRewardKey(ctx, "ticket:"+orderID.String()); err != nil {
		return nil, nil, err
	}
	var ticket RewardExchangeTicket
	if err := r.db.WithContext(ctx).Limit(1).Find(&ticket, "order_id = ?", orderID).Error; err != nil {
		return nil, nil, err
	}
	return &order, &ticket, nil
}

// OpenTicket 为 CDK 订单发起工单；被拒后再次发起追加到同一工单并重新置为处理中。
func (r *RewardRepository) OpenTicket(ctx context.Context, player, orderID uuid.UUID, body string) (*RewardExchangeTicket, error) {
	if strings.TrimSpace(body) == "" || len(body) > 20000 {
		return nil, ErrRewardQuantity
	}
	var out *RewardExchangeTicket
	err := r.WithTx(ctx, func(tx *RewardRepository) error {
		if enabled, err := tx.TicketsEnabled(ctx); err != nil || !enabled {
			if err == nil {
				err = ErrTicketsDisabled
			}
			return err
		}
		order, ticket, err := tx.lockTicketOrder(ctx, orderID)
		if err != nil {
			return err
		}
		if order.PlayerID != player {
			return ErrNotFound
		}
		if order.Snapshot["kind"] != ExchangeKindCDK {
			return ErrTicketKind
		}
		now := time.Now()
		switch ticket.Status {
		case "":
			*ticket = RewardExchangeTicket{ID: uuid.New(), OrderID: orderID, PlayerID: player, Status: "open", CreatedAt: now, UpdatedAt: now}
			if err := tx.db.Omit("Entries", "Order").Create(ticket).Error; err != nil {
				return err
			}
		case "rejected":
			if err := tx.db.Exec("UPDATE reward_exchange_tickets SET status = 'open', updated_at = ? WHERE id = ?", now, ticket.ID).Error; err != nil {
				return err
			}
		default:
			return ErrTicketState
		}
		if err := tx.db.Create(&RewardExchangeTicketEntry{ID: uuid.New(), TicketID: ticket.ID, Kind: "request", ActorID: player, Body: body, CreatedAt: now}).Error; err != nil {
			return err
		}
		out, err = tx.Ticket(ctx, ticket.ID)
		return err
	})
	return out, err
}

// ResolveTicket 处理处理中的工单。同意时同一事务内全额退回积分、CDK 作废（不回到可用）、
// 额度随作废退还，工单置为已退款并结束；拒绝时必须填写理由。
func (r *RewardRepository) ResolveTicket(ctx context.Context, actor, ticketID uuid.UUID, approve bool, reason string) (*RewardExchangeTicket, error) {
	if !approve && (strings.TrimSpace(reason) == "" || len(reason) > 20000) {
		return nil, ErrRewardQuantity
	}
	var out *RewardExchangeTicket
	err := r.WithTx(ctx, func(tx *RewardRepository) error {
		if err := tx.requireRewardAdmin(ctx, actor); err != nil {
			return err
		}
		var found RewardExchangeTicket
		if err := tx.db.First(&found, "id = ?", ticketID).Error; err != nil {
			return asNotFound(err)
		}
		order, ticket, err := tx.lockTicketOrder(ctx, found.OrderID)
		if err != nil {
			return err
		}
		if ticket.Status != "open" {
			return ErrTicketState
		}
		now, kind := time.Now(), "reject"
		if approve {
			kind = "refund"
			refund, err := tx.mutateCurrentTx(ctx, RewardMutation{PlayerID: order.PlayerID, RewardKey: PointsKey, Quantity: order.CostQuantity, SourceType: "reward_exchange_refund", SourceID: order.ID.String(), IdempotencyKey: "exchange:refund:" + order.ID.String(), RelatedEntryID: &order.CostLedgerID, Metadata: JSONMap{"ticket_id": ticket.ID}}, "refund")
			if err != nil {
				return err
			}
			if err := tx.db.Exec(`UPDATE reward_exchange_stock SET status = 'voided', updated_at = ? WHERE order_id = ? AND status = 'redeemed'`, now, order.ID).Error; err != nil {
				return err
			}
			err = tx.db.Exec("UPDATE reward_exchange_tickets SET status = 'refunded', refund_ledger_id = ?, updated_at = ? WHERE id = ?", refund.Ledger.ID, now, ticket.ID).Error
			if err != nil {
				return err
			}
		} else if err := tx.db.Exec("UPDATE reward_exchange_tickets SET status = 'rejected', updated_at = ? WHERE id = ?", now, ticket.ID).Error; err != nil {
			return err
		}
		if err := tx.db.Create(&RewardExchangeTicketEntry{ID: uuid.New(), TicketID: ticket.ID, Kind: kind, ActorID: actor, Body: reason, CreatedAt: now}).Error; err != nil {
			return err
		}
		if err := tx.rewardAudit(ctx, actor, &order.PlayerID, "reward_exchange_ticket_"+kind, uuid.NewString(), JSONMap{"ticket_id": ticket.ID, "order_id": order.ID}); err != nil {
			return err
		}
		out, err = tx.Ticket(ctx, ticket.ID)
		return err
	})
	return out, err
}

func (r *RewardRepository) ticketQuery(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).Select("reward_exchange_tickets.*, "+usernameOf("reward_exchange_tickets.player_id")).Preload("Entries", func(db *gorm.DB) *gorm.DB { return db.Order("created_at, id") }).Preload("Order")
}

// usernameOf 为管理端列表附带用户名，与后台用户列表的 @username 一致。
func usernameOf(column string) string {
	return "COALESCE((SELECT username FROM users WHERE users.id = " + column + "), '') AS username"
}

func (r *RewardRepository) Ticket(ctx context.Context, id uuid.UUID) (*RewardExchangeTicket, error) {
	var row RewardExchangeTicket
	err := r.ticketQuery(ctx).First(&row, "id = ?", id).Error
	return &row, asNotFound(err)
}

// Tickets 列出工单：player 非空时只列该玩家的，status 为空时不过滤。
func (r *RewardRepository) Tickets(ctx context.Context, player *uuid.UUID, status string, limit, offset int) ([]RewardExchangeTicket, error) {
	q := r.ticketQuery(ctx)
	if player != nil {
		q = q.Where("player_id = ?", *player)
	}
	if status != "" {
		q = q.Where("status = ?", status)
	}
	rows := []RewardExchangeTicket{}
	err := q.Order("updated_at DESC, id DESC").Limit(limit).Offset(offset).Find(&rows).Error
	return rows, err
}
