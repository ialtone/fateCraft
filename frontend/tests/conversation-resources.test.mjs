import test, { before, after } from 'node:test'
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { fileURLToPath } from 'node:url'
import { createPinia, setActivePinia } from 'pinia'

let server, useConversationResources, useStateStore

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
  ;({ useConversationResources } = await server.ssrLoadModule(
    '/src/stores/conversationResources.ts',
  ))
  ;({ useStateStore } = await server.ssrLoadModule('/src/stores/state.ts'))
})
after(async () => {
  await server?.close()
})

test('same resource is single-flight and re-reads once after an in-flight invalidation', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  setActivePinia(createPinia())
  const resources = useConversationResources()
  resources.enter('a')
  const state = useStateStore()
  const first = deferred()
  let reads = 0
  state.load = async () => {
    reads++
    return reads === 1 ? first.promise : undefined
  }

  const one = resources.load('state')
  const two = resources.load('state')
  await settle()
  assert.equal(reads, 1)
  resources.invalidate('state')
  first.resolve()
  await Promise.all([one, two])
  await settle()
  assert.equal(reads, 1)

  t.mock.timers.tick(50)
  await settle()
  assert.equal(reads, 2)
})

test('hidden actions only become dirty and read once when shown', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  setActivePinia(createPinia())
  const resources = useConversationResources()
  resources.enter('a')
  let reads = 0
  resources.registerLoader('actions', async () => {
    reads++
  })
  resources.setVisible('actions', false)
  resources.invalidate('actions', true)
  t.mock.timers.tick(100)
  await settle()
  assert.equal(reads, 0)

  resources.setVisible('actions', true)
  await settle()
  assert.equal(reads, 1)
})

test('suspended connection context blocks loads and invalidations', async () => {
  setActivePinia(createPinia())
  const resources = useConversationResources()
  resources.enter('a')
  resources.suspend()
  const state = useStateStore()
  let reads = 0
  state.load = async () => {
    reads++
  }

  resources.invalidate('state', true)
  await resources.load('state')
  await settle()
  assert.equal(reads, 0)

  resources.resume()
  resources.invalidate('state', true)
  await settle()
  assert.equal(reads, 1)
})

test('concurrent readers without an invalidation do not trigger an extra read', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  setActivePinia(createPinia())
  const resources = useConversationResources()
  resources.enter('a')
  const first = deferred()
  let reads = 0
  resources.registerLoader('state', async () => {
    reads++
    return first.promise
  })
  const one = resources.load('state'),
    two = resources.load('state')
  await settle()
  first.resolve()
  await Promise.all([one, two])
  t.mock.timers.tick(1000)
  await settle()
  assert.equal(reads, 1)
})

test('queued loaders are discarded when suspended or switched before they start', async () => {
  setActivePinia(createPinia())
  const resources = useConversationResources()
  resources.enter('a')
  let reads = 0
  resources.registerLoader('state', async () => {
    reads++
  })
  const first = resources.load('state')
  resources.suspend()
  await first
  assert.equal(reads, 0)
  resources.resume()
  const second = resources.load('state')
  resources.enter('b')
  resources.registerLoader('state', async () => {
    reads++
  })
  await second
  assert.equal(reads, 0)
})

test('suspending a real store read prevents its late response from overwriting the last page', async () => {
  setActivePinia(createPinia())
  const api = await server.ssrLoadModule('/src/api/index.ts')
  const resources = useConversationResources()
  resources.enter('a')
  const state = useStateStore()
  state.set({ last: true })
  const old = deferred()
  api.stateApi.get = () => old.promise
  const request = resources.load('state')
  await settle()
  resources.suspend()
  old.resolve({ old: true })
  await request
  assert.deepEqual(state.state, { last: true })
})

test('resource failures retain dirty state without creating an unlimited retry timer', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  setActivePinia(createPinia())
  const resources = useConversationResources()
  resources.enter('a')
  let reads = 0
  resources.registerLoader('state', async () => {
    reads++
    throw Error('offline')
  })
  await assert.rejects(resources.load('state'))
  assert.equal(resources.isDirty('state'), true)
  t.mock.timers.tick(60000)
  await settle()
  assert.equal(reads, 1)
})

test('first-screen endings wait for conversation identity without duplicating its read', async () => {
  setActivePinia(createPinia())
  const api = await server.ssrLoadModule('/src/api/index.ts')
  const { useConversationStore } = await server.ssrLoadModule('/src/stores/conversations.ts')
  const resources = useConversationResources()
  resources.enter('a')
  const conversation = deferred()
  let conversationReads = 0,
    endingReads = 0
  api.conversationsApi.get = async () => {
    conversationReads++
    return conversation.promise
  }
  api.moduleActionsApi.endings = async () => {
    endingReads++
    return { conversation_id: 'a', branch_epoch: 0, ending_capabilities: [] }
  }
  const requests = resources.refresh(['conversation', 'endings'])
  await settle()
  assert.equal(endingReads, 0)
  conversation.resolve({ conversation: { id: 'a', branch_epoch: 0 } })
  await requests
  assert.equal(conversationReads, 1)
  assert.equal(endingReads, 1)
  assert.equal(useConversationStore().endingState?.conversation_id, 'a')
})
