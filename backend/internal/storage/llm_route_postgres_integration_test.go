package storage

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// seedLLMRouteTestGraph 说明：本文件的种子直接内联在各测试中，
// 复用 openGenerationTestDB 建表（执行最新 Goose migrations）。
func TestPostgresLLMRouteImmutableAndForeignKey(t *testing.T) {
	db := openGenerationTestDB(t)
	playerID, packID, versionID := uuid.New(), uuid.New(), uuid.New()
	campaignID, conversationID := uuid.New(), uuid.New()
	hash := strings.Repeat("0", 64)
	execAll := func(statements ...string) {
		t.Helper()
		for _, statement := range statements {
			if err := db.Exec(statement).Error; err != nil {
				t.Fatalf("seed llm route test: %v", err)
			}
		}
	}
	execAll(
		`INSERT INTO users (id, username, display_name) VALUES ('`+playerID.String()+`', 'route-test', 'Route Test')`,
		`INSERT INTO challenge_packs (id, slug, name) VALUES ('`+packID.String()+`', 'route-test', 'Route Test')`,
		`INSERT INTO challenge_versions (id, challenge_pack_id, version, status, manifest, content_hash, published_at) VALUES ('`+versionID.String()+`', '`+packID.String()+`', '1.0.0', 'published', '{}'::jsonb, '`+hash+`', CURRENT_TIMESTAMP)`,
		`UPDATE challenge_packs SET current_version_id = '`+versionID.String()+`', status = 'active' WHERE id = '`+packID.String()+`'`,
		`INSERT INTO campaigns (id, player_id, title, challenge_version_id, challenge_hash, current_location_key, random_seed, llm_route) VALUES ('`+campaignID.String()+`', '`+playerID.String()+`', 'Route', '`+versionID.String()+`', '`+hash+`', 'world.start', 1, 'server')`,
		`INSERT INTO conversations (id, player_id, title, challenge_version_id, challenge_hash, campaign_id, llm_route) VALUES ('`+conversationID.String()+`', '`+playerID.String()+`', 'Route', '`+versionID.String()+`', '`+hash+`', '`+campaignID.String()+`', 'server')`,
	)

	// 触发器：对话与 Campaign 的 llm_route 不可修改。
	if err := db.Exec(`UPDATE conversations SET llm_route = 'client' WHERE id = '` + conversationID.String() + `'`).Error; err == nil {
		t.Fatal("conversation llm_route update was not rejected")
	}
	if err := db.Exec(`UPDATE campaigns SET llm_route = 'client' WHERE id = '` + campaignID.String() + `'`).Error; err == nil {
		t.Fatal("campaign llm_route update was not rejected")
	}
	// 其他字段仍可更新。
	if err := db.Exec(`UPDATE conversations SET title = 'Renamed' WHERE id = '` + conversationID.String() + `'`).Error; err != nil {
		t.Fatalf("unrelated conversation update failed: %v", err)
	}

	// 复合外键：对话路由必须等于所属 Campaign 路由。
	mismatchedID := uuid.New()
	if err := db.Exec(`INSERT INTO conversations (id, player_id, title, challenge_version_id, challenge_hash, campaign_id, llm_route) VALUES ('` + mismatchedID.String() + `', '` + playerID.String() + `', 'Mismatch', '` + versionID.String() + `', '` + hash + `', '` + campaignID.String() + `', 'client')`).Error; err == nil {
		t.Fatal("conversation with mismatched llm_route was not rejected by foreign key")
	}

	// 路由一致时写入成功。
	matchedID := uuid.New()
	otherCampaignID := uuid.New()
	execAll(
		`INSERT INTO campaigns (id, player_id, title, challenge_version_id, challenge_hash, current_location_key, random_seed, llm_route) VALUES ('`+otherCampaignID.String()+`', '`+playerID.String()+`', 'Route Client', '`+versionID.String()+`', '`+hash+`', 'world.start', 1, 'client')`,
		`INSERT INTO conversations (id, player_id, title, challenge_version_id, challenge_hash, campaign_id, llm_route) VALUES ('`+matchedID.String()+`', '`+playerID.String()+`', 'Matched', '`+versionID.String()+`', '`+hash+`', '`+otherCampaignID.String()+`', 'client')`,
	)
	var count int64
	if err := db.Model(&Conversation{}).Where("id = ?", matchedID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("matched conversation was not inserted: count=%d err=%v", count, err)
	}
}

