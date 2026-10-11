package worldview

import (
	"context"
	"fmt"
	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldcontract"
	"github.com/ialtone/fatecraft/internal/storage"
)

type Provider struct{}

func (Provider) Snapshot(_ context.Context, input campaign.ProjectionContext) (map[string]any, error) {
	state := input.Domain
	if state == nil {
		state = &storage.CampaignDomainState{}
	}
	row := input.Campaign
	return map[string]any{
		"inventory": InventoryView(state, 0), "markets": MarketViews(row, state, 0),
		"world": WorldView(row, state, false), "relationships": RelationshipViews(row, state, false, 0),
		"factions": FactionViews(state, 0), "region_state": CurrentRegionView(row, state),
		"quests": QuestViews(row, state, "quest", false, 0), "obligations": QuestViews(row, state, "obligation", false, 0),
	}, nil
}

func (p Provider) Inquiry(ctx context.Context, input campaign.ProjectionContext) (map[string]any, error) {
	return p.Snapshot(ctx, input) // this module exposes the same public facts to inquiry
}

func (Provider) Context(_ context.Context, input campaign.ProjectionContext) (campaign.ProjectionView, error) {
	if input.ContextPolicy != worldcontract.ContextPolicy {
		return campaign.ProjectionView{}, fmt.Errorf("不支持的世界上下文策略: %s", input.ContextPolicy)
	}
	state := input.Domain
	if state == nil {
		state = &storage.CampaignDomainState{}
	}
	row := input.Campaign
	return campaign.ProjectionView{Data: map[string]any{
		"inventory": InventoryView(state, 32), "markets": MarketViews(row, state, 64), "world": WorldView(row, state, true),
		"relationships": RelationshipViews(row, state, true, 12), "referenced_relationships": ReferencedRelationshipViews(row, state, input.Query, 4),
		"current_npc_private_context": PrivateNPCViews(row, state, 8), "available_npc_archetypes": SpawnableNPCArchetypeViews(row, state, 12),
		"factions": FactionViews(state, 12), "region_state": CurrentRegionView(row, state),
		"quests": QuestViews(row, state, "quest", true, 12), "obligations": QuestViews(row, state, "obligation", true, 12),
	}, Priority: []string{"region_state", "world", "quests", "obligations", "referenced_relationships", "current_npc_private_context", "inventory", "markets", "factions", "relationships", "available_npc_archetypes"}}, nil
}
