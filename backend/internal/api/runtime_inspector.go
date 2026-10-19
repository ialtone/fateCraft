package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ialtone/fatecraft/internal/storage"
)

type runtimeInspectorResponse struct {
	ConversationID string                        `json:"conversation_id"`
	UpdatedAt      time.Time                     `json:"updated_at"`
	Activity       conversationActivityResponse  `json:"activity"`
	Request        *playerContextBuildSummary    `json:"request,omitempty"`
	Performance    *playerTurnPerformanceSummary `json:"performance,omitempty"`
	MemoryArchive  *playerMemoryArchiveSummary   `json:"memory_archive,omitempty"`
	Memory         string                        `json:"memory"`
	Diagnostics    []runtimeDiagnostic           `json:"diagnostics"`
}

type playerTurnPerformanceSummary struct {
	AttemptID              string     `json:"attempt_id"`
	Status                 string     `json:"status"`
	Model                  string     `json:"model"`
	StepCount              int        `json:"step_count"`
	ToolCallCount          int64      `json:"tool_call_count"`
	PromptTokens           int        `json:"prompt_tokens"`
	CompletionTokens       int        `json:"completion_tokens"`
	ModelDurationMS        int64      `json:"model_duration_ms"`
	GenerationMS           int64      `json:"generation_ms"`
	AverageFirstResponseMS int64      `json:"average_first_response_ms"`
	AttemptDurationMS      int64      `json:"attempt_duration_ms"`
	TokensPerSecond        float64    `json:"tokens_per_second"`
	UsageEstimated         bool       `json:"usage_estimated"`
	StartedAt              time.Time  `json:"started_at"`
	FinishedAt             *time.Time `json:"finished_at,omitempty"`
}
type runtimeDiagnostic struct {
	Kind    string    `json:"kind"`
	Message string    `json:"message"`
	At      time.Time `json:"at"`
}

func (s *Server) getRuntimeInspector(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	snapshot, ok := s.ownedActivity(c, id)
	if !ok {
		return
	}
	act := *snapshot
	log, _ := s.ctxLogs.Last(c.Request.Context(), id)
	var req *playerContextBuildSummary
	if log != nil {
		v := summarizePlayerContextBuild(log)
		req = &v
	}
	var performance *playerTurnPerformanceSummary
	if s.agents != nil {
		if snapshot, err := s.agents.LatestAttemptPerformance(c.Request.Context(), id); err == nil && snapshot != nil {
			value := summarizePlayerTurnPerformance(snapshot, time.Now())
			performance = &value
		}
	}
	var archive *playerMemoryArchiveSummary
	if s.memory != nil {
		if latest, err := s.memory.Latest(c.Request.Context(), id); err == nil && latest != nil {
			archive = &playerMemoryArchiveSummary{CoveredFromTurn: latest.CoveredFromTurnSeq, CoveredUntilTurn: latest.CoveredUntilTurnSeq, TokenCount: latest.TokenCount, CreatedAt: latest.CreatedAt}
		}
	}
	c.JSON(http.StatusOK, runtimeInspectorResponse{ConversationID: id.String(), UpdatedAt: time.Now(), Activity: act, Request: req, Performance: performance, MemoryArchive: archive, Memory: act.Memory, Diagnostics: []runtimeDiagnostic{}})
}

// Public metadata only: archive bodies, worker identity, leases and provider
// errors belong to administrator diagnostics, not the player runtime panel.
type playerMemoryJobSummary = storage.ActivityMemoryJob

type playerMemoryArchiveSummary struct {
	CoveredFromTurn  int64     `json:"covered_from_turn"`
	CoveredUntilTurn int64     `json:"covered_until_turn"`
	TokenCount       int       `json:"token_count"`
	CreatedAt        time.Time `json:"created_at"`
}

func summarizePlayerMemoryJob(job storage.MemoryJob) *playerMemoryJobSummary {
	return &playerMemoryJobSummary{ID: job.ID, Status: job.Status, TargetUntilTurn: job.TargetUntilTurnSeq, ExecutionCount: job.ExecutionCount, MaxExecutions: job.MaxExecutions, CreatedAt: job.CreatedAt, StartedAt: job.StartedAt, FinishedAt: job.FinishedAt}
}

func summarizePlayerTurnPerformance(snapshot *storage.AgentAttemptPerformance, now time.Time) playerTurnPerformanceSummary {
	result := playerTurnPerformanceSummary{
		AttemptID: snapshot.Attempt.ID.String(), Status: snapshot.Attempt.Status,
		StepCount: len(snapshot.Steps), ToolCallCount: snapshot.ToolCalls,
		AverageFirstResponseMS: -1, UsageEstimated: false,
		StartedAt: snapshot.Attempt.StartedAt, FinishedAt: snapshot.Attempt.FinishedAt,
	}
	finishedAt := now
	if snapshot.Attempt.FinishedAt != nil {
		finishedAt = *snapshot.Attempt.FinishedAt
	}
	result.AttemptDurationMS = finishedAt.Sub(snapshot.Attempt.StartedAt).Milliseconds()
	var firstResponseTotal int64
	var firstResponseCount int64
	for _, step := range snapshot.Steps {
		if result.Model == "" {
			result.Model = step.Model
		}
		result.PromptTokens += step.PromptTokens
		result.CompletionTokens += step.CompletionTokens
		result.ModelDurationMS += step.ModelDurationMS
		result.GenerationMS += step.GenerationMS
		if step.FirstEventMS >= 0 && step.FinishedAt != nil {
			firstResponseTotal += step.FirstEventMS
			firstResponseCount++
		}
		if step.FinishedAt != nil && step.TokenUsageSource != "provider" {
			result.UsageEstimated = true
		}
	}
	if firstResponseCount > 0 {
		result.AverageFirstResponseMS = firstResponseTotal / firstResponseCount
	}
	// Provider output usage can include hidden reasoning generated before the
	// first visible event. Use the full request duration for both streaming and
	// non-streaming calls so those tokens never lose their corresponding time.
	if result.ModelDurationMS > 0 {
		result.TokensPerSecond = float64(result.CompletionTokens) * 1000 / float64(result.ModelDurationMS)
	}
	return result
}
