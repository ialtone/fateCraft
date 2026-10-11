package cultivation

import (
	"testing"

	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/campaigncontract"
)

func TestCampaignModuleOwnsRuntimeActionAllowlist(t *testing.T) {
	registry := gameplay.NewCoreRegistry()
	Register(registry)
	contract := &gameplay.RuntimeContract{
		Engine: gameplay.EngineRef{Key: CampaignEngineKey, Version: CampaignEngineVersion}, Mode: gameplay.RuntimeModeCampaign,
		Campaign: &gameplay.CampaignConfig{
			LifecycleHandler: CampaignLifecycleHandler, ProjectionHandler: CampaignProjectionHandler,
			ContextPolicy: CampaignContextPolicy, ActionAllowlist: campaigncontract.ActionKeys(), InitialLocationKey: "world.start",
			FieldOwnership: []gameplay.FieldOwnership{{Path: "character", Owner: gameplay.OwnerCampaign, ValueType: "object", Visibility: gameplay.VisibilityPlayer, StorageProjection: "campaign_characters", WriteCapability: campaigncontract.ActionRenameCharacter, EventTypes: []string{"character.renamed"}}},
			WorldClock:     gameplay.WorldClockConfig{CalendarKey: "world.default", MaxDueEventsPerTurn: 16},
		},
		StateSchema: gameplay.StateSchema{SchemaVersion: 1, Fields: []gameplay.StateField{{
			Path: "narrative.current_focus", Type: "string", Visibility: gameplay.VisibilityPlayer,
			UpdatePolicy: gameplay.UpdateModel,
		}}},
		Actions:      gameplay.ActionConfig{Model: []string{"update_state", "domain_action"}},
		Presentation: gameplay.Presentation{StatePanelTitle: "state"},
	}
	if err := registry.ValidateRuntime(contract); err != nil {
		t.Fatalf("valid campaign runtime rejected: %v", err)
	}
	contract.Campaign.ActionAllowlist = append(contract.Campaign.ActionAllowlist, "other.module.action")
	if err := registry.ValidateRuntime(contract); err == nil {
		t.Fatal("cross-module campaign action was accepted")
	}
}
