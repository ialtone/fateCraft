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
