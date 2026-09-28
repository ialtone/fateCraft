import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { settingsApi } from '@/api'
import type { LLMSettings } from '@/types'

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

  /** 账号切换（含登出/会话过期）时丢弃旧账号设置，由 App 重新加载。 */
  function reset() {
    settings.value = null
  }

  return { settings, effectiveRoute, load, update, reset }
})
