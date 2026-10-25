package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/challenge"
	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/realtime"
	"github.com/ialtone/fatecraft/internal/storage"
)

func (s *Server) getModuleActions(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	conv, err := s.conv.GetOwned(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	if conv.Status != storage.StatusActive || conv.IsProcessing {
		c.JSON(http.StatusOK, gin.H{"branch_epoch": conv.BranchEpoch, "action_capabilities": []any{}})
		return
	}
	projection, err := s.actionSnapshot(c.Request.Context(), conv)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, projection)
}

func (s *Server) executeModuleAction(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var input challenge.ModulePlayerActionInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	playerID, err := uuid.Parse(s.identity.CurrentPlayerID(c.Request.Context()))
	if err != nil {
		fail(c, storage.ErrNotFound)
		return
	}
	if !s.allowPlayerAction(c) {
		return
	}
	result, err := s.challenges.ExecuteModulePlayerAction(c.Request.Context(), id, playerID, input)
	if err != nil {
		var violation *gameplay.RuleViolation
		if errors.As(err, &violation) {
			c.JSON(http.StatusConflict, gin.H{"error": violation.Message, "code": violation.Code})
			return
		}
		if errors.Is(err, storage.ErrModuleActionConflict) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "state.conflict"})
			return
		}
		fail(c, err)
		return
	}
	if s.hub != nil {
		var typed challenge.ModuleActionResult
		encoded, encodeErr := json.Marshal(result)
		if encodeErr == nil && json.Unmarshal(encoded, &typed) == nil {
			s.hub.Broadcast(id, realtime.Event{Type: realtime.TypeModuleActionCommitted, ConversationID: id.String(), StageProgress: typed.StageProgress, PendingStagePoints: typed.PendingStagePoints})
			if typed.Completion != nil {
				s.hub.Broadcast(id, realtime.Event{Type: realtime.TypeChallengeCompleted, ConversationID: id.String(), Completion: typed.Completion})
			} else if gen := s.hub.Gen(); gen != nil {
				// 动作已结算且未触发结局：追加一个 module_action 叙事回合，
				// 把 ActionResult 讲成剧情；对话忙时静默跳过，结果由下一次
				// 叙事回合按未消费 ActionResult 注入补齐。
				narrationRequest := moduleActionNarrationRequestID(id, input)
				s.hub.RunDetached(c.Request.Context(), id, func(ctx context.Context, emit realtime.EmitFunc) {
					gen.GenerateForModuleAction(ctx, id, narrationRequest, emit)
				})
			}
		} else {
			s.hub.Broadcast(id, realtime.Event{Type: realtime.TypeModuleActionCommitted, ConversationID: id.String()})
		}
	}
	c.JSON(http.StatusOK, result)
}

// The same action retry must replay its narration run, not buy another LLM
// response. Include conversation and branch because action IDs are scoped there.
func moduleActionNarrationRequestID(conversationID uuid.UUID, input challenge.ModulePlayerActionInput) uuid.UUID {
	key := "module_action:narration:" + strconv.FormatInt(input.BranchEpoch, 10) + ":" + input.RequestID.String()
	return uuid.NewSHA1(conversationID, []byte(key))
}
