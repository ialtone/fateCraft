package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/storage"
)

type conversationActivityResponse = storage.ActivitySnapshot

func (s *Server) ownedActivity(c *gin.Context, id uuid.UUID) (*storage.ActivitySnapshot, bool) {
	owner := s.identity.CurrentPlayerID(c.Request.Context())
	if owner == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return nil, false
	}
	snapshot, err := s.activity.Snapshot(c.Request.Context(), id, owner)
	if err != nil {
		fail(c, err)
		return nil, false
	}
	return snapshot, true
}

func (s *Server) getConversationActivity(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	snapshot, ok := s.ownedActivity(c, id)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, snapshot)
}
