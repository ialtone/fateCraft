package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/contextbuilder"
	"github.com/ialtone/fatecraft/internal/llm"
	"github.com/ialtone/fatecraft/internal/llmsettings"
	"github.com/ialtone/fatecraft/internal/modelcapability"
	"github.com/ialtone/fatecraft/internal/storage"
)

const (
	minContextTokens  = 4096
	maxContextTokens  = 2_000_000
	minResponseTokens = 128
)

func (s *Server) getLLMSettings(c *gin.Context) {
	playerID := s.identity.CurrentPlayerID(c.Request.Context())
	var st *storage.LLMSettings
	var err error
	if s.llmSettings != nil {
		st, err = s.llmSettings.UserView(c.Request.Context(), playerID)
	} else {
		st, err = s.settings.Get(c.Request.Context(), playerID)
	}
	if err != nil {
		fail(c, err)
		return
	}
	if s.llmSettings == nil {
		st.HasAPIKey = st.APIKey != "" || st.APIKeyEncrypted != ""
		st.APIKey = ""
	}
	c.JSON(http.StatusOK, st)
}

// listLLMProviderProfiles keeps provider/API capability metadata server-owned.
// The frontend must not maintain a parallel compatibility matrix.
func (s *Server) listLLMProviderProfiles(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"providers": llm.Profiles()})
}

// llmSettingsBody 更新 LLM 设置。api_key 为 nil 时保留原值，避免置空。
// llm_route 为 nil 时保持当前路由；client 路由的 api_key 只写浏览器，不提交服务端。
type llmSettingsBody struct {
	Provider          *string  `json:"provider"`
	BaseURL           *string  `json:"base_url"`
	Protocol          *string  `json:"protocol"`
	APIKey            *string  `json:"api_key"`
	LLMRoute          *string  `json:"llm_route"`
	Model             *string  `json:"model"`
	MemoryModel       *string  `json:"memory_model"`
	InquiryModel      *string  `json:"inquiry_model"`
	MaxContextTokens  *int     `json:"max_context_tokens"`
	MaxResponseTokens *int     `json:"max_response_tokens"`
	Temperature       *float64 `json:"temperature"`
	TopP              *float64 `json:"top_p"`
	StreamEnabled     *bool    `json:"stream_enabled"`
	ReasoningEffort   *string  `json:"reasoning_effort"`
	MemoryReasoning   *string  `json:"memory_reasoning_effort"`
	InquiryReasoning  *string  `json:"inquiry_reasoning_effort"`
}

