import test from 'node:test'
import assert from 'node:assert/strict'
import { createActionScope, clearMatchingAction } from '../src/lib/action-scope.ts'
import { outcomeRewardBudgets } from '../src/lib/reward-budget.ts'
import { actionBlock } from '../src/lib/action-gate.ts'
import {
  campaignFallbackGuidance,
  withoutEntityBoundActions,
} from '../src/lib/campaign-guidance.ts'

test('action gate exposes one reason with stable priority', () => {
  assert.equal(actionBlock({}), null)
  assert.equal(actionBlock({ generating: true, syncing: true }).code, 'generating')
  assert.equal(actionBlock({ resultPending: true, generating: true }).code, 'result_pending')
  assert.equal(actionBlock({ ended: true, resultPending: true }).code, 'ended')
  assert.equal(actionBlock({ connectionBlocked: true, generating: true }).code, 'syncing')
  assert.match(actionBlock({ connectionBlocked: true }).message, /连接/)
  assert.equal(actionBlock({ syncing: true, conflict: true }).code, 'syncing')
  assert.equal(actionBlock({ conflict: true }).code, 'conflict')
})

test('campaign fallback guidance derives message intents from the public snapshot', () => {
  const snapshot = {
    encounters: [],
    quests: [
      { key: 'q1', name: '寻找失踪药农', quest_type: 'task', can_submit: true },
      { key: 'q2', name: '守夜', quest_type: 'obligation', can_accept: true },
    ],
    world: {
      current: { key: 'village' },
      available_routes: [{ location_key: 'market', name: '青石集', accessible: true }],
    },
    relationships: [
      { instance_key: 'npc1', name: '老邢', status: 'active', location_key: 'village' },
      { instance_key: 'npc2', name: '远方客', status: 'active', location_key: 'market' },
    ],
    training: [{ key: 'retreat', name: '筑基静修', available: true }],
  }
  const items = campaignFallbackGuidance(snapshot)
  assert.ok(items.length > 0 && items.length <= 4)
  assert.ok(items.some((item) => item.label === '提交委托 · 寻找失踪药农'))
  assert.ok(items.some((item) => item.label === '接受承诺 · 守夜'))
  assert.ok(items.some((item) => item.label === '与老邢交谈'))
  assert.ok(!items.some((item) => item.label.includes('远方客')))
  assert.ok(items.every((item) => item.message.length > 0))
  assert.deepEqual(campaignFallbackGuidance(null), [])
})

test('entity-bound campaign actions are not rendered twice in the action panel', () => {
  const capabilities = [
    { key: 'quest.accept' },
    { key: 'cultivation.practice' },
    { key: 'world.investigate' },
  ]
  const actions = [
    { key: 'quest.accept', source_path: 'quests' },
    { key: 'cultivation.practice', source_path: 'techniques' },
    { key: 'world.investigate' },
  ]
  assert.deepEqual(
    withoutEntityBoundActions(capabilities, actions).map((capability) => capability.key),
    ['world.investigate'],
  )
  assert.deepEqual(withoutEntityBoundActions(capabilities, []), capabilities)
})

test('module-described reward budget includes stages exactly once for each ending', () => {
  const schemas = {
    example: {
      'x-reward-budget': {
        stages: 'stages',
        points: 'points',
        outcomes: 'endings',
        total: 'budget',
      },
    },
  }
  const definitions = [
    {
      kind: 'example',
      content: {
        stages: [{ points: 100 }, { points: 100 }, { points: 100 }],
        endings: ['a', 'b'],
        budget: 1000,
      },
    },
  ]
  assert.equal(outcomeRewardBudgets(definitions, schemas, 'a', 700)[0].valid, true)
  assert.equal(outcomeRewardBudgets(definitions, schemas, 'b', 1000)[0].valid, false)
  assert.deepEqual(outcomeRewardBudgets(definitions, schemas, 'death', 0), [])
  assert.deepEqual(outcomeRewardBudgets(definitions, {}, 'a', 700), [])
  definitions[0].content.stages[0].points = -1
  assert.equal(outcomeRewardBudgets(definitions, schemas, 'a', 700)[0].valid, false)
})

test('single-run stage budget uses a cap instead of a fixed total', () => {
  const schemas = {
    challenge: {
      'x-reward-budget': {
        stages: 'stages',
        points: 'points',
        outcomes: 'outcomes',
        cap: 'stage_cap',
      },
    },
  }
  const definitions = [
    {
      kind: 'challenge',
      content: {
        stages: [{ points: 25 }, { points: 25 }, { points: 25 }],
        outcomes: ['ascend'],
        stage_cap: 75,
      },
    },
  ]
  const [budget] = outcomeRewardBudgets(definitions, schemas, 'ascend', 150)
  assert.equal(budget.valid, true)
  assert.match(budget.text, /阶段 75 积分/)
  assert.match(budget.text, /终局 150 积分单独结算/)

  // The cap is not derived from the terminal payout at runtime; it is a fixed
  // single-run ceiling, so every success route validates against the same cap.
  assert.equal(outcomeRewardBudgets(definitions, schemas, 'ascend', 700)[0].valid, true)
  assert.deepEqual(outcomeRewardBudgets(definitions, schemas, 'other', 150), [])

  definitions[0].content.stage_cap = 50
  assert.equal(outcomeRewardBudgets(definitions, schemas, 'ascend', 150)[0].valid, false)

  // Malformed or missing configuration must be reported, never treated as valid.
  definitions[0].content.stage_cap = '75'
  assert.equal(outcomeRewardBudgets(definitions, schemas, 'ascend', 150)[0].valid, false)
  definitions[0].content.stages = 'nope'
  assert.deepEqual(outcomeRewardBudgets(definitions, schemas, 'ascend', 150), [
    { text: '阶段积分配置不完整', valid: false },
  ])
})

test('an old result cannot update a reopened conversation with the same ID', () => {
  const scope = createActionScope()
  scope.enter('first')
  const old = scope.capture()
  scope.enter('second')
  scope.enter('first')
  assert.equal(scope.isCurrent(old), false)
  const current = scope.capture()
  assert.equal(scope.isCurrent(current), true)
  scope.dispose()
  assert.equal(scope.isCurrent(current), false)
})

test('a late response clears only its own persisted request', () => {
  const data = new Map([['fatecraft:action:first', JSON.stringify({ request_id: 'new' })]])
  const storage = { getItem: (key) => data.get(key) ?? null, removeItem: (key) => data.delete(key) }
  clearMatchingAction(storage, 'first', 'old')
  assert.equal(JSON.parse(data.get('fatecraft:action:first')).request_id, 'new')
  clearMatchingAction(storage, 'first', 'new')
  assert.equal(data.size, 0)
})

test('disabled or malformed session storage does not turn a committed action into an error', () => {
  assert.doesNotThrow(() =>
    clearMatchingAction(
      {
        getItem() {
          throw Error('disabled')
        },
        removeItem() {},
      },
      'first',
      'id',
    ),
  )
  assert.doesNotThrow(() =>
    clearMatchingAction(
      {
        getItem() {
          return '{'
        },
        removeItem() {
          throw Error('must not run')
        },
      },
      'first',
      'id',
    ),
  )
})
