import test, { before, after, afterEach } from 'node:test'
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { fileURLToPath } from 'node:url'
import { createPinia, setActivePinia } from 'pinia'

let server, api, useContextStore, useSocketStore, useChatStore, useConversationStore
let useStateStore, useCampaignStore, useGuidanceStore, useRuntimeStore, useRewardStore
let sockets = []
const deferred = () => {
  let resolve, reject
  const promise = new Promise((yes, no) => {
    resolve = yes
    reject = no
  })
  return { promise, resolve, reject }
}
const snapshot = (version = 1, extra = {}) => ({
  conversation_id: 'a',
  activity_version: version,
  branch_epoch: 0,
  conversation_status: 'active',
  is_processing: false,
  turn_id: 'turn',
  attempt_id: 'attempt',
  turn_status: 'completed',
  narrative: 'idle',
  memory: 'idle',
  can_continue: true,
  can_regenerate: true,
  regenerate_cost_quantity: 1,
  ...extra,
})
const settle = async () => {
  for (let i = 0; i < 20; i++) await Promise.resolve()
}
class Socket {
  constructor() {
    sockets.push(this)
  }
  open() {
    this.onopen?.()
  }
  receive(event) {
    this.onmessage?.({ data: JSON.stringify(event) })
  }
  close() {
    this.onclose?.()
  }
  send() {}
}
before(async () => {
  server = await createServer({
    root: fileURLToPath(new URL('..', import.meta.url)),
    optimizeDeps: { noDiscovery: true, include: [] },
    server: { middlewareMode: true, watch: null, hmr: false, ws: false },
  })
  api = await server.ssrLoadModule('/src/api/index.ts')
  ;({ useContextStore } = await server.ssrLoadModule('/src/stores/context.ts'))
  ;({ useSocketStore } = await server.ssrLoadModule('/src/stores/socket.ts'))
  ;({ useChatStore } = await server.ssrLoadModule('/src/stores/chat.ts'))
  ;({ useConversationStore } = await server.ssrLoadModule('/src/stores/conversations.ts'))
  ;({ useStateStore } = await server.ssrLoadModule('/src/stores/state.ts'))
  ;({ useCampaignStore } = await server.ssrLoadModule('/src/stores/campaign.ts'))
  ;({ useGuidanceStore } = await server.ssrLoadModule('/src/stores/guidance.ts'))
  ;({ useRuntimeStore } = await server.ssrLoadModule('/src/stores/runtime.ts'))
  ;({ useRewardStore } = await server.ssrLoadModule('/src/stores/rewards.ts'))
})
after(async () => {
  await server?.close()
})
let cleanup = () => {}
afterEach(() => {
  cleanup()
  cleanup = () => {}
})

function setup() {
  setActivePinia(createPinia())
  const context = useContextStore()
  context.enter('a')
  let reads = 0
  let inspectorReads = 0
  api.activityApi.get = async () => {
    reads++
    return snapshot()
  }
  api.runtimeInspectorApi.get = async () => {
    inspectorReads++
    return { activity: snapshot(), request: null }
  }
  return { context, reads: () => reads, inspectorReads: () => inspectorReads }
}

function browserSetup() {
  const fixture = setup()
  globalThis.window = Object.assign(new EventTarget(), {
    location: { protocol: 'http:', host: 'test' },
  })
  globalThis.document = Object.assign(new EventTarget(), { visibilityState: 'visible' })
  Object.defineProperty(globalThis, 'navigator', { configurable: true, value: { onLine: true } })
  globalThis.WebSocket = Socket
  sockets = []
  const socket = useSocketStore(),
    chat = useChatStore(),
    conv = useConversationStore()
  conv.current = { id: 'a', status: 'active', branch_epoch: 0, is_processing: false }
  let businessReads = 0
  conv.load = async () => {
    businessReads++
    return conv.current
  }
  api.conversationsApi.get = async () => ({ conversation: conv.current })
  conv.loadEndingState = async () => {}
  chat.load = async () => {
    chat.messages = []
  }
  useStateStore().load = async () => {}
  useCampaignStore().load = async () => {}
  useGuidanceStore().load = async () => {}
  useRuntimeStore().load = async () => {}
  useRewardStore().load = async () => {}
  cleanup = () => socket.disconnect()
  return { ...fixture, socket, chat, conv, businessReads: () => businessReads }
}

