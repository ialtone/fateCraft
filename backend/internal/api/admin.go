package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/identity"
	"github.com/ialtone/fatecraft/internal/llm"
	"github.com/ialtone/fatecraft/internal/llmsettings"
	"github.com/ialtone/fatecraft/internal/modelcapability"
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
	c.JSON(http.StatusOK, &adminSystemSettingsResponse{SystemSettings: settings, PublicLLMResolvedProfile: resolveLLMProfile(settings.PublicLLMBaseURL)})
}

// adminSystemSettingsResponse 在管理员系统设置上附加公共 base_url 的只读
// 档位识别结果。
type adminSystemSettingsResponse struct {
	*storage.SystemSettings
	PublicLLMResolvedProfile llmResolvedProfile `json:"public_llm_resolved_profile"`
}

type adminSystemSettingsBody struct {
	Enabled                    *bool    `json:"roll_system_enabled"`
	DifficultyMode             *string  `json:"roll_difficulty_mode"`
	DetailedLLMLogging         *bool    `json:"detailed_llm_logging_enabled"`
	DetailedLLMLoggingScope    *string  `json:"detailed_llm_logging_scope"`
	DetailedLLMLoggingTargetID *string  `json:"detailed_llm_logging_target_id"`
	DiagnosticLogCleanup       *bool    `json:"diagnostic_log_cleanup_enabled"`
	DiagnosticRetentionDays    *int     `json:"diagnostic_log_retention_days"`
	LLMAccessMode              *string  `json:"llm_access_mode"`
	ClientLLMRouteEnabled      *bool    `json:"client_llm_route_enabled"`
	PublicBaseURL              *string  `json:"public_llm_base_url"`
	PublicProtocol             *string  `json:"public_llm_protocol"`
	PublicAPIKey               *string  `json:"public_llm_api_key"`
	ClearPublicAPIKey          bool     `json:"clear_public_llm_api_key"`
	PublicModel                *string  `json:"public_llm_model"`
	PublicMemoryModel          *string  `json:"public_llm_memory_model"`
	PublicInquiryModel         *string  `json:"public_llm_inquiry_model"`
	PublicMaxContext           *int     `json:"public_llm_max_context_tokens"`
	PublicMaxResponse          *int     `json:"public_llm_max_response_tokens"`
	PublicTemperature          *float64 `json:"public_llm_temperature"`
	PublicTopP                 *float64 `json:"public_llm_top_p"`
	PublicStream               *bool    `json:"public_llm_stream_enabled"`
	PublicReasoning            *string  `json:"public_llm_reasoning_effort"`
	PublicMemoryReasoning      *string  `json:"public_llm_memory_reasoning_effort"`
	PublicInquiryReasoning     *string  `json:"public_llm_inquiry_reasoning_effort"`
}

func (s *Server) updateAdminSystemSettings(c *gin.Context) {
	var body adminSystemSettingsBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求格式无效"})
		return
	}
	actorValue, _ := c.Get(authUserKey)
	actor, _ := actorValue.(*storage.User)
	if actor != nil {
		c.Request = c.Request.WithContext(identity.WithPlayerID(c.Request.Context(), actor.ID.String()))
	}
	if actor == nil || !actor.IsOwner() {
		if body.LLMAccessMode != nil || body.ClientLLMRouteEnabled != nil || body.PublicBaseURL != nil ||
			body.PublicProtocol != nil || body.PublicAPIKey != nil || body.ClearPublicAPIKey ||
			body.PublicModel != nil || body.PublicMemoryModel != nil || body.PublicInquiryModel != nil ||
			body.PublicMaxContext != nil || body.PublicMaxResponse != nil || body.PublicTemperature != nil ||
			body.PublicTopP != nil || body.PublicStream != nil || body.PublicReasoning != nil ||
			body.PublicMemoryReasoning != nil || body.PublicInquiryReasoning != nil {
			c.JSON(http.StatusForbidden, gin.H{"error": "只有平台拥有者可以修改平台 LLM 设置"})
			return
		}
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
	if err := applyDetailedLLMLoggingSettings(settings, body); err != nil {
		fail(c, err)
		return
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
	if body.ClientLLMRouteEnabled != nil {
		settings.ClientLLMRouteEnabled = *body.ClientLLMRouteEnabled
	}
	if body.PublicBaseURL != nil {
		settings.PublicLLMBaseURL = *body.PublicBaseURL
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
				BaseURL: settings.PublicLLMBaseURL, Protocol: settings.PublicLLMProtocol,
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
			fail(c, updateErr)
			return
		}
		// 关闭客户端直连开关时立刻关闭全部 Relay 连接，在途的客户端直连回合
		// 按连接断开收尾。切换公共 LLM 不改变任何用户的有效路由，不断开连接。
		// 注意：ClientLLMRouteEnabled 不参与 public_llm_settings_revision 判断（见
		// llmsettings.samePlatformInvocation），开关本身不递增 revision。
		if s.relays != nil && body.ClientLLMRouteEnabled != nil && !*body.ClientLLMRouteEnabled {
			s.relays.CloseAll()
		}
		c.JSON(http.StatusOK, &adminSystemSettingsResponse{SystemSettings: updated, PublicLLMResolvedProfile: resolveLLMProfile(updated.PublicLLMBaseURL)})
		return
	}
	if err := s.system.Save(c.Request.Context(), settings); err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, &adminSystemSettingsResponse{SystemSettings: settings, PublicLLMResolvedProfile: resolveLLMProfile(settings.PublicLLMBaseURL)})
}

