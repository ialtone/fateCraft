package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/storage"
)

func (s *Server) listAdminGameplayModules(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"modules": s.challenges.Registry().Modules()})
}

func (s *Server) listAdminRewardDefinitions(c *gin.Context) {
	definitions, err := s.rewards.ListDefinitions(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"reward_definitions": definitions})
}

func (s *Server) createAdminRewardDefinition(c *gin.Context) {
	var definition storage.RewardDefinition
	if err := c.ShouldBindJSON(&definition); err != nil || !validRewardDefinition(&definition) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "奖励定义格式无效"})
		return
	}
	creator, err := uuid.Parse(s.identity.CurrentPlayerID(c.Request.Context()))
	if err != nil {
		fail(c, err)
		return
	}
	definition.ID = uuid.New()
	definition.Key = strings.TrimSpace(definition.Key)
	definition.Name = strings.TrimSpace(definition.Name)
	definition.Kind = strings.TrimSpace(definition.Kind)
	definition.CreatedBy = &creator
	if err := s.rewards.CreateDefinition(c.Request.Context(), &definition); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, definition)
}

func (s *Server) updateAdminRewardDefinition(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var definition storage.RewardDefinition
	if err := c.ShouldBindJSON(&definition); err != nil || !validRewardDefinition(&definition) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "奖励定义格式无效"})
		return
	}
	definition.Key = strings.TrimSpace(definition.Key)
	definition.Name = strings.TrimSpace(definition.Name)
	definition.Kind = strings.TrimSpace(definition.Kind)
	updated, err := s.rewards.UpdateDefinition(c.Request.Context(), id, &definition)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, updated)
}

func validRewardDefinition(definition *storage.RewardDefinition) bool {
	if definition == nil || strings.TrimSpace(definition.Key) == "" ||
		strings.TrimSpace(definition.Kind) == "" || strings.TrimSpace(definition.Name) == "" {
		return false
	}
	switch definition.Kind {
	case "currency", "item", "badge", "entitlement", "credit":
		return true
	default:
		return false
	}
}
