package campaign

import (
	"context"
	"testing"

	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/storage"
)

func TestActionsResolveExactModuleAndVersion(t *testing.T) {
	service := NewService(nil, nil, nil, nil)
	first := contract.EngineRef{Key: "world.first", Version: 1}
	second := contract.EngineRef{Key: "world.second", Version: 1}
	for _, engine := range []contract.EngineRef{first, second} {
		label := engine.Key
		service.RegisterAction(engine, RegisteredAction{
			Definition: ActionDefinition{Key: "world.act", Description: label, Actors: []string{"model"}, ArgumentSchema: storage.JSONMap{"type": "object", "properties": map[string]any{}, "additionalProperties": false}},
			Capability: StaticCapabilityProvider{}, Handler: ActionHandlerFunc(func(context.Context, ActionExecution, map[string]any) (storage.CampaignActionOutcome, error) {
				return storage.CampaignActionOutcome{}, nil
			}),
		})
	}
	for _, engine := range []contract.EngineRef{first, second} {
		capabilities, err := service.actionCapabilities(context.Background(), storage.Campaign{}, &storage.CampaignDomainState{}, engine, []string{"world.act"}, "model", "")
		if err != nil || len(capabilities) != 1 || capabilities[0].Description != engine.Key {
			t.Fatalf("module %v resolved wrong handler: %+v %v", engine, capabilities, err)
		}
	}
	for _, engine := range []contract.EngineRef{{Key: first.Key, Version: 2}, {Key: "world.missing", Version: 1}} {
		if _, err := service.actionCapabilities(context.Background(), storage.Campaign{}, &storage.CampaignDomainState{}, engine, []string{"world.act"}, "model", ""); err == nil {
			t.Fatalf("unknown module version resolved: %v", engine)
		}
	}
}

func TestInstallationRejectsCatalogueWithoutExecutableAction(t *testing.T) {
	registry := contract.NewRegistry()
	registry.RegisterModule(contract.ModuleDescriptor{Key: "test.world", Version: 1, Modes: []string{contract.RuntimeModeCampaign}, CampaignActions: []string{"world.act"}})
	if err := NewService(nil, nil, nil, nil).ValidateInstallation(registry); err == nil {
		t.Fatal("missing action handler accepted")
	}
}
