package campaignactions

import (
	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/effectrules"
	"github.com/ialtone/fatecraft/internal/storage"
)

func equippedEffects(execution campaign.ActionExecution) (map[string]int64, error) {
	state := &storage.CampaignDomainState{Definitions: map[string]storage.GameplayDefinition{}}
	var err error
	state.Techniques, err = cultivationStore(execution).Techniques(execution.Campaign.ID)
	if err != nil {
		return nil, err
	}
	state.ItemInstances, err = execution.Store.ItemInstances(execution.Campaign.ID)
	if err != nil {
		return nil, err
	}
	for _, t := range state.Techniques {
		if t.Status != "active" || t.EquippedSlot == nil {
			continue
		}
		d, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionTechnique, t.TechniqueKey)
		if err != nil {
			return nil, err
		}
		state.Definitions[d.Kind+":"+d.Key] = *d
	}
	for _, item := range state.ItemInstances {
		if item.State != "equipped" || item.EquippedSlot == nil || item.Durability <= 0 {
			continue
		}
		d, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionEquipment, item.DefinitionKey)
		if err != nil {
			return nil, err
		}
		state.Definitions[d.Kind+":"+d.Key] = *d
	}
	return effectrules.Equipped(state)
}
