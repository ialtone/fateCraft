package campaignactions

import (
	"context"
	"testing"

	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/trainingrules"
	"github.com/ialtone/fatecraft/internal/storage"
)

func breakthroughFixture() (*actionStoreStub, campaign.ActionExecution) {
	s, e := trainingFixture()
	c := s.character
	c.CultivationProgress = c.CultivationRequired
	c.AgeDays = c.LifespanDays - 1
	c.Injuries = storage.JSONMap{}
	s.definitions["realm:foundation"].Content = storage.JSONMap{
		"rank": 2, "next_realm_key": "core", "breakthrough_minutes": 60,
		"failure_health_damage": 250,
	}
	s.definitions["realm:core"] = &storage.GameplayDefinition{Kind: "realm", Key: "core", Content: storage.JSONMap{
		"rank": 3, "lifespan_days": 100000, "cultivation_required": 2000,
	}}
	e.Roll = forcedCampaignRoll(true, "success", 1, 95)
	return s, e
}

func TestBreakthroughChecksOldLifespanBeforeRolling(t *testing.T) {
	for _, tc := range []struct {
		name             string
		time             int64
		alive, available bool
	}{
		{"one-minute-spare", 1379, true, true},
		{"exact-boundary", 1380, true, false},
		{"past-boundary", 1439, true, false},
		{"dead", 0, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, e := breakthroughFixture()
			e.Campaign.GameTime = tc.time
			if !tc.alive {
				s.character.Health = 0
			}
			capability := campaign.ActionCapability{Available: true, Metadata: storage.JSONMap{}}
			state := &storage.CampaignDomainState{Character: s.character, Definitions: map[string]storage.GameplayDefinition{"realm:foundation": *s.definitions["realm:foundation"]}}
			projectBreakthroughCapability(&capability, e.Campaign, state)
			if capability.Available != tc.available {
				t.Fatalf("incorrect preview: %+v", capability)
			}
			if !tc.available {
				e.Roll = func(context.Context, campaign.CampaignRollRequest) (*campaign.CampaignRollResult, error) {
					t.Fatal("ineligible breakthrough rolled")
					return nil, nil
				}
			}
			before := *s.character
			out, err := attemptBreakthrough(context.Background(), e, map[string]any{})
			if tc.available {
				if err != nil || out.Result["success"] != true || s.character.RealmKey != "core" || s.character.LifespanDays != 100000 {
					t.Fatalf("valid last-day breakthrough: %+v %v", out, err)
				}
			} else if err == nil || s.character.AgeDays != before.AgeDays || s.character.LifespanDays != before.LifespanDays || s.character.RealmKey != before.RealmKey || s.character.CultivationProgress != before.CultivationProgress {
				t.Fatalf("rejected breakthrough mutated character: %+v %v", s.character, err)
			}
		})
	}
}

func TestBreakthroughBacklashRequiresPaidRecovery(t *testing.T) {
	s, e := breakthroughFixture()
	s.character.AgeDays = 5000
	e.Roll = forcedCampaignRoll(false, "failure", 99, 50)
	if _, err := attemptBreakthrough(context.Background(), e, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	wound := s.character.Injuries["breakthrough_backlash"].(map[string]any)
	if wound["category"] != "injury" || wound["severity"] != int64(100) {
		t.Fatalf("invalid injury: %+v", wound)
	}
	e.ActorType = "model"
	if _, err := resolveCondition(context.Background(), e, map[string]any{"category": "injury", "effect_key": "breakthrough_backlash"}); err == nil {
		t.Fatal("model removed rule injury for free")
	}
	if _, err := applyCondition(context.Background(), e, map[string]any{"category": "injury", "effect_key": "breakthrough_backlash", "severity": 1, "duration_minutes": 1}); err == nil {
		t.Fatal("model replaced rule injury")
	}
	e.ActorType = "player"
	s.definitions[trainingrules.DefinitionKind+":recovery"].Content["injury_relief"] = 50
	for i := 0; i < 2; i++ {
		out, err := recoverCharacter(context.Background(), e, map[string]any{"plan_key": "recovery"})
		if err != nil {
			t.Fatal(err)
		}
		e.Campaign.GameTime += out.GameTimeDelta
		if i == 0 && s.character.Injuries["breakthrough_backlash"].(map[string]any)["severity"] != int64(50) {
			t.Fatal("first recovery did not reduce severity")
		}
	}
	if _, ok := s.character.Injuries["breakthrough_backlash"]; ok {
		t.Fatal("paid recovery left injury behind")
	}
	if s.stacks["stone:common"].Quantity != 16 || s.character.AgeDays != 5014 {
		t.Fatal("recovery did not pay resources/time")
	}
}
