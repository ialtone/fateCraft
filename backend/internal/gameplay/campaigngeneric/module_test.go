package campaigngeneric

import (
	"encoding/json"
	"os"
	"testing"

	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
)

func TestGenericCampaignViewFixtureUsesSharedContract(t *testing.T) {
	data, err := os.ReadFile("testdata/runtime/runtime.json")
	if err != nil {
		t.Fatal(err)
	}
	var entry struct {
		Engine   gameplay.EngineRef `json:"engine"`
		Mode     string             `json:"mode"`
		Includes struct {
			Campaign     string `json:"campaign"`
			StateSchema  string `json:"state_schema"`
			Actions      string `json:"actions"`
			Settlement   string `json:"settlement"`
			Presentation string `json:"presentation"`
		} `json:"includes"`
	}
	if err := json.Unmarshal(data, &entry); err != nil {
		t.Fatal(err)
	}
	read := func(name string, target any) {
		raw, err := os.ReadFile("testdata/runtime/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, target); err != nil {
			t.Fatal(err)
		}
	}
	contract := gameplay.RuntimeContract{Engine: entry.Engine, Mode: entry.Mode}
	var statePart struct {
		StateSchema gameplay.StateSchema `json:"state_schema"`
	}
	var actionsPart struct {
		Actions gameplay.ActionConfig `json:"actions"`
	}
	var settlementPart struct {
		Settlement gameplay.SettlementConfig `json:"settlement"`
	}
	var presentationPart struct {
		Presentation gameplay.Presentation `json:"presentation"`
	}
	read(entry.Includes.StateSchema, &statePart)
	read(entry.Includes.Actions, &actionsPart)
	read(entry.Includes.Settlement, &settlementPart)
	read(entry.Includes.Presentation, &presentationPart)
	contract.StateSchema = statePart.StateSchema
	contract.Actions = actionsPart.Actions
	contract.Settlement = settlementPart.Settlement
	contract.Presentation = presentationPart.Presentation
	if entry.Includes.Campaign != "" {
		var campaignPart struct {
			Campaign gameplay.CampaignConfig `json:"campaign"`
		}
		read(entry.Includes.Campaign, &campaignPart)
		contract.Campaign = &campaignPart.Campaign
	}
	registry := gameplay.NewCoreRegistry()
	Register(registry)
	if err := registry.ValidateRuntime(&contract); err != nil {
		t.Fatalf("generic campaign view fixture rejected: %v", err)
	}
	if contract.Presentation.CampaignView == nil || contract.Presentation.CampaignView.Renderer != "generic" {
		t.Fatal("generic campaign fixture did not declare the shared renderer")
	}
}

func TestGenericCampaignModuleAcceptsRealmFreeRuntime(t *testing.T) {
	registry := gameplay.NewCoreRegistry()
	Register(registry)
	contract := &gameplay.RuntimeContract{
		Engine: gameplay.EngineRef{Key: EngineKey, Version: 1},
		Mode:   gameplay.RuntimeModeCampaign,
		Campaign: &gameplay.CampaignConfig{
			LifecycleHandler: LifecycleHandler, ProjectionHandler: ProjectionHandler,
			ContextPolicy: ContextPolicy, ActionAllowlist: []string{ActionKeys()[0]},
			FieldOwnership:     []gameplay.FieldOwnership{{Path: "world", Owner: gameplay.OwnerCampaign, ValueType: "object", Visibility: gameplay.VisibilityPlayer, StorageProjection: "campaigns"}},
			InitialLocationKey: "world.start",
			WorldClock:         gameplay.WorldClockConfig{CalendarKey: "world.default", MaxDueEventsPerTurn: 16},
		},
		StateSchema: gameplay.StateSchema{SchemaVersion: 1, Fields: []gameplay.StateField{{
			Path: "narrative.current_focus", Type: "string", Visibility: gameplay.VisibilityPlayer,
			UpdatePolicy: gameplay.UpdateModel,
		}}},
		Actions:      gameplay.ActionConfig{Model: []string{"update_state", "domain_action"}},
		Presentation: gameplay.Presentation{StatePanelTitle: "World"},
	}
	if err := registry.ValidateRuntime(contract); err != nil {
		t.Fatalf("generic runtime rejected: %v", err)
	}
}

func TestGenericCampaignModuleDoesNotExposeCultivationActions(t *testing.T) {
	registry := gameplay.NewCoreRegistry()
	Register(registry)
	contract := &gameplay.RuntimeContract{
		Engine: gameplay.EngineRef{Key: EngineKey, Version: 1},
		Mode:   gameplay.RuntimeModeCampaign,
		Campaign: &gameplay.CampaignConfig{
			LifecycleHandler: LifecycleHandler, ProjectionHandler: ProjectionHandler,
			ContextPolicy: ContextPolicy, ActionAllowlist: []string{"cultivation.practice"},
			FieldOwnership:     []gameplay.FieldOwnership{{Path: "world", Owner: gameplay.OwnerCampaign, ValueType: "object", Visibility: gameplay.VisibilityPlayer, StorageProjection: "campaigns"}},
			InitialLocationKey: "world.start",
			WorldClock:         gameplay.WorldClockConfig{CalendarKey: "world.default", MaxDueEventsPerTurn: 16},
		},
		StateSchema: gameplay.StateSchema{SchemaVersion: 1, Fields: []gameplay.StateField{{
			Path: "narrative.current_focus", Type: "string", Visibility: gameplay.VisibilityPlayer,
			UpdatePolicy: gameplay.UpdateModel,
		}}},
		Actions:      gameplay.ActionConfig{Model: []string{"update_state", "domain_action"}},
		Presentation: gameplay.Presentation{StatePanelTitle: "World"},
	}
	if err := registry.ValidateRuntime(contract); err == nil {
		t.Fatal("generic runtime accepted a cultivation-only action")
	}
}
