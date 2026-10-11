package cultivation

import (
	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/ascensionrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/campaigncontract"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/contentrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/originrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/progressrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/trainingrules"
)

const (
	CampaignEngineKey         = campaigncontract.EngineKey
	CampaignEngineVersion     = campaigncontract.EngineVersion
	CampaignLifecycleHandler  = "cultivation.campaign_lifecycle"
	CampaignProjectionHandler = "cultivation.campaign_projection"
	CampaignContextPolicy     = "cultivation.campaign_context"
)

func Register(registry *gameplay.Registry) {
	registry.RegisterModule(gameplay.ModuleDescriptor{
		Key: CampaignEngineKey, Version: CampaignEngineVersion, Name: "修仙长期存档",
		Category:                   "cultivation",
		Description:                "以 Campaign 作为权威存档，使用受控领域动作、事件账本和有界上下文投影。",
		Modes:                      []string{gameplay.RuntimeModeCampaign},
		Actions:                    []string{"update_state", "roll", "domain_action"},
		Verifiers:                  []string{"core.always", "core.conditions"},
		CampaignLifecycleHandlers:  []string{CampaignLifecycleHandler},
		CampaignProjectionHandlers: []string{CampaignProjectionHandler},
		CampaignContextPolicies:    []string{CampaignContextPolicy},
		CampaignActions:            campaigncontract.ActionKeys(),
		AbandonAction:              campaigncontract.ActionAbandon,
		CampaignActionWrites: map[string][]string{
			campaigncontract.ActionRenameCharacter: {"character.name"},
		},
		CampaignActionEvents: map[string][]string{
			campaigncontract.ActionRenameCharacter: {"character.renamed"},
		},
		DefinitionValidators: contentrules.Shapes(),
		DefinitionSchemas:    map[string]map[string]any{ascensionrules.DefinitionKind: ascensionrules.Schema(), trainingrules.DefinitionKind: trainingrules.Schema(), progressrules.DefinitionKind: progressrules.Schema(), originrules.DefinitionKind: originrules.Schema()},
		DefinitionLabels:     map[string]string{ascensionrules.DefinitionKind: "飞升与最终渡劫", trainingrules.DefinitionKind: "闭关与休整方案", progressrules.DefinitionKind: "阶段积分与通关预算", originrules.DefinitionKind: "出身与开局物资"},
		ValidateContent:      contentrules.Validate,
	})
}
