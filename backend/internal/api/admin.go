package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/challengeimport"
	"github.com/ialtone/fatecraft/internal/llmsettings"
	"github.com/ialtone/fatecraft/internal/storage"
)

var allowedRollDifficulties = map[string]bool{
	"easy": true, "normal": true, "hard": true,
}

func (s *Server) getAdminSystemSettings(c *gin.Context) {
	var settings *storage.SystemSettings
	var err error
	if s.llmSettings != nil {
		settings, err = s.llmSettings.AdminView(c.Request.Context())
	} else {
		settings, err = s.system.Get(c.Request.Context())
	}
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, settings)
}

type adminSystemSettingsBody struct {
	Enabled                 *bool    `json:"roll_system_enabled"`
	DifficultyMode          *string  `json:"roll_difficulty_mode"`
	DetailedLLMLogging      *bool    `json:"detailed_llm_logging_enabled"`
	DiagnosticLogCleanup    *bool    `json:"diagnostic_log_cleanup_enabled"`
	DiagnosticRetentionDays *int     `json:"diagnostic_log_retention_days"`
	LLMAccessMode           *string  `json:"llm_access_mode"`
	PublicBaseURL           *string  `json:"public_llm_base_url"`
	PublicProvider          *string  `json:"public_llm_provider"`
	PublicProtocol          *string  `json:"public_llm_protocol"`
	PublicAPIKey            *string  `json:"public_llm_api_key"`
	ClearPublicAPIKey       bool     `json:"clear_public_llm_api_key"`
	PublicModel             *string  `json:"public_llm_model"`
	PublicMemoryModel       *string  `json:"public_llm_memory_model"`
	PublicInquiryModel      *string  `json:"public_llm_inquiry_model"`
	PublicMaxContext        *int     `json:"public_llm_max_context_tokens"`
	PublicMaxResponse       *int     `json:"public_llm_max_response_tokens"`
	PublicTemperature       *float64 `json:"public_llm_temperature"`
	PublicTopP              *float64 `json:"public_llm_top_p"`
	PublicStream            *bool    `json:"public_llm_stream_enabled"`
	PublicReasoning         *string  `json:"public_llm_reasoning_effort"`
	PublicMemoryReasoning   *string  `json:"public_llm_memory_reasoning_effort"`
	PublicInquiryReasoning  *string  `json:"public_llm_inquiry_reasoning_effort"`
}

