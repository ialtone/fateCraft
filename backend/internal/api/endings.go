package api

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/storage"
)

func (s *Server) actionSnapshot(ctx context.Context, conv *storage.Conversation) (map[string]any, error) {
	content, err := s.challenges.ModuleContentForVersion(ctx, conv.ChallengeVersionID, conv.ChallengeHash)
	if err != nil {
		return nil, err
	}
	descriptor, err := s.challenges.Registry().Module(content.Runtime.Engine)
	if err != nil {
		return nil, err
	}
	projection := map[string]any{"revision": int64(0), "action_capabilities": []any{}, "ending_capabilities": []any{}}
	if conv.Status != storage.StatusActive {
		projection["branch_epoch"] = conv.BranchEpoch
		projection["abandon_action"] = descriptor.AbandonAction
		projection["has_trusted_actions"] = descriptor.Runtime != nil || conv.CampaignID != nil
		return projection, nil
	}
	if content.Runtime.Mode == gameplay.RuntimeModeCampaign && conv.CampaignID != nil {
		projection, err = s.campaigns.PlayerModuleProjection(ctx, *conv.CampaignID)
	} else if descriptor.Runtime != nil {
		var trusted map[string]any
		trusted, err = s.state.Get(ctx, conv.ID)
		if err == nil {
			projection, err = s.challenges.ModuleProjection(content, trusted, "player")
		}
	}
	if err != nil {
		return nil, err
	}
	if conv.CampaignID == nil {
		progress, pending, progressErr := s.challenges.StageProgressFacts(ctx, *conv)
		if progressErr != nil {
			return nil, progressErr
		}
		projection["stage_progress"] = progress
		projection["pending_stage_points"] = pending
	}
	projection["branch_epoch"] = conv.BranchEpoch
	projection["abandon_action"] = descriptor.AbandonAction
	projection["has_trusted_actions"] = descriptor.Runtime != nil || conv.CampaignID != nil
	return projection, nil
}

func (s *Server) getEndingState(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	conv, err := s.conv.GetOwned(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	projection, err := s.actionSnapshot(c.Request.Context(), conv)
	if err != nil {
		fail(c, err)
		return
	}
	completion, err := s.challenges.Completion(c.Request.Context(), *conv)
	if err != nil {
		fail(c, err)
		return
	}
	proposal, err := s.challenges.EndingProposal(c.Request.Context(), *conv)
	if err != nil {
		fail(c, err)
		return
	}
	if proposal != nil && projection["revision"] != proposal.Revision {
		proposal = nil
	}
	stageRewards, stagePoints, err := s.challenges.StageFacts(c.Request.Context(), *conv)
	if err != nil {
		fail(c, err)
		return
	}
	stageProgress, pendingStagePoints := projection["stage_progress"], projection["pending_stage_points"]
	if stageProgress == nil {
		stageProgress, pendingStagePoints = []storage.StageProgressView{}, int64(0)
	}
	c.JSON(http.StatusOK, gin.H{"revision": projection["revision"], "branch_epoch": conv.BranchEpoch, "abandon_action": projection["abandon_action"], "has_trusted_actions": projection["has_trusted_actions"], "ending_capabilities": projection["ending_capabilities"], "completion": completion, "proposal": proposal, "is_processing": conv.IsProcessing, "stage_progress": stageProgress, "pending_stage_points": pendingStagePoints, "stage_rewards": stageRewards, "stage_reward_points": stagePoints})
}
