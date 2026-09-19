import { defineStore } from 'pinia'
import { ref } from 'vue'
import { messagesApi } from '@/api'
import type { DisplayMessage } from '@/types'

// 流式期间的临时标记。
export interface ChatMessage extends DisplayMessage {
  streaming?: boolean
  reasoningExpanded?: boolean
}

// 时间线使用事件驱动跟随流式内容，避免对整个 messages 数组建立持续 watch。
export const CHAT_CONTENT_CHANGED_EVENT = 'fatecraft:chat-content-changed'

function notifyContentChanged() {
  if (typeof window !== 'undefined') window.dispatchEvent(new Event(CHAT_CONTENT_CHANGED_EVENT))
}

let tempSeq = 0

// 单页拉取数量；首页与向前翻页共用。返回满页即认为可能还有更早的历史。
const PAGE_SIZE = 200

export const useChatStore = defineStore('chat', () => {
  const messages = ref<ChatMessage[]>([])
  const isGenerating = ref(false)
  const streamingId = ref<string | null>(null)
  // 是否还有更早的历史可以加载（时间线顶部的"加载更早"按钮据此显隐）
  const hasEarlier = ref(false)
  const loadingEarlier = ref(false)
  // 加载代际：切换对话或整表重载后，旧的在途翻页响应不得写回列表
  let loadGeneration = 0

  async function load(convId: string) {
    const generation = ++loadGeneration
    const loaded = await messagesApi.list(convId, { limit: PAGE_SIZE })
    if (generation !== loadGeneration) return
    messages.value = loaded
    hasEarlier.value = loaded.length >= PAGE_SIZE
    loadingEarlier.value = false
    notifyContentChanged()
  }

  // 向前加载一页更早的历史并插入列表头部；滚动锚定由时间线组件负责。
  // 注意：resync 的整表 load 会把列表收回最新一页，此时 hasEarlier 重算、
  // 已翻出的更早页被丢弃，属于预期行为（resync 语义是全量快照）。
  async function loadEarlier(convId: string) {
    if (loadingEarlier.value || !hasEarlier.value) return
    // 游标必须是服务端已认领的消息 id：temp- 乐观占位服务端不认识，
    // 传过去会被静默忽略、退化成重新拉最新一页。
    const oldest = messages.value.find((m) => !m.id.startsWith('temp-'))
    if (!oldest) return
    const generation = loadGeneration
    loadingEarlier.value = true
    try {
      const earlier = await messagesApi.list(convId, { before: oldest.id, limit: PAGE_SIZE })
      if (generation !== loadGeneration) return
      // 防御性按 id 去重后前插，避免与并发写入产生重复行
      const known = new Set(messages.value.map((m) => m.id))
      const fresh = earlier.filter((m) => !known.has(m.id))
      if (fresh.length) messages.value = [...fresh, ...messages.value]
      hasEarlier.value = earlier.length >= PAGE_SIZE
    } finally {
      if (generation === loadGeneration) loadingEarlier.value = false
    }
  }

  function reset() {
    loadGeneration++
    messages.value = []
    isGenerating.value = false
    streamingId.value = null
    hasEarlier.value = false
    loadingEarlier.value = false
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
    notifyContentChanged()
    return id
  }

  // 发送失败时移除乐观插入的消息。
  function removeById(id: string) {
    messages.value = messages.value.filter((m) => m.id !== id)
    notifyContentChanged()
  }

  // 清理所有未被服务端认领的乐观用户消息（收到 error 事件时调用）。
  // 场景：agent.turn.started 前快速连发两条，第二条被后端生成锁拒绝时
  // 只会回一个 error 事件，对应的 temp 占位不会再有 message_created 来认领，
  // 不清理就会作为幽灵消息永远留在时间线上。
  // 安全性：同一连接上事件按序到达，后端总是先广播 message_created（认领
  // 占位）再进入生成/报错，error 到达时仍是 temp- 前缀的用户消息必然已被拒绝。
  function removeStaleTempUsers() {
    messages.value = messages.value.filter((m) => !(m.id.startsWith('temp-') && m.type === 'user'))
    notifyContentChanged()
  }

  // upsert 一条服务端持久化消息；用户消息会认领最早的乐观占位。
  function applyServerMessage(msg: DisplayMessage) {
    const idx = messages.value.findIndex((m) => m.id === msg.id)
    if (idx >= 0) {
      messages.value[idx] = { ...msg }
      notifyContentChanged()
      return
    }
    if (msg.type === 'user') {
      // 匹配最早的 temp 用户消息（role 相同即认领），
      // 不要求内容全等，避免后端 trim/规范化后匹配失败导致重复显示。
      const tempIdx = messages.value.findIndex((m) => m.id.startsWith('temp-') && m.type === 'user')
      if (tempIdx >= 0) {
        messages.value[tempIdx] = { ...msg }
        notifyContentChanged()
        return
      }
    }
    messages.value.push({ ...msg })
    notifyContentChanged()
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
        reasoningExpanded: false,
      })
    }
    notifyContentChanged()
  }

  function replaceBranch(invalidatedIds: string[], updated?: DisplayMessage) {
    const invalidated = new Set(invalidatedIds)
    messages.value = messages.value.filter((message) => !invalidated.has(message.id))
    if (updated) {
      const index = messages.value.findIndex((message) => message.id === updated.id)
      if (index >= 0) messages.value[index] = { ...updated }
      else messages.value.push({ ...updated })
    }
    notifyContentChanged()
  }

  // 增量到达时若找不到对应占位（重连 resync 整表重载冲掉、或错过了
  // agent.turn.started），自动补建流式占位，保证打字机效果不会静默消失。
  function ensureStreamingMessage(id: string): ChatMessage | undefined {
    let msg = messages.value.find((m) => m.id === id)
    if (!msg) {
      beginStream(id)
      msg = messages.value.find((m) => m.id === id)
    }
    return msg
  }

  function appendDelta(id: string, delta: string) {
    const msg = ensureStreamingMessage(id)
    if (msg) {
      msg.markdown += delta
      notifyContentChanged()
    }
  }

  // 清空尚未确认的临时正文预览。工具调用出现在同一步时，后端会撤销
  // 已经推送的正文；reasoning 和生成状态必须保留，等待下一步继续叙事。
  function resetStreamText(id: string) {
    const msg = messages.value.find((m) => m.id === id)
    if (!msg || !msg.streaming) return
    msg.markdown = ''
    notifyContentChanged()
  }

  // 思考输出期间保持展开；正文首个增量到达时由 collapseReasoning 收起。
  function appendReasoningDelta(id: string, delta: string) {
    if (!delta) return
    const msg = ensureStreamingMessage(id)
    if (!msg) return
    const textAlreadyStarted = msg.markdown.trim().length > 0
    msg.reasoning = `${msg.reasoning || ''}${delta}`
    // 部分兼容网关只在终止响应事件中提供可读 reasoning。正文已经开始时
    // 保持折叠，避免 late reasoning 事件重新打开面板。
    msg.reasoningExpanded = !textAlreadyStarted
    notifyContentChanged()
  }

  function collapseReasoning(id: string) {
    const msg = messages.value.find((m) => m.id === id)
    if (msg) msg.reasoningExpanded = false
  }

  function endStream(id: string) {
    isGenerating.value = false
    streamingId.value = null
    const msg = messages.value.find((m) => m.id === id)
    if (msg) msg.streaming = false
  }

  // 未完成的文本从未持久化；失败、取消或 partial 时移除临时缓冲。
  function abortStream(id: string) {
    messages.value = messages.value.filter((m) => m.id !== id)
    if (streamingId.value === id) {
      streamingId.value = null
      isGenerating.value = false
    }
  }

  // 打开对话时服务端仍在生成（conversation.is_processing）：先锁住输入并
  // 亮起进度指示。此时无法回放已错过的 agent.turn.started，不知道 message_id，
  // 流式占位与增量交由后续 WS 事件补齐；若生成早已结束、收尾事件也已错过，
  // 由 activity 轮询在 narrative 回到 idle 时兜底调用 stopGenerating 复位。
  function markGenerating() {
    isGenerating.value = true
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
    hasEarlier,
    loadingEarlier,
    load,
    loadEarlier,
    reset,
    addOptimisticUser,
    removeById,
    removeStaleTempUsers,
    applyServerMessage,
    replaceBranch,
    beginStream,
    appendDelta,
    resetStreamText,
    appendReasoningDelta,
    collapseReasoning,
    endStream,
    abortStream,
    markGenerating,
    stopGenerating,
  }
})
