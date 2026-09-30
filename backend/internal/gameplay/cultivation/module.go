package cultivation

import (
	"github.com/ialtone/fatecraft/internal/gameplay"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/campaigncontract"
)

const (
	CampaignEngineKey         = "cultivation.campaign"
	CampaignLifecycleHandler  = "cultivation.campaign_lifecycle"
	CampaignProjectionHandler = "cultivation.campaign_projection"
	CampaignContextPolicy     = "cultivation.campaign_context"
)

func Register(registry *gameplay.Registry) {
	registry.RegisterModule(gameplay.ModuleDescriptor{
		Key: CampaignEngineKey, Version: 1, Name: "修仙长期存档",
		Description:                "以 Campaign 作为权威存档，使用受控领域动作、事件账本和有界上下文投影。",
		Modes:                      []string{gameplay.RuntimeModeCampaign},
		Actions:                    []string{"update_state", "roll", "domain_action"},
		Verifiers:                  []string{"core.always", "core.conditions"},
		CampaignLifecycleHandlers:  []string{CampaignLifecycleHandler},
		CampaignProjectionHandlers: []string{CampaignProjectionHandler},
		CampaignContextPolicies:    []string{CampaignContextPolicy},
		CampaignActions:            campaigncontract.ActionKeys(),
		CampaignActionWrites: map[string][]string{
			campaigncontract.ActionRenameCharacter: {"character.name"},
		},
		CampaignActionEvents: map[string][]string{
			campaigncontract.ActionRenameCharacter: {"character.renamed"},
		},
		CampaignRequiresRealm: true,
	})
}
