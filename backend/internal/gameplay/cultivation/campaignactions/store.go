package campaignactions

import (
	"context"
	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/storage"
)

// Store is a cultivation-owned persistence facet; unrelated Campaign modules
// do not need characters, techniques or cultivation encounters.
type Store interface {
	Character(uuid.UUID) (*storage.CampaignCharacter, error)
	SaveCharacter(*storage.CampaignCharacter) error
	Technique(uuid.UUID, string) (*storage.CampaignTechnique, error)
	CreateTechnique(*storage.CampaignTechnique) error
	SaveTechnique(*storage.CampaignTechnique) error
	Techniques(uuid.UUID) ([]storage.CampaignTechnique, error)
	ClearTechniqueSlot(uuid.UUID, string, uuid.UUID) error
	Encounter(uuid.UUID, uuid.UUID) (*storage.CampaignEncounter, error)
	ActiveEncounter(uuid.UUID) (*storage.CampaignEncounter, error)
	CreateEncounter(*storage.CampaignEncounter) error
	SaveEncounter(*storage.CampaignEncounter) error
	GrantCampaignStageReward(context.Context, storage.Campaign, string, string, int64) (*storage.CampaignStageReward, bool, error)
	RecordCampaignStageProgress(context.Context, storage.Campaign, []string) ([]storage.CampaignStageProgress, []storage.CampaignStageProgress, error)
	SettleCampaignStageRewards(context.Context, storage.Campaign, []storage.StageRewardGrant) ([]storage.CampaignStageReward, int64, error)
}

var _ Store = (*storage.CampaignActionStore)(nil)

// Registered handlers check this facet at the boundary before executing rules.
func cultivationStore(execution campaign.ActionExecution) Store {
	return execution.Store.(Store)
}