func (s *Server) updateAdminSystemSettings(c *gin.Context) {
	var body adminSystemSettingsBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式无效"})
		return
	}
	settings, err := s.system.Get(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	if body.Enabled != nil {
		settings.RollSystemEnabled = *body.Enabled
	}
	if body.DifficultyMode != nil {
		if !allowedRollDifficulties[*body.DifficultyMode] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "判定难度必须为 easy、normal 或 hard"})
			return
		}
		settings.RollDifficultyMode = *body.DifficultyMode
	}
	if body.DetailedLLMLogging != nil {
		settings.DetailedLLMLoggingEnabled = *body.DetailedLLMLogging
	}
	if body.DiagnosticLogCleanup != nil {
		settings.DiagnosticLogCleanupEnabled = *body.DiagnosticLogCleanup
	}
	if body.DiagnosticRetentionDays != nil {
		if err := validateDiagnosticLogRetention(*body.DiagnosticRetentionDays); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		settings.DiagnosticLogRetentionDays = *body.DiagnosticRetentionDays
	}
	if body.LLMAccessMode != nil {
		settings.LLMAccessMode = *body.LLMAccessMode
	}
	if body.PublicBaseURL != nil {
		settings.PublicLLMBaseURL = *body.PublicBaseURL
	}
	if body.PublicProvider != nil {
		settings.PublicLLMProvider = strings.TrimSpace(*body.PublicProvider)
	}
	if body.PublicProtocol != nil {
		settings.PublicLLMProtocol = *body.PublicProtocol
	}
	if body.PublicModel != nil {
		settings.PublicLLMModel = *body.PublicModel
	}
	if body.PublicMemoryModel != nil {
		settings.PublicLLMMemoryModel = *body.PublicMemoryModel
	}
	if body.PublicInquiryModel != nil {
		settings.PublicLLMInquiryModel = *body.PublicInquiryModel
	}
	if body.PublicMaxContext != nil {
		settings.PublicLLMMaxContextTokens = *body.PublicMaxContext
	}
	if body.PublicMaxResponse != nil {
		settings.PublicLLMMaxResponseTokens = *body.PublicMaxResponse
	}
	if body.PublicTemperature != nil {
		settings.PublicLLMTemperature = *body.PublicTemperature
	}
	if body.PublicTopP != nil {
		settings.PublicLLMTopP = *body.PublicTopP
	}
	if body.PublicStream != nil {
		settings.PublicLLMStreamEnabled = *body.PublicStream
	}
	if body.PublicReasoning != nil {
		settings.PublicLLMReasoningEffort = strings.TrimSpace(*body.PublicReasoning)
	}
	if body.PublicMemoryReasoning != nil {
		settings.PublicLLMMemoryReasoningEffort = strings.TrimSpace(*body.PublicMemoryReasoning)
	}
	if body.PublicInquiryReasoning != nil {
		settings.PublicLLMInquiryReasoningEffort = strings.TrimSpace(*body.PublicInquiryReasoning)
	}
	if s.llmSettings != nil {
		if strings.TrimSpace(settings.LLMAccessMode) == llmsettings.AccessModePlatformManaged {
			candidate := &storage.LLMSettings{
				Provider: settings.PublicLLMProvider, BaseURL: settings.PublicLLMBaseURL, Protocol: settings.PublicLLMProtocol,
				Model: settings.PublicLLMModel, MaxContextTokens: settings.PublicLLMMaxContextTokens,
				MaxResponseTokens: settings.PublicLLMMaxResponseTokens,
				Temperature:       settings.PublicLLMTemperature, TopP: settings.PublicLLMTopP,
				ReasoningEffort:        settings.PublicLLMReasoningEffort,
				MemoryReasoningEffort:  settings.PublicLLMMemoryReasoningEffort,
				InquiryReasoningEffort: settings.PublicLLMInquiryReasoningEffort,
			}
			if err := validateLLMSettings(candidate); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
		}
		updated, updateErr := s.llmSettings.UpdatePlatform(c.Request.Context(), settings, body.PublicAPIKey, body.ClearPublicAPIKey)
		if updateErr != nil {
			status := http.StatusBadRequest
			if updateErr == llmsettings.ErrEncryptionUnavailable {
				status = http.StatusServiceUnavailable
			}
			c.JSON(status, gin.H{"error": updateErr.Error()})
			return
		}
		c.JSON(http.StatusOK, updated)
		return
	}
	if err := s.system.Save(c.Request.Context(), settings); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, settings)
}

func validateDiagnosticLogRetention(days int) error {
	if days < 1 || days > 3650 {
		return fmt.Errorf("诊断日志保留天数必须在 1 到 3650 之间")
	}
	return nil
}

// fetchAdminPublicModels proxies the administrator's write-only public key to
// the configured provider. It deliberately has a separate route from the
// player endpoint so platform mode never leaks a key to users.
func (s *Server) fetchAdminPublicModels(c *gin.Context) {
	if s.llmSettings == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "LLM 设置服务未配置"})
		return
	}
	var body struct {
		BaseURL string  `json:"base_url"`
		APIKey  *string `json:"api_key"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式无效"})
		return
	}
	baseURL := strings.TrimRight(strings.TrimSpace(body.BaseURL), "/")
	if baseURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "base_url 不能为空"})
		return
	}
	key := ""
	if body.APIKey != nil {
		key = strings.TrimSpace(*body.APIKey)
	}
	if key == "" {
		var err error
		key, err = s.llmSettings.PlatformAPIKey(c.Request.Context())
		if err != nil {
			fail(c, err)
			return
		}
	}
	models, err := fetchProviderModels(c, baseURL, key)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"models": models})
}

func fetchProviderModels(c *gin.Context, baseURL, apiKey string) ([]string, error) {
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("LLM 服务返回错误: %s", strings.TrimSpace(string(body)))
	}
	var result struct {
		Data []struct {
			ID    string `json:"id"`
			Model string `json:"model"`
			Name  string `json:"name"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	models := make([]string, 0, len(result.Data))
	for _, item := range result.Data {
		id := item.ID
		if id == "" {
			id = item.Model
		}
		if id == "" {
			id = item.Name
		}
		if id != "" {
			models = append(models, id)
		}
	}
	return models, nil
}