async function ready(fixture) {
  fixture.socket.connect('a')
  const transport = sockets.at(-1)
  transport.open()
  transport.receive({ type: 'subscription.ready', conversation_id: 'a' })
  await settle()
  return transport
}

test('single flight catches a newer invalidation arriving during an older read', async () => {
  const { context } = setup()
  const first = deferred(),
    second = deferred()
  let calls = 0
  api.activityApi.get = () => (++calls === 1 ? first.promise : second.promise)
  const p = context.loadActivity('a')
  const concurrent = context.loadActivity('a', 41)
  assert.equal(calls, 1)
  first.resolve(snapshot(40))
  await settle()
  assert.equal(calls, 2)
  second.resolve(snapshot(41))
  await p
  await concurrent
  assert.equal(context.activity.activity_version, 41)
  assert.equal(context.activityStale, false)
})

test('newer complete WS snapshot satisfies the target without another REST read', async () => {
  const { context } = setup()
  const read = deferred()
  let calls = 0
  api.activityApi.get = () => {
    calls++
    return read.promise
  }
  const p = context.loadActivity('a', 41)
  context.applyActivitySnapshot(snapshot(41))
  read.resolve(snapshot(40))
  await p
  assert.equal(calls, 1)
  assert.equal(context.activity.activity_version, 41)
})

test('REST and inspector cannot roll back a newer phase or stable snapshot', async () => {
  const { context } = setup()
  context.applyActivitySnapshot(snapshot(2, { is_processing: true, turn_status: 'pending' }))
  const read = deferred()
  api.activityApi.get = () => read.promise
  const p = context.loadActivity('a')
  context.notePhaseAttempt('attempt')
  context.setNarrative('tool', { step_no: 2, tool_category: 'state' })
  read.resolve(snapshot(2, { is_processing: true, narrative: 'model', turn_status: 'pending' }))
  await p
  assert.equal(context.activity.narrative, 'tool')
  context.applyActivitySnapshot(snapshot(5))
  await context.loadRuntimeInspector('a')
  assert.equal(context.activity.activity_version, 5)
})

test('read failure preserves the last snapshot and marks it stale', async () => {
  const { context } = setup()
  context.applyActivitySnapshot(snapshot(4, { is_processing: true, narrative: 'model' }))
  api.activityApi.get = async () => {
    throw Error('offline')
  }
  await assert.rejects(context.loadActivity('a'))
  assert.equal(context.activity.narrative, 'model')
  assert.equal(context.activity.activity_version, 4)
  assert.equal(context.activityStale, true)
})

test('late response and finally cannot affect a reopened conversation request', async () => {
  const { context } = setup()
  const old = deferred(),
    current = deferred()
  api.activityApi.get = () => old.promise
  const p = context.loadActivity('a')
  context.reset()
  context.enter('a')
  let currentReads = 0
  api.activityApi.get = () => {
    currentReads++
    return current.promise
  }
  const next = context.loadActivity('a')
  old.resolve(snapshot(99))
  await p
  assert.equal(context.activity.activity_version, undefined)
  const shared = context.loadActivity('a')
  assert.equal(currentReads, 1)
  current.resolve(snapshot(3))
  await next
  await shared
  assert.equal(context.activity.activity_version, 3)
})

test('hidden inspector only records invalidations and loads once when shown', async () => {
  const { context, inspectorReads } = setup()
  context.invalidateInspector()
  context.invalidateInspector()
  assert.equal(inspectorReads(), 0)
  context.setInspectorVisible(true)
  await settle()
  assert.equal(inspectorReads(), 1)
})

