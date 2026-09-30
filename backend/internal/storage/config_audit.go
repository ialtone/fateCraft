package storage

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/identity"
	"github.com/ialtone/fatecraft/internal/logging"
	"gorm.io/gorm"
)

var ErrSettingsConflict = errors.New("配置已被其他操作更新，请刷新后重试")

func configActor(ctx context.Context) *uuid.UUID {
	id, err := uuid.Parse(identity.NewContext().CurrentPlayerID(ctx))
	if err != nil {
		return nil
	}
	return &id
}

func writeConfigAudit(ctx context.Context, tx *gorm.DB, action string, changes JSONMap) error {
	actor := configActor(ctx)
	// Unauthenticated calls are internal initialization; HTTP callers always bind identity.
	if actor == nil || len(changes) == 0 {
		return nil
	}
	row := AdminAuditLog{ID: uuid.New(), ActorUserID: actor, Action: action, Result: "success", Metadata: JSONMap{"changes": changes}, CreatedAt: time.Now()}
	if id, err := uuid.Parse(logging.TraceID(ctx)); err == nil {
		row.TraceID = &id
	}
	return tx.Create(&row).Error
}

func changedFields(before, after JSONMap) JSONMap {
	changes := JSONMap{}
	for key, next := range after {
		if !reflect.DeepEqual(before[key], next) {
			changes[key] = JSONMap{"before": before[key], "after": next}
		}
	}
	return changes
}

func secretChange(changes JSONMap, key, old, next string) {
	if old == next {
		return
	}
	action := "replace"
	if old == "" {
		action = "set"
	}
	if next == "" {
		action = "clear"
	}
	changes[key] = JSONMap{"action": action}
}

func systemSnapshot(s *SystemSettings) JSONMap {
	return JSONMap{
		"roll_system_enabled": s.RollSystemEnabled, "roll_difficulty_mode": s.RollDifficultyMode,
		"detailed_llm_logging_enabled": s.DetailedLLMLoggingEnabled, "diagnostic_log_cleanup_enabled": s.DiagnosticLogCleanupEnabled, "diagnostic_log_retention_days": s.DiagnosticLogRetentionDays,
		"detailed_llm_logging_scope": s.DetailedLLMLoggingScope, "detailed_llm_logging_target_id": s.DetailedLLMLoggingTargetID,
		"llm_access_mode": s.LLMAccessMode, "client_llm_route_enabled": s.ClientLLMRouteEnabled, "public_llm_protocol": s.PublicLLMProtocol,
		"public_llm_model": s.PublicLLMModel, "public_llm_memory_model": s.PublicLLMMemoryModel, "public_llm_inquiry_model": s.PublicLLMInquiryModel,
		"public_llm_max_context_tokens": s.PublicLLMMaxContextTokens, "public_llm_max_response_tokens": s.PublicLLMMaxResponseTokens,
		"public_llm_temperature": s.PublicLLMTemperature, "public_llm_top_p": s.PublicLLMTopP, "public_llm_stream_enabled": s.PublicLLMStreamEnabled,
		"public_llm_reasoning_effort": s.PublicLLMReasoningEffort, "public_llm_memory_reasoning_effort": s.PublicLLMMemoryReasoningEffort, "public_llm_inquiry_reasoning_effort": s.PublicLLMInquiryReasoningEffort,
	}
}

func authSnapshot(s *AuthSettings) JSONMap {
	return JSONMap{"email_auth_enabled": s.EmailAuthEnabled, "linuxdo_login_enabled": s.LinuxDOLoginEnabled, "email_registration_enabled": s.EmailRegistrationEnabled, "email_verification_enabled": s.EmailVerificationEnabled, "invitation_required": s.InvitationRequired, "smtp_port": s.SMTPPort, "smtp_security": s.SMTPSecurity, "verification_code_ttl_minutes": s.VerificationCodeTTLMinutes, "verification_resend_seconds": s.VerificationResendSeconds}
}
