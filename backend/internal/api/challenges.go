package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/challenge"
)

func (s *Server) listChallenges(c *gin.Context) {
	items, err := s.challenges.ListPublished(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"challenges": items})
}

func (s *Server) getChallenge(c *gin.Context) {
	item, err := s.challenges.GetPublished(c.Request.Context(), c.Param("slug"))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, item)
}

func (s *Server) listAdminChallenges(c *gin.Context) {
	items, err := s.challenges.ListAdmin(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"challenges": items})
}

func (s *Server) createAdminChallenge(c *gin.Context) {
	var in challenge.CreatePackInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式无效"})
		return
	}
	creator, err := uuid.Parse(s.identity.CurrentPlayerID(c.Request.Context()))
	if err != nil {
		fail(c, err)
		return
	}
	pack, err := s.challenges.CreatePack(c.Request.Context(), creator, in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, pack)
}

func (s *Server) getAdminChallengeVersion(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	doc, err := s.challenges.VersionDocument(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, doc)
}

func (s *Server) createAdminChallengeVersion(c *gin.Context) {
	packID, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var body struct {
		Version string `json:"version"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式无效"})
		return
	}
	creator, err := uuid.Parse(s.identity.CurrentPlayerID(c.Request.Context()))
	if err != nil {
		fail(c, err)
		return
	}
	doc, err := s.challenges.CreateDraft(c.Request.Context(), packID, creator, body.Version)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, doc)
}

func (s *Server) saveAdminChallengeVersion(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var in challenge.SaveVersionInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式无效"})
		return
	}
	doc, err := s.challenges.SaveDraft(c.Request.Context(), id, in)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, doc)
}

func (s *Server) publishAdminChallengeVersion(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	publisher, err := uuid.Parse(s.identity.CurrentPlayerID(c.Request.Context()))
	if err != nil {
		fail(c, err)
		return
	}
	doc, err := s.challenges.Publish(c.Request.Context(), id, publisher)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, doc)
}

func challengeFailureStatus(err error) bool {
	return errors.Is(err, challenge.ErrInvalidChallenge) || errors.Is(err, challenge.ErrInvalidSelection) ||
		errors.Is(err, challenge.ErrSettlementDenied)
}
