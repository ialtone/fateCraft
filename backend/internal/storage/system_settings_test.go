package storage

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDefaultSystemSettingsKeepsDetailedLLMLoggingDisabled(t *testing.T) {
	settings := DefaultSystemSettings()
	if settings.DetailedLLMLoggingEnabled {
		t.Fatal("detailed LLM logging must be disabled by default")
	}
	if settings.DetailedLLMLoggingScope != "all" || settings.DetailedLLMLoggingTargetID != nil {
		t.Fatal("default scope must be all without a target")
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
	if !settings.ClientLLMRouteEnabled {
		t.Fatal("client route choice must be enabled by default")
	}
}

func TestSystemSettingsCacheReturnsIndependentCopiesAndExpires(t *testing.T) {
	repo := &SystemSettingsRepo{}
	now := time.Now()
	target := uuid.New()
	original := target
	repo.storeCached(&SystemSettings{ID: 1, RollDifficultyMode: "normal", DetailedLLMLoggingScope: "user", DetailedLLMLoggingTargetID: &target}, now)
	target = uuid.New()
	first := repo.loadCached(now.Add(time.Second))
	if *first.DetailedLLMLoggingTargetID != original {
		t.Fatal("cache aliases input target")
	}
	*first.DetailedLLMLoggingTargetID = uuid.New()
	first.RollDifficultyMode = "hard"
	second := repo.loadCached(now.Add(2 * time.Second))
	if second == nil || second.RollDifficultyMode != "normal" || *second.DetailedLLMLoggingTargetID != original {
		t.Fatalf("cached settings were mutated through returned copy: %+v", second)
	}
	if got := repo.loadCached(now.Add(systemSettingsCacheTTL)); got != nil {
		t.Fatalf("expired cache returned %+v", got)
	}
}
