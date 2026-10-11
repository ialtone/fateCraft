// Package trial owns the bounded, single-life cultivation rules. Only the
// generic Challenge adapter persists these states or grants account rewards.
package trial

import (
	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
)

const EngineKey = "cultivation.trial"

func Register(registry *contract.Registry) {
	registry.RegisterModule(contract.ModuleDescriptor{
		Key: EngineKey, Version: 2, Name: "修仙单局试炼", Category: "cultivation",
		Description:        "一世一局的修仙试炼，以固定版本的判定、资格和结局规则结算。",
		Modes:              []string{contract.RuntimeModeChallenge},
		Actions:            []string{"update_state", "domain_action", "complete_challenge"},
		Verifiers:          []string{"core.conditions"},
		SettlementHandlers: []string{"core.challenge_completion"},
		Runtime:            Module{}, AbandonAction: "trial.abandon", ValidateContent: ValidateContent,
		DefinitionLabels:  map[string]string{DefinitionKind: "一世修行规则"},
		DefinitionSchemas: map[string]map[string]any{DefinitionKind: configSchema()},
		DefinitionValidators: map[string]contract.DefinitionValidator{DefinitionKind: func(d contract.Definition) error {
			c, err := ruledata.Decode[Config](d.Content)
			if err != nil {
				return err
			}
			return validateConfig(c)
		}},
	})
}
