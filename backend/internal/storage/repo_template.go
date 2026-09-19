package storage

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SettingsRepo 负责按用户隔离的 LLM 运行配置。
type SettingsRepo struct{ db *gorm.DB }

func NewSettingsRepo(db *gorm.DB) *SettingsRepo { return &SettingsRepo{db: db} }

const (
	DefaultLLMMaxContextTokens  = 128000
	DefaultLLMMaxResponseTokens = 16384
	DefaultLLMProtocol          = "responses"
)

// Get 返回指定用户的 LLM 设置；不存在时返回内置默认值（不落库）。
func (r *SettingsRepo) Get(ctx context.Context, playerID string) (*LLMSettings, error) {
	var s LLMSettings
	err := r.db.WithContext(ctx).First(&s, "player_id = ?", playerID).Error
	if err == gorm.ErrRecordNotFound {
		return &LLMSettings{
			PlayerID:         playerID,
			Provider:         "openai",
			BaseURL:          "https://api.openai.com/v1",
			Protocol:         DefaultLLMProtocol,
			MaxContextTokens: DefaultLLMMaxContextTokens,
			// 为长篇叙事和复杂工具参数保留充足的单步输出空间。
			MaxResponseTokens: DefaultLLMMaxResponseTokens,
			Temperature:       0.8,
			TopP:              1.0,
			StreamEnabled:     true,
			SettingsRevision:  1,
		}, nil
	}
	if err != nil {
		return nil, err
	}
	if s.Protocol == "" {
		s.Protocol = DefaultLLMProtocol
	}
	if s.Provider == "" {
		s.Provider = "openai"
	}
	s.HasAPIKey = s.APIKeyEncrypted != ""
	return &s, nil
}

// Save 以 player_id upsert LLM 设置（行不存在时插入）。
func (r *SettingsRepo) Save(ctx context.Context, playerID string, s *LLMSettings) error {
	s.PlayerID = playerID
	if s.Protocol == "" {
		s.Protocol = DefaultLLMProtocol
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current LLMSettings
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "player_id = ?", playerID).Error
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			s.SettingsRevision = 1
		case err != nil:
			return err
		case sameLLMInvocationSettings(&current, s):
			s.SettingsRevision = current.SettingsRevision
			if s.SettingsRevision <= 0 {
				s.SettingsRevision = 1
			}
		default:
			s.SettingsRevision = current.SettingsRevision + 1
			if s.SettingsRevision <= 1 {
				s.SettingsRevision = 2
			}
		}
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "player_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"provider", "base_url", "protocol", "api_key_encrypted", "model", "memory_model", "inquiry_model", "max_context_tokens",
				"max_response_tokens", "temperature", "top_p", "stream_enabled", "reasoning_effort",
				"memory_reasoning_effort", "inquiry_reasoning_effort", "settings_revision", "updated_at",
			}),
		}).Create(s).Error
	})
}

func sameLLMInvocationSettings(a, b *LLMSettings) bool {
	return a.Provider == b.Provider && a.BaseURL == b.BaseURL && a.Protocol == b.Protocol && a.APIKeyEncrypted == b.APIKeyEncrypted &&
		a.Model == b.Model && a.MemoryModel == b.MemoryModel && a.InquiryModel == b.InquiryModel &&
		a.MaxContextTokens == b.MaxContextTokens && a.MaxResponseTokens == b.MaxResponseTokens &&
		a.Temperature == b.Temperature && a.TopP == b.TopP && a.StreamEnabled == b.StreamEnabled &&
		a.ReasoningEffort == b.ReasoningEffort && a.MemoryReasoningEffort == b.MemoryReasoningEffort &&
		a.InquiryReasoningEffort == b.InquiryReasoningEffort
}
