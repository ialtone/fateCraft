<script setup lang="ts">
import { onMounted, ref, computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  NButton,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NSelect,
  NSlider,
  NSwitch,
  useMessage,
} from 'naive-ui'
import { useSettingsStore } from '@/stores/settings'
import { useThemeStore } from '@/stores/theme'
import { useAuthStore } from '@/stores/auth'
import { identityApi, settingsApi } from '@/api'
import FieldHelp from '@/components/FieldHelp.vue'
import LLMModelSetting from '@/components/LLMModelSetting.vue'
import type {
  ExternalIdentity,
  LLMCapabilityDiagnostic,
  LLMCapabilityRole,
  LLMProviderProfile,
} from '@/types'

const store = useSettingsStore()
const theme = useThemeStore()
const auth = useAuthStore()
const message = useMessage()
const route = useRoute()
const router = useRouter()
const form = ref<Record<string, any>>({})
const apiKey = ref('')
const fetchingModels = ref(false)
const availableModels = ref<string[]>([])
const diagnostics = ref<Partial<Record<LLMCapabilityRole, LLMCapabilityDiagnostic>>>({})
const checkingRoles = ref<Partial<Record<LLMCapabilityRole, boolean>>>({})
const providerProfiles = ref<LLMProviderProfile[]>([])
const identities = ref<ExternalIdentity[]>([])
const loadingIdentities = ref(false)
const defaultProtocol = 'responses'
const platformManaged = computed(
  () => store.settings?.access_mode === 'platform_managed' || store.settings?.editable === false,
)

const providerOptions = computed(() =>
  providerProfiles.value.map((profile) => ({ label: profile.id, value: profile.id })),
)
const selectedProfile = computed<LLMProviderProfile | undefined>(() =>
  providerProfiles.value.find((profile) => profile.id === form.value.provider),
)
const protocolOptions = computed(() => {
  const allowed = selectedProfile.value?.allowed_apis || ['chat_completions', 'responses']
  return allowed.map((value) => ({
    label: value === 'responses' ? 'Responses' : 'Chat Completions',
    value,
  }))
})

const maxResponseTokens = computed(() =>
  Math.max(128, Number(form.value.max_context_tokens || 4096) - 257),
)

const capabilitySettingsDirty = computed(() => {
  if (platformManaged.value) return false
  const saved = store.settings
  if (!saved) return true
  return (
    apiKey.value.trim() !== '' ||
    String(form.value.base_url || '').trim() !== String(saved.base_url || '').trim() ||
    String(form.value.provider || 'openai') !== String(saved.provider || 'openai') ||
    String(form.value.protocol || defaultProtocol) !== String(saved.protocol || defaultProtocol) ||
    String(form.value.model || '').trim() !== String(saved.model || '').trim() ||
    String(form.value.memory_model || '').trim() !== String(saved.memory_model || '').trim() ||
    String(form.value.inquiry_model || '').trim() !== String(saved.inquiry_model || '').trim() ||
    Boolean(form.value.stream_enabled) !== Boolean(saved.stream_enabled)
  )
})

const capabilityConfigKey = computed(() =>
  JSON.stringify([
    form.value.base_url,
    form.value.provider,
    form.value.protocol,
    form.value.model,
    form.value.memory_model,
    form.value.inquiry_model,
    form.value.stream_enabled,
    apiKey.value.trim() !== '',
  ]),
)

watch(capabilityConfigKey, () => {
  diagnostics.value = {}
})

watch(
  () => form.value.provider,
  () => {
    const allowed = selectedProfile.value?.allowed_apis
    if (allowed && !allowed.includes(form.value.protocol)) {
      form.value.protocol = selectedProfile.value?.default_api || allowed[0]
    }
  },
)

onMounted(async () => {
  showLinuxDOBindResult()
  loadingIdentities.value = true
  await Promise.all([
    store.load(),
    settingsApi.providers().then((value) => (providerProfiles.value = value)),
    identityApi
      .list()
      .then((value) => (identities.value = value))
      .catch(() => {}),
  ])
  loadingIdentities.value = false
  if (store.settings) {
    form.value = {
      ...store.settings,
      provider: store.settings.provider || 'openai',
      protocol: store.settings.protocol || defaultProtocol,
    }
  }
})

