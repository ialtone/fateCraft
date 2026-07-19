package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ialtone/fatecraft/internal/conversation"
)

func (s *Server) createConversation(c *gin.Context) {
	var in conversation.CreateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	conv, err := s.conv.Create(c.Request.Context(), in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, conv)
}

func (s *Server) listConversations(c *gin.Context) {
	var statuses []string
	if q := c.Query("status"); q != "" {
		statuses = strings.Split(q, ",")
	}
	convs, err := s.conv.List(c.Request.Context(), statuses, c.Query("search"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"conversations": convs})
}

func (s *Server) getConversation(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	conv, err := s.conv.Get(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	books, _ := s.conv.WorldBookIDs(c.Request.Context(), id)
	c.JSON(http.StatusOK, gin.H{"conversation": conv, "world_book_ids": books})
}

// updateConvBody 是 PATCH 对话的入参（模板 id 为文件 slug）。
type updateConvBody struct {
	Title          *string   `json:"title"`
	CharacterID    *string   `json:"character_id"`
	PersonaID      *string   `json:"persona_id"`
	PromptPresetID *string   `json:"prompt_preset_id"`
	ScenarioID     *string   `json:"scenario_id"`
	WorldBookIDs   *[]string `json:"world_book_ids"`
}

func (s *Server) updateConversation(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var body updateConvBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	fields := map[string]any{}
	if body.Title != nil {
		fields["title"] = *body.Title
	}
	if body.CharacterID != nil {
		fields["character_id"] = *body.CharacterID
	}
	if body.PersonaID != nil {
		fields["persona_id"] = *body.PersonaID
	}
	if body.PromptPresetID != nil {
		fields["prompt_preset_id"] = *body.PromptPresetID
	}
	if body.ScenarioID != nil {
		fields["scenario_id"] = *body.ScenarioID
	}
	if body.WorldBookIDs != nil {
		if err := s.conv.SetWorldBooks(c.Request.Context(), id, *body.WorldBookIDs); err != nil {
			fail(c, err)
			return
		}
	}
	if len(fields) == 0 {
		conv, err := s.conv.Get(c.Request.Context(), id)
		if err != nil {
			fail(c, err)
			return
		}
		c.JSON(http.StatusOK, conv)
		return
	}
	conv, err := s.conv.Update(c.Request.Context(), id, fields)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, conv)
}

func (s *Server) deleteConversation(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	if err := s.conv.Delete(c.Request.Context(), id); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func (s *Server) endConversation(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	conv, err := s.conv.End(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	// 通知该对话的所有连接：对话已结束。
	s.hub.Broadcast(id, endedEvent(id))
	c.JSON(http.StatusOK, conv)
}

func (s *Server) archiveConversation(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	conv, err := s.conv.Archive(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, conv)
}

func (s *Server) restoreConversation(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	conv, err := s.conv.Restore(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, conv)
}
