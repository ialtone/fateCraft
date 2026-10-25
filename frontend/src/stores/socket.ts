import { defineStore } from 'pinia'
import { computed, ref, shallowRef, watch } from 'vue'
import type { DisplayMessage, IncomingSocketEvent } from '@/types'
import { isKnownSocketEvent } from '@/types'
import { isRetryableReadError } from '@/utils/recovery'
import { conversationsApi } from '@/api'
import { useChatStore } from './chat'
import { useStateStore } from './state'
import { useContextStore } from './context'
import { useConversationStore } from './conversations'
import { useCampaignStore } from './campaign'
import { useGuidanceStore } from './guidance'
import { useConversationResources } from './conversationResources'

// 自动重连参数：首次连接不计入预算，失败后最多再建立 5 个实例。
const MAX_RECONNECT_ATTEMPTS = 5
const RECONNECT_DELAYS = [1000, 2000, 4000, 8000, 16000]
// 连接和订阅就绪都必须有明确超时，避免握手或注册永久挂起。
const HANDSHAKE_TIMEOUT = 10000
const READY_TIMEOUT = 5000
// ready 后恢复成功且连续健康 30 秒，才归还下一故障周期的重连预算。
const STABLE_WINDOW = 30000
// ready 后的业务恢复最多 5 轮（含首次），耗尽后停止自动恢复。
const MAX_RECOVERY_ATTEMPTS = 5
// 带 operation 标识但只用于展示或重新加载的事件，不参与 operation 锁定。
const NON_LOCKING_SCOPED_EVENTS = new Set(['notice', 'error', 'generation.replayed'])
// 终止事件是主要的清锁信号；即使被 terminalAttempts 去重，也必须执行幂等清理。
const TERMINAL_AGENT_EVENTS = new Set([
  'agent.turn.completed',
  'agent.turn.partial',
  'agent.turn.failed',
  'agent.turn.cancelled',
])

export type SocketConnectionState =
  'connecting' | 'ready' | 'retry_wait' | 'reconnecting' | 'exhausted' | 'stopped' | 'access_denied'

