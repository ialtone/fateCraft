package campaignactions

import (
	"context"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldactions"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldrules"
	"github.com/ialtone/fatecraft/internal/storage"
)

func buildScheduleTest(repeat, end int64, budget int) (*actionStoreStub, campaign.ActionExecution) {
	store := newActionStore()
	id, version := uuid.New(), uuid.New()
	store.regions["county"] = &storage.CampaignRegionState{CampaignID: id, RegionKey: "county", ResourceLevel: 5000, DangerLevel: 1000, Stability: 5000, FactionPower: storage.JSONMap{}}
	store.definitions[storage.DefinitionWorldEvent+":cycle"] = testWorldEventDefinition("cycle", repeat)
	row := &storage.CampaignScheduledEvent{ID: uuid.New(), CampaignID: id, EventKind: storage.DefinitionWorldEvent, TemplateKey: "cycle", DueGameTime: 10, Status: "pending"}
	store.scheduled[row.ID] = row
	return store, campaign.ActionExecution{Campaign: storage.Campaign{ID: id, ChallengeVersionID: version, GameTime: end, RandomSeed: 9}, Store: store, MaxDueEventsPerTurn: budget}
}

func TestScheduledBudgetRejectsBeforeChangingWorldAndReturnsSafeSpan(t *testing.T) {
	store, execution := buildScheduleTest(5, 0, 2)
	outcome := storage.CampaignActionOutcome{GameTimeDelta: 100}
	err := worldactions.ProcessDueScheduledEvents(execution, &outcome)
	violation, ok := campaign.PublicActionViolation(err)
	if !ok || violation.Code != "world.time_span_exceeded" || err.(*campaign.ActionViolationError).Violation.Details["max_minutes"] != int64(19) {
		t.Fatalf("missing safe boundary: %+v, %v", violation, err)
	}
	if len(outcome.Events) != 0 || scheduledStatusCount(store, "processed") != 0 || store.regions["county"].DangerLevel != 1000 {
		t.Fatal("rejected action changed scheduled state")
	}
	outcome.GameTimeDelta = 19
	if err := worldactions.ProcessDueScheduledEvents(execution, &outcome); err != nil {
		t.Fatal(err)
	}
	if len(outcome.Events) != 2 || pendingScheduled(store).DueGameTime != 20 {
		t.Fatal("safe span lost an occurrence")
	}
	for _, row := range store.scheduled {
		if row.Status == "processed" && (row.ProcessedAtGameTime == nil || *row.ProcessedAtGameTime != row.DueGameTime) {
			t.Fatal("event recorded at action endpoint instead of deadline")
		}
	}
}

func TestRecurringWorldEventsMatchPartitionedTimeAdvances(t *testing.T) {
	for seed := int64(0); seed < 100; seed++ {
		bulk, bulkExecution := buildScheduleTest(5, 0, 32)
		split, splitExecution := buildScheduleTest(5, 0, 32)
		bulkExecution.Campaign.RandomSeed, splitExecution.Campaign.RandomSeed = seed, seed
		all := storage.CampaignActionOutcome{GameTimeDelta: 100}
		if err := worldactions.ProcessDueScheduledEvents(bulkExecution, &all); err != nil {
			t.Fatal(err)
		}
		combined := []storage.CampaignEventDraft{}
		for at := int64(0); at < 100; at += 5 {
			splitExecution.Campaign.GameTime = at
			part := storage.CampaignActionOutcome{GameTimeDelta: 5}
			if err := worldactions.ProcessDueScheduledEvents(splitExecution, &part); err != nil {
				t.Fatal(err)
			}
			combined = append(combined, part.Events...)
		}
		if len(all.Events) != 19 || !reflect.DeepEqual(all.Events, combined) || bulk.regions["county"].DangerLevel != split.regions["county"].DangerLevel {
			t.Fatalf("partition changed world for seed %d", seed)
		}
	}
}

func TestRecurringEventsRespectInterleavedQuestDeadlineAndBudget(t *testing.T) {
	store, execution := buildScheduleTest(5, 0, 4)
	store.definitions[storage.DefinitionQuest+":delivery"] = testQuestDefinition("obligation")
	due := int64(17)
	store.quests["delivery"] = &storage.CampaignQuest{CampaignID: execution.Campaign.ID, QuestKey: "delivery", QuestType: "obligation", Status: "active", DueGameTime: &due}
	row := &storage.CampaignScheduledEvent{ID: uuid.New(), CampaignID: execution.Campaign.ID, EventKind: "quest_deadline", TemplateKey: "delivery", DueGameTime: due, Status: "pending", Priority: 1000}
	store.scheduled[row.ID] = row
	outcome := storage.CampaignActionOutcome{GameTimeDelta: 20}
	if err := worldactions.ProcessDueScheduledEvents(execution, &outcome); err != nil {
		t.Fatal(err)
	}
	for i, at := range []int64{10, 15, 17, 20} {
		if outcome.Events[i].Payload["due_game_time"] != at {
			t.Fatalf("incorrect order: %+v", outcome.Events)
		}
	}
	if store.quests["delivery"].Status != "failed" || *store.quests["delivery"].ResolvedAtGameTime != 17 {
		t.Fatal("quest deadline skipped")
	}
}

func TestScheduledEventInvalidOrFullBudgetDoesNotSilentlySkip(t *testing.T) {
	for _, budget := range []int{0, -1} {
		_, execution := buildScheduleTest(0, 0, budget)
		if err := worldactions.CheckTimeAdvance(execution, 10, 0); err == nil {
			t.Fatal("invalid budget accepted")
		}
	}
	_, execution := buildScheduleTest(0, 0, 8)
	for _, minutes := range []int64{-1, 10} {
		if err := worldactions.CheckTimeAdvance(execution, minutes, 64); err == nil {
			t.Fatal("invalid span or full event budget accepted")
		}
	}
	execution.Campaign.GameTime = 1
	if err := worldactions.CheckTimeAdvance(execution, 9223372036854775807, 0); err == nil {
		t.Fatal("overflow accepted")
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
	if err := worldactions.ProcessDueScheduledEvents(execution, &outcome); err != nil {
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

	if _, err := worldactions.AcceptQuest(context.Background(), execution, map[string]any{"quest_key": "delivery"}); err != nil {
		t.Fatal(err)
	}
	if store.quests["delivery"].Status != "active" || store.quests["delivery"].DueGameTime == nil || pendingScheduled(store) == nil {
		t.Fatalf("quest was not activated with deadline: %#v", store.quests["delivery"])
	}
	if _, err := worldactions.SubmitQuest(context.Background(), execution, map[string]any{"quest_key": "delivery"}); err != nil {
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
	if err := worldactions.ProcessDueScheduledEvents(execution, &outcome); err != nil {
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

	first, err := worldactions.SubmitQuest(context.Background(), execution, map[string]any{"quest_key": "delivery"})
	if err != nil {
		t.Fatal(err)
	}
	if store.quests["next-step"] == nil || store.quests["next-step"].Status != "offered" || len(first.Events) != 2 {
		t.Fatalf("successor was not offered: quest=%#v events=%#v", store.quests["next-step"], first.Events)
	}
	events, keys, err := worldactions.OfferUnlockedQuests(execution, worldQuestFromDefinition(t, root))
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
	outcome, err := worldactions.SubmitQuest(context.Background(), execution, map[string]any{"quest_key": "seal-rift"})
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
