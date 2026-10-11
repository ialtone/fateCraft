import { defineStore } from 'pinia'
import { ref } from 'vue'
import type { IncomingSocketEvent } from '@/types'
import { isKnownSocketEvent } from '@/types'
import { useChatStore } from './chat'
import { useStateStore } from './state'
import { useContextStore } from './context'
import { useConversationStore } from './conversations'
import { useRewardStore } from './rewards'
import { useCampaignStore } from './campaign'
import { useRuntimeStore } from './runtime'
import { useGuidanceStore } from './guidance'

// 自动重连参数：指数退避，1s 起步，上限 30s。
const RECONNECT_BASE_DELAY = 1000
const RECONNECT_MAX_DELAY = 30000
// 带 operation 标识但只用于展示或重新加载的事件，不参与 operation 锁定。
const NON_LOCKING_SCOPED_EVENTS = new Set(['notice', 'error', 'generation.replayed'])

export const useSocketStore = defineStore('socket', () => {
  const ws = ref<WebSocket | null>(null)
  const connected = ref(false)
  // 断线后正在尝试自动重连
  const reconnecting = ref(false)
  const lastError = ref<string | null>(null)
  const queuedCount = ref(0)
  const currentConvId = ref<string | null>(null)
  // 重连尝试次数（成功连接后重置）
  let reconnectAttempts = 0
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null
  const seenAgentEvents = new Set<string>()
  const lastAgentSequence = new Map<string, number>()
  let activeAgentAttempt: string | null = null
  let activeOperationID: string | null = null
  // 已收到终止事件的 operation。它迟到的 notice/error 只展示、不再重新锁定，
  // 否则下一回合的新 operation 事件会被当作“其他操作”全部丢弃。
  const finishedOperations = new Set<string>()

  function finishActiveOperation() {
    if (activeOperationID) {
      finishedOperations.add(activeOperationID)
      if (finishedOperations.size > 256) {
        const oldest = finishedOperations.values().next().value
        if (oldest) finishedOperations.delete(oldest)
      }
    }
    activeAgentAttempt = null
    activeOperationID = null
  }

  // 切换对话时关闭旧连接并打开新连接（doc 05）。
  function connect(convId: string) {
    disconnect()
    currentConvId.value = convId
    openSocket(convId)
  }

  // 重连成功后重新拉取全量快照。
  // 为什么需要：WS 断线期间服务端推送的 agent.* / message_created /
  // state_update / context_built / conversation_ended 等事件没有任何补发机制，
  // 错过即永久丢失；不重拉的话，时间线、状态面板和对话状态会缺一段，
  // 直到用户手动刷新页面。
  async function resyncConversation(convId: string) {
    useGuidanceStore().load(convId)
    const chat = useChatStore()
    const state = useStateStore()
    const context = useContextStore()
    const convStore = useConversationStore()
    const campaign = useCampaignStore()
    // 状态与上下文相互独立、失败互不影响；resync 失败不致命，静默忽略即可
    state.load(convId).catch(() => {})
    context.loadRuntimeInspector(convId).catch(() => {})

    // 对话本体与消息列表需要等到结果再做流式复位判断：
    // - 对话本体带回 status 真值（补上断线期间错过的结局/结束）
    //   与 is_processing（服务端是否仍在生成）
    // - 消息列表带回断线期间落库的新消息（整表替换天然不会产生重复）
    // 先记下等待前的流式 id：若等待期间收到了新的 agent.turn.started，
    // 说明快照里的 is_processing 已过期，不能拿旧快照去复位新一轮生成。
    const streamingIdBefore = chat.streamingId
    const [convRes] = await Promise.allSettled([convStore.load(convId), chat.load(convId)])

    if (currentConvId.value === convId) convStore.loadEndingState(convId).catch(() => {})

    // 等待期间用户已切走（disconnect 会清空 currentConvId）：立即中止，
    // 避免对旧对话的流式复位波及新对话
    if (currentConvId.value !== convId) return

    if (convRes.status === 'fulfilled' && convRes.value.campaign_id) {
      campaign.load(convRes.value.campaign_id).catch(() => {})
    }

    // 服务端已不在生成：说明断线窗口里错过了 agent.turn.completed / error，
    // 必须复位流式状态，否则输入框会永久停留在「叙事织就中」。
    // 服务端仍在生成、或对话本体拉取失败（真值未知）：保持现状不强行复位，
    // 让后续的流式事件自然收尾——宁可保守也不误杀仍在进行的生成。
    // 已知限制：仍在生成时上面的 chat.load 会冲掉尚未落库的流式占位，
    // 后续增量在 message_created 到达前暂不可见（找不到对应消息被丢弃），
    // 最终消息不丢也不重复，只是这段时间内看不到打字机效果。
    if (convRes.status === 'fulfilled' && chat.streamingId === streamingIdBefore) {
      if (convRes.value.is_processing) chat.markGenerating()
      else chat.stopGenerating()
    }
  }

  // 实际建立 WebSocket 连接（供首连与重连复用）。
  function openSocket(convId: string) {
    const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
    const socket = new WebSocket(`${proto}://${window.location.host}/ws/conversations/${convId}`)
    ws.value = socket

    socket.onopen = () => {
      if (ws.value !== socket || currentConvId.value !== convId) return
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
      if (ws.value !== socket) return
      connected.value = false
      scheduleReconnect()
    }
    socket.onerror = () => {
      if (ws.value !== socket) return
      lastError.value = 'WebSocket 连接出错'
    }
    socket.onmessage = (ev) => {
      if (ws.value !== socket || currentConvId.value !== convId) return
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
    seenAgentEvents.clear()
    lastAgentSequence.clear()
    finishedOperations.clear()
    activeAgentAttempt = null
    activeOperationID = null
    queuedCount.value = 0
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
    if (ev.conversation_id && ev.conversation_id !== currentConvId.value) return

    const sequenced = ev.type.startsWith('agent.') || ev.type === 'guidance.updated'
    if (sequenced && !acceptAgentEvent(ev)) return
    if (!sequenced && (ev.operation_id || ev.attempt_id) && !acceptScopedEvent(ev)) return

    const chat = useChatStore()
    const state = useStateStore()
    const context = useContextStore()
    const conv = useConversationStore()
    const rewards = useRewardStore()
    const campaign = useCampaignStore()
    const runtime = useRuntimeStore()
    const guidance = useGuidanceStore()

    switch (ev.type) {
      case 'agent.turn.started':
        guidance.reset()
        lastError.value = null
        context.setNarrative('preparing')
        if (ev.payload?.message_id) chat.beginStream(ev.payload.message_id)
        if (currentConvId.value) context.loadRuntimeInspector(currentConvId.value).catch(() => {})
        break
      case 'agent.step.started':
        context.setNarrative('model', { step_no: ev.step_no })
        break
      case 'agent.tool.started':
        context.setNarrative('tool', {
          step_no: ev.step_no,
          tool_category: ev.payload?.category,
        })
        if (currentConvId.value) context.loadRuntimeInspector(currentConvId.value).catch(() => {})
        break
      case 'agent.tool.finished':
        context.setNarrative('preparing', { step_no: ev.step_no })
        break
      case 'agent.reasoning.delta':
        context.setNarrative('model', { step_no: ev.step_no })
        if (ev.payload?.message_id && ev.payload.delta)
          chat.appendReasoningDelta(ev.payload.message_id, ev.payload.delta)
        break
      case 'agent.text.delta':
        context.setNarrative('streaming', { step_no: ev.step_no })
        if (ev.payload?.message_id && ev.payload.delta) {
          chat.collapseReasoning(ev.payload.message_id)
          chat.appendDelta(ev.payload.message_id, ev.payload.delta)
        }
        break
      case 'agent.text.reset':
        if (ev.payload?.message_id) chat.resetStreamText(ev.payload.message_id)
        break
      case 'agent.turn.completed':
        if (currentConvId.value) conv.loadEndingState(currentConvId.value).catch(() => {})
        context.setNarrative('idle')
        if (ev.payload?.message_id) chat.endStream(ev.payload.message_id)
        rewards.load().catch(() => {})
        if (currentConvId.value) context.loadRuntimeInspector(currentConvId.value).catch(() => {})
        finishActiveOperation()
        break
      case 'agent.turn.partial':
        if (currentConvId.value) conv.loadEndingState(currentConvId.value).catch(() => {})
        guidance.reset()
        context.setNarrative('idle')
        // partial 回合已经提交了可信事实，重新生成要消耗改写额度；
        // 这里不写死数量，交给平台能力和随后刷新的 inspector 决定。
        context.setTurnCapabilities({
          turn_status: 'partial',
          can_regenerate: true,
          regenerate_cost_quantity: undefined,
        })
        if (ev.payload?.message_id) chat.abortStream(ev.payload.message_id)
        lastError.value = ev.payload?.message || '动作已执行，但叙事未完成。请继续叙事。'
        rewards.load().catch(() => {})
        if (currentConvId.value) context.loadRuntimeInspector(currentConvId.value).catch(() => {})
        finishActiveOperation()
        break
      case 'agent.turn.failed':
      case 'agent.turn.cancelled':
        if (currentConvId.value) conv.loadEndingState(currentConvId.value).catch(() => {})
        guidance.reset()
        context.setNarrative('idle')
        context.setTurnCapabilities({
          turn_status: ev.type === 'agent.turn.cancelled' ? 'cancelled' : 'failed',
          can_regenerate: true,
          regenerate_cost_quantity: 0,
        })
        if (ev.payload?.message_id) chat.abortStream(ev.payload.message_id)
        // 失败或取消后付费的重新生成会先退款，再推送终止事件；这里刷新余额。
        rewards.load().catch(() => {})
        if (currentConvId.value) context.loadRuntimeInspector(currentConvId.value).catch(() => {})
        finishActiveOperation()
        break
      case 'agent.queue.updated':
        queuedCount.value = Math.max(0, ev.payload?.count || 0)
        break
      case 'message_created':
        if (ev.message?.type === 'user') guidance.reset()
        if (ev.message) chat.applyServerMessage(ev.message)
        break
      case 'guidance.updated':
        guidance.set(ev.payload.guidance)
        break
      case 'message_updated':
        if (ev.message) chat.applyServerMessage(ev.message)
        break
      case 'branch.replaced':
        guidance.reset()
        chat.replaceBranch(ev.payload?.invalidated_message_ids ?? [], ev.message)
        rewards.load().catch(() => {})
        break
      case 'generation.replayed':
        if (currentConvId.value) guidance.load(currentConvId.value)
        if (currentConvId.value) {
          const replayConvId = currentConvId.value
          Promise.allSettled([conv.load(replayConvId), chat.load(replayConvId)]).then(
            ([convResult]) => {
              if (currentConvId.value !== replayConvId || convResult.status !== 'fulfilled') return
              if (convResult.value.is_processing) chat.markGenerating()
              else chat.stopGenerating()
            },
          )
          context.loadRuntimeInspector(currentConvId.value).catch(() => {})
        }
        break
      case 'module.action_committed':
        if (currentConvId.value) conv.loadEndingState(currentConvId.value).catch(() => {})
        if (currentConvId.value) resyncConversation(currentConvId.value).catch(() => {})
        break
      case 'state_update':
        guidance.reset()
        if (currentConvId.value && !chat.isGenerating) guidance.load(currentConvId.value)
        if (ev.state) state.set(ev.state)
        // Guidance visibility is projected by the server against the latest
        // player state.  Refresh the optional runtime presentation at this
        // explicit state boundary so items hidden at initial load can become
        // visible later without polling or per-delta recomputation.
        if (currentConvId.value) runtime.load(currentConvId.value).catch(() => {})
        break
      case 'campaign_updated':
        guidance.reset()
        if (currentConvId.value && !chat.isGenerating) guidance.load(currentConvId.value)
        if (ev.campaign) {
          campaign.applyCampaign(ev.campaign)
          campaign.load(ev.campaign.id).catch(() => {})
        }
        break
      case 'context_built':
        // Context is constructed before the outcome is known. Only committed
        // successful attempts should replace the runtime panel statistics.
        break
      case 'activity_updated':
        if (ev.activity === 'memory' && ev.status) {
          context.setActivity('memory', ev.status)
          if (currentConvId.value) context.loadRuntimeInspector(currentConvId.value).catch(() => {})
        }
        break
      case 'conversation_ended':
        guidance.reset()
        conv.markEnded()
        chat.stopGenerating()
        context.setNarrative('idle')
        break
      case 'ending.proposed':
        if (currentConvId.value) conv.loadEndingState(currentConvId.value).catch(() => {})
        break
      case 'challenge_completed':
        guidance.reset()
        if (ev.completion) conv.consumeCompletion(ev.completion)
        rewards.load().catch(() => {})
        chat.stopGenerating()
        context.setNarrative('idle')
        break
      case 'notice':
        // 非终止提示（对话忙、请求被拒绝、已取消等）：只展示横幅。正在进行的
        // 流式输出（可能来自另一个标签页）不能被打断，因此绝不复位 streamingId。
        lastError.value = ev.error || '请求未执行'
        if (!chat.streamingId) {
          // 本端乐观标记了"生成中"但服务端拒绝了请求：解锁输入并清理幽灵占位。
          chat.stopGenerating()
          chat.removeStaleTempUsers()
          if (currentConvId.value) context.loadActivity(currentConvId.value).catch(() => {})
        }
        break
      case 'error':
        guidance.reset()
        if (currentConvId.value) guidance.load(currentConvId.value)
        lastError.value = ev.error || '生成出错'
        // 本轮生成已经终止：复位生成状态并闭合未完成的流式消息
        chat.stopGenerating()
        context.setNarrative('idle')
        chat.removeStaleTempUsers()
        rewards.load().catch(() => {})
        if (currentConvId.value) {
          chat.load(currentConvId.value).catch(() => {})
          context.loadActivity(currentConvId.value).catch(() => {})
        }
        finishActiveOperation()
        break
    }
  }

  function acceptAgentEvent(ev: any) {
    const eventId = typeof ev.event_id === 'string' ? ev.event_id : ''
    if (eventId && seenAgentEvents.has(eventId)) return false
    if (eventId) {
      seenAgentEvents.add(eventId)
      if (seenAgentEvents.size > 2048) {
        const oldest = seenAgentEvents.values().next().value
        if (oldest) seenAgentEvents.delete(oldest)
      }
    }
    const attemptId = typeof ev.attempt_id === 'string' ? ev.attempt_id : ''
    const operationId = typeof ev.operation_id === 'string' ? ev.operation_id : ''
    const sequence = typeof ev.sequence === 'number' ? ev.sequence : 0
    if (operationId) {
      // 已结束操作的迟到 agent 事件（增量等）直接丢弃。
      if (finishedOperations.has(operationId)) return false
      if (activeOperationID && activeOperationID !== operationId) {
        // 服务端对同一对话串行执行生成：新的 turn.started 说明上一次操作已经结束
        // （即便它的终止事件丢失），允许切换锁定；其他事件仍按旧操作过滤。
        if (ev.type !== 'agent.turn.started') return false
        finishActiveOperation()
      }
      activeOperationID = operationId
    }
    if (!attemptId || sequence <= 0) return true
    if (activeAgentAttempt && activeAgentAttempt !== attemptId) return false
    activeAgentAttempt = attemptId
    const previous = lastAgentSequence.get(attemptId) || 0
    // A coalesced delta carries the first source sequence in its payload and
    // the last source sequence in `sequence`. Compare the range start so an
    // intentional merge is not mistaken for a dropped event.
    const sequenceStart =
      typeof ev.payload?.sequence_start === 'number' && ev.payload.sequence_start > 0
        ? ev.payload.sequence_start
        : sequence
    if (sequenceStart <= previous) return false
    lastAgentSequence.set(attemptId, sequence)
    if (previous > 0 && sequenceStart > previous + 1 && currentConvId.value) {
      resyncConversation(currentConvId.value).catch(() => {})
    }
    return true
  }

  // 同一次生成产生的事实事件也携带 durable operation / attempt 标识。拒绝来自
  // 已被替换分支的迟到事件，避免重新生成或改写后把界面上的状态、Campaign、消息
  // 投影回滚成旧数据。
  function acceptScopedEvent(ev: any) {
    // 提示、错误和回放通知只触发展示或重新加载，从不参与锁定：它们常常紧跟在
    // 终止事件之后，或者属于一个早已结束的请求，锁定到它们会让下一回合的事件全部被丢弃。
    if (NON_LOCKING_SCOPED_EVENTS.has(ev.type)) return true
    const operationId = typeof ev.operation_id === 'string' ? ev.operation_id : ''
    const attemptId = typeof ev.attempt_id === 'string' ? ev.attempt_id : ''
    if (operationId) {
      // 已结束操作的迟到事实直接丢弃。
      if (finishedOperations.has(operationId)) return false
      if (activeOperationID && activeOperationID !== operationId) return false
      activeOperationID = operationId
    }
    if (attemptId) {
      if (activeAgentAttempt && activeAgentAttempt !== attemptId) return false
      activeAgentAttempt = attemptId
    }
    return true
  }

  // 通过 WebSocket 发送用户输入。
  function sendUserInput(content: string, requestId?: string) {
    if (ws.value && connected.value) {
      ws.value.send(JSON.stringify({ type: 'user_input', content, request_id: requestId }))
      return true
    }
    return false
  }

  function cancel() {
    if (ws.value && connected.value) {
      ws.value.send(JSON.stringify({ type: 'cancel_generation' }))
    }
  }

  function steer(content: string, requestId?: string) {
    if (ws.value && connected.value && content.trim()) {
      ws.value.send(JSON.stringify({ type: 'steer_generation', content, request_id: requestId }))
      return true
    }
    return false
  }

  function followUp(content: string, requestId?: string) {
    if (ws.value && connected.value && content.trim()) {
      ws.value.send(JSON.stringify({ type: 'follow_up', content, request_id: requestId }))
      return true
    }
    return false
  }

  function clearQueue() {
    if (ws.value && connected.value) {
      ws.value.send(JSON.stringify({ type: 'clear_generation_queue' }))
      return true
    }
    return false
  }

  function clearError() {
    lastError.value = null
  }

  return {
    connected,
    reconnecting,
    lastError,
    queuedCount,
    currentConvId,
    connect,
    disconnect,
    sendUserInput,
    cancel,
    steer,
    followUp,
    clearQueue,
    clearError,
  }
})
