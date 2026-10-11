import test, { after, before } from 'node:test'
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { fileURLToPath } from 'node:url'
import { createPinia, setActivePinia } from 'pinia'

let server, api, useCampaignStore, useConversationStore, useChatStore, useRewardStore
before(async () => {
  server = await createServer({
    root: fileURLToPath(new URL('..', import.meta.url)),
    optimizeDeps: { noDiscovery: true, include: [] },
    server: { middlewareMode: true, watch: null, hmr: false },
  })
  api = await server.ssrLoadModule('/src/api/index.ts')
  ;({ useCampaignStore } = await server.ssrLoadModule('/src/stores/campaign.ts'))
  ;({ useConversationStore } = await server.ssrLoadModule('/src/stores/conversations.ts'))
  ;({ useChatStore } = await server.ssrLoadModule('/src/stores/chat.ts'))
  ;({ useRewardStore } = await server.ssrLoadModule('/src/stores/rewards.ts'))
})
after(async () => {
  await server?.close()
})

function deferred() {
  let resolve, reject
  const promise = new Promise((yes, no) => {
    resolve = yes
    reject = no
  })
  return { promise, resolve, reject }
}
function snapshot(id = 'world', revision = 1, status = 'active') {
  return { campaign: { id, revision, status } }
}
async function setup() {
  setActivePinia(createPinia())
  const cache = new Map()
  globalThis.sessionStorage = {
    getItem: (key) => cache.get(key) ?? null,
    setItem: (key, value) => cache.set(key, value),
    removeItem: (key) => cache.delete(key),
  }
  api.campaignsApi.snapshot = async (id) => snapshot(id)
  api.messagesApi.list = async () => []
  api.conversationsApi.get = async (id) => ({
    conversation: { id, campaign_id: 'world', status: 'ended' },
  })
  api.rewardsApi.list = async () => [{ asset: { key: 'points' }, balance: 1000 }]
  const conversations = useConversationStore()
  conversations.current = { id: 'conversation', campaign_id: 'world', status: 'active' }
  const campaign = useCampaignStore()
  await campaign.load('world')
  return { campaign, conversations, chat: useChatStore(), rewards: useRewardStore(), cache }
}

test('completion updates conversation and balances even when a snapshot refresh fails', async () => {
  const { campaign, conversations, rewards } = await setup()
  api.campaignsApi.action = async () => snapshot('world', 2, 'completed')
  api.campaignsApi.snapshot = async () => {
    throw new Error('refresh unavailable')
  }
  const result = await campaign.execute('quest.submit', { quest_key: 'ending' })
  assert.equal(result.campaign.status, 'completed')
  assert.equal(conversations.current.status, 'ended')
  assert.equal(campaign.snapshot.campaign.revision, 2)
  assert.equal(rewards.balanceFor('points'), 1000)
  assert.equal(campaign.pending, null)
  assert.match(campaign.error, /操作已完成/)
})

test('an ambiguous request survives reopening and retries the exact original action', async () => {
  const { campaign } = await setup()
  const received = []
  api.campaignsApi.action = async (_id, input) => {
    received.push(JSON.parse(JSON.stringify(input)))
    if (received.length === 1) throw new Error('connection lost after commit')
    return snapshot('world', 2)
  }
  await assert.rejects(campaign.execute('cultivation.practice', { sessions: 3 }))
  assert.ok(campaign.pending)
  assert.equal(await campaign.execute('inventory.use', {}), null)
  campaign.reset()
  await campaign.load('world')
  assert.ok(campaign.pending)
  await campaign.retry()
  assert.deepEqual(received[1], received[0])
  assert.equal(campaign.pending, null)
})

test('leaving and reopening the same conversation ignores the old mutation response', async () => {
  const { campaign, conversations, chat } = await setup()
  const request = deferred()
  api.campaignsApi.action = () => request.promise
  const pending = campaign.execute('quest.submit', { quest_key: 'ending' })
  campaign.reset()
  await campaign.load('world')
  request.resolve({ ...snapshot('world', 2, 'completed'), roll_message: { id: 'old-roll' } })
  await pending
  assert.equal(campaign.snapshot.campaign.revision, 1)
  assert.equal(conversations.current.status, 'active')
  assert.equal(chat.messages.length, 0)
})

test('a late snapshot cannot replace a different campaign or a newer revision', async () => {
  const { campaign } = await setup()
  const request = deferred()
  api.campaignsApi.snapshot = (id) =>
    id === 'world' ? request.promise : Promise.resolve(snapshot(id, 8))
  const stale = campaign.load('world')
  await campaign.load('other-world')
  request.resolve(snapshot('world', 10))
  await stale
  assert.equal(campaign.snapshot.campaign.id, 'other-world')
  campaign.applyCampaign({ id: 'other-world', revision: 7, status: 'active' })
  assert.equal(campaign.snapshot.campaign.revision, 8)
})