test('subscription ready precedes initial snapshot and idle pages never poll', async (t) => {
  const fixture = browserSetup()
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'] })
  fixture.socket.connect('a')
  const transport = sockets.at(-1)
  transport.open()
  assert.equal(fixture.reads(), 0)
  assert.equal(fixture.socket.connected, false)
  transport.receive({ type: 'subscription.ready', conversation_id: 'a' })
  await settle()
  assert.equal(fixture.reads(), 1)
  assert.equal(fixture.socket.connected, true)
  t.mock.timers.tick(600000)
  await settle()
  assert.equal(fixture.reads(), 1)
  assert.equal(fixture.inspectorReads(), 0)
})

test('complete snapshot can skip versions without requesting activity', async () => {
  const fixture = browserSetup()
  const transport = await ready(fixture)
  transport.receive({
    type: 'activity_updated',
    conversation_id: 'a',
    activity_version: 42,
    branch_epoch: 0,
    snapshot: snapshot(42, { memory: 'running' }),
  })
  await settle()
  assert.equal(fixture.context.activity.activity_version, 42)
  assert.equal(fixture.reads(), 1)
})

test('messages during a snapshot read are buffered and merged after replacement', async () => {
  const fixture = browserSetup()
  const messages = deferred()
  fixture.chat.load = async () => {
    await messages.promise
    fixture.chat.messages = []
  }
  fixture.socket.connect('a')
  const transport = sockets.at(-1)
  transport.open()
  transport.receive({ type: 'subscription.ready', conversation_id: 'a' })
  const message = {
    id: 'message',
    type: 'assistant',
    markdown: 'persisted',
    metadata: {},
    created_at: 'now',
  }
  transport.receive({ type: 'message_created', conversation_id: 'a', message })
  messages.resolve()
  await settle()
  assert.equal(fixture.chat.messages.length, 1)
  assert.equal(fixture.chat.messages[0].markdown, 'persisted')
})

test('missed terminal recovers and unlocks even with a streaming placeholder', async (t) => {
  const fixture = browserSetup()
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'] })
  api.activityApi.get = async () => snapshot(1, { is_processing: true, turn_status: 'pending' })
  await ready(fixture)
  fixture.chat.beginStream('stream')
  await settle()
  api.activityApi.get = async () => snapshot(3)
  t.mock.timers.tick(75001)
  await settle()
  assert.equal(fixture.chat.isGenerating, false)
  assert.equal(fixture.chat.streamingId, null)
  assert.equal(fixture.context.activity.is_processing, false)
})

test('normal agent phases and a matching start snapshot do not fetch REST or inspector', async () => {
  const fixture = browserSetup()
  const transport = await ready(fixture)
  const scoped = { conversation_id: 'a', attempt_id: 'new', operation_id: 'new-operation' }
  transport.receive({
    ...scoped,
    type: 'agent.turn.started',
    sequence: 1,
    payload: { message_id: 'm' },
  })
  transport.receive({
    ...scoped,
    type: 'agent.tool.started',
    sequence: 2,
    step_no: 1,
    payload: { category: 'state' },
  })
  transport.receive({
    type: 'activity_updated',
    conversation_id: 'a',
    activity_version: 2,
    branch_epoch: 0,
    snapshot: snapshot(2, {
      turn_id: 'new-turn',
      attempt_id: 'new',
      is_processing: true,
      turn_status: 'pending',
      narrative: 'preparing',
    }),
  })
  await settle()
  assert.equal(fixture.reads(), 1)
  assert.equal(fixture.inspectorReads(), 0)
  assert.equal(fixture.context.activity.narrative, 'tool')
})

test('zero branch epoch may be omitted from the event envelope', async () => {
  const fixture = browserSetup()
  const transport = await ready(fixture)
  transport.receive({
    type: 'activity_updated',
    conversation_id: 'a',
    activity_version: 2,
    snapshot: snapshot(2),
  })
  await settle()
  assert.equal(fixture.context.activity.activity_version, 2)
  assert.equal(fixture.reads(), 1)
})

