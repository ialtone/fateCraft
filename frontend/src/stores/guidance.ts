import { defineStore } from 'pinia'
import { ref } from 'vue'
import { guidanceApi } from '@/api'
import type { ActiveGuidance } from '@/types'

export const useGuidanceStore = defineStore('guidance', () => {
  const active = ref<ActiveGuidance | null>(null)
  let version = 0

  function set(value: ActiveGuidance | null) {
    version++
    active.value = value
  }

  async function load(convId: string) {
    const requestVersion = ++version
    try {
      const value = await guidanceApi.get(convId)
      if (version === requestVersion) active.value = value
    } catch {
      if (version === requestVersion) active.value = null
    }
  }

  function reset() { set(null) }
  return { active, set, load, reset }
})
