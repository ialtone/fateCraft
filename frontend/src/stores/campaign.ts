import { defineStore } from 'pinia'
import { ref } from 'vue'
import axios from 'axios'
import { campaignsApi } from '@/api'
import type { Campaign, CampaignActionInput, CampaignSnapshot } from '@/types'
import { useChatStore } from '@/stores/chat'
import { useConversationStore } from '@/stores/conversations'
import { useRewardStore } from '@/stores/rewards'

export const useCampaignStore = defineStore('campaign', () => {
  const chat = useChatStore()
  const snapshot = ref<CampaignSnapshot | null>(null)
  const loading = ref(false)
  const acting = ref(false)
  const error = ref<string | null>(null)
  const pending = ref<CampaignActionInput | null>(null)
  let generation = 0
  let campaignID = ''
  let activeRequests = 0
  let latestRequest = 0

  async function load(id: string) {
    if (campaignID !== id) {
      reset()
      campaignID = id
      try {
        pending.value = JSON.parse(
          sessionStorage.getItem(`fatecraft:campaign-action:${id}`) || 'null',
        )
      } catch {
        /* Storage may be unavailable. */
      }
    }
    const requestGeneration = generation
    const requestID = ++latestRequest
    activeRequests++
    loading.value = true
    try {
      const loaded = await campaignsApi.snapshot(id)
      if (generation !== requestGeneration) return loaded
      applySnapshot(loaded)
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
    if (snapshot.value?.campaign.id !== campaign.id) return
    if (campaign.revision < snapshot.value.campaign.revision) return
    if (
      campaign.revision === snapshot.value.campaign.revision &&
      campaign.state_hash !== snapshot.value.campaign.state_hash
    ) {
      error.value = '状态版本冲突，正在重新同步'
      void load(campaign.id).catch(() => {})
      return
    }
    snapshot.value.campaign = campaign
  }

  function applySnapshot(next: CampaignSnapshot): boolean {
    const current = snapshot.value
    if (!current || current.campaign.id !== next.campaign.id) {
      snapshot.value = next
      return true
    }
    if (next.campaign.revision < current.campaign.revision) return false
    if (
      next.campaign.revision === current.campaign.revision &&
      next.campaign.state_hash !== current.campaign.state_hash
    ) {
      error.value = '状态版本冲突，正在重新同步'
      return false
    }
    snapshot.value = next
    return true
  }

  function matches(revision?: number, stateHash?: string): boolean {
    const current = snapshot.value?.campaign
    if (!current || revision === undefined) return false
    if (current.revision !== revision) return false
    return !stateHash || current.state_hash === stateHash
  }

  async function execute(key: string, args: Record<string, unknown>) {
    const current = snapshot.value
    if (!current || current.campaign.status !== 'active' || acting.value || pending.value)
      return null
    const input: CampaignActionInput = {
      action_id: globalThis.crypto.randomUUID(),
      key,
      expected_revision: current.campaign.revision,
      arguments: structuredClone(args),
    }
    pending.value = input
    try {
      sessionStorage.setItem(
        `fatecraft:campaign-action:${current.campaign.id}`,
        JSON.stringify(input),
      )
    } catch {
      /* The in-memory request still supports retry. */
    }
    return retry()
  }

  async function retry() {
    const input = pending.value
    const conversations = useConversationStore()
    const conversationID = conversations.current?.id
    if (
      !input ||
      acting.value ||
      !campaignID ||
      !conversationID ||
      conversations.current?.campaign_id !== campaignID
    )
      return null
    const id = campaignID
    const actionGeneration = generation
    const isCurrent = () =>
      generation === actionGeneration && conversations.current?.id === conversationID
    const clearPending = () => {
      try {
        const key = `fatecraft:campaign-action:${id}`
        const saved = JSON.parse(sessionStorage.getItem(key) || 'null')
        if (saved?.action_id === input.action_id) sessionStorage.removeItem(key)
      } catch {
        /* Storage may be unavailable. */
      }
      if (isCurrent() && pending.value?.action_id === input.action_id) pending.value = null
    }
    acting.value = true
    error.value = null
    try {
      const result = await campaignsApi.action(id, input)
      clearPending()
      if (!isCurrent()) return result
      applyCampaign(result.campaign)
      if (result.roll_message) chat.applyServerMessage(result.roll_message)
      if (result.result?.completion)
        conversations.consumeCompletion(
          result.result.completion as import('@/types/endings').CompletionView,
        )
      if (result.campaign.status !== 'active') conversations.markEnded()
      // A committed action remains successful even if a display refresh fails.
      const refreshed = await Promise.allSettled([
        load(id),
        chat.load(conversationID),
        conversations.load(conversationID),
        useRewardStore().load(),
      ])
      if (isCurrent() && refreshed.some((r) => r.status === 'rejected'))
        error.value = '操作已完成，部分展示未刷新。请刷新页面查看最新结果。'
      return result
    } catch (cause: any) {
      const status = axios.isAxiosError(cause) ? cause.response?.status : undefined
      if (status && status >= 400 && status < 500 && status !== 408) clearPending()
      if (!isCurrent()) return null
      error.value = pending.value
        ? '尚不能确认操作结果，请查询上次操作结果；重复查询不会再次消耗资源。'
        : cause?.response?.data?.error || cause?.message || '领域动作执行失败'
      if (!pending.value) await load(id).catch(() => {})
      throw cause
    } finally {
      if (isCurrent()) acting.value = false
    }
  }

  function reset() {
    generation++
    campaignID = ''
    pending.value = null
    activeRequests = 0
    snapshot.value = null
    loading.value = false
    acting.value = false
    error.value = null
  }

  return {
    snapshot,
    loading,
    acting,
    error,
    pending,
    load,
    execute,
    retry,
    applyCampaign,
    applySnapshot,
    matches,
    reset,
  }
})
