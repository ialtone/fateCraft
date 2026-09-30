import assert from 'node:assert/strict'
import fs from 'node:fs'
import test from 'node:test'
import { parse } from 'vue/compiler-sfc'
import { confirmsPendingExchange, validExchangeRequestID } from '../src/lib/rewardExchangeState.ts'

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
