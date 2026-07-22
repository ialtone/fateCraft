import { defineStore } from 'pinia'
import { ref } from 'vue'
import { challengesApi } from '@/api'
import type { PublicChallenge } from '@/types'

export const useChallengeStore = defineStore('challenges', () => {
  const list = ref<PublicChallenge[]>([])
  const loading = ref(false)

  async function load() {
    loading.value = true
    try {
      list.value = await challengesApi.list()
    } finally {
      loading.value = false
    }
  }

  return { list, loading, load }
})
