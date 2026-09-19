package storage

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/operation"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestPostgresGenerationRunLifecycleAndWriteOwnership(t *testing.T) {
	db := openGenerationTestDB(t)
	ctx := context.Background()
	conversationID := seedGenerationTestConversation(t, db)
	repo := NewConversationRepo(db)

	request := GenerationRequest{RequestID: uuid.New(), OperationType: "user_input", PayloadHash: "test"}
	_, oldRun, _, err := repo.StartGeneration(ctx, conversationID, request)
	if err != nil {
		t.Fatal(err)
	}
	_, replayRun, replayed, err := repo.StartGeneration(ctx, conversationID, request)
	if err != nil || !replayed || replayRun.ID != oldRun.ID {
		t.Fatalf("same request was not replayed: run=%v replayed=%v err=%v", replayRun, replayed, err)
	}
	if _, _, _, err := repo.StartGeneration(ctx, conversationID, GenerationRequest{
		RequestID: request.RequestID, OperationType: request.OperationType, PayloadHash: "different",
	}); !errors.Is(err, ErrGenerationRequestConflict) {
		t.Fatalf("request payload conflict error=%v", err)
	}
	if _, _, _, err := repo.StartGeneration(ctx, conversationID, GenerationRequest{RequestID: uuid.New(), OperationType: "user_input", PayloadHash: "test2"}); !errors.Is(err, ErrGenerationInProgress) {
		t.Fatalf("duplicate StartGeneration error=%v", err)
	}

	ownedCtx := operation.WithGeneration(ctx, conversationID, oldRun.ID)
	messages := NewMessageRepo(db)
	if err := messages.CreateChat(ownedCtx, &ChatMessage{
		ID: uuid.New(), ConversationID: conversationID, Role: RoleAssistant,
		Content: "before archive", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("active run write failed: %v", err)
	}
	if err := repo.TransitionLifecycle(ctx, conversationID, StatusArchived, map[string]any{
		"archived_at": time.Now(), "updated_at": time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if _, replayRun, replayed, err := repo.StartGeneration(ctx, conversationID, request); err != nil || !replayed || replayRun.ID != oldRun.ID {
		t.Fatalf("archived replay failed: run=%v replayed=%v err=%v", replayRun, replayed, err)
	}
	if err := messages.CreateChat(ownedCtx, &ChatMessage{
		ID: uuid.New(), ConversationID: conversationID, Role: RoleAssistant,
		Content: "must not persist", CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}); !errors.Is(err, ErrGenerationNotActive) {
		t.Fatalf("stale generation write error=%v", err)
	}
	if err := repo.FinishGeneration(ctx, oldRun.ID, GenerationRunCancelled, "archived"); err != nil {
		t.Fatal(err)
	}
	if err := repo.TransitionLifecycle(ctx, conversationID, StatusActive, map[string]any{
		"archived_at": nil, "updated_at": time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	_, newRun, _, err := repo.StartGeneration(ctx, conversationID, GenerationRequest{RequestID: uuid.New(), OperationType: "user_input", PayloadHash: "test3"})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.FinishGeneration(ctx, oldRun.ID, GenerationRunCompleted, ""); err != nil {
		t.Fatal(err)
	}
	conversation, err := repo.Get(ctx, conversationID)
	if err != nil || !conversation.IsProcessing {
		t.Fatalf("old run released new run: conversation=%+v err=%v", conversation, err)
	}
	if err := repo.FinishGeneration(ctx, newRun.ID, GenerationRunCompleted, ""); err != nil {
		t.Fatal(err)
	}
	conversation, err = repo.Get(ctx, conversationID)
	if err != nil || conversation.IsProcessing {
		t.Fatalf("new run did not release processing state: conversation=%+v err=%v", conversation, err)
	}

	var count int64
	if err := db.Model(&ChatMessage{}).Where("conversation_id = ?", conversationID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("persisted message count=%d err=%v", count, err)
	}
}

func TestPostgresPeriodicReaperPreservesLiveOwnedGeneration(t *testing.T) {
	db := openGenerationTestDB(t)
	ctx := context.Background()
	conversationID := seedGenerationTestConversation(t, db)
	repo := NewConversationRepo(db)
	repo.SetInstanceID("generation-reaper-test")

	_, run, _, err := repo.StartGeneration(ctx, conversationID, GenerationRequest{
		RequestID: uuid.New(), OperationType: "user_input", PayloadHash: "reaper-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if count, err := repo.ReapStaleGenerations(ctx); err != nil || count != 0 {
		t.Fatalf("periodic reaper reclaimed live owned run: count=%d err=%v", count, err)
	}
	var persisted ConversationGenerationRun
	if err := db.First(&persisted, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.Status != GenerationRunRunning {
		t.Fatalf("live owned run status=%q, want %q", persisted.Status, GenerationRunRunning)
	}

	if count, err := repo.ReapOwnedGenerations(ctx); err != nil || count != 1 {
		t.Fatalf("startup recovery count=%d err=%v", count, err)
	}
	if err := db.First(&persisted, "id = ?", run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.Status != GenerationRunAbandoned || persisted.ErrorCode != "instance_restarted" {
		t.Fatalf("startup recovery persisted run=%+v", persisted)
	}
	conversation, err := repo.Get(ctx, conversationID)
	if err != nil || conversation.IsProcessing {
		t.Fatalf("startup recovery did not release conversation: conversation=%+v err=%v", conversation, err)
	}
}

func TestPostgresDisplayMessageCursorIsStableForEqualTimestamps(t *testing.T) {
	db := openGenerationTestDB(t)
	conversationID := seedGenerationTestConversation(t, db)
	createdAt := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	ids := make([]uuid.UUID, 5)
	for index := range ids {
		ids[index] = uuid.MustParse("00000000-0000-0000-0000-00000000000" + string(rune('1'+index)))
		if err := db.Create(&DisplayMessage{
			ID: ids[index], ConversationID: conversationID, Type: "assistant",
			Markdown: string(rune('a' + index)), Metadata: JSONMap{}, CreatedAt: createdAt,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}

	repo := NewMessageRepo(db)
	first, err := repo.ListDisplay(context.Background(), conversationID, nil, 2)
	if err != nil || len(first) != 2 || first[0].ID != ids[3] || first[1].ID != ids[4] {
		t.Fatalf("first page=%+v err=%v", first, err)
	}
	second, err := repo.ListDisplay(context.Background(), conversationID, &first[0].ID, 2)
	if err != nil || len(second) != 2 || second[0].ID != ids[1] || second[1].ID != ids[2] {
		t.Fatalf("second page=%+v err=%v", second, err)
	}
	third, err := repo.ListDisplay(context.Background(), conversationID, &second[0].ID, 2)
	if err != nil || len(third) != 1 || third[0].ID != ids[0] {
		t.Fatalf("third page=%+v err=%v", third, err)
	}
	missing := uuid.New()
	if _, err := repo.ListDisplay(context.Background(), conversationID, &missing, 2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing cursor error=%v", err)
	}
}

func openGenerationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	databaseURL := os.Getenv("FATECRAFT_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("FATECRAFT_TEST_DATABASE_URL is not set")
	}
	baseDB, err := gorm.Open(postgres.Open(databaseURL), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	schemaName := "fatecraft_generation_verify_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := baseDB.Exec("CREATE SCHEMA " + schemaName).Error; err != nil {
		t.Fatalf("create verification schema: %v", err)
	}
	t.Cleanup(func() {
		if err := baseDB.Exec("DROP SCHEMA " + schemaName + " CASCADE").Error; err != nil {
			t.Errorf("drop verification schema: %v", err)
		}
	})
	parsed, err := url.Parse(databaseURL)
	if err != nil || parsed.Scheme == "" {
		t.Fatalf("invalid PostgreSQL test URL: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schemaName)
	parsed.RawQuery = query.Encode()
	db, err := gorm.Open(postgres.Open(parsed.String()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	schema, err := os.ReadFile(filepath.Join("..", "..", "scripts", "sql", "init.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(string(schema)).Error; err != nil {
		t.Fatalf("initialize verification schema: %v", err)
	}
	return db
}

func seedGenerationTestConversation(t *testing.T, db *gorm.DB) uuid.UUID {
	t.Helper()
	playerID, packID, versionID, conversationID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	hash := strings.Repeat("0", 64)
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO users (id, display_name) VALUES (?, 'Generation Test')`, []any{playerID}},
		{`INSERT INTO challenge_packs (id, slug, name) VALUES (?, 'generation-test', 'Generation Test')`, []any{packID}},
		{`INSERT INTO challenge_versions (id, challenge_pack_id, version, status, manifest, content_hash, published_at) VALUES (?, ?, '1.0.0', 'published', '{}'::jsonb, ?, CURRENT_TIMESTAMP)`, []any{versionID, packID, hash}},
		{`INSERT INTO conversations (id, player_id, title, challenge_version_id, challenge_hash) VALUES (?, ?, 'Generation Test', ?, ?)`, []any{conversationID, playerID, versionID, hash}},
	}
	for _, statement := range statements {
		if err := db.Exec(statement.query, statement.args...).Error; err != nil {
			t.Fatalf("seed generation test: %v", err)
		}
	}
	return conversationID
}