func (s *Server) updateLLMSettings(c *gin.Context) {
	playerID := s.identity.CurrentPlayerID(c.Request.Context())
	var b llmSettingsBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if s.llmSettings != nil {
		mode, modeErr := s.llmSettings.AccessMode(c.Request.Context())
		if modeErr != nil {
			fail(c, modeErr)
			return
		}
		if mode != llmsettings.AccessModeUserManaged {
			c.JSON(http.StatusForbidden, gin.H{"error": llmsettings.ErrUserSettingsDisabled.Error()})
			return
		}
	}
	var cur *storage.LLMSettings
	var err error
	if s.llmSettings != nil {
		cur, err = s.llmSettings.Get(c.Request.Context(), playerID)
	} else {
		cur, err = s.settings.Get(c.Request.Context(), playerID)
	}
	if err != nil {
		fail(c, err)
		return
	}
	if b.BaseURL != nil {
		cur.BaseURL = strings.TrimSpace(*b.BaseURL)
	}
	if b.Provider != nil {
		cur.Provider = strings.TrimSpace(*b.Provider)
	}
	if b.Protocol != nil {
		cur.Protocol = strings.TrimSpace(*b.Protocol)
	}
	apiKey := b.APIKey
	if b.Model != nil {
		cur.Model = strings.TrimSpace(*b.Model)
	}
	if b.MemoryModel != nil {
		cur.MemoryModel = strings.TrimSpace(*b.MemoryModel)
	}
	if b.InquiryModel != nil {
		cur.InquiryModel = strings.TrimSpace(*b.InquiryModel)
	}
	if b.MaxContextTokens != nil {
		cur.MaxContextTokens = *b.MaxContextTokens
	}
	if b.MaxResponseTokens != nil {
		cur.MaxResponseTokens = *b.MaxResponseTokens
	}
	if b.Temperature != nil {
		cur.Temperature = *b.Temperature
	}
	if b.TopP != nil {
		cur.TopP = *b.TopP
	}
	if b.StreamEnabled != nil {
		cur.StreamEnabled = *b.StreamEnabled
	}
	if b.ReasoningEffort != nil {
		cur.ReasoningEffort = strings.TrimSpace(*b.ReasoningEffort)
	}
	if b.MemoryReasoning != nil {
		cur.MemoryReasoningEffort = strings.TrimSpace(*b.MemoryReasoning)
	}
	if b.InquiryReasoning != nil {
		cur.InquiryReasoningEffort = strings.TrimSpace(*b.InquiryReasoning)
	}
	if !llm.IsValidProtocol(cur.Protocol) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "protocol 必须是 chat_completions 或 responses"})
		return
	}
	if cur.Protocol == "" {
		cur.Protocol = storage.DefaultLLMProtocol
	}
	if cur.Provider == "" {
		cur.Provider = "openai"
	}
	if err := validateLLMSettings(cur); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	cur.UpdatedAt = time.Now()
	if s.llmSettings != nil {
		route := cur.LLMRoute
		if b.LLMRoute != nil {
			route = *b.LLMRoute
		}
		updated, err := s.llmSettings.UpdateUser(c.Request.Context(), playerID, cur, apiKey, route)
		if err != nil {
			if err == llmsettings.ErrUserSettingsDisabled {
				c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
				return
			}
			if err == llmsettings.ErrMissingAPIKey || err == llmsettings.ErrInvalidLLMRoute {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			if err == llmsettings.ErrEncryptionUnavailable {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
				return
			}
			fail(c, err)
			return
		}
		// 切离 client 路由时主动关闭该玩家的 Relay 连接，在途请求按连接断开处理。
		if s.relays != nil && updated.LLMRoute != storage.LLMRouteClient {
			s.relays.ClosePlayer(playerID)
		}
		c.JSON(http.StatusOK, updated)
		return
	}
	if err := s.settings.Save(c.Request.Context(), playerID, cur); err != nil {
		fail(c, err)
		return
	}
	cur.HasAPIKey = cur.APIKey != ""
	cur.APIKey = ""
	c.JSON(http.StatusOK, cur)
}

