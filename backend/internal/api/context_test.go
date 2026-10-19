package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/storage"
)

func TestRespondLastContextTreatsMissingBuildAsEmpty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	respondLastContext(ctx, nil, storage.ErrNotFound)
	ctx.Writer.WriteHeaderNow()

	if recorder.Code != http.StatusNoContent || recorder.Body.Len() != 0 {
		t.Fatalf("missing context response = status %d body %q", recorder.Code, recorder.Body.String())
	}
}

func TestPlayerContextSummaryDoesNotExposePromptDiagnostics(t *testing.T) {
	log := &storage.ContextBuildSummary{
		ID: uuid.New(), ConversationID: uuid.New(), Model: "test-model",
		MaxContextTokens: 128000, ReservedResponseTokens: 16384, UsedPromptTokens: 4096,
		IncludedMessageCount: 2, ActivatedWorldEntryCount: 1, CreatedAt: time.Now(),
	}
	summary := summarizePlayerContextBuild(log)
	if summary.IncludedMessageCount != 2 || summary.ActivatedWorldEntryCount != 1 {
		t.Fatalf("summary counts = %#v", summary)
	}
	raw, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(raw)
	for _, forbidden := range []string{"segments", "included_message_ids", "activated_world_entry_ids", "system_policy", "secret-world-entry"} {
		if strings.Contains(encoded, forbidden) {
			t.Fatalf("player context summary exposed %q: %s", forbidden, encoded)
		}
	}
}