func TestPostgresReleaseClaimKeepsJobPending(t *testing.T) {
	db := openGenerationTestDB(t)
	ctx := context.Background()
	conversationID := seedGenerationTestConversation(t, db)
	jobID := uuid.New()
	// available_at 显式写成库时钟的过去时刻：种子行若用 DEFAULT CURRENT_TIMESTAMP，
	// 在应用与数据库有时钟偏差时会落在应用时钟的未来，导致 Claim 扫不到（flaky）。
	if err := db.Exec(`INSERT INTO memory_jobs (id, conversation_id, source_branch_epoch, target_until_turn_seq, status, max_executions, available_at) VALUES (?, ?, 0, 1, 'pending', 4, CURRENT_TIMESTAMP - INTERVAL '1 minute')`, jobID, conversationID).Error; err != nil {
		t.Fatalf("seed memory job: %v", err)
	}
	repo := NewMemoryRepo(db)
	claimed, err := repo.Claim(ctx, "worker-1", time.Minute)
	if err != nil {
		t.Fatalf("claim memory job: %v", err)
	}
	if claimed.ExecutionCount != 1 {
		t.Fatalf("claimed execution count = %d, want 1", claimed.ExecutionCount)
	}
	if claimed.LeaseToken == nil {
		t.Fatal("claimed job has no lease token")
	}
	if err := repo.ReleaseClaim(ctx, jobID, "worker-1", *claimed.LeaseToken, 30*time.Second); err != nil {
		t.Fatalf("release claim: %v", err)
	}
	var job MemoryJob
	if err := db.First(&job, "id = ?", jobID).Error; err != nil {
		t.Fatal(err)
	}
	if job.Status != MemoryJobPending {
		t.Fatalf("released job status = %q, want pending", job.Status)
	}
	if job.ExecutionCount != 0 {
		t.Fatalf("released job execution count = %d, want 0 (not counted)", job.ExecutionCount)
	}
	if time.Until(job.AvailableAt) <= 0 {
		t.Fatalf("released job available_at = %v, want future", job.AvailableAt)
	}
}

