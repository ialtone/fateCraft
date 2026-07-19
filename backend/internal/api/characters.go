package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ialtone/fatecraft/internal/filestore"
	"github.com/ialtone/fatecraft/internal/storage"
)

func (s *Server) listCharacters(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"characters": s.char.List()})
}

func (s *Server) getCharacter(c *gin.Context) {
	id, ok := paramSlug(c, "id")
	if !ok {
		return
	}
	ch, err := s.char.Get(id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, ch)
}

// characterBody 是创建/更新角色卡的入参。
type characterBody struct {
	Name                 string   `json:"name"`
	Description          string   `json:"description"`
	Personality          string   `json:"personality"`
	Scenario             string   `json:"scenario"`
	FirstMessage         string   `json:"first_message"`
	ExampleDialogues     string   `json:"example_dialogues"`
	SystemPromptOverride string   `json:"system_prompt_override"`
	Tags                 []string `json:"tags"`
	AvatarURL            *string  `json:"avatar_url"`
}

func (s *Server) createCharacter(c *gin.Context) {
	var b characterBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	now := time.Now()
	ch := storage.Character{
		ID:                   filestore.GenerateSlug("char", b.Name, s.char.Exists),
		Name:                 b.Name,
		Description:          b.Description,
		Personality:          b.Personality,
		Scenario:             b.Scenario,
		FirstMessage:         b.FirstMessage,
		ExampleDialogues:     b.ExampleDialogues,
		SystemPromptOverride: b.SystemPromptOverride,
		Tags:                 b.Tags,
		AvatarURL:            b.AvatarURL,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	if ch.Tags == nil {
		ch.Tags = []string{}
	}
	if err := s.char.Save(ch); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, ch)
}

func (s *Server) updateCharacter(c *gin.Context) {
	id, ok := paramSlug(c, "id")
	if !ok {
		return
	}
	var b characterBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ch, err := s.char.Get(id)
	if err != nil {
		fail(c, err)
		return
	}
	// 与原数据库实现保持一致：整体覆盖可编辑字段。
	ch.Name = b.Name
	ch.Description = b.Description
	ch.Personality = b.Personality
	ch.Scenario = b.Scenario
	ch.FirstMessage = b.FirstMessage
	ch.ExampleDialogues = b.ExampleDialogues
	ch.SystemPromptOverride = b.SystemPromptOverride
	ch.Tags = b.Tags
	ch.AvatarURL = b.AvatarURL
	ch.UpdatedAt = time.Now()
	if ch.Tags == nil {
		ch.Tags = []string{}
	}
	if err := s.char.Save(ch); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, ch)
}

func (s *Server) deleteCharacter(c *gin.Context) {
	id, ok := paramSlug(c, "id")
	if !ok {
		return
	}
	if err := s.char.Delete(id); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func (s *Server) listPersonas(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"personas": s.persona.List()})
}

// personaBody 是创建 Persona 的入参。
type personaBody struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	SpeakingStyle string `json:"speaking_style"`
	Constraints   string `json:"constraints"`
}

func (s *Server) createPersona(c *gin.Context) {
	var b personaBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	now := time.Now()
	p := storage.Persona{
		ID:            filestore.GenerateSlug("persona", b.Name, s.persona.Exists),
		Name:          b.Name,
		Description:   b.Description,
		SpeakingStyle: b.SpeakingStyle,
		Constraints:   b.Constraints,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.persona.Save(p); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, p)
}

func (s *Server) listScenarios(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"scenarios": s.scenario.List()})
}
