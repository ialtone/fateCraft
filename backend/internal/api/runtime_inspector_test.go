package api

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/storage"
)

func TestSummarizePlayerTurnPerformanceUsesWeightedTPS(t *testing.T) {
	started := time.Date(2026, time.September, 18, 12, 0, 0, 0, time.UTC)
	finished := started.Add(8 * time.Second)
	snapshot := &storage.AgentAttemptPerformance{
		Attempt: storage.GenerationAttempt{
			ID: uuid.New(), Status: storage.AttemptStatusCompleted,
			StartedAt: started, FinishedAt: &finished,
		},
		Steps: []storage.AgentStep{
			{Model: "test-model", PromptTokens: 100, CompletionTokens: 30, TokenUsageSource: "provider", FirstEventMS: 400, ModelDurationMS: 2400, GenerationMS: 2000, FinishedAt: &finished},
			{Model: "test-model", PromptTokens: 140, CompletionTokens: 10, TokenUsageSource: "estimated", FirstEventMS: 600, ModelDurationMS: 1600, GenerationMS: 1000, FinishedAt: &finished},
		},
		ToolCalls: 2,
	}

	got := summarizePlayerTurnPerformance(snapshot, finished.Add(time.Hour))
	if got.PromptTokens != 240 || got.CompletionTokens != 40 || got.StepCount != 2 || got.ToolCallCount != 2 {
		t.Fatalf("counts = %#v", got)
	}
	if got.AverageFirstResponseMS != 500 || got.ModelDurationMS != 4000 || got.AttemptDurationMS != 8000 {
		t.Fatalf("durations = %#v", got)
	}
	if math.Abs(got.TokensPerSecond-13.333333) > 0.0001 {
		t.Fatalf("tokens_per_second = %f", got.TokensPerSecond)
	}
	if !got.UsageEstimated {
		t.Fatal("mixed provider/local token counts must be marked estimated")
	}
}

func TestSummarizePlayerTurnPerformanceKeepsMissingFirstResponse(t *testing.T) {
	started := time.Now()
	finished := started.Add(time.Second)
	got := summarizePlayerTurnPerformance(&storage.AgentAttemptPerformance{
		Attempt: storage.GenerationAttempt{ID: uuid.New(), StartedAt: started, FinishedAt: &finished},
		Steps:   []storage.AgentStep{{FirstEventMS: -1, FinishedAt: &finished}},
	}, finished)
	if got.AverageFirstResponseMS != -1 {
		t.Fatalf("average_first_response_ms = %d", got.AverageFirstResponseMS)
	}
}
