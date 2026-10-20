import { defineStore } from 'pinia'
import { ref } from 'vue'
import { inquiriesApi } from '@/api'
import { useSocketStore } from './socket'
import type { CharacterInquiryMessage, CharacterInquiryThread } from '@/types'

export const useInquiryStore = defineStore('inquiry', () => {
  const thread = ref<CharacterInquiryThread | null>(null)
  const messages = ref<CharacterInquiryMessage[]>([])
  const loading = ref(false)
  const asking = ref(false)
  const cancelling = ref(false)
  const cancelRequested = ref(false)
  const stale = ref(false)
  const error = ref('')

  async function load(conversationId: string) {
    if (useSocketStore().writeBlocked) return
    loading.value = true
    error.value = ''
    try {
      const result = await inquiriesApi.history(conversationId)
      thread.value = result.thread
      messages.value = result.messages
    } catch (cause) {
      error.value = cause instanceof Error ? cause.message : '询问记录加载失败'
      throw cause
    } finally {
      loading.value = false
    }
  }

  async function ask(conversationId: string, question: string) {
    if (useSocketStore().writeBlocked) return
    const content = question.trim()
    if (!content || asking.value) return
    asking.value = true
    cancelRequested.value = false
    error.value = ''
    stale.value = false
    const optimisticId = `pending-${Date.now()}`
    messages.value.push({
      id: optimisticId,
      thread_id: thread.value?.id || '',
      conversation_id: conversationId,
      player_id: '',
      role: 'user',
      content,
      snapshot_branch_epoch: 0,
      snapshot_revision: 0,
      source_refs: [],
      status: 'completed',
      created_at: new Date().toISOString(),
    })
    try {
      const result = await inquiriesApi.ask(conversationId, content)
      messages.value.push(result.message)
      stale.value = result.stale
      await load(conversationId)
      stale.value = result.stale
    } catch (cause) {
      messages.value = messages.value.filter((message) => message.id !== optimisticId)
      if (!cancelRequested.value) {
        error.value = cause instanceof Error ? cause.message : '询问失败'
      }
      throw cause
    } finally {
      asking.value = false
      cancelling.value = false
      cancelRequested.value = false
    }
  }

  async function cancel(conversationId: string) {
    if (useSocketStore().writeBlocked) return
    if (!asking.value || cancelling.value) return
    cancelling.value = true
    cancelRequested.value = true
    error.value = ''
    try {
      await inquiriesApi.cancel(conversationId)
    } catch (cause) {
      cancelRequested.value = false
      cancelling.value = false
      error.value = cause instanceof Error ? cause.message : '取消询问失败'
    }
  }

  async function clear(conversationId: string) {
    if (useSocketStore().writeBlocked) return
    error.value = ''
    try {
      await inquiriesApi.clear(conversationId)
      messages.value = []
      stale.value = false
    } catch (cause) {
      error.value = cause instanceof Error ? cause.message : '清空询问记录失败'
      throw cause
    }
  }

  function reset() {
    thread.value = null
    messages.value = []
    loading.value = false
    asking.value = false
    cancelling.value = false
    cancelRequested.value = false
    stale.value = false
    error.value = ''
  }

  return {
    thread,
    messages,
    loading,
    asking,
    cancelling,
    stale,
    error,
    load,
    ask,
    cancel,
    clear,
    reset,
  }
})
