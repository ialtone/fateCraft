import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { ref, computed } from 'vue'
import ts from 'typescript'
import { parse } from 'vue/compiler-sfc'

function panel(api = {}) {
  const { descriptor } = parse(
    readFileSync(new URL('../src/components/AdminLLMProxyPanel.vue', import.meta.url), 'utf8'),
  )
  const source = descriptor.scriptSetup.content.replace(
    /^import[\s\S]*?from\s+['"][^'"]+['"]\s*$/gm,
    '',
  )
  let unmount
  const errors = []
  const compiled = ts.transpile(source, { target: ts.ScriptTarget.ESNext })
  const state = new Function(
    'ref',
    'computed',
    'onMounted',
    'onBeforeUnmount',
    'useMessage',
    'adminApi',
    `${compiled}; return { load, save, test, apply, toggleClearPassword, settings, loaded, loadError, password, clearPassword, proxyURL, dirty, testResult, busy, testSummary }`,
  )(
    ref,
    computed,
    () => {},
    (fn) => {
      unmount = fn
    },
    () => ({
      error: (msg) => errors.push(msg),
      warning: (msg) => errors.push(msg),
      success: () => {},
    }),
    api,
  )
  return { ...state, unmount: () => unmount(), errors }
}

const saved = {
  enabled: true,
  url: 'http://proxy.internal:3128',
  username: 'alice',
  has_password: true,
  ca_pem: '',
  revision: 3,
}
function deferred() {
  let resolve
  const promise = new Promise((r) => {
    resolve = r
  })
  return { promise, resolve }
}

test('proxy panel can retry an initial read failure', async () => {
  let attempts = 0
  const p = panel({
    getLLMProxy: async () => {
      if (++attempts === 1) throw new Error('offline')
      return saved
    },
  })
  await p.load()
  assert.equal(p.loaded.value, false)
  assert.equal(p.loadError.value, 'offline')
  await p.load()
  assert.equal(p.loaded.value, true)
  assert.equal(p.loadError.value, '')
  assert.equal(p.dirty.value, false)
})

test('proxy saves preserve omitted passwords, support undoing clear, and explicitly replace or clear', async () => {
  const bodies = []
  const p = panel({
    updateLLMProxy: async (body) => {
      bodies.push(body)
      return { ...saved, revision: body.expected_revision + 1 }
    },
  })
  p.apply(saved)
  p.toggleClearPassword()
  assert.equal(p.clearPassword.value, true)
  p.toggleClearPassword()
  assert.equal(p.dirty.value, false)
  await p.save()
  assert.equal(bodies[0].expected_revision, 3)
  assert.ok(!('password' in bodies[0]))
  assert.ok(!('clear_password' in bodies[0]))
  p.password.value = 'replacement'
  await p.save()
  assert.equal(bodies[1].password, 'replacement')
  assert.equal(p.password.value, '')
  p.toggleClearPassword()
  await p.save()
  assert.equal(bodies[2].clear_password, true)
  assert.ok(!('password' in bodies[2]))
})

test('pending save blocks overlapping reads, saves and checks', async () => {
  const pending = deferred()
  let saves = 0,
    reads = 0,
    tests = 0
  const p = panel({
    updateLLMProxy: () => {
      saves++
      return pending.promise
    },
    getLLMProxy: async () => {
      reads++
      return saved
    },
    testLLMProxy: async () => {
      tests++
      return {}
    },
  })
  p.apply(saved)
  const saving = p.save()
  await Promise.all([p.load(), p.save(), p.test()])
  assert.deepEqual([saves, reads, tests], [1, 0, 0])
  pending.resolve(saved)
  await saving
  assert.equal(p.busy.value, false)
})

test('tests require saved drafts and ignore results after panel unmount', async () => {
  const pending = deferred()
  let checks = 0
  const p = panel({
    testLLMProxy: () => {
      checks++
      return pending.promise
    },
  })
  p.apply(saved)
  p.proxyURL.value = 'http://unsaved:3128'
  await p.test()
  assert.equal(checks, 0)
  p.apply(saved)
  const checking = p.test()
  p.unmount()
  pending.resolve({
    ok: true,
    checked_at: '2026-10-21T00:00:00Z',
    elapsed_ms: 5,
    revision: 3,
    stale: false,
  })
  await checking
  assert.equal(checks, 1)
  assert.equal(p.testResult.value, null)
})