function bindLinuxDO() {
  window.location.assign(identityApi.bindLinuxDOURL('/settings'))
}

// LinuxDo 绑定回调会带着 linuxdo_bind=ok 或 linuxdo_bind_error=原因 回到本页；
// 展示结果后移除这些查询参数，避免刷新时重复提示。
function showLinuxDOBindResult() {
  const bindError = route.query.linuxdo_bind_error
  const bindResult = route.query.linuxdo_bind
  if (typeof bindError === 'string' && bindError) {
    message.error(bindError)
  } else if (bindResult === 'ok') {
    message.success('LinuxDo 账户已绑定')
  } else {
    return
  }
  const query = { ...route.query }
  delete query.linuxdo_bind_error
  delete query.linuxdo_bind
  void router.replace({ query })
}

async function unbindIdentity(identity: ExternalIdentity) {
  if (
    !window.confirm(
      `确认解除 ${identity.provider} 账号「${identity.username || identity.provider_user_id}」的绑定？`,
    )
  )
    return
  try {
    await identityApi.unbind(identity.id)
    identities.value = identities.value.filter((item) => item.id !== identity.id)
    message.success('第三方账户已解除绑定')
  } catch (err: any) {
    message.error(`解除绑定失败：${err.message || '未知错误'}`)
  }
}

async function save() {
  if (platformManaged.value) {
    message.info('当前由平台统一提供 LLM 配置')
    return
  }
  if (Number(form.value.max_response_tokens) + 256 >= Number(form.value.max_context_tokens)) {
    message.error('最大回复 token 加 256 token 格式预留后必须小于最大上下文 token')
    return
  }
  const body: Record<string, any> = {
    provider: form.value.provider || 'openai',
    base_url: form.value.base_url,
    protocol: form.value.protocol || defaultProtocol,
    model: form.value.model,
    memory_model: form.value.memory_model || '',
    inquiry_model: form.value.inquiry_model || '',
    max_context_tokens: Number(form.value.max_context_tokens),
    max_response_tokens: Number(form.value.max_response_tokens),
    temperature: Number(form.value.temperature),
    top_p: Number(form.value.top_p),
    stream_enabled: !!form.value.stream_enabled,
    reasoning_effort: String(form.value.reasoning_effort ?? '').trim(),
    memory_reasoning_effort: String(form.value.memory_reasoning_effort ?? '').trim(),
    inquiry_reasoning_effort: String(form.value.inquiry_reasoning_effort ?? '').trim(),
  }
  // 仅当填写了新 key 时才提交，避免清空已有密钥。
  if (apiKey.value) body.api_key = apiKey.value
  try {
    await store.update(body)
    apiKey.value = ''
    message.success('设置已保存')
    diagnostics.value = {}
  } catch (err: any) {
    message.error(`保存设置失败：${err.message || '未知错误'}`)
  }
}

async function checkCapability(role: LLMCapabilityRole) {
  if (capabilitySettingsDirty.value) {
    message.warning('请先保存当前 LLM 设置，再执行模型检测')
    return
  }
  checkingRoles.value = { ...checkingRoles.value, [role]: true }
  try {
    const data = await settingsApi.checkCapabilities(role)
    diagnostics.value = { ...diagnostics.value, [role]: data.result }
  } catch (err: any) {
    message.error(`模型检测失败：${err.message || '未知错误'}`)
  } finally {
    checkingRoles.value = { ...checkingRoles.value, [role]: false }
  }
}

