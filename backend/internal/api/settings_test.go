package api

import (
	"strings"
	"testing"

	"github.com/ialtone/fatecraft/internal/storage"
)

func TestValidateLLMSettingsRejectsImpossibleTokenBudget(t *testing.T) {
	settings := validLLMSettings()
	settings.MaxContextTokens = 4096
	settings.MaxResponseTokens = 4096 - 256
	if err := validateLLMSettings(settings); err == nil || !strings.Contains(err.Error(), "格式预留") {
		t.Fatalf("expected reserve validation error, got %v", err)
	}
}

func TestValidateLLMSettingsAcceptsSmallAndModernWindows(t *testing.T) {
	for _, maxContext := range []int{16000, 128000, 2_000_000} {
		settings := validLLMSettings()
		settings.MaxContextTokens = maxContext
		if err := validateLLMSettings(settings); err != nil {
			t.Fatalf("max context %d rejected: %v", maxContext, err)
		}
	}
}

func TestValidateLLMSettingsRejectsUnknownProtocol(t *testing.T) {
	settings := validLLMSettings()
	settings.Protocol = "legacy"
	if err := validateLLMSettings(settings); err == nil || !strings.Contains(err.Error(), "protocol") {
		t.Fatalf("expected protocol validation error, got %v", err)
	}
}

func TestValidateDiagnosticLogRetention(t *testing.T) {
	for _, days := range []int{1, 30, 3650} {
		if err := validateDiagnosticLogRetention(days); err != nil {
			t.Fatalf("retention %d rejected: %v", days, err)
		}
	}
	for _, days := range []int{0, 3651} {
		if err := validateDiagnosticLogRetention(days); err == nil {
			t.Fatalf("retention %d accepted", days)
		}
	}
}

func validLLMSettings() *storage.LLMSettings {
	return &storage.LLMSettings{
		BaseURL: "https://example.test/v1", Model: "model",
		MaxContextTokens: 128000, MaxResponseTokens: 4096,
		Temperature: 0.8, TopP: 1,
	}
}