func TestPostgresClientRouteSettlementSkipsRewards(t *testing.T) {
	db := openGenerationTestDB(t)
	ctx := context.Background()
	playerID, packID, versionID, outcomeRuleID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	hash := strings.Repeat("0", 64)
	for _, statement := range []string{
		`INSERT INTO users (id, username, display_name) VALUES ('` + playerID.String() + `', 'settle-route-test', 'Settle Route Test')`,
		`INSERT INTO challenge_packs (id, slug, name) VALUES ('` + packID.String() + `', 'settle-route-test', 'Settle Route Test')`,
		`INSERT INTO challenge_versions (id, challenge_pack_id, version, status, manifest, content_hash, published_at) VALUES ('` + versionID.String() + `', '` + packID.String() + `', '1.0.0', 'published', '{}'::jsonb, '` + hash + `', CURRENT_TIMESTAMP)`,
		`UPDATE challenge_packs SET current_version_id = '` + versionID.String() + `', status = 'active' WHERE id = '` + packID.String() + `'`,
		`INSERT INTO challenge_outcome_rules (id, challenge_version_id, outcome_key, category, title, verifier_key, verifier_config) VALUES ('` + outcomeRuleID.String() + `', '` + versionID.String() + `', 'good_end', 'success', 'Good End', 'none', '{}'::jsonb)`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("seed settlement route test: %v", err)
		}
	}
	newConversation := func(route string) uuid.UUID {
		t.Helper()
		convID := uuid.New()
		if err := db.Exec(`INSERT INTO conversations (id, player_id, title, challenge_version_id, challenge_hash, llm_route) VALUES (?, ?, 'Route', ?, ?, ?)`,
			convID, playerID, versionID, hash, route).Error; err != nil {
			t.Fatalf("seed conversation: %v", err)
		}
		return convID
	}
	settle := func(convID uuid.UUID, route string) {
		t.Helper()
		repo := NewChallengeRepository(db)
		conv, err := repo.LockConversation(ctx, convID)
		if err != nil {
			t.Fatalf("lock conversation: %v", err)
		}
		run := &ChallengeRun{
			ID: uuid.New(), ConversationID: convID, PlayerID: playerID,
			ChallengeVersionID: versionID, OutcomeRuleID: outcomeRuleID, OutcomeKey: "good_end",
			Category: "success", TitleSnapshot: "Good End", Summary: "done",
			SettlementDetail: JSONMap{}, SettledAt: time.Now(),
		}
		points := int64(5)
		if _, err := repo.CompleteRun(ctx, conv, run, points); err != nil {
			t.Fatalf("complete run (%s): %v", route, err)
		}
		updated, err := repo.LockConversation(ctx, convID)
		if err != nil || updated.Status != StatusEnded {
			t.Fatalf("conversation not ended (%s): %+v err=%v", route, updated, err)
		}
	}
	ledgerCount := func() int64 {
		t.Helper()
		var count int64
		if err := db.Model(&RewardLedgerEntry{}).Where("player_id = ?", playerID).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		return count
	}
	balanceOf := func() int64 {
		t.Helper()
		var balance PlayerRewardBalance
		if err := db.First(&balance, "player_id = ? AND reward_key = ?", playerID, PointsKey).Error; err != nil {
			return -1
		}
		return balance.Balance
	}

	// client 路由：对话照常结束，但账本与余额无变化。
	settle(newConversation(LLMRouteClient), LLMRouteClient)
	if count := ledgerCount(); count != 0 {
		t.Fatalf("client settlement wrote %d ledger entries, want 0", count)
	}
	if balance := balanceOf(); balance != -1 {
		t.Fatalf("client settlement balance = %d, want no row", balance)
	}

	// server 路由：行为不变，正常发放。
	settle(newConversation(LLMRouteServer), LLMRouteServer)
	if count := ledgerCount(); count != 1 {
		t.Fatalf("server settlement ledger entries = %d, want 1", count)
	}
	if balance := balanceOf(); balance != 5 {
		t.Fatalf("server settlement balance = %d, want 5", balance)
	}
}

