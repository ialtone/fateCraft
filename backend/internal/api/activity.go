package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ialtone/fatecraft/internal/storage"
)

type conversationActivityResponse struct {
	Narrative              string `json:"narrative"`
	Memory                 string `json:"memory"`
	TurnStatus             string `json:"turn_status,omitempty"`
	CanContinue            bool   `json:"can_continue"`
	CanRegenerate          bool   `json:"can_regenerate"`
	RegenerateCostQuantity int64  `json:"regenerate_cost_quantity"`
	StepNo                 int    `json:"step_no,omitempty"`
	ToolCategory           string `json:"tool_category,omitempty"`
}

func (s *Server) getConversationActivity(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	conversation, err := s.conv.GetOwned(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	result := conversationActivityResponse{Narrative: "idle", Memory: "idle"}
	if s.turns != nil {
		capabilities, capabilitiesErr := s.turns.CurrentCapabilities(c.Request.Context(), id)
		if capabilitiesErr != nil {
			fail(c, capabilitiesErr)
			return
		}
		if capabilities != nil {
			result.TurnStatus = capabilities.Status
			result.CanContinue = capabilities.CanContinue
			result.CanRegenerate = capabilities.CanRegenerate
			result.RegenerateCostQuantity = capabilities.RegenerateCostQuantity
		}
	}
	// client 路由不扣费：展示的改写额度恒为 0。
	if conversation.LLMRoute == storage.LLMRouteClient {
		result.RegenerateCostQuantity = 0
	}
	if conversation.IsProcessing {
		result.Narrative = "preparing"
		if s.agents != nil {
			activity, activityErr := s.agents.CurrentActivity(c.Request.Context(), id)
			if activityErr != nil {
				fail(c, activityErr)
				return
			}
			if activity != nil {
				result.Narrative = activity.Phase
				result.StepNo = activity.StepNo
				result.ToolCategory = publicAgentToolCategory(activity.ToolName)
			}
		}
	}
	if s.memory != nil {
		jobs, jobsErr := s.memory.Jobs(c.Request.Context(), id, 1)
		if jobsErr != nil {
			fail(c, jobsErr)
			return
		}
		if len(jobs) > 0 {
			result.Memory = publicMemoryActivity(jobs[0].Status)
		}
	}
	c.JSON(http.StatusOK, result)
}

func publicAgentToolCategory(name string) string {
	switch {
	case strings.HasPrefix(name, "fc_state_"):
		return "state"
	case name == "fc_request_roll":
		return "roll"
	case name == "fc_complete_challenge":
		return "settlement"
	case strings.HasPrefix(name, "fc_action_"):
		return "action"
	default:
		return ""
	}
}

func publicMemoryActivity(status string) string {
	switch status {
	case storage.MemoryJobPending:
		return "queued"
	case storage.MemoryJobRunning:
		return "running"
	default:
		return "idle"
	}
}
