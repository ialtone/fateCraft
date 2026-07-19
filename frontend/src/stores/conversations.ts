import { defineStore } from 'pinia'
import { ref } from 'vue'
import { conversationsApi } from '@/api'
import type { Conversation, CreateConversationInput, UpdateConversationInput } from '@/types'

// 结局触发信息（WS ending_triggered 事件的业务结果，供结局弹窗消费）。
export interface EndingInfo {
  type: string
  title: string
  summary: string
  spiritStones?: number
  survivalTurns?: number
}

export const useConversationStore = defineStore('conversation', () => {
  const list = ref<Conversation[]>([])
  const current = ref<Conversation | null>(null)
  const currentWorldBookIds = ref<string[]>([])
  const loading = ref(false)
  // 结局信息放在对话 store 而非 socket store：它是业务事件的结果，不是连接状态；
  // 且跟随 clearCurrent 一起清理，切换对话不会带上旧结局。
  const endingTriggered = ref<EndingInfo | null>(null)

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
    currentWorldBookIds.value = data.world_book_ids || []
    return data.conversation
  }

  async function create(body: CreateConversationInput) {
    const conv = await conversationsApi.create(body)
    return conv
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

  // 将当前对话标记为已结束（WS 收到 conversation_ended / ending_triggered 时调用）。
  function markEnded() {
    if (current.value) current.value.status = 'ended'
  }

  // 记录结局信息，交由 UI 弹窗展示。
  function setEndingTriggered(info: EndingInfo) {
    endingTriggered.value = info
  }

  // 结局弹窗关闭后清除结局信息。
  function clearEnding() {
    endingTriggered.value = null
  }

  // 切换对话时清空当前缓存。
  function clearCurrent() {
    current.value = null
    currentWorldBookIds.value = []
    // 同步清掉旧结局，避免切到新对话时弹出上一局的结局弹窗
    endingTriggered.value = null
  }

  return {
    list,
    current,
    currentWorldBookIds,
    loading,
    endingTriggered,
    loadList,
    load,
    create,
    update,
    end,
    archive,
    restore,
    remove,
    markEnded,
    setEndingTriggered,
    clearEnding,
    clearCurrent,
  }
})
