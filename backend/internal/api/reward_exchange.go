package api

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/storage"
)

func (s *Server) listExchangeOffers(c *gin.Context)      { s.exchangeOffers(c, false) }
func (s *Server) listAdminExchangeOffers(c *gin.Context) { s.exchangeOffers(c, true) }

// exchangeOfferView 在玩家商品列表上附加当前账号的额度；管理端列表不带任何玩家统计。
type exchangeOfferView struct {
	storage.RewardExchangeOffer
	RedeemedUnits  int64  `json:"redeemed_units"`
	RemainingUnits *int64 `json:"remaining_units"`
}

func (s *Server) exchangeOffers(c *gin.Context, admin bool) {
	ctx := c.Request.Context()
	rows, err := s.rewards.ListExchangeOffers(ctx, admin)
	if err != nil {
		rewardFail(c, err)
		return
	}
	if admin {
		c.JSON(200, gin.H{"offers": rows})
		return
	}
	usage, err := s.rewards.ExchangeUsage(ctx, rewardActor(c))
	if err != nil {
		rewardFail(c, err)
		return
	}
	views := make([]exchangeOfferView, 0, len(rows))
	for _, offer := range rows {
		view := exchangeOfferView{RewardExchangeOffer: offer, RedeemedUnits: usage[offer.ID]}
		if offer.MaxRedemptionsPerUser > 0 {
			remaining := offer.MaxRedemptionsPerUser - view.RedeemedUnits
			if remaining < 0 {
				remaining = 0
			}
			view.RemainingUnits = &remaining
		}
		views = append(views, view)
	}
	c.JSON(200, gin.H{"offers": views})
}

func (s *Server) saveExchangeOffer(c *gin.Context) {
	id := uuid.Nil
	if c.Param("id") != "" {
		var ok bool
		id, ok = paramUUID(c, "id")
		if !ok {
			return
		}
	}
	var body struct {
		Name                  string `json:"name"`
		Description           string `json:"description"`
		CostQuantity          int64  `json:"cost_quantity"`
		RewardQuantity        int64  `json:"reward_quantity"`
		MaxRedemptionsPerUser int64  `json:"max_redemptions_per_user"`
		Enabled               bool   `json:"enabled"`
		SortOrder             int    `json:"sort_order"`
		Revision              int64  `json:"revision"`
	}
	if err := bindRewardJSON(c, &body); err != nil {
		c.JSON(400, gin.H{"error": "兑换商品格式无效"})
		return
	}
	row, err := s.rewards.SaveExchangeOffer(c.Request.Context(), rewardActor(c), id, storage.RewardExchangeOffer{
		Name: body.Name, Description: body.Description, CostQuantity: body.CostQuantity,
		RewardQuantity: body.RewardQuantity, MaxRedemptionsPerUser: body.MaxRedemptionsPerUser,
		Enabled: body.Enabled, SortOrder: body.SortOrder, Revision: body.Revision,
	})
	if err != nil {
		rewardFail(c, err)
		return
	}
	c.JSON(200, row)
}

func (s *Server) exchangeReward(c *gin.Context) {
	var body storage.RewardExchangeRequest
	if err := bindRewardJSON(c, &body); err != nil {
		c.JSON(400, gin.H{"error": "兑换请求格式无效"})
		return
	}
	row, err := s.rewards.Exchange(c.Request.Context(), rewardActor(c), body)
	if err != nil {
		rewardFail(c, err)
		return
	}
	c.JSON(200, row)
}

func (s *Server) getExchangeRequest(c *gin.Context) {
	id, ok := paramUUID(c, "request_id")
	if !ok {
		return
	}
	row, err := s.rewards.ExchangeByRequest(c.Request.Context(), rewardActor(c), id)
	if err != nil {
		rewardFail(c, err)
		return
	}
	c.JSON(200, row)
}

func (s *Server) listExchangeOrders(c *gin.Context)      { s.exchangeOrders(c, false) }
func (s *Server) listAdminExchangeOrders(c *gin.Context) { s.exchangeOrders(c, true) }
func (s *Server) exchangeOrders(c *gin.Context, admin bool) {
	limit, ok := adminListLimit(c)
	if !ok {
		return
	}
	cursor, ok := rewardCursor(c)
	if !ok {
		return
	}
	var player *uuid.UUID
	if !admin {
		id := rewardActor(c)
		player = &id
	} else if raw := c.Query("user_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			c.JSON(400, gin.H{"error": "用户 ID 无效"})
			return
		}
		player = &id
	}
	rows, err := s.rewards.ExchangeOrders(c.Request.Context(), player, cursor, limit+1)
	if err != nil {
		rewardFail(c, err)
		return
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		next = encodeRewardCursor(last.CreatedAt, last.ID)
	}
	c.JSON(200, gin.H{"orders": rows, "next_cursor": next})
}

func (s *Server) listExchangeRevisions(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	rows, err := s.rewards.ExchangeRevisions(c.Request.Context(), id)
	if err != nil {
		rewardFail(c, err)
		return
	}
	c.JSON(200, gin.H{"revisions": rows})
}

func (s *Server) rewardRuleCatalog(c *gin.Context) {
	limit, ok := adminListLimit(c)
	if !ok {
		return
	}
	offset, ok := adminListOffset(c)
	if !ok {
		return
	}
	rows, err := s.rewards.RuleCatalog(c.Request.Context(), limit+1, offset)
	if err != nil {
		rewardFail(c, err)
		return
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	c.JSON(200, gin.H{"rules": rows, "has_more": more})
}

func (s *Server) rewardAuditPage(c *gin.Context) {
	limit, ok := adminListLimit(c)
	if !ok {
		return
	}
	cursor, ok := rewardCursor(c)
	if !ok {
		return
	}
	rows, err := s.rewards.AuditPage(c.Request.Context(), cursor, limit+1)
	if err != nil {
		rewardFail(c, err)
		return
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		next = encodeRewardCursor(last.CreatedAt, last.ID)
	}
	c.JSON(200, gin.H{"audits": rows, "next_cursor": next})
}
