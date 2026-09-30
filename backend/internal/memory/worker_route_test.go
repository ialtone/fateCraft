package memory

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/llm"
	"github.com/ialtone/fatecraft/internal/storage"
	"github.com/ialtone/fatecraft/internal/tokenizer"
)

// routeRepoStub 按脚本返回记忆任务来源，并记录终结调用。
type routeRepoStub struct {
	source       *storage.MemorySource
	released     []uuid.UUID
	failed       []uuid.UUID
	committed    int
	sourcesFixed int
}

func (r *routeRepoStub) Claim(context.Context, string, time.Duration) (*storage.MemoryJob, error) {
	return nil, storage.ErrNotFound
}

func (r *routeRepoStub) RenewLease(context.Context, uuid.UUID, string, uuid.UUID, time.Duration) (bool, error) {
	return true, nil
}

func (r *routeRepoStub) LoadSource(context.Context, storage.MemoryJob) (*storage.MemorySource, error) {
	return r.source, nil
}

func (r *routeRepoStub) SetJobSource(context.Context, uuid.UUID, string, uuid.UUID, string, string) error {
	r.sourcesFixed++
	return nil
}

func (r *routeRepoStub) Commit(context.Context, storage.MemoryCommitInput) error {
	r.committed++
	return nil
}

func (r *routeRepoStub) FailJob(_ context.Context, jobID uuid.UUID, _ string, _ uuid.UUID, _ string, _ string, _ *time.Time) error {
	r.failed = append(r.failed, jobID)
	return nil
}

func (r *routeRepoStub) ReleaseClaim(_ context.Context, jobID uuid.UUID, _ string, _ uuid.UUID, _ time.Duration) error {
	r.released = append(r.released, jobID)
	return nil
}

type routeSettingsStub struct {
	route   string
	baseURL string
	apiKey  string
}

func (s routeSettingsStub) Get(context.Context, string) (*storage.LLMSettings, error) {
	return &storage.LLMSettings{LLMRoute: s.route, BaseURL: s.baseURL, APIKey: s.apiKey, Protocol: string(llm.ProtocolChatCompletions), Model: "test-model", MaxResponseTokens: 2048}, nil
}

func runRouteGateJob(t *testing.T, convRoute, effectiveRoute string) *routeRepoStub {
	t.Helper()
	convID, jobID := uuid.New(), uuid.New()
	token := uuid.New()
	repo := &routeRepoStub{
		source: &storage.MemorySource{
			Conversation: storage.Conversation{ID: convID, PlayerID: "player-1", LLMRoute: convRoute},
		},
	}
	worker := NewWorker(repo, routeSettingsStub{route: effectiveRoute}, llm.NewClient(), tokenizer.NewHeuristic(), WorkerConfig{
		WorkerID: "test-worker", PollInterval: time.Hour, LeaseDuration: time.Minute, RequestTimeout: time.Minute,
	})
	worker.execute(context.Background(), &storage.MemoryJob{
		ID: jobID, ConversationID: convID, LeaseToken: &token,
	})
	return repo
}

func TestMemoryWorkerReleasesJobOnRouteMismatch(t *testing.T) {
	repo := runRouteGateJob(t, storage.LLMRouteServer, storage.LLMRouteClient)
	if len(repo.released) != 1 {
		t.Fatalf("released = %d, want 1", len(repo.released))
	}
	if len(repo.failed) != 0 || repo.committed != 0 {
		t.Fatalf("mismatched job must not fail or commit: failed=%d committed=%d", len(repo.failed), repo.committed)
	}
}

func TestMemoryWorkerReleasesClientJobWithoutRelay(t *testing.T) {
	repo := runRouteGateJob(t, storage.LLMRouteClient, storage.LLMRouteClient)
	if len(repo.released) != 1 {
		t.Fatalf("released = %d, want 1", len(repo.released))
	}
	if len(repo.failed) != 0 || repo.committed != 0 {
		t.Fatalf("unrelayed job must not fail or commit: failed=%d committed=%d", len(repo.failed), repo.committed)
	}
}

func TestMemoryWorkerAttributesCaptureWithoutRequestIdentity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"choices":[{"message":{"content":"{\"synopsis\":\"记忆摘要\",\"facts\":[],\"entities\":[],\"goals\":[],\"open_threads\":[],\"player_choices\":[]}"},"finish_reason":"stop"}]}`)
	}))
	defer server.Close()
	player, conv, token := uuid.New(), uuid.New(), uuid.New()
	repo := &routeRepoStub{source: &storage.MemorySource{Conversation: storage.Conversation{ID: conv, PlayerID: player.String(), LLMRoute: storage.LLMRouteServer}}}
	client := llm.NewClient()
	decisions, summaries := 0, 0
	client.SetRequestAudit(func(_ context.Context, a llm.RequestAudit) error {
		summaries++
		if a.BodyStatus != "captured" || a.ConversationID != conv.String() {
			t.Error("memory body missing")
		}
		return nil
	}, func(_ context.Context, p llm.Params) bool {
		decisions++
		if p.PlayerID != player.String() || p.ConversationID != conv.String() || p.Purpose != "memory" {
			t.Error("memory call lost trusted attribution")
		}
		settings := storage.DefaultSystemSettings()
		settings.DetailedLLMLoggingEnabled, settings.DetailedLLMLoggingScope, settings.DetailedLLMLoggingTargetID = true, "user", &player
		return settings.CaptureDetailedLLMLog(p.PlayerID, p.ConversationID)
	})
	worker := NewWorker(repo, routeSettingsStub{route: storage.LLMRouteServer, baseURL: server.URL, apiKey: "test-key"}, client, tokenizer.NewHeuristic(), WorkerConfig{WorkerID: "scope-test", PollInterval: time.Hour, LeaseDuration: time.Minute, RequestTimeout: time.Minute})
	worker.execute(context.Background(), &storage.MemoryJob{ID: uuid.New(), ConversationID: conv, LeaseToken: &token})
	if decisions != 1 || summaries != 1 || repo.committed != 1 {
		t.Fatalf("decisions=%d summaries=%d committed=%d failed=%d", decisions, summaries, repo.committed, len(repo.failed))
	}
}
