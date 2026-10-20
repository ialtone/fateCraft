import test, { after, before } from 'node:test'
import assert from 'node:assert/strict'
import { createServer } from 'vite'
import { fileURLToPath } from 'node:url'

let server, collectEndingEntries
before(async () => {
  server = await createServer({
    root: fileURLToPath(new URL('..', import.meta.url)),
    optimizeDeps: { noDiscovery: true, include: [] },
    server: { middlewareMode: true, watch: null, hmr: false, ws: false },
  })
  ;({ collectEndingEntries } = await server.ssrLoadModule('/src/lib/ending-actions.ts'))
})
after(async () => {
  await server?.close()
})

function capability(key, extra = {}) {
  return {
    key,
    description: key,
    available: true,
    revision: 3,
    metadata: { label: key, minutes: 0 },
    ...extra,
  }
}

test('generic ending bar exposes finish when presentation has no action binding', () => {
  const entries = collectEndingEntries(
    [capability('management.finish'), capability('management.close')],
    'management.close',
    new Set(),
  )
  assert.deepEqual(
    entries.map((entry) => entry.capability.key),
    ['management.finish'],
  )
})

test('generic ending bar does not duplicate rendered actions or unavailable endings', () => {
  const entries = collectEndingEntries(
    [
      capability('sect.finish'),
      capability('sect.dissolve'),
      capability('relationship.conclude', { available: false }),
    ],
    'sect.dissolve',
    new Set(['sect.finish']),
  )
  assert.deepEqual(entries, [])
})

test('ending plans become one button per plan', () => {
  const entries = collectEndingEntries(
    [
      capability('quest.submit', {
        plans: [
          { label: '守界', arguments: { quest_key: 'warden-ending' }, minutes: 0 },
          { label: '求道', arguments: { quest_key: 'seeker-ending' }, minutes: 0 },
        ],
      }),
    ],
    'campaign.abandon',
    new Set(),
  )
  assert.deepEqual(
    entries.map((entry) => [entry.label, entry.arguments.quest_key]),
    [
      ['守界', 'warden-ending'],
      ['求道', 'seeker-ending'],
    ],
  )
})

test('plan-based ending stays reachable even when the generic action key is rendered', () => {
  const entries = collectEndingEntries(
    [
      capability('quest.submit', {
        plans: [{ label: '守界', arguments: { quest_key: 'warden-ending' }, minutes: 0 }],
      }),
    ],
    'campaign.abandon',
    new Set(['quest.submit']),
  )
  assert.equal(entries.length, 1)
  assert.equal(entries[0].label, '守界')
})