async function fetchModels() {
  if (!form.value.base_url) {
    message.warning('请先填写 Base URL')
    return
  }

  fetchingModels.value = true
  try {
    // 通过后端代理请求，避免跨域问题
    const data = await settingsApi.fetchModels({
      base_url: form.value.base_url,
      api_key: apiKey.value || undefined, // 优先使用新填写的 key
    })

    if (!data.models || !Array.isArray(data.models)) {
      throw new Error('返回数据格式不正确')
    }

    const models = data.models
    if (models.length === 0) {
      message.warning('未找到可用模型')
      availableModels.value = []
      return
    }

    // 新的服务商列表不能继续沿用旧服务商的模型名。叙事模型选择
    // 列表首项；两个可选模型仅在原值仍有效时保留，否则改为继承。
    availableModels.value = models
    form.value.model = models[0]
    if (form.value.memory_model && !models.includes(form.value.memory_model)) {
      form.value.memory_model = ''
    }
    if (form.value.inquiry_model && !models.includes(form.value.inquiry_model)) {
      form.value.inquiry_model = ''
    }
    message.success(`获取到 ${models.length} 个模型`)
  } catch (err: any) {
    message.error(`获取模型列表失败：${err.message}`)
    console.error('获取模型失败：', err)
    availableModels.value = []
  } finally {
    fetchingModels.value = false
  }
}
</script>

