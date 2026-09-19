package campaignview

import (
	"context"
	"fmt"

	"github.com/ialtone/fatecraft/internal/campaign"
)

const (
	ContextPolicy        = "cultivation.campaign_context"
	GenericContextPolicy = "campaign.generic_context"
)

type Provider struct{}

func (Provider) Snapshot(_ context.Context, input campaign.ProjectionContext) (map[string]any, error) {
	return publicDomainSnapshot(input.Campaign, input.Domain), nil
}

func (Provider) Context(_ context.Context, input campaign.ProjectionContext) (campaign.ProjectionView, error) {
	if input.ContextPolicy != ContextPolicy && input.ContextPolicy != GenericContextPolicy {
		return campaign.ProjectionView{}, fmt.Errorf("不支持的 Campaign 上下文策略: %s", input.ContextPolicy)
	}
	projection := map[string]any{}
	addDomainProjectionForQuery(projection, input.Campaign, input.Domain, input.Query)
	if input.ContextPolicy == GenericContextPolicy {
		for _, key := range []string{
			"character", "realm_system", "techniques", "recipes",
			"encounters", "available_encounters",
		} {
			delete(projection, key)
		}
		return campaign.ProjectionView{
			Data: projection,
			Priority: []string{
				"region_state", "world", "quests", "obligations",
				"referenced_relationships", "current_npc_private_context",
				"inventory", "markets", "factions", "relationships",
				"available_npc_archetypes",
			},
		}, nil
	}
	return campaign.ProjectionView{
		Data: projection,
		Priority: []string{
			"character", "realm_system", "encounters", "available_encounters", "region_state", "world",
			"quests", "obligations", "referenced_relationships", "current_npc_private_context",
			"inventory", "techniques", "recipes", "markets", "factions", "relationships",
			"available_npc_archetypes",
		},
	}, nil
}
