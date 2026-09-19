package storage

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SystemSettingsRepo 负责全局玩法配置。设置不按玩家隔离，只能通过管理员 API 修改。
type SystemSettingsRepo struct{ db *gorm.DB }

func NewSystemSettingsRepo(db *gorm.DB) *SystemSettingsRepo { return &SystemSettingsRepo{db: db} }

func (r *SystemSettingsRepo) Get(ctx context.Context) (*SystemSettings, error) {
	var settings SystemSettings
	err := r.db.WithContext(ctx).First(&settings, "id = ?", 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return DefaultSystemSettings(), nil
	}
	if err != nil {
		return nil, err
	}
	if settings.PublicLLMProvider == "" {
		settings.PublicLLMProvider = "openai"
	}
	return &settings, nil
}

func (r *SystemSettingsRepo) Save(ctx context.Context, settings *SystemSettings) error {
	settings.ID = 1
	settings.UpdatedAt = time.Now()
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"roll_system_enabled", "roll_difficulty_mode", "detailed_llm_logging_enabled",
			"llm_access_mode", "public_llm_base_url", "public_llm_provider", "public_llm_protocol", "public_llm_api_key_encrypted",
			"public_llm_model", "public_llm_memory_model", "public_llm_inquiry_model",
			"public_llm_max_context_tokens", "public_llm_max_response_tokens", "public_llm_temperature",
			"public_llm_top_p", "public_llm_stream_enabled", "public_llm_reasoning_effort",
			"public_llm_memory_reasoning_effort", "public_llm_inquiry_reasoning_effort",
			"public_llm_settings_revision", "updated_at",
		}),
	}).Create(settings).Error
}
