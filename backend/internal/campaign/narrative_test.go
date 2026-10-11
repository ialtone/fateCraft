package campaign

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/storage"
)

type narrativeProjectionProbe struct{}

func (narrativeProjectionProbe) Snapshot(_ context.Context, input ProjectionContext) (map[string]any, error) {
	return map[string]any{"cash": input.Campaign.GameTime, "player_only": "private-player"}, nil
}
func (p narrativeProjectionProbe) Inquiry(ctx context.Context, input ProjectionContext) (map[string]any, error) {
	return p.Snapshot(ctx, input)
}
func (narrativeProjectionProbe) Context(_ context.Context, input ProjectionContext) (ProjectionView, error) {
	return ProjectionView{Data: map[string]any{"cash": input.Campaign.GameTime, "internal": "private-model"}, FactPaths: []string{"cash"}}, nil
}

func TestCampaignNarrativeFeedbackUsesPostActionHeaderAndSharedAudience(t *testing.T) {
	s := NewService(nil, nil, nil, nil)
	s.RegisterProjection("test.narrative", narrativeProjectionProbe{})
	contract := &gameplay.RuntimeContract{Campaign: &gameplay.CampaignConfig{ProjectionHandler: "test.narrative"}}
	beforeRow := storage.Campaign{Revision: 4, Status: storage.CampaignStatusActive, GameTime: 100, CurrentLocationKey: "start"}
	status, location, outcome := storage.CampaignStatusDead, "exit", "timeout"
	afterRow := narrativeCampaignAfter(beforeRow, storage.CampaignActionOutcome{GameTimeDelta: 60, Status: &status, CurrentLocationKey: &location, OutcomeKey: &outcome})
	before, paths, bp, err := s.narrativeActionView(context.Background(), contract, beforeRow, nil)
	if err != nil {
		t.Fatal(err)
	}
	after, _, ap, err := s.narrativeActionView(context.Background(), contract, afterRow, nil)
	if err != nil {
		t.Fatal(err)
	}
	feedback, err := gameplay.NarrativeFeedback("management.work", beforeRow.Revision, afterRow.Revision, before, after, paths, map[string]any{"success": false}, bp || ap)
	if err != nil {
		t.Fatal(err)
	}
	facts := feedback["current_facts"].(map[string]any)
	if feedback["revision"] != int64(5) || facts["campaign.status"] != status || facts["campaign.location"] != location || facts["cash"] != json.Number("160") || feedback["goal_succeeded"] != false {
		t.Fatalf("feedback=%v", feedback)
	}
	raw, _ := json.Marshal(feedback)
	if strings.Contains(string(raw), "private-") {
		t.Fatalf("audience leak: %s", raw)
	}
}
