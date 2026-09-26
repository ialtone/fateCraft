import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { authApi, setupApi } from '@/api'
import { clearLocalKey } from '@/llm/localKey'
import type { AuthConfig, AuthUser } from '@/types'

const defaultConfig: AuthConfig = {
  email_auth_enabled: false,
  linuxdo_login_enabled: false,
  email_registration_enabled: false,
  email_verification_enabled: false,
  invitation_required: false,
  verification_resend_seconds: 60,
  linuxdo_enabled: false,
}

// 与后端 normalizeUsername 保持一致的用户名规则说明。
export const USERNAME_RULE_HINT = '2-32 个字符，不能包含 @、空格或控制字符；大小写不敏感'

export const useAuthStore = defineStore('auth', () => {
  const user = ref<AuthUser | null>(null)
  const config = ref<AuthConfig>({ ...defaultConfig })
  const initialized = ref(false)
  const installationInitialized = ref<boolean | null>(null)
  const loading = ref(false)
  let initialization: Promise<void> | null = null

  const authenticated = computed(() => user.value !== null)
  const isAdmin = computed(() => user.value?.role === 'admin' || user.value?.role === 'owner')
  const isOwner = computed(() => user.value?.role === 'owner')

  async function initialize() {
    if (initialized.value) return
    if (initialization) return initialization
    loading.value = true
    initialization = Promise.allSettled([authApi.config(), authApi.me(), setupApi.status()])
      .then(([configResult, userResult, setupResult]) => {
        if (configResult.status === 'fulfilled') config.value = configResult.value
        if (userResult.status === 'fulfilled') user.value = userResult.value
        if (setupResult.status === 'fulfilled')
          installationInitialized.value = setupResult.value.initialized
      })
      .finally(() => {
        initialized.value = true
        loading.value = false
        initialization = null
      })
    return initialization
  }

  async function login(identifier: string, password: string) {
    user.value = await authApi.login({ identifier, password })
  }

  async function refreshConfig() {
    config.value = await authApi.config()
  }

  async function refreshInstallation() {
    installationInitialized.value = (await setupApi.status()).initialized
  }

  async function refreshUser() {
    user.value = await authApi.me()
  }

  async function initializeOwner(email: string, password: string, username: string) {
    const result = await setupApi.initialize({
      email,
      password,
      username,
      confirm: true,
    })
    user.value = result.user
    installationInitialized.value = true
    await refreshConfig()
  }

  async function sendRegistrationCode(email: string) {
    await authApi.sendRegistrationCode(email)
  }

  async function register(
    email: string,
    username: string,
    password: string,
    displayName: string,
    verificationCode: string,
    invitationCode: string,
  ) {
    user.value = await authApi.register({
      email,
      username,
      password,
      display_name: displayName,
      verification_code: verificationCode,
      invitation_code: invitationCode,
    })
  }

  async function logout() {
    const userId = user.value?.id ?? ''
    try {
      await authApi.logout()
    } finally {
      // 登出时清除本设备的直连 Key（按用户隔离）。
      if (userId) clearLocalKey(userId)
      user.value = null
    }
  }

  // 会话已在服务端失效（业务请求返回 401）时仅清空本地登录态，
  // 不再调用 logout 接口。
  function expireSession() {
    user.value = null
  }

  function linuxDOLoginURL(returnTo = '/') {
    const params = new URLSearchParams({ return_to: returnTo })
    return `/api/auth/linuxdo/start?${params.toString()}`
  }

  return {
    user,
    config,
    initialized,
    installationInitialized,
    loading,
    authenticated,
    isAdmin,
    isOwner,
    initialize,
    refreshConfig,
    refreshInstallation,
    refreshUser,
    initializeOwner,
    login,
    sendRegistrationCode,
    register,
    logout,
    expireSession,
    linuxDOLoginURL,
  }
})
