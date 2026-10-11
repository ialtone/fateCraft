package storage

import (
	"encoding/json"
	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
)

func DecodeGameplayRuntime(row ChallengeRuntimeConfig) (*gameplay.RuntimeContract, error) {
	document := gameplay.RuntimeDocument{
		EngineKey: row.EngineKey, EngineVersion: row.EngineVersion, Mode: row.Mode,
		StateSchema: json.RawMessage(row.StateSchema), ActionConfig: json.RawMessage(row.ActionConfig),
		SettlementConfig: json.RawMessage(row.SettlementConfig), Presentation: json.RawMessage(row.Presentation),
	}
	if row.CampaignConfig != nil {
		config := json.RawMessage(*row.CampaignConfig)
		document.CampaignConfig = &config
	}
	return gameplay.DecodeRuntime(document)
}

func EncodeGameplayRuntime(contract gameplay.RuntimeContract) (ChallengeRuntimeConfig, error) {
	document, err := gameplay.EncodeRuntime(contract)
	if err != nil {
		return ChallengeRuntimeConfig{}, err
	}
	row := ChallengeRuntimeConfig{
		EngineKey: document.EngineKey, EngineVersion: document.EngineVersion, Mode: document.Mode,
		StateSchema: JSONDoc(document.StateSchema), ActionConfig: JSONDoc(document.ActionConfig),
		SettlementConfig: JSONDoc(document.SettlementConfig), Presentation: JSONDoc(document.Presentation),
	}
	if document.CampaignConfig != nil {
		config := JSONDoc(*document.CampaignConfig)
		row.CampaignConfig = &config
	}
	return row, nil
}