func TestPostgresClientRouteRegenerateSkipsCharge(t *testing.T) {
	db := openGenerationTestDB(t)
	ctx := context.Background()
	playerID, packID, versionID := uuid.New(), uuid.New(), uuid.New()
	// 两项余额身份由平台固定，测试按实际授予创建余额。
	hash := strings.Repeat("0", 64)
	for _, statement := range []string{
		`INSERT INTO users (id, username, display_name) VALUES ('` + playerID.String() + `', 'regen-route-test', 'Regen Route Test')`,
		`INSERT INTO challenge_packs (id, slug, name) VALUES ('` + packID.String() + `', 'regen-route-test', 'Regen Route Test')`,
		`INSERT INTO challenge_versions (id, challenge_pack_id, version, status, manifest, content_hash, published_at) VALUES ('` + versionID.String() + `', '` + packID.String() + `', '1.0.0', 'published', '{}'::jsonb, '` + hash + `', CURRENT_TIMESTAMP)`,
		`UPDATE challenge_packs SET current_version_id = '` + versionID.String() + `', status = 'active' WHERE id = '` + packID.String() + `'`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("seed regenerate route test: %v", err)
		}
	}
	// 给玩家 10 个改写额度，server 对照组应能扣费。
	rewards := NewRewardRepository(db)
	if err := rewards.WithTx(ctx, func(r *RewardRepository) error {
		_, err := r.GrantInCurrentTx(ctx, RewardMutation{
			PlayerID: playerID, RewardKey: "fatecraft.message-rewrite-credit", Quantity: 10,
			SourceType: "test", SourceID: "seed", IdempotencyKey: "seed:" + playerID.String(),
		})
		return err
	}); err != nil {
		t.Fatalf("seed rewrite balance: %v", err)
	}
	newConversation := func(route string) uuid.UUID {
		t.Helper()
		convID, turnID, triggerID, runID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
		for _, statement := range []string{
			`INSERT INTO conversations (id, player_id, title, challenge_version_id, challenge_hash, llm_route) VALUES ('` + convID.String() + `', '` + playerID.String() + `', 'Regen', '` + versionID.String() + `', '` + hash + `', '` + route + `')`,
			`INSERT INTO conversation_turns (id, conversation_id, turn_seq, trigger_type, status) VALUES ('` + turnID.String() + `', '` + convID.String() + `', 1, 'user_input', 'completed')`,
			`INSERT INTO chat_messages (id, conversation_id, role, content, turn_id, message_seq) VALUES ('` + triggerID.String() + `', '` + convID.String() + `', 'user', 'hi', '` + turnID.String() + `', 1)`,
			`INSERT INTO conversation_generation_runs (id, conversation_id, request_id, operation_type, payload_hash, conversation_revision, status, lease_expires_at) VALUES ('` + runID.String() + `', '` + convID.String() + `', '` + uuid.NewString() + `', 'user_input', '` + hash + `', 0, 'running', CURRENT_TIMESTAMP + INTERVAL '10 minutes')`,
			// 已占用 turn_seq=1 / message_seq=1，新回合与新消息从 2 开始分配，否则违反唯一约束。
			`UPDATE conversations SET next_turn_seq = 2, next_message_seq = 2 WHERE id = '` + convID.String() + `'`,
		} {
			if err := db.Exec(statement).Error; err != nil {
				t.Fatalf("seed regenerate conversation: %v", err)
			}
		}
		return convID
	}
	regenerate := func(convID uuid.UUID, key string) *RegeneratePreparation {
		t.Helper()
		preparation, err := NewTurnRepo(db).BeginRegenerate(ctx, RegenerateInput{
			ConversationID: convID, PlayerID: playerID,
			IdempotencyKey: key, InitialState: JSONMap{},
		})
		if err != nil {
			t.Fatalf("begin regenerate: %v", err)
		}
		return preparation
	}
	consumeCount := func() int64 {
		t.Helper()
		var count int64
		if err := db.Model(&RewardLedgerEntry{}).
			Where("player_id = ? AND source_type = ?", playerID, "message_regenerate").
			Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		return count
	}

	// client 路由：不扣费。
	clientPreparation := regenerate(newConversation(LLMRouteClient), "regen:client:1")
	if clientPreparation.RewardConsumed {
		t.Fatal("client regenerate consumed rewrite credit, want no charge")
	}
	if count := consumeCount(); count != 0 {
		t.Fatalf("client regenerate wrote %d consume entries, want 0", count)
	}

	// server 路由：行为不变，正常扣费。
	serverConversation := newConversation(LLMRouteServer)
	serverPreparation := regenerate(serverConversation, "regen:server:1")
	if !serverPreparation.RewardConsumed {
		t.Fatal("server regenerate did not consume rewrite credit")
	}
	if count := consumeCount(); count != 1 {
		t.Fatalf("server regenerate consume entries = %d, want 1", count)
	}
	// 同一对话的新请求应重新扣费，不能被来源防重误当成第一次消费。
	if err := db.Model(&ConversationTurn{}).Where("id = ?", serverPreparation.Turn.ID).Update("status", TurnStatusCompleted).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&GenerationAttempt{}).Where("id = ?", serverPreparation.Attempt.ID).Update("status", AttemptStatusCompleted).Error; err != nil {
		t.Fatal(err)
	}
	second := regenerate(serverConversation, "regen:server:2")
	if !second.RewardConsumed || consumeCount() != 2 {
		t.Fatal("separate regenerate request was not charged")
	}
	checkRewardBalance(t, db, playerID, MessageRewriteCreditKey, 8)
}

