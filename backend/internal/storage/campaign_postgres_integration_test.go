package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/testdb"
)

func TestPostgresCampaignRevisionIdempotencyAndInventoryConstraints(t *testing.T) {
	db := testdb.Open(t, "campaign_verify")

	playerID, packID, versionID, campaignID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	rewardID, extractedRewardID, valuedRewardID := uuid.New(), uuid.New(), uuid.New()
	outcomeID, rewardRuleID, extractedRuleID, valuedRuleID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	extractedItemID, valuedItemID := uuid.New(), uuid.New()
	hash := strings.Repeat("0", 64)
	if err := db.Exec(`INSERT INTO users (id, username, display_name) VALUES (?, 'test-player', 'Test Player')`, playerID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO challenge_packs (id, slug, name) VALUES (?, 'integration-campaign', 'Integration Campaign')`, packID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO challenge_versions
        (id, challenge_pack_id, version, status, manifest, content_hash, published_at)
        VALUES (?, ?, '1.0.0', 'published', '{}'::jsonb, ?, CURRENT_TIMESTAMP)`, versionID, packID, hash).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO reward_definitions
        (id, key, kind, name, description, unit, icon, stackable, consumable, featured, sort_order)
        VALUES (?, 'integration.world-seal', 'badge', 'World Seal', '', 'seal', 'ribbon', TRUE, FALSE, TRUE, 1)`, rewardID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO reward_definitions
        (id, key, kind, name, description, unit, icon, stackable, consumable, featured, sort_order)
        VALUES
        (?, 'integration.extracted', 'currency', 'Extracted', '', 'unit', 'sparkles', TRUE, TRUE, TRUE, 2),
        (?, 'integration.valued', 'currency', 'Valued', '', 'unit', 'diamond', TRUE, TRUE, TRUE, 3)`,
		extractedRewardID, valuedRewardID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO gameplay_definitions
        (id, challenge_version_id, kind, key, name, content)
        VALUES
        (?, ?, 'item', 'forged', 'Forged Material', '{"stackable":true}'::jsonb),
        (?, ?, 'item', 'proof', 'Proof Material', '{"stackable":true}'::jsonb)`,
		extractedItemID, versionID, valuedItemID, versionID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO challenge_outcome_rules
        (id, challenge_version_id, outcome_key, category, title, tone, verifier_key, verifier_config)
        VALUES (?, ?, 'world_warden', 'success', 'World Warden', 'success', 'core.always', '{}'::jsonb)`, outcomeID, versionID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO challenge_reward_rules
        (id, challenge_version_id, outcome_rule_id, reward_definition_id, calculator_key, calculator_config, public_snapshot)
        VALUES (?, ?, ?, ?, 'core.fixed', '{"amount":1}'::jsonb, '{"key":"integration.world-seal"}'::jsonb)`, rewardRuleID, versionID, outcomeID, rewardID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO challenge_reward_rules
        (id, challenge_version_id, outcome_rule_id, reward_definition_id, calculator_key, calculator_config, public_snapshot)
        VALUES
        (?, ?, ?, ?, 'campaign.extract_item_stack', '{"item_key":"forged","quality_key":"common","source_quantity":3,"reward_quantity":2,"maximum_reward_quantity":6,"consume":true}'::jsonb, '{"key":"integration.extracted","kind":"currency","name":"Published Extracted","unit":"unit","icon":"sparkles"}'::jsonb),
        (?, ?, ?, ?, 'campaign.extract_item_stack', '{"item_key":"proof","quality_key":"common","source_quantity":2,"reward_quantity":1,"maximum_reward_quantity":4,"consume":false}'::jsonb, '{"key":"integration.valued"}'::jsonb)`,
		extractedRuleID, versionID, outcomeID, extractedRewardID,
		valuedRuleID, versionID, outcomeID, valuedRewardID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO campaigns
        (id, player_id, title, challenge_version_id, challenge_hash, current_location_key, random_seed, llm_route)
        VALUES (?, ?, 'Integration', ?, ?, 'world.start', 1, 'server')`, campaignID, playerID, versionID, hash).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO campaign_item_stacks
        (campaign_id, item_key, quality_key, quantity, metadata)
        VALUES (?, 'forged', 'common', 20, '{}'::jsonb), (?, 'proof', 'common', 5, '{}'::jsonb)`,
		campaignID, campaignID).Error; err != nil {
		t.Fatal(err)
	}

	repository := NewCampaignRepository(db)
	now := time.Now()
	conversation := &Conversation{
		ID: uuid.New(), PlayerID: playerID.String(), Title: "会被存档名覆盖",
		Status: StatusActive, ChallengeVersionID: versionID, ChallengeHash: hash,
		CampaignID: &campaignID, LLMRoute: LLMRouteServer, CreatedAt: now, UpdatedAt: now,
	}
	if err := repository.CreateConversation(
		context.Background(), playerID, conversation, JSONMap{"scene": map[string]any{}}, 1,
	); err != nil {
		t.Fatalf("create campaign conversation: %v", err)
	}
	if conversation.Title != "Integration" {
		t.Fatalf("campaign conversation title = %q, want campaign title", conversation.Title)
	}
	secondConversation := &Conversation{
		ID: uuid.New(), PlayerID: playerID.String(), Title: "另一个对话",
		Status: StatusActive, ChallengeVersionID: versionID, ChallengeHash: hash,
		CampaignID: &campaignID, CreatedAt: now, UpdatedAt: now,
	}
	if err := repository.CreateConversation(
		context.Background(), playerID, secondConversation, JSONMap{}, 1,
	); !errors.Is(err, ErrCampaignHasConversation) {
		t.Fatalf("second campaign conversation error = %v", err)
	}
	var detailedLoggingEnabled bool
	if err := db.Raw("SELECT detailed_llm_logging_enabled FROM system_settings WHERE id = 1").Scan(&detailedLoggingEnabled).Error; err != nil {
		t.Fatal(err)
	}
	if detailedLoggingEnabled {
		t.Fatal("detailed LLM logging must default to false")
	}
	responseStatus := 200
	requestLog := &LLMRequestLog{
		ID: uuid.New(), ConversationID: &conversation.ID, MessageID: &conversation.ID,
		Model: "integration-model", BaseURL: "https://example.invalid/v1", Stream: true,
		RequestBody: JSONDoc(`{"model":"integration-model","messages":[]}`),
		RawResponse: "data: [DONE]", ResponseText: "{}", ResponseStatus: &responseStatus,
		FinishReason: "stop", ReceivedDone: true, Success: true, DurationMS: 12, CreatedAt: now,
	}
	requestLogs := NewLLMRequestLogRepo(db)
	if err := requestLogs.Create(context.Background(), requestLog); err != nil {
		t.Fatalf("create detailed LLM request log: %v", err)
	}
	storedLog, err := requestLogs.Get(context.Background(), requestLog.ID)
	if err != nil || storedLog.RawResponse != requestLog.RawResponse || !storedLog.ReceivedDone {
		t.Fatalf("stored detailed LLM request log = %+v, err = %v", storedLog, err)
	}
	listedLogs, _, _, err := requestLogs.List(context.Background(), LogQuery{Limit: 10, ConversationID: &conversation.ID})
	if err != nil || len(listedLogs) != 1 || len(listedLogs[0].RawResponse) != 0 {
		t.Fatalf("request log summaries = %+v, err = %v", listedLogs, err)
	}

	turns := NewTurnRepo(db)
	messages := NewMessageRepo(db)
	var sourceTurnIDs, sourceMessageIDs []uuid.UUID
	for index := 0; index < 9; index++ {
		turn, attempt, beginErr := turns.Begin(context.Background(), conversation.ID, "user_input")
		if beginErr != nil {
			t.Fatalf("begin memory source turn %d: %v", index+1, beginErr)
		}
		for _, role := range []string{RoleUser, RoleAssistant} {
			tokens := 500
			message := &ChatMessage{
				ID: uuid.New(), ConversationID: conversation.ID, Role: role, Content: fmt.Sprintf("turn-%d-%s", index+1, role),
				TokenCount: &tokens, TurnID: &turn.ID, AttemptID: &attempt.ID, CreatedAt: now, UpdatedAt: now,
			}
			if err := messages.CreateChat(context.Background(), message); err != nil {
				t.Fatalf("create memory source message: %v", err)
			}
			if index < 4 {
				sourceMessageIDs = append(sourceMessageIDs, message.ID)
			}
		}
		if err := turns.Seal(context.Background(), turn.ID, attempt.ID, true, "", ""); err != nil {
			t.Fatalf("seal memory source turn: %v", err)
		}
		if index < 4 {
			sourceTurnIDs = append(sourceTurnIDs, turn.ID)
		}
	}
	memoryRepo := NewMemoryRepo(db)
	planning, err := memoryRepo.PlanningState(context.Background(), conversation.ID, 4)
	if err != nil || planning.TargetUntil != 4 || planning.UncoveredTokens != 5000 {
		t.Fatalf("memory planning = %+v, err = %v", planning, err)
	}
	job, err := memoryRepo.Schedule(context.Background(), conversation.ID, planning)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := memoryRepo.Claim(context.Background(), "integration-worker-old", time.Minute)
	if err != nil || claimed.ID != job.ID || claimed.LeaseToken == nil {
		t.Fatalf("claimed memory job = %+v, err = %v", claimed, err)
	}
	oldLeaseToken := *claimed.LeaseToken
	if _, duplicateErr := memoryRepo.Claim(context.Background(), "integration-worker-duplicate", time.Minute); !errors.Is(duplicateErr, ErrNotFound) {
		t.Fatalf("active lease was claimed twice: %v", duplicateErr)
	}
	if err := db.Model(&MemoryJob{}).Where("id = ?", claimed.ID).
		Update("lease_expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	claimed, err = memoryRepo.Claim(context.Background(), "integration-worker-new", time.Minute)
	if err != nil || claimed.LeaseToken == nil || *claimed.LeaseToken == oldLeaseToken {
		t.Fatalf("expired lease was not fenced: %+v, err=%v", claimed, err)
	}
	sourceHash := strings.Repeat("1", 64)
	fingerprint := strings.Repeat("2", 64)
	if oldErr := memoryRepo.SetJobSource(context.Background(), claimed.ID, "integration-worker-old", oldLeaseToken, sourceHash, fingerprint); !errors.Is(oldErr, ErrNotFound) {
		t.Fatalf("old lease token updated fixed source: %v", oldErr)
	}
	if err := memoryRepo.SetJobSource(context.Background(), claimed.ID, "integration-worker-new", *claimed.LeaseToken, sourceHash, fingerprint); err != nil {
		t.Fatal(err)
	}
	memory := ConversationMemory{
		ID: uuid.New(), ConversationID: conversation.ID, SourceBranchEpoch: 0,
		CoveredFromTurnSeq: 1, CoveredUntilTurnSeq: 4, CoveredUntilMessageSeq: 8,
		SchemaVersion: 1, PromptVersion: "test", RendererVersion: "test",
		Content:      JSONDoc(`{"synopsis":"test","facts":[],"entities":[],"goals":[],"open_threads":[],"player_choices":[]}`),
		RenderedText: "test", SourceHash: sourceHash, GenerationFingerprint: fingerprint,
		Model: "integration-model", TokenCount: 1, TokenizerVersion: "heuristic-v1", CreatedAt: now,
	}
	if err := memoryRepo.Commit(context.Background(), MemoryCommitInput{
		JobID: claimed.ID, WorkerID: "integration-worker-new", LeaseToken: *claimed.LeaseToken,
		Memory: memory, TurnIDs: sourceTurnIDs, MessageIDs: sourceMessageIDs,
	}); err != nil {
		t.Fatalf("commit memory: %v", err)
	}
	latestMemory, err := memoryRepo.Latest(context.Background(), conversation.ID)
	if err != nil || latestMemory.ID != memory.ID || latestMemory.Version != 1 {
		t.Fatalf("latest memory = %+v, err = %v", latestMemory, err)
	}

	inquiries := NewInquiryRepo(db)
	thread, err := inquiries.Thread(context.Background(), conversation.ID, playerID)
	if err != nil {
		t.Fatalf("create inquiry thread: %v", err)
	}
	existingThread, err := inquiries.Thread(context.Background(), conversation.ID, playerID)
	if err != nil {
		t.Fatalf("get existing inquiry thread: %v", err)
	}
	if existingThread.ID != thread.ID {
		t.Fatalf("existing inquiry thread id = %s, want %s", existingThread.ID, thread.ID)
	}
	snapshotID := uuid.New()
	inquiryMessage := &CharacterInquiryMessage{
		ID: uuid.New(), ThreadID: thread.ID, ConversationID: conversation.ID, PlayerID: playerID,
		Role: RoleUser, Content: "当前状态如何？", SnapshotID: &snapshotID,
		SnapshotBranchEpoch: conversation.BranchEpoch, SnapshotRevision: 9,
		SourceRefs: JSONDoc("[]"), Status: "completed", CreatedAt: time.Now(),
	}
	if err := inquiries.Append(context.Background(), inquiryMessage); err != nil {
		t.Fatalf("append inquiry: %v", err)
	}
	for index := 0; index < 12; index++ {
		allowed, limitErr := inquiries.ConsumeRateLimit(context.Background(), playerID, now, time.Minute, 12)
		if limitErr != nil || !allowed {
			t.Fatalf("rate limit request %d allowed=%t err=%v", index+1, allowed, limitErr)
		}
	}
	allowed, err := inquiries.ConsumeRateLimit(context.Background(), playerID, now, time.Minute, 12)
	if err != nil || allowed {
		t.Fatalf("thirteenth inquiry rate limit allowed=%t err=%v", allowed, err)
	}
	inquiryRows, err := inquiries.List(context.Background(), thread.ID, conversation.ID, playerID, 10)
	if err != nil || len(inquiryRows) != 1 || inquiryRows[0].SnapshotBranchEpoch != conversation.BranchEpoch {
		t.Fatalf("inquiry rows = %+v, err=%v", inquiryRows, err)
	}

	type attempt struct {
		key    string
		result *CampaignActionResult
		err    error
	}
	attempts := make(chan attempt, 2)
	var start sync.WaitGroup
	start.Add(1)
	for _, key := range []string{"parallel-a", "parallel-b"} {
		key := key
		go func() {
			start.Wait()
			result, err := repository.CommitAction(context.Background(), CommitCampaignActionInput{
				CampaignID: campaignID, PlayerID: playerID, ExpectedRevision: 0,
				IdempotencyKey: key, ActionKey: "test.advance", ActorType: "system",
			}, func(store *CampaignActionStore, campaign Campaign) (CampaignActionOutcome, error) {
				domain, domainErr := store.DomainState(context.Background(), campaign)
				if domainErr != nil || domain == nil || domain.Definitions == nil {
					return CampaignActionOutcome{}, fmt.Errorf("load locked domain state: %w", domainErr)
				}
				return CampaignActionOutcome{
					Events: []CampaignEventDraft{{EventType: "test.advanced", Visibility: CampaignVisibilityPlayer}},
					Result: JSONMap{"key": key},
					Roll: &CampaignRollSnapshot{
						RollKey: "test.roll", Title: "Integration Roll", Category: "exploration", Success: true,
						Content: "roll committed", Markdown: "roll committed", Metadata: JSONMap{"source": "integration"},
					},
				}, nil
			})
			attempts <- attempt{key: key, result: result, err: err}
		}()
	}
	start.Done()
	var winner attempt
	conflicts := 0
	for range 2 {
		item := <-attempts
		if item.err == nil {
			winner = item
		} else if errors.Is(item.err, ErrCampaignRevisionConflict) {
			conflicts++
		} else {
			t.Fatalf("unexpected concurrent commit error: %v", item.err)
		}
	}
	if winner.result == nil || winner.result.RollMessage == nil || conflicts != 1 || winner.result.Campaign.Revision != 1 {
		t.Fatalf("unexpected concurrent result: winner=%+v conflicts=%d", winner, conflicts)
	}
	replayed, err := repository.CommitAction(context.Background(), CommitCampaignActionInput{
		CampaignID: campaignID, PlayerID: playerID, ExpectedRevision: 0,
		IdempotencyKey: winner.key, ActionKey: "test.advance", ActorType: "system",
	}, func(*CampaignActionStore, Campaign) (CampaignActionOutcome, error) {
		t.Fatal("idempotent replay executed handler")
		return CampaignActionOutcome{}, nil
	})
	if err != nil || !replayed.Replayed || replayed.Campaign.Revision != 1 || replayed.RollMessage == nil || replayed.RollMessage.ID != winner.result.RollMessage.ID {
		t.Fatalf("idempotent replay failed: result=%+v err=%v", replayed, err)
	}
	for name, model := range map[string]any{
		"history": &RollHistoryEntry{}, "chat": &ChatMessage{}, "display": &DisplayMessage{},
	} {
		var count int64
		if err := db.Model(model).Where("conversation_id = ?", conversation.ID).Count(&count).Error; err != nil || count == 0 {
			t.Fatalf("campaign roll %s count=%d err=%v", name, count, err)
		}
	}
	if err := db.Create(&CampaignItemStack{
		CampaignID: campaignID, ItemKey: "forged", QualityKey: "common", Quantity: -1, Metadata: JSONMap{},
	}).Error; err == nil {
		t.Fatal("PostgreSQL accepted negative inventory")
	}

	grantOutcome := func(expectedRevision int64, actionID string) []CampaignOutcomeReward {
		var granted []CampaignOutcomeReward
		result, err := repository.CommitAction(context.Background(), CommitCampaignActionInput{
			CampaignID: campaignID, PlayerID: playerID, ExpectedRevision: expectedRevision,
			IdempotencyKey: actionID, ActionKey: "test.grant-outcome", ActorType: "system",
		}, func(store *CampaignActionStore, campaign Campaign) (CampaignActionOutcome, error) {
			var grantErr error
			granted, grantErr = store.GrantCampaignOutcomeRewards(context.Background(), campaign, "world_warden")
			return CampaignActionOutcome{
				Events: []CampaignEventDraft{{
					EventType:  "test.outcome-reward-granted",
					Visibility: CampaignVisibilityPlayer,
				}},
			}, grantErr
		})
		if err != nil {
			t.Fatalf("grant campaign outcome reward: %v", err)
		}
		if result.Campaign.Revision != expectedRevision+1 || len(granted) != 3 {
			t.Fatalf("unexpected campaign outcome grant: result=%+v rewards=%+v", result, granted)
		}
		return granted
	}
	firstGrant := grantOutcome(1, "outcome-reward-a")
	if err := db.Model(&RewardDefinition{}).Where("id = ?", extractedRewardID).Update("name", "Renamed Extracted").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&GameplayDefinition{}).Where("id = ?", extractedItemID).Update("name", "Renamed Material").Error; err != nil {
		t.Fatal(err)
	}
	secondGrant := grantOutcome(2, "outcome-reward-b")
	firstByKey, secondByKey := map[string]CampaignOutcomeReward{}, map[string]CampaignOutcomeReward{}
	for _, reward := range firstGrant {
		firstByKey[reward.Key] = reward
	}
	for _, reward := range secondGrant {
		secondByKey[reward.Key] = reward
	}
	if firstByKey["integration.world-seal"].Balance != 1 || secondByKey["integration.world-seal"].Balance != 1 ||
		firstByKey["integration.extracted"].Quantity != 6 || secondByKey["integration.extracted"].Balance != 6 ||
		firstByKey["integration.valued"].Quantity != 2 || secondByKey["integration.valued"].Balance != 2 ||
		firstByKey["integration.extracted"].Name != "Published Extracted" || secondByKey["integration.extracted"].Name != "Published Extracted" {
		t.Fatalf("campaign outcome reward was not idempotent: first=%+v second=%+v", firstGrant, secondGrant)
	}
	if source := firstByKey["integration.extracted"].Source; source == nil || source.AvailableQuantity != 20 || source.ConsumedQuantity != 9 {
		t.Fatalf("consumed extraction source = %+v", source)
	}
	if source := secondByKey["integration.extracted"].Source; source == nil || source.Name != "Forged Material" {
		t.Fatalf("replayed extraction source snapshot = %+v", source)
	}
	if source := firstByKey["integration.valued"].Source; source == nil || source.AvailableQuantity != 5 || source.ConsumedQuantity != 0 {
		t.Fatalf("non-consuming valuation source = %+v", source)
	}
	var forgedStack, proofStack CampaignItemStack
	if err := db.First(&forgedStack, "campaign_id = ? AND item_key = ? AND quality_key = ?", campaignID, "forged", "common").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&proofStack, "campaign_id = ? AND item_key = ? AND quality_key = ?", campaignID, "proof", "common").Error; err != nil {
		t.Fatal(err)
	}
	if forgedStack.Quantity != 11 || proofStack.Quantity != 5 {
		t.Fatalf("campaign extraction was applied more than once: forged=%d proof=%d", forgedStack.Quantity, proofStack.Quantity)
	}
	if err := db.Model(&Campaign{}).Where("id = ?", campaignID).Updates(map[string]any{
		"status": CampaignStatusCompleted, "outcome_key": "world_warden", "completed_at_game_time": int64(0),
	}).Error; err != nil {
		t.Fatal(err)
	}
	domain, err := repository.DomainState(context.Background(), campaignID)
	if err != nil {
		t.Fatal(err)
	}
	reloadedRewards := map[string]CampaignOutcomeReward{}
	for _, reward := range domain.OutcomeRewards {
		reloadedRewards[reward.Key] = reward
	}
	if len(reloadedRewards) != 3 || reloadedRewards["integration.extracted"].Name != "Published Extracted" ||
		reloadedRewards["integration.extracted"].Source == nil || reloadedRewards["integration.extracted"].Source.Name != "Forged Material" {
		t.Fatalf("reloaded campaign outcome rewards = %+v", domain.OutcomeRewards)
	}
	var balance PlayerRewardBalance
	if err := db.First(&balance, "player_id = ? AND reward_definition_id = ?", playerID, rewardID).Error; err != nil {
		t.Fatal(err)
	}
	var ledgerCount int64
	if err := db.Model(&RewardLedgerEntry{}).
		Where("player_id = ? AND reward_definition_id = ?", playerID, rewardID).
		Count(&ledgerCount).Error; err != nil {
		t.Fatal(err)
	}
	if balance.Balance != 1 || balance.GrantedTotal != 1 || ledgerCount != 1 {
		t.Fatalf("unexpected account reward state: balance=%+v ledger_count=%d", balance, ledgerCount)
	}
	var outcomeLedgerCount int64
	if err := db.Model(&RewardLedgerEntry{}).
		Where("player_id = ? AND source_type = ? AND source_id = ?", playerID, "campaign_outcome", campaignID.String()).
		Count(&outcomeLedgerCount).Error; err != nil {
		t.Fatal(err)
	}
	if outcomeLedgerCount != 3 {
		t.Fatalf("campaign outcome ledger count = %d, want 3", outcomeLedgerCount)
	}
}
