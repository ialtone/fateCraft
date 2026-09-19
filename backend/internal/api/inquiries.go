package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type inquiryQuestionBody struct {
	Question string `json:"question" binding:"required"`
}

func (s *Server) listInquiries(c *gin.Context) {
	conversationID, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	result, err := s.inquiries.History(c.Request.Context(), conversationID)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) askInquiry(c *gin.Context) {
	conversationID, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var body inquiryQuestionBody
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.Question) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "问题不能为空"})
		return
	}
	result, err := s.inquiries.Ask(c.Request.Context(), conversationID, body.Question)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) clearInquiries(c *gin.Context) {
	conversationID, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	if err := s.inquiries.Clear(c.Request.Context(), conversationID); err != nil {
		fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) cancelInquiry(c *gin.Context) {
	conversationID, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	if err := s.inquiries.Cancel(c.Request.Context(), conversationID); err != nil {
		fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
