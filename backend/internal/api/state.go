package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/ialtone/fatecraft/internal/storage"
)

func (s *Server) getState(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	st, err := s.state.Get(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"state": st})
}

// patchStateBody 支持两种方式：patch（点号路径补丁）或 state（整体替换）。
type patchStateBody struct {
	Patch map[string]any `json:"patch"`
	State map[string]any `json:"state"`
}

func (s *Server) patchState(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var body patchStateBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if body.State != nil {
		after, err := s.state.Replace(c.Request.Context(), id, body.State, storage.SourceUser)
		if err != nil {
			fail(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"state": after})
		return
	}
	after, skipped, err := s.state.Apply(c.Request.Context(), id, body.Patch, storage.SourceUser, nil)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"state": after, "skipped": skipped})
}

func (s *Server) stateRevisions(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	limit := 50
	if l := c.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil {
			limit = n
		}
	}
	revs, err := s.state.Revisions(c.Request.Context(), id, limit)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"revisions": revs})
}
