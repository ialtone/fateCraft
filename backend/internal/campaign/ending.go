package campaign

import (
	"context"
	"encoding/json"
	"fmt"
	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/storage"
)

func ValidateEndingArguments(schema map[string]any, arguments map[string]any) error {
	return validateActionArguments(storage.JSONMap(schema), arguments)
}

func (s *Service) PlayerModuleProjectionInTx(ctx context.Context, repo *storage.ChallengeRepository, conv *storage.Conversation, runtime *gameplay.RuntimeContract) (map[string]any, error) {
	if conv.CampaignID == nil {
		return nil, ErrCampaignModeRequired
	}
	committer := repo.CampaignActions()
	row, err := committer.Get(ctx, *conv.CampaignID)
	if err != nil {
		return nil, err
	}
	domain, err := committer.DomainState(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	return s.playerProjection(ctx, *row, domain, runtime)
}

func (s *Service) playerProjection(ctx context.Context, row storage.Campaign, domain *storage.CampaignDomainState, runtime *gameplay.RuntimeContract) (map[string]any, error) {
	caps, err := s.actionCapabilities(ctx, row, domain, runtime.Engine, runtime.Campaign.ActionAllowlist, "player", "")
	if err != nil {
		return nil, err
	}
	result := []map[string]any{}
	endings := []map[string]any{}
	if row.Status == storage.CampaignStatusActive {
		for _, c := range caps {
			definition := s.actions[moduleActionKey{Engine: runtime.Engine, Action: c.Key}].Definition
			entry := map[string]any{"key": c.Key, "ending": definition.Ending, "description": c.Description, "available": c.Available, "revision": c.Revision, "argument_schema": c.ArgumentSchema, "violations": c.Violations, "metadata": c.Metadata, "parameters": c.Metadata["parameters"], "plans": c.Metadata["plans"], "cost_labels": c.Metadata["cost_labels"]}
			var endingPlans []gameplay.ActionPlan
			encoded, _ := json.Marshal(c.Metadata["ending_plans"])
			_ = json.Unmarshal(encoded, &endingPlans)
			if len(endingPlans) > 0 {
				if err := validateEndingPlans(c.ArgumentSchema, endingPlans); err != nil {
					return nil, fmt.Errorf("终局方案 %s: %w", c.Key, err)
				}
				// A mixed action remains an ordinary action; only its listed terminal
				// plans enter the read-only ending directory.
				terminal := map[string]any{}
				for key, value := range entry {
					terminal[key] = value
				}
				terminal["ending"] = true
				terminal["plans"] = endingPlans
				terminal["argument_schema"] = cloneJSONMap(c.ArgumentSchema)
				endings = append(endings, terminal)
			}
			result = append(result, entry)
			if definition.Ending {
				endings = append(endings, entry)
			}
		}
	}
	projection := map[string]any{"revision": row.Revision, "status": row.Status, "action_capabilities": result, "ending_capabilities": endings,
		"stage_progress": []storage.StageProgressView{}, "pending_stage_points": int64(0)}
	if row.Status == storage.CampaignStatusActive {
		provider, ok := s.projections[runtime.Campaign.ProjectionHandler]
		if !ok {
			return nil, fmt.Errorf("Campaign 投影处理器未安装: %s", runtime.Campaign.ProjectionHandler)
		}
		snapshot, err := provider.Snapshot(ctx, ProjectionContext{Campaign: row, Domain: domain, ContextPolicy: runtime.Campaign.ContextPolicy})
		if err != nil {
			return nil, err
		}
		if progress, ok := snapshot["stage_progress"]; ok {
			projection["stage_progress"] = progress
			projection["pending_stage_points"] = snapshot["pending_stage_points"]
		}
	}
	return projection, nil
}

func validateEndingPlans(schema storage.JSONMap, plans []gameplay.ActionPlan) error {
	if len(plans) > 16 {
		return fmt.Errorf("方案过多")
	}
	for _, plan := range plans {
		if err := validateActionArguments(schema, plan.Arguments); err != nil {
			return err
		}
	}
	return nil
}
