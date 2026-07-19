import { defineStore } from 'pinia'
import { ref } from 'vue'
import { settingsApi } from '@/api'
import type { LLMSettings } from '@/types'

export const useSettingsStore = defineStore('settings', () => {
  const settings = ref<LLMSettings | null>(null)

  async function load() {
    settings.value = await settingsApi.get()
  }

  async function update(body: Record<string, any>) {
    settings.value = await settingsApi.update(body)
  }

  return { settings, load, update }
})
