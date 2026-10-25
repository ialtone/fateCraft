import { defineStore } from 'pinia'
import { ref } from 'vue'
import axios from 'axios'
import { moduleActionsApi } from '@/api'
import { createActionScope, clearMatchingAction } from '@/lib/action-scope'
import { useConversationStore } from './conversations'
import { useChatStore } from './chat'
import { useCampaignStore } from './campaign'
import { useConversationResources } from './conversationResources'
import type { ConversationResource } from './conversationResources'
import { useSocketStore } from './socket'
import type { ModuleActionInput } from '@/types/module-actions'

// One request journal serves ordinary module actions, confirmed endings and top-bar exit.
export const usePlayerActionStore = defineStore('playerActions', () => {
  const pending = ref<ModuleActionInput | null>(null)
  const acting = ref(false)
  const error = ref('')
  const scope = createActionScope()
  let currentID = ''

  function enter(id: string) {
    currentID = id
    scope.enter(id)
    acting.value = false
    error.value = ''
    pending.value = null
    try {
      pending.value = JSON.parse(sessionStorage.getItem(`fatecraft:action:${id}`) || 'null')
    } catch {
      /* unavailable storage */
    }
    if (pending.value) error.value = '上次操作结果尚未确认，请查询原请求；不会重复消耗。'
  }

  function reset() {
    enter('')
  }

  async function execute(id: string, input?: ModuleActionInput) {
    if (useSocketStore().writeBlocked) return null
    const conversations = useConversationStore()
    if (acting.value || currentID !== id || conversations.current?.id !== id) return null
    if (pending.value && input && input.request_id !== pending.value.request_id) return null
    const request = pending.value ?? input
    if (!request) return null
    if (!pending.value) {
      pending.value = JSON.parse(JSON.stringify(request))
      try {
        sessionStorage.setItem(`fatecraft:action:${id}`, JSON.stringify(request))
      } catch {
        /* in-memory retry */
      }
    }
    const token = scope.capture()
    acting.value = true
    error.value = ''
    const clear = () => {
      try {
        clearMatchingAction(sessionStorage, id, request.request_id)
      } catch {
        /* unavailable storage */
      }
      if (scope.isCurrent(token) && pending.value?.request_id === request.request_id)
        pending.value = null
    }
    try {
      const result = await moduleActionsApi.execute(id, request)
      clear()
      if (!scope.isCurrent(token)) return result
      if (result.completion) conversations.consumeCompletion(result.completion)
      const campaign = useCampaignStore()
      const chat = useChatStore()
      if (result.campaign) campaign.applyCampaign(result.campaign)
      const resources = useConversationResources()
      resources.enter(id)
      // 局中只记录阶段进度，不改变账号余额；只有终局结算才需要刷新奖励。
      const pendingRefresh: ConversationResource[] = [
        'state',
        'messages',
        'conversation',
        'endings',
      ]
      if (result.completion) pendingRefresh.push('rewards')
      const refreshed = await resources.refresh(pendingRefresh)
      if (result.campaign) await resources.load('campaign').catch(() => {})
      if (result.campaign && scope.isCurrent(token)) {
        const latestBound = [...chat.messages]
          .reverse()
          .find(
            (message) => message.type === 'assistant' && message.campaign_revision !== undefined,
          )
        if (
          latestBound &&
          !campaign.matches(latestBound.campaign_revision, latestBound.campaign_state_hash)
        ) {
          await resources.load('campaign').catch(() => {})
        }
      }
      if (scope.isCurrent(token) && refreshed.some((r) => r.status === 'rejected'))
        error.value = '操作已完成，部分展示未刷新，请刷新查看。'
      return result
    } catch (cause) {
      const status = axios.isAxiosError(cause) ? cause.response?.status : undefined
      if (status && status >= 400 && status < 500 && status !== 408) clear()
      if (scope.isCurrent(token))
        error.value = pending.value
          ? '连接中断，尚不能确认操作结果。请查询原请求；不会重复消耗。'
          : axios.isAxiosError(cause)
            ? cause.response?.data?.error || '操作未完成，请刷新后重新确认。'
            : '操作未完成，请重试。'
      return null
    } finally {
      if (scope.isCurrent(token)) acting.value = false
    }
  }
  return { pending, acting, error, enter, reset, execute }
})
