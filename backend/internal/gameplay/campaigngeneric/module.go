// Package campaigngeneric exposes a reusable long-running world engine. It
// deliberately includes only trusted actions that do not require cultivation
// realms, techniques, character vitals, or topic-specific combat rules.
package campaigngeneric

import (
	"github.com/ialtone/fatecraft/internal/campaignreward"
	"github.com/ialtone/fatecraft/internal/gameplay"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/campaignactions"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/campaignview"
)

const (
	EngineKey         = "campaign.generic"
	LifecycleHandler  = "campaign.generic_lifecycle"
	ProjectionHandler = "campaign.generic_projection"
	ContextPolicy     = campaignview.GenericContextPolicy
)

var actionKeys = []string{
	campaignactions.ActionGainItem,
	campaignactions.ActionTradeBuy,
	campaignactions.ActionTradeSell,
	campaignactions.ActionAdjustMarket,
	campaignactions.ActionTravel,
	campaignactions.ActionInvestigate,
	campaignactions.ActionResolveUncertain,
	campaignactions.ActionInstantiateNPC,
	campaignactions.ActionMoveNPC,
	campaignactions.ActionSetNPCStatus,
	campaignactions.ActionRecordInteraction,
	campaignactions.ActionAdjustFaction,
	campaignactions.ActionAcceptQuest,
	campaignactions.ActionSubmitQuest,
}

func ActionKeys() []string { return append([]string(nil), actionKeys...) }

func Register(registry *gameplay.Registry) {
	registry.RegisterModule(gameplay.ModuleDescriptor{
		Key: EngineKey, Version: 1, Name: "通用长期世界",
		Description:                "以版本化地点、物品、市场、NPC、关系、势力、任务和世界事件构建长期存档。",
		Modes:                      []string{gameplay.RuntimeModeCampaign},
		Actions:                    []string{"update_state", "roll", "domain_action"},
		Verifiers:                  []string{"core.always", "core.conditions"},
		Calculators:                []string{"core.fixed", "core.state_multiplier", campaignreward.ItemStackCalculatorKey},
		CampaignLifecycleHandlers:  []string{LifecycleHandler},
		CampaignProjectionHandlers: []string{ProjectionHandler},
		CampaignContextPolicies:    []string{ContextPolicy},
		CampaignActions:            ActionKeys(),
	})
}
