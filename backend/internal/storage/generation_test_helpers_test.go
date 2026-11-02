package storage

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/testdb"
	"gorm.io/gorm"
)

func openGenerationTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testdb.Open(t, "generation_verify")
}

func seedGenerationTestConversation(t *testing.T, db *gorm.DB) uuid.UUID {
	t.Helper()
	playerID, packID, versionID, conversationID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	hash := strings.Repeat("0", 64)
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO users (id, username, display_name) VALUES (?, 'generation-test', 'Generation Test')`, []any{playerID}},
		{`INSERT INTO challenge_packs (id, slug, name) VALUES (?, 'generation-test', 'Generation Test')`, []any{packID}},
		{`INSERT INTO challenge_versions (id, challenge_pack_id, version, status, manifest, content_hash, published_at) VALUES (?, ?, '1.0.0', 'published', '{}'::jsonb, ?, CURRENT_TIMESTAMP)`, []any{versionID, packID, hash}},
		{`UPDATE challenge_packs SET current_version_id = ?, status = 'active' WHERE id = ?`, []any{versionID, packID}},
		{`INSERT INTO conversations (id, player_id, title, challenge_version_id, challenge_hash, llm_route) VALUES (?, ?, 'Generation Test', ?, ?, 'server')`, []any{conversationID, playerID, versionID, hash}},
	}
	for _, statement := range statements {
		if err := db.Exec(statement.query, statement.args...).Error; err != nil {
			t.Fatalf("seed generation test: %v", err)
		}
	}
	return conversationID
}