test('failed business recovery retries even when the activity read succeeded', async (t) => {
  const fixture = browserSetup()
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'] })
  let attempts = 0
  fixture.chat.load = async () => {
    if (++attempts === 1) throw Error('temporary failure')
  }
  await ready(fixture)
  assert.equal(attempts, 1)
  assert.equal(fixture.context.activityStale, false)
  t.mock.timers.tick(6000)
  await settle()
  assert.equal(attempts, 2)
  const reads = fixture.reads()
  t.mock.timers.tick(600000)
  await settle()
  assert.equal(fixture.reads(), reads)
})

test('a committed current-attempt message survives terminal snapshot recovery', async () => {
  const fixture = browserSetup()
  api.activityApi.get = async () => snapshot(1, { is_processing: true, turn_status: 'pending' })
  const transport = await ready(fixture)
  const scoped = { conversation_id: 'a', attempt_id: 'attempt', operation_id: 'operation' }
  transport.receive({
    ...scoped,
    type: 'agent.turn.started',
    sequence: 1,
    payload: { message_id: 'message' },
  })
  const read = deferred()
  fixture.chat.load = async () => {
    await read.promise
    fixture.chat.messages = []
  }
  api.activityApi.get = async () => snapshot(2)
  window.dispatchEvent(new Event('online'))
  transport.receive({
    ...scoped,
    type: 'message_created',
    message: {
      id: 'message',
      type: 'assistant',
      markdown: 'committed',
      metadata: {},
      created_at: 'now',
    },
  })
  read.resolve()
  await settle()
  assert.equal(fixture.chat.isGenerating, false)
  assert.equal(fixture.chat.messages.length, 1)
  assert.equal(fixture.chat.messages[0].markdown, 'committed')
})

test('duplicate agent events cannot postpone a stalled-task watchdog', async (t) => {
  const fixture = browserSetup()
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'] })
  let reads = 0
  api.activityApi.get = async () => {
    reads++
    return snapshot(1, { is_processing: true, turn_status: 'pending' })
  }
  const transport = await ready(fixture)
  const phase = {
    type: 'agent.step.started',
    conversation_id: 'a',
    attempt_id: 'attempt',
    operation_id: 'operation',
    sequence: 1,
    event_id: 'phase',
    step_no: 1,
  }
  transport.receive(phase)
  t.mock.timers.tick(70000)
  transport.receive(phase)
  t.mock.timers.tick(5001)
  await settle()
  assert.equal(reads, 2)
})

test('missing subscription acknowledgement closes the socket for recovery', async (t) => {
  const fixture = browserSetup()
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'] })
  fixture.socket.connect('a')
  sockets.at(-1).open()
  t.mock.timers.tick(5001)
  await settle()
  assert.equal(fixture.socket.connected, false)
  assert.equal(fixture.socket.reconnecting, true)
})

test('reconnect refreshes a visible inspector after missed terminal events', async (t) => {
  const fixture = browserSetup()
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'] })
  const transport = await ready(fixture)
  fixture.context.setInspectorVisible(true)
  await settle()
  const before = fixture.inspectorReads()
  transport.close()
  assert.equal(fixture.context.inspectorDirty, true)
  t.mock.timers.tick(1000)
  sockets.at(-1).open()
  sockets.at(-1).receive({ type: 'subscription.ready', conversation_id: 'a' })
  await settle()
  assert.equal(fixture.inspectorReads(), before + 1)
  assert.equal(fixture.context.inspectorDirty, false)
})

