import { ref } from 'vue'
import { defineStore } from 'pinia'
import { rewardsApi } from '@/api'
import type { PlayerReward } from '@/types'

export const useRewardStore = defineStore('rewards', () => {
  const items = ref<PlayerReward[]>([])
  const loading = ref(false)
  const loaded = ref(false)
  let inFlight: Promise<PlayerReward[]> | null = null
  let generation = 0

  function balanceFor(key?: string) {
    if (!key) return 0
    return items.value.find((item) => item.asset.key === key)?.balance ?? 0
  }

  async function load() {
    if (inFlight) return inFlight
    const epoch = generation
    loading.value = true
    const request = rewardsApi.list()
    inFlight = request
    try {
      const next = await request
      if (epoch === generation) {
        items.value = next
        loaded.value = true
      }
      return next
    } finally {
      if (inFlight === request) inFlight = null
      if (epoch === generation) loading.value = false
    }
  }

  function reset() {
    invalidateRequests()
    items.value = []
    loaded.value = false
    loading.value = false
  }

  function invalidateRequests() {
    generation++
    inFlight = null
    loading.value = false
  }

  return { items, loading, loaded, balanceFor, load, reset, invalidateRequests }
})
