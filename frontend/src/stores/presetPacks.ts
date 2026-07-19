import { defineStore } from 'pinia'
import { ref } from 'vue'
import http from '@/api'
import type { PresetPack } from '@/types'

export const usePresetPackStore = defineStore('presetPack', () => {
  const packs = ref<PresetPack[]>([])
  const loading = ref(false)

  async function loadPacks() {
    loading.value = true
    try {
      const { data } = await http.get<{ packs: PresetPack[] }>('/preset-packs')
      packs.value = data.packs || []
    } catch (error) {
      console.error('加载预设包失败:', error)
      packs.value = []
    } finally {
      loading.value = false
    }
  }

  async function getPack(id: string) {
    try {
      const { data } = await http.get(`/preset-packs/${id}`)
      return data
    } catch (error) {
      console.error('获取预设包失败:', error)
      return null
    }
  }

  // 根据世界书 ID 找到对应的预设包：
  // 通过 default_config.required_worldbooks 精确匹配，不再用 ID 前缀启发式。
  function getPackByWorldBookId(worldBookId: string): PresetPack | null {
    return (
      packs.value.find((pack) => pack.default_config?.required_worldbooks?.includes(worldBookId)) ||
      null
    )
  }

  return {
    packs,
    loading,
    loadPacks,
    getPack,
    getPackByWorldBookId,
  }
})
