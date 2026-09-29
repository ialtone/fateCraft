package api

import (
	"net/http"

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

func (s *Server) listRewardLedger(c *gin.Context) { s.rewardLedgerPage(c, false) }
