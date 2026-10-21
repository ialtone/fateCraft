import type { AdminAuthSettings, AdminSystemSettings } from '@/types'

export const systemSettingFields = {
  gameplay: ['roll_system_enabled', 'roll_difficulty_mode'],
  llm: [
    'llm_access_mode',
    'client_llm_route_enabled',
    'public_llm_base_url',
    'public_llm_protocol',
    'public_llm_model',
    'public_llm_memory_model',
    'public_llm_inquiry_model',
    'public_llm_max_context_tokens',
    'public_llm_max_response_tokens',
    'public_llm_temperature',
    'public_llm_top_p',
    'public_llm_stream_enabled',
    'public_llm_reasoning_effort',
    'public_llm_memory_reasoning_effort',
    'public_llm_inquiry_reasoning_effort',
  ],
} as const satisfies Record<string, readonly (keyof AdminSystemSettings)[]>

export const logSettingFields = [
  'detailed_llm_logging_enabled',
  'detailed_llm_logging_scope',
  'detailed_llm_logging_target_id',
  'diagnostic_log_cleanup_enabled',
  'diagnostic_log_retention_days',
] as const satisfies readonly (keyof AdminSystemSettings)[]

export const authSettingFields = {
  login: [
    'email_auth_enabled',
    'linuxdo_login_enabled',
    'email_registration_enabled',
    'email_verification_enabled',
    'invitation_required',
  ],
  smtp: ['smtp_host', 'smtp_port', 'smtp_security', 'smtp_username', 'sender_email', 'sender_name'],
  verification: [
    'verification_subject',
    'verification_template',
    'verification_code_ttl_minutes',
    'verification_resend_seconds',
  ],
} as const satisfies Record<string, readonly (keyof AdminAuthSettings)[]>

export type SystemSettingsTab = keyof typeof systemSettingFields | 'proxy'
export type AuthSettingsTab = keyof typeof authSettingFields

export function pickSettings<T extends object, K extends keyof T>(
  settings: T,
  fields: readonly K[],
): Pick<T, K> {
  return Object.fromEntries(fields.map((field) => [field, settings[field]])) as Pick<T, K>
}

// Merge only the saved group so another tab's unsaved draft remains intact.
export function applySavedSettings<T extends { updated_at: string }>(
  draft: T,
  saved: T,
  fields: readonly (keyof T)[],
) {
  Object.assign(draft, pickSettings(saved, fields), { updated_at: saved.updated_at })
}
