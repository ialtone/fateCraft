import { defineStore } from 'pinia'
import { ref } from 'vue'
import { contextApi } from '@/api'
import type { ContextBuildLog } from '@/types'

export const useContextStore = defineStore('context', () => {
  const lastBuild = ref<ContextBuildLog | null>(null)

  async function loadLast(convId: string) {
    try {
      lastBuild.value = await contextApi.last(convId)
    } catch {
      lastBuild.value = null
    }
  }

  async function loadById(id: string) {
    try {
      lastBuild.value = await contextApi.get(id)
    } catch {
      // 忽略：日志可能尚未可读。
    }
  }

  function reset() {
    lastBuild.value = null
  }

  return { lastBuild, loadLast, loadById, reset }
})
