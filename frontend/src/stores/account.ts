import { defineStore } from 'pinia'
import { ref } from 'vue'
import { accountApi } from '@/api'

export const useAccountStore = defineStore('account', () => {
  // 未加载完成前不使用假数据，UI 应根据 loaded 显示加载态
  const rewindCount = ref(0)
  const totalRewindsEarned = ref(0)
  const rewindsUsed = ref(0)
  const totalSpiritStones = ref(0)
  const successfulTrials = ref(0)
  const failedTrials = ref(0)
  const loading = ref(false)
  // 是否已成功从后端加载过账号数据
  const loaded = ref(false)

  async function load() {
    loading.value = true
    try {
      const data = await accountApi.get()
      rewindCount.value = data.rewind_count
      totalRewindsEarned.value = data.total_rewinds_earned
      rewindsUsed.value = data.rewinds_used
      totalSpiritStones.value = data.total_spirit_stones || 0
      successfulTrials.value = data.successful_trials || 0
      failedTrials.value = data.failed_trials || 0
      loaded.value = true
    } finally {
      loading.value = false
    }
  }

  return {
    rewindCount,
    totalRewindsEarned,
    rewindsUsed,
    totalSpiritStones,
    successfulTrials,
    failedTrials,
    loading,
    loaded,
    load,
  }
})
