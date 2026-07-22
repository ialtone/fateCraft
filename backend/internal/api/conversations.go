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
	c.JSON(http.StatusOK, gin.H{"conversation": conv})
}

// updateConvBody 是 PATCH 对话的入参；挑战内容在创建后不可替换。
type updateConvBody struct {
	Title *string `json:"title"`
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
	if body.Title == nil {
		conv, err := s.conv.Get(c.Request.Context(), id)
		if err != nil {
			fail(c, err)
			return
		}
		c.JSON(http.StatusOK, conv)
		return
	}
	conv, err := s.conv.UpdateTitle(c.Request.Context(), id, *body.Title)
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
