import test, { before, after } from 'node:test'
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { fileURLToPath } from 'node:url'
import { createPinia, setActivePinia } from 'pinia'
let server, api, useConversationStore, usePlayerActionStore
before(async () => {
  server = await createServer({
    root: fileURLToPath(new URL('..', import.meta.url)),
    optimizeDeps: { noDiscovery: true, include: [] },
    server: { middlewareMode: true, watch: null, hmr: false },
  })
  api = await server.ssrLoadModule('/src/api/index.ts')
  ;({ useConversationStore } = await server.ssrLoadModule('/src/stores/conversations.ts'))
  ;({ usePlayerActionStore } = await server.ssrLoadModule('/src/stores/playerActions.ts'))
})
after(async () => {
  await server?.close()
})
const receipt = (id = 'a', branch = 0) => ({
  id: `receipt-${id}`,
  conversation_id: id,
  branch_epoch: branch,
  revision: 1,
  key: 'abandoned',
  category: 'neutral',
  title: '提前中止',
  summary: '无终局积分',
  tone: 'info',
  rewards: [],
  metrics: [],
  next_actions: [],
})
function setup() {
  setActivePinia(createPinia())
  const cache = new Map()
  globalThis.sessionStorage = {
    getItem: (k) => cache.get(k) ?? null,
    setItem: (k, v) => cache.set(k, v),
    removeItem: (k) => cache.delete(k),
  }
  const conv = useConversationStore()
  conv.current = { id: 'a', branch_epoch: 0, status: 'active' }
  const actions = usePlayerActionStore()
  actions.enter('a')
  api.messagesApi.list = async () => []
  api.stateApi.get = async () => ({ state: {}, schema_version: 1 })
  api.rewardsApi.list = async () => []
  api.conversationsApi.get = async (id) => ({
    conversation: { id, branch_epoch: 0, status: 'ended' },
  })
  api.moduleActionsApi.endings = async () => ({
    branch_epoch: 0,
    revision: 1,
    ending_capabilities: [],
    completion: receipt(),
    proposal: null,
  })
  return { conv, actions }
}
test('HTTP/WS receipts deduplicate and remain reviewable after dismissal and reopen', async () => {
  const { conv } = setup()
  conv.consumeCompletion(receipt())
  const first = conv.completion
  conv.consumeCompletion(receipt())
  assert.equal(conv.completion, first)
  conv.clearEnding()
  await conv.loadEndingState('a')
  assert.equal(conv.completion, null)
  assert.equal(conv.current.status, 'ended')
  assert.equal(conv.lastCompletion.id, 'receipt-a')
  conv.showLastCompletion()
  assert.equal(conv.completion.id, 'receipt-a')
})
test('foreign conversations and invalidated branches cannot complete current conversation', () => {
  const { conv } = setup()
  conv.consumeCompletion(receipt('b'))
  conv.consumeCompletion(receipt('a', 2))
  assert.equal(conv.current.status, 'active')
  assert.equal(conv.completion, null)
})
test('dismissed proposal stays hidden after state refresh', async () => {
  const { conv } = setup()
  api.moduleActionsApi.endings = async () => ({
    branch_epoch: 0,
    revision: 0,
    ending_capabilities: [],
    completion: null,
    proposal: { id: 'p' },
  })
  await conv.loadEndingState('a')
  conv.dismissProposal('p')
  await conv.loadEndingState('a')
  assert.equal(conv.endingState.proposal, null)
  assert.equal(conv.current.status, 'active')
})
test('lost response reuses original request and does not substitute another action', async () => {
  const { actions } = setup()
  const calls = []
  api.moduleActionsApi.execute = async (_id, input) => {
    calls.push(JSON.parse(JSON.stringify(input)))
    if (calls.length === 1) throw new Error('lost response')
    return { revision: 1, result: {}, completion: receipt() }
  }
  const input = {
    request_id: 'original',
    branch_epoch: 0,
    expected_revision: 0,
    key: 'trial.abandon',
    arguments: {},
  }
  await actions.execute('a', input)
  assert.ok(actions.pending)
  assert.equal(await actions.execute('a', { ...input, request_id: 'new' }), null)
  actions.enter('a')
  await actions.execute('a')
  assert.deepEqual(calls, [input, input])
  assert.equal(actions.pending, null)
})
test('late response after switching conversations cannot open an old ending', async () => {
  const { conv, actions } = setup()
  let finish
  api.moduleActionsApi.execute = () =>
    new Promise((resolve) => {
      finish = resolve
    })
  const running = actions.execute('a', {
    request_id: 'r',
    branch_epoch: 0,
    expected_revision: 0,
    key: 'trial.abandon',
    arguments: {},
  })
  actions.enter('b')
  conv.clearCurrent()
  conv.current = { id: 'b', branch_epoch: 0, status: 'active' }
  finish({ revision: 1, result: {}, completion: receipt() })
  await running
  assert.equal(conv.current.status, 'active')
  assert.equal(conv.completion, null)
  assert.equal(actions.acting, false)
})
