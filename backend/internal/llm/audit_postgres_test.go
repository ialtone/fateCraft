package llm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/storage"
	"github.com/ialtone/fatecraft/internal/testdb"
)

func TestPostgresAuditJSONAndTextBoundaries(t *testing.T) {
	db := testdb.Open(t, "audit_json")
	repo := storage.NewLLMRequestLogRepo(db)
	large, _ := json.Marshal(map[string]any{"text": strings.Repeat("中文\n<", 600000), "api_key": "synthetic-secret"})
	for _, raw := range [][]byte{nil, []byte(`{"invalid`), []byte(`{"text":"\u0000"}`), []byte("{\"text\":\"\xff\"}"), large} {
		body, info := safeAuditJSON(raw, "synthetic-secret")
		text, _ := safeAuditText(strings.Repeat("中文", 200000)+"\x00\xff", "", auditTextLimit)
		row := &storage.LLMRequestLog{ID: uuid.New(), Model: "boundary", StartedAt: time.Now(), CreatedAt: time.Now(), BodyStatus: "captured", RequestBody: storage.JSONDoc(body), ResponseText: text, BodyMetadata: storage.JSONMap{"request": info}}
		if err := repo.Create(context.Background(), row); err != nil {
			t.Fatalf("JSONB/text write failed for %d bytes: %v", len(raw), err)
		}
		got, err := repo.Get(context.Background(), row.ID)
		if err != nil || got.ResponseText != text {
			t.Fatalf("round trip failed: %v", err)
		}
	}
}
