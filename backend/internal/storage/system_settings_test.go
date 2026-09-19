package storage

import (
	"testing"
	"time"
)

func TestDefaultSystemSettingsKeepsDetailedLLMLoggingDisabled(t *testing.T) {
	settings := DefaultSystemSettings()
	if settings.DetailedLLMLoggingEnabled {
		t.Fatal("detailed LLM logging must be disabled by default")
	}
}

func TestDefaultSystemSettingsCleansDiagnosticLogsAfterThirtyDays(t *testing.T) {
	settings := DefaultSystemSettings()
	if !settings.DiagnosticLogCleanupEnabled || settings.DiagnosticLogRetentionDays != 30 {
		t.Fatalf("diagnostic log cleanup defaults = enabled:%v days:%d", settings.DiagnosticLogCleanupEnabled, settings.DiagnosticLogRetentionDays)
	}
}

func TestDefaultSystemSettingsUsesUserManagedLLM(t *testing.T) {
	settings := DefaultSystemSettings()
	if settings.LLMAccessMode != "user_managed" {
		t.Fatalf("default LLM access mode = %q, want user_managed", settings.LLMAccessMode)
	}
}

func TestSystemSettingsCacheReturnsIndependentCopiesAndExpires(t *testing.T) {
	repo := &SystemSettingsRepo{}
	now := time.Now()
	repo.storeCached(&SystemSettings{ID: 1, RollDifficultyMode: "normal"}, now)
	first := repo.loadCached(now.Add(time.Second))
	first.RollDifficultyMode = "hard"
	second := repo.loadCached(now.Add(2 * time.Second))
	if second == nil || second.RollDifficultyMode != "normal" {
		t.Fatalf("cached settings were mutated through returned copy: %+v", second)
	}
	if got := repo.loadCached(now.Add(systemSettingsCacheTTL)); got != nil {
		t.Fatalf("expired cache returned %+v", got)
	}
}
