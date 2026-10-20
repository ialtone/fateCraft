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
    const value = await guidanceApi.get(convId)
    if (version === requestVersion) active.value = value
  }

  function reset() {
    set(null)
  }
  function invalidateRequests() {
    version++
  }

  return { active, set, load, reset, invalidateRequests }
})
