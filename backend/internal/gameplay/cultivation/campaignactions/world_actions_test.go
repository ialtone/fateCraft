package campaignactions

import (
	"context"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/worldrules"
	"github.com/ialtone/fatecraft/internal/storage"
)

func TestScheduledWorldEventsAreDeterministicAndBoundedWithoutLoss(t *testing.T) {
	build := func() (*actionStoreStub, campaign.ActionExecution) {
		store := newActionStore()
		campaignID, versionID := uuid.New(), uuid.New()
		store.regions["county"] = &storage.CampaignRegionState{
			CampaignID: campaignID, RegionKey: "county", ResourceLevel: 5000, DangerLevel: 1000,
			Stability: 5000, FactionPower: storage.JSONMap{},
		}
		for index, key := range []string{"event-a", "event-b", "event-c"} {
			store.definitions[storage.DefinitionWorldEvent+":"+key] = testWorldEventDefinition(key, 0)
			id := uuid.MustParse([]string{
				"00000000-0000-0000-0000-000000000001",
				"00000000-0000-0000-0000-000000000002",
				"00000000-0000-0000-0000-000000000003",
			}[index])
			store.scheduled[id] = &storage.CampaignScheduledEvent{
				ID: id, CampaignID: campaignID, EventKind: storage.DefinitionWorldEvent,
				TemplateKey: key, DueGameTime: 10, Priority: 10, Status: "pending", Occurrence: 0,
			}
		}
		return store, campaign.ActionExecution{
			Campaign: storage.Campaign{ID: campaignID, ChallengeVersionID: versionID, GameTime: 10, RandomSeed: 778899},
			Store:    store, MaxDueEventsPerTurn: 2,
		}
	}

	firstStore, firstExecution := build()
	firstOutcome := storage.CampaignActionOutcome{Events: []storage.CampaignEventDraft{{EventType: "action.done", Visibility: storage.CampaignVisibilityPlayer, Payload: storage.JSONMap{}}}}
	if err := processDueScheduledEvents(firstExecution, &firstOutcome); err != nil {
		t.Fatal(err)
	}
	if len(firstOutcome.Events) != 3 || scheduledStatusCount(firstStore, "processed") != 2 || scheduledStatusCount(firstStore, "pending") != 1 {
		t.Fatalf("bounded processing lost or over-processed events: events=%d scheduled=%#v", len(firstOutcome.Events), firstStore.scheduled)
	}

	secondStore, secondExecution := build()
	secondOutcome := storage.CampaignActionOutcome{Events: []storage.CampaignEventDraft{{EventType: "action.done", Visibility: storage.CampaignVisibilityPlayer, Payload: storage.JSONMap{}}}}
	if err := processDueScheduledEvents(secondExecution, &secondOutcome); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(firstOutcome.Events[1].Payload, secondOutcome.Events[1].Payload) ||
		!reflect.DeepEqual(firstOutcome.Events[2].Payload, secondOutcome.Events[2].Payload) {
		t.Fatalf("same seed and sequence produced different events: %#v / %#v", firstOutcome.Events, secondOutcome.Events)
	}
	if firstStore.regions["county"].DangerLevel != secondStore.regions["county"].DangerLevel {
		t.Fatal("same seed produced different regional state")
	}

	firstExecution.MaxDueEventsPerTurn = 2
	next := storage.CampaignActionOutcome{Events: []storage.CampaignEventDraft{{EventType: "action.done", Visibility: storage.CampaignVisibilityPlayer, Payload: storage.JSONMap{}}}}
	if err := processDueScheduledEvents(firstExecution, &next); err != nil {
		t.Fatal(err)
	}
	if scheduledStatusCount(firstStore, "pending") != 0 || scheduledStatusCount(firstStore, "processed") != 3 {
		t.Fatal("pending event was not preserved for the next action")
	}
}