func (s *Server) checkLLMCapabilities(c *gin.Context) {
	if s.capabilities == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "模型能力服务未配置"})
		return
	}
	playerID := s.identity.CurrentPlayerID(c.Request.Context())
	var settings *storage.LLMSettings
	var err error
	if s.llmSettings != nil {
		settings, err = s.llmSettings.Get(c.Request.Context(), playerID)
	} else {
		settings, err = s.settings.Get(c.Request.Context(), playerID)
	}
	if err != nil {
		fail(c, err)
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

func validateLLMSettings(settings *storage.LLMSettings) error {
	if !llm.IsValidProtocol(settings.Protocol) {
		return fmt.Errorf("protocol 必须是 chat_completions 或 responses")
	}
	if strings.TrimSpace(settings.BaseURL) == "" {
		return fmt.Errorf("base_url 不能为空")
	}
	if strings.TrimSpace(settings.Model) == "" {
		return fmt.Errorf("model 不能为空")
	}
	if settings.MaxContextTokens < minContextTokens || settings.MaxContextTokens > maxContextTokens {
		return fmt.Errorf("最大上下文 token 必须在 %d 到 %d 之间", minContextTokens, maxContextTokens)
	}
	if settings.MaxResponseTokens < minResponseTokens {
		return fmt.Errorf("最大回复 token 不能小于 %d", minResponseTokens)
	}
	if settings.MaxResponseTokens+contextbuilder.FormatReserve >= settings.MaxContextTokens {
		return fmt.Errorf("最大回复 token 加 %d token 格式预留后必须小于最大上下文 token", contextbuilder.FormatReserve)
	}
	if settings.Temperature < 0 || settings.Temperature > 2 {
		return fmt.Errorf("temperature 必须在 0 到 2 之间")
	}
	if settings.TopP < 0 || settings.TopP > 1 {
		return fmt.Errorf("top_p 必须在 0 到 1 之间")
	}
	for _, effort := range []string{settings.ReasoningEffort, settings.MemoryReasoningEffort, settings.InquiryReasoningEffort} {
		if effort != "" && !reasoningEffortPattern.MatchString(effort) {
			return fmt.Errorf("思考强度只能留空（跟随供应商默认），或使用不超过 32 个字符的字母、数字、点、下划线或连字符")
		}
	}
	return nil
}

// reasoningEffortPattern 允许自定义档位（如 ultra、minimal），只限制字符集
// 和长度，不做枚举白名单：各供应商取值不一，服务端保持原样透传。
var reasoningEffortPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,32}$`)

// fetchModelsBody 获取模型列表请求体。
type fetchModelsBody struct {
	BaseURL string  `json:"base_url"`
	APIKey  *string `json:"api_key"` // 可选，优先使用新填写的 key
}

// fetchModels 代理请求 LLM 服务商的 /models 接口。
// client 路由返回 409，由前端在浏览器直接请求 /models（Key 不上服务端）。
func (s *Server) fetchModels(c *gin.Context) {
	if s.llmSettings != nil {
		mode, err := s.llmSettings.AccessMode(c.Request.Context())
		if err != nil {
			fail(c, err)
			return
		}
		if mode != llmsettings.AccessModeUserManaged {
			c.JSON(http.StatusForbidden, gin.H{"error": "平台统一提供模型配置，用户不能修改或获取个人模型列表"})
			return
		}
		route, err := s.llmSettings.EffectiveRoute(c.Request.Context(), s.identity.CurrentPlayerID(c.Request.Context()))
		if err != nil {
			fail(c, err)
			return
		}
		if route == storage.LLMRouteClient {
			c.JSON(http.StatusConflict, gin.H{"error": "客户端直连模式请在浏览器直接获取模型列表", "code": "llm_route_client"})
			return
		}
	}
	var b fetchModelsBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if b.BaseURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "base_url 不能为空"})
		return
	}

	// 确定使用哪个 API Key
	apiKey := ""
	if b.APIKey != nil && *b.APIKey != "" {
		// 优先使用新填写的 key
		apiKey = *b.APIKey
	} else {
		// 否则使用数据库中保存的 key
		var settings *storage.LLMSettings
		var err error
		if s.llmSettings != nil {
			settings, err = s.llmSettings.Get(c.Request.Context(), s.identity.CurrentPlayerID(c.Request.Context()))
		} else {
			settings, err = s.settings.Get(c.Request.Context(), s.identity.CurrentPlayerID(c.Request.Context()))
		}
		if err == nil && settings.APIKey != "" {
			apiKey = settings.APIKey
		}
	}

	// 构造请求
	url := b.BaseURL
	if url[len(url)-1] != '/' {
		url += "/"
	}
	url += "models"

	req, err := http.NewRequestWithContext(c.Request.Context(), "GET", url, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建请求失败: " + err.Error()})
		return
	}

	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	// 发送请求
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "请求失败: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		c.JSON(http.StatusBadGateway, gin.H{"error": "LLM 服务返回错误: " + string(body)})
		return
	}

	// 解析响应
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "解析响应失败: " + err.Error()})
		return
	}

	// 提取模型列表（标准 OpenAI 格式：{ "data": [{"id": "model-name"}, ...] }）
	models := []string{}
	if data, ok := result["data"].([]any); ok {
		for _, item := range data {
			if m, ok := item.(map[string]any); ok {
				if id, ok := m["id"].(string); ok && id != "" {
					models = append(models, id)
				} else if name, ok := m["model"].(string); ok && name != "" {
					models = append(models, name)
				} else if name, ok := m["name"].(string); ok && name != "" {
					models = append(models, name)
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"models": models})
}
