package challenge

import (
	"context"
	"errors"
	"github.com/ialtone/fatecraft/internal/state"
	"github.com/ialtone/fatecraft/internal/storage"
)

func (s *Service) Completion(ctx context.Context, conv storage.Conversation) (*storage.CompletionView, error) {
	return s.repo.Completion(ctx, conv)
}

// StageFacts returns the conversation's immutable stage facts and their
// actual points. It never re-verifies or grants; readers may use it while the
// conversation is still active.
func (s *Service) StageFacts(ctx context.Context, conv storage.Conversation) ([]storage.ChallengeStageReward, int64, error) {
	rows, err := s.repo.ConversationStageRewards(ctx, conv)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	for _, row := range rows {
		total += row.GrantedPoints
	}
	return rows, total, nil
}

// StageProgressFacts returns milestones reached on the current branch that
// have not been settled yet. It never writes progress or rewards.
func (s *Service) StageProgressFacts(ctx context.Context, conv storage.Conversation) ([]storage.StageProgressView, int64, error) {
	if conv.CampaignID != nil || conv.Status != storage.StatusActive {
		return []storage.StageProgressView{}, 0, nil
	}
	content, err := s.ModuleContentForVersion(ctx, conv.ChallengeVersionID, conv.ChallengeHash)
	if err != nil {
		return nil, 0, err
	}
	disabled := ""
	if conv.LLMRoute == storage.LLMRouteClient {
		disabled = storage.RewardsDisabledReasonClientRoute
	}
	stored, err := s.repo.StateRepo().Get(ctx, conv.ID)
	if errors.Is(err, storage.ErrNotFound) {
		return []storage.StageProgressView{}, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	views := stageProgressViews(content, state.ReadStageProgress(stored.State), disabled)
	var pending int64
	for _, view := range views {
		pending += view.ConfiguredPoints
	}
	return views, pending, nil
}

func (s *Service) EndingProposal(ctx context.Context, conv storage.Conversation) (*storage.EndingProposal, error) {
	return s.repo.EndingProposal(ctx, conv)
}