func TestTravelAndInvestigateWorkForRealmFreeCampaigns(t *testing.T) {
	store := newActionStore()
	campaignID, versionID := uuid.New(), uuid.New()
	store.definitions[storage.DefinitionLocation+":start"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionLocation, Key: "start", Name: "Start",
		Content: storage.JSONMap{"node_type": "location", "connections": []any{
			map[string]any{"location_key": "next", "travel_minutes": int64(30)},
		}},
	}
	store.definitions[storage.DefinitionLocation+":next"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionLocation, Key: "next", Name: "Next",
		Content: storage.JSONMap{"node_type": "location", "connections": []any{}},
	}
	execution := campaign.ActionExecution{
		Campaign: storage.Campaign{ID: campaignID, ChallengeVersionID: versionID, CurrentLocationKey: "start"},
		Store:    store,
	}
	if _, err := investigate(context.Background(), execution, map[string]any{}); err != nil {
		t.Fatalf("realm-free investigate failed: %v", err)
	}
	if !store.discovered["next"] || store.character != nil {
		t.Fatalf("investigate initialized cultivation character or did not discover route: %#v", store.discovered)
	}
	store.discovered["next"] = true
	outcome, err := travel(context.Background(), execution, map[string]any{"destination_key": "next"})
	if err != nil {
		t.Fatalf("realm-free travel failed: %v", err)
	}
	if outcome.CurrentLocationKey == nil || *outcome.CurrentLocationKey != "next" || store.character != nil {
		t.Fatalf("realm-free travel result = %#v, character = %#v", outcome, store.character)
	}
}

func TestRecurringWorldEventKeepsEveryOverdueOccurrence(t *testing.T) {
	store := newActionStore()
	campaignID, versionID := uuid.New(), uuid.New()
	store.regions["county"] = &storage.CampaignRegionState{CampaignID: campaignID, RegionKey: "county", ResourceLevel: 5000, Stability: 5000, FactionPower: storage.JSONMap{}}
	store.definitions[storage.DefinitionWorldEvent+":cycle"] = testWorldEventDefinition("cycle", 5)
	id := uuid.New()
	store.scheduled[id] = &storage.CampaignScheduledEvent{ID: id, CampaignID: campaignID, EventKind: storage.DefinitionWorldEvent, TemplateKey: "cycle", DueGameTime: 10, Status: "pending"}
	execution := campaign.ActionExecution{Campaign: storage.Campaign{ID: campaignID, ChallengeVersionID: versionID, GameTime: 100, RandomSeed: 9}, Store: store, MaxDueEventsPerTurn: 1}

	for occurrence := int64(0); occurrence < 3; occurrence++ {
		outcome := storage.CampaignActionOutcome{Events: []storage.CampaignEventDraft{{EventType: "action.done", Visibility: storage.CampaignVisibilityPlayer, Payload: storage.JSONMap{}}}}
		if err := processDueScheduledEvents(execution, &outcome); err != nil {
			t.Fatal(err)
		}
		pending := pendingScheduled(store)
		if pending == nil || pending.Occurrence != occurrence+1 || pending.DueGameTime != 10+(occurrence+1)*5 {
			t.Fatalf("recurrence skipped: %#v", pending)
		}
	}
}