test('subscription registration failures stop after five reconnect attempts', async (t) => {
  const fixture = browserSetup()
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'] })
  fixture.socket.connect('a')
  const initial = sockets.at(-1)
  initial.open()
  initial.close()
  for (const delay of [1000, 2000, 4000, 8000, 16000]) {
    const count = sockets.length
    t.mock.timers.tick(delay - 1)
    await settle()
    assert.equal(sockets.length, count)
    t.mock.timers.tick(1)
    await settle()
    assert.equal(sockets.length, count + 1)
    assert.equal(fixture.socket.reconnecting, true)
    const transport = sockets.at(-1)
    transport.open()
    assert.equal(fixture.socket.connected, false)
    transport.close()
    await settle()
  }
  assert.equal(sockets.length, 6)
  assert.equal(fixture.socket.connectionState, 'exhausted')
  assert.equal(fixture.socket.writeBlocked, true)
  assert.match(fixture.socket.connectionNotice, /刷新页面/)
  assert.equal(fixture.socket.sendUserInput('blocked'), false)
  assert.equal(fixture.socket.steer('blocked'), false)
  assert.equal(fixture.socket.followUp('blocked'), false)
  assert.equal(fixture.socket.clearQueue(), false)
  const count = sockets.length
  t.mock.timers.tick(60000)
  await settle()
  assert.equal(sockets.length, count)
})

test('the fifth reconnect can succeed and later restores its budget after stability', async (t) => {
  const fixture = browserSetup()
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'] })
  fixture.socket.connect('a')
  sockets.at(-1).open()
  sockets.at(-1).close()
  for (const delay of [1000, 2000, 4000, 8000]) {
    t.mock.timers.tick(delay)
    await settle()
    sockets.at(-1).open()
    sockets.at(-1).close()
    await settle()
  }
  t.mock.timers.tick(16000)
  await settle()
  assert.equal(sockets.length, 6)
  const fifth = sockets.at(-1)
  fifth.open()
  fifth.receive({ type: 'subscription.ready', conversation_id: 'a' })
  await settle()
  assert.equal(fixture.socket.connected, true)
  assert.equal(fixture.socket.connectionState, 'ready')
  assert.equal(fixture.socket.reconnectAttempt, 5)
  t.mock.timers.tick(29999)
  await settle()
  assert.equal(fixture.socket.reconnectAttempt, 5)
  t.mock.timers.tick(1)
  await settle()
  assert.equal(fixture.socket.reconnectAttempt, 0)
  assert.equal(fixture.socket.connectionState, 'ready')
})

test('foreign ready cannot reset registration backoff or start recovery', async () => {
  const fixture = browserSetup()
  fixture.socket.connect('a')
  sockets.at(-1).open()
  sockets.at(-1).receive({ type: 'subscription.ready', conversation_id: 'other' })
  await settle()
  assert.equal(fixture.socket.connected, false)
  assert.equal(fixture.reads(), 0)
})

test('permanent read failures stop recovery while transient failures remain retryable', async (t) => {
  const { isRetryableReadError } = await server.ssrLoadModule('/src/utils/recovery.ts')
  for (const status of [400, 401, 403, 404, 410, 422]) {
    assert.equal(isRetryableReadError({ response: { status } }), false)
    assert.equal(isRetryableReadError({ status }), false)
  }
  for (const status of [408, 429, 500, 502, 503, 504]) {
    assert.equal(isRetryableReadError({ response: { status } }), true)
  }
  assert.equal(isRetryableReadError(Error('Network Error')), true)
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'] })
  for (const status of [401, 403, 404, 410]) {
    const fixture = browserSetup()
    fixture.conv.load = async () => {
      throw { response: { status } }
    }
    await ready(fixture)
    assert.equal(fixture.socket.currentConvId, 'a')
    assert.equal(fixture.socket.connectionState, 'access_denied')
    assert.equal(fixture.socket.connected, false)
    assert.equal(fixture.socket.writeBlocked, true)
    assert.equal(fixture.socket.reconnecting, false)
    const count = sockets.length
    const reads = fixture.reads()
    t.mock.timers.tick(60000)
    await settle()
    assert.equal(sockets.length, count)
    assert.equal(fixture.reads(), reads)
    fixture.socket.disconnect()
  }
})

