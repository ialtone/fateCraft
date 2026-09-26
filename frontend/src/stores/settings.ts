import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { settingsApi } from '@/api'
import type { LLMSettings } from '@/types'

export type LLMRoute = 'server' | 'client'

export const useSettingsStore = defineStore('settings', () => {
  const settings = ref<LLMSettings | null>(null)

  // 有效路由：平台托管时恒为 server，否则为用户设置的路由（缺省 server）。
  const effectiveRoute = computed<LLMRoute>(() => {
    if (settings.value?.access_mode === 'platform_managed') return 'server'
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