func TestWorldEventAppliesRegionMarketAndPrivateNPCGoalAtomically(t *testing.T) {
	store := newActionStore()
	campaignID, versionID := uuid.New(), uuid.New()
	store.regions["county"] = &storage.CampaignRegionState{CampaignID: campaignID, RegionKey: "county", ResourceLevel: 5000, DangerLevel: 1000, Stability: 5000, FactionPower: storage.JSONMap{"sect": int64(100)}}
	store.markets["market"] = &storage.CampaignMarketState{CampaignID: campaignID, MarketKey: "market", PriceModifierBPS: 10000}
	store.npcs["keeper"] = &storage.CampaignNPC{CampaignID: campaignID, InstanceKey: "keeper", Goals: storage.JSONMap{"progress": int64(3)}}
	store.definitions[storage.DefinitionWorldEvent+":shift"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionWorldEvent, Key: "shift", Name: "局势变化", Content: storage.JSONMap{
			"region_key": "county", "initial_delay_minutes": int64(0), "priority": int64(1),
			"variants": []any{map[string]any{
				"key": "only", "weight": int64(1), "summary": "变化已经发生。", "effects": map[string]any{
					"danger_delta": int64(20), "faction_power_deltas": map[string]any{"sect": int64(30)},
					"market_price_modifier_deltas_bps": map[string]any{"market": int64(500)},
					"npc_goal_deltas":                  map[string]any{"keeper": map[string]any{"progress": int64(4)}},
				},
			}},
		},
	}
	scheduled := &storage.CampaignScheduledEvent{ID: uuid.New(), CampaignID: campaignID, EventKind: storage.DefinitionWorldEvent, TemplateKey: "shift", DueGameTime: 0, Status: "pending"}
	store.scheduled[scheduled.ID] = scheduled
	execution := campaign.ActionExecution{Campaign: storage.Campaign{ID: campaignID, ChallengeVersionID: versionID, RandomSeed: 1}, Store: store, MaxDueEventsPerTurn: 1}
	outcome := storage.CampaignActionOutcome{Events: []storage.CampaignEventDraft{{EventType: "action.done", Visibility: storage.CampaignVisibilityPlayer, Payload: storage.JSONMap{}}}}
	if err := processDueScheduledEvents(execution, &outcome); err != nil {
		t.Fatal(err)
	}
	if store.regions["county"].DangerLevel != 1020 || store.regions["county"].FactionPower["sect"] != int64(130) ||
		store.markets["market"].PriceModifierBPS != 10500 || store.npcs["keeper"].Goals["progress"] != int64(7) {
		t.Fatalf("world effects were not applied: region=%#v market=%#v npc=%#v", store.regions["county"], store.markets["market"], store.npcs["keeper"])
	}
}

func TestQuestSubmissionConsumesObjectivesGrantsRewardsAndCancelsDeadline(t *testing.T) {
	store := newActionStore()
	campaignID, versionID := uuid.New(), uuid.New()
	store.definitions[storage.DefinitionQuest+":delivery"] = testQuestDefinition("obligation")
	store.definitions[storage.DefinitionFaction+":guild"] = &storage.GameplayDefinition{Kind: storage.DefinitionFaction, Key: "guild", Content: storage.JSONMap{}}
	store.quests["delivery"] = &storage.CampaignQuest{CampaignID: campaignID, QuestKey: "delivery", QuestType: "obligation", Status: "offered", Progress: storage.JSONMap{}}
	store.stacks[stackKey("herb", "common")] = &storage.CampaignItemStack{CampaignID: campaignID, ItemKey: "herb", QualityKey: "common", Quantity: 3, Metadata: storage.JSONMap{}}
	store.factions["guild"] = &storage.CampaignFactionStanding{CampaignID: campaignID, FactionKey: "guild", Reputation: 0, PositionKey: "outsider", Permissions: storage.JSONMap{}}
	execution := campaign.ActionExecution{Campaign: storage.Campaign{ID: campaignID, ChallengeVersionID: versionID, CurrentLocationKey: "market", GameTime: 100}, Store: store, ActorType: "player"}

	if _, err := acceptQuest(context.Background(), execution, map[string]any{"quest_key": "delivery"}); err != nil {
		t.Fatal(err)
	}
	if store.quests["delivery"].Status != "active" || store.quests["delivery"].DueGameTime == nil || pendingScheduled(store) == nil {
		t.Fatalf("quest was not activated with deadline: %#v", store.quests["delivery"])
	}
	if _, err := submitQuest(context.Background(), execution, map[string]any{"quest_key": "delivery"}); err != nil {
		t.Fatal(err)
	}
	if store.quests["delivery"].Status != "completed" || store.stacks[stackKey("herb", "common")] != nil ||
		store.stacks[stackKey("stone", "common")].Quantity != 5 || store.factions["guild"].Reputation != 20 || scheduledStatusCount(store, "cancelled") != 1 {
		t.Fatalf("quest completion was not atomic: quest=%#v stacks=%#v faction=%#v scheduled=%#v", store.quests["delivery"], store.stacks, store.factions["guild"], store.scheduled)
	}
}