type adminLLMRequestLogSummary struct {
	ID                         uuid.UUID  `json:"id"`
	ConversationID             *uuid.UUID `json:"conversation_id,omitempty"`
	MessageID                  *uuid.UUID `json:"message_id,omitempty"`
	Purpose                    string     `json:"purpose"`
	AttemptID                  *uuid.UUID `json:"attempt_id,omitempty"`
	MemoryJobID                *uuid.UUID `json:"memory_job_id,omitempty"`
	MemoryID                   *uuid.UUID `json:"memory_id,omitempty"`
	InquiryID                  *uuid.UUID `json:"inquiry_id,omitempty"`
	InquirySnapshotID          *uuid.UUID `json:"inquiry_snapshot_id,omitempty"`
	InquirySnapshotBranchEpoch int64      `json:"inquiry_snapshot_branch_epoch"`
	InquirySnapshotRevision    int64      `json:"inquiry_snapshot_revision"`
	Model                      string     `json:"model"`
	Stream                     bool       `json:"stream"`
	ResponseStatus             *int       `json:"response_status,omitempty"`
	FinishReason               string     `json:"finish_reason,omitempty"`
	ReceivedDone               bool       `json:"received_done"`
	Success                    bool       `json:"success"`
	AuditTruncated             bool       `json:"audit_truncated"`
	Error                      string     `json:"error,omitempty"`
	DurationMS                 int64      `json:"duration_ms"`
	CreatedAt                  string     `json:"created_at"`
	SettingsSource             string     `json:"settings_source"`
}

type adminContextBuildLogSummary struct {
	ID                       uuid.UUID  `json:"id"`
	ConversationID           uuid.UUID  `json:"conversation_id"`
	TriggerMessageID         *uuid.UUID `json:"trigger_message_id,omitempty"`
	Model                    string     `json:"model"`
	MaxContextTokens         int        `json:"max_context_tokens"`
	ReservedResponseTokens   int        `json:"reserved_response_tokens"`
	UsedPromptTokens         int        `json:"used_prompt_tokens"`
	IncludedMessageCount     int        `json:"included_message_count"`
	ActivatedWorldEntryCount int        `json:"activated_world_entry_count"`
	TurnID                   *uuid.UUID `json:"turn_id,omitempty"`
	AttemptID                *uuid.UUID `json:"attempt_id,omitempty"`
	BranchEpoch              int64      `json:"branch_epoch"`
	MemoryID                 *uuid.UUID `json:"memory_id,omitempty"`
	MemoryLag                bool       `json:"memory_lag"`
	MemoryJobID              *uuid.UUID `json:"memory_job_id,omitempty"`
	CreatedAt                string     `json:"created_at"`
}

func summarizeAdminContextBuildLog(row storage.ContextBuildLog) adminContextBuildLogSummary {
	return adminContextBuildLogSummary{
		ID: row.ID, ConversationID: row.ConversationID, TriggerMessageID: row.TriggerMessageID,
		Model: row.Model, MaxContextTokens: row.MaxContextTokens, ReservedResponseTokens: row.ReservedResponseTokens,
		UsedPromptTokens: row.UsedPromptTokens, IncludedMessageCount: len(row.IncludedMessageIDs),
		ActivatedWorldEntryCount: len(row.ActivatedWorldEntryIDs), TurnID: row.TurnID, AttemptID: row.AttemptID,
		BranchEpoch: row.BranchEpoch, MemoryID: row.MemoryID, MemoryLag: row.MemoryLag,
		MemoryJobID: row.MemoryJobID, CreatedAt: row.CreatedAt.Format(time.RFC3339Nano),
	}
}

