import { defineStore } from 'pinia'
import { ref } from 'vue'
import { messagesApi } from '@/api'
import type { DisplayMessage } from '@/types'

// 流式期间的临时标记。
export interface ChatMessage extends DisplayMessage {
  streaming?: boolean
}

let tempSeq = 0

export const useChatStore = defineStore('chat', () => {
  const messages = ref<ChatMessage[]>([])
  const isGenerating = ref(false)
  const streamingId = ref<string | null>(null)

  async function load(convId: string) {
    messages.value = await messagesApi.list(convId, { limit: 200 })
  }

  function reset() {
    messages.value = []
    isGenerating.value = false
    streamingId.value = null
  }

  // 发送前在时间线乐观插入用户消息，返回临时 ID 便于失败时回滚。
  function addOptimisticUser(content: string): string {
    const id = `temp-${++tempSeq}`
    messages.value.push({
      id,
      conversation_id: '',
      type: 'user',
      markdown: content,
      metadata: {},
      created_at: new Date().toISOString(),
    })
    return id
  }

  // 发送失败时移除乐观插入的消息。
  function removeById(id: string) {
    messages.value = messages.value.filter((m) => m.id !== id)
  }

  // 清理所有未被服务端认领的乐观用户消息（收到 error 事件时调用）。
  // 场景：generation_started 前快速连发两条，第二条被后端生成锁拒绝时
  // 只会回一个 error 事件，对应的 temp 占位不会再有 message_created 来认领，
  // 不清理就会作为幽灵消息永远留在时间线上。
  // 安全性：同一连接上事件按序到达，后端总是先广播 message_created（认领
  // 占位）再进入生成/报错，error 到达时仍是 temp- 前缀的用户消息必然已被拒绝。
  function removeStaleTempUsers() {
    messages.value = messages.value.filter((m) => !(m.id.startsWith('temp-') && m.type === 'user'))
  }

  // upsert 一条服务端持久化消息；用户消息会认领最早的乐观占位。
  function applyServerMessage(msg: DisplayMessage) {
    const idx = messages.value.findIndex((m) => m.id === msg.id)
    if (idx >= 0) {
      messages.value[idx] = { ...msg }
      return
    }
    if (msg.type === 'user') {
      // 匹配最早的 temp 用户消息（role 相同即认领），
      // 不要求内容全等，避免后端 trim/规范化后匹配失败导致重复显示。
      const tempIdx = messages.value.findIndex((m) => m.id.startsWith('temp-') && m.type === 'user')
      if (tempIdx >= 0) {
        messages.value[tempIdx] = { ...msg }
        return
      }
    }
    messages.value.push({ ...msg })
  }

  function beginStream(id: string) {
    isGenerating.value = true
    streamingId.value = id
    if (!messages.value.find((m) => m.id === id)) {
      messages.value.push({
        id,
        conversation_id: '',
        type: 'assistant',
        markdown: '',
        metadata: {},
        created_at: new Date().toISOString(),
        streaming: true,
      })
    }
  }

  function appendDelta(id: string, delta: string) {
    const msg = messages.value.find((m) => m.id === id)
    if (msg) msg.markdown += delta
  }

  function endStream(id: string) {
    isGenerating.value = false
    streamingId.value = null
    const msg = messages.value.find((m) => m.id === id)
    if (msg) msg.streaming = false
  }

  // 服务端在动作预校验失败时撤销整份候选回复；该消息从未落库，必须连同
  // 已显示的流式增量一起移除，避免玩家把无效结局当成已经发生的剧情。
  function discardStream(id: string) {
    messages.value = messages.value.filter((m) => m.id !== id)
    if (streamingId.value === id) {
      streamingId.value = null
      isGenerating.value = false
    }
  }

  // 删除指定消息之后的所有消息（天道回溯：编辑历史消息后乐观截断时间线）。
  function truncateAfter(messageId: string) {
    const idx = messages.value.findIndex((m) => m.id === messageId)
    if (idx >= 0) messages.value.splice(idx + 1)
  }

  // 删除最后一条 assistant 消息（重新生成前先移除旧回复）。
  function removeLastAssistant() {
    for (let i = messages.value.length - 1; i >= 0; i--) {
      if (messages.value[i].type === 'assistant') {
        messages.value.splice(i, 1)
        return
      }
    }
  }

  // 覆写指定消息的 markdown 内容（天道回溯：把编辑后的文本写回时间线）。
  function updateMarkdown(messageId: string, markdown: string) {
    const msg = messages.value.find((m) => m.id === messageId)
    if (msg) msg.markdown = markdown
  }

  // 复位流式生成状态（对话结束、结局触发或生成出错时调用）；
  // 若仍有未闭合的流式消息，一并落定其 streaming 标记。
  function stopGenerating() {
    isGenerating.value = false
    if (streamingId.value) {
      const msg = messages.value.find((m) => m.id === streamingId.value)
      if (msg) msg.streaming = false
      streamingId.value = null
    }
  }

  return {
    messages,
    isGenerating,
    streamingId,
    load,
    reset,
    addOptimisticUser,
    removeById,
    removeStaleTempUsers,
    applyServerMessage,
    beginStream,
    appendDelta,
    endStream,
    discardStream,
    truncateAfter,
    removeLastAssistant,
    updateMarkdown,
    stopGenerating,
  }
})