func applyDetailedLLMLoggingSettings(settings *storage.SystemSettings, body adminSystemSettingsBody) error {
	next := *settings
	if body.DetailedLLMLogging != nil {
		next.DetailedLLMLoggingEnabled = *body.DetailedLLMLogging
	}
	if body.DetailedLLMLoggingScope != nil {
		scope := strings.TrimSpace(*body.DetailedLLMLoggingScope)
		if scope != next.DetailedLLMLoggingScope {
			// A user ID must never be silently reused as a conversation ID.
			next.DetailedLLMLoggingTargetID = nil
		}
		next.DetailedLLMLoggingScope = scope
	}
	if body.DetailedLLMLoggingTargetID != nil {
		raw := strings.TrimSpace(*body.DetailedLLMLoggingTargetID)
		next.DetailedLLMLoggingTargetID = nil
		if raw != "" {
			id, err := uuid.Parse(raw)
			if err != nil || id == uuid.Nil {
				return storage.ErrDetailedLLMLoggingScope
			}
			next.DetailedLLMLoggingTargetID = &id
		}
	}
	if err := next.ValidateDetailedLLMLoggingScope(); err != nil {
		return err
	}
	*settings = next
	return nil
}

func validateDiagnosticLogRetention(days int) error {
	if days < 1 || days > 3650 {
		return fmt.Errorf("诊断日志保留天数必须在 1 到 3650 之间")
	}
	return nil
}

