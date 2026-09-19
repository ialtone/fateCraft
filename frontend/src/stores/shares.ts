import { defineStore } from 'pinia'
import { ref } from 'vue'
import { sharesApi } from '@/api'
import type { PublicConversationShare } from '@/types'

export const useShareStore = defineStore('shares', () => {
  const current = ref<PublicConversationShare | null>(null)
  const loading = ref(false)
  const error = ref('')

  async function load(token: string) {
    loading.value = true
    error.value = ''
    current.value = null
    try {
      current.value = await sharesApi.public(token)
    } catch (cause: any) {
      error.value =
        cause?.response?.status === 404 ? '分享不存在或已失效' : cause?.message || '加载分享失败'
      throw cause
    } finally {
      loading.value = false
    }
  }

  function reset() {
    current.value = null
    loading.value = false
    error.value = ''
  }

  return { current, loading, error, load, reset }
})
