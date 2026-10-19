import assert from 'node:assert/strict'
import fs from 'node:fs'
import test from 'node:test'
import { parse as parseSFC } from 'vue/compiler-sfc'
import {
  applySavedSettings,
  pickSettings,
  systemSettingFields,
  authSettingFields,
} from '../src/lib/adminSettingGroups.ts'

test('saving one settings tab excludes hidden drafts and credentials', () => {
  const draft = {
    roll_system_enabled: false,
    roll_difficulty_mode: 'hard',
    detailed_llm_logging_scope: 'user',
    detailed_llm_logging_target_id: null, // An incomplete, unsaved log target.
    public_llm_model: 'unsaved-model',
    public_llm_api_key: 'unsaved-secret',
    updated_at: 'before',
  }
  assert.deepEqual(pickSettings(draft, systemSettingFields.gameplay), {
    roll_system_enabled: false,
    roll_difficulty_mode: 'hard',
  })
  applySavedSettings(
    draft,
    {
      ...draft,
      public_llm_model: 'old-server-model',
      detailed_llm_logging_scope: 'all',
      updated_at: 'after',
    },
    systemSettingFields.gameplay,
  )
  assert.equal(draft.public_llm_model, 'unsaved-model')
  assert.equal(draft.detailed_llm_logging_scope, 'user')
  assert.equal(draft.public_llm_api_key, 'unsaved-secret')
  assert.equal(draft.updated_at, 'after')
  const login = pickSettings(
    { email_auth_enabled: false, smtp_password: 'unsaved-secret', smtp_host: 'draft-host' },
    authSettingFields.login,
  )
  assert.equal(login.email_auth_enabled, false)
  assert.ok(!('smtp_password' in login))
  assert.ok(!('smtp_host' in login))
})

function tabGroups(file) {
  const { descriptor, errors } = parseSFC(fs.readFileSync(new URL(file, import.meta.url), 'utf8'))
  assert.deepEqual(errors, [])
  const groups = []
  const attribute = (node, name) =>
    node.props?.find((p) => p.type === 6 && p.name === name)?.value?.content
  function walk(node, owners = []) {
    if (node.tag === 'NTabs') {
      const panes = node.children.filter((child) => child.tag === 'NTabPane')
      const names = panes.map((pane) => attribute(pane, 'name'))
      assert.equal(new Set(names).size, names.length, 'duplicate pane names')
      groups.push({ owners, names, panes })
    }
    for (const child of node.children || [])
      walk(child, node.tag === 'NTabPane' ? [...owners, attribute(node, 'name')] : owners)
  }
  walk(descriptor.template.ast)
  return groups
}

test('sub-tabs belong to their management area and retain mounted drafts', () => {
  const groups = tabGroups('../src/views/AdminView.vue')
  for (const [owner, names] of Object.entries({
    settings: ['gameplay', 'llm'],
    auth: ['login', 'smtp', 'verification', 'invitations'],
    challenges: ['catalog', 'versions', 'upload', 'imports', 'editor'],
    sharing: ['shares', 'reports'],
  })) {
    const group = groups.find((g) => g.owners.at(-1) === owner)
    assert.ok(group, `${owner} has its own tabs`)
    assert.deepEqual(group.names, names)
    for (const pane of group.panes)
      assert.ok(
        pane.props.some((p) => p.name === 'display-directive' && p.value.content === 'show:lazy'),
      )
  }
  assert.ok(
    !groups[0].names.includes('editor'),
    'version editor must not leak into other admin areas',
  )
})

test('diagnostics tabs include the log policy', () => {
  assert.deepEqual(tabGroups('../src/components/AdminDiagnostics.vue')[0].names, [
    'llm',
    'context',
    'audit',
    'status',
    'policy',
  ])
})

test('reward, user, and challenge panels separate independent workflows', () => {
  assert.deepEqual(tabGroups('../src/components/AdminRewardsPanel.vue')[0].names, [
    'rules',
    'exchange',
    'records',
  ])
  assert.deepEqual(tabGroups('../src/components/AdminUsersPanel.vue')[0].names, ['users', 'audit'])
  assert.deepEqual(tabGroups('../src/components/AdminRewardUserPanel.vue')[0].names, [
    'grant',
    'ledger',
  ])
  const editor = tabGroups('../src/components/ChallengeVersionEditor.vue')[0].names
  assert.ok(editor.includes('runtime') && editor.includes('state'))
})
