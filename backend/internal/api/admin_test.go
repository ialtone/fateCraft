package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/challengeimport"
	"github.com/ialtone/fatecraft/internal/storage"
)

type builtinImporterStub struct {
	items           []challengeimport.BuiltinInfo
	importResult    *challengeimport.ImportResult
	importedSlug    string
	overwrittenSlug string
	listErr         error
	importErr       error
	overwriteErr    error
}

func (s *builtinImporterStub) ListBuiltins(context.Context) ([]challengeimport.BuiltinInfo, error) {
	return s.items, s.listErr
}

func (s *builtinImporterStub) PreviewBuiltin(_ context.Context, slug string, actor uuid.UUID) (*storage.GameplayImport, error) {
	s.importedSlug = slug
	return &storage.GameplayImport{ID: uuid.New(), ActorID: actor, Status: "preview"}, s.importErr
}

func TestPreviewAdminBuiltinUsesAuthenticatedActor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &builtinImporterStub{}
	server := &Server{builtins: stub}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/admin/builtin-challenges/trial/preview", nil)
	ctx.Params = gin.Params{{Key: "slug", Value: "trial"}}
	actor := uuid.New()
	ctx.Set(authUserKey, &storage.User{ID: actor})
	server.previewAdminBuiltin(ctx)
	var body storage.GameplayImport
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || body.ActorID != actor {
		t.Fatalf("actor binding lost: %+v %v", body, err)
	}
	if recorder.Code != http.StatusOK || stub.importedSlug != "trial" {
		t.Fatalf("preview failed: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestAllowedRollDifficulties(t *testing.T) {
	for _, mode := range []string{"easy", "normal", "hard"} {
		if !allowedRollDifficulties[mode] {
			t.Fatalf("expected %q to be allowed", mode)
		}
	}
	for _, mode := range []string{"", "custom", "impossible"} {
		if allowedRollDifficulties[mode] {
			t.Fatalf("expected %q to be rejected", mode)
		}
	}
}

func TestSummarizeAdminLLMRequestLogKeepsDiagnosticLinks(t *testing.T) {
	conversationID := uuid.New()
	attemptID := uuid.New()
	memoryJobID := uuid.New()
	memoryID := uuid.New()
	inquiryID := uuid.New()
	snapshotID := uuid.New()
	createdAt := time.Date(2026, 7, 31, 12, 0, 0, 123, time.UTC)

	summary := summarizeAdminLLMRequestLog(storage.LLMRequestLog{
		ID: uuid.New(), ConversationID: &conversationID, Purpose: "character_inquiry", AttemptID: &attemptID,
		MemoryJobID: &memoryJobID, MemoryID: &memoryID, InquiryID: &inquiryID, InquirySnapshotID: &snapshotID,
		InquirySnapshotBranchEpoch: 4, InquirySnapshotRevision: 12, Model: "test-model", CreatedAt: createdAt,
	})

	if summary.Purpose != "character_inquiry" || summary.AttemptID != &attemptID || summary.MemoryJobID != &memoryJobID ||
		summary.MemoryID != &memoryID || summary.InquiryID != &inquiryID || summary.InquirySnapshotID != &snapshotID {
		t.Fatalf("summary lost diagnostic links: %#v", summary)
	}
	if summary.InquirySnapshotBranchEpoch != 4 || summary.InquirySnapshotRevision != 12 ||
		summary.CreatedAt != createdAt.Format(time.RFC3339Nano) {
		t.Fatalf("summary lost snapshot metadata: %#v", summary)
	}
}

func TestSummarizeAdminContextBuildLogKeepsDiagnosticLinks(t *testing.T) {
	attemptID := uuid.New()
	memoryJobID := uuid.New()
	createdAt := time.Date(2026, 8, 1, 12, 0, 0, 123, time.UTC)
	summary := summarizeAdminContextBuildLog(storage.ContextBuildLog{
		ID: uuid.New(), ConversationID: uuid.New(), AttemptID: &attemptID, MemoryJobID: &memoryJobID,
		Model: "test-model", MaxContextTokens: 128000, ReservedResponseTokens: 16384, UsedPromptTokens: 4096,
		IncludedMessageIDs: []string{"message-1", "message-2"}, ActivatedWorldEntryIDs: []string{"entry-1"},
		MemoryLag: true, CreatedAt: createdAt,
	})
	if summary.AttemptID != &attemptID || summary.MemoryJobID != &memoryJobID || !summary.MemoryLag {
		t.Fatalf("summary lost diagnostic links: %#v", summary)
	}
	if summary.IncludedMessageCount != 2 || summary.ActivatedWorldEntryCount != 1 ||
		summary.CreatedAt != createdAt.Format(time.RFC3339Nano) {
		t.Fatalf("summary counts or time = %#v", summary)
	}
}

func TestListAdminBuiltins(t *testing.T) {
	gin.SetMode(gin.TestMode)
	stub := &builtinImporterStub{items: []challengeimport.BuiltinInfo{{Slug: "trial", Version: "1.0.0"}}}
	server := &Server{builtins: stub}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/admin/builtin-challenges", nil)

	server.listAdminBuiltins(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var body struct {
		Builtins []challengeimport.BuiltinInfo `json:"builtins"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Builtins) != 1 || body.Builtins[0].Slug != "trial" {
		t.Fatalf("body = %#v", body)
	}
}