func summarizeAdminLLMRequestLog(row storage.LLMRequestLog) adminLLMRequestLogSummary {
	return adminLLMRequestLogSummary{
		ID: row.ID, ConversationID: row.ConversationID, MessageID: row.MessageID,
		Purpose: row.Purpose, AttemptID: row.AttemptID, MemoryJobID: row.MemoryJobID, MemoryID: row.MemoryID,
		InquiryID: row.InquiryID, InquirySnapshotID: row.InquirySnapshotID,
		InquirySnapshotBranchEpoch: row.InquirySnapshotBranchEpoch, InquirySnapshotRevision: row.InquirySnapshotRevision,
		Model: row.Model, Stream: row.Stream, ResponseStatus: row.ResponseStatus,
		FinishReason: row.FinishReason, ReceivedDone: row.ReceivedDone, Success: row.Success, AuditTruncated: row.AuditTruncated,
		Error: row.Error, DurationMS: row.DurationMS, CreatedAt: row.CreatedAt.Format(time.RFC3339Nano),
		SettingsSource: row.SettingsSource,
	}
}

func (s *Server) listAdminLLMRequestLogs(c *gin.Context) {
	limit, err := optionalInt64(c.Query("limit"), 50)
	if err != nil || limit < 1 || limit > 200 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "limit 必须是 1 到 200 的整数"})
		return
	}
	if s.llmRequestLogs == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "模型请求日志服务未配置"})
		return
	}
	var conversationID *uuid.UUID
	if raw := c.Query("conversation_id"); raw != "" {
		id, parseErr := uuid.Parse(raw)
		if parseErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "conversation_id 无效"})
			return
		}
		conversationID = &id
	}
	rows, err := s.llmRequestLogs.List(c.Request.Context(), int(limit), conversationID)
	if err != nil {
		fail(c, err)
		return
	}
	out := make([]adminLLMRequestLogSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, summarizeAdminLLMRequestLog(row))
	}
	c.JSON(http.StatusOK, gin.H{"logs": out})
}

func (s *Server) getAdminLLMRequestLog(c *gin.Context) {
	if s.llmRequestLogs == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "模型请求日志服务未配置"})
		return
	}
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	row, err := s.llmRequestLogs.Get(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

func (s *Server) listAdminContextBuildLogs(c *gin.Context) {
	limit, err := optionalInt64(c.Query("limit"), 50)
	if err != nil || limit < 1 || limit > 200 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "limit 必须是 1 到 200 的整数"})
		return
	}
	if s.ctxLogs == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "上下文日志服务未配置"})
		return
	}
	var conversationID *uuid.UUID
	if raw := c.Query("conversation_id"); raw != "" {
		id, parseErr := uuid.Parse(raw)
		if parseErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "conversation_id 无效"})
			return
		}
		conversationID = &id
	}
	rows, err := s.ctxLogs.List(c.Request.Context(), int(limit), conversationID)
	if err != nil {
		fail(c, err)
		return
	}
	out := make([]adminContextBuildLogSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, summarizeAdminContextBuildLog(row))
	}
	c.JSON(http.StatusOK, gin.H{"logs": out})
}

func (s *Server) getAdminContextBuildLog(c *gin.Context) {
	if s.ctxLogs == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "上下文日志服务未配置"})
		return
	}
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	row, err := s.ctxLogs.Get(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, row)
}

func (s *Server) listAdminBuiltins(c *gin.Context) {
	items, err := s.builtins.ListBuiltins(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"builtins": items})
}

type adminBuiltinImportBody struct {
	Overwrite bool `json:"overwrite"`
}

func (s *Server) importAdminBuiltin(c *gin.Context) {
	slug, ok := paramSlug(c, "slug")
	if !ok {
		return
	}
	var body adminBuiltinImportBody
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式无效"})
			return
		}
	}
	var (
		result *challengeimport.ImportResult
		err    error
	)
	if body.Overwrite {
		result, err = s.builtins.OverwriteBuiltin(c.Request.Context(), slug)
	} else {
		result, err = s.builtins.ImportBuiltin(c.Request.Context(), slug)
	}
	if err != nil {
		fail(c, err)
		return
	}
	status := http.StatusOK
	if result.Created && !result.Overwritten {
		status = http.StatusCreated
	}
	c.JSON(status, result)
}
