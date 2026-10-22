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
	outcomeID := uuid.New()
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
	if err := db.Exec(`INSERT INTO gameplay_definitions
        (id, challenge_version_id, kind, key, name, content)
        VALUES
        (?, ?, 'item', 'forged', 'Forged Material', '{"stackable":true}'::jsonb),
        (?, ?, 'item', 'proof', 'Proof Material', '{"stackable":true}'::jsonb)`,
		extractedItemID, versionID, valuedItemID, versionID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO challenge_outcome_rules
        (id, challenge_version_id, outcome_key, category, title, tone, verifier_key, verifier_config, reward_points)
        VALUES (?, ?, 'world_warden', 'success', 'World Warden', 'success', 'core.always', '{}'::jsonb, 1000)`, outcomeID, versionID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`UPDATE challenge_packs SET current_version_id=?,status='active' WHERE id=?`, versionID, packID).Error; err != nil {
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
	if err := db.Model(&Conversation{}).Where("id = ?", conversation.ID).
		Updates(map[string]any{"status": StatusDeleted, "deleted_at": now}).Error; err != nil {
		t.Fatal(err)
	}
	if conversations, err := repository.ListConversations(context.Background(), campaignID); err != nil || len(conversations) != 0 {
		t.Fatalf("deleted campaign conversation remained listed: %+v err=%v", conversations, err)
	}
	// Deleting the only conversation frees the campaign slot; the replacement
	// is a new row and keeps the deleted one available for explicit restore.
	secondConversation = &Conversation{
		ID: uuid.New(), PlayerID: playerID.String(), Title: "另一个对话",
		Status: StatusActive, ChallengeVersionID: versionID, ChallengeHash: hash,
		CampaignID: &campaignID, LLMRoute: LLMRouteServer, CreatedAt: now, UpdatedAt: now,
	}
	if err := repository.CreateConversation(
		context.Background(), playerID, secondConversation, JSONMap{}, 1,
	); err != nil {
		t.Fatalf("create replacement campaign conversation: %v", err)
	}
	if err := NewConversationRepo(db).TransitionLifecycle(
		context.Background(), conversation.ID, StatusActive,
		map[string]any{"deleted_at": nil, "archived_at": nil, "ended_at": nil, "updated_at": now},
	); !errors.Is(err, ErrCampaignHasConversation) {
		t.Fatalf("restore alongside active campaign conversation error = %v", err)
	}
	if err := db.Model(&Conversation{}).Where("id = ?", secondConversation.ID).
		Updates(map[string]any{"status": StatusDeleted, "deleted_at": now}).Error; err != nil {
		t.Fatal(err)
	}
	if err := NewConversationRepo(db).TransitionLifecycle(
		context.Background(), conversation.ID, StatusActive,
		map[string]any{"deleted_at": nil, "archived_at": nil, "ended_at": nil, "updated_at": now},
	); err != nil {
		t.Fatalf("restore deleted campaign conversation: %v", err)
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
		if result.Campaign.Revision != expectedRevision+1 || len(granted) != 1 {
			t.Fatalf("unexpected campaign outcome grant: result=%+v rewards=%+v", result, granted)
		}
		return granted
	}
	firstGrant := grantOutcome(1, "outcome-reward-a")
	secondGrant := grantOutcome(2, "outcome-reward-b")
	if firstGrant[0].Key != PointsKey || firstGrant[0].Quantity != 1000 || secondGrant[0].Balance != 1000 {
		t.Fatalf("duplicate campaign points: %+v / %+v", firstGrant, secondGrant)
	}
	var forgedStack, proofStack CampaignItemStack
	if err := db.First(&forgedStack, "campaign_id = ? AND item_key = ? AND quality_key = ?", campaignID, "forged", "common").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&proofStack, "campaign_id = ? AND item_key = ? AND quality_key = ?", campaignID, "proof", "common").Error; err != nil {
		t.Fatal(err)
	}
	if forgedStack.Quantity != 20 || proofStack.Quantity != 5 {
		t.Fatalf("fixed points changed local inventory: forged=%d proof=%d", forgedStack.Quantity, proofStack.Quantity)
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
	if len(reloadedRewards) != 1 || reloadedRewards[PointsKey].Quantity != 1000 {
		t.Fatalf("reloaded points: %+v", domain.OutcomeRewards)
	}
	var balance PlayerRewardBalance
	if err := db.First(&balance, "player_id = ? AND reward_key = ?", playerID, PointsKey).Error; err != nil {
		t.Fatal(err)
	}
	var ledgerCount int64
	if err := db.Model(&RewardLedgerEntry{}).
		Where("player_id = ? AND reward_key = ?", playerID, PointsKey).
		Count(&ledgerCount).Error; err != nil {
		t.Fatal(err)
	}
	if balance.Balance != 1000 || balance.GrantedTotal != 1000 || ledgerCount != 1 {
		t.Fatalf("unexpected account reward state: balance=%+v ledger_count=%d", balance, ledgerCount)
	}
	var outcomeLedgerCount int64
	if err := db.Model(&RewardLedgerEntry{}).
		Where("player_id = ? AND source_type = ? AND source_id = ?", playerID, "campaign_outcome", campaignID.String()).
		Count(&outcomeLedgerCount).Error; err != nil {
		t.Fatal(err)
	}
	if outcomeLedgerCount != 1 {
		t.Fatalf("campaign outcome ledger count = %d, want 1", outcomeLedgerCount)
	}
}

func TestPostgresPlayerCampaignActionWithoutConversation(t *testing.T) {
	db := testdb.Open(t, "campaign_no_conversation")
	playerID, packID, versionID, campaignID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	hash := strings.Repeat("0", 64)
	if err := db.Exec(`INSERT INTO users (id, username, display_name) VALUES (?, 'no-conversation-player', 'Player')`, playerID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO challenge_packs (id, slug, name) VALUES (?, 'no-conversation-pack', 'Pack')`, packID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO challenge_versions
		(id, challenge_pack_id, version, status, manifest, content_hash, published_at)
		VALUES (?, ?, '1.0.0', 'published', '{}'::jsonb, ?, CURRENT_TIMESTAMP)`, versionID, packID, hash).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`UPDATE challenge_packs SET current_version_id=?,status='active' WHERE id=?`, versionID, packID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO campaigns
		(id, player_id, title, challenge_version_id, challenge_hash, current_location_key, random_seed, llm_route)
		VALUES (?, ?, 'Before first conversation', ?, ?, 'world.start', 1, 'server')`, campaignID, playerID, versionID, hash).Error; err != nil {
		t.Fatal(err)
	}
	repository := NewCampaignRepository(db)
	called := false
	result, err := repository.CommitPlayerCampaignAction(context.Background(), CommitCampaignActionInput{
		CampaignID: campaignID, PlayerID: playerID, IdempotencyKey: "noop-before-start", ActionKey: "core.noop",
	}, "Noop", JSONMap{}, func(context.Context, *CampaignRepository, *RollHistoryRepo) (*CampaignActionResult, error) {
		called = true
		return &CampaignActionResult{}, nil
	})
	if err != nil || !called || result == nil {
		t.Fatalf("action without a conversation did not execute directly: result=%+v called=%t err=%v", result, called, err)
	}
	status, outcomeKey := CampaignStatusAbandoned, "abandoned"
	committed, err := repository.CommitAction(context.Background(), CommitCampaignActionInput{
		CampaignID: campaignID, PlayerID: playerID, ExpectedRevision: 0, IdempotencyKey: "abandon-before-start",
		ActionKey: "core.abandon", ActorType: "player",
	}, func(*CampaignActionStore, Campaign) (CampaignActionOutcome, error) {
		return CampaignActionOutcome{
			Events: []CampaignEventDraft{{
				EventType:  "campaign.abandoned",
				Visibility: CampaignVisibilityPlayer,
			}},
			Status:     &status,
			OutcomeKey: &outcomeKey,
		}, nil
	})
	if err != nil {
		t.Fatalf("abandon without a conversation: %v", err)
	}
	if committed.Campaign.Status != CampaignStatusAbandoned {
		t.Fatalf("campaign status = %q, want abandoned", committed.Campaign.Status)
	}
	var conversations int64
	if err := db.Model(&Conversation{}).Where("campaign_id = ?", campaignID).Count(&conversations).Error; err != nil {
		t.Fatal(err)
	}
	if conversations != 0 {
		t.Fatalf("abandon before first conversation created %d conversations", conversations)
	}
}
