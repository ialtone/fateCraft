import { ref } from 'vue'
import { defineStore } from 'pinia'
import http from '@/api'

export interface GameplayVersionInfo {
  pack_name: string
  pack_slug: string
  version: string
  current_version?: string
  new_start_available: boolean
  run_allowed: boolean
  reason?: string
}

// 玩法版本状态是运行闸门的展示缓存，不参与授权。缓存超过 60 秒或
// 页面重新可见时刷新；生成/行动写入前由服务端继续强制校验。
export const useGameplayVersionStore = defineStore('gameplayVersion', () => {
  const versions = ref<Record<string, GameplayVersionInfo>>({})
  const loadedAt = ref<Record<string, number>>({})
  const errors = ref<Record<string, string>>({})
  const flights = new Map<string, Promise<GameplayVersionInfo>>()
  let generation = 0
  const CACHE_TTL = 60000

  function fresh(conversationId: string): boolean {
    const fetched = loadedAt.value[conversationId] ?? 0
    return (
      !errors.value[conversationId] &&
      !!versions.value[conversationId] &&
      Date.now() - fetched < CACHE_TTL
    )
  }

  async function load(conversationId: string, force = false): Promise<GameplayVersionInfo> {
    if (!force && fresh(conversationId)) return versions.value[conversationId]
    const existing = flights.get(conversationId)
    if (existing) return existing
    const epoch = generation
    const request = http
      .get<GameplayVersionInfo>(`/conversations/${conversationId}/gameplay-version`)
      .then(({ data }) => {
        if (epoch === generation) {
          versions.value[conversationId] = data
          loadedAt.value[conversationId] = Date.now()
          delete errors.value[conversationId]
        }
        return data
      })
      .catch((cause) => {
        if (epoch === generation)
          errors.value[conversationId] = '玩法版本状态暂时无法读取，请稍后刷新。'
        throw cause
      })
      .finally(() => {
        if (flights.get(conversationId) === request) flights.delete(conversationId)
      })
    flights.set(conversationId, request)
    return request
  }

  async function ensureFresh(conversationId: string): Promise<GameplayVersionInfo> {
    return load(conversationId)
  }

  function reset() {
    generation++
    versions.value = {}
    loadedAt.value = {}
    errors.value = {}
    flights.clear()
  }

  return { versions, loadedAt, errors, fresh, load, ensureFresh, reset }
})
