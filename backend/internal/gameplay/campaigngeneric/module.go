// Package campaigngeneric exposes a reusable long-running world engine. It
// deliberately includes only trusted actions that do not require cultivation
// realms, techniques, character vitals, or topic-specific combat rules.
package campaigngeneric

import (
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldcontent"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldcontract"
	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
)

const (
	EngineKey         = "campaign.generic"
	LifecycleHandler  = "campaign.generic_lifecycle"
	ProjectionHandler = "campaign.generic_projection"
	ContextPolicy     = worldcontract.ContextPolicy
)

var actionKeys = []string{
	worldcontract.ActionGainItem,
	worldcontract.ActionTradeBuy,
	worldcontract.ActionTradeSell,
	worldcontract.ActionAdjustMarket,
	worldcontract.ActionTravel,
	worldcontract.ActionInvestigate,
	worldcontract.ActionResolveUncertain,
	worldcontract.ActionInstantiateNPC,
	worldcontract.ActionMoveNPC,
	worldcontract.ActionSetNPCStatus,
	worldcontract.ActionRecordInteraction,
	worldcontract.ActionAdjustFaction,
	worldcontract.ActionAcceptQuest,
	worldcontract.ActionSubmitQuest,
}

func ActionKeys() []string { return append([]string(nil), actionKeys...) }

func Register(registry *gameplay.Registry) {
	registry.RegisterModule(gameplay.ModuleDescriptor{
		Key: EngineKey, Version: 1, Name: "通用长期世界",
		Description:                "以版本化地点、物品、市场、NPC、关系、势力、任务和世界事件构建长期存档。",
		Modes:                      []string{gameplay.RuntimeModeCampaign},
		Actions:                    []string{"update_state", "roll", "domain_action"},
		Verifiers:                  []string{"core.always", "core.conditions"},
		CampaignLifecycleHandlers:  []string{LifecycleHandler},
		CampaignProjectionHandlers: []string{ProjectionHandler},
		CampaignContextPolicies:    []string{ContextPolicy},
		CampaignActions:            ActionKeys(),
		DefinitionValidators:       worldcontent.Shapes(),
		ValidateContent:            worldcontent.ValidateGeneric,
	})
}
