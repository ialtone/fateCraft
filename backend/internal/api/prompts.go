package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/ialtone/fatecraft/internal/prompt"
)

func (s *Server) listPrompts(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"prompts": s.prompt.List()})
}

func (s *Server) getPrompt(c *gin.Context) {
	id, ok := paramSlug(c, "id")
	if !ok {
		return
	}
	p, err := s.prompt.Get(id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, p)
}

func (s *Server) createPrompt(c *gin.Context) {
	var in prompt.CreateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	p, err := s.prompt.Create(in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, p)
}

// updatePromptBody 更新预设名称/描述。
type updatePromptBody struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

func (s *Server) updatePrompt(c *gin.Context) {
	id, ok := paramSlug(c, "id")
	if !ok {
		return
	}
	var b updatePromptBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	p, err := s.prompt.Update(id, b.Name, b.Description)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, p)
}

func (s *Server) deletePrompt(c *gin.Context) {
	id, ok := paramSlug(c, "id")
	if !ok {
		return
	}
	if err := s.prompt.Delete(id); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func (s *Server) addSegment(c *gin.Context) {
	id, ok := paramSlug(c, "id")
	if !ok {
		return
	}
	var in prompt.SegmentInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	seg, err := s.prompt.AddSegment(id, in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, seg)
}

func (s *Server) updateSegment(c *gin.Context) {
	id, ok := paramSlug(c, "id")
	if !ok {
		return
	}
	var in prompt.SegmentInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	seg, err := s.prompt.UpdateSegment(id, in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, seg)
}

func (s *Server) deleteSegment(c *gin.Context) {
	id, ok := paramSlug(c, "id")
	if !ok {
		return
	}
	if err := s.prompt.DeleteSegment(id); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}
