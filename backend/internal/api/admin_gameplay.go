package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (s *Server) listAdminGameplayModules(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"modules": s.challenges.Registry().Modules()})
}
