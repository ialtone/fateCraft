import test, { before, after } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import ts from 'typescript'
import { createServer } from 'vite'
import { fileURLToPath } from 'node:url'
import { createPinia, setActivePinia } from 'pinia'

let server, api, useSettingsStore, useRewardStore, useAuthStore

const deferred = () => {
  let resolve, reject
  const promise = new Promise((yes, no) => {
    resolve = yes
    reject = no
  })
  return { promise, resolve, reject }
}
const settle = async () => {
  for (let i = 0; i < 20; i++) await Promise.resolve()
}

before(async () => {
  server = await createServer({
    root: fileURLToPath(new URL('..', import.meta.url)),
    optimizeDeps: { noDiscovery: true, include: [] },
    server: { middlewareMode: true, watch: null, hmr: false, ws: false },
  })
  api = await server.ssrLoadModule('/src/api/index.ts')
  ;({ useSettingsStore } = await server.ssrLoadModule('/src/stores/settings.ts'))
  ;({ useRewardStore } = await server.ssrLoadModule('/src/stores/rewards.ts'))
  ;({ useAuthStore } = await server.ssrLoadModule('/src/stores/auth.ts'))
})
after(async () => {
  await server?.close()
})

test('settings reuse the account cache and merge concurrent reads', async () => {
  setActivePinia(createPinia())
  useAuthStore().user = { id: 'u1' }
  let calls = 0
  const first = deferred()
  api.settingsApi.get = () => {
    calls++
    return first.promise
  }
  const settings = useSettingsStore()
  const one = settings.load()
  const two = settings.load()
  await settle()
  assert.equal(calls, 1)

  first.resolve({ llm_route: 'server', client_route_enabled: true })
  await Promise.all([one, two])
  assert.equal(settings.settings?.llm_route, 'server')
  await settings.load()
  assert.equal(calls, 1)

  const forced = deferred()
  api.settingsApi.get = () => {
    calls++
    return forced.promise
  }
  const three = settings.load(true)
  await settle()
  assert.equal(calls, 2)
  forced.resolve({ llm_route: 'client', client_route_enabled: true })
  await three
  assert.equal(settings.settings?.llm_route, 'client')
})

test('settings reset drops a late response from the previous account', async () => {
  setActivePinia(createPinia())
  const auth = useAuthStore()
  auth.user = { id: 'old' }
  const old = deferred()
  api.settingsApi.get = () => old.promise
  const settings = useSettingsStore()
  const pending = settings.load(true)
  await settle()

  settings.reset()
  auth.user = { id: 'new' }
  api.settingsApi.get = async () => ({ llm_route: 'server', client_route_enabled: false })
  assert.equal((await settings.load()).llm_route, 'server')
  old.resolve({ llm_route: 'client', client_route_enabled: true })
  await pending
  assert.equal(settings.settings?.llm_route, 'server')
})

test('rewards merge concurrent reads and ignore a reset generation', async () => {
  setActivePinia(createPinia())
  let calls = 0
  const first = deferred()
  api.rewardsApi.list = () => {
    calls++
    return first.promise
  }
  const rewards = useRewardStore()
  const one = rewards.load()
  const two = rewards.load()
  await settle()
  assert.equal(calls, 1)
  first.resolve([])
  await Promise.all([one, two])
  assert.equal(rewards.loaded, true)

  const stale = deferred()
  api.rewardsApi.list = () => stale.promise
  const pending = rewards.load()
  await settle()
  rewards.reset()
  api.rewardsApi.list = async () => [{ asset: { key: 'points' }, balance: 7 }]
  await rewards.load()
  stale.resolve([{ asset: { key: 'points' }, balance: 99 }])
  await pending
  assert.equal(rewards.balanceFor('points'), 7)
})

