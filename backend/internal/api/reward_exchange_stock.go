package api

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/storage"
)

func (s *Server) listExchangeStock(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	limit, ok := adminListLimit(c)
	if !ok {
		return
	}
	offset, ok := adminListOffset(c)
	if !ok {
		return
	}
	rows, err := s.rewards.ListExchangeStock(c.Request.Context(), id, c.Query("status"), c.Query("code"), limit+1, offset)
	if err != nil {
		rewardFail(c, err)
		return
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	c.JSON(200, gin.H{"stock": rows, "has_more": more})
}

// addExchangeStock 接收 CDK 文本（一行一个）或站内奖励份数。
func (s *Server) addExchangeStock(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var body struct {
		Codes    string `json:"codes"`
		Quantity int    `json:"quantity"`
	}
	if err := bindRewardJSON(c, &body); err != nil {
		c.JSON(400, gin.H{"error": "库存格式无效"})
		return
	}
	n, err := s.rewards.AddExchangeStock(c.Request.Context(), rewardActor(c), id, strings.Split(body.Codes, "\n"), body.Quantity)
	if err != nil {
		rewardFail(c, err)
		return
	}
	c.JSON(200, gin.H{"added": n})
}

func (s *Server) removeExchangeStock(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var body struct {
		IDs []uuid.UUID `json:"ids"`
	}
	if err := bindRewardJSON(c, &body); err != nil {
		c.JSON(400, gin.H{"error": "库存 ID 无效"})
		return
	}
	n, err := s.rewards.RemoveExchangeStock(c.Request.Context(), rewardActor(c), id, body.IDs)
	if err != nil {
		rewardFail(c, err)
		return
	}
	c.JSON(200, gin.H{"removed": n})
}

func (s *Server) listMyExchangeTickets(c *gin.Context) {
	player := rewardActor(c)
	s.exchangeTickets(c, &player)
}
func (s *Server) listAdminExchangeTickets(c *gin.Context) { s.exchangeTickets(c, nil) }
func (s *Server) exchangeTickets(c *gin.Context, player *uuid.UUID) {
	limit, ok := adminListLimit(c)
	if !ok {
		return
	}
	offset, ok := adminListOffset(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	rows, err := s.rewards.Tickets(ctx, player, c.Query("status"), limit+1, offset)
	if err != nil {
		rewardFail(c, err)
		return
	}
	enabled, err := s.rewards.TicketsEnabled(ctx)
	if err != nil {
		rewardFail(c, err)
		return
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	c.JSON(200, gin.H{"tickets": rows, "has_more": more, "tickets_enabled": enabled})
}

func (s *Server) openExchangeTicket(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var body struct {
		Body string `json:"body"`
	}
	if err := bindRewardJSON(c, &body); err != nil {
		c.JSON(400, gin.H{"error": "工单格式无效"})
		return
	}
	row, err := s.rewards.OpenTicket(c.Request.Context(), rewardActor(c), id, body.Body)
	if err != nil {
		rewardFail(c, err)
		return
	}
	c.JSON(200, row)
}

func (s *Server) resolveExchangeTicket(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var body struct {
		Approve bool   `json:"approve"`
		Reason  string `json:"reason"`
	}
	if err := bindRewardJSON(c, &body); err != nil {
		c.JSON(400, gin.H{"error": "处理请求格式无效"})
		return
	}
	row, err := s.rewards.ResolveTicket(c.Request.Context(), rewardActor(c), id, body.Approve, body.Reason)
	if err != nil {
		rewardFail(c, err)
		return
	}
	c.JSON(200, row)
}

func (s *Server) setExchangeTicketsEnabled(c *gin.Context) {
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := bindRewardJSON(c, &body); err != nil {
		c.JSON(400, gin.H{"error": "开关格式无效"})
		return
	}
	if err := s.rewards.SetTicketsEnabled(c.Request.Context(), rewardActor(c), body.Enabled); err != nil {
		rewardFail(c, err)
		return
	}
	c.JSON(200, gin.H{"tickets_enabled": body.Enabled})
}

func (s *Server) redeemPointsCDK(c *gin.Context) {
	var body struct {
		Code string `json:"code"`
	}
	if err := bindRewardJSON(c, &body); err != nil {
		c.JSON(400, gin.H{"error": "CDK 格式无效"})
		return
	}
	batch, err := s.rewards.RedeemPointsCDK(c.Request.Context(), rewardActor(c), body.Code)
	if err != nil {
		rewardFail(c, err)
		return
	}
	c.JSON(200, gin.H{"points": batch.Points})
}

func (s *Server) generatePointsCDKs(c *gin.Context) {
	var body struct {
		Name     string `json:"name"`
		Points   int64  `json:"points"`
		Quantity int    `json:"quantity"`
		Days     int    `json:"days"`
	}
	if err := bindRewardJSON(c, &body); err != nil {
		c.JSON(400, gin.H{"error": "CDK 批次格式无效"})
		return
	}
	if body.Days == 0 {
		body.Days = 7
	}
	batch, codes, err := s.rewards.GeneratePointsCDKs(c.Request.Context(), rewardActor(c), body.Name, body.Points, body.Quantity, body.Days)
	if err != nil {
		rewardFail(c, err)
		return
	}
	c.JSON(200, gin.H{"batch": batch, "codes": codes})
}

func (s *Server) listPointsCDKBatches(c *gin.Context) {
	limit, ok := adminListLimit(c)
	if !ok {
		return
	}
	offset, ok := adminListOffset(c)
	if !ok {
		return
	}
	rows, err := s.rewards.PointsCDKBatches(c.Request.Context(), limit+1, offset)
	if err != nil {
		rewardFail(c, err)
		return
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	c.JSON(200, gin.H{"batches": rows, "has_more": more, "expiry_days": storage.PointsCDKExpiryDays})
}

func (s *Server) listPointsCDKs(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	rows, err := s.rewards.PointsCDKs(c.Request.Context(), id)
	if err != nil {
		rewardFail(c, err)
		return
	}
	c.JSON(200, gin.H{"codes": rows})
}