<template>
  <div class="page">
    <div class="page-title">
      <h2>设置</h2>
      <span class="ornament">◆</span>
    </div>
    <p class="page-sub">叙事引擎与外观的调校台。</p>

    <section class="card">
      <div class="card-head">外观</div>
      <div class="appearance-row row spread">
        <div>
          <div>主题模式</div>
          <div class="faint sm">
            {{ theme.mode === 'dark' ? '夜茶馆（暗色）' : '宣纸（亮色）' }}
          </div>
        </div>
        <NButton secondary @click="theme.toggle()">
          {{ theme.mode === 'dark' ? '☀ 切换到宣纸' : '☾ 切换到夜茶馆' }}
        </NButton>
      </div>
    </section>

    <section class="card">
      <div class="card-head">LLM 接入</div>
      <div v-if="platformManaged" class="managed-state">
        <div class="managed-state-title">模型服务已准备就绪</div>
        <p>平台已经为你配置好模型服务，无需填写 API Key 或选择模型，直接开始使用即可。</p>
        <p class="managed-state-hint">如需调整模型服务，请联系管理员。</p>
      </div>
      <NForm v-else label-placement="top" size="small">
        <NFormItem label="Provider">
          <NSelect
            v-model:value="form.provider"
            :options="providerOptions"
            placeholder="选择服务商配置"
          />
        </NFormItem>
        <NFormItem label="Base URL（OpenAI 兼容）">
          <NInput v-model:value="form.base_url" placeholder="https://api.openai.com/v1" />
        </NFormItem>
        <NFormItem label="接口协议">
          <div class="protocol-setting">
            <NSelect
              v-model:value="form.protocol"
              :options="protocolOptions"
              :disabled="!!selectedProfile && selectedProfile.allowed_apis.length === 1"
            />
            <div v-if="selectedProfile" class="protocol-setting__hint">
              <span class="protocol-setting__badge">Provider 能力</span>
              <span>Profile：{{ selectedProfile.id }} · 协议由后端自动解析</span>
            </div>
          </div>
        </NFormItem>
        <NFormItem
          :label="`API Key${store.settings?.has_api_key ? '（已配置，留空则保持不变）' : ''}`"
        >
          <NInput
            v-model:value="apiKey"
            type="password"
            show-password-on="click"
            placeholder="sk-…"
          />
        </NFormItem>
        <div class="model-setting-list">
          <LLMModelSetting
            v-model="form.model"
            v-model:reasoning-effort="form.reasoning_effort"
            :options="availableModels"
            role="narrative"
            :diagnostic="diagnostics.narrative"
            :checking="checkingRoles.narrative"
            :model-feedback="
              availableModels.length > 0
                ? `已加载 ${availableModels.length} 个模型，可下拉选择，也可直接输入。`
                : ''
            "
            placeholder="可直接输入模型名称，或先获取模型列表"
            @detect="checkCapability('narrative')"
          >
            <template #model-label-extra>
              <NButton
                text
                type="primary"
                size="tiny"
                :loading="fetchingModels"
                @click="fetchModels"
              >
                获取模型列表
              </NButton>
            </template>
          </LLMModelSetting>

          <LLMModelSetting
            v-model="form.memory_model"
            v-model:reasoning-effort="form.memory_reasoning_effort"
            :options="availableModels"
            role="memory"
            :diagnostic="diagnostics.memory"
            :checking="checkingRoles.memory"
            placeholder="留空则继承叙事模型，也可直接输入"
            @detect="checkCapability('memory')"
          />

          <LLMModelSetting
            v-model="form.inquiry_model"
            v-model:reasoning-effort="form.inquiry_reasoning_effort"
            :options="availableModels"
            role="character_inquiry"
            :diagnostic="diagnostics.character_inquiry"
            :checking="checkingRoles.character_inquiry"
            placeholder="留空则继承叙事模型，也可直接输入"
            @detect="checkCapability('character_inquiry')"
          />
        </div>
        <NFormItem>
          <template #label>
            <FieldHelp label="流式输出">
              <p>开启后模型会边生成边返回内容，界面可以逐步显示；关闭后等待完整回复再展示。</p>
              <p>流式输出不会改变模型能力或最大 token；如果服务商的流式接口不稳定，可以关闭。</p>
            </FieldHelp>
          </template>
          <NSwitch v-model:value="form.stream_enabled">
            <template #checked>开启</template>
            <template #unchecked>关闭</template>
          </NSwitch>
        </NFormItem>
        <div class="token-row row">
          <NFormItem style="flex: 1">
            <template #label>
              <FieldHelp label="最大上下文 token">
                <p>
                  限制一次请求可使用的上下文总预算，包括系统提示、世界书、历史消息和模型回复。过小会丢失较早内容，过大可能超出模型窗口并增加成本。
                </p>
              </FieldHelp>
            </template>
            <NInputNumber
              v-model:value="form.max_context_tokens"
              :min="4096"
              :max="2000000"
              :step="1024"
              style="width: 100%"
            />
          </NFormItem>
          <NFormItem style="flex: 1">
            <template #label>
              <FieldHelp label="最大回复 token">
                <p>
                  限制模型单次回复的最大
                  token，不代表模型一定会生成这么长。该值需要小于上下文预算，并为协议和上下文留出空间。
                </p>
              </FieldHelp>
            </template>
            <NInputNumber
              v-model:value="form.max_response_tokens"
              :min="128"
              :max="maxResponseTokens"
              :step="128"
              style="width: 100%"
            />
          </NFormItem>
        </div>
        <NFormItem>
          <template #label>
            <FieldHelp :label="`temperature：${form.temperature ?? '—'}`">
              <p>
                控制输出的随机性。值越高，输出越有创造力和不可预测性；值越低，输出越稳定和确定性。
              </p>
              <p>推荐范围：0.7-1.0（叙事）/ 0.2-0.5（逻辑任务）。</p>
            </FieldHelp>
          </template>
          <NSlider v-model:value="form.temperature" :min="0" :max="2" :step="0.1" />
        </NFormItem>
        <NFormItem>
          <template #label>
            <FieldHelp :label="`top_p：${form.top_p ?? '—'}`">
              <p>
                核采样（Nucleus
                Sampling）控制候选词汇的范围。值越小，模型越倾向于选择最可能的词汇；值越大，候选范围越广。
              </p>
              <p>推荐范围：0.9-1.0，通常不要和 temperature 同时大幅调整。</p>
            </FieldHelp>
          </template>
          <NSlider v-model:value="form.top_p" :min="0" :max="1" :step="0.05" />
        </NFormItem>
      </NForm>
      <div v-if="!platformManaged" class="save-row row">
        <NButton type="primary" @click="save">保存设置</NButton>
      </div>
    </section>

    <section class="card">
      <div class="card-head">第三方账户</div>
      <p class="faint sm">
        第三方账户只作为登录身份，必须绑定到已有的本地账号，不会单独创建站内用户。
      </p>
      <div class="identity-list">
        <div v-if="loadingIdentities" class="faint sm">正在读取绑定状态…</div>
        <div v-else-if="!identities.length" class="faint sm">尚未绑定第三方账户。</div>
        <div v-for="identity in identities" :key="identity.id" class="identity-row">
          <div>
            <strong>{{ identity.provider === 'linuxdo' ? 'LinuxDo' : identity.provider }}</strong>
            <span class="faint sm">{{ identity.username || identity.provider_user_id }}</span>
          </div>
          <NButton size="small" secondary @click="unbindIdentity(identity)">解除绑定</NButton>
        </div>
      </div>
      <NButton secondary :disabled="!auth.config.linuxdo_enabled" @click="bindLinuxDO">
        {{ auth.config.linuxdo_enabled ? '绑定 LinuxDo' : 'LinuxDo 未配置' }}
      </NButton>
    </section>
  </div>
