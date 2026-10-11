package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/storage"
)

func (s *Server) getState(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	if _, err := s.conv.GetOwned(c.Request.Context(), id); err != nil {
		fail(c, err)
		return
	}
	st, err := s.state.GetPlayer(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"state": st})
}

func (s *Server) getConversationRuntime(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	runtime, err := s.challenges.PublicRuntimeForConversation(
		c.Request.Context(), id, s.identity.CurrentPlayerID(c.Request.Context()),
	)
	if err != nil {
		fail(c, err)
		return
	}
	// Guidance visibility is projected server-side from the same public state
	// snapshot returned by the state endpoint.  The projection only affects UI;
	// message/action handlers still revalidate all permissions and revisions.
	if runtime.Mode == gameplay.RuntimeModeChallenge && runtime.Presentation.Guidance != nil {
		var projected *gameplay.Guidance
		if s.state != nil {
			if playerState, stateErr := s.state.GetPlayer(c.Request.Context(), id); stateErr == nil {
				value := gameplay.ProjectGuidance(*runtime.Presentation.Guidance, playerState, nil)
				projected = &value
			}
		}
		if projected == nil {
			// A failed projection must not fall back to authored items: doing so
			// could reveal guidance that is conditional on hidden state.  The
			// primary runtime response remains useful without the optional UI.
			empty := gameplay.Guidance{}
			projected = &empty
		}
		runtime.Presentation.Guidance = projected
	}
	c.JSON(http.StatusOK, gin.H{"runtime": runtime})
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
	if _, err := s.conv.EnsureActive(c.Request.Context(), id); err != nil {
		fail(c, err)
		return
	}
	var body patchStateBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if body.State != nil {
		after, err := s.state.Replace(c.Request.Context(), id, body.State, storage.SourceSystem)
		if err != nil {
			fail(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"state": after})
		return
	}
	after, skipped, err := s.state.Apply(c.Request.Context(), id, body.Patch, storage.SourceSystem, nil)
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
		n, err := strconv.Atoi(l)
		if err != nil || n < 1 || n > 200 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "limit 必须是 1 到 200 的整数"})
			return
		}
		limit = n
	}
	if _, err := s.conv.GetOwned(c.Request.Context(), id); err != nil {
		fail(c, err)
		return
	}
	revs, err := s.state.PlayerRevisions(c.Request.Context(), id, limit)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"revisions": revs})
}
