package storage

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SettingsRepo 负责 LLM 设置单行（运行配置，仍存数据库；模板数据见 internal/filestore）。
type SettingsRepo struct{ db *gorm.DB }

func NewSettingsRepo(db *gorm.DB) *SettingsRepo { return &SettingsRepo{db: db} }

// Get 返回 LLM 设置；不存在时返回内置默认值（不落库）。
func (r *SettingsRepo) Get(ctx context.Context) (*LLMSettings, error) {
	var s LLMSettings
	err := r.db.WithContext(ctx).First(&s, "id = ?", 1).Error
	if err == gorm.ErrRecordNotFound {
		return &LLMSettings{
			ID:                1,
			BaseURL:           "https://api.openai.com/v1",
			Model:             "gpt-4o-mini",
			MaxContextTokens:  16000,
			MaxResponseTokens: 1024,
			Temperature:       0.8,
			TopP:              1.0,
			StreamEnabled:     true,
		}, nil
	}
	if err != nil {
		return nil, err
	}
	s.HasAPIKey = s.APIKey != ""
	return &s, nil
}

// Save 以 id=1 upsert LLM 设置（行不存在时插入）。
func (r *SettingsRepo) Save(ctx context.Context, s *LLMSettings) error {
	s.ID = 1
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"base_url", "api_key", "model", "max_context_tokens",
			"max_response_tokens", "temperature", "top_p", "stream_enabled", "updated_at",
		}),
	}).Create(s).Error
}
