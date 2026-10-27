import assert from 'node:assert/strict'
import fs from 'node:fs'
import test from 'node:test'
import { parse as parseSFC } from 'vue/compiler-sfc'
import { canChangeStatus, registrationLabel, roleLabel } from '../src/lib/adminUsers.ts'

const template = (file) =>
  parseSFC(fs.readFileSync(new URL(file, import.meta.url), 'utf8')).descriptor.template.content

test('only an owner can change status, and never an owner account', () => {
  assert.equal(canChangeStatus(true, { role: 'user' }), true)
  assert.equal(canChangeStatus(true, { role: 'admin' }), true)
  assert.equal(canChangeStatus(true, { role: 'owner' }), false)
  assert.equal(canChangeStatus(false, { role: 'user' }), false)
})

test('labels cover registration methods and roles', () => {
  assert.equal(registrationLabel('email'), '邮箱注册')
  assert.equal(registrationLabel('linuxdo'), 'LinuxDo')
  assert.equal(registrationLabel(undefined), '—')
  assert.equal(roleLabel('owner'), '拥有者')
})

test('admin user views identify users by user ID, never by UUID', () => {
  for (const file of [
    '../src/components/AdminUserDetailModal.vue',
    '../src/components/AdminUsersPanel.vue',
    '../src/components/AdminDiagnostics.vue',
  ]) {
    const html = template(file)
    for (const [, expr] of html.matchAll(/\{\{([^}]*)\}\}/g)) {
      assert.doesNotMatch(
        expr,
        /\b(user|row|actor|target)\??\.id\b|actor_user_id|target_user_id|user_id/,
        `${file}: ${expr}`,
      )
    }
  }
})

test('conversation records tab is only rendered when logging covers the user', () => {
  const html = template('../src/components/AdminUserDetailModal.vue')
  assert.match(html, /<NTabPane\s+v-if="covered"\s+name="records"/)
})
