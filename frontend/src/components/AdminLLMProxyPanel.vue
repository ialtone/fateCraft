<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref } from 'vue'
import {
  NAlert,
  NButton,
  NForm,
  NFormItem,
  NInput,
  NSpin,
  NSwitch,
  NTag,
  useMessage,
} from 'naive-ui'
import { adminApi } from '@/api'
import type { AdminLLMProxySettings, AdminLLMProxyTestResult } from '@/types'

const message = useMessage()
const loading = ref(false)
const saving = ref(false)
const testing = ref(false)
const loaded = ref(false)
const settings = ref<AdminLLMProxySettings | null>(null)
const enabled = ref(false)
const proxyURL = ref('')
const username = ref('')
const caPEM = ref('')
const password = ref('')
const clearPassword = ref(false)
const testURL = ref('')
const testResult = ref<AdminLLMProxyTestResult | null>(null)
const savedKey = ref('')
const loadError = ref('')
const busy = computed(() => loading.value || saving.value || testing.value)
let alive = true
onBeforeUnmount(() => {
  alive = false
})

const formKey = computed(() =>
  JSON.stringify([enabled.value, proxyURL.value, username.value, caPEM.value]),
)
const passwordDirty = computed(() => password.value !== '' || clearPassword.value)
const dirty = computed(() => formKey.value !== savedKey.value || passwordDirty.value)
const testAlertType = computed(() => {
  const result = testResult.value
  if (!result) return 'info'
  if (!result.ok) return 'error'
  return result.stale ? 'warning' : 'success'
})
const testSummary = computed(() => {
  const result = testResult.value
  if (!result) return ''
  const details = `耗时 ${result.elapsed_ms} ms，配置修订 ${result.revision}，检测时间 ${new Date(result.checked_at).toLocaleString()}。`
  if (!result.ok) {
    return `${result.error || '检测失败'}${result.code ? `（${result.code}）` : ''}。${details}`
  }
  const status = result.target_status ? `目标返回 HTTP ${result.target_status}。` : ''
  return `网络通路可达。${status}${details}`
})

function apply(settingsValue: AdminLLMProxySettings) {
  settings.value = settingsValue
  enabled.value = settingsValue.enabled
  proxyURL.value = settingsValue.url
  username.value = settingsValue.username
  caPEM.value = settingsValue.ca_pem
  password.value = ''
  clearPassword.value = false
  savedKey.value = JSON.stringify([
    settingsValue.enabled,
    settingsValue.url,
    settingsValue.username,
    settingsValue.ca_pem,
  ])
}

async function load() {
  if (busy.value || !alive) return
  loading.value = true
  loadError.value = ''
  try {
    const next = await adminApi.getLLMProxy()
    if (!alive) return
    apply(next)
    testResult.value = null
    loaded.value = true
  } catch (err: any) {
    if (!alive) return
    loadError.value = apiError(err)
    message.error(`读取 LLM 出站代理设置失败：${apiError(err)}`)
  } finally {
    loading.value = false
  }
}

function toggleClearPassword() {
  clearPassword.value = !clearPassword.value
  if (clearPassword.value) password.value = ''
}

async function save() {
  if (!settings.value || busy.value || !alive) return
  if (clearPassword.value && password.value !== '') {
    message.warning('不能同时清除和替换密码')
    return
  }
  saving.value = true
  try {
    const body: Parameters<typeof adminApi.updateLLMProxy>[0] = {
      expected_revision: settings.value.revision,
      enabled: enabled.value,
      url: proxyURL.value,
      username: username.value,
      ca_pem: caPEM.value,
    }
    if (password.value !== '') body.password = password.value
    if (clearPassword.value) body.clear_password = true
    const next = await adminApi.updateLLMProxy(body)
    if (!alive) return
    apply(next)
    testResult.value = null
    message.success('LLM 出站代理设置已保存')
  } catch (err: any) {
    if (!alive) return
    const code = err?.response?.data?.code
    if (code === 'settings_conflict') {
      message.error('代理配置已被其他操作更新，请重新读取后再保存')
    } else {
      message.error(`保存 LLM 出站代理失败：${apiError(err)}`)
    }
  } finally {
    saving.value = false
  }
}

async function test() {
  if (busy.value || !alive) return
  if (!settings.value || !settings.value.enabled) {
    message.warning('当前为直连模式，请先启用并保存代理')
    return
  }
  if (dirty.value) {
    message.warning('请先保存修改，再测试已保存的代理配置')
    return
  }
  testing.value = true
  try {
    const result = await adminApi.testLLMProxy(testURL.value.trim())
    if (alive) testResult.value = result
  } catch (err: any) {
    if (!alive) return
    testResult.value = null
    message.error(`代理连通性检测失败：${apiError(err)}`)
  } finally {
    testing.value = false
  }
}

function apiError(err: any) {
  return err?.response?.data?.error || err?.message || '未知错误'
}

onMounted(load)
</script>

