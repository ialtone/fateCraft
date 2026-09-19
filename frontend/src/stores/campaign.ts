import { defineStore } from 'pinia'
import { ref } from 'vue'
import { campaignsApi } from '@/api'
import type { Campaign, CampaignSnapshot } from '@/types'
import { useChatStore } from '@/stores/chat'

export const useCampaignStore = defineStore('campaign', () => {
  const chat = useChatStore()
  const snapshot = ref<CampaignSnapshot | null>(null)
  const loading = ref(false)
  const acting = ref(false)
  const error = ref<string | null>(null)
  let generation = 0
  let activeRequests = 0
  let latestRequest = 0

  async function load(id: string) {
    const requestGeneration = generation
    const requestID = ++latestRequest
    activeRequests++
    loading.value = true
    error.value = null
    try {
      const loaded = await campaignsApi.snapshot(id)
      if (generation !== requestGeneration) return loaded
      const current = snapshot.value
      if (
        !current ||
        current.campaign.id !== id ||
        loaded.campaign.revision >= current.campaign.revision
      ) {
        snapshot.value = loaded
      }
      return loaded
    } catch (cause: any) {
      if (generation === requestGeneration && requestID === latestRequest) {
        error.value = cause?.message || '长期世界加载失败'
      }
      throw cause
    } finally {
      if (generation === requestGeneration) {
        activeRequests--
        loading.value = activeRequests > 0
      }
    }
  }

  function applyCampaign(campaign: Campaign) {
    if (snapshot.value?.campaign.id === campaign.id) snapshot.value.campaign = campaign
  }

  async function execute(key: string, args: Record<string, unknown>) {
    const current = snapshot.value
    if (!current || current.campaign.status !== 'active' || acting.value) return null
    acting.value = true
    error.value = null
    const actionID =
      globalThis.crypto?.randomUUID?.() || `ui-${Date.now()}-${Math.random().toString(36).slice(2)}`
    try {
      const result = await campaignsApi.action(current.campaign.id, {
        action_id: actionID,
        key,
        expected_revision: current.campaign.revision,
        arguments: args,
      })
      applyCampaign(result.campaign)
      if (result.roll_message) chat.applyServerMessage(result.roll_message)
      await load(current.campaign.id)
      return result
    } catch (cause: any) {
      await load(current.campaign.id).catch(() => {})
      error.value = cause?.message || '领域动作执行失败'
      throw cause
    } finally {
      acting.value = false
    }
  }

  function reset() {
    generation++
    activeRequests = 0
    snapshot.value = null
    loading.value = false
    acting.value = false
    error.value = null
  }

  return { snapshot, loading, acting, error, load, execute, applyCampaign, reset }
})
