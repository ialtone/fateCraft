package campaignruntime

import (
	"context"
	"testing"

	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/storage"
)

type audienceProbe struct{ contract.RuntimeModule }

func (audienceProbe) ValidateState(contract.Content, contract.RunState) error { return nil }
func (audienceProbe) Project(_ contract.Content, _ contract.RunState, audience string) (map[string]any, error) {
	return map[string]any{"audience": audience}, nil
}

func TestAdapterUsesEachAudienceAndValidatesTerminalCursor(t *testing.T) {
	ref := contract.EngineRef{Key: "test.audience", Version: 1}
	runtime, err := storage.EncodeGameplayRuntime(contract.RuntimeContract{Engine: ref, Mode: contract.RuntimeModeCampaign})
	if err != nil {
		t.Fatal(err)
	}
	a := adapter{descriptor: contract.ModuleDescriptor{Key: ref.Key, Version: ref.Version, Runtime: audienceProbe{}}}
	input := campaign.ProjectionContext{Campaign: storage.Campaign{Status: storage.CampaignStatusActive}, Domain: &storage.CampaignDomainState{
		RuntimeDocument: runtime, RuntimeState: storage.JSONMap{"schema_version": 1, "revision": 0, "game_time": 0, "status": "active", "data": map[string]any{"internal": "private"}},
	}}
	for audience, project := range map[string]func(context.Context, campaign.ProjectionContext) (map[string]any, error){"player": a.Snapshot, "inquiry": a.Inquiry} {
		view, err := project(context.Background(), input)
		if err != nil || view["gameplay"].(map[string]any)["audience"] != audience {
			t.Fatalf("%s projection = %v, %v", audience, view, err)
		}
	}
	model, err := a.Context(context.Background(), input)
	if err != nil || model.Data["gameplay"].(map[string]any)["audience"] != "model" {
		t.Fatalf("model projection = %v, %v", model, err)
	}
	input.Campaign.Status = storage.CampaignStatusCompleted
	if _, err := a.Snapshot(context.Background(), input); err == nil {
		t.Fatal("terminal Campaign accepted active module state")
	}
	input.Campaign.Status = storage.CampaignStatusActive
	input.Campaign.Revision = 1
	if _, err := a.Snapshot(context.Background(), input); err == nil {
		t.Fatal("old module revision accepted")
	}
}
