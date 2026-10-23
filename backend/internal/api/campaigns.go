package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/realtime"
	"github.com/ialtone/fatecraft/internal/storage"
)

func (s *Server) createCampaign(c *gin.Context) {
	var in campaign.CreateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	row, err := s.campaigns.Create(c.Request.Context(), in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, row)
}

func (s *Server) listCampaigns(c *gin.Context) {
	var statuses []string
	if raw := strings.TrimSpace(c.Query("status")); raw != "" {
		statuses = strings.Split(raw, ",")
	}
	rows, err := s.campaigns.ListOwned(c.Request.Context(), statuses)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"campaigns": rows})
}

func (s *Server) getCampaign(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	row, err := s.campaigns.GetOwned(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"campaign": row})
}

func (s *Server) getCampaignSnapshot(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	snapshot, err := s.campaigns.Snapshot(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"snapshot": snapshot})
}

func (s *Server) listCampaignEvents(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	after, err := optionalInt64(c.Query("after_sequence"), 0)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "after_sequence 必须是整数"})
		return
	}
	limit64, err := optionalInt64(c.Query("limit"), 50)
	if err != nil || limit64 < 1 || limit64 > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "limit 必须是 1 到 100 的整数"})
		return
	}
	rows, err := s.campaigns.ListEvents(c.Request.Context(), id, after, int(limit64))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"events": rows})
}

func (s *Server) listCampaignConversations(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	rows, err := s.campaigns.ListConversations(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"conversations": rows})
}

func (s *Server) createCampaignConversation(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	row, err := s.campaigns.CreateConversation(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, row)
}

func (s *Server) executeCampaignAction(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var in campaign.ActionInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !s.allowPlayerAction(c) {
		return
	}
	result, err := s.campaigns.ExecutePlayerAction(c.Request.Context(), id, in)
	if err != nil {
		fail(c, err)
		return
	}
	s.broadcastCampaignCompletion(result)
	c.JSON(http.StatusOK, result)
}

func (s *Server) abandonCampaign(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var in campaign.AbandonInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	result, err := s.campaigns.Abandon(c.Request.Context(), id, in)
	if err != nil {
		fail(c, err)
		return
	}
	s.broadcastCampaignCompletion(result)
	c.JSON(http.StatusOK, result)
}

func optionalInt64(raw string, fallback int64) (int64, error) {
	if strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	return strconv.ParseInt(raw, 10, 64)
}

func (s *Server) broadcastCampaignCompletion(result *storage.CampaignActionResult) {
	if s.hub == nil || result == nil {
		return
	}
	if receipt := storage.CompletionFromResult(result.Result); receipt != nil {
		s.hub.Broadcast(receipt.ConversationID, realtime.Event{Type: realtime.TypeModuleActionCommitted, ConversationID: receipt.ConversationID.String()})
		s.hub.Broadcast(receipt.ConversationID, realtime.Event{Type: realtime.TypeChallengeCompleted, ConversationID: receipt.ConversationID.String(), Completion: receipt})
	}
}