func TestObligationDeadlineTransitionsToFailed(t *testing.T) {
	store := newActionStore()
	campaignID, versionID := uuid.New(), uuid.New()
	store.definitions[storage.DefinitionQuest+":delivery"] = testQuestDefinition("obligation")
	accepted, due := int64(0), int64(10)
	store.quests["delivery"] = &storage.CampaignQuest{CampaignID: campaignID, QuestKey: "delivery", QuestType: "obligation", Status: "active", AcceptedAtGameTime: &accepted, DueGameTime: &due, Progress: storage.JSONMap{}}
	scheduled := &storage.CampaignScheduledEvent{ID: uuid.New(), CampaignID: campaignID, EventKind: "quest_deadline", TemplateKey: "delivery", DueGameTime: due, Priority: 1000, Status: "pending"}
	store.scheduled[scheduled.ID] = scheduled
	execution := campaign.ActionExecution{Campaign: storage.Campaign{ID: campaignID, ChallengeVersionID: versionID, GameTime: due}, Store: store, MaxDueEventsPerTurn: 1}
	outcome := storage.CampaignActionOutcome{Events: []storage.CampaignEventDraft{{EventType: "action.done", Visibility: storage.CampaignVisibilityPlayer, Payload: storage.JSONMap{}}}}
	if err := processDueScheduledEvents(execution, &outcome); err != nil {
		t.Fatal(err)
	}
	if store.quests["delivery"].Status != "failed" || store.quests["delivery"].ResolvedAtGameTime == nil {
		t.Fatalf("obligation did not fail at deadline: %#v", store.quests["delivery"])
	}
}

func TestQuestCompletionOffersConfiguredSuccessorExactlyOnce(t *testing.T) {
	store := newActionStore()
	campaignID, versionID := uuid.New(), uuid.New()
	root := testQuestDefinition("quest")
	root.Content["unlock_quest_keys"] = []any{"next-step"}
	store.definitions[storage.DefinitionQuest+":delivery"] = root
	store.definitions[storage.DefinitionQuest+":next-step"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionQuest, Key: "next-step", Name: "后续线索", Content: storage.JSONMap{
			"quest_type": "quest", "initial": false, "starting_status": "offered",
			"objectives": []any{map[string]any{"key": "arrive", "type": "location", "location_key": "market"}},
		},
	}
	store.definitions[storage.DefinitionFaction+":guild"] = &storage.GameplayDefinition{Kind: storage.DefinitionFaction, Key: "guild", Content: storage.JSONMap{}}
	store.quests["delivery"] = &storage.CampaignQuest{CampaignID: campaignID, QuestKey: "delivery", QuestType: "quest", Status: "active", Progress: storage.JSONMap{}}
	store.stacks[stackKey("herb", "common")] = &storage.CampaignItemStack{CampaignID: campaignID, ItemKey: "herb", QualityKey: "common", Quantity: 3, Metadata: storage.JSONMap{}}
	store.factions["guild"] = &storage.CampaignFactionStanding{CampaignID: campaignID, FactionKey: "guild", Permissions: storage.JSONMap{}}
	execution := campaign.ActionExecution{Campaign: storage.Campaign{ID: campaignID, ChallengeVersionID: versionID, CurrentLocationKey: "market", GameTime: 100}, Store: store}

	first, err := submitQuest(context.Background(), execution, map[string]any{"quest_key": "delivery"})
	if err != nil {
		t.Fatal(err)
	}
	if store.quests["next-step"] == nil || store.quests["next-step"].Status != "offered" || len(first.Events) != 2 {
		t.Fatalf("successor was not offered: quest=%#v events=%#v", store.quests["next-step"], first.Events)
	}
	events, keys, err := offerUnlockedQuests(execution, worldQuestFromDefinition(t, root))
	if err != nil || len(events) != 0 || len(keys) != 0 {
		t.Fatalf("successor was offered twice: events=%#v keys=%#v err=%v", events, keys, err)
	}
}

