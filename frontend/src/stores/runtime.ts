import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { runtimeApi } from '@/api'
import type { PublicRuntime } from '@/types'

export const useRuntimeStore = defineStore('runtime', () => {
  const runtime = ref<PublicRuntime | null>(null)
  const loading = ref(false)
  const presentation = computed(() => runtime.value?.presentation ?? null)
  let latestRequest = 0

  async function load(conversationId: string) {
    const requestID = ++latestRequest
    loading.value = true
    try {
      const loaded = await runtimeApi.get(conversationId)
      // Keep the client tolerant of a rolling backend deployment where an
      // older response has no platform capability projection yet.
      loaded.platform_capabilities ||= {}
      loaded.capabilities ||= {}
      if (requestID === latestRequest) runtime.value = loaded
      return runtime.value
    } finally {
      if (requestID === latestRequest) loading.value = false
    }
  }

  function capability(key: string) {
    return runtime.value?.capabilities?.[key]
  }

  function platformCapability(key: string) {
    return runtime.value?.platform_capabilities?.[key]
  }

  function reset() {
    latestRequest++
    runtime.value = null
    loading.value = false
  }

  function invalidateRequests() {
    latestRequest++
    loading.value = false
  }

  return {
    runtime,
    presentation,
    loading,
    load,
    capability,
    platformCapability,
    reset,
    invalidateRequests,
  }
})