export const useSocketStore = defineStore('socket', () => {
  const ws = shallowRef<WebSocket | null>(null)
  const connected = ref(false)
  // 断线后正在尝试自动重连
  const reconnecting = ref(false)
  const lastError = ref<string | null>(null)
  const queuedCount = ref(0)
  const currentConvId = ref<string | null>(null)
  const connectionState = ref<SocketConnectionState>('stopped')
  const reconnectAttempt = ref(0)
  const connectionError = ref<string | null>(null)
  const maxReconnectAttempts = MAX_RECONNECT_ATTEMPTS
  // 连接错误与生成错误分开：clearError / 新回合不能清掉“请刷新页面”的提示。
  const writeBlocked = computed(
    () => connectionState.value === 'exhausted' || connectionState.value === 'access_denied',
  )
  const terminal = computed(
    () => connectionState.value === 'exhausted' || connectionState.value === 'access_denied',
  )
  const connectionNotice = computed(() => {
    if (connectionState.value === 'exhausted')
      return connectionError.value || '连接已中断，自动重连 5 次仍未成功，请刷新页面后重试。'
    if (connectionState.value === 'access_denied') return connectionError.value
    if (connectionState.value === 'retry_wait' || connectionState.value === 'reconnecting')
      return `连接已断开，正在重新连接（${Math.max(1, reconnectAttempt.value)}/${MAX_RECONNECT_ATTEMPTS}）…`
    if (connectionState.value === 'connecting' && !connected.value) return '正在连接对话…'
    return null
  })
  // 重连尝试次数（连续健康 30 秒后才重置）
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
  let lifecycle = 0
  let ready = false
  let recovering = false
  let recoveryFlight: Promise<void> | null = null
  let businessPending = false
  let activityPending = false
  let buffered: IncomingSocketEvent[] = []
  let bufferedBytes = 0
  let readyTimer: ReturnType<typeof setTimeout> | null = null
  let handshakeTimer: ReturnType<typeof setTimeout> | null = null
  let stableTimer: ReturnType<typeof setTimeout> | null = null
  let retryTimer: ReturnType<typeof setTimeout> | null = null
  let taskTimer: ReturnType<typeof setTimeout> | null = null
  let recoveryTimer: ReturnType<typeof setTimeout> | null = null
  let stopTaskWatch: (() => void) | null = null
  let retryDelay = 5000
  // 记忆任务本身可以跑很久；叙事租约的目标自愈窗口更短，两者不能共用一个
  // 退避值，否则一次丢终止事件会让横幅挂到 75 秒。
  let taskDelay = 75000
  let narrativeDelay = 15000
  let recoveryFailures = 0
  let locateAttempted = false
  let targetVersion = 0
  let narrativeProgress = 0
  let memoryProgress = 0
  const terminalAttempts = new Set<string>()

  const foreground = () => typeof document === 'undefined' || document.visibilityState !== 'hidden'
  const online = () => typeof navigator === 'undefined' || navigator.onLine !== false
  const isTerminal = () =>
    connectionState.value === 'exhausted' || connectionState.value === 'access_denied'

  function clearConnectionTimers() {
    if (reconnectTimer) clearTimeout(reconnectTimer)
    if (handshakeTimer) clearTimeout(handshakeTimer)
    if (readyTimer) clearTimeout(readyTimer)
    if (stableTimer) clearTimeout(stableTimer)
    reconnectTimer = handshakeTimer = readyTimer = stableTimer = null
  }

  function stopRecoveryTimers() {
    if (retryTimer) clearTimeout(retryTimer)
    if (taskTimer) clearTimeout(taskTimer)
    retryTimer = taskTimer = null
  }

  function setState(state: SocketConnectionState) {
    connectionState.value = state
    reconnecting.value = state === 'retry_wait' || state === 'reconnecting'
  }

  function stopConnection(state: 'exhausted' | 'access_denied', message: string | null) {
    session++
    useContextStore().invalidateRequests()
    useContextStore().setRecoverySuspended(true)
    useConversationResources().suspend()
    clearConnectionTimers()
    stopRecoveryTimers()
    if (recoveryTimer) clearTimeout(recoveryTimer)
    recoveryTimer = null
    ready = false
    recovering = false
    recoveryFlight = null
    businessPending = false
    activityPending = false
    buffered = []
    bufferedBytes = 0
    stopTaskWatch?.()
    stopTaskWatch = null
    if (typeof window !== 'undefined') {
      window.removeEventListener('online', resume)
      window.removeEventListener('offline', resume)
      document.removeEventListener('visibilitychange', resume)
    }
    if (ws.value) {
      ws.value.onclose = null
      ws.value.onerror = null
      ws.value.close()
      ws.value = null
    }
    connected.value = false
    reconnecting.value = false
    connectionError.value = message
    setState(state)
  }

  function scheduleFallback(immediate = false) {
    if (!currentConvId.value || retryTimer || isTerminal() || !foreground() || !online()) return
    const epoch = session
    const wait = immediate ? 0 : retryDelay * (0.9 + Math.random() * 0.2)
    retryTimer = setTimeout(() => {
      retryTimer = null
      if (epoch !== session || !currentConvId.value || isTerminal()) return
      void resyncConversation(currentConvId.value, true)
        .catch(() => {})
        .finally(() => {
          if (epoch !== session || isTerminal()) return
          retryDelay = retryDelay === 5000 ? 10000 : 30000
          if (!ready || businessPending || useContextStore().activityStale) scheduleFallback()
        })
    }, wait)
  }

  function armTaskWatchdog() {
    if (taskTimer) clearTimeout(taskTimer)
    taskTimer = null
    if (!ready || !currentConvId.value || isTerminal() || !foreground() || !online()) return
    const activity = useContextStore().activity
    const generating = activity.is_processing || useChatStore().isGenerating
    const memoryBusy = activity.memory === 'queued' || activity.memory === 'running'
    if (!generating && !memoryBusy) return
    const deadline = Math.min(
      generating ? narrativeProgress + narrativeDelay : Infinity,
      memoryBusy ? memoryProgress + taskDelay : Infinity,
    )
    const epoch = session
    taskTimer = setTimeout(
      () => {
        taskTimer = null
        if (epoch !== session || !currentConvId.value || isTerminal()) return
        const current = useContextStore().activity
        const narrativeDue =
          (current.is_processing || useChatStore().isGenerating) &&
          Date.now() - narrativeProgress >= narrativeDelay
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
            if (epoch === session && !isTerminal()) scheduleFallback()
          })
          .finally(() => {
            if (epoch !== session || isTerminal()) return
            narrativeDelay = Math.min(narrativeDelay * 2, 300000)
            taskDelay = Math.min(taskDelay * 2, 300000)
            narrativeProgress = memoryProgress = Date.now()
            armTaskWatchdog()
          })
      },
      Math.max(0, deadline - Date.now()),
    )
  }

  function resume() {
    if (isTerminal()) return
    stopRecoveryTimers()
    if (!currentConvId.value || !foreground() || !online()) {
      if (reconnectTimer) clearTimeout(reconnectTimer)
      reconnectTimer = null
      return
    }
    retryDelay = 5000
    if (ready) {
      useContextStore().invalidateInspector()
      void resyncConversation(currentConvId.value, true).catch(() => scheduleFallback())
    } else if (!ws.value) {
      scheduleFallback(true)
      if (!reconnectTimer) scheduleReconnect()
    }
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

  // 首连失败时只做一次 REST 定位，用于区分“网络/订阅故障”和“401/403/404”。
  // 成功定位的数据不视为订阅后的权威快照；ready 后仍由统一恢复重新读取。
  function locateConversation(convId: string) {
    if (locateAttempted || currentConvId.value !== convId) return
    locateAttempted = true
    const identity = lifecycle
    void conversationsApi.get(convId).catch((error) => {
      if (identity !== lifecycle || currentConvId.value !== convId || isTerminal()) return
      if (!isRetryableReadError(error))
        stopConnection('access_denied', '无法继续访问此对话，请重新登录或返回对话列表')
    })
  }

  // 切换对话时关闭旧连接并打开新连接（doc 05）。
  function connect(convId: string) {
    // 同一对话的重复 connect 必须幂等，不能绕过 exhausted 预算。
    if (
      currentConvId.value === convId &&
      ['connecting', 'ready', 'retry_wait', 'reconnecting', 'exhausted', 'access_denied'].includes(
        connectionState.value,
      )
    )
      return
    disconnect()
    currentConvId.value = convId
    connectionError.value = null
    reconnectAttempts = 0
    recoveryFailures = 0
    locateAttempted = false
    reconnectAttempt.value = 0
    setState('connecting')
    useContextStore().enter(convId)
    useContextStore().setRecoverySuspended(false)
    useConversationResources().enter(convId)
    useConversationResources().setFailureHandler((error) => {
      if (currentConvId.value !== convId || isTerminal()) return
      if (!isRetryableReadError(error)) {
        stopConnection('access_denied', '无法继续访问此对话，请重新登录或返回对话列表')
        return
      }
      businessPending = true
      if (stableTimer) clearTimeout(stableTimer)
      stableTimer = null
      scheduleFallback()
    })
    narrativeProgress = memoryProgress = Date.now()
    narrativeDelay = 15000
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
        useCampaignStore().snapshot,
      ],
      () => {
        armTaskWatchdog()
        if (currentConvId.value === convId && !isTerminal()) flushCampaignMessages(convId)
      },
    )
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
    if (currentConvId.value !== convId || isTerminal()) return Promise.resolve()
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
          const businessResources = [
            'conversation',
            'messages',
            'state',
            'guidance',
            'endings',
            'runtime',
            'rewards',
          ] as const
          const tasks: Promise<unknown>[] = []
          if (activityRead) tasks.push(context.loadActivity(convId, targetVersion))
          if (business) {
            const resources = useConversationResources()
            for (const resource of businessResources) tasks.push(resources.load(resource))
          }
          const results = await Promise.allSettled(tasks)
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
            const resources = useConversationResources()
            if (context.inspectorVisible && context.inspectorDirty) {
              void resources.load('inspector')
            }
            if (conv.current?.campaign_id) {
              await resources.load('campaign')
              if (epoch !== session) return
              const latest = [...chat.messages]
                .reverse()
                .find((m) => m.type === 'assistant' && m.campaign_revision !== undefined)
              if (
                latest &&
                !campaign.matches(latest.campaign_revision, latest.campaign_state_hash)
              ) {
                await resources.load('campaign')
                if (epoch !== session) return
              }
            }
            if (
              snapshot.attempt_id &&
              snapshot.turn_status !== 'pending' &&
              snapshot.is_processing === false
            )
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
        if (epoch === session && !isTerminal()) {
          if (stableTimer) clearTimeout(stableTimer)
          stableTimer = null
          businessPending = true
          if (!isRetryableReadError(error)) {
            stopConnection('access_denied', '无法继续访问此对话，请重新登录或返回对话列表')
          } else if (ready) {
            recoveryFailures++
            if (recoveryFailures >= MAX_RECOVERY_ATTEMPTS)
              stopConnection('exhausted', '连接恢复失败，自动重试已达到上限，请刷新页面后重试。')
          }
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
        else if (!useContextStore().activityStale) armStableReset()
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
    if (handshakeTimer) clearTimeout(handshakeTimer)
    if (readyTimer) clearTimeout(readyTimer)
    handshakeTimer = readyTimer = null
    const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
    const socket = new WebSocket(`${proto}://${window.location.host}/ws/conversations/${convId}`)
    ws.value = socket
    let settled = false

    const fail = (message?: string) => {
      if (settled || ws.value !== socket || currentConvId.value !== convId) return
      settled = true
      const wasReady = ready
      if (message) lastError.value = message
      if (handshakeTimer) clearTimeout(handshakeTimer)
      if (readyTimer) clearTimeout(readyTimer)
      if (stableTimer) clearTimeout(stableTimer)
      handshakeTimer = readyTimer = stableTimer = null
      if (isTerminal()) return
      session++
      useContextStore().invalidateRequests()
      useConversationResources().resetInFlight()
      recoveryFlight = null
      recovering = false
      buffered = []
      bufferedBytes = 0
      if (recoveryTimer) clearTimeout(recoveryTimer)
      recoveryTimer = null
      ready = false
      connected.value = false
      stopRecoveryTimers()
      // 先关闭旧实例并清空引用，避免 onclose 再结算一次。
      socket.onclose = null
      socket.onerror = null
      socket.close()
      if (ws.value === socket) ws.value = null
      if (!wasReady) locateConversation(convId)
      if (reconnectAttempts >= MAX_RECONNECT_ATTEMPTS) {
        stopConnection('exhausted', '连接已中断，自动重连 5 次仍未成功，请刷新页面后重试。')
        return
      }
      scheduleReconnect()
      scheduleFallback()
    }

    handshakeTimer = setTimeout(() => {
      if (ws.value !== socket || currentConvId.value !== convId || ready) return
      fail('WebSocket 连接超时')
    }, HANDSHAKE_TIMEOUT)

    socket.onopen = () => {
      if (ws.value !== socket || currentConvId.value !== convId) return
      if (handshakeTimer) clearTimeout(handshakeTimer)
      handshakeTimer = null
      // 连接已恢复：清掉断线期间 onerror 留下的错误提示，避免横幅常驻不散
      lastError.value = null
      // Subscription readiness is required. A missing acknowledgement enters
      // disconnected recovery instead of treating an unregistered socket as live.
      readyTimer = setTimeout(() => {
        readyTimer = null
        if (ws.value !== socket || currentConvId.value !== convId || ready) return
        fail('WebSocket 订阅确认超时')
      }, READY_TIMEOUT)
    }
    socket.onclose = () => {
      fail()
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
          connectionError.value = null
          setState('ready')
          if (handshakeTimer) clearTimeout(handshakeTimer)
          if (readyTimer) clearTimeout(readyTimer)
          handshakeTimer = readyTimer = null
          stopRecoveryTimers()
          void resyncConversation(convId).catch(() => {
            if (!isTerminal()) scheduleFallback()
          })
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

  // 第 1～5 次重连分别等待 1、2、4、8、16 秒；在真正建立连接时消耗一次预算。
  function scheduleReconnect() {
    if (!currentConvId.value || reconnectTimer || isTerminal()) return
    if (reconnectAttempts >= MAX_RECONNECT_ATTEMPTS) {
      stopConnection('exhausted', '连接已中断，自动重连 5 次仍未成功，请刷新页面后重试。')
      return
    }
    const next = reconnectAttempts + 1
    reconnectAttempt.value = next
    setState('retry_wait')
    // 离线或页面隐藏时暂停安排新连接，也不消耗次数；恢复后继续剩余预算。
    if (!foreground() || !online()) return
    const delay = RECONNECT_DELAYS[next - 1]
    reconnectTimer = setTimeout(() => {
      reconnectTimer = null
      if (!currentConvId.value || isTerminal() || !foreground() || !online() || ws.value) return
      reconnectAttempts = next
      reconnectAttempt.value = next
      setState('reconnecting')
      openSocket(currentConvId.value)
    }, delay)
  }

  function armStableReset() {
    if (stableTimer) return
    if (!ready || isTerminal()) return
    const epoch = session
    stableTimer = setTimeout(() => {
      stableTimer = null
      if (epoch !== session || !ready || isTerminal()) return
      if (
        recovering ||
        recoveryFlight ||
        businessPending ||
        activityPending ||
        useContextStore().activityStale
      ) {
        armStableReset()
        return
      }
      reconnectAttempts = 0
      reconnectAttempt.value = 0
      recoveryFailures = 0
      connectionError.value = null
      setState('ready')
    }, STABLE_WINDOW)
  }

  function disconnect() {
    lifecycle++
    session++
    useContextStore().invalidateRequests()
    useContextStore().setRecoverySuspended(false)
    ready = false
    recovering = false
    recoveryFlight = null
    businessPending = false
    activityPending = false
    buffered = []
    bufferedBytes = 0
    terminalAttempts.clear()
    stopRecoveryTimers()
    clearConnectionTimers()
    if (recoveryTimer) clearTimeout(recoveryTimer)
    recoveryTimer = null
    stopTaskWatch?.()
    stopTaskWatch = null
    if (typeof window !== 'undefined') {
      window.removeEventListener('online', resume)
      window.removeEventListener('offline', resume)
      document.removeEventListener('visibilitychange', resume)
    }
    retryDelay = 5000
    taskDelay = 75000
    narrativeDelay = 15000
    recoveryFailures = 0
    locateAttempted = false
    targetVersion = 0
    // 主动断开：取消重连并清空状态
    reconnecting.value = false
    reconnectAttempts = 0
    reconnectAttempt.value = 0
    connectionError.value = null
    setState('stopped')
    useConversationResources().reset()
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
      ws.value.onerror = null
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
    const epoch = session
    const resources = useConversationResources()
    resources.invalidate('campaign')
    void resources
      .load('campaign')
      .then(() => {
        if (epoch === session && currentConvId.value === convId && !isTerminal())
          flushCampaignMessages(convId)
      })
      .catch(() => {})
  }

  function handleEvent(ev: IncomingSocketEvent) {
    // 后端新增而前端尚未定义的事件类型：直接忽略，防止炸掉运行时；
    // 过滤后 ev 收窄为 SocketEvent，switch 各分支按 type 字面量继续收窄成员。
    if (!isKnownSocketEvent(ev)) return

    if (typeof ev.conversation_id === 'string' && ev.conversation_id !== currentConvId.value) return
    const eventAttempt = typeof ev.attempt_id === 'string' ? ev.attempt_id : ''
    if (eventAttempt && ev.type.startsWith('agent.') && terminalAttempts.has(eventAttempt)) {
      // 去重只跳过重复的消息/通知处理；生成锁、操作锁和资源失效必须幂等执行。
      if (TERMINAL_AGENT_EVENTS.has(ev.type)) {
        useChatStore().stopGenerating()
        useContextStore().setNarrative('idle')
        finishActiveOperation()
        useConversationResources().invalidateMany(
          ['conversation', 'state', 'campaign', 'endings', 'rewards', 'actions'],
          true,
        )
      }
      return
    }
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
          // 活动快照仍持有租约时不能把 attempt 标记为终态；否则真正的终止
          // 事件会被去重，生成锁只能等 watchdog。只有租约已释放才登记。
          if (next.attempt_id && next.turn_status !== 'pending' && next.is_processing === false)
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
      narrativeDelay = 15000
    }

    const chat = useChatStore()
    const state = useStateStore()
    const context = useContextStore()
    const conv = useConversationStore()
    const campaign = useCampaignStore()
    const guidance = useGuidanceStore()
    const resources = useConversationResources()
    if (eventAttempt && ev.type.startsWith('agent.')) context.notePhaseAttempt(eventAttempt)

    switch (ev.type) {
      case 'agent.turn.started':
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
        context.setNarrative('idle')
        if (ev.payload?.message_id) chat.endStream(ev.payload.message_id)
        // 终止事件必须无条件清锁；message_id 缺失时 endStream 不会执行。
        chat.stopGenerating()
        // 回合内工具可能已改写玩家状态/存档；终止事件统一作为状态边界刷新。
        resources.invalidateMany([
          'conversation',
          'state',
          'campaign',
          'endings',
          'rewards',
          'actions',
        ])
        context.invalidateInspector()
        finishActiveOperation()
        break
      case 'agent.turn.partial':
        context.setNarrative('idle')
        // partial 回合已经提交了可信事实，重新生成要消耗改写额度；
        // 这里不写死数量，交给平台能力和随后刷新的 inspector 决定。
        chat.abortStream(ev.payload?.message_id)
        lastError.value = ev.payload?.message || '动作已执行，但叙事未完成。请继续叙事。'
        // 回合内工具可能已改写玩家状态/存档；终止事件统一作为状态边界刷新。
        resources.invalidateMany([
          'conversation',
          'state',
          'campaign',
          'endings',
          'rewards',
          'actions',
        ])
        context.invalidateInspector()
        finishActiveOperation()
        break
      case 'agent.turn.failed':
      case 'agent.turn.cancelled':
        context.setNarrative('idle')
        chat.abortStream(ev.payload?.message_id)
        // 失败或取消后付费的重新生成会先退款，再推送终止事件；这里刷新余额。
        // 回合内工具可能已改写玩家状态/存档；终止事件统一作为状态边界刷新。
        resources.invalidateMany([
          'conversation',
          'state',
          'campaign',
          'endings',
          'rewards',
          'actions',
        ])
        context.invalidateInspector()
        finishActiveOperation()
        break
      case 'agent.queue.updated':
        queuedCount.value = Math.max(0, ev.payload?.count || 0)
        break
      case 'message_created':
        if (ev.message && currentConvId.value) applyCampaignMessage(currentConvId.value, ev.message)
        break
      case 'guidance.updated': {
        const incoming = ev.payload.guidance
        const currentRevision = campaign.snapshot?.campaign?.revision
        if (
          incoming?.campaign_revision &&
          currentRevision !== undefined &&
          incoming.campaign_revision < currentRevision
        )
          break
        guidance.set(incoming)
        break
      }
      case 'message_updated':
        if (ev.message && currentConvId.value) applyCampaignMessage(currentConvId.value, ev.message)
        break
      case 'branch.replaced':
        guidance.reset()
        chat.replaceBranch(ev.payload?.invalidated_message_ids ?? [], ev.message)
        resources.invalidate('rewards')
        break
      case 'generation.replayed':
        if (currentConvId.value) {
          context.invalidateInspector()
          void resyncConversation(currentConvId.value, true).catch(() => scheduleFallback())
        }
        break
      case 'module.action_committed':
        // 局中只记录阶段进度，不改变账号余额；到账与结局由
        // challenge_completed 负责刷新，避免每条动作都拉取奖励余额。
        resources.invalidateMany(
          ['conversation', 'messages', 'state', 'guidance', 'endings', 'runtime', 'campaign'],
          true,
        )
        break
      case 'state_update':
        if (ev.state) state.set(ev.state)
        // 服务端提交后读取状态失败时只下发 refresh_required，由客户端重新拉取。
        else if (ev.refresh_required && currentConvId.value) resources.invalidate('state', true)
        // Guidance visibility is projected by the server against the latest
        // player state. Refresh the optional runtime/actions presentation at
        // this explicit state boundary, coalesced by the resource scheduler.
        if (currentConvId.value && !chat.isGenerating) resources.invalidate('guidance')
        if (currentConvId.value) {
          resources.invalidate('runtime')
          resources.invalidate('actions')
        }
        break
      case 'campaign_updated':
        if (ev.campaign) {
          campaign.applyCampaign(ev.campaign)
          resources.invalidate('campaign')
        }
        if (currentConvId.value && !chat.isGenerating) resources.invalidate('guidance')
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
        if (currentConvId.value) resources.invalidate('endings')
        break
      case 'challenge_completed':
        guidance.reset()
        if (ev.completion) conv.consumeCompletion(ev.completion)
        resources.invalidate('rewards')
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
        if (currentConvId.value) resources.invalidate('guidance')
        lastError.value = ev.error || '生成出错'
        // 本轮生成已经终止：复位生成状态并闭合未完成的流式消息
        chat.stopGenerating()
        context.setNarrative('idle')
        chat.removeStaleTempUsers()
        resources.invalidate('rewards')
        if (currentConvId.value) {
          resources.invalidate('messages')
          resources.invalidate('actions')
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
    // Message/delta events can be discarded after event-id de-duplication,
    // but terminal events must still reach the switch below. A reconnect can
    // deliver a duplicate terminal frame after the first cleanup; clearing
    // the local generation lock and finishing the operation are idempotent.
    if (eventId && seenAgentEvents.has(eventId)) {
      if (!TERMINAL_AGENT_EVENTS.has(ev.type)) return false
      const duplicateOperation = typeof ev.operation_id === 'string' ? ev.operation_id : ''
      const duplicateAttempt = typeof ev.attempt_id === 'string' ? ev.attempt_id : ''
      // A late duplicate from a finished operation must not clear a newer
      // generation. Allow replay only while it still belongs to the active
      // operation/attempt (or when the envelope omitted those optional IDs).
      if (duplicateOperation && activeOperationID && duplicateOperation !== activeOperationID)
        return false
      if (duplicateAttempt && activeAgentAttempt && duplicateAttempt !== activeAgentAttempt)
        return false
      return true
    }
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
    // A terminal frame is safe to replay for the current attempt even when
    // its sequence was already observed. This preserves the mandatory
    // idempotent cleanup path without replaying ordinary deltas.
    if (TERMINAL_AGENT_EVENTS.has(ev.type)) return true
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
    if (writeBlocked.value) return false
    if (ws.value && connected.value) {
      ws.value.send(JSON.stringify({ type: 'user_input', content, request_id: requestId }))
      return true
    }
    return false
  }

  function cancel() {
    if (writeBlocked.value) return
    if (ws.value && connected.value) {
      ws.value.send(JSON.stringify({ type: 'cancel_generation' }))
    }
  }

  function steer(content: string, requestId?: string) {
    if (writeBlocked.value) return false
    if (ws.value && connected.value && content.trim()) {
      ws.value.send(JSON.stringify({ type: 'steer_generation', content, request_id: requestId }))
      return true
    }
    return false
  }

  function followUp(content: string, requestId?: string) {
    if (writeBlocked.value) return false
    if (ws.value && connected.value && content.trim()) {
      ws.value.send(JSON.stringify({ type: 'follow_up', content, request_id: requestId }))
      return true
    }
    return false
  }

  function clearQueue() {
    if (writeBlocked.value) return false
    if (ws.value && connected.value) {
      ws.value.send(JSON.stringify({ type: 'clear_generation_queue' }))
      return true
    }
    return false
  }

  function clearError() {
    lastError.value = null
  }

  // 可见恢复入口：生成中横幅超过阈值时由玩家主动触发，不要求整页刷新。
  function resyncNow() {
    if (!currentConvId.value || isTerminal()) return
    useContextStore().invalidateInspector()
    void resyncConversation(currentConvId.value, true).catch(() => scheduleFallback())
  }

  function refreshPage() {
    if (typeof window !== 'undefined') window.location.reload()
  }

  return {
    connected,
    reconnecting,
    lastError,
    queuedCount,
    currentConvId,
    connectionState,
    reconnectAttempt,
    maxReconnectAttempts,
    connectionError,
    connectionNotice,
    writeBlocked,
    terminal,
    connect,
    disconnect,
    sendUserInput,
    cancel,
    steer,
    followUp,
    clearQueue,
    clearError,
    resyncNow,
    refreshPage,
  }
})
