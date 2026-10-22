import assert from 'node:assert/strict'
import fs from 'node:fs'
import test from 'node:test'
import { parse } from 'vue/compiler-sfc'
import ts from 'typescript'
import { computed, effectScope, nextTick, ref, watch } from 'vue'
import {
  confirmsPendingExchange,
  maxExchangeUnits,
  validExchangeRequestID,
} from '../src/lib/rewardExchangeState.ts'

const requestA = '11111111-1111-4111-8111-111111111111'
const requestB = '22222222-2222-4222-8222-222222222222'

test('looking up an old order cannot resolve another pending exchange', () => {
  const pending = { request_id: requestA, offer_id: 'offer', revision: 3, units: 2 }
  const historical = { request_id: requestB }
  assert.equal(confirmsPendingExchange(pending, historical), false)
  assert.equal(pending.request_id, requestA)
  assert.equal(
    confirmsPendingExchange(pending, {
      request_id: requestA,
      offer_id: 'offer',
      offer_revision: 3,
      units: 2,
    }),
    true,
  )
  assert.equal(
    confirmsPendingExchange(pending, {
      request_id: requestA,
      offer_id: 'offer',
      offer_revision: 4,
      units: 2,
    }),
    false,
  )
  assert.equal(confirmsPendingExchange(undefined, historical), false)
  assert.equal(validExchangeRequestID(' ' + requestA + ' '), true)
  for (const invalid of ['', '00000000-0000-0000-0000-000000000000', '../orders', 'not-an-id']) {
    assert.equal(validExchangeRequestID(invalid), false)
  }
})

test('exchange quantity respects balance and quota but preserves pending retries', () => {
  const offer = { cost_quantity: 25, reward_quantity: 2, remaining_units: 5 }
  assert.equal(maxExchangeUnits(offer, 74, false), 2)
  assert.equal(maxExchangeUnits(offer, 1000, false), 5)
  assert.equal(maxExchangeUnits({ ...offer, remaining_units: null }, 49, false), 1)
  assert.equal(maxExchangeUnits(offer, 0, true), Math.floor(Number.MAX_SAFE_INTEGER / 25))
  assert.ok(maxExchangeUnits({ ...offer, remaining_units: 0 }, 0, true) >= 1)
  assert.equal(maxExchangeUnits({ ...offer, remaining_units: 0 }, 1000, false), 0)
})

function exchangePanel(t, api, saved) {
  const { descriptor } = parse(
    fs.readFileSync(new URL('../src/components/RewardExchangePanel.vue', import.meta.url), 'utf8'),
  )
  const script = ts
    .transpileModule(descriptor.scriptSetup.content, {
      compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext },
    })
    .outputText.replace(/^import .* from .*;\r?\n/gm, '')
  const mounted = []
  const dependencies = {
    computed,
    ref,
    watch,
    confirmsPendingExchange,
    maxExchangeUnits,
    onMounted: (fn) => mounted.push(fn),
    defineProps: () => ({ active: false }),
    defineEmits: () => () => {},
    defineExpose: () => {},
    useMessage: () => ({ success() {} }),
    useRewardStore: () => ({ balanceFor: () => 1000 }),
    useAuthStore: () => ({ user: { id: 'player' } }),
    POINTS: 'points',
    rewardError: (err) => err.message || 'request failed',
    sessionStorage: { getItem: () => JSON.stringify(saved), setItem() {}, removeItem() {} },
    api,
  }
  const scope = effectScope()
  t.after(() => scope.stop())
  return scope.run(() => {
    const panel = new Function(
      ...Object.keys(dependencies),
      script +
        '\nreturn { load, choose, submit, acceptConfirmedOrder, selected, pending, valid, error, offers };',
    )(...Object.values(dependencies))
    mounted.forEach((fn) => fn())
    return panel
  })
}

const savedOffer = {
  id: 'offer',
  revision: 3,
  cost_quantity: 100,
  reward_quantity: 1,
  max_redemptions_per_user: 2,
  redeemed_units: 0,
  remaining_units: 2,
}
const savedRequest = { request_id: requestA, offer_id: 'offer', revision: 3, units: 2 }
const confirmed = { ...savedRequest, offer_revision: 3, id: 'order' }

test('pending exchange survives refreshed quota, changed price and an unpublished offer', async (t) => {
  let catalogue = [{ ...savedOffer, revision: 4, cost_quantity: 200, remaining_units: 0 }]
  const calls = []
  const panel = exchangePanel(
    t,
    {
      offers: async () => catalogue,
      exchange: async (request) => {
        calls.push({ ...request })
        return confirmed
      },
    },
    { request: savedRequest, offer: savedOffer },
  )
  await panel.load()
  assert.equal(panel.selected.value.revision, 3)
  assert.equal(panel.valid.value, true)
  catalogue = []
  await panel.load()
  assert.equal(panel.selected.value.id, 'offer')
  await panel.submit()
  assert.deepEqual(calls, [savedRequest])
  assert.equal(panel.pending.value, undefined)
})

