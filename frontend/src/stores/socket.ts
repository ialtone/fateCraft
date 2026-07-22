import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { IncomingSocketEvent } from '@/types'
import { isKnownSocketEvent } from '@/types'
import { useChatStore } from './chat'
import { useStateStore } from './state'
import { useContextStore } from './context'
import { useConversationStore } from './conversations'

// 自动重连参数：指数退避，1s 起步，上限 30s。
const RECONNECT_BASE_DELAY = 1000
const RECONNECT_MAX_DELAY = 30000

export const useSocketStore = defineStore('socket', () => {
  const ws = ref<WebSocket | null>(null)
  const connected = ref(false)
  // 断线后正在尝试自动重连
  const reconnecting = ref(false)
  const lastError = ref<string | null>(null)
  const currentConvId = ref<string | null>(null)
  // 重连尝试次数（成功连接后重置）
  let reconnectAttempts = 0
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null

  // 切换对话时关闭旧连接并打开新连接（doc 05）。
  function connect(convId: string) {
    disconnect()
    currentConvId.value = convId
    openSocket(convId)
  }

  // 重连成功后重新拉取全量快照。
  // 为什么需要：WS 断线期间服务端推送的 generation_delta / message_created /
  // state_update / context_built / conversation_ended 等事件没有任何补发机制，
  // 错过即永久丢失；不重拉的话，时间线、状态面板和对话状态会缺一段，
  // 直到用户手动刷新页面。
  async function resyncConversation(convId: string) {
    const chat = useChatStore()
    const state = useStateStore()
    const context = useContextStore()
    const convStore = useConversationStore()
    // 状态与上下文相互独立、失败互不影响；resync 失败不致命，静默忽略即可
    state.load(convId).catch(() => {})
    context.loadLast(convId).catch(() => {})

    // 对话本体与消息列表需要等到结果再做流式复位判断：
    // - 对话本体带回 status 真值（补上断线期间错过的结局/结束）
    //   与 is_processing（服务端是否仍在生成）
    // - 消息列表带回断线期间落库的新消息（整表替换天然不会产生重复）
    // 先记下等待前的流式 id：若等待期间收到了新的 generation_started，
    // 说明快照里的 is_processing 已过期，不能拿旧快照去复位新一轮生成。
    const streamingIdBefore = chat.streamingId
    const [convRes] = await Promise.allSettled([convStore.load(convId), chat.load(convId)])

    // 等待期间用户已切走（disconnect 会清空 currentConvId）：立即中止，
    // 避免对旧对话的流式复位波及新对话
    if (currentConvId.value !== convId) return

    // 服务端已不在生成：说明断线窗口里错过了 generation_finished / error，
    // 必须复位流式状态，否则输入框会永久停留在「叙事织就中」。
    // 服务端仍在生成、或对话本体拉取失败（真值未知）：保持现状不强行复位，
    // 让后续的流式事件自然收尾——宁可保守也不误杀仍在进行的生成。
    // 已知限制：仍在生成时上面的 chat.load 会冲掉尚未落库的流式占位，
    // 后续增量在 message_created 到达前暂不可见（找不到对应消息被丢弃），
    // 最终消息不丢也不重复，只是这段时间内看不到打字机效果。
    if (
      convRes.status === 'fulfilled' &&
      !convRes.value.is_processing &&
      chat.streamingId === streamingIdBefore
    ) {
      chat.stopGenerating()
    }
  }

  // 实际建立 WebSocket 连接（供首连与重连复用）。
  function openSocket(convId: string) {
    const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
    const socket = new WebSocket(`${proto}://${window.location.host}/ws/conversations/${convId}`)
    ws.value = socket

    socket.onopen = () => {
      connected.value = true
      // 连接已恢复：清掉断线期间 onerror 留下的错误提示，避免横幅常驻不散
      lastError.value = null
      // 此前有过退避尝试则说明本次是断线重连，而非首次连接
      const isReconnect = reconnectAttempts > 0
      // 重连成功：重置退避状态
      reconnecting.value = false
      reconnectAttempts = 0
      if (isReconnect && currentConvId.value) resyncConversation(currentConvId.value)
    }
    socket.onclose = () => {
      connected.value = false
      scheduleReconnect()
    }
    socket.onerror = () => {
      lastError.value = 'WebSocket 连接出错'
    }
    socket.onmessage = (ev) => {
      try {
        handleEvent(JSON.parse(ev.data) as IncomingSocketEvent)
      } catch {
        // 忽略无法解析的消息。
      }
    }
  }

  // 指数退避安排下一次重连。
  function scheduleReconnect() {
    if (!currentConvId.value || reconnectTimer) return
    reconnecting.value = true
    const delay = Math.min(RECONNECT_BASE_DELAY * 2 ** reconnectAttempts, RECONNECT_MAX_DELAY)
    reconnectAttempts++
    reconnectTimer = setTimeout(() => {
      reconnectTimer = null
      if (currentConvId.value) openSocket(currentConvId.value)
    }, delay)
  }

  function disconnect() {
    // 主动断开：取消重连并清空状态
    if (reconnectTimer) {
      clearTimeout(reconnectTimer)
      reconnectTimer = null
    }
    reconnecting.value = false
    reconnectAttempts = 0
    currentConvId.value = null
    // 清掉旧连接遗留的错误提示，避免切换对话后带出上一个对话的报错
    lastError.value = null
    if (ws.value) {
      ws.value.onclose = null
      ws.value.close()
      ws.value = null
    }
    connected.value = false
  }

  function handleEvent(ev: IncomingSocketEvent) {
    // 后端新增而前端尚未定义的事件类型：直接忽略，防止炸掉运行时；
    // 过滤后 ev 收窄为 SocketEvent，switch 各分支按 type 字面量继续收窄成员。
    if (!isKnownSocketEvent(ev)) return

    const chat = useChatStore()
    const state = useStateStore()
    const context = useContextStore()
    const conv = useConversationStore()

    switch (ev.type) {
      case 'generation_started':
        if (ev.message_id) chat.beginStream(ev.message_id)
        break
      case 'generation_delta':
        if (ev.message_id && ev.delta) chat.appendDelta(ev.message_id, ev.delta)
        break
      case 'generation_finished':
        if (ev.message_id) chat.endStream(ev.message_id)
        break
      case 'generation_discarded':
        if (ev.message_id) chat.discardStream(ev.message_id)
        break
      case 'message_created':
        if (ev.message) chat.applyServerMessage(ev.message)
        break
      case 'message_updated':
        if (ev.message) chat.applyServerMessage(ev.message)
        break
      case 'state_update':
        if (ev.state) state.set(ev.state)
        break
      case 'context_built':
        if (ev.context_build_id) context.loadById(ev.context_build_id)
        break
      case 'conversation_ended':
        conv.markEnded()
        chat.stopGenerating()
        break
      case 'ending_triggered':
        // 结局信息交给对话 store 记录（业务结果不属于连接层），由结局弹窗消费
        conv.setEndingTriggered({
          type: ev.ending_type || 'neutral',
          title: ev.ending_title || '故事终章',
          summary: ev.ending_summary || '',
          spiritStones: ev.spirit_stones,
          survivalTurns: ev.survival_turns,
        })
        conv.markEnded()
        chat.stopGenerating()
        break
      case 'error':
        lastError.value = ev.error || '生成出错'
        // 复位生成状态并闭合未完成的流式消息
        chat.stopGenerating()
        // 清理被生成锁拒绝、永远等不到 message_created 的幽灵 temp 用户消息。
        // 已被认领的用户消息此时已不是 temp- 前缀，不受影响。
        chat.removeStaleTempUsers()
        break
    }
  }

  // 通过 WebSocket 发送用户输入。
  function sendUserInput(content: string) {
    if (ws.value && connected.value) {
      ws.value.send(JSON.stringify({ type: 'user_input', content }))
      return true
    }
    return false
  }

  function cancel() {
    if (ws.value && connected.value) {
      ws.value.send(JSON.stringify({ type: 'cancel_generation' }))
    }
  }

  function clearError() {
    lastError.value = null
  }

  return {
    connected,
    reconnecting,
    lastError,
    currentConvId,
    connect,
    disconnect,
    sendUserInput,
    cancel,
    clearError,
  }
})