func TestPostgresClientRouteRewriteSkipsCharge(t *testing.T) {
	db := openGenerationTestDB(t)
	ctx := context.Background()
	playerID, packID, versionID := uuid.New(), uuid.New(), uuid.New()
	hash := strings.Repeat("0", 64)
	convID, turnID, chatID, displayID, runID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, statement := range []string{
		`INSERT INTO users (id, username, display_name) VALUES ('` + playerID.String() + `', 'rewrite-route-test', 'Rewrite Route Test')`,
		`INSERT INTO challenge_packs (id, slug, name) VALUES ('` + packID.String() + `', 'rewrite-route-test', 'Rewrite Route Test')`,
		`INSERT INTO challenge_versions (id, challenge_pack_id, version, status, manifest, content_hash, published_at) VALUES ('` + versionID.String() + `', '` + packID.String() + `', '1.0.0', 'published', '{}'::jsonb, '` + hash + `', CURRENT_TIMESTAMP)`,
		`UPDATE challenge_packs SET current_version_id = '` + versionID.String() + `', status = 'active' WHERE id = '` + packID.String() + `'`,
		`INSERT INTO conversations (id, player_id, title, challenge_version_id, challenge_hash, llm_route) VALUES ('` + convID.String() + `', '` + playerID.String() + `', 'Rewrite', '` + versionID.String() + `', '` + hash + `', 'client')`,
		`INSERT INTO conversation_turns (id, conversation_id, turn_seq, trigger_type, status) VALUES ('` + turnID.String() + `', '` + convID.String() + `', 1, 'user_input', 'completed')`,
		`INSERT INTO chat_messages (id, conversation_id, role, content, turn_id, message_seq) VALUES ('` + chatID.String() + `', '` + convID.String() + `', 'user', 'hi', '` + turnID.String() + `', 1)`,
		`INSERT INTO display_messages (id, conversation_id, chat_message_id, type, markdown) VALUES ('` + displayID.String() + `', '` + convID.String() + `', '` + chatID.String() + `', 'user', 'hi')`,
		`INSERT INTO conversation_generation_runs (id, conversation_id, request_id, operation_type, payload_hash, conversation_revision, status, lease_expires_at) VALUES ('` + runID.String() + `', '` + convID.String() + `', '` + uuid.NewString() + `', 'user_input', '` + hash + `', 0, 'running', CURRENT_TIMESTAMP + INTERVAL '10 minutes')`,
		// 已占用 turn_seq=1 / message_seq=1，新回合与新消息从 2 开始分配，否则违反唯一约束。
		`UPDATE conversations SET next_turn_seq = 2, next_message_seq = 2 WHERE id = '` + convID.String() + `'`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("seed rewrite route test: %v", err)
		}
	}
	preparation, err := NewTurnRepo(db).BeginRewrite(ctx, RewriteUserMessageInput{
		ConversationID: convID, MessageID: displayID, Content: "hello edited",
		PlayerID: playerID, IdempotencyKey: "rewrite:client:1", InitialState: JSONMap{},
	})
	if err != nil {
		t.Fatalf("begin rewrite: %v", err)
	}
	if !preparation.Applied {
		t.Fatal("client rewrite was not applied")
	}
	if preparation.RewardConsumed {
		t.Fatal("client rewrite consumed rewrite credit, want no charge")
	}
	var count int64
	if err := db.Model(&RewardLedgerEntry{}).Where("player_id = ? AND source_type = ?", playerID, "message_rewrite").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("client rewrite wrote %d consume entries, want 0", count)
	}
}

