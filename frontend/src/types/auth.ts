export interface AuthUser {
  id: string
  username: string
  email?: string
  display_name: string
  avatar_url?: string
  status: string
  role: 'owner' | 'admin' | 'user'
  last_login_at?: string
  created_at: string
  updated_at: string
}

export interface AuthConfig {
  email_auth_enabled: boolean
  linuxdo_login_enabled: boolean
  email_registration_enabled: boolean
  email_verification_enabled: boolean
  invitation_required: boolean
  verification_resend_seconds: number
  linuxdo_enabled: boolean
}

export interface ExternalIdentity {
  id: string
  provider: string
  provider_user_id: string
  username: string
  provider_email?: string
  trust_level?: number
  created_at: string
  updated_at: string
}
