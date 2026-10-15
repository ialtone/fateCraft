package api

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/challenge"
	"net/http"
)

func (s *Server) savedGameplayVersion(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	conv, err := s.conv.GetOwned(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	info, err := s.challenges.SavedVersionInfo(c.Request.Context(), conv)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, info)
}

func (s *Server) gameplayVersionReferences(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	refs, err := s.challenges.Repository().VersionReferences(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, refs)
}
func (s *Server) gameplayRunPolicy(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var body struct {
		Policy           string `json:"policy"`
		Reason           string `json:"reason"`
		ExpectedRevision int64  `json:"expected_revision"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.ExpectedRevision < 1 || len(body.Reason) > 2000 || (body.Policy != "allowed" && body.Policy != "blocked") || (body.Policy == "blocked" && body.Reason == "") {
		c.JSON(400, gin.H{"error": "请提供运行策略、暂停原因和当前策略修订"})
		return
	}
	if err := s.challenges.Repository().SetRunPolicy(c.Request.Context(), id, rewardActor(c), body.Policy, body.Reason, body.ExpectedRevision); err != nil {
		fail(c, err)
		return
	}
	doc, err := s.challenges.VersionDocument(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, doc)
}
func (s *Server) retireGameplayVersion(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var body struct {
		ExpectedRevision int64 `json:"expected_revision"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.ExpectedRevision < 1 {
		c.JSON(400, gin.H{"error": "请提供策略修订"})
		return
	}
	if err := s.challenges.Repository().Retire(c.Request.Context(), id, rewardActor(c), body.ExpectedRevision); err != nil {
		fail(c, err)
		return
	}
	c.Status(204)
}
func (s *Server) selectGameplayVersion(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var body struct {
		VersionID        uuid.UUID `json:"version_id"`
		ExpectedRevision int64     `json:"expected_revision"`
		ConfirmRollback  bool      `json:"confirm_rollback"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.VersionID == uuid.Nil || body.ExpectedRevision < 1 {
		c.JSON(400, gin.H{"error": "请提供目标版本和当前管理修订"})
		return
	}
	doc, err := s.challenges.VersionDocument(c.Request.Context(), body.VersionID)
	if err != nil {
		fail(c, err)
		return
	}
	if doc.Version.ChallengePackID != id {
		c.JSON(404, gin.H{"error": "版本不属于该玩法包"})
		return
	}
	if err := s.challenges.ValidateDocument(c.Request.Context(), doc); err != nil {
		fail(c, err)
		return
	}
	pack, err := s.challenges.Repository().GetPack(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	if pack.CurrentVersionID != nil {
		current, err := s.challenges.Repository().GetVersion(c.Request.Context(), *pack.CurrentVersionID)
		if err != nil {
			fail(c, err)
			return
		}
		comparison, err := challenge.CompareStableVersions(doc.Version.Version, current.Version)
		if err != nil {
			fail(c, err)
			return
		}
		if comparison < 0 && !body.ConfirmRollback {
			c.JSON(http.StatusConflict, gin.H{"error": "目标是旧版，请明确确认回退；已有游戏和积分不会回滚", "code": "gameplay_rollback_confirmation"})
			return
		}
	}
	if err := s.challenges.SelectCurrent(c.Request.Context(), id, body.VersionID, rewardActor(c), body.ExpectedRevision); err != nil {
		fail(c, err)
		return
	}
	pack, err = s.challenges.Repository().GetPack(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(200, pack)
}
