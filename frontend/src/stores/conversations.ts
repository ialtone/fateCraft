import { defineStore } from 'pinia'
import { ref } from 'vue'
import { conversationsApi } from '@/api'
import type {
  ChallengeRunReward,
  CompletionMetric,
  Conversation,
  CreateConversationInput,
  UpdateConversationInput,
} from '@/types/api'

// 挑战完成事件的业务结果，供通用完成弹窗消费。
export interface CompletionInfo {
  key: string
  category: 'success' | 'failure' | 'neutral'
  title: string
  summary: string
  tone: string
  rewards: ChallengeRunReward[]
  metrics: CompletionMetric[]
  nextActions: Array<'replay' | 'back_to_challenges'>
}

export const useConversationStore = defineStore('conversation', () => {
  const list = ref<Conversation[]>([])
  const current = ref<Conversation | null>(null)
  const loading = ref(false)
  // 结局信息放在对话 store 而非 socket store：它是业务事件的结果，不是连接状态；
  // 且跟随 clearCurrent 一起清理，切换对话不会带上旧结局。
  const completion = ref<CompletionInfo | null>(null)

  async function loadList(params?: { status?: string; search?: string }) {
    loading.value = true
    try {
      list.value = await conversationsApi.list(params)
    } finally {
      loading.value = false
    }
  }

  async function load(id: string) {
    const data = await conversationsApi.get(id)
    current.value = data.conversation
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

  // 记录结局信息，交由 UI 弹窗展示。
  function setCompletion(info: CompletionInfo) {
    completion.value = info
  }

  // 结局弹窗关闭后清除结局信息。
  function clearEnding() {
    completion.value = null
  }

  // 切换对话时清空当前缓存。
  function clearCurrent() {
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
    setCompletion,
    clearEnding,
    clearCurrent,
  }
})