// checkAdminPublicCapability 检测公共 LLM 配置（owner-only）。直接使用平台
// 配置经服务端直连执行，与 owner 本人的路由无关：owner 在 platform_managed
// 下选了客户端直连时，用户接口会返回其个人配置，不能复用。
func (s *Server) checkAdminPublicCapability(c *gin.Context) {
	if s.capabilities == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "模型能力服务未配置"})
		return
	}
	if s.llmSettings == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "LLM 设置服务未配置"})
		return
	}
	var body struct {
		Role modelcapability.Role `json:"role"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if body.Role != modelcapability.RoleNarrative && body.Role != modelcapability.RoleMemory && body.Role != modelcapability.RoleInquiry {
		c.JSON(http.StatusBadRequest, gin.H{"error": "role 必须是 narrative、memory 或 character_inquiry"})
		return
	}
	settings, err := s.llmSettings.PlatformRuntime(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	playerID := s.identity.CurrentPlayerID(c.Request.Context())
	checkCtx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Minute)
	defer cancel()
	playerUUID, err := uuid.Parse(playerID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "当前用户身份无效"})
		return
	}
	result := s.capabilities.Check(checkCtx, playerUUID, settings, body.Role)
	c.JSON(http.StatusOK, gin.H{"settings_revision": settings.SettingsRevision, "result": result})
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
	models, err := fetchUpstreamModels(c, baseURL, key)
	if err != nil {
		upstreamFailure(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"models": models})
}

func fetchUpstreamModels(c *gin.Context, baseURL, apiKey string) ([]string, error) {
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
		return nil, &llm.HTTPError{StatusCode: resp.StatusCode}
	}
	var result struct {
		Data []struct {
			ID    string `json:"id"`
			Model string `json:"model"`
			Name  string `json:"name"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&result); err != nil {
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
	TraceID                    *uuid.UUID      `json:"trace_id,omitempty"`
	Protocol                   string          `json:"protocol"`
	StartedAt                  string          `json:"started_at"`
	TransportAttempts          int             `json:"transport_attempts"`
	InputTokens                *int            `json:"input_tokens"`
	OutputTokens               *int            `json:"output_tokens"`
	BodyStatus                 string          `json:"body_status"`
	ErrorCode                  string          `json:"error_code,omitempty"`
	BodyMetadata               storage.JSONMap `json:"body_metadata"`
	ID                         uuid.UUID       `json:"id"`
	ConversationID             *uuid.UUID      `json:"conversation_id,omitempty"`
	MessageID                  *uuid.UUID      `json:"message_id,omitempty"`
	Purpose                    string          `json:"purpose"`
	AttemptID                  *uuid.UUID      `json:"attempt_id,omitempty"`
	MemoryJobID                *uuid.UUID      `json:"memory_job_id,omitempty"`
	MemoryID                   *uuid.UUID      `json:"memory_id,omitempty"`
	InquiryID                  *uuid.UUID      `json:"inquiry_id,omitempty"`
	InquirySnapshotID          *uuid.UUID      `json:"inquiry_snapshot_id,omitempty"`
	InquirySnapshotBranchEpoch int64           `json:"inquiry_snapshot_branch_epoch"`
	InquirySnapshotRevision    int64           `json:"inquiry_snapshot_revision"`
	Model                      string          `json:"model"`
	Stream                     bool            `json:"stream"`
	ResponseStatus             *int            `json:"response_status,omitempty"`
	FinishReason               string          `json:"finish_reason,omitempty"`
	ReceivedDone               bool            `json:"received_done"`
	Success                    bool            `json:"success"`
	// 列表状态列依赖这三个字段判断“传输失败 / 结构失败”，必须随摘要返回。
	TransportSuccess    bool     `json:"transport_success"`
	StructuredSuccess   bool     `json:"structured_success"`
	OutputMode          string   `json:"output_mode,omitempty"`
	AuditTruncated      bool     `json:"audit_truncated"`
	Error               string   `json:"error,omitempty"`
	DurationMS          int64    `json:"duration_ms"`
	CreatedAt           string   `json:"created_at"`
	SettingsSource      string   `json:"settings_source"`
	SettingsRevision    int64    `json:"settings_revision"`
	ContractName        string   `json:"contract_name,omitempty"`
	ContractVersion     string   `json:"contract_version,omitempty"`
	ContractFingerprint string   `json:"contract_fingerprint,omitempty"`
	StructuredErrorCode string   `json:"structured_error_code,omitempty"`
	OutputTypes         []string `json:"output_types"`
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
		TraceID: row.TraceID, Protocol: row.Protocol, TransportAttempts: row.TransportAttempts, InputTokens: row.InputTokens, OutputTokens: row.OutputTokens, BodyStatus: row.BodyStatus, ErrorCode: row.ErrorCode, BodyMetadata: row.BodyMetadata,
		ID: row.ID, ConversationID: row.ConversationID, MessageID: row.MessageID,
		Purpose: row.Purpose, AttemptID: row.AttemptID, MemoryJobID: row.MemoryJobID, MemoryID: row.MemoryID,
		InquiryID: row.InquiryID, InquirySnapshotID: row.InquirySnapshotID,
		InquirySnapshotBranchEpoch: row.InquirySnapshotBranchEpoch, InquirySnapshotRevision: row.InquirySnapshotRevision,
		Model: row.Model, Stream: row.Stream, ResponseStatus: row.ResponseStatus,
		FinishReason: row.FinishReason, ReceivedDone: row.ReceivedDone, Success: row.Success, AuditTruncated: row.AuditTruncated,
		TransportSuccess: row.TransportSuccess, StructuredSuccess: row.StructuredSuccess, OutputMode: row.OutputMode,
		Error: row.Error, DurationMS: row.DurationMS, CreatedAt: row.CreatedAt.Format(time.RFC3339Nano),
		SettingsSource: row.SettingsSource, SettingsRevision: row.SettingsRevision,
		StartedAt: row.StartedAt.Format(time.RFC3339Nano), ContractName: row.ContractName,
		ContractVersion: row.ContractVersion, ContractFingerprint: row.ContractFingerprint,
		StructuredErrorCode: row.StructuredErrorCode, OutputTypes: []string(row.OutputTypes),
	}
}

func (s *Server) listAdminLLMRequestLogs(c *gin.Context) {
	filter, ok := logQuery(c, "llm")
	if !ok {
		return
	}
	if s.llmRequestLogs == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "模型请求日志服务未配置"})
		return
	}
	rows, next, more, err := s.llmRequestLogs.List(c.Request.Context(), filter)
	if err != nil {
		fail(c, err)
		return
	}
	out := make([]adminLLMRequestLogSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, summarizeAdminLLMRequestLog(row))
	}
	c.JSON(http.StatusOK, gin.H{"logs": out, "next_cursor": next, "has_more": more})
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
	filter, ok := logQuery(c, "context")
	if !ok {
		return
	}
	if s.ctxLogs == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "上下文日志服务未配置"})
		return
	}
	rows, next, more, err := s.ctxLogs.List(c.Request.Context(), filter)
	if err != nil {
		fail(c, err)
		return
	}
	out := make([]adminContextBuildLogSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, summarizeAdminContextBuildLog(row))
	}
	c.JSON(http.StatusOK, gin.H{"logs": out, "next_cursor": next, "has_more": more})
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

func (s *Server) previewAdminBuiltin(c *gin.Context) {
	slug, ok := paramSlug(c, "slug")
	if !ok {
		return
	}
	preview, err := s.builtins.PreviewBuiltin(c.Request.Context(), slug, rewardActor(c))
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, preview)
}
