package storage

import "testing"

func TestDefaultSystemSettingsKeepsDetailedLLMLoggingDisabled(t *testing.T) {
	settings := DefaultSystemSettings()
	if settings.DetailedLLMLoggingEnabled {
		t.Fatal("detailed LLM logging must be disabled by default")
	}
}

func TestDefaultSystemSettingsUsesUserManagedLLM(t *testing.T) {
	settings := DefaultSystemSettings()
	if settings.LLMAccessMode != "user_managed" {
		t.Fatalf("default LLM access mode = %q, want user_managed", settings.LLMAccessMode)
	}
}
