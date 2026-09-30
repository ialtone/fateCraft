import { ref } from 'vue'
import { defineStore } from 'pinia'
import { rewardsApi } from '@/api'
import type { PlayerReward } from '@/types'

export const useRewardStore = defineStore('rewards', () => {
  const items = ref<PlayerReward[]>([])
  const loading = ref(false)
  const loaded = ref(false)

  function balanceFor(key?: string) {
    if (!key) return 0
    return items.value.find((item) => item.asset.key === key)?.balance ?? 0
  }

  async function load() {
    loading.value = true
    try {
      items.value = await rewardsApi.list()
      loaded.value = true
    } finally {
      loading.value = false
    }
  }

  function reset() {
    items.value = []
    loaded.value = false
  }

  return { items, loading, loaded, balanceFor, load, reset }
})
