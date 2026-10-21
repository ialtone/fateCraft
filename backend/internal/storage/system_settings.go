package storage

import (
	"context"
	"errors"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SystemSettingsRepo 负责全局玩法配置。设置不按玩家隔离，只能通过管理员 API 修改。
type SystemSettingsRepo struct {
	db       *gorm.DB
	mu       sync.RWMutex
	reloadMu sync.Mutex
	cached   *SystemSettings
	cachedAt time.Time
}

func NewSystemSettingsRepo(db *gorm.DB) *SystemSettingsRepo { return &SystemSettingsRepo{db: db} }

func (r *SystemSettingsRepo) Get(ctx context.Context) (*SystemSettings, error) {
	if cached := r.loadCached(time.Now()); cached != nil {
		return cached, nil
	}
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	if cached := r.loadCached(time.Now()); cached != nil {
		return cached, nil
	}
	var settings SystemSettings
	err := r.db.WithContext(ctx).First(&settings, "id = ?", 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		settings = *DefaultSystemSettings()
		r.storeCached(&settings, time.Now())
		return cloneSystemSettings(&settings), nil
	}
	if err != nil {
		return nil, err
	}
	r.storeCached(&settings, time.Now())
	return cloneSystemSettings(&settings), nil
}

func (r *SystemSettingsRepo) Save(ctx context.Context, settings *SystemSettings) error {
	if err := settings.ValidateDetailedLLMLoggingScope(); err != nil {
		return err
	}
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	settings.ID = 1
	expected := settings.UpdatedAt
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var previous SystemSettings
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&previous, "id = ?", 1).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if configActor(ctx) != nil && !previous.UpdatedAt.Equal(expected) {
			return ErrSettingsConflict
		}
		// This save owns only the general settings group. Preserve the locked
		// proxy values in both the insert struct and the post-commit cache.
		settings.LLMProxyEnabled = previous.LLMProxyEnabled
		settings.LLMProxyURL = previous.LLMProxyURL
		settings.LLMProxyUsername = previous.LLMProxyUsername
		settings.LLMProxyPasswordEncrypted = previous.LLMProxyPasswordEncrypted
		settings.LLMProxyCAPEM = previous.LLMProxyCAPEM
		settings.LLMProxyRevision = previous.LLMProxyRevision
		// Validate new targets and re-enabling in this transaction. A deleted target
		// must not prevent an administrator from turning capture off.
		if err := validateDetailedLLMLoggingTarget(tx, &previous, settings); err != nil {
			return err
		}
		changes := changedFields(systemSnapshot(&previous), systemSnapshot(settings))
		secretChange(changes, "public_llm_api_key", previous.PublicLLMAPIKeyEncrypted, settings.PublicLLMAPIKeyEncrypted)
		secretChange(changes, "public_llm_base_url", previous.PublicLLMBaseURL, settings.PublicLLMBaseURL)
		settings.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond)
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"installation_completed_at", "roll_system_enabled", "roll_difficulty_mode", "detailed_llm_logging_enabled",
				"detailed_llm_logging_scope", "detailed_llm_logging_target_id",
				"diagnostic_log_cleanup_enabled", "diagnostic_log_retention_days",
				"llm_access_mode", "client_llm_route_enabled", "public_llm_base_url", "public_llm_protocol", "public_llm_api_key_encrypted",
				"public_llm_model", "public_llm_memory_model", "public_llm_inquiry_model",
				"public_llm_max_context_tokens", "public_llm_max_response_tokens", "public_llm_temperature",
				"public_llm_top_p", "public_llm_stream_enabled", "public_llm_reasoning_effort",
				"public_llm_memory_reasoning_effort", "public_llm_inquiry_reasoning_effort",
				"public_llm_settings_revision", "updated_at",
			}),
		}).Create(settings).Error; err != nil {
			return err
		}
		return writeConfigAudit(ctx, tx, "system_settings_change", changes)
	})
	if errors.Is(err, ErrSettingsConflict) {
		r.InvalidateCache()
	}
	if err == nil {
		r.storeCached(settings, time.Now())
	}
	return err
}

// InvalidateCache makes an externally committed installation/role state visible
// immediately without waiting for the short settings cache TTL.
func (r *SystemSettingsRepo) InvalidateCache() {
	r.mu.Lock()
	r.cached = nil
	r.cachedAt = time.Time{}
	r.mu.Unlock()
}

const systemSettingsCacheTTL = 5 * time.Second

func (r *SystemSettingsRepo) loadCached(now time.Time) *SystemSettings {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.cached == nil || now.Sub(r.cachedAt) >= systemSettingsCacheTTL {
		return nil
	}
	return cloneSystemSettings(r.cached)
}

func (r *SystemSettingsRepo) storeCached(settings *SystemSettings, now time.Time) {
	r.mu.Lock()
	r.cached = cloneSystemSettings(settings)
	r.cachedAt = now
	r.mu.Unlock()
}

func cloneSystemSettings(settings *SystemSettings) *SystemSettings {
	if settings == nil {
		return nil
	}
	clone := *settings
	if settings.DetailedLLMLoggingTargetID != nil {
		target := *settings.DetailedLLMLoggingTargetID
		clone.DetailedLLMLoggingTargetID = &target
	}
	return &clone
}
