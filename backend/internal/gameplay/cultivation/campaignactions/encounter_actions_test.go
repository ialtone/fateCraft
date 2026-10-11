package campaignactions

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/storage"
)

func encounterFixture(t *testing.T, mutate func(storage.JSONMap)) (*actionStoreStub, campaign.ActionExecution) {
	t.Helper()
	campaignID, versionID := uuid.New(), uuid.New()
	store := newActionStore()
	store.character = &storage.CampaignCharacter{
		CampaignID: campaignID, RealmKey: "mortal", RealmRank: 1,
		AgeDays: 20, LifespanDays: 10_000, Health: 100, MaxHealth: 100, Mana: 50, MaxMana: 50,
	}
	content := storage.JSONMap{
		"location_key": "mist-wood", "opponent_key": "mist-beast", "opponent_name": "雾兽",
		"opponent_realm_rank": int64(1), "opponent_max_health": int64(60), "opponent_attack": int64(12),
		"opponent_defense": int64(3), "player_damage_base": int64(5), "player_damage_scale_bps": int64(10_000),
		"player_mana_cost": int64(2), "round_minutes": int64(5), "max_rounds": int64(10), "flee_chance_bps": int64(2_500),
		"rewards": []any{map[string]any{"item_key": "mist-hide", "quality_key": "common", "quantity": int64(2)}},
	}
	if mutate != nil {
		mutate(content)
	}
	store.definitions[storage.DefinitionEncounter+":mist-beast"] = &storage.GameplayDefinition{
		ChallengeVersionID: versionID, Kind: storage.DefinitionEncounter, Key: "mist-beast", Name: "林间雾兽", Content: content,
	}
	store.definitions[storage.DefinitionItem+":mist-hide"] = &storage.GameplayDefinition{
		ChallengeVersionID: versionID, Kind: storage.DefinitionItem, Key: "mist-hide", Name: "雾兽皮", Content: storage.JSONMap{"stackable": true},
	}
	execution := campaign.ActionExecution{
		Campaign: storage.Campaign{ID: campaignID, ChallengeVersionID: versionID, CurrentLocationKey: "mist-wood", RandomSeed: 41, Revision: 7},
		Store:    store, ActorType: "player", ActionID: "encounter-turn-1", ActionKey: ActionEncounterAct,
	}
	return store, execution
}

