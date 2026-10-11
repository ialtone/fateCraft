package campaign

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/storage"
)

func (s *Service) ExecutePlayerModuleInTx(ctx context.Context, repo *storage.ChallengeRepository, conv *storage.Conversation, runtime *gameplay.RuntimeContract, revision int64, key string, arguments map[string]any) (*storage.CampaignActionResult, error) {
	if conv.CampaignID == nil {
		return nil, ErrCampaignModeRequired
	}
	// The outer player transaction owns request-id replay and branch checks.
	// This second key identifies the corresponding Campaign state transition.
	input := ActionInput{ActionID: fmt.Sprintf("module/%s/%d/%d", conv.ID, conv.BranchEpoch, revision), ExpectedRevision: revision, Key: key, Arguments: arguments}
	return s.executeAction(ctx, repo.CampaignActions(), *conv.CampaignID, input, "player", nil, &TrustedActionContext{Contract: runtime})
}

func (s *Service) PlayerModuleProjection(ctx context.Context, id uuid.UUID) (map[string]any, error) {
	row, err := s.GetOwned(ctx, id)
	if err != nil {
		return nil, err
	}
	runtime, err := s.challenges.RuntimeForVersion(ctx, row.ChallengeVersionID, row.ChallengeHash)
	if err != nil {
		return nil, err
	}
	if runtime.Campaign == nil {
		return nil, ErrCampaignModeRequired
	}
	domain, err := s.repo.DomainState(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.playerProjection(ctx, *row, domain, runtime)
}