test('hiding or going offline cancels queued retries without spending the budget', async (t) => {
  const fixture = browserSetup()
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'] })
  fixture.socket.connect('a')
  sockets.at(-1).close()
  for (const offline of [false, true]) {
    if (offline) navigator.onLine = false
    else document.visibilityState = 'hidden'
    ;(offline ? window : document).dispatchEvent(
      new Event(offline ? 'offline' : 'visibilitychange'),
    )
    t.mock.timers.tick(1000)
    await settle()
    assert.equal(sockets.length, 1)
    if (offline) navigator.onLine = true
    else document.visibilityState = 'visible'
    ;(offline ? window : document).dispatchEvent(new Event(offline ? 'online' : 'visibilitychange'))
    await settle()
  }
  t.mock.timers.tick(1000)
  await settle()
  assert.equal(sockets.length, 2)
  sockets.at(-1).close()
  t.mock.timers.tick(1999)
  await settle()
  assert.equal(sockets.length, 2)
  t.mock.timers.tick(1)
  await settle()
  assert.equal(sockets.length, 3)
})

test('visibility and online events cannot create another socket during handshake or registration', async (t) => {
  const fixture = browserSetup()
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'] })
  fixture.socket.connect('a')
  for (const opened of [false, true]) {
    if (opened) sockets.at(-1).open()
    window.dispatchEvent(new Event('online'))
    document.dispatchEvent(new Event('visibilitychange'))
    t.mock.timers.tick(1000)
    await settle()
    assert.equal(sockets.length, 1)
  }
})

test('a late access denial from an old conversation probe cannot stop the new connection', async () => {
  const fixture = browserSetup()
  const old = deferred()
  api.conversationsApi.get = () => old.promise
  fixture.socket.connect('a')
  sockets.at(-1).close()
  fixture.socket.connect('b')
  const transport = sockets.at(-1)
  transport.open()
  api.activityApi.get = async () => snapshot(1, { conversation_id: 'b' })
  transport.receive({ type: 'subscription.ready', conversation_id: 'b' })
  await settle()
  old.reject({ response: { status: 404 } })
  await settle()
  assert.equal(fixture.socket.currentConvId, 'b')
  assert.equal(fixture.socket.connectionState, 'ready')
  assert.equal(fixture.socket.writeBlocked, false)
})

test('inspector invalidation while its old request is in flight schedules one newer read', async (t) => {
  const { context } = setup()
  t.mock.timers.enable({ apis: ['setTimeout'] })
  const first = deferred()
  let reads = 0
  api.runtimeInspectorApi.get = async () => {
    reads++
    return reads === 1 ? first.promise : { activity: snapshot(2) }
  }
  context.setInspectorVisible(true)
  context.invalidateInspector()
  t.mock.timers.tick(50)
  await settle()
  assert.equal(reads, 1)
  first.resolve({ activity: snapshot(1) })
  await settle()
  t.mock.timers.tick(50)
  await settle()
  assert.equal(reads, 2)
  assert.equal(context.inspectorDirty, false)
})

test('a transient first recovery failure still returns the budget after a successful fallback and stability', async (t) => {
  const fixture = browserSetup()
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'] })
  const original = fixture.conv.load
  let reads = 0
  fixture.conv.load = async (...args) => {
    if (++reads === 1) throw Error('offline')
    return original(...args)
  }
  fixture.socket.connect('a')
  sockets.at(-1).close()
  t.mock.timers.tick(1000)
  await settle()
  sockets.at(-1).open()
  sockets.at(-1).receive({ type: 'subscription.ready', conversation_id: 'a' })
  await settle()
  assert.equal(fixture.socket.reconnectAttempt, 1)
  t.mock.timers.tick(5501)
  await settle()
  assert.equal(reads, 2)
  t.mock.timers.tick(30000)
  await settle()
  assert.equal(fixture.socket.reconnectAttempt, 0)
})

