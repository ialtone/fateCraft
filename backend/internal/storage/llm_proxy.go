package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SaveLLMProxy updates only the LLM outbound proxy group. Generic system
// settings saves deliberately omit these columns so two tabs cannot overwrite
// each other. expectedRevision is the llm_proxy_revision the caller last read.
func (r *SystemSettingsRepo) SaveLLMProxy(ctx context.Context, next *SystemSettings, expectedRevision int64) (*SystemSettings, error) {
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()

	next.ID = 1
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var previous SystemSettings
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&previous, "id = ?", 1).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrSettingsConflict
			}
			return err
		}
		if previous.LLMProxyRevision != expectedRevision {
			return ErrSettingsConflict
		}
		changes := changedFields(llmProxySnapshot(&previous), llmProxySnapshot(next))
		secretChange(changes, "llm_proxy_password", previous.LLMProxyPasswordEncrypted, next.LLMProxyPasswordEncrypted)
		next.LLMProxyRevision = previous.LLMProxyRevision + 1
		changes["llm_proxy_revision"] = JSONMap{"before": previous.LLMProxyRevision, "after": next.LLMProxyRevision}
		next.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond)
		updates := map[string]any{
			"llm_proxy_enabled":            next.LLMProxyEnabled,
			"llm_proxy_url":                next.LLMProxyURL,
			"llm_proxy_username":           next.LLMProxyUsername,
			"llm_proxy_password_encrypted": next.LLMProxyPasswordEncrypted,
			"llm_proxy_ca_pem":             next.LLMProxyCAPEM,
			"llm_proxy_revision":           next.LLMProxyRevision,
			"updated_at":                   next.UpdatedAt,
		}
		if err := tx.Model(&SystemSettings{}).Where("id = ?", 1).Updates(updates).Error; err != nil {
			return err
		}
		// Keep the freshly locked non-proxy fields in the cache. A concurrent
		// generic settings save only updates other columns, so reusing the
		// caller's pre-read struct here would roll those fields back in memory.
		previous.LLMProxyEnabled = next.LLMProxyEnabled
		previous.LLMProxyURL = next.LLMProxyURL
		previous.LLMProxyUsername = next.LLMProxyUsername
		previous.LLMProxyPasswordEncrypted = next.LLMProxyPasswordEncrypted
		previous.LLMProxyCAPEM = next.LLMProxyCAPEM
		previous.LLMProxyRevision = next.LLMProxyRevision
		previous.UpdatedAt = next.UpdatedAt
		*next = previous
		return writeConfigAudit(ctx, tx, "llm_proxy_change", changes)
	})
	if errors.Is(err, ErrSettingsConflict) {
		r.InvalidateCache()
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	r.storeCached(next, time.Now())
	return cloneSystemSettings(next), nil
}

func llmProxySnapshot(s *SystemSettings) JSONMap {
	return JSONMap{
		"llm_proxy_enabled":        s.LLMProxyEnabled,
		"llm_proxy_url":            s.LLMProxyURL,
		"llm_proxy_username":       s.LLMProxyUsername,
		"llm_proxy_ca_configured":  s.LLMProxyCAPEM != "",
		"llm_proxy_ca_fingerprint": caPEMFingerprint(s.LLMProxyCAPEM),
	}
}

// caPEMFingerprint records CA changes without copying a potentially large PEM
// into the audit log.
func caPEMFingerprint(pem string) string {
	if pem == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(pem))
	return hex.EncodeToString(sum[:])
}