// 执行输入组件的真实提交函数，用可控的玩法校验 Promise 覆盖 await 窗口。
function inputFixture() {
  const source = readFileSync(new URL('../src/components/ActionInput.vue', import.meta.url), 'utf8')
  const start = source.indexOf('async function ensureGameplayFresh(')
  const end = source.indexOf('function dispatchGuidanceMessage(', start)
  const compiled = ts.transpile(source.slice(start, end), { target: ts.ScriptTarget.ESNext })
  const check = deferred()
  let reads = 0,
    posts = 0,
    frames = 0,
    optimistic = 0
  const props = { convId: 'a', disabled: false }
  const socket = {
    writeBlocked: false,
    clearError() {},
    sendUserInput() {
      frames++
      return false
    },
  }
  const chat = {
    isGenerating: false,
    addOptimisticUser() {
      optimistic++
      return 'temp'
    },
    markGenerating() {
      this.isGenerating = true
    },
    removeById() {},
    stopGenerating() {
      this.isGenerating = false
    },
  }
  const dispatch = new Function(
    'props',
    'socket',
    'chat',
    'gameplay',
    'message',
    'messagesApi',
    'checkingGameplay',
    'awaitingRestGen',
    'actionPending',
    'let inputEpoch = 0; ' + compiled + '; return dispatchUserInput',
  )(
    props,
    socket,
    chat,
    {
      ensureFresh() {
        reads++
        return check.promise
      },
    },
    { error() {}, warning() {} },
    {
      async send() {
        posts++
      },
    },
    { value: false },
    { value: false },
    { value: false },
  )
  return {
    check,
    props,
    socket,
    chat,
    dispatch,
    reads: () => reads,
    posts: () => posts,
    frames: () => frames,
    optimistic: () => optimistic,
  }
}

test('exhaustion while gameplay validation is pending cannot bypass the limit through REST', async () => {
  const f = inputFixture()
  const request = f.dispatch('hello')
  f.socket.writeBlocked = true
  f.check.resolve({ run_allowed: true })
  assert.equal(await request, false)
  assert.equal(f.posts(), 0)
  assert.equal(f.frames(), 0)
  assert.equal(f.optimistic(), 0)
})

test('switching conversations during validation cannot send old input into the new conversation', async () => {
  const f = inputFixture()
  const request = f.dispatch('old input')
  f.props.convId = 'b'
  f.check.resolve({ run_allowed: true })
  assert.equal(await request, false)
  assert.equal(f.posts(), 0)
  assert.equal(f.frames(), 0)
})

test('double clicks during gameplay validation submit one intent only', async () => {
  const f = inputFixture()
  const one = f.dispatch('hello'),
    two = f.dispatch('hello')
  assert.equal(f.reads(), 1)
  f.check.resolve({ run_allowed: true })
  assert.deepEqual(await Promise.all([one, two]), [true, false])
  assert.equal(f.posts(), 1)
  assert.equal(f.optimistic(), 1)
})

test('paused gameplay returned by the freshness check blocks generation', async () => {
  const f = inputFixture()
  const request = f.dispatch('hello')
  f.check.resolve({ run_allowed: false })
  assert.equal(await request, false)
  assert.equal(f.posts(), 0)
  assert.equal(f.frames(), 0)
})

test('terminal connection state blocks module actions and inquiry writes inside their stores', async () => {
  setActivePinia(createPinia())
  const { useSocketStore } = await server.ssrLoadModule('/src/stores/socket.ts')
  const { useConversationStore } = await server.ssrLoadModule('/src/stores/conversations.ts')
  const { usePlayerActionStore } = await server.ssrLoadModule('/src/stores/playerActions.ts')
  const { useInquiryStore } = await server.ssrLoadModule('/src/stores/inquiry.ts')
  useConversationStore().current = { id: 'a' }
  const actions = usePlayerActionStore()
  actions.enter('a')
  useSocketStore().connectionState = 'exhausted'
  let calls = 0
  api.moduleActionsApi.execute = async () => {
    calls++
    throw Error('must not send')
  }
  for (const key of ['ask', 'cancel', 'clear'])
    api.inquiriesApi[key] = async () => {
      calls++
    }
  const inquiry = useInquiryStore()
  inquiry.asking = true
  await actions.execute('a', { request_id: 'new' })
  await inquiry.ask('a', 'hello')
  await inquiry.cancel('a')
  await inquiry.clear('a')
  assert.equal(calls, 0)
  assert.equal(actions.pending, null)
})

test('a failed gameplay refresh invalidates the cached gate until a successful retry', async () => {
  setActivePinia(createPinia())
  const { useGameplayVersionStore } = await server.ssrLoadModule('/src/stores/gameplayVersion.ts')
  const gameplay = useGameplayVersionStore()
  const original = api.default.get
  let reads = 0
  api.default.get = async () => {
    if (++reads === 2) throw Error('offline')
    return { data: { run_allowed: reads === 1, version: '1' } }
  }
  try {
    await gameplay.load('a')
    assert.equal(gameplay.fresh('a'), true)
    await assert.rejects(gameplay.load('a', true))
    assert.equal(gameplay.fresh('a'), false)
    assert.equal((await gameplay.ensureFresh('a')).run_allowed, false)
    assert.equal(reads, 3)
  } finally {
    api.default.get = original
  }
})
