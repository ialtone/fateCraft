import { defineStore } from 'pinia'
import { ref } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { useSettingsStore } from '@/stores/settings'
import { getLocalKey } from '@/llm/localKey'

// 服务端下发的 endpoint 白名单之外一律拒绝，不接受服务端下发 URL。
const RELAY_ENDPOINTS = ['/chat/completions', '/responses']

// DeepSeek 的 Responses 不在 /v1 下：用户存的 base_url 通常是 Chat 时代的
// https://api.deepseek.com/v1，直接拼接会得到 /v1/responses 而 404；
// deepseek 官方域名走 /responses 时去掉 /v1 后缀（与后端 EndpointURL 同规则）。
function relayRequestURL(baseUrl: string, endpoint: string): string {
  if (endpoint === '/responses') {
    try {
      const host = new URL(baseUrl).hostname.toLowerCase()
      if (host === 'deepseek.com' || host.endsWith('.deepseek.com')) {
        baseUrl = baseUrl.replace(/\/v1\/*$/, '')
      }
    } catch {
      // 解析失败时保持原样，由服务端返回可解释错误。
    }
  }
  return `${baseUrl}${endpoint}`
}
// chunk 合并阈值：32KiB（UTF-8 字节）或 50ms，先到先发。
const CHUNK_BYTES = 32 * 1024
const CHUNK_INTERVAL_MS = 50
// 服务端入站单帧上限 256KiB；此处按 §4.6 约定每帧 ≤ 64KiB，留足余量。
const MAX_FRAME_BYTES = 64 * 1024
// 背压：待发字节超过 1MiB 时暂停读取上游。
const BACKPRESSURE_BYTES = 1024 * 1024
// 重连退避与 socket.ts 一致：1s 起步，指数退避，上限 30s。
const RECONNECT_BASE_DELAY = 1000
const RECONNECT_MAX_DELAY = 30000

interface PendingFetch {
  controller: AbortController
  reader?: ReadableStreamDefaultReader<Uint8Array>
}

/**
 * 客户端直连 Relay：应用级用户 WebSocket（与会话页无关）。
 * 满足“已登录 && llm_route==='client'”时连接，否则断开。
 * 收到 relay.request 后用本设备 Key 直接 fetch 模型服务，把原始响应字节回传；
 * Key 只发往本地 base_url + 白名单 endpoint。
 */
export const useLlmRelayStore = defineStore('llmRelay', () => {
  const connected = ref(false)
  const inFlight = ref(0)

  let ws: WebSocket | null = null
  let wanted = false
  let reconnectAttempts = 0
  let reconnectTimer: ReturnType<typeof setTimeout> | null = null
  const pending = new Map<string, PendingFetch>()

  function shouldConnect(): boolean {
    const auth = useAuthStore()
    const settings = useSettingsStore()
    // 有效路由已由后端按“平台开关 × 用户保存值”算好，与接入模式无关：
    // platform_managed + 开关开启 + 用户选 client 同样连接。
    return auth.authenticated && settings.effectiveRoute === 'client'
  }

  function relayUrl(): string {
    const protocol = window.location.protocol === 'https:' ? 'wss' : 'ws'
    return `${protocol}://${window.location.host}/ws/llm-relay`
  }

  /** App.vue 监听登录态与设置变化后调用：按条件连接或断开。 */
  function ensure() {
    wanted = useAuthStore().authenticated
    if (wanted && shouldConnect()) connect()
    else disconnect()
  }

  /** 设置页保存后调用：路由可能已变化，立即重估。 */
  function refresh() {
    ensure()
  }

  function connect() {
    if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) return
    if (reconnectTimer) {
      clearTimeout(reconnectTimer)
      reconnectTimer = null
    }
    const socket = new WebSocket(relayUrl())
    ws = socket
    socket.onopen = () => {
      if (ws !== socket) return
      connected.value = true
      reconnectAttempts = 0
    }
    socket.onmessage = (event) => {
      if (ws !== socket) return
      handleMessage(event.data)
    }
    const onGone = () => {
      if (ws !== socket) return
      ws = null
      connected.value = false
      scheduleReconnect()
    }
    socket.onclose = onGone
    socket.onerror = onGone
  }

  function disconnect() {
    wanted = false
    if (reconnectTimer) {
      clearTimeout(reconnectTimer)
      reconnectTimer = null
    }
    // 进行中的 fetch 全部 abort，各 pump 观测到后自行收尾（发送已发不出，静默退出）。
    for (const task of pending.values()) {
      try {
        task.controller.abort()
      } catch {
        // 忽略。
      }
    }
    pending.clear()
    if (ws) {
      const socket = ws
      ws = null
      connected.value = false
      try {
        socket.close()
      } catch {
        // 忽略。
      }
    } else {
      connected.value = false
    }
  }

  function scheduleReconnect() {
    if (!wanted || ws) return
    if (!shouldConnect()) return
    const delay = Math.min(RECONNECT_BASE_DELAY * 2 ** reconnectAttempts, RECONNECT_MAX_DELAY)
    reconnectAttempts += 1
    reconnectTimer = setTimeout(() => {
      reconnectTimer = null
      if (wanted && shouldConnect()) connect()
    }, delay)
  }

  function send(frame: unknown): boolean {
    if (!ws || ws.readyState !== WebSocket.OPEN) return false
    try {
      ws.send(JSON.stringify(frame))
      return true
    } catch {
      return false
    }
  }

  function handleMessage(raw: unknown) {
    if (typeof raw !== 'string') return
    let frame: any
    try {
      frame = JSON.parse(raw)
    } catch {
      return
    }
    if (!frame || typeof frame.type !== 'string') return
    if (frame.type === 'relay.request') void executeRequest(frame)
    else if (frame.type === 'relay.cancel') cancelRequest(frame.relay_id)
  }

  // 取消只 abort，终结帧由 pump 统一发送（保证恰好一帧）。
  function cancelRequest(relayId: unknown) {
    if (typeof relayId !== 'string') return
    const task = pending.get(relayId)
    if (!task) return
    try {
      task.controller.abort()
    } catch {
      // 忽略。
    }
  }

  function finishRequest(relayId: string) {
    pending.delete(relayId)
    inFlight.value = Math.max(0, inFlight.value - 1)
  }

  async function executeRequest(frame: any) {
    const relayId = frame.relay_id
    if (typeof relayId !== 'string' || !relayId) return
    const fail = (code: string, message = '') => {
      send({ type: 'relay.response.error', relay_id: relayId, code, message })
    }
    // endpoint 白名单校验。
    if (!RELAY_ENDPOINTS.includes(frame.endpoint)) {
      fail('invalid_request', 'endpoint 不在白名单')
      return
    }
    // URL 由本地设置拼接，不接受服务端下发，保证 Key 只发往用户配置的地址。
    const settings = useSettingsStore()
    const baseUrl = String(settings.settings?.base_url || '')
      .trim()
      .replace(/\/+$/, '')
    if (!baseUrl) {
      fail('invalid_request', '本地 base_url 为空')
      return
    }
    const auth = useAuthStore()
    const key = auth.user ? getLocalKey(auth.user.id) : ''
    if (!key) {
      fail('no_key', '本设备尚未保存 API Key')
      return
    }
    const controller = new AbortController()
    pending.set(relayId, { controller })
    inFlight.value += 1
    let headSent = false
    try {
      const headers: Record<string, string> = {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${key}`,
      }
      if (frame.stream) headers['Accept'] = 'text/event-stream'
      const response = await fetch(relayRequestURL(baseUrl, frame.endpoint), {
        method: 'POST',
        headers,
        body: typeof frame.body === 'string' ? frame.body : '',
        signal: controller.signal,
      })
      send({
        type: 'relay.response.head',
        relay_id: relayId,
        status: response.status,
        content_type: response.headers.get('content-type') || '',
        retry_after: response.headers.get('retry-after') || '',
      })
      headSent = true
      await pumpBody(relayId, response, controller.signal)
    } catch (err: any) {
      // head 之前失败（网络/断网/CORS/DNS/混合内容都表现为 TypeError）。
      if (!headSent) fail('network', err?.message || '网络失败')
      // head 之后失败由 pump 收尾（恰好一帧），这里不再补发。
    } finally {
      finishRequest(relayId)
    }
  }

  // pump 拥有终结权：正常结束发 end，被取消发 error aborted，
  // 连接断开（pending 被清）则静默退出。三种出口恰好一帧或零帧。
  async function pumpBody(relayId: string, response: Response, signal: AbortSignal): Promise<void> {
    if (!response.body) {
      if (pending.has(relayId)) {
        send({ type: 'relay.response.end', relay_id: relayId, seq: 0 })
        pending.delete(relayId)
      }
      return
    }
    const reader = response.body.getReader()
    const task = pending.get(relayId)
    if (task) task.reader = reader
    const decoder = new TextDecoder('utf-8')
    const frameEncoder = new TextEncoder()
    let buffer = ''
    let seq = 0
    let lastFlush = Date.now()
    let flushTimer: ReturnType<typeof setTimeout> | null = null
    // 按 UTF-8 字节切出至多 maxBytes 的前缀（按码点切，不断开代理对）。
    // 字节数由码点直接计算，避免逐字符 encode 分配数组。
    const splitByBytes = (text: string, maxBytes: number): [string, string] => {
      let bytes = 0
      let end = 0
      for (const ch of text) {
        const cp = ch.codePointAt(0) ?? 0
        const len = cp < 0x80 ? 1 : cp < 0x800 ? 2 : cp < 0x10000 ? 3 : 4
        if (bytes + len > maxBytes) break
        bytes += len
        end += ch.length
      }
      return [text.slice(0, end), text.slice(end)]
    }
    const clearFlushTimer = () => {
      if (flushTimer) {
        clearTimeout(flushTimer)
        flushTimer = null
      }
    }
    const flush = () => {
      clearFlushTimer()
      // 单次 read 可能很大：拆成多个 ≤64KiB 帧、seq 连续，保证不断开服务端连接。
      while (buffer) {
        const [head, rest] = splitByBytes(buffer, MAX_FRAME_BYTES)
        if (!head) break
        buffer = rest
        send({ type: 'relay.response.chunk', relay_id: relayId, seq: seq + 1, data: head })
        seq += 1
      }
      lastFlush = Date.now()
    }
    // 上游短暂停顿时，已缓存但不足阈值的片段由该定时器发出，避免尾部滞留。
    const armFlushTimer = () => {
      if (flushTimer || !buffer) return
      flushTimer = setTimeout(() => {
        flushTimer = null
        if (pending.has(relayId) && !signal.aborted && buffer) flush()
      }, CHUNK_INTERVAL_MS)
    }
    const aborted = () => signal.aborted
    const gone = () => !pending.has(relayId)
    const abortAndReport = () => {
      clearFlushTimer()
      send({
        type: 'relay.response.error',
        relay_id: relayId,
        code: 'aborted',
        message: '请求已取消',
      })
      pending.delete(relayId)
    }
    try {
      for (;;) {
        if (gone()) return
        if (aborted()) {
          abortAndReport()
          return
        }
        // 背压：待发字节过多时暂停读取上游。
        if (ws && ws.bufferedAmount > BACKPRESSURE_BYTES) {
          await new Promise((resolve) => setTimeout(resolve, 50))
          continue
        }
        let read: ReadableStreamReadResult<Uint8Array>
        try {
          read = await reader.read()
        } catch {
          if (gone()) return
          abortAndReport()
          return
        }
        if (gone()) return
        if (aborted()) {
          abortAndReport()
          return
        }
        if (read.done) break
        buffer += decoder.decode(read.value, { stream: true })
        if (
          frameEncoder.encode(buffer).length >= CHUNK_BYTES ||
          Date.now() - lastFlush >= CHUNK_INTERVAL_MS
        ) {
          flush()
        } else {
          armFlushTimer()
        }
      }
      buffer += decoder.decode()
      flush()
      if (gone()) return
      send({ type: 'relay.response.end', relay_id: relayId, seq })
      pending.delete(relayId)
    } finally {
      clearFlushTimer()
      try {
        reader.releaseLock()
      } catch {
        // 忽略。
      }
    }
  }

  return { connected, inFlight, ensure, refresh, disconnect }
})
