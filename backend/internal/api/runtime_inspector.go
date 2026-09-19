package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ialtone/fatecraft/internal/challenge"
	"github.com/ialtone/fatecraft/internal/storage"
)

type runtimeInspectorResponse struct {
	ConversationID string                        `json:"conversation_id"`
	UpdatedAt      time.Time                     `json:"updated_at"`
	Activity       conversationActivityResponse  `json:"activity"`
	Request        *playerContextBuildSummary    `json:"request,omitempty"`
	Performance    *playerTurnPerformanceSummary `json:"performance,omitempty"`
	Cache          challenge.CacheStats          `json:"cache"`
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
	if _, err := s.conv.GetOwned(c.Request.Context(), id); err != nil {
		fail(c, err)
		return
	}
	act := conversationActivityResponse{Narrative: "idle", Memory: "idle"}
	if s.turns != nil {
		if cap, err := s.turns.CurrentCapabilities(c.Request.Context(), id); err == nil && cap != nil {
			act.TurnStatus = cap.Status
			act.CanContinue = cap.CanContinue
			act.CanRegenerate = cap.CanRegenerate
			act.RegenerateCostQuantity = cap.RegenerateCostQuantity
		}
	}
	if s.agents != nil {
		if a, err := s.agents.CurrentActivity(c.Request.Context(), id); err == nil && a != nil {
			act.Narrative = a.Phase
			act.StepNo = a.StepNo
			act.ToolCategory = publicAgentToolCategory(a.ToolName)
		}
	}
	log, _ := s.ctxLogs.Last(c.Request.Context(), id)
	var req *playerContextBuildSummary
	if log != nil {
		v := summarizePlayerContextBuild(log)
		req = &v
	}
	if s.memory != nil {
		if jobs, err := s.memory.Jobs(c.Request.Context(), id, 1); err == nil && len(jobs) > 0 {
			act.Memory = publicMemoryActivity(jobs[0].Status)
		}
	}
	var performance *playerTurnPerformanceSummary
	if s.agents != nil {
		if snapshot, err := s.agents.LatestAttemptPerformance(c.Request.Context(), id); err == nil && snapshot != nil {
			value := summarizePlayerTurnPerformance(snapshot, time.Now())
			performance = &value
		}
	}
	cache := challenge.CacheStats{}
	if s.challenges != nil {
		cache = s.challenges.CacheStats()
	}
	c.JSON(http.StatusOK, runtimeInspectorResponse{ConversationID: id.String(), UpdatedAt: time.Now(), Activity: act, Request: req, Performance: performance, Cache: cache, Memory: act.Memory, Diagnostics: []runtimeDiagnostic{}})
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
	if result.GenerationMS > 0 {
		result.TokensPerSecond = float64(result.CompletionTokens) * 1000 / float64(result.GenerationMS)
	}
	return result
}
