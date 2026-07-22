export interface AuthUser {
  id: string
  email?: string
  display_name: string
  avatar_url?: string
  status: string
  role: 'admin' | 'user'
  last_login_at?: string
  created_at: string
  updated_at: string
}

export interface AuthConfig {
  email_auth_enabled: boolean
  email_registration_enabled: boolean
  email_verification_enabled: boolean
  invitation_required: boolean
  verification_resend_seconds: number
  linuxdo_enabled: boolean
}