func TestEncounterVictoryQuestCompletesCampaignWithMilestoneAndAccountReward(t *testing.T) {
	store := newActionStore()
	campaignID, versionID, playerID := uuid.New(), uuid.New(), uuid.New()
	store.definitions[storage.DefinitionQuest+":seal-rift"] = &storage.GameplayDefinition{
		Kind: storage.DefinitionQuest, Key: "seal-rift", Name: "封合界隙", Content: storage.JSONMap{
			"quest_type": "quest", "initial": false, "starting_status": "offered",
			"objectives": []any{map[string]any{
				"key": "defeat-guardian", "type": "encounter_win", "encounter_key": "rift-guardian", "quantity": int64(1),
			}},
			"milestone_key": "rift-sealed", "campaign_outcome_key": "world-warden",
		},
	}
	store.quests["seal-rift"] = &storage.CampaignQuest{
		CampaignID: campaignID, QuestKey: "seal-rift", QuestType: "quest", Status: "active", Progress: storage.JSONMap{},
	}
	encounter := &storage.CampaignEncounter{
		ID: uuid.New(), CampaignID: campaignID, EncounterKey: "rift-guardian", Status: "won",
	}
	store.encounters[encounter.ID] = encounter
	execution := campaign.ActionExecution{Campaign: storage.Campaign{
		ID: campaignID, PlayerID: playerID, ChallengeVersionID: versionID, GameTime: 900,
	}, Store: store}
	outcome, err := submitQuest(context.Background(), execution, map[string]any{"quest_key": "seal-rift"})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Status == nil || *outcome.Status != storage.CampaignStatusCompleted ||
		outcome.OutcomeKey == nil || *outcome.OutcomeKey != "world-warden" || len(outcome.Events) != 3 {
		t.Fatalf("unexpected campaign completion: %+v", outcome)
	}
	if rewards, ok := outcome.Result["account_rewards"].([]storage.CampaignOutcomeReward); !ok || len(rewards) != 1 || rewards[0].Key != storage.PointsKey {
		t.Fatalf("account reward missing: %#v", outcome.Result["account_rewards"])
	}
}

func worldQuestFromDefinition(t *testing.T, definition *storage.GameplayDefinition) worldrules.Quest {
	t.Helper()
	quest, ok := worldrules.ParseQuest(map[string]any(definition.Content))
	if !ok {
		t.Fatal("test quest definition is invalid")
	}
	return quest
}

func testWorldEventDefinition(key string, repeat int64) *storage.GameplayDefinition {
	content := storage.JSONMap{
		"region_key": "county", "initial_delay_minutes": int64(0), "repeat_interval_minutes": repeat, "priority": int64(10),
		"variants": []any{
			map[string]any{"key": "rise", "weight": int64(1), "summary": "危险有所上升。", "effects": map[string]any{"danger_delta": int64(10)}},
			map[string]any{"key": "fall", "weight": int64(1), "summary": "危险有所下降。", "effects": map[string]any{"danger_delta": int64(-10)}},
		},
	}
	return &storage.GameplayDefinition{Kind: storage.DefinitionWorldEvent, Key: key, Name: key, Content: content}
}

func testQuestDefinition(questType string) *storage.GameplayDefinition {
	return &storage.GameplayDefinition{Kind: storage.DefinitionQuest, Key: "delivery", Name: "交付药材", Content: storage.JSONMap{
		"quest_type": questType, "initial": true, "starting_status": "offered", "deadline_minutes": int64(60),
		"accept_location_keys": []any{"market"},
		"objectives":           []any{map[string]any{"key": "herbs", "type": "item", "item_key": "herb", "quality_key": "common", "quantity": int64(3), "consume": true}},
		"rewards": []any{
			map[string]any{"type": "item", "item_key": "stone", "quality_key": "common", "quantity": int64(5)},
			map[string]any{"type": "faction_reputation", "faction_key": "guild", "amount": int64(20)},
		},
	}}
}

func scheduledStatusCount(store *actionStoreStub, status string) int {
	count := 0
	for _, row := range store.scheduled {
		if row.Status == status {
			count++
		}
	}
	return count
}

func pendingScheduled(store *actionStoreStub) *storage.CampaignScheduledEvent {
	for _, row := range store.scheduled {
		if row.Status == "pending" {
			return row
		}
	}
	return nil
}