func TestPostgresClientRouteSettingsSaveAllowsEmptyKey(t *testing.T) {
	db := openGenerationTestDB(t)
	ctx := context.Background()
	playerID := uuid.New()
	if err := db.Exec(`INSERT INTO users (id, username, display_name) VALUES (?, 'client-settings-test', 'Client Settings Test')`, playerID).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	repo := NewSettingsRepo(db)
	base := func() *LLMSettings {
		return &LLMSettings{
			PlayerID: playerID.String(), BaseURL: "https://api.openai.com/v1",
			Protocol: DefaultLLMProtocol, Model: "m", MaxContextTokens: DefaultLLMMaxContextTokens,
			MaxResponseTokens: DefaultLLMMaxResponseTokens, StreamEnabled: true,
		}
	}
	storedKey := func() string {
		t.Helper()
		var encrypted *string
		if err := db.Raw(`SELECT api_key_encrypted FROM llm_settings WHERE player_id = ?`, playerID).Scan(&encrypted).Error; err != nil {
			t.Fatalf("read api_key_encrypted: %v", err)
		}
		if encrypted == nil {
			return ""
		}
		return *encrypted
	}

	// 1. 新用户首次以 client 保存：Go 空串写入 ''，约束必须放行。
	first := base()
	first.LLMRoute = LLMRouteClient
	first.APIKeyEncrypted = ""
	if err := repo.Save(ctx, playerID.String(), first); err != nil {
		t.Fatalf("first client save: %v", err)
	}
	if key := storedKey(); key != "" {
		t.Fatalf("stored key after first client save = %q, want empty", key)
	}

	// 2. 切回 server 并写入 Key，再从 server 切到 client：必须成功且库中无 Key。
	server := base()
	server.LLMRoute = LLMRouteServer
	server.APIKeyEncrypted = "encrypted-secret"
	if err := repo.Save(ctx, playerID.String(), server); err != nil {
		t.Fatalf("server save: %v", err)
	}
	client := base()
	client.LLMRoute = LLMRouteClient
	client.APIKeyEncrypted = ""
	if err := repo.Save(ctx, playerID.String(), client); err != nil {
		t.Fatalf("server to client save: %v", err)
	}
	if key := storedKey(); key != "" {
		t.Fatalf("stored key after switch to client = %q, want empty", key)
	}
	got, err := repo.Get(ctx, playerID.String())
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	if got.LLMRoute != LLMRouteClient || got.HasAPIKey {
		t.Fatalf("settings after switch = route %q hasKey %v, want client/false", got.LLMRoute, got.HasAPIKey)
	}
}

func TestPostgresClientRouteCampaignOutcomeSkipsLedger(t *testing.T) {
	db := openGenerationTestDB(t)
	ctx := context.Background()
	playerID, packID, versionID, campaignID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	hash := strings.Repeat("0", 64)
	for _, statement := range []string{
		`INSERT INTO users (id, username, display_name) VALUES ('` + playerID.String() + `', 'outcome-route-test', 'Outcome Route Test')`,
		`INSERT INTO challenge_packs (id, slug, name) VALUES ('` + packID.String() + `', 'outcome-route-test', 'Outcome Route Test')`,
		`INSERT INTO challenge_versions (id, challenge_pack_id, version, status, manifest, content_hash, published_at) VALUES ('` + versionID.String() + `', '` + packID.String() + `', '1.0.0', 'published', '{}'::jsonb, '` + hash + `', CURRENT_TIMESTAMP)`,
		`UPDATE challenge_packs SET current_version_id = '` + versionID.String() + `', status = 'active' WHERE id = '` + packID.String() + `'`,
		`INSERT INTO campaigns (id, player_id, title, challenge_version_id, challenge_hash, current_location_key, random_seed, llm_route) VALUES ('` + campaignID.String() + `', '` + playerID.String() + `', 'Route', '` + versionID.String() + `', '` + hash + `', 'world.start', 1, 'client')`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("seed outcome route test: %v", err)
		}
	}
	store := &CampaignActionStore{tx: db}
	campaign := Campaign{ID: campaignID, PlayerID: playerID, ChallengeVersionID: versionID, LLMRoute: LLMRouteClient}
	rewards, err := store.GrantCampaignOutcomeRewards(ctx, campaign, "world_warden")
	if err != nil {
		t.Fatalf("grant campaign outcome rewards: %v", err)
	}
	if len(rewards) != 0 {
		t.Fatalf("client outcome rewards = %+v, want empty", rewards)
	}
	var count int64
	if err := db.Model(&RewardLedgerEntry{}).Where("player_id = ?", playerID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("client outcome wrote %d ledger entries, want 0", count)
	}
}
