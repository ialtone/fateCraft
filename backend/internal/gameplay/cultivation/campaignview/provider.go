package campaignview

import (
	"context"
	"fmt"
	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/ascensionrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/effectrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/progressrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/trainingrules"
	"github.com/ialtone/fatecraft/internal/storage"
)

const (
	ContextPolicy = "cultivation.campaign_context"
)

type Provider struct{}

func (Provider) Snapshot(_ context.Context, input campaign.ProjectionContext) (map[string]any, error) {
	if err := validateTechniqueState(input.Domain); err != nil {
		return nil, err
	}
	return snapshotWithAscension(input)
}

func (Provider) Inquiry(_ context.Context, input campaign.ProjectionContext) (map[string]any, error) {
	if err := validateTechniqueState(input.Domain); err != nil {
		return nil, err
	}
	return snapshotWithAscension(input)
}

func (Provider) Context(_ context.Context, input campaign.ProjectionContext) (campaign.ProjectionView, error) {
	if err := validateTechniqueState(input.Domain); err != nil {
		return campaign.ProjectionView{}, err
	}
	if input.ContextPolicy != ContextPolicy {
		return campaign.ProjectionView{}, fmt.Errorf("不支持的 Campaign 上下文策略: %s", input.ContextPolicy)
	}
	projection := map[string]any{}
	addDomainProjectionForQuery(projection, input.Campaign, input.Domain, input.Query)
	training, err := trainingrules.Views(input.Campaign, input.Domain)
	if err != nil {
		return campaign.ProjectionView{}, err
	}
	projection["training"] = training
	progress, err := progressrules.Views(input.Domain)
	if err != nil {
		return campaign.ProjectionView{}, err
	}
	projection["progression"] = progress
	ascension, err := ascensionrules.Inspect(input.Campaign, input.Domain)
	if err != nil {
		return campaign.ProjectionView{}, err
	}
	if ascension != nil {
		projection["ascension"] = ascension
	}

	return campaign.ProjectionView{
		Data:      projection,
		FactPaths: []string{"character.name", "character.realm_key", "character.realm_name", "character.stage_name", "character.cultivation_progress", "character.cultivation_required", "character.health", "character.age_days", "character.lifespan_days"},
		Priority: []string{
			"character", "ascension", "progression", "training", "realm_system", "encounters", "available_encounters", "region_state", "world",
			"quests", "obligations", "referenced_relationships", "current_npc_private_context",
			"inventory", "techniques", "recipes", "markets", "factions", "relationships",
			"available_npc_archetypes",
		},
	}, nil
}

func snapshotWithAscension(input campaign.ProjectionContext) (map[string]any, error) {
	view := publicDomainSnapshot(input.Campaign, input.Domain)
	training, err := trainingrules.Views(input.Campaign, input.Domain)
	if err != nil {
		return nil, err
	}
	view["training"] = training
	progress, err := progressrules.Views(input.Domain)
	if err != nil {
		return nil, err
	}
	view["progression"] = progress
	ascension, err := ascensionrules.Inspect(input.Campaign, input.Domain)
	if err != nil {
		return nil, err
	}
	if ascension != nil {
		view["ascension"] = ascension
	}
	pendingProgress, pending, err := stageProgress(input)
	if err != nil {
		return nil, err
	}
	view["stage_progress"] = pendingProgress
	view["pending_stage_points"] = pending
	return view, nil
}

// stageProgress exposes branch-scoped milestones that have not been settled.
// Once the Campaign ends the receipts are the authoritative record, so the
// pending view is emptied.
func stageProgress(input campaign.ProjectionContext) ([]storage.StageProgressView, int64, error) {
	views := []storage.StageProgressView{}
	if input.Domain == nil || input.Campaign.Status != storage.CampaignStatusActive {
		return views, 0, nil
	}
	d, ok := input.Domain.Definition(progressrules.DefinitionKind, progressrules.DefinitionKey)
	if !ok {
		return views, 0, nil
	}
	c, err := progressrules.Parse(d.Content)
	if err != nil {
		return nil, 0, err
	}
	reached := make(map[string]bool, len(input.Domain.StageProgress))
	for _, row := range input.Domain.StageProgress {
		reached[row.StageKey] = true
	}
	var pending int64
	for index, stage := range c.Stages {
		if !reached[stage.Key] {
			continue
		}
		disabled := ""
		if input.Campaign.LLMRoute == storage.LLMRouteClient {
			disabled = storage.RewardsDisabledReasonClientRoute
		}
		views = append(views, storage.StageProgressView{
			StageKey: stage.Key, Name: stage.Name, SortOrder: index,
			ConfiguredPoints: stage.Points, DisabledReason: disabled,
		})
		pending += stage.Points
	}
	return views, pending, nil
}

func validateTechniqueState(state *storage.CampaignDomainState) error {
	if _, err := effectrules.Equipped(state); err != nil {
		return err
	}
	if state == nil {
		return nil
	}
	for _, row := range state.Techniques {
		definition, exists := state.Definition(storage.DefinitionTechnique, row.TechniqueKey)
		if !exists {
			return fmt.Errorf("功法定义不存在: %s", row.TechniqueKey)
		}
		if _, valid := effectrules.MasteredEffects(definition.Content, row.Mastery, row.LearnedLayer); !valid {
			return fmt.Errorf("功法熟练度、层数或效果不一致: %s", row.TechniqueKey)
		}
	}
	return nil
}
