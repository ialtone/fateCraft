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

  // 有效路由：后端返回的 llm_route 已经是有效路由（只由平台开关和用户
  // 保存值决定，与接入模式无关），此处只做开关兜底。
  const effectiveRoute = computed<LLMRoute>(() => {
    if (!settings.value?.client_route_enabled) return 'server'
    return settings.value?.llm_route === 'client' ? 'client' : 'server'
  })

  async function load() {
    settings.value = await settingsApi.get()
  }

  async function update(body: Record<string, any>) {
    settings.value = await settingsApi.update(body)
  }

  /** 开始游戏前重新读取配置，避免沿用旧开关或旧设备密钥状态。 */
  async function checkPlayReadiness(): Promise<string> {
    try {
      await load()
    } catch {
      return playConfigurationIssue(null, false)
    }
    const userId = useAuthStore().user?.id
    return playConfigurationIssue(settings.value, !!userId && !!getLocalKey(userId).trim())
  }

  /** 账号切换（含登出/会话过期）时丢弃旧账号设置，由 App 重新加载。 */
  function reset() {
    settings.value = null
  }

  return { settings, effectiveRoute, load, update, checkPlayReadiness, reset }
})