<template>
  <section class="admin-section llm-proxy-panel">
    <div class="section-toolbar">
      <div>
        <h3>LLM 出站代理</h3>
        <p class="section-sub">
          仅影响后端发出的模型请求。浏览器直连、Relay、网站、数据库和邮件流量不使用此代理。
        </p>
      </div>
      <NTag :type="settings?.enabled ? 'success' : 'default'">
        {{ settings?.enabled ? '已启用代理' : '当前直连' }}
      </NTag>
    </div>

    <NAlert v-if="loadError" type="error">
      读取代理设置失败：{{ loadError }}
      <NButton :disabled="busy" @click="load">重新读取</NButton>
    </NAlert>
    <NSpin :show="loading">
      <template v-if="loaded">
        <NAlert type="info" :bordered="false" class="proxy-note">
          关闭代理后严格直连，不再继承 HTTP_PROXY / HTTPS_PROXY / NO_PROXY；启用后不会因 NO_PROXY
          绕过。Docker 部署时地址必须从后端容器内可达，容器里的 127.0.0.1 指向容器自身。
        </NAlert>

        <NForm label-placement="top" :disabled="busy" class="proxy-form">
          <NFormItem label="启用出站代理">
            <NSwitch v-model:value="enabled">
              <template #checked>已启用</template>
              <template #unchecked>直连</template>
            </NSwitch>
            <template #feedback>
              保存后新开始的模型调用立即使用新出口；进行中的流可以正常结束。
            </template>
          </NFormItem>

          <NFormItem label="代理地址">
            <NInput
              v-model:value="proxyURL"
              placeholder="http://host.docker.internal:7890"
              autocomplete="off"
            />
            <template #feedback>
              必须是 http:// 或 https:// 加明确主机和端口。http/https 指连接代理本身的协议，
              不能因为模型目标是 HTTPS 就写成 https://。
            </template>
          </NFormItem>

          <div class="proxy-grid">
            <NFormItem label="代理用户名">
              <NInput v-model:value="username" autocomplete="off" placeholder="可选" />
            </NFormItem>
            <NFormItem label="代理密码">
              <div class="password-setting">
                <NInput
                  v-model:value="password"
                  type="password"
                  show-password-on="click"
                  autocomplete="new-password"
                  :disabled="clearPassword || busy"
                  :placeholder="
                    settings?.has_password && !clearPassword ? '已配置，留空不修改' : '输入代理密码'
                  "
                />
                <NButton
                  secondary
                  :disabled="!settings?.has_password || busy"
                  @click="toggleClearPassword"
                >
                  {{ clearPassword ? '待清除' : '清除' }}
                </NButton>
              </div>
              <template #feedback>
                使用标准 Proxy Basic Auth。密码只以密文保存，任何读取接口都不会回显。
              </template>
            </NFormItem>
          </div>

          <NFormItem label="额外可信 CA（可选）">
            <NInput
              v-model:value="caPEM"
              type="textarea"
              :autosize="{ minRows: 4, maxRows: 10 }"
              placeholder="-----BEGIN CERTIFICATE-----"
            />
            <template #feedback>
              只追加到 LLM 专用客户端，同时影响代理端和模型目标的 TLS 校验；不修改系统或其他 HTTP
              客户端的证书池。仍会校验证书链和主机名，不会跳过校验。
            </template>
          </NFormItem>
        </NForm>

        <div class="actions proxy-actions">
          <NButton secondary :loading="loading" :disabled="busy" @click="load">重新读取</NButton>
          <NButton type="primary" :loading="saving" :disabled="busy" @click="save"
            >保存代理设置</NButton
          >
        </div>
        <p v-if="dirty" class="section-sub">有未保存的修改；测试按钮只会检测已保存的配置。</p>

        <section class="proxy-test">
          <h4>测试已保存配置</h4>
          <p class="section-sub">
            发送一次无凭据 HEAD，验证经代理到目标的网络通路。目标返回 401、403 或 405
            仍表示通路可达，不代表模型权限或协议通过。
          </p>
          <div class="proxy-test-row">
            <NInput
              v-model:value="testURL"
              placeholder="https://api.example.com（留空则使用平台 Base URL 的 origin）"
              :disabled="busy"
            />
            <NButton
              type="primary"
              secondary
              :loading="testing"
              :disabled="busy || !settings?.enabled || dirty"
              @click="test"
            >
              测试连接
            </NButton>
          </div>
          <NAlert v-if="testResult" :type="testAlertType" :bordered="false" class="proxy-result">
            {{ testSummary }}
            <template v-if="!testResult.ok">
              只验证网络通路；模型权限和协议请使用现有的能力检测。
            </template>
            <template v-if="testResult.stale">
              测试期间配置已更新，本次结果属于旧修订 {{ testResult.revision }}，请重新测试。
            </template>
          </NAlert>
          <p v-if="settings && !settings.enabled" class="section-sub">
            当前为直连模式，测试按钮不可用，也不会把直连成功显示为代理成功。
          </p>
        </section>
      </template>
    </NSpin>
  </section>
</template>

<style scoped>
.llm-proxy-panel {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.proxy-note {
  margin-bottom: 4px;
}

.proxy-form {
  max-width: 760px;
}

.proxy-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}

.password-setting {
  display: flex;
  width: 100%;
  gap: 8px;
}

.proxy-actions {
  justify-content: flex-end;
}

.proxy-test {
  margin-top: 8px;
  padding-top: 16px;
  border-top: 1px solid var(--app-border, rgba(128, 128, 128, 0.2));
}

.proxy-test-row {
  display: flex;
  gap: 8px;
  align-items: center;
}

.proxy-result {
  margin-top: 12px;
  white-space: pre-wrap;
}

@media (max-width: 720px) {
  .proxy-grid {
    grid-template-columns: 1fr;
  }
}
</style>
