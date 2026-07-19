package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/ialtone/fatecraft/internal/llm"
	"github.com/ialtone/fatecraft/internal/storage"
)

func (s *Server) listSummaries(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	sums, err := s.memory.List(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"summaries": sums})
}

// 摘要生成的系统提示（doc 09 §摘要内容要求）。
const summarySystemPrompt = `你是一个剧情摘要助手。请把以下对话压缩为一段简洁但信息完整的中文摘要，必须保留：重要角色和关系、已发生事件、当前目标、未解决伏笔、获得或失去的物品、地点和世界状态、用户与 AI 达成的约定。只输出摘要正文，不要额外说明。`

func (s *Server) generateSummary(c *gin.Context) {
	id, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	ctx := c.Request.Context()
	history, err := s.chat.History(ctx, id, false)
	if err != nil {
		fail(c, err)
		return
	}
	if len(history) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "没有可用于摘要的历史"})
		return
	}

	var transcript strings.Builder
	for _, m := range history {
		transcript.WriteString(m.Role + ": " + m.Content + "\n")
	}

	settings, err := s.settings.Get(ctx)
	if err != nil {
		fail(c, err)
		return
	}
	apiKey := settings.APIKey
	if apiKey == "" {
		apiKey = s.apiKeyEnv
	}
	params := llm.Params{
		BaseURL: settings.BaseURL, APIKey: apiKey, Model: settings.Model,
		Temperature: 0.3, TopP: settings.TopP, MaxTokens: settings.MaxResponseTokens,
	}
	content, err := s.llmClient.Complete(ctx, params, []llm.Message{
		{Role: storage.RoleSystem, Content: summarySystemPrompt},
		{Role: storage.RoleUser, Content: transcript.String()},
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "生成摘要失败: " + err.Error()})
		return
	}

	lastID := history[len(history)-1].ID
	sum, err := s.memory.CreateVersion(ctx, id, strings.TrimSpace(content), &lastID)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, sum)
}

// editSummaryBody 编辑摘要正文。
type editSummaryBody struct {
	ConversationID string `json:"conversation_id"`
	Content        string `json:"content"`
}

func (s *Server) editSummary(c *gin.Context) {
	sumID, ok := paramUUID(c, "id")
	if !ok {
		return
	}
	var body editSummaryBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	convID, ok := paramUUIDFromString(c, body.ConversationID)
	if !ok {
		return
	}
	sum, err := s.memory.Edit(c.Request.Context(), convID, sumID, body.Content)
	if err != nil {
		fail(c, err)
		return
	}
	c.JSON(http.StatusOK, sum)
}
