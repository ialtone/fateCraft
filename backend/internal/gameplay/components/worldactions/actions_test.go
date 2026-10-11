package worldactions

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldcontract"
	"github.com/ialtone/fatecraft/internal/storage"
)

// Every unimplemented store operation panics. These tests therefore detect
// accidental reads of cultivation characters, techniques or encounters.
type neutralWorldStore struct {
	campaign.ActionStore
	definitions map[string]*storage.GameplayDefinition
	discovered  map[string]bool
}

func (s *neutralWorldStore) Definition(_ uuid.UUID, kind, key string) (*storage.GameplayDefinition, error) {
	row := s.definitions[kind+":"+key]
	if row == nil {
		return nil, storage.ErrNotFound
	}
	return row, nil
}
func (s *neutralWorldStore) IsLocationDiscovered(_ uuid.UUID, key string) (bool, error) {
	return s.discovered[key], nil
}
func (s *neutralWorldStore) DiscoverLocation(row *storage.CampaignLocationDiscovery) error {
	s.discovered[row.LocationKey] = true
	return nil
}

func (s *neutralWorldStore) DueScheduledEvents(uuid.UUID, int64, int) ([]storage.CampaignScheduledEvent, error) {
	return nil, nil
}

func TestRegisteredWorldActionsWorkWithoutCultivationState(t *testing.T) {
	store := &neutralWorldStore{definitions: map[string]*storage.GameplayDefinition{
		"location:start": {Kind: "location", Key: "start", Name: "营地", Content: storage.JSONMap{"connections": []any{map[string]any{"location_key": "next", "travel_minutes": int64(1500)}}}},
		"location:next":  {Kind: "location", Key: "next", Name: "海岸", Content: storage.JSONMap{}},
	}, discovered: map[string]bool{"start": true}}
	execution := campaign.ActionExecution{Campaign: storage.Campaign{ID: uuid.New(), CurrentLocationKey: "start", Status: storage.CampaignStatusActive}, Store: store, ActorType: "player", ActionID: "explore", MaxDueEventsPerTurn: 8}
	calls := 0
	execution.Roll = func(_ context.Context, request campaign.CampaignRollRequest) (*campaign.CampaignRollResult, error) {
		calls++
		if len(request.RelevantAttributes) != 0 || len(request.PlayerState) != 0 || request.Context != nil {
			t.Fatalf("topic facts leaked into shared roll: %+v", request)
		}
		return &campaign.CampaignRollResult{Success: true, Rolled: 1, SuccessRate: 70}, nil
	}
	actions := map[string]campaign.RegisteredAction{}
	for _, action := range Actions() {
		actions[action.Definition.Key] = action
	}
	// Select an ordinary successful server seed; the assertion is about
	// isolation, while the companion test proves failures are also possible.
	for DeterministicPercent(execution, "roll:world.investigate.start") > 70 {
		execution.Campaign.RandomSeed++
	}
	outcome, err := actions[worldcontract.ActionInvestigate].Handler.Execute(context.Background(), execution, map[string]any{})
	if err != nil || outcome.Result["discovered"] != true || calls != 0 || !store.discovered["next"] || outcome.Roll == nil {
		t.Fatalf("investigate: %+v %v", outcome, err)
	}
	outcome, err = actions[worldcontract.ActionTravel].Handler.Execute(context.Background(), execution, map[string]any{"destination_key": "next"})
	if err != nil || outcome.CurrentLocationKey == nil || *outcome.CurrentLocationKey != "next" || outcome.GameTimeDelta != 1500 || outcome.Status != nil {
		t.Fatalf("travel: %+v %v", outcome, err)
	}
	_, err = actions[worldcontract.ActionTravel].Handler.Execute(context.Background(), execution, map[string]any{"destination_key": "unknown"})
	if !errors.Is(err, campaign.ErrActionRejected) {
		t.Fatalf("unknown destination accepted: %v", err)
	}
}

func TestUncertainActionDoesNotWriteWorldFacts(t *testing.T) {
	execution := campaign.ActionExecution{Campaign: storage.Campaign{ID: uuid.New()}, Store: &neutralWorldStore{}, ActorType: "model", ActionID: "social-roll"}
	outcome, err := ResolveUncertainAction(context.Background(), execution, map[string]any{"kind": "social", "description": "向守门人说明来意"})
	if err != nil || outcome.GameTimeDelta != 0 || len(outcome.Events) != 1 {
		t.Fatalf("unexpected uncertain result: %+v %v", outcome, err)
	}
	_, err = ResolveUncertainAction(context.Background(), execution, map[string]any{"kind": "breakthrough", "description": "绕过专用动作"})
	if !errors.Is(err, campaign.ErrInvalidAction) {
		t.Fatalf("unsupported roll accepted: %v", err)
	}
}

func TestNeutralRollDoesNotSilentlyGrantSuccess(t *testing.T) {
	execution := campaign.ActionExecution{Campaign: storage.Campaign{RandomSeed: 4}, ActionID: "neutral"}
	request := campaign.CampaignRollRequest{Key: "test", Title: "测试", Sides: 100, BaseSuccessRate: 0}
	result, err := ResolveCampaignRoll(context.Background(), execution, request)
	if err != nil || result == nil || result.Success || result.Snapshot == nil {
		t.Fatalf("missing roll silently succeeded: %+v %v", result, err)
	}
	repeat, err := ResolveCampaignRoll(context.Background(), execution, request)
	if err != nil || repeat.Rolled != result.Rolled {
		t.Fatal("neutral roll changed on retry")
	}
}
