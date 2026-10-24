import test from 'node:test'
import assert from 'node:assert/strict'
import { groupTimeline, isActionReceipt } from '../src/lib/receipt-groups.ts'

function message(id, type, metadata = {}) {
  return {
    id,
    conversation_id: 'conv',
    type,
    markdown: '',
    metadata,
    created_at: '2026-01-01T00:00:00Z',
  }
}

test('system messages without module_action metadata are not receipts', () => {
  assert.equal(isActionReceipt(message('a', 'system')), false)
  assert.equal(isActionReceipt(message('a', 'system', { other: true })), false)
  assert.equal(isActionReceipt(message('a', 'system', { module_action: 'trial.practice' })), true)
  assert.equal(isActionReceipt(message('a', 'assistant', { module_action: 'trial.practice' })), false)
})

test('consecutive module action receipts collapse into one group', () => {
  const groups = groupTimeline([
    message('n1', 'assistant'),
    message('r1', 'system', { module_action: 'trial.practice' }),
    message('r2', 'system', { module_action: 'trial.practice' }),
    message('r3', 'system', { module_action: 'trial.buy' }),
    message('n2', 'assistant'),
    message('r4', 'system', { module_action: 'trial.rest' }),
  ])
  assert.equal(groups.length, 4)
  assert.equal(groups[0].message?.id, 'n1')
  assert.equal(groups[1].receipts?.length, 3)
  assert.equal(groups[1].key, 'receipts-r1')
  assert.equal(groups[2].message?.id, 'n2')
  assert.equal(groups[3].receipts?.length, 1)
})

test('non-consecutive receipts stay in separate groups', () => {
  const groups = groupTimeline([
    message('r1', 'system', { module_action: 'trial.practice' }),
    message('n1', 'assistant'),
    message('r2', 'system', { module_action: 'trial.buy' }),
  ])
  assert.equal(groups.length, 3)
  assert.equal(groups[0].receipts?.length, 1)
  assert.equal(groups[2].receipts?.length, 1)
  assert.notEqual(groups[0].key, groups[2].key)
})

test('an empty timeline produces no groups', () => {
  assert.deepEqual(groupTimeline([]), [])
})