func startFixtureEncounter(t *testing.T, store *actionStoreStub, execution campaign.ActionExecution) *storage.CampaignEncounter {
	t.Helper()
	if _, err := startEncounter(context.Background(), execution, map[string]any{"encounter_key": "mist-beast"}); err != nil {
		t.Fatal(err)
	}
	row, err := store.ActiveEncounter(execution.Campaign.ID)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func TestEncounterStartRequiresLocationAndOnlyOneActiveEncounter(t *testing.T) {
	store, execution := encounterFixture(t, nil)
	execution.Campaign.CurrentLocationKey = "green-river-village"
	if _, err := startEncounter(context.Background(), execution, map[string]any{"encounter_key": "mist-beast"}); !errors.Is(err, campaign.ErrActionRejected) {
		t.Fatalf("expected location rejection, got %v", err)
	}
	execution.Campaign.CurrentLocationKey = "mist-wood"
	row := startFixtureEncounter(t, store, execution)
	if row.Round != 1 || row.OpponentHealth != 60 || row.Status != "active" {
		t.Fatalf("unexpected encounter snapshot: %+v", row)
	}
	if _, err := startEncounter(context.Background(), execution, map[string]any{"encounter_key": "mist-beast"}); !errors.Is(err, campaign.ErrActionRejected) {
		t.Fatalf("expected duplicate active encounter rejection, got %v", err)
	}
}

func TestActiveEncounterBlocksUnrelatedDomainActions(t *testing.T) {
	store, execution := encounterFixture(t, nil)
	startFixtureEncounter(t, store, execution)
	if err := rejectWhileEncounterActive(execution, ActionTravel); !errors.Is(err, campaign.ErrActionRejected) {
		t.Fatalf("expected active encounter guard, got %v", err)
	}
	if err := rejectWhileEncounterActive(execution, ActionEncounterAct); err != nil {
		t.Fatalf("encounter action should remain available: %v", err)
	}
	if err := rejectWhileEncounterActive(execution, ActionAbandon); err != nil {
		t.Fatalf("active encounter blocks voluntary exit: %v", err)
	}
	if err := rejectWhileEncounterActive(execution, ActionEncounterFlee); err != nil {
		t.Fatalf("flee action should remain available: %v", err)
	}
}

func TestEncounterAttackIsDeterministicAndVictoryRewardsOnce(t *testing.T) {
	mutate := func(content storage.JSONMap) { content["opponent_max_health"] = int64(1) }
	store, execution := encounterFixture(t, mutate)
	slot := "spell"
	store.techniques["ember-strike"] = &storage.CampaignTechnique{
		ID: uuid.New(), CampaignID: execution.Campaign.ID, TechniqueKey: "ember-strike", LearnedLayer: 1, EquippedSlot: &slot, Status: "active",
	}
	store.definitions[storage.DefinitionTechnique+":ember-strike"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionTechnique, Key: "ember-strike", Content: storage.JSONMap{
			"max_layer": int64(12), "combat_mana_cost": int64(3),
			"layer_effects": []any{map[string]any{"layer": int64(1), "effects": map[string]any{"spiritual_attack": int64(10)}}},
		},
	}
	row := startFixtureEncounter(t, store, execution)
	outcome, err := actEncounter(context.Background(), execution, map[string]any{
		"encounter_id": row.ID.String(), "move": "attack", "technique_key": "ember-strike",
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.GameTimeDelta != 5 || outcome.Result["status"] != "won" || store.character.Mana != 45 {
		t.Fatalf("unexpected victory: outcome=%+v character=%+v", outcome, store.character)
	}
	if stack := store.stacks["mist-hide:common"]; stack == nil || stack.Quantity != 2 {
		t.Fatalf("unexpected reward stack: %+v", stack)
	}
	if _, err := actEncounter(context.Background(), execution, map[string]any{"encounter_id": row.ID.String(), "move": "attack"}); !errors.Is(err, campaign.ErrActionRejected) {
		t.Fatalf("ended encounter should reject replay, got %v", err)
	}
	if store.stacks["mist-hide:common"].Quantity != 2 {
		t.Fatal("replayed victory granted rewards twice")
	}

	otherStore, otherExecution := encounterFixture(t, mutate)
	otherExecution.ActionID = execution.ActionID
	otherRow := startFixtureEncounter(t, otherStore, otherExecution)
	otherOutcome, err := actEncounter(context.Background(), otherExecution, map[string]any{"encounter_id": otherRow.ID.String(), "move": "attack"})
	if err != nil {
		t.Fatal(err)
	}
	plainStore, plainExecution := encounterFixture(t, mutate)
	plainExecution.ActionID = execution.ActionID
	plainRow := startFixtureEncounter(t, plainStore, plainExecution)
	plainOutcome, err := actEncounter(context.Background(), plainExecution, map[string]any{"encounter_id": plainRow.ID.String(), "move": "attack"})
	if err != nil {
		t.Fatal(err)
	}
	if otherOutcome.Result["player_damage"] != plainOutcome.Result["player_damage"] {
		t.Fatalf("same seed, revision and action id must reproduce damage: %v != %v", otherOutcome.Result["player_damage"], plainOutcome.Result["player_damage"])
	}
}

func TestEncounterAttackUsesAuthoritativeCampaignRoll(t *testing.T) {
	store, execution := encounterFixture(t, nil)
	row := startFixtureEncounter(t, store, execution)
	execution.Roll = forcedCampaignRoll(false, "failure", 90, 65)
	outcome, err := actEncounter(context.Background(), execution, map[string]any{
		"encounter_id": row.ID.String(), "move": "attack",
	})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Result["player_damage"] != int64(0) || row.OpponentHealth != 60 || outcome.Roll == nil {
		t.Fatalf("authoritative roll did not decide attack: outcome=%+v encounter=%+v", outcome, row)
	}
}

func TestEncounterDefendFleeAndTerminalStates(t *testing.T) {
	t.Run("defend halves incoming damage and advances the round clock", func(t *testing.T) {
		store, execution := encounterFixture(t, nil)
		row := startFixtureEncounter(t, store, execution)
		outcome, err := actEncounter(context.Background(), execution, map[string]any{"encounter_id": row.ID.String(), "move": "defend"})
		if err != nil {
			t.Fatal(err)
		}
		if store.character.Health != 94 || outcome.GameTimeDelta != 5 || row.Round != 2 {
			t.Fatalf("unexpected defend resolution: outcome=%+v encounter=%+v character=%+v", outcome, row, store.character)
		}
	})

	t.Run("guaranteed flee resolves without retaliation", func(t *testing.T) {
		store, execution := encounterFixture(t, func(content storage.JSONMap) { content["flee_chance_bps"] = int64(10_000) })
		row := startFixtureEncounter(t, store, execution)
		outcome, err := fleeEncounter(context.Background(), execution, map[string]any{"encounter_id": row.ID.String()})
		if err != nil {
			t.Fatal(err)
		}
		if row.Status != "fled" || store.character.Health != 100 || outcome.GameTimeDelta != 5 {
			t.Fatalf("unexpected flee resolution: outcome=%+v encounter=%+v", outcome, row)
		}
	})

	t.Run("lethal retaliation marks campaign dead", func(t *testing.T) {
		store, execution := encounterFixture(t, func(content storage.JSONMap) { content["opponent_attack"] = int64(200) })
		row := startFixtureEncounter(t, store, execution)
		outcome, err := actEncounter(context.Background(), execution, map[string]any{"encounter_id": row.ID.String(), "move": "defend"})
		if err != nil {
			t.Fatal(err)
		}
		if row.Status != "lost" || outcome.Status == nil || *outcome.Status != storage.CampaignStatusDead {
			t.Fatalf("unexpected death resolution: outcome=%+v encounter=%+v", outcome, row)
		}
	})

	t.Run("round limit resolves without writing an invalid round", func(t *testing.T) {
		store, execution := encounterFixture(t, func(content storage.JSONMap) { content["max_rounds"] = int64(1) })
		row := startFixtureEncounter(t, store, execution)
		outcome, err := actEncounter(context.Background(), execution, map[string]any{"encounter_id": row.ID.String(), "move": "defend"})
		if err != nil {
			t.Fatal(err)
		}
		if row.Status != "lost" || row.Round != 1 || outcome.Status != nil {
			t.Fatalf("unexpected round-limit resolution: outcome=%+v encounter=%+v", outcome, row)
		}
	})

	t.Run("lifespan exhaustion closes the encounter and campaign", func(t *testing.T) {
		store, execution := encounterFixture(t, func(content storage.JSONMap) { content["round_minutes"] = int64(1) })
		store.character.AgeDays, store.character.LifespanDays = 9, 10
		execution.Campaign.GameTime = 1_439
		row := startFixtureEncounter(t, store, execution)
		outcome, err := fleeEncounter(context.Background(), execution, map[string]any{"encounter_id": row.ID.String()})
		if err != nil {
			t.Fatal(err)
		}
		if store.character.AgeDays != 10 || row.Status != "lost" || outcome.Status == nil || *outcome.Status != storage.CampaignStatusDead {
			t.Fatalf("unexpected lifespan resolution: outcome=%+v encounter=%+v character=%+v", outcome, row, store.character)
		}
	})
}
