package api

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func (s *Server) getLLMSettings(c *gin.Context) {
	st, err := s.settings.Get(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	st.HasAPIKey = st.APIKey != ""
	c.JSON(http.StatusOK, st)
}

// llmSettingsBody 更新 LLM 设置。api_key 为 nil 时保留原值，避免置空。
type llmSettingsBody struct {
	BaseURL            *string  `json:"base_url"`
	APIKey             *string  `json:"api_key"`
	Model              *string  `json:"model"`
	MaxContextTokens   *int     `json:"max_context_tokens"`
	MaxResponseTokens  *int     `json:"max_response_tokens"`
	Temperature        *float64 `json:"temperature"`
	TopP               *float64 `json:"top_p"`
	StreamEnabled      *bool    `json:"stream_enabled"`
	RollSystemEnabled  *bool    `json:"roll_system_enabled"`
	RollDifficultyMode *string  `json:"roll_difficulty_mode"`
}

func (s *Server) updateLLMSettings(c *gin.Context) {
	var b llmSettingsBody
	if err := c.ShouldBindJSON(&b); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	cur, err := s.settings.Get(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	if b.BaseURL != nil {
		cur.BaseURL = *b.BaseURL
	}
	if b.APIKey != nil {
		cur.APIKey = *b.APIKey
	}
	if b.Model != nil {
		cur.Model = *b.Model
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
	if b.RollSystemEnabled != nil {
		cur.RollSystemEnabled = *b.RollSystemEnabled
	}
	if b.RollDifficultyMode != nil {
		cur.RollDifficultyMode = *b.RollDifficultyMode
	}
	cur.UpdatedAt = time.Now()
	if err := s.settings.Save(c.Request.Context(), cur); err != nil {
		fail(c, err)
		return
	}
	cur.HasAPIKey = cur.APIKey != ""
	c.JSON(http.StatusOK, cur)
}

// fetchModelsBody 获取模型列表请求体。
type fetchModelsBody struct {
	BaseURL string  `json:"base_url"`
	APIKey  *string `json:"api_key"` // 可选，优先使用新填写的 key
}

// fetchModels 代理请求 LLM 服务商的 /models 接口。
func (s *Server) fetchModels(c *gin.Context) {
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
		settings, err := s.settings.Get(c.Request.Context())
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