test('repeated business recovery failures exhaust their budget and stop all automatic reads', async (t) => {
  const fixture = browserSetup()
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'] })
  let reads = 0
  fixture.conv.load = async () => {
    reads++
    throw Error('offline')
  }
  await ready(fixture)
  for (let attempt = 1; attempt < 5; attempt++) {
    t.mock.timers.tick(attempt === 1 ? 5501 : attempt === 2 ? 11001 : 33001)
    await settle()
  }
  assert.equal(reads, 5)
  assert.equal(fixture.socket.connectionState, 'exhausted')
  const before = fixture.reads()
  t.mock.timers.tick(600000)
  await settle()
  assert.equal(fixture.reads(), before)
})

test('failed event resource refresh enters bounded recovery instead of losing the invalidation', async (t) => {
  const fixture = browserSetup()
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'] })
  const transport = await ready(fixture)
  let reads = 0
  useRuntimeStore().load = async () => {
    if (++reads === 1) throw Error('offline')
  }
  transport.receive({ type: 'state_update', state: { changed: true } })
  t.mock.timers.tick(50)
  await settle()
  assert.equal(reads, 1)
  t.mock.timers.tick(5501)
  await settle()
  assert.equal(reads, 2)
  assert.equal(fixture.socket.connectionState, 'ready')
})

test('campaign messages arriving during a snapshot read survive the coalesced follow-up', async (t) => {
  const fixture = browserSetup()
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'] })
  const transport = await ready(fixture)
  fixture.conv.current.campaign_id = 'world'
  const campaign = useCampaignStore()
  campaign.snapshot = { campaign: { id: 'world', revision: 1, state_hash: 'one' } }
  const first = deferred()
  let reads = 0
  campaign.load = async () => {
    reads++
    if (reads === 1) await first.promise
    campaign.snapshot = {
      campaign: {
        id: 'world',
        revision: reads === 1 ? 1 : 2,
        state_hash: reads === 1 ? 'one' : 'two',
      },
    }
  }
  // 已有的旧读尚未返回，随后到达的新正文必须促成一次更新的快照读取。
  const { useConversationResources } = await server.ssrLoadModule(
    '/src/stores/conversationResources.ts',
  )
  const old = useConversationResources().load('campaign')
  await settle()
  transport.receive({
    type: 'message_created',
    message: {
      id: 'new-message',
      conversation_id: 'a',
      type: 'assistant',
      markdown: 'new narrative',
      campaign_revision: 2,
      campaign_state_hash: 'two',
      metadata: {},
      created_at: 'now',
    },
  })
  await settle()
  assert.equal(reads, 1)
  assert.equal(fixture.chat.messages.length, 0)
  first.resolve()
  await old
  await settle()
  t.mock.timers.tick(50)
  await settle()
  assert.equal(reads, 2)
  assert.equal(fixture.chat.messages[0]?.id, 'new-message')
})

test('state_update with refresh_required reloads player state instead of keeping stale data', async (t) => {
  const fixture = browserSetup()
  t.mock.timers.enable({ apis: ['setTimeout', 'Date'] })
  const transport = await ready(fixture)
  let reads = 0
  useStateStore().load = async () => {
    reads++
  }
  transport.receive({ type: 'state_update', conversation_id: 'a', refresh_required: true })
  t.mock.timers.tick(50)
  await settle()
  assert.equal(reads, 1)
})

for (const type of [
  'agent.turn.completed',
  'agent.turn.partial',
  'agent.turn.failed',
  'agent.turn.cancelled',
]) {
  test(`${type} refreshes player state and campaign`, async (t) => {
    const fixture = browserSetup()
    t.mock.timers.enable({ apis: ['setTimeout', 'Date'] })
    const transport = await ready(fixture)
    fixture.conv.current.campaign_id = 'world'
    let stateReads = 0,
      campaignReads = 0
    useStateStore().load = async () => {
      stateReads++
    }
    useCampaignStore().load = async () => {
      campaignReads++
    }
    transport.receive({
      type,
      conversation_id: 'a',
      attempt_id: 'attempt-x',
      sequence: 1,
      payload: {},
    })
    t.mock.timers.tick(50)
    await settle()
    assert.ok(stateReads >= 1, 'state reloaded')
    assert.ok(campaignReads >= 1, 'campaign reloaded')
  })
}
