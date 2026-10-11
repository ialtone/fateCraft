package campaign

import (
	"context"
	"fmt"

	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/storage"
)

func (s *Service) narrativeActionView(ctx context.Context, contract *gameplay.RuntimeContract, row storage.Campaign, domain *storage.CampaignDomainState) (map[string]any, []string, bool, error) {
	provider, ok := s.projections[contract.Campaign.ProjectionHandler]
	if !ok {
		return nil, nil, false, fmt.Errorf("Campaign 投影处理器未安装: %s", contract.Campaign.ProjectionHandler)
	}
	input := ProjectionContext{Campaign: row, Domain: domain, ContextPolicy: contract.Campaign.ContextPolicy}
	model, err := provider.Context(ctx, input)
	if err != nil {
		return nil, nil, false, err
	}
	player, err := provider.Snapshot(ctx, input)
	if err != nil {
		return nil, nil, false, err
	}
	view, partial, err := gameplay.SharedNarrativeView(player, model.Data)
	if err != nil {
		return nil, nil, false, err
	}
	view["campaign"] = map[string]any{"status": row.Status, "game_time": row.GameTime, "location": row.CurrentLocationKey, "outcome_key": row.OutcomeKey}
	paths := append([]string{}, model.FactPaths...)
	paths = append(paths, "campaign.status", "campaign.game_time", "campaign.location", "campaign.outcome_key")
	return view, paths, partial, nil
}

// This header is the same post-action header CommitAction will persist. The
// domain read uses the transaction's already-updated domain tables.
func narrativeCampaignAfter(row storage.Campaign, outcome storage.CampaignActionOutcome) storage.Campaign {
	row.Revision++
	row.GameTime += outcome.GameTimeDelta
	if outcome.CurrentLocationKey != nil {
		row.CurrentLocationKey = *outcome.CurrentLocationKey
	}
	if outcome.Status != nil {
		row.Status = *outcome.Status
	}
	if outcome.OutcomeKey != nil {
		row.OutcomeKey = *outcome.OutcomeKey
	}
	return row
}
