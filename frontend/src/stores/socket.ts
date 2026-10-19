import { defineStore } from 'pinia'
import { ref, shallowRef, watch } from 'vue'
import type { DisplayMessage, IncomingSocketEvent } from '@/types'
import { isKnownSocketEvent } from '@/types'
import { isRetryableReadError } from '@/utils/recovery'
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
  const ws = shallowRef<WebSocket | null>(null)
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
  // Campaign 正文先到达、快照后到达时，先缓冲正文，避免正文与侧栏处于不同版本。
  const pendingCampaignMessages = new Map<string, DisplayMessage[]>()
  let session = 0
  let ready = false
  let recovering = false
  let recoveryFlight: Promise<void> | null = null
  let businessPending = false
  let activityPending = false
  let buffered: IncomingSocketEvent[] = []
  let bufferedBytes = 0
  let readyTimer: ReturnType<typeof setTimeout> | null = null
  let retryTimer: ReturnType<typeof setTimeout> | null = null
  let taskTimer: ReturnType<typeof setTimeout> | null = null
  let recoveryTimer: ReturnType<typeof setTimeout> | null = null
  let stopTaskWatch: (() => void) | null = null
  let retryDelay = 5000
  let taskDelay = 75000
  let targetVersion = 0
  let narrativeProgress = 0
  let memoryProgress = 0
  const terminalAttempts = new Set<string>()

  const foreground = () => typeof document === 'undefined' || document.visibilityState !== 'hidden'
  const online = () => typeof navigator === 'undefined' || navigator.onLine !== false

  function stopRecoveryTimers() {
    if (retryTimer) clearTimeout(retryTimer)
    if (taskTimer) clearTimeout(taskTimer)
    retryTimer = taskTimer = null
  }

  function scheduleFallback(immediate = false) {
    if (!currentConvId.value || retryTimer || !foreground() || !online()) return
    const epoch = session
    const wait = immediate ? 0 : retryDelay * (0.9 + Math.random() * 0.2)
    retryTimer = setTimeout(() => {
      retryTimer = null
      if (epoch !== session || !currentConvId.value) return
      void resyncConversation(currentConvId.value, true)
        .catch(() => {})
        .finally(() => {
          if (epoch !== session) return
          retryDelay = retryDelay === 5000 ? 10000 : 30000
          if (!ready || businessPending || useContextStore().activityStale) scheduleFallback()
        })
    }, wait)
  }

  function armTaskWatchdog() {
    if (taskTimer) clearTimeout(taskTimer)
    taskTimer = null
    if (!ready || !currentConvId.value || !foreground() || !online()) return
    const activity = useContextStore().activity
    const generating = activity.is_processing || useChatStore().isGenerating
    const memoryBusy = activity.memory === 'queued' || activity.memory === 'running'
    if (!generating && !memoryBusy) return
    const deadline = Math.min(
      generating ? narrativeProgress + taskDelay : Infinity,
      memoryBusy ? memoryProgress + taskDelay : Infinity,
    )
    const epoch = session
    taskTimer = setTimeout(
      () => {
        taskTimer = null
        if (epoch !== session || !currentConvId.value) return
        const current = useContextStore().activity
        const narrativeDue =
          (current.is_processing || useChatStore().isGenerating) &&
          Date.now() - narrativeProgress >= taskDelay
        const memoryDue = current.memory !== 'idle' && Date.now() - memoryProgress >= taskDelay
        if (!narrativeDue && !memoryDue) {
          armTaskWatchdog()
          return
        }
        const context = useContextStore()
        const previous = context.activity.activity_version
        void context
          .loadActivity(currentConvId.value)
          .then(() => {
            if (epoch !== session) return
            const snapshot = context.activity
            if (
              (snapshot.is_processing === false && useChatStore().isGenerating) ||
              snapshot.activity_version !== previous
            ) {
              return resyncConversation(currentConvId.value!, true)
            }
          })
          .catch(() => {
            if (epoch === session) scheduleFallback()
          })
          .finally(() => {
            if (epoch !== session) return
            taskDelay = Math.min(taskDelay * 2, 300000)
            narrativeProgress = memoryProgress = Date.now()
            armTaskWatchdog()
          })
      },
      Math.max(0, deadline - Date.now()),
    )
  }

  function resume() {
    stopRecoveryTimers()
    if (!currentConvId.value || !foreground() || !online()) return
    retryDelay = 5000
    useContextStore().invalidateInspector()
    if (ready) void resyncConversation(currentConvId.value, true).catch(() => scheduleFallback())
    else scheduleFallback(true)
  }

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
    useContextStore().enter(convId)
    narrativeProgress = memoryProgress = Date.now()
    if (typeof window !== 'undefined') {
      window.addEventListener('online', resume)
      window.addEventListener('offline', resume)
      document.addEventListener('visibilitychange', resume)
    }
    stopTaskWatch = watch(
      () => [
        useChatStore().isGenerating,
        useContextStore().activity.memory,
        useContextStore().activity.is_processing,
      ],
      armTaskWatchdog,
    )
    scheduleFallback()
    openSocket(convId)
  }

  // 重连成功后重新拉取全量快照。
  // 为什么需要：WS 断线期间服务端推送的 agent.* / message_created /
  // state_update / context_built / conversation_ended 等事件没有任何补发机制，
  // 错过即永久丢失；不重拉的话，时间线、状态面板和对话状态会缺一段，
  // 直到用户手动刷新页面。
  function resyncConversation(
    convId: string,
    full = true,
    minimumVersion = 0,
    readActivity = true,
  ): Promise<void> {
    if (currentConvId.value !== convId) return Promise.resolve()
    businessPending ||= full
    activityPending ||= readActivity
    targetVersion = Math.max(targetVersion, minimumVersion)
    if (recoveryFlight) return recoveryFlight
    const epoch = session
    const request = (async () => {
      const chat = useChatStore()
      const context = useContextStore()
      const conv = useConversationStore()
      const campaign = useCampaignStore()
      recovering = true
      recoveryTimer = setTimeout(() => {
        if (epoch === session) ws.value?.close()
      }, 15000)
      try {
        // At most two immediate passes. Subsequent changes recover with backoff.
        for (let pass = 0; pass < 2; pass++) {
          const business = businessPending
          const activityRead = activityPending
          businessPending = false
          activityPending = false
          const before = context.activity
          const results = await Promise.allSettled([
            ...(activityRead ? [context.loadActivity(convId, targetVersion)] : []),
            ...(business
              ? [
                  conv.load(convId),
                  chat.load(convId),
                  useStateStore().load(convId),
                  useGuidanceStore().load(convId),
                  conv.loadEndingState(convId),
                  useRuntimeStore().load(convId),
                  useRewardStore().load(),
                ]
              : []),
          ])
          if (epoch !== session) return
          const failure = results.find((result) => result.status === 'rejected')
          if (failure?.status === 'rejected') throw failure.reason
          const snapshot = context.activity
          if (
            !business &&
            ((snapshot.is_processing === false && chat.isGenerating) ||
              snapshot.branch_epoch !== before.branch_epoch ||
              snapshot.turn_id !== before.turn_id ||
              snapshot.conversation_status !== before.conversation_status)
          ) {
            businessPending = true
          }
          if (business) {
            if (context.inspectorVisible && context.inspectorDirty) {
              void context.loadRuntimeInspector(convId)
            }
            if (conv.current?.campaign_id) {
              await campaign.load(conv.current.campaign_id)
              if (epoch !== session) return
              const latest = [...chat.messages]
                .reverse()
                .find((m) => m.type === 'assistant' && m.campaign_revision !== undefined)
              if (
                latest &&
                !campaign.matches(latest.campaign_revision, latest.campaign_state_hash)
              ) {
                await campaign.load(conv.current.campaign_id)
                if (epoch !== session) return
              }
            }
            if (snapshot.attempt_id && snapshot.turn_status !== 'pending')
              terminalAttempts.add(snapshot.attempt_id)
            const processing = snapshot.is_processing ?? conv.current?.is_processing
            if (processing === false) {
              finishActiveOperation()
              chat.stopGenerating()
            } else if (processing) {
              if (activeAgentAttempt !== snapshot.attempt_id) finishActiveOperation()
              activeAgentAttempt = snapshot.attempt_id || null
              chat.markGenerating()
            }
            if (conv.current && snapshot.conversation_status) {
              conv.current.status = snapshot.conversation_status as typeof conv.current.status
              if (snapshot.is_processing !== undefined)
                conv.current.is_processing = snapshot.is_processing
            }
          }
          if (!businessPending && !activityPending) break
        }
        if (ready) retryDelay = 5000
      } catch (error: any) {
        if (epoch === session) businessPending = true
        if (epoch === session && !isRetryableReadError(error)) {
          disconnect()
          lastError.value = '无法继续访问此对话，请重新登录或返回对话列表'
        }
        throw error
      } finally {
        if (epoch === session) {
          if (recoveryTimer) clearTimeout(recoveryTimer)
          recoveryTimer = null
          recovering = false
          const events = buffered
          buffered = []
          bufferedBytes = 0
          for (const event of events) {
            const attempt = typeof event.attempt_id === 'string' ? event.attempt_id : null
            if (attempt && context.activity.attempt_id && attempt !== context.activity.attempt_id) {
              if (event.type === 'agent.turn.started') businessPending = true
              continue
            }
            // State events have no comparable REST revision. A buffered older
            // state must not roll back a newer snapshot; verify once more.
            if (event.type === 'state_update') {
              businessPending = true
              continue
            }
            handleEvent(event)
          }
          armTaskWatchdog()
        }
      }
    })()
    recoveryFlight = request
    void request
      .finally(() => {
        if (epoch !== session || recoveryFlight !== request) return
        recoveryFlight = null
        if (businessPending || activityPending) scheduleFallback()
      })
      .catch(() => {})
    return request
  }

  // 实际建立 WebSocket 连接（供首连与重连复用）。
  function openSocket(convId: string) {
    ready = false
    recovering = false
    buffered = []
    bufferedBytes = 0
    if (readyTimer) clearTimeout(readyTimer)
    const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
    const socket = new WebSocket(`${proto}://${window.location.host}/ws/conversations/${convId}`)
    ws.value = socket

    socket.onopen = () => {
      if (ws.value !== socket || currentConvId.value !== convId) return
      // 连接已恢复：清掉断线期间 onerror 留下的错误提示，避免横幅常驻不散
      lastError.value = null
      // Subscription readiness is required. A missing acknowledgement enters
      // disconnected recovery instead of treating an unregistered socket as live.
      readyTimer = setTimeout(() => {
        readyTimer = null
        if (ws.value !== socket || currentConvId.value !== convId || ready) return
        socket.close()
      }, 5000)
    }
    socket.onclose = () => {
      if (ws.value !== socket) return
      session++
      useContextStore().invalidateRequests()
      recoveryFlight = null
      recovering = false
      buffered = []
      bufferedBytes = 0
      if (recoveryTimer) clearTimeout(recoveryTimer)
      recoveryTimer = null
      ready = false
      if (readyTimer) clearTimeout(readyTimer)
      readyTimer = null
      connected.value = false
      stopRecoveryTimers()
      scheduleFallback()
      scheduleReconnect()
    }
    socket.onerror = () => {
      if (ws.value !== socket) return
      lastError.value = 'WebSocket 连接出错'
    }
    socket.onmessage = (ev) => {
      if (ws.value !== socket || currentConvId.value !== convId) return
      try {
        const event = JSON.parse(ev.data) as IncomingSocketEvent
        if (!event || typeof event.type !== 'string') throw new Error('Invalid socket event')
        if (event.type === 'subscription.ready') {
          if (event.conversation_id !== convId) return
          if (ready) return
          ready = true
          connected.value = true
          reconnecting.value = false
          reconnectAttempts = 0
          if (readyTimer) clearTimeout(readyTimer)
          readyTimer = null
          stopRecoveryTimers()
          void resyncConversation(convId).catch(() => scheduleFallback())
        } else if (!ready || recovering) {
          buffered.push(event)
          bufferedBytes += typeof ev.data === 'string' ? ev.data.length * 2 : 0
          if (buffered.length > 2048 || bufferedBytes > 2 * 1024 * 1024) socket.close()
        } else handleEvent(event)
      } catch {
        socket.close()
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
    session++
    useContextStore().invalidateRequests()
    ready = false
    recovering = false
    recoveryFlight = null
    businessPending = false
    activityPending = false
    buffered = []
    bufferedBytes = 0
    terminalAttempts.clear()
    stopRecoveryTimers()
    if (readyTimer) clearTimeout(readyTimer)
    if (recoveryTimer) clearTimeout(recoveryTimer)
    readyTimer = recoveryTimer = null
    stopTaskWatch?.()
    stopTaskWatch = null
    if (typeof window !== 'undefined') {
      window.removeEventListener('online', resume)
      window.removeEventListener('offline', resume)
      document.removeEventListener('visibilitychange', resume)
    }
    retryDelay = 5000
    taskDelay = 75000
    targetVersion = 0
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
    pendingCampaignMessages.clear()
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

  function flushCampaignMessages(convId: string) {
    const queued = pendingCampaignMessages.get(convId)
    if (!queued?.length) return
    const campaign = useCampaignStore()
    const chat = useChatStore()
    const remaining: DisplayMessage[] = []
    for (const message of queued) {
      if (
        campaign.snapshot?.campaign &&
        campaign.snapshot.campaign.revision > (message.campaign_revision ?? 0)
      ) {
        continue
      }
      if (campaign.matches(message.campaign_revision, message.campaign_state_hash)) {
        chat.applyServerMessage(message)
      } else {
        remaining.push(message)
      }
    }
    if (remaining.length) pendingCampaignMessages.set(convId, remaining)
    else pendingCampaignMessages.delete(convId)
  }

  function applyCampaignMessage(convId: string, message: DisplayMessage) {
    const conv = useConversationStore()
    const campaign = useCampaignStore()
    const chat = useChatStore()
    if (
      message.campaign_revision === undefined ||
      !conv.current?.campaign_id ||
      conv.current.id !== convId
    ) {
      chat.applyServerMessage(message)
      return
    }
    const current = campaign.snapshot?.campaign
    if (current && current.revision > message.campaign_revision) {
      // The narrative belongs to an older revision; showing it beside the new
      // sidebar would be the exact mismatch this guard exists to prevent.
      return
    }
    if (campaign.matches(message.campaign_revision, message.campaign_state_hash)) {
      chat.applyServerMessage(message)
      flushCampaignMessages(convId)
      return
    }
    const queued = pendingCampaignMessages.get(convId) || []
    queued.push(message)
    pendingCampaignMessages.set(convId, queued)
    void campaign
      .load(conv.current.campaign_id)
      .then(() => flushCampaignMessages(convId))
      .catch(() => {})
  }

  function handleEvent(ev: IncomingSocketEvent) {
    // 后端新增而前端尚未定义的事件类型：直接忽略，防止炸掉运行时；
    // 过滤后 ev 收窄为 SocketEvent，switch 各分支按 type 字面量继续收窄成员。
    if (!isKnownSocketEvent(ev)) return

    if (typeof ev.conversation_id === 'string' && ev.conversation_id !== currentConvId.value) return
    const eventAttempt = typeof ev.attempt_id === 'string' ? ev.attempt_id : ''
    if (eventAttempt && ev.type.startsWith('agent.') && terminalAttempts.has(eventAttempt)) return
    const contextStore = useContextStore()
    if (ev.type === 'activity_updated') {
      const previous = contextStore.activity
      if (ev.snapshot) {
        if (
          ev.snapshot.activity_version !== ev.activity_version ||
          ev.snapshot.branch_epoch !== (ev.branch_epoch ?? 0)
        ) {
          if (currentConvId.value)
            void resyncConversation(currentConvId.value).catch(() => scheduleFallback())
          return
        }
        if (contextStore.applyActivitySnapshot(ev.snapshot)) {
          const next = contextStore.activity
          if (
            previous.memory !== next.memory ||
            previous.memory_job?.id !== next.memory_job?.id ||
            previous.memory_job?.execution_count !== next.memory_job?.execution_count
          ) {
            memoryProgress = Date.now()
            taskDelay = 75000
            if (next.memory === 'idle') contextStore.invalidateInspector()
          }
          if (
            (previous.turn_status !== next.turn_status && next.turn_status !== 'pending') ||
            previous.branch_epoch !== next.branch_epoch
          )
            contextStore.invalidateInspector()
          if (
            (next.is_processing === false && useChatStore().isGenerating) ||
            previous.branch_epoch !== next.branch_epoch ||
            (previous.turn_id !== next.turn_id && activeAgentAttempt !== next.attempt_id) ||
            previous.conversation_status !== next.conversation_status
          ) {
            if (next.is_processing === false) useChatStore().stopGenerating()
            if (currentConvId.value)
              void resyncConversation(currentConvId.value, true, 0, false).catch(() =>
                scheduleFallback(),
              )
          }
          if (next.attempt_id && next.turn_status !== 'pending')
            terminalAttempts.add(next.attempt_id)
          if (terminalAttempts.size > 256)
            terminalAttempts.delete(terminalAttempts.values().next().value!)
        }
      } else if (ev.refresh_required && typeof ev.activity_version === 'number') {
        if (ev.activity_version > (previous.activity_version ?? -1) && currentConvId.value) {
          void resyncConversation(currentConvId.value, false, ev.activity_version).catch(() =>
            scheduleFallback(),
          )
        }
      } else if (currentConvId.value) {
        // Legacy memory notifications are invalidations, never unversioned writes.
        memoryProgress = Date.now()
        void resyncConversation(currentConvId.value, false).catch(() => scheduleFallback())
      }
      armTaskWatchdog()
      return
    }
    if (ev.conversation_id && ev.conversation_id !== currentConvId.value) return

    const sequenced = ev.type.startsWith('agent.') || ev.type === 'guidance.updated'
    if (sequenced && !acceptAgentEvent(ev)) return
    if (!sequenced && (ev.operation_id || ev.attempt_id) && !acceptScopedEvent(ev)) return
    if (ev.type.startsWith('agent.')) {
      narrativeProgress = Date.now()
      taskDelay = 75000
    }

    const chat = useChatStore()
    const state = useStateStore()
    const context = useContextStore()
    const conv = useConversationStore()
    const rewards = useRewardStore()
    const campaign = useCampaignStore()
    const runtime = useRuntimeStore()
    const guidance = useGuidanceStore()
    if (eventAttempt && ev.type.startsWith('agent.')) context.notePhaseAttempt(eventAttempt)

    switch (ev.type) {
      case 'agent.turn.started':
        guidance.reset()
        lastError.value = null
        context.setNarrative('preparing')
        if (ev.payload?.message_id) chat.beginStream(ev.payload.message_id)
        break
      case 'agent.step.started':
        context.setNarrative('model', { step_no: ev.step_no })
        break
      case 'agent.tool.started':
        context.setNarrative('tool', {
          step_no: ev.step_no,
          tool_category: ev.payload?.category,
        })
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
        context.invalidateInspector()
        finishActiveOperation()
        break
      case 'agent.turn.partial':
        if (currentConvId.value) conv.loadEndingState(currentConvId.value).catch(() => {})
        guidance.reset()
        context.setNarrative('idle')
        // partial 回合已经提交了可信事实，重新生成要消耗改写额度；
        // 这里不写死数量，交给平台能力和随后刷新的 inspector 决定。
        if (ev.payload?.message_id) chat.abortStream(ev.payload.message_id)
        lastError.value = ev.payload?.message || '动作已执行，但叙事未完成。请继续叙事。'
        rewards.load().catch(() => {})
        context.invalidateInspector()
        finishActiveOperation()
        break
      case 'agent.turn.failed':
      case 'agent.turn.cancelled':
        if (currentConvId.value) conv.loadEndingState(currentConvId.value).catch(() => {})
        guidance.reset()
        context.setNarrative('idle')
        if (ev.payload?.message_id) chat.abortStream(ev.payload.message_id)
        // 失败或取消后付费的重新生成会先退款，再推送终止事件；这里刷新余额。
        rewards.load().catch(() => {})
        context.invalidateInspector()
        finishActiveOperation()
        break
      case 'agent.queue.updated':
        queuedCount.value = Math.max(0, ev.payload?.count || 0)
        break
      case 'message_created':
        if (ev.message?.type === 'user') guidance.reset()
        if (ev.message && currentConvId.value) applyCampaignMessage(currentConvId.value, ev.message)
        break
      case 'guidance.updated':
        guidance.set(ev.payload.guidance)
        break
      case 'message_updated':
        if (ev.message && currentConvId.value) applyCampaignMessage(currentConvId.value, ev.message)
        break
      case 'branch.replaced':
        guidance.reset()
        chat.replaceBranch(ev.payload?.invalidated_message_ids ?? [], ev.message)
        rewards.load().catch(() => {})
        break
      case 'generation.replayed':
        if (currentConvId.value) {
          context.invalidateInspector()
          void resyncConversation(currentConvId.value, true).catch(() => scheduleFallback())
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
      case 'conversation_ended':
        guidance.reset()
        // Legacy lifecycle events have no revision. Verify them instead of
        // allowing a late end event to overwrite a newer restore snapshot.
        if (currentConvId.value)
          void resyncConversation(currentConvId.value).catch(() => scheduleFallback())
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
    if (
      [
        'agent.turn.completed',
        'agent.turn.partial',
        'agent.turn.failed',
        'agent.turn.cancelled',
      ].includes(ev.type)
    ) {
      if (eventAttempt) terminalAttempts.add(eventAttempt)
      if (context.activity.activity_version === undefined && currentConvId.value) {
        void resyncConversation(currentConvId.value, false).catch(() => scheduleFallback())
      }
    }
    armTaskWatchdog()
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
      void resyncConversation(currentConvId.value).catch(() => scheduleFallback())
      return false
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
      // A committed message may arrive after the REST message read and terminal
      // snapshot. Merge it only for the authoritative current attempt, without
      // re-locking the finished operation. Other facts still require recovery.
      if (finishedOperations.has(operationId)) {
        return (
          (ev.type === 'message_created' || ev.type === 'message_updated') &&
          !!attemptId &&
          attemptId === useContextStore().activity.attempt_id
        )
      }
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
