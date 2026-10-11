import test from 'node:test'
import assert from 'node:assert/strict'
import { createActionScope, clearMatchingAction } from '../src/lib/action-scope.ts'
import { outcomeRewardBudgets } from '../src/lib/reward-budget.ts'

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
