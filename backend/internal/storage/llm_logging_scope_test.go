package storage

import (
	"testing"

	"github.com/google/uuid"
)

func TestDetailedLLMLoggingScopeMatchesTrustedAttribution(t *testing.T) {
	user, conv, other, zero := uuid.New(), uuid.New(), uuid.New(), uuid.Nil
	for _, tt := range []struct {
		name, scope, player, conversation string
		target                            *uuid.UUID
		enabled, valid, capture           bool
	}{
		{"off", "all", user.String(), conv.String(), nil, false, true, false},
		{"all", "all", "", "", nil, true, true, true},
		{"user", "user", user.String(), conv.String(), &user, true, true, true},
		{"user diagnostic", "user", user.String(), "", &user, true, true, true},
		{"other user", "user", other.String(), conv.String(), &user, true, true, false},
		{"missing user", "user", "", conv.String(), &user, true, true, false},
		{"conversation", "conversation", user.String(), conv.String(), &conv, true, true, true},
		{"other conversation", "conversation", user.String(), other.String(), &conv, true, true, false},
		{"no conversation diagnostic", "conversation", user.String(), "", &conv, true, true, false},
		{"disabled target", "user", user.String(), conv.String(), &user, false, true, false},
		{"unknown scope", "invalid", user.String(), conv.String(), &user, true, false, false},
		{"missing target", "user", user.String(), conv.String(), nil, true, false, false},
		{"zero target", "conversation", user.String(), zero.String(), &zero, true, false, false},
		{"all with target", "all", user.String(), conv.String(), &user, true, false, false},
		{"bad attribution", "user", "not-a-uuid", conv.String(), &user, true, true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := &SystemSettings{DetailedLLMLoggingEnabled: tt.enabled, DetailedLLMLoggingScope: tt.scope, DetailedLLMLoggingTargetID: tt.target}
			if valid := s.ValidateDetailedLLMLoggingScope() == nil; valid != tt.valid {
				t.Fatalf("valid=%v, want %v", valid, tt.valid)
			}
			if got := s.CaptureDetailedLLMLog(tt.player, tt.conversation); got != tt.capture {
				t.Fatalf("capture=%v, want %v", got, tt.capture)
			}
		})
	}
	var missing *SystemSettings
	if missing.CaptureDetailedLLMLog(user.String(), conv.String()) {
		t.Fatal("missing settings must not capture bodies")
	}
}
