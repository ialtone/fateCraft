<script setup lang="ts">
import { onMounted, ref, computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  NAlert,
  NButton,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NRadioButton,
  NRadioGroup,
  NSelect,
  NSlider,
  NSwitch,
  useDialog,
  useMessage,
} from 'naive-ui'
import { useSettingsStore } from '@/stores/settings'
import { useThemeStore } from '@/stores/theme'
import { useAuthStore } from '@/stores/auth'
import { useLlmRelayStore } from '@/stores/llmRelay'
import { getLocalKey, setLocalKey, clearLocalKey } from '@/llm/localKey'
import { identityApi, settingsApi } from '@/api'
import FieldHelp from '@/components/FieldHelp.vue'
import LLMModelSetting from '@/components/LLMModelSetting.vue'
import type { ExternalIdentity, LLMCapabilityDiagnostic, LLMCapabilityRole } from '@/types'

const store = useSettingsStore()
const theme = useThemeStore()
const auth = useAuthStore()
const relay = useLlmRelayStore()
const message = useMessage()
const dialog = useDialog()
const route = useRoute()
const router = useRouter()
const form = ref<Record<string, any>>({})
const apiKey = ref('')
// 请求路由（user_managed 下可选）：server=服务端转发，client=本设备直连。
const llmRoute = ref<'server' | 'client'>('server')
// 本设备 Key：只存浏览器，绝不随设置提交。
const localApiKey = ref('')
const testingConnection = ref(false)
const connectionResult = ref<{ ok: boolean; text: string } | null>(null)
const fetchingModels = ref(false)
const availableModels = ref<string[]>([])
const diagnostics = ref<Partial<Record<LLMCapabilityRole, LLMCapabilityDiagnostic>>>({})
const checkingRoles = ref<Partial<Record<LLMCapabilityRole, boolean>>>({})
const identities = ref<ExternalIdentity[]>([])
const loadingIdentities = ref(false)
const defaultProtocol = 'responses'
// platformModel：接入模式是否为平台统一模型，决定“服务端”选项的文案，
// 以及选中服务端时显示“已就绪”卡片（个人配置保留但不生效）。
const platformModel = computed(() => store.settings?.access_mode === 'platform_managed')
// 是否显示路由选择：只由平台“允许客户端直连”开关决定，与接入模式无关。
const routeChoiceVisible = computed(() => store.settings?.client_route_enabled === true)
const clientRoute = computed(() => routeChoiceVisible.value && llmRoute.value === 'client')
// user_managed 下开关关闭、用户之前选过客户端直连：服务端返回 server 且无
// Key，提示填写。platform_managed 下关闭开关会自动回到平台模型，无需 Key。
const clientRouteClosedHint = computed(
  () =>
    !platformModel.value &&
    !routeChoiceVisible.value &&
    !!store.settings &&
    !store.settings.has_api_key,
)
// 保存过的路由：切换对比与脏检查的基准。
const savedRoute = computed<'server' | 'client'>(() =>
  store.settings?.llm_route === 'client' ? 'client' : 'server',
)
// https 页面配 http 非回环地址时，浏览器会拦截混合内容，直连必然失败。
const mixedContentHint = computed(() => {
  if (window.location.protocol !== 'https:') return ''
  const raw = String(form.value.base_url || '').trim()
  if (!/^http:\/\//i.test(raw)) return ''
  let host = ''
  try {
    host = new URL(raw).hostname.toLowerCase()
  } catch {
    return ''
  }
  if (host === 'localhost' || host === '127.0.0.1' || host === '::1') return ''
  return '当前为 https 页面，该地址为 http 非回环地址，浏览器会拦截混合内容请求，直连不可用。'
})

// 接口协议始终提供两项，由用户手动选择，服务端永不改写；档位只决定
// 可选字段的取舍与提前拦截（识别结果见 resolved_profile）。
const protocolOptions = [
  { label: 'Chat Completions', value: 'chat_completions' },
  { label: 'Responses', value: 'responses' },
]
// 服务端按已保存 base_url 识别出的端点档位；base_url 改了没保存时识别结果
// 已过期，不展示。
const savedProfile = computed(() => store.settings?.resolved_profile)
const baseUrlUnchanged = computed(
  () => String(form.value.base_url || '').trim() === String(store.settings?.base_url || '').trim(),
)
const profileNames: Record<string, string> = {
  openai: 'OpenAI 官方',
  deepseek: 'DeepSeek 官方',
  openrouter: 'OpenRouter',
}
const recognizedHint = computed(() => {
  const profile = savedProfile.value
  if (!profile || !baseUrlUnchanged.value || profile.id === 'custom_openai_compatible') return ''
  return `已识别为 ${profileNames[profile.id] || profile.id}，已自动适配请求参数`
})

const maxResponseTokens = computed(() =>
  Math.max(128, Number(form.value.max_context_tokens || 4096) - 257),
)

const capabilitySettingsDirty = computed(() => {
  // 平台统一模型且选中服务端时：唯一可保存的是路由（切回客户端直连），
  // 只比较路由；其他情况比较完整表单。
  if (platformModel.value && !clientRoute.value) {
    return llmRoute.value !== savedRoute.value
  }
  // 只有整卡只读（个人配置不可编辑）时才视为无改动；platform_managed 下选中
  // 客户端直连时个人表单可编辑，必须走完整比较，否则诊断会测到未保存的旧配置。
  if (store.settings?.editable === false) return false
  const saved = store.settings
  if (!saved) return true
  return (
    // 服务端 Key 需先保存；本设备 Key 直接生效，不参与脏检查。
    (!clientRoute.value && apiKey.value.trim() !== '') ||
    llmRoute.value !== savedRoute.value ||
    String(form.value.base_url || '').trim() !== String(saved.base_url || '').trim() ||
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
    form.value.protocol,
    form.value.model,
    form.value.memory_model,
    form.value.inquiry_model,
    form.value.stream_enabled,
    apiKey.value.trim() !== '',
    localApiKey.value.trim() !== '',
    llmRoute.value,
  ]),
)

watch(capabilityConfigKey, () => {
  diagnostics.value = {}
})

onMounted(async () => {
  showLinuxDOBindResult()
  loadingIdentities.value = true
  await Promise.all([
    store.load(),
    identityApi
      .list()
      .then((value) => (identities.value = value))
      .catch(() => {}),
  ])
  loadingIdentities.value = false
  if (store.settings) {
    form.value = {
      ...store.settings,
      protocol: store.settings.protocol || defaultProtocol,
    }
    llmRoute.value = store.settings.llm_route === 'client' ? 'client' : 'server'
  }
  if (auth.user) localApiKey.value = getLocalKey(auth.user.id)
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

function unbindIdentity(identity: ExternalIdentity) {
  dialog.warning({
    title: '解除第三方账户绑定？',
    content: `确认解除 ${identity.provider} 账号「${identity.username || identity.provider_user_id}」的绑定？`,
    positiveText: '解除绑定',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        await identityApi.unbind(identity.id)
        identities.value = identities.value.filter((item) => item.id !== identity.id)
        message.success('第三方账户已解除绑定')
      } catch (err: any) {
        message.error(`解除绑定失败：${err.message || '未知错误'}`)
      }
    },
  })
}

// 切换请求路由时弹确认框：列出不可逆后果，取消则保持原值。
function onRouteChange(next: 'server' | 'client') {
  if (next === llmRoute.value) return
  if (next === savedRoute.value) {
    llmRoute.value = next
    return
  }
  const toClient = next === 'client'
  const toClientContent = platformModel.value
    ? '1. 平台模型下创建的对话将无法继续生成，需新建对话；\n2. 此模式下挑战与 Campaign 不发放奖励，重新生成不消耗额度。'
    : '1. 另一模式（服务端转发）下创建的对话将无法继续生成，需新建对话；\n2. 切到客户端直连会删除服务器上保存的 Key；\n3. 此模式下挑战与 Campaign 不发放奖励，重新生成不消耗额度。'
  const toServerContent = platformModel.value
    ? '1. 客户端直连模式下创建的对话将无法继续生成，需新建对话；\n2. 切回平台模型无需填写 Key，个人配置会保留。'
    : '1. 客户端直连模式下创建的对话将无法继续生成，需新建对话；\n2. 切回服务端需要重新填写 Key。'
  dialog.warning({
    title: toClient
      ? '切换到客户端直连？'
      : platformModel.value
        ? '切回平台模型？'
        : '切回服务端转发？',
    content: toClient ? toClientContent : toServerContent,
    positiveText: '确认切换',
    negativeText: '取消',
    onPositiveClick: () => {
      llmRoute.value = next
    },
  })
}

async function save() {
  if (platformModel.value && !routeChoiceVisible.value) {
    message.info('当前由平台统一提供 LLM 配置')
    return
  }
  // 平台统一模型且选中服务端：只提交路由（使用平台模型），个人配置保留，
  // 后端会忽略其他字段；此时个人表单隐藏，无需校验。
  if (platformModel.value && !clientRoute.value) {
    try {
      await store.update({ llm_route: 'server' })
      message.success('已切回平台模型')
      diagnostics.value = {}
      relay.refresh()
    } catch (err: any) {
      message.error(`保存设置失败：${err.message || '未知错误'}`)
    }
    return
  }
  if (Number(form.value.max_response_tokens) + 256 >= Number(form.value.max_context_tokens)) {
    message.error('最大回复 token 加 256 token 格式预留后必须小于最大上下文 token')
    return
  }
  const body: Record<string, any> = {
    base_url: form.value.base_url,
    protocol: form.value.protocol || defaultProtocol,
    // 平台未开放客户端直连时固定提交 server（路由选择已隐藏）。
    llm_route: routeChoiceVisible.value ? llmRoute.value : 'server',
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
  if (clientRoute.value) {
    // 客户端直连：Key 只写本设备，不随设置提交；顺手清掉服务端输入框。
    if (auth.user && localApiKey.value.trim()) setLocalKey(auth.user.id, localApiKey.value)
    apiKey.value = ''
  } else {
    // 仅当填写了新 key 时才提交，避免清空已有密钥。
    if (apiKey.value) body.api_key = apiKey.value
  }
  try {
    await store.update(body)
    apiKey.value = ''
    message.success('设置已保存')
    diagnostics.value = {}
    connectionResult.value = null
    relay.refresh()
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
  // 客户端直连：服务端 409，由浏览器直接请求。
  if (clientRoute.value) {
    fetchingModels.value = true
    try {
      await fetchModelsDirect(false)
    } catch (err: any) {
      message.error(`获取模型列表失败：${err.message}`)
      availableModels.value = []
    } finally {
      fetchingModels.value = false
    }
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

// 浏览器直接请求 {base_url}/models。Key 只发往本地 base_url。
async function fetchModelsDirect(report: boolean): Promise<string[]> {
  const baseUrl = String(form.value.base_url || '')
    .trim()
    .replace(/\/+$/, '')
  const key = localApiKey.value.trim()
  if (!baseUrl) throw new Error('请先填写 Base URL')
  if (!key) throw new Error('请先填写本设备 API Key')
  let response: Response
  try {
    response = await fetch(`${baseUrl}/models`, {
      headers: { Authorization: `Bearer ${key}` },
    })
  } catch (err: any) {
    throw new Error(`浏览器无法访问该地址（跨域限制、网络或混合内容）：${err?.message || ''}`)
  }
  if (response.status === 401 || response.status === 403) {
    throw new Error('鉴权失败：API Key 无效或无权限')
  }
  if (!response.ok) {
    throw new Error(`模型服务返回 ${response.status}`)
  }
  const data: any = await response.json()
  const models = Array.isArray(data?.data)
    ? data.data.map((item: any) => String(item?.id || '')).filter(Boolean)
    : []
  if (!report) {
    if (!models.length) {
      message.warning('未找到可用模型')
      availableModels.value = []
      return models
    }
    availableModels.value = models
    form.value.model = models[0]
    if (form.value.memory_model && !models.includes(form.value.memory_model)) {
      form.value.memory_model = ''
    }
    if (form.value.inquiry_model && !models.includes(form.value.inquiry_model)) {
      form.value.inquiry_model = ''
    }
    message.success(`获取到 ${models.length} 个模型`)
  }
  return models
}

// 测试连接（仅客户端直连）：区分鉴权失败 / 跨域或网络失败 / 成功。
async function testConnection() {
  if (mixedContentHint.value) {
    connectionResult.value = { ok: false, text: mixedContentHint.value }
    return
  }
  testingConnection.value = true
  connectionResult.value = null
  try {
    const models = await fetchModelsDirect(true)
    connectionResult.value = { ok: true, text: `连接成功，找到 ${models.length} 个模型` }
  } catch (err: any) {
    connectionResult.value = { ok: false, text: err?.message || '连接失败' }
  } finally {
    testingConnection.value = false
  }
}

function clearDeviceKey() {
  if (auth.user) clearLocalKey(auth.user.id)
  localApiKey.value = ''
  message.success('本设备 Key 已清除')
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
      <div v-if="platformModel && !routeChoiceVisible" class="managed-state">
        <div class="managed-state-title">
          {{ store.settings?.configuration_ready ? '模型服务已准备就绪' : '平台模型尚未配置完成' }}
        </div>
        <p v-if="store.settings?.configuration_ready">
          平台已经为你配置好模型服务，无需填写 API Key 或选择模型，直接开始使用即可。
        </p>
        <p v-else>请联系管理员完成模型配置后再开始游戏，无需填写个人 API Key。</p>
        <p class="managed-state-hint">如需调整模型服务，请联系管理员。</p>
      </div>
      <NForm v-else label-placement="top" size="small">
        <NFormItem v-if="routeChoiceVisible">
          <template #label>
            <FieldHelp label="请求路由">
              <p>
                <strong>服务端转发（平台统一模型时叫“平台模型”）</strong>
                由服务器向模型服务商发请求。挑战与 Campaign
                正常发放奖励，重新生成消耗额度；个人模式下 API Key 加密保存在服务器。
              </p>
              <p>
                <strong>客户端直连</strong>
                由你的浏览器直接向模型服务商发请求，API Key
                只保存在本设备。但服务端无法校验模型输出：挑战与 Campaign
                不发放奖励，重新生成不消耗额度；发给模型的完整提示词（含隐藏剧情）可在浏览器中查看。该模式只适合体验对话玩法。
              </p>
            </FieldHelp>
          </template>
          <NRadioGroup :value="llmRoute" size="medium" @update:value="onRouteChange">
            <NRadioButton value="server">{{
              platformModel ? '平台模型（推荐）' : '服务端转发（推荐）'
            }}</NRadioButton>
            <NRadioButton value="client">客户端直连</NRadioButton>
          </NRadioGroup>
          <template #feedback>
            <span class="faint sm">
              {{
                llmRoute === 'client'
                  ? '由本设备浏览器直接请求模型，API Key 只保存在本设备'
                  : platformModel
                    ? '使用平台统一配置的模型，完整享有奖励与挑战记录'
                    : '由服务器请求模型，完整享有奖励与挑战记录'
              }}
            </span>
          </template>
        </NFormItem>
        <NAlert v-if="clientRoute" type="warning" :show-icon="false" class="route-note">
          此模式下，服务端无法校验模型输出：挑战与 Campaign
          不发放奖励，重新生成不消耗额度；发给模型的完整提示词（含隐藏剧情信息）可在浏览器中查看。该模式只用于体验对话玩法。
        </NAlert>
        <div v-if="platformModel && !clientRoute" class="managed-state">
          <div class="managed-state-title">平台统一提供模型服务</div>
          <p v-if="store.settings?.source !== 'platform'">
            保存后将使用平台统一配置的模型，无需填写个人 API Key。
          </p>
          <p v-else-if="store.settings?.configuration_ready">
            当前使用平台统一配置的模型，直接开始使用即可；个人配置会保留，切回客户端直连时可继续使用。
          </p>
          <p v-else>请联系管理员完成模型配置，或切换到客户端直连并配置自己的 API。</p>
        </div>
        <template v-if="!platformModel || clientRoute">
          <NFormItem label="Base URL（OpenAI 兼容）">
            <div class="protocol-setting">
              <NInput v-model:value="form.base_url" placeholder="https://api.openai.com/v1" />
              <div class="protocol-setting__hint">
                填谁的地址就用谁的服务（官方、OpenRouter、中转站、本地模型均可）。OpenAI、DeepSeek、OpenRouter
                官方地址会自动适配。
              </div>
            </div>
          </NFormItem>
          <NFormItem label="接口协议">
            <div class="protocol-setting">
              <NSelect v-model:value="form.protocol" :options="protocolOptions" />
              <div class="protocol-setting__hint">
                <template v-if="recognizedHint">{{ recognizedHint }}</template>
                <template v-else> 按所选协议请求。不确定时先用“测试连接”或能力诊断确认。 </template>
              </div>
            </div>
          </NFormItem>
          <NAlert v-if="clientRouteClosedHint" type="info" :show-icon="false" class="route-note">
            平台已关闭客户端直连，请填写 API Key 以使用服务端转发。
            您的密钥会在服务端加密保存，请放心配置。
          </NAlert>
          <NFormItem
            v-if="!clientRoute"
            :label="`API Key${store.settings?.has_api_key ? '（已配置，留空则保持不变）' : ''}`"
          >
            <NInput
              v-model:value="apiKey"
              type="password"
              show-password-on="click"
              placeholder="sk-…"
            />
            <template #feedback>
              API Key 经服务端加密后存入数据库，不以明文保存，保存后也不会在页面中回显。
            </template>
          </NFormItem>
          <template v-else>
            <NFormItem label="本设备 API Key（只保存在本设备，不会发送到服务器）">
              <NInput
                v-model:value="localApiKey"
                type="password"
                show-password-on="click"
                placeholder="sk-…"
              />
            </NFormItem>
            <NAlert v-if="mixedContentHint" type="error" :show-icon="false" class="route-note">
              {{ mixedContentHint }}
            </NAlert>
            <div class="row device-key-row">
              <NButton secondary size="small" :loading="testingConnection" @click="testConnection">
                测试连接
              </NButton>
              <NButton secondary size="small" @click="clearDeviceKey">清除本设备 Key</NButton>
              <span class="faint sm">Relay 状态：{{ relay.connected ? '已连接' : '未连接' }}</span>
            </div>
            <NAlert
              v-if="connectionResult"
              :type="connectionResult.ok ? 'success' : 'error'"
              :show-icon="false"
              class="route-note"
            >
              {{ connectionResult.text }}
            </NAlert>
          </template>
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
        </template>
      </NForm>
      <div v-if="!platformModel || routeChoiceVisible" class="save-row row">
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
        {{ auth.config.linuxdo_enabled ? '绑定 LinuxDo' : 'LinuxDo 不可用' }}
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
  min-width: 0;
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.5;
  overflow-wrap: anywhere;
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

.route-note {
  margin-bottom: 14px;
}

.device-key-row {
  align-items: center;
  gap: 8px;
  margin-bottom: 14px;
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
    position: static;
    margin-top: 20px;
    padding: 8px 0 calc(8px + env(safe-area-inset-bottom));
  }

  .save-row > .n-button {
    width: 100%;
    min-width: 0;
  }
}
</style>
