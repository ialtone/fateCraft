package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (s *Server) listRewards(c *gin.Context) {
	playerID, err := uuid.Parse(s.identity.CurrentPlayerID(c.Request.Context()))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "当前用户身份无效"})
		return
	}
	items, err := s.rewards.ListPlayerRewards(c.Request.Context(), playerID)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"rewards": items})
}

func (s *Server) listRewardLedger(c *gin.Context) {
	playerID, err := uuid.Parse(s.identity.CurrentPlayerID(c.Request.Context()))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "当前用户身份无效"})
		return
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if err != nil || limit < 1 || limit > 200 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "limit 必须是 1 到 200 的整数"})
		return
	}
	entries, err := s.rewards.ListLedger(c.Request.Context(), playerID, limit)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"entries": entries})
}