test('confirming an order re-reads quota after any older catalogue read', async (t) => {
  let resolveFirst
  let reads = 0
  const panel = exchangePanel(
    t,
    {
      offers: () => {
        reads++
        return reads === 1
          ? new Promise((resolve) => {
              resolveFirst = resolve
            })
          : Promise.resolve([{ ...savedOffer, remaining_units: 0 }])
      },
    },
    { request: savedRequest, offer: savedOffer },
  )
  const initial = panel.load()
  panel.acceptConfirmedOrder(confirmed)
  resolveFirst([savedOffer])
  await initial
  await panel.load()
  await nextTick()
  assert.equal(reads, 2)
  assert.equal(panel.offers.value[0].remaining_units, 0)
})

test('failed quota refresh reports failure instead of claiming success', async (t) => {
  const panel = exchangePanel(
    t,
    {
      exchange: async () => {
        throw { response: { status: 409, data: { code: 'exchange_limit_reached' } } }
      },
      request: async () => {
        throw new Error('not found')
      },
      offers: async () => {
        throw new Error('offline')
      },
    },
    null,
  )
  panel.choose(savedOffer)
  await panel.submit()
  assert.equal(panel.pending.value, undefined)
  assert.match(panel.error.value, /刷新失败.*offline/)
})

function template(file) {
  const { descriptor, errors } = parse(fs.readFileSync(new URL(file, import.meta.url), 'utf8'))
  assert.deepEqual(errors, [])
  return descriptor.template.ast
}
function find(node, tag) {
  return [
    ...(node.tag === tag ? [node] : []),
    ...(node.children || []).flatMap((child) => find(child, tag)),
  ]
}
function attribute(node, name) {
  return node.props.find((p) => p.type === 6 && p.name === name)?.value?.content
}

test('admin and player exchange surfaces expose per-user limits', () => {
  const admin = template('../src/components/AdminRewardExchangePanel.vue')
  assert.equal(
    find(admin, 'NRadioGroup').length,
    1,
    'admin must offer an explicit unlimited choice',
  )
  assert.ok(
    find(admin, 'NInputNumber').some((input) =>
      input.props.some(
        (prop) =>
          prop.type === 7 &&
          prop.name === 'model' &&
          prop.arg?.content === 'value' &&
          prop.exp?.content === 'form.max_redemptions_per_user',
      ),
    ),
    'admin must edit the per-user limit',
  )
  const player = template('../src/components/RewardExchangePanel.vue')
  assert.ok(
    find(player, 'NButton').some((button) =>
      button.props.some(
        (prop) =>
          prop.type === 7 &&
          prop.name === 'bind' &&
          prop.arg?.content === 'disabled' &&
          prop.exp?.content === 'exhausted(offer)',
      ),
    ),
    'player purchase must disable at the limit',
  )
})

test('catalogue, history, and lookup have separate homes while pending state stays mounted', () => {
  const view = template('../src/views/RewardsView.vue')
  const panes = find(view, 'NTabPane')
  assert.deepEqual(
    panes.map((p) => attribute(p, 'name')),
    ['exchange', 'orders', 'ledger'],
  )
  const exchange = panes.find((p) => attribute(p, 'name') === 'exchange')
  assert.equal(
    attribute(exchange, 'display-directive'),
    'show',
    'restore pending exchanges even before visiting the catalogue',
  )
  const orders = panes.find((p) => attribute(p, 'name') === 'orders')
  assert.equal(find(orders, 'RewardExchangeOrders').length, 1)
  const catalogue = template('../src/components/RewardExchangePanel.vue')
  assert.equal(find(catalogue, 'RewardExchangeOrders').length, 0)
  assert.equal(find(catalogue, 'NInput').length, 0, 'request lookup must not sit beneath products')
  assert.ok(
    find(catalogue, 'NButton').some((button) =>
      button.props.some(
        (prop) =>
          prop.type === 7 &&
          prop.name === 'on' &&
          prop.arg?.content === 'click' &&
          prop.exp?.content === 'viewOrders',
      ),
    ),
    'successful exchange links to its history tab',
  )
  const lookup = find(template('../src/components/RewardExchangeOrders.vue'), 'NModal').find(
    (modal) => attribute(modal, 'title') === '查询兑换结果',
  )
  assert.ok(lookup)
  assert.equal(find(lookup, 'NInput').length, 1)
})
