package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

var allowedRollDifficulties = map[string]bool{
	"easy": true, "normal": true, "hard": true,
}

func (s *Server) getAdminRollSettings(c *gin.Context) {
	settings, err := s.system.Get(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, settings)
}

type adminRollSettingsBody struct {
	Enabled        *bool   `json:"roll_system_enabled"`
	DifficultyMode *string `json:"roll_difficulty_mode"`
}

func (s *Server) updateAdminRollSettings(c *gin.Context) {
	var body adminRollSettingsBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式无效"})
		return
	}
	settings, err := s.system.Get(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	if body.Enabled != nil {
		settings.RollSystemEnabled = *body.Enabled
	}
	if body.DifficultyMode != nil {
		if !allowedRollDifficulties[*body.DifficultyMode] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "判定难度必须为 easy、normal 或 hard"})
			return
		}
		settings.RollDifficultyMode = *body.DifficultyMode
	}
	if err := s.system.Save(c.Request.Context(), settings); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, settings)
}

func (s *Server) listAdminBuiltins(c *gin.Context) {
	items, err := s.builtins.ListBuiltins(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"builtins": items})
}

func (s *Server) importAdminBuiltin(c *gin.Context) {
	slug, ok := paramSlug(c, "slug")
	if !ok {
		return
	}
	result, err := s.builtins.ImportBuiltin(c.Request.Context(), slug)
	if err != nil {
		fail(c, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	c.JSON(status, result)
}
