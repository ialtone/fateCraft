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
	return &settings, nil
}

func (r *SystemSettingsRepo) Save(ctx context.Context, settings *SystemSettings) error {
	settings.ID = 1
	settings.UpdatedAt = time.Now()
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"roll_system_enabled", "roll_difficulty_mode", "updated_at"}),
	}).Create(settings).Error
}