</template>

<style scoped>
.page {
  max-width: 720px;
  margin: 0 auto;
}

.card {
  background: var(--bg-panel);
  border: 1px solid var(--border-soft);
  border-radius: var(--radius);
  padding: 18px 22px;
  margin-bottom: 18px;
  box-shadow: var(--shadow-card);
}

.card-head {
  font-weight: 600;
  letter-spacing: 2px;
  margin-bottom: 14px;
  padding-bottom: 10px;
  border-bottom: 1px solid var(--border-soft);
}

.managed-state {
  padding: 8px 0 2px;
  color: var(--text-muted);
  font-size: 14px;
  line-height: 1.65;
}

.managed-state-title {
  color: var(--text);
  font-weight: 600;
}

.managed-state p {
  margin: 6px 0 0;
}

.managed-state-hint {
  color: var(--text-muted);
  font-size: 13px;
}

.sm {
  font-size: 12px;
}

.identity-list {
  margin: 14px 0;
  border-top: 1px solid var(--border-soft);
}

.identity-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 11px 0;
  border-bottom: 1px solid var(--border-soft);
}

.identity-row > div {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 3px;
}

.protocol-setting {
  display: grid;
  width: 100%;
  min-width: 0;
  gap: 8px;
}

.protocol-setting :deep(.n-select) {
  width: 100%;
}

.protocol-setting__hint {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 8px;
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.5;
}

.protocol-setting__hint > span:last-child {
  min-width: 0;
  overflow-wrap: anywhere;
}

.protocol-setting__badge {
  flex: 0 0 auto;
  padding: 2px 7px;
  border: 1px solid color-mix(in srgb, var(--accent) 30%, var(--border-soft));
  border-radius: 999px;
  background: var(--accent-soft);
  color: var(--accent-hover);
  white-space: nowrap;
}

.section-divider {
  margin: 24px 0 16px;
  text-align: center;
  position: relative;
}

.section-divider::before {
  content: '';
  position: absolute;
  left: 0;
  right: 0;
  top: 50%;
  height: 1px;
  background: var(--border-soft);
}

.section-divider span {
  position: relative;
  padding: 0 12px;
  background: var(--bg-panel);
  color: var(--text-faint);
  font-size: 13px;
  font-weight: 500;
  letter-spacing: 1px;
}

.appearance-row {
  padding: 4px 0;
}

.token-row {
  gap: 16px;
}

.model-setting-list {
  margin-bottom: 14px;
  border-top: 1px solid var(--border-soft);
}

.save-row {
  position: sticky;
  bottom: 16px;
  z-index: 5;
  justify-content: center;
  margin-top: 16px;
  padding: 0;
  background: transparent;
  border: none;
  pointer-events: none;
}

.save-row > .n-button {
  pointer-events: auto;
  min-width: 200px;
  border-radius: 999px;
  box-shadow: var(--shadow-card);
}

@media (max-width: 560px) {
  .card {
    padding: 16px;
  }

  .appearance-row {
    align-items: stretch;
    flex-direction: column;
    gap: 12px;
  }

  .appearance-row > .n-button {
    width: 100%;
  }

  .token-row {
    align-items: stretch;
    flex-direction: column;
    gap: 0;
  }

  .save-row {
    bottom: calc(12px + env(safe-area-inset-bottom));
  }

  .save-row > .n-button {
    width: auto;
    min-width: min(240px, 70vw);
  }
}
</style>
