import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { settingsApi } from '@/api'
import type { LLMSettings } from '@/types'
import { useAuthStore } from '@/stores/auth'
import { getLocalKey } from '@/llm/localKey'
import { playConfigurationIssue } from '@/lib/playReadiness'

export type LLMRoute = 'server' | 'client'

export const useSettingsStore = defineStore('settings', () => {
  const settings = ref<LLMSettings | null>(null)
  const loadedAt = ref(0)
  let loadedOwner = ''
  let inFlight: Promise<LLMSettings> | null = null
  let generation = 0
  const CACHE_TTL = 60000

  const currentOwner = () => useAuthStore().user?.id ?? ''

  // 有效路由：后端返回的 llm_route 已经是有效路由（只由平台开关和用户
  // 保存值决定，与接入模式无关），此处只做开关兜底。
  const effectiveRoute = computed<LLMRoute>(() => {
    if (!settings.value?.client_route_enabled) return 'server'
    return settings.value?.llm_route === 'client' ? 'client' : 'server'
  })

  // 页面展示复用账号内缓存；创建/提交等权威边界用 force=true 重新读取。
  async function load(force = false) {
    const owner = currentOwner()
    if (owner !== loadedOwner) {
      generation++
      settings.value = null
      loadedAt.value = 0
      inFlight = null
      loadedOwner = owner
    }
    if (!force && settings.value && Date.now() - loadedAt.value < CACHE_TTL) return settings.value
    if (inFlight) return inFlight
    const epoch = generation
    const request = settingsApi.get()
    inFlight = request
    try {
      const next = await request
      if (epoch === generation && owner === currentOwner()) {
        settings.value = next
        loadedAt.value = Date.now()
        loadedOwner = owner
      }
      return next
    } finally {
      if (inFlight === request) inFlight = null
    }
  }

  async function update(body: Record<string, any>) {
    settings.value = await settingsApi.update(body)
    loadedAt.value = Date.now()
    loadedOwner = currentOwner()
  }

  /** 开始游戏前重新读取配置，避免沿用旧开关或旧设备密钥状态。 */
  async function checkPlayReadiness(force = true): Promise<string> {
    try {
      await load(force)
    } catch {
      return playConfigurationIssue(null, false)
    }
    const userId = useAuthStore().user?.id
    return playConfigurationIssue(settings.value, !!userId && !!getLocalKey(userId).trim())
  }

  /** 账号切换（含登出/会话过期）时丢弃旧账号设置，由 App 重新加载。 */
  function reset() {
    generation++
    settings.value = null
    loadedAt.value = 0
    loadedOwner = ''
    inFlight = null
  }

  return { settings, effectiveRoute, load, update, checkPlayReadiness, reset }
})
