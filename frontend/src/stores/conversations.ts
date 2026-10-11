import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { CompletionView, EndingSnapshot } from '@/types/endings'
import { conversationsApi, moduleActionsApi } from '@/api'
import type {
  CompletionMetric,
  Conversation,
  CreateConversationInput,
  UpdateConversationInput,
} from '@/types/api'

// 挑战完成事件的业务结果，供通用完成弹窗消费。
export interface CompletionInfo {
  id: string
  key: string
  category: 'success' | 'failure' | 'neutral'
  title: string
  summary: string
  tone: string
  rewards: CompletionView['rewards']
  metrics: CompletionMetric[]
  nextActions: Array<'replay' | 'back_to_challenges'>
  /** 非空表示未发放奖励的原因（客户端直连为 client_llm_route）。 */
  rewardsDisabledReason?: string
}

export const useConversationStore = defineStore('conversation', () => {
  const list = ref<Conversation[]>([])
  const current = ref<Conversation | null>(null)
  const loading = ref(false)
  // 结局信息放在对话 store 而非 socket store：它是业务事件的结果，不是连接状态；
  // 且跟随 clearCurrent 一起清理，切换对话不会带上旧结局。
  const completion = ref<CompletionInfo | null>(null)
  const lastCompletion = ref<CompletionInfo | null>(null)
  const endingState = ref<EndingSnapshot | null>(null)
  const seen = new Set<string>()
  const dismissed = new Set<string>()
  let endingGeneration = 0
  let loadGeneration = 0

  async function loadList(params?: { status?: string; search?: string }) {
    loading.value = true
    try {
      list.value = await conversationsApi.list(params)
    } finally {
      loading.value = false
    }
  }

  async function load(id: string) {
    const generation = ++loadGeneration
    const data = await conversationsApi.get(id)
    if (generation === loadGeneration) current.value = data.conversation
    return data.conversation
  }

  async function create(body: CreateConversationInput) {
    const conv = await conversationsApi.create(body)
    return conv
  }

  async function fork(id: string, title?: string) {
    return conversationsApi.fork(id, title)
  }

  async function update(id: string, body: UpdateConversationInput) {
    const conv = await conversationsApi.update(id, body)
    if (current.value?.id === id) current.value = conv
    return conv
  }

  async function end(id: string) {
    const conv = await conversationsApi.end(id)
    if (current.value?.id === id) current.value = conv
    return conv
  }

  async function archive(id: string) {
    await conversationsApi.archive(id)
  }

  async function restore(id: string) {
    const conv = await conversationsApi.restore(id)
    if (current.value?.id === id) current.value = conv
    return conv
  }

  async function remove(id: string) {
    await conversationsApi.remove(id)
    list.value = list.value.filter((c) => c.id !== id)
  }

  // 将当前对话标记为已结束。
  function markEnded() {
    if (current.value) current.value.status = 'ended'
  }

  function consumeCompletion(view: CompletionView) {
    if (
      current.value?.id !== view.conversation_id ||
      current.value.branch_epoch !== view.branch_epoch
    )
      return
    const info: CompletionInfo = {
      ...view,
      nextActions: view.next_actions,
      rewardsDisabledReason: view.rewards_disabled_reason,
    }
    markEnded()
    endingState.value = null
    lastCompletion.value = info
    if (completion.value?.id === info.id) return
    let shown = seen.has(info.id)
    try {
      shown = shown || sessionStorage.getItem(`fatecraft:completion:${info.id}`) === 'seen'
    } catch {
      /* session disabled */
    }
    if (!shown) completion.value = info
  }

  async function loadEndingState(id: string) {
    const generation = ++endingGeneration
    const snapshot = await moduleActionsApi.endings(id)
    if (
      generation !== endingGeneration ||
      current.value?.id !== id ||
      current.value.branch_epoch !== snapshot.branch_epoch
    )
      return
    if (snapshot.proposal) {
      let hidden = dismissed.has(snapshot.proposal.id)
      try {
        hidden =
          hidden ||
          sessionStorage.getItem(`fatecraft:ending-proposal:${snapshot.proposal.id}`) ===
            'dismissed'
      } catch {}
      if (hidden) snapshot.proposal = null
    }
    endingState.value = snapshot
    if (snapshot.completion) consumeCompletion(snapshot.completion)
    return snapshot
  }

  function showLastCompletion() {
    if (lastCompletion.value) completion.value = lastCompletion.value
  }

  function dismissProposal(id: string) {
    dismissed.add(id)
    try {
      sessionStorage.setItem(`fatecraft:ending-proposal:${id}`, 'dismissed')
    } catch {
      /* session disabled */
    }
    if (endingState.value?.proposal?.id === id) endingState.value.proposal = null
  }

  // 结局弹窗关闭后清除结局信息。
  function clearEnding() {
    if (completion.value) {
      seen.add(completion.value.id)
      try {
        sessionStorage.setItem(`fatecraft:completion:${completion.value.id}`, 'seen')
      } catch {
        /* session disabled */
      }
    }
    completion.value = null
  }

  // 切换对话时清空当前缓存。
  function clearCurrent() {
    loadGeneration++
    endingGeneration++
    endingState.value = null
    lastCompletion.value = null
    current.value = null
    // 同步清掉旧结局，避免切到新对话时弹出上一局的结局弹窗
    completion.value = null
  }

  return {
    list,
    current,
    loading,
    completion,
    loadList,
    load,
    create,
    fork,
    update,
    end,
    archive,
    restore,
    remove,
    markEnded,
    consumeCompletion,
    endingState,
    lastCompletion,
    loadEndingState,
    showLastCompletion,
    dismissProposal,
    clearEnding,
    clearCurrent,
  }
})
