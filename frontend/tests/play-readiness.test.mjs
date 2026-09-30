import assert from 'node:assert/strict'
import test from 'node:test'
import { playConfigurationIssue } from '../src/lib/playReadiness.ts'

const configured = {
  base_url: 'https://model.example/v1',
  model: 'model',
  has_api_key: true,
  configuration_ready: true,
  access_mode: 'user_managed',
  llm_route: 'server',
  client_route_enabled: true,
}

test('personal server requires saved API key, API address and model', () => {
  assert.equal(playConfigurationIssue(configured, false), '')
  for (const missing of [{ has_api_key: false }, { base_url: '  ' }, { model: '' }]) {
    assert.notEqual(playConfigurationIssue({ ...configured, ...missing }, true), '')
  }
})

test('client requires this device key even if the server used to have a key', () => {
  const client = { ...configured, llm_route: 'client' }
  assert.match(playConfigurationIssue(client, false), /本设备尚未保存 API Key/)
  assert.equal(playConfigurationIssue({ ...client, has_api_key: false }, true), '')
  assert.notEqual(playConfigurationIssue({ ...client, model: '' }, true), '')
})

test('platform readiness uses platform config, not personal config', () => {
  const platform = {
    ...configured,
    access_mode: 'platform_managed',
    has_api_key: false,
    base_url: '',
    model: '',
  }
  assert.equal(playConfigurationIssue(platform, false), '')
  assert.match(
    playConfigurationIssue({ ...platform, configuration_ready: false }, true),
    /联系管理员/,
  )
  assert.notEqual(playConfigurationIssue({ ...platform, llm_route: 'client' }, true), '')
})

test('closed client route cannot use a local key to bypass server requirements', () => {
  assert.notEqual(
    playConfigurationIssue(
      { ...configured, llm_route: 'client', client_route_enabled: false, has_api_key: false },
      true,
    ),
    '',
  )
})

test('failed settings load or incomplete configuration blocks starting', () => {
  assert.notEqual(playConfigurationIssue(null, true), '')
  assert.notEqual(playConfigurationIssue({ ...configured, configuration_ready: false }, true), '')
})
