package api

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/storage"
)

func TestDetailedLLMLoggingSettingsPatch(t *testing.T) {
	id := uuid.New()
	for _, tt := range []struct {
		name, patch, scope     string
		valid, target, enabled bool
	}{
		{"keep", `{}`, "user", true, true, true},
		{"off keeps target", `{"detailed_llm_logging_enabled":false}`, "user", true, true, false},
		{"all clears target", `{"detailed_llm_logging_scope":"all"}`, "all", true, false, true},
		{"switch requires new target", `{"detailed_llm_logging_scope":"conversation"}`, "", false, false, false},
		{"conversation", `{"detailed_llm_logging_scope":"conversation","detailed_llm_logging_target_id":"` + id.String() + `"}`, "conversation", true, true, true},
		{"unknown", `{"detailed_llm_logging_scope":"any"}`, "", false, false, false},
		{"zero", `{"detailed_llm_logging_target_id":"00000000-0000-0000-0000-000000000000"}`, "", false, false, false},
		{"invalid id", `{"detailed_llm_logging_target_id":"bad"}`, "", false, false, false},
		{"empty target", `{"detailed_llm_logging_target_id":""}`, "", false, false, false},
		{"all rejects target", `{"detailed_llm_logging_scope":"all","detailed_llm_logging_target_id":"` + id.String() + `"}`, "", false, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := storage.DefaultSystemSettings()
			s.DetailedLLMLoggingEnabled, s.DetailedLLMLoggingScope, s.DetailedLLMLoggingTargetID = true, "user", &id
			var body adminSystemSettingsBody
			if err := json.Unmarshal([]byte(tt.patch), &body); err != nil {
				t.Fatal(err)
			}
			err := applyDetailedLLMLoggingSettings(s, body)
			if (err == nil) != tt.valid {
				t.Fatalf("error=%v", err)
			}
			if !tt.valid {
				if s.DetailedLLMLoggingScope != "user" || s.DetailedLLMLoggingTargetID == nil || *s.DetailedLLMLoggingTargetID != id || !s.DetailedLLMLoggingEnabled {
					t.Fatal("invalid patch partially changed settings")
				}
				return
			}
			if s.DetailedLLMLoggingScope != tt.scope || (s.DetailedLLMLoggingTargetID != nil) != tt.target || s.DetailedLLMLoggingEnabled != tt.enabled {
				t.Fatalf("unexpected patch result: %+v", s)
			}
		})
	}
}
