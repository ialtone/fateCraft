<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import {
  NButton,
  NAlert,
  NCollapse,
  NCollapseItem,
  NDatePicker,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NModal,
  NRadioButton,
  NRadioGroup,
  NSpin,
  NSelect,
  NSwitch,
  NTabPane,
  NTabs,
  NTag,
  useDialog,
  useMessage,
} from 'naive-ui'
import { adminApi } from '@/api'
import { useAuthStore } from '@/stores/auth'
import ChallengeVersionEditor from '@/components/ChallengeVersionEditor.vue'
import FieldHelp from '@/components/FieldHelp.vue'
import LLMModelSetting from '@/components/LLMModelSetting.vue'
import AdminUsersPanel from '@/components/AdminUsersPanel.vue'
import type {
  AdminBuiltinChallenge,
  AdminAuthSettings,
  AdminExternalIdentity,
  AdminChallengePack,
  AdminLLMRequestLog,
  AdminLLMRequestLogSummary,
  AdminContextBuildLog,
  AdminContextBuildLogSummary,
  AdminSystemSettings,
  ChallengeVersionDocument,
  DifficultyModeEnum,
  GameplayModuleDescriptor,
  InvitationCode,
  LLMCapabilityDiagnostic,
  LLMCapabilityRole,
  RewardDefinition,
  SaveChallengeVersionInput,
  ConversationShare,
  ConversationShareReport,
} from '@/types'

const props = withDefaults(
  defineProps<{
    /** When set, render only the matching admin area. The default keeps the legacy all-in-one view. */
    section?: string
  }>(),
  { section: 'all' },
)

const message = useMessage()
const dialog = useDialog()
const auth = useAuthStore()
const activeTab = ref('settings')

function tabNameForSection(section: string) {
  if (section === 'all') return 'settings'
  if (section === 'content') return 'builtins'
  return section
}

const sectionTitle = computed(
  () =>
    ({
      users: '用户与权限',
      settings: '全局设置',
      logs: '诊断日志',
      auth: '账号与注册',
      identities: '第三方账户',
      gameplay: '玩法能力',
      content: '内置内容',
      challenges: '挑战内容',
      sharing: '分享治理',
    })[props.section] || '管理',
)

function sectionVisible(name: string) {
  return props.section === 'all' || props.section === name
}
const settings = ref<AdminSystemSettings | null>(null)
const publicLLMKey = ref('')
const clearPublicLLMKey = ref(false)
const publicModels = ref<string[]>([])
const fetchingPublicModels = ref(false)
const publicDiagnostics = ref<Partial<Record<LLMCapabilityRole, LLMCapabilityDiagnostic>>>({})
const checkingPublicRoles = ref<Partial<Record<LLMCapabilityRole, boolean>>>({})
const savedPublicCapabilityKey = ref('')
const publicLLMExpandedNames = ref<string[]>(['public-llm'])
// 接口协议始终提供两项，由用户手动选择，服务端永不改写；档位只决定
// 可选字段的取舍与提前拦截（识别结果见 public_llm_resolved_profile）。
const publicProtocolOptions = [
  { label: 'Chat Completions', value: 'chat_completions' },
  { label: 'Responses', value: 'responses' },
]
// 已保存的公共 base_url 快照：表单改了没保存时识别结果已过期，不展示。
const savedPublicBaseURL = ref('')
const publicProfileNames: Record<string, string> = {
  openai: 'OpenAI 官方',
  deepseek: 'DeepSeek 官方',
  openrouter: 'OpenRouter',
}
const publicRecognizedHint = computed(() => {
  const profile = settings.value?.public_llm_resolved_profile
  if (!profile) return ''
  const unchanged =
    String(settings.value?.public_llm_base_url || '').trim() ===
    savedPublicBaseURL.value.trim()
  if (!unchanged || profile.id === 'custom_openai_compatible') return ''
  return `已识别为 ${publicProfileNames[profile.id] || profile.id}，已自动适配请求参数`
})
const llmLogs = ref<AdminLLMRequestLogSummary[]>([])
const selectedLLMLog = ref<AdminLLMRequestLog | null>(null)
const contextLogs = ref<AdminContextBuildLogSummary[]>([])
const selectedContextLog = ref<AdminContextBuildLog | null>(null)
const authSettings = ref<AdminAuthSettings | null>(null)
const authIdentities = ref<AdminExternalIdentity[]>([])
const smtpPassword = ref('')
const clearSMTPPassword = ref(false)
const testEmail = ref('')
const invitations = ref<InvitationCode[]>([])
const packs = ref<AdminChallengePack[]>([])
const builtins = ref<AdminBuiltinChallenge[]>([])
const modules = ref<GameplayModuleDescriptor[]>([])
const rewardDefinitions = ref<RewardDefinition[]>([])
const publicShares = ref<ConversationShare[]>([])
const shareReports = ref<ConversationShareReport[]>([])
const loadingSettings = ref(false)
const loadingLLMLogs = ref(false)
const loadingContextLogs = ref(false)
const loadingAuthSettings = ref(false)
const loadingAuthIdentities = ref(false)
const loadingInvitations = ref(false)
const loadingChallenges = ref(false)
const loadingBuiltins = ref(false)
const loadingGameplay = ref(false)
const loadingShareGovernance = ref(false)
const savingSettings = ref(false)
const savingAuthSettings = ref(false)
const testingEmail = ref(false)
const creatingInvitation = ref(false)
const updatingInvitation = ref('')
const savingVersion = ref(false)
const publishingVersion = ref(false)
const deletingVersion = ref(false)
const importingBuiltin = ref('')
const versionDoc = ref<ChallengeVersionDocument | null>(null)
const versionPayload = ref<SaveChallengeVersionInput | null>(null)
const selectedVersionId = ref('')
const showPackModal = ref(false)
const showInvitationModal = ref(false)
const showCreatedInvitationModal = ref(false)
const showRewardModal = ref(false)
const showLLMLogModal = ref(false)
const showContextLogModal = ref(false)
const createdInvitationCode = ref('')
const creatingPack = ref(false)
const creatingReward = ref(false)
const packForm = ref({ slug: '', name: '', description: '' })
const showModerateModal = ref(false)
const moderateTarget = ref<ConversationShare | null>(null)
const moderateReason = ref('policy_violation')
const moderatingShare = ref(false)
const showVersionModal = ref(false)
const versionPackId = ref('')
const newVersion = ref('')
const creatingVersion = ref(false)
const invitationForm = ref<{ label: string; max_uses: number; expires_at: number | null }>({
  label: '',
  max_uses: 1,
  expires_at: null,
})
const rewardForm = ref({
  key: '',
  kind: 'currency',
  name: '',
  description: '',
  unit: '',
  icon: '',
  stackable: true,
  consumable: true,
  featured: false,
  sort_order: 100,
})
const rewardKindOptions = [
  { label: '货币', value: 'currency' },
  { label: '物品', value: 'item' },
  { label: '徽章', value: 'badge' },
  { label: '权限', value: 'entitlement' },
  { label: '额度', value: 'credit' },
]

const smtpSecurityOptions = [
  { label: 'STARTTLS（推荐）', value: 'starttls' },
  { label: 'TLS / SSL', value: 'tls' },
  { label: '无加密', value: 'none' },
]

const difficultyOptions: Array<{ label: string; value: DifficultyModeEnum; hint: string }> = [
  { label: '简单', value: 'easy', hint: '成功率 +15%' },
  { label: '普通', value: 'normal', hint: '标准规则' },
  { label: '困难', value: 'hard', hint: '成功率 -15%' },
]

const allVersions = computed(() =>
  packs.value.flatMap((item) => item.versions.map((version) => ({ version, pack: item.pack }))),
)

const publicCapabilityKey = computed(() => {
  const value = settings.value
  if (!value) return ''
  return JSON.stringify([
    value.llm_access_mode,
    value.client_llm_route_enabled,
    value.public_llm_base_url,
    value.public_llm_protocol,
    value.public_llm_model,
    value.public_llm_memory_model,
    value.public_llm_inquiry_model,
    value.public_llm_stream_enabled,
    value.public_llm_reasoning_effort,
    value.public_llm_memory_reasoning_effort,
    value.public_llm_inquiry_reasoning_effort,
    value.public_llm_max_context_tokens,
    value.public_llm_max_response_tokens,
    value.public_llm_temperature,
    value.public_llm_top_p,
    publicLLMKey.value.trim() !== '',
  ])
})

const publicCapabilityDirty = computed(
  () => publicCapabilityKey.value !== savedPublicCapabilityKey.value,
)

const publicLLMConfigured = computed(() => {
  const value = settings.value
  return Boolean(
    value?.llm_access_mode === 'platform_managed' &&
    String(value.public_llm_base_url || '').trim() &&
    value.public_llm_has_api_key &&
    String(value.public_llm_model || '').trim(),
  )
})

function syncPublicLLMExpansion() {
  publicLLMExpandedNames.value = publicLLMConfigured.value ? [] : ['public-llm']
}

watch(publicCapabilityKey, () => {
  publicDiagnostics.value = {}
})

async function loadSettings() {
  loadingSettings.value = true
  try {
    settings.value = await adminApi.getSystemSettings()
    savedPublicBaseURL.value = settings.value.public_llm_base_url || ''
    syncPublicLLMExpansion()
    savedPublicCapabilityKey.value = publicCapabilityKey.value
    publicDiagnostics.value = {}
  } catch (err: any) {
    message.error(`读取管理设置失败：${err.message || '未知错误'}`)
  } finally {
    loadingSettings.value = false
  }
}

async function saveSettings() {
  if (!settings.value) return
  savingSettings.value = true
  try {
    const body: Record<string, unknown> = {
      roll_system_enabled: settings.value.roll_system_enabled,
      roll_difficulty_mode: settings.value.roll_difficulty_mode,
      detailed_llm_logging_enabled: settings.value.detailed_llm_logging_enabled,
      diagnostic_log_cleanup_enabled: settings.value.diagnostic_log_cleanup_enabled,
      diagnostic_log_retention_days: settings.value.diagnostic_log_retention_days,
    }
    if (auth.isOwner) {
      Object.assign(body, {
        llm_access_mode: settings.value.llm_access_mode,
        client_llm_route_enabled: settings.value.client_llm_route_enabled,
        public_llm_base_url: settings.value.public_llm_base_url,
        public_llm_protocol: settings.value.public_llm_protocol,
        public_llm_model: settings.value.public_llm_model,
        public_llm_memory_model: settings.value.public_llm_memory_model,
        public_llm_inquiry_model: settings.value.public_llm_inquiry_model,
        public_llm_max_context_tokens: settings.value.public_llm_max_context_tokens,
        public_llm_max_response_tokens: settings.value.public_llm_max_response_tokens,
        public_llm_temperature: settings.value.public_llm_temperature,
        public_llm_top_p: settings.value.public_llm_top_p,
        public_llm_stream_enabled: settings.value.public_llm_stream_enabled,
        public_llm_reasoning_effort: String(
          settings.value.public_llm_reasoning_effort ?? '',
        ).trim(),
        public_llm_memory_reasoning_effort: String(
          settings.value.public_llm_memory_reasoning_effort ?? '',
        ).trim(),
        public_llm_inquiry_reasoning_effort: String(
          settings.value.public_llm_inquiry_reasoning_effort ?? '',
        ).trim(),
      })
      if (publicLLMKey.value.trim()) body.public_llm_api_key = publicLLMKey.value.trim()
      if (clearPublicLLMKey.value) body.clear_public_llm_api_key = true
    }
    settings.value = await adminApi.updateSystemSettings(body as any)
    syncPublicLLMExpansion()
    publicLLMKey.value = ''
    clearPublicLLMKey.value = false
    savedPublicBaseURL.value = settings.value.public_llm_base_url || ''
    savedPublicCapabilityKey.value = publicCapabilityKey.value
    publicDiagnostics.value = {}
    message.success('玩法设置已保存')
  } catch (err: any) {
    message.error(`保存管理设置失败：${err.message || '未知错误'}`)
  } finally {
    savingSettings.value = false
  }
}

function setPlatformLLMEnabled(enabled: boolean) {
  if (!settings.value) return
  settings.value.llm_access_mode = enabled ? 'platform_managed' : 'user_managed'
  syncPublicLLMExpansion()
}

function setClientRouteEnabled(enabled: boolean) {
  if (!settings.value) return
  if (!enabled && settings.value.client_llm_route_enabled) {
    const platformMode = settings.value.llm_access_mode === 'platform_managed'
    dialog.warning({
      title: '关闭“允许用户选择客户端直连”？',
      content:
        '1. 所有进行中的客户端直连回合会立即中断；\n' +
        '2. 已有客户端直连对话变为只读，重新开启后恢复；\n' +
        (platformMode
          ? '3. 选择过客户端直连的用户会自动回到平台模型，无需填写 Key。'
          : '3. 选择过客户端直连的用户需要填写 API Key 才能继续使用服务端转发。'),
      positiveText: '确认关闭',
      negativeText: '取消',
      onPositiveClick: () => {
        if (settings.value) settings.value.client_llm_route_enabled = false
      },
    })
    return
  }
  settings.value.client_llm_route_enabled = enabled
}

async function checkPublicCapability(role: LLMCapabilityRole) {
  if (!settings.value || settings.value.llm_access_mode !== 'platform_managed') return
  if (publicCapabilityDirty.value) {
    message.warning('请先保存公共 LLM 配置，再执行模型检测')
    return
  }
  checkingPublicRoles.value = { ...checkingPublicRoles.value, [role]: true }
  try {
    // 公共配置诊断走独立的 owner 接口，直接使用平台配置，与 owner 本人的
    // 路由无关（owner 选了客户端直连时用户接口会返回其个人配置）。
    const data = await adminApi.checkPublicCapabilities(role)
    publicDiagnostics.value = { ...publicDiagnostics.value, [role]: data.result }
  } catch (err: any) {
    message.error(`公共模型检测失败：${err.message || '未知错误'}`)
  } finally {
    checkingPublicRoles.value = { ...checkingPublicRoles.value, [role]: false }
  }
}

async function fetchPublicModels() {
  if (!auth.isOwner) {
    message.warning('只有平台拥有者可以获取公共模型列表')
    return
  }
  if (!settings.value?.public_llm_base_url?.trim()) {
    message.warning('请先填写公共模型 Base URL')
    return
  }
  fetchingPublicModels.value = true
  try {
    const data = await adminApi.fetchPublicModels({
      base_url: settings.value.public_llm_base_url,
      api_key: publicLLMKey.value.trim() || undefined,
    })
    publicModels.value = data.models || []
    if (publicModels.value.length) {
      settings.value.public_llm_model = publicModels.value[0]
      if (
        settings.value.public_llm_memory_model &&
        !publicModels.value.includes(settings.value.public_llm_memory_model)
      )
        settings.value.public_llm_memory_model = ''
      if (
        settings.value.public_llm_inquiry_model &&
        !publicModels.value.includes(settings.value.public_llm_inquiry_model)
      )
        settings.value.public_llm_inquiry_model = ''
      message.success(`获取到 ${publicModels.value.length} 个公共模型`)
    } else message.warning('未找到可用模型')
  } catch (err: any) {
    message.error(`获取公共模型列表失败：${err.message || '未知错误'}`)
    publicModels.value = []
  } finally {
    fetchingPublicModels.value = false
  }
}

async function loadLLMLogs() {
  loadingLLMLogs.value = true
  try {
    llmLogs.value = await adminApi.listLLMRequestLogs({ limit: 50 })
  } catch (err: any) {
    message.error(`读取模型请求日志失败：${err.message || '未知错误'}`)
  } finally {
    loadingLLMLogs.value = false
  }
}

async function openLLMLog(row: AdminLLMRequestLogSummary) {
  try {
    selectedLLMLog.value = await adminApi.getLLMRequestLog(row.id)
    showLLMLogModal.value = true
  } catch (err: any) {
    message.error(`读取请求明细失败：${err.message || '未知错误'}`)
  }
}

async function loadContextLogs() {
  loadingContextLogs.value = true
  try {
    contextLogs.value = await adminApi.listContextBuildLogs({ limit: 50 })
  } catch (err: any) {
    message.error(`读取上下文构建日志失败：${err.message || '未知错误'}`)
  } finally {
    loadingContextLogs.value = false
  }
}

async function loadShareGovernance() {
  loadingShareGovernance.value = true
  try {
    ;[publicShares.value, shareReports.value] = await Promise.all([
      adminApi.listConversationShares({ limit: 50 }),
      adminApi.listShareReports({ status: 'open', limit: 50 }),
    ])
  } catch (err: any) {
    message.error(`读取分享治理数据失败：${err.message || '未知错误'}`)
  } finally {
    loadingShareGovernance.value = false
  }
}

function moderateShare(item: ConversationShare) {
  moderateTarget.value = item
  moderateReason.value = 'policy_violation'
  showModerateModal.value = true
}

async function submitModerateShare() {
  const target = moderateTarget.value
  const reason = moderateReason.value.trim()
  if (!target || !reason) return
  moderatingShare.value = true
  try {
    await adminApi.moderateConversationShare(target.id, reason)
    target.status = 'moderated'
    target.moderated_at = new Date().toISOString()
    showModerateModal.value = false
    message.success('分享已下架')
  } catch (err: any) {
    message.error(`下架失败：${err.message || '未知错误'}`)
  } finally {
    moderatingShare.value = false
  }
}

async function resolveShareReport(item: ConversationShareReport, status: 'resolved' | 'dismissed') {
  try {
    await adminApi.resolveShareReport(item.id, status)
    item.status = status
    shareReports.value = shareReports.value.filter((entry) => entry.id !== item.id)
  } catch (err: any) {
    message.error(`处理举报失败：${err.message || '未知错误'}`)
  }
}

async function openContextLog(row: AdminContextBuildLogSummary) {
  try {
    selectedContextLog.value = await adminApi.getContextBuildLog(row.id)
    showContextLogModal.value = true
  } catch (err: any) {
    message.error(`读取上下文构建明细失败：${err.message || '未知错误'}`)
  }
}

function llmLogStatus(row: AdminLLMRequestLogSummary) {
  if (!row.transport_success) return '传输失败'
  if (row.output_mode && !row.structured_success) return '结构失败'
  if (row.stream && !row.received_done) return '未收到结束标记'
  return '成功'
}

function llmLogStatusType(row: AdminLLMRequestLogSummary): 'error' | 'warning' | 'success' {
  if (!row.transport_success || (row.output_mode && !row.structured_success)) return 'error'
  if (row.stream && !row.received_done) return 'warning'
  return 'success'
}

function llmLogPurposeLabel(purpose: string) {
  return (
    {
      narrative: '主叙事',
      memory: '长期记忆',
      character_inquiry: '角色询问',
    }[purpose] || purpose
  )
}

async function loadAuthSettings() {
  loadingAuthSettings.value = true
  try {
    authSettings.value = await adminApi.getAuthSettings()
    smtpPassword.value = ''
    clearSMTPPassword.value = false
  } catch (err: any) {
    message.error(`读取认证设置失败：${err.message || '未知错误'}`)
  } finally {
    loadingAuthSettings.value = false
  }
}

async function loadAuthIdentities() {
  loadingAuthIdentities.value = true
  try {
    authIdentities.value = await adminApi.listAuthIdentities()
  } catch (err: any) {
    message.error(`读取第三方账户失败：${err.message || '未知错误'}`)
  } finally {
    loadingAuthIdentities.value = false
  }
}

async function saveAuthSettings() {
  if (!auth.isOwner) {
    message.warning('只有平台拥有者可以修改认证设置')
    return
  }
  if (!authSettings.value) return
  savingAuthSettings.value = true
  try {
    authSettings.value = await adminApi.updateAuthSettings({
      ...authSettings.value,
      smtp_password: smtpPassword.value,
      clear_smtp_password: clearSMTPPassword.value,
    })
    smtpPassword.value = ''
    clearSMTPPassword.value = false
    message.success('账号与注册设置已保存')
  } catch (err: any) {
    message.error(`保存认证设置失败：${err.message || '未知错误'}`)
  } finally {
    savingAuthSettings.value = false
  }
}

function onSMTPPasswordInput(value: string) {
  smtpPassword.value = value
  if (value) clearSMTPPassword.value = false
}

function markSMTPPasswordForClearing() {
  smtpPassword.value = ''
  clearSMTPPassword.value = true
}

async function sendTestEmail() {
  if (!auth.isOwner) {
    message.warning('只有平台拥有者可以测试发信设置')
    return
  }
  if (!testEmail.value.trim()) {
    message.warning('请填写测试收件邮箱')
    return
  }
  testingEmail.value = true
  try {
    await adminApi.testEmail(testEmail.value)
    message.success('测试邮件已发送')
  } catch (err: any) {
    message.error(`测试邮件发送失败：${err.message || '未知错误'}`)
  } finally {
    testingEmail.value = false
  }
}

async function loadInvitations() {
  loadingInvitations.value = true
  try {
    invitations.value = await adminApi.listInvitationCodes()
  } catch (err: any) {
    message.error(`读取邀请码失败：${err.message || '未知错误'}`)
  } finally {
    loadingInvitations.value = false
  }
}

async function createInvitation() {
  creatingInvitation.value = true
  try {
    const created = await adminApi.createInvitationCode({
      label: invitationForm.value.label,
      max_uses: invitationForm.value.max_uses,
      ...(invitationForm.value.expires_at
        ? { expires_at: new Date(invitationForm.value.expires_at).toISOString() }
        : {}),
    })
    createdInvitationCode.value = created.code
    showInvitationModal.value = false
    showCreatedInvitationModal.value = true
    invitationForm.value = { label: '', max_uses: 1, expires_at: null }
    await loadInvitations()
  } catch (err: any) {
    message.error(`创建邀请码失败：${err.message || '未知错误'}`)
  } finally {
    creatingInvitation.value = false
  }
}

async function updateInvitation(item: InvitationCode, enabled: boolean) {
  updatingInvitation.value = item.id
  try {
    const updated = await adminApi.updateInvitationCode(item.id, {
      label: item.label,
      enabled,
      max_uses: item.max_uses,
      expires_at: item.expires_at || null,
    })
    const index = invitations.value.findIndex((entry) => entry.id === item.id)
    if (index >= 0) invitations.value[index] = updated
  } catch (err: any) {
    message.error(`更新邀请码失败：${err.message || '未知错误'}`)
  } finally {
    updatingInvitation.value = ''
  }
}

async function copyInvitationCode() {
  try {
    await navigator.clipboard.writeText(createdInvitationCode.value)
    message.success('邀请码已复制')
  } catch {
    message.warning('浏览器未允许复制，请手动复制')
  }
}

function invitationStatus(item: InvitationCode) {
  if (!item.enabled) return '已停用'
  if (item.expires_at && new Date(item.expires_at).getTime() <= Date.now()) return '已过期'
  if (item.max_uses > 0 && item.used_count >= item.max_uses) return '已用完'
  return '可使用'
}

async function loadChallenges() {
  loadingChallenges.value = true
  try {
    packs.value = await adminApi.listChallenges()
  } catch (err: any) {
    message.error(`读取挑战内容失败：${err.message || '未知错误'}`)
  } finally {
    loadingChallenges.value = false
  }
}

async function loadBuiltins() {
  loadingBuiltins.value = true
  try {
    builtins.value = await adminApi.listBuiltins()
  } catch (err: any) {
    message.error(`读取内置内容失败：${err.message || '未知错误'}`)
  } finally {
    loadingBuiltins.value = false
  }
}

function builtinStatusLabel(status: AdminBuiltinChallenge['status']) {
  if (status === 'imported') return '已导入'
  if (status === 'update_available') return '内容有更新'
  return '可导入'
}

function builtinStatusType(status: AdminBuiltinChallenge['status']): 'success' | 'warning' {
  if (status === 'imported') return 'success'
  return 'warning'
}

async function importBuiltin(item: AdminBuiltinChallenge) {
  const overwrite = item.status === 'update_available' && item.overwritable
  if (!item.importable && !overwrite) return
  const confirmation = overwrite
    ? `覆盖数据库中的「${item.name}」并以仓库 v${item.version} 作为当前内容？已被使用的同版本快照不会被覆盖；更新已使用内容请先提升版本号。`
    : `导入并发布「${item.name}」v${item.version}？`
  dialog.warning({
    title: overwrite ? '覆盖挑战内容？' : '导入并发布？',
    content: confirmation,
    positiveText: overwrite ? '确认覆盖' : '确认导入',
    negativeText: '取消',
    onPositiveClick: async () => {
      importingBuiltin.value = item.slug
      try {
        await adminApi.importBuiltin(item.slug, overwrite)
        await Promise.all([loadBuiltins(), loadChallenges()])
        message.success(
          overwrite
            ? `已用仓库内容覆盖 ${item.name} v${item.version}`
            : `已导入并发布 ${item.name} v${item.version}`,
        )
      } catch (err: any) {
        message.error(`${overwrite ? '覆盖' : '导入'}失败：${err.message || '未知错误'}`)
      } finally {
        importingBuiltin.value = ''
      }
    },
  })
}

async function openVersion(versionId: string) {
  selectedVersionId.value = versionId
  try {
    setVersionDocument(await adminApi.getChallengeVersion(versionId))
  } catch (err: any) {
    message.error(`读取版本失败：${err.message || '未知错误'}`)
  }
}

async function saveVersion() {
  if (!versionDoc.value || !versionPayload.value) return
  savingVersion.value = true
  try {
    setVersionDocument(
      await adminApi.saveChallengeVersion(versionDoc.value.version.id, versionPayload.value),
    )
    await loadChallenges()
    message.success('草稿已保存')
  } catch (err: any) {
    message.error(`保存草稿失败：${err.message || '未知错误'}`)
  } finally {
    savingVersion.value = false
  }
}

async function publishVersion() {
  if (!versionDoc.value || !versionPayload.value) return
  publishingVersion.value = true
  try {
    // 发布按钮同时保存当前表单，避免管理员误发布数据库中的旧草稿。
    await adminApi.saveChallengeVersion(versionDoc.value.version.id, versionPayload.value)
    setVersionDocument(await adminApi.publishChallengeVersion(versionDoc.value.version.id))
    await loadChallenges()
    message.success('版本已发布，新对话将使用它')
  } catch (err: any) {
    message.error(`发布失败：${err.message || '未知错误'}`)
  } finally {
    publishingVersion.value = false
  }
}

async function deleteVersion() {
  if (!versionDoc.value) return
  const label = `${versionDoc.value.version.version}（${versionDoc.value.version.status}）`
  dialog.warning({
    title: `删除版本 ${label}？`,
    content: '仅当没有任何对话或存档引用它时才能删除。',
    positiveText: '删除',
    negativeText: '取消',
    onPositiveClick: async () => {
      if (!versionDoc.value) return
      deletingVersion.value = true
      try {
        await adminApi.deleteChallengeVersion(versionDoc.value.version.id)
        versionDoc.value = null
        versionPayload.value = null
        selectedVersionId.value = ''
        await loadChallenges()
        message.success('版本已删除')
      } catch (err: any) {
        message.error(`删除失败：${err.message || '未知错误'}`)
      } finally {
        deletingVersion.value = false
      }
    },
  })
}

function setVersionDocument(doc: ChallengeVersionDocument) {
  versionDoc.value = doc
  const pack = packs.value.find((item) => item.pack.id === doc.version.challenge_pack_id)?.pack
  const manifest = structuredClone(doc.version.manifest || {}) as Record<string, any>
  manifest.id ||= pack?.slug || ''
  manifest.name ||= pack?.name || ''
  manifest.description ??= pack?.description || ''
  manifest.version = doc.version.version
  manifest.author ||= ''
  manifest.icon ||= ''
  manifest.tags ||= []
  manifest.features ||= []
  manifest.dependencies ||= []
  manifest.quick_start_guide ||= ''
  manifest.default_config ||= {
    recommended_character: '',
    recommended_persona: '',
    recommended_scenario: '',
    recommended_prompt: '',
    required_worldbooks: [],
    allowed_personas: [],
  }
  manifest.default_config.required_worldbooks ||= []
  manifest.default_config.allowed_personas ||= []
  manifest.entry_requirement ||= {}

  const runtime = structuredClone(doc.runtime) as any
  runtime.state_schema ||= { schema_version: 1, fields: [] }
  runtime.state_schema.fields ||= []
  runtime.action_config ||= { model: [], player: [], capabilities: {} }
  runtime.action_config.model ||= []
  runtime.action_config.player ||= []
  runtime.action_config.capabilities ||= {}
  runtime.settlement_config ||= { handler: 'core.challenge_completion' }
  runtime.presentation ||= {}
  runtime.presentation.state_panel_title ||= '状态'
  runtime.presentation.state_sections ||= []
  runtime.presentation.completion_view ||= {}
  runtime.presentation.terminology ||= {}
  runtime.presentation.value_labels ||= {}
  if (runtime.mode === 'campaign') {
    runtime.presentation.campaign_view ||= {
      renderer: 'generic',
      sections: [],
      actions: [],
      empty_states: {},
    }
  }

  versionPayload.value = {
    manifest,
    runtime,
    assets: structuredClone(doc.assets || []),
    definitions: structuredClone(doc.definitions || []),
    outcomes: structuredClone(doc.outcomes || []),
    reward_rules: structuredClone(doc.reward_rules || []),
  }
}

async function createPack() {
  if (!packForm.value.slug || !packForm.value.name) {
    message.warning('请填写 slug 和名称')
    return
  }
  creatingPack.value = true
  try {
    await adminApi.createChallenge(packForm.value)
    showPackModal.value = false
    packForm.value = { slug: '', name: '', description: '' }
    await loadChallenges()
    message.success('挑战包已创建')
  } catch (err: any) {
    message.error(`创建挑战包失败：${err.message || '未知错误'}`)
  } finally {
    creatingPack.value = false
  }
}

function createVersion(packId: string) {
  versionPackId.value = packId
  newVersion.value = ''
  showVersionModal.value = true
}

async function submitCreateVersion() {
  const version = newVersion.value.trim()
  if (!version) {
    message.warning('请输入新版本号')
    return
  }
  creatingVersion.value = true
  try {
    const doc = await adminApi.createChallengeVersion(versionPackId.value, version)
    showVersionModal.value = false
    await loadChallenges()
    await openVersion(doc.version.id)
    message.success('草稿版本已创建')
  } catch (err: any) {
    message.error(`创建版本失败：${err.message || '未知错误'}`)
  } finally {
    creatingVersion.value = false
  }
}

async function loadGameplayCatalog() {
  loadingGameplay.value = true
  try {
    ;[modules.value, rewardDefinitions.value] = await Promise.all([
      adminApi.listGameplayModules(),
      adminApi.listRewardDefinitions(),
    ])
  } catch (err: any) {
    message.error(`读取玩法能力失败：${err.message || '未知错误'}`)
  } finally {
    loadingGameplay.value = false
  }
}

async function createRewardDefinition() {
  if (!rewardForm.value.key.trim() || !rewardForm.value.name.trim()) {
    message.warning('请填写奖励 key 和名称')
    return
  }
  creatingReward.value = true
  try {
    await adminApi.createRewardDefinition({
      ...rewardForm.value,
      kind: rewardForm.value.kind as RewardDefinition['kind'],
      metadata: {},
    })
    showRewardModal.value = false
    rewardForm.value = {
      key: '',
      kind: 'currency',
      name: '',
      description: '',
      unit: '',
      icon: '',
      stackable: true,
      consumable: true,
      featured: false,
      sort_order: 100,
    }
    await loadGameplayCatalog()
    message.success('奖励定义已创建')
  } catch (err: any) {
    message.error(`创建奖励失败：${err.message || '未知错误'}`)
  } finally {
    creatingReward.value = false
  }
}

watch(
  () => props.section,
  (section) => {
    activeTab.value = tabNameForSection(section)
    if (section === 'all' || section === 'settings') void loadSettings()
    if (section === 'all' || section === 'logs') {
      void loadLLMLogs()
      void loadContextLogs()
    }
    if (section === 'all' || section === 'auth') {
      void loadAuthSettings()
      void loadInvitations()
    }
    if (section === 'all' || section === 'identities') void loadAuthIdentities()
    if (section === 'all' || section === 'challenges') void loadChallenges()
    if (section === 'all' || section === 'content') void loadBuiltins()
    if (section === 'all' || section === 'gameplay') void loadGameplayCatalog()
    if (section === 'all' || section === 'sharing') void loadShareGovernance()
  },
  { immediate: true },
)
</script>

<template>
  <div class="page">
    <div class="page-title">
      <h2>{{ section === 'all' ? '管理' : sectionTitle }}</h2>
      <span class="ornament">◆</span>
    </div>
    <p class="page-sub">平台规则与挑战内容都由管理员发布，已发布版本不可原地修改。</p>

    <NTabs
      v-model:value="activeTab"
      type="line"
      animated
      :class="{ 'single-section-tabs': section !== 'all' }"
    >
      <NTabPane v-if="sectionVisible('users')" name="users" tab="用户与权限">
        <AdminUsersPanel />
      </NTabPane>
      <NTabPane v-if="sectionVisible('settings')" name="settings" tab="全局设置">
        <section v-if="sectionVisible('settings')" class="admin-section">
          <h3>
            <FieldHelp label="天命判定">
              <p>
                控制玩法中的概率判定系统。启用后，挑战动作可以按照平台规则进行成功、失败或难度判定。
              </p>
              <p>普通用户只能参与判定，不能修改这个开关或平台难度。</p>
            </FieldHelp>
          </h3>
          <p class="section-sub">普通用户只能参与判定，无法覆盖平台难度或开关。</p>
          <NSpin :show="loadingSettings">
            <NForm v-if="settings" label-placement="top">
              <NFormItem label="判定系统">
                <NSwitch v-model:value="settings.roll_system_enabled">
                  <template #checked>已启用</template>
                  <template #unchecked>已停用</template>
                </NSwitch>
              </NFormItem>
              <NFormItem>
                <template #label>
                  <FieldHelp label="挑战难度">
                    <p>调整平台判定的整体难度，会影响需要进行概率判定的挑战动作。</p>
                    <p>简单、普通、困难分别对应更高、标准和更低的成功倾向。</p>
                  </FieldHelp>
                </template>
                <NRadioGroup v-model:value="settings.roll_difficulty_mode">
                  <NRadioButton
                    v-for="option in difficultyOptions"
                    :key="option.value"
                    :value="option.value"
                  >
                    {{ option.label }}
                  </NRadioButton>
                </NRadioGroup>
              </NFormItem>
              <NFormItem>
                <template #label>
                  <FieldHelp label="自动清理诊断日志">
                    <p>
                      定期分批清理超过保留期限的模型请求日志和上下文构建日志，避免大字段长期占用数据库空间。
                    </p>
                    <p>分享治理审计记录不受此策略影响；关闭开关不会删除现有日志。</p>
                  </FieldHelp>
                </template>
                <NSwitch v-model:value="settings.diagnostic_log_cleanup_enabled">
                  <template #checked>已启用</template>
                  <template #unchecked>已关闭</template>
                </NSwitch>
              </NFormItem>
              <NFormItem v-if="settings.diagnostic_log_cleanup_enabled" label="诊断日志保留天数">
                <NInputNumber
                  v-model:value="settings.diagnostic_log_retention_days"
                  :min="1"
                  :max="3650"
                  :precision="0"
                />
                <template #feedback>
                  默认保留 30 天；清理任务每小时检查一次，每次只做有限批次，避免形成数据库峰值。
                </template>
              </NFormItem>
              <NFormItem>
                <template #label>
                  <FieldHelp label="模型请求详细日志">
                    <p>记录模型请求体、原始响应和处理结果，用于排查模型协议、超时和上下文问题。</p>
                    <p>不会记录 API Key 或认证头；日志可能包含剧情上下文，排查完成后建议关闭。</p>
                  </FieldHelp>
                </template>
                <NSwitch v-model:value="settings.detailed_llm_logging_enabled">
                  <template #checked>记录中</template>
                  <template #unchecked>已关闭</template>
                </NSwitch>
                <template #feedback>
                  开启后保存完整模型请求与原始响应；不会记录 API Key 或认证头。
                </template>
              </NFormItem>
              <NFormItem>
                <template #label>
                  <FieldHelp label="启用平台公共 LLM">
                    <p>开启后，所有用户统一使用本页下方配置的公共模型服务。</p>
                    <p>关闭后，用户改用各自的 LLM 配置；切换不会删除用户原有设置。</p>
                  </FieldHelp>
                </template>
                <NSwitch
                  :value="settings.llm_access_mode === 'platform_managed'"
                  :disabled="!auth.isOwner"
                  @update:value="setPlatformLLMEnabled"
                >
                  <template #checked>已启用</template>
                  <template #unchecked>未启用</template>
                </NSwitch>
                <template #feedback>
                  关闭时默认使用用户自己的配置；开启后所有用户统一使用下面的公共连接，切回关闭不会删除用户原有配置。
                </template>
              </NFormItem>
              <NFormItem>
                <template #label>
                  <FieldHelp label="允许用户选择客户端直连">
                    <p>开启后，用户可以在设置中选择由其浏览器直接发出模型请求，API Key 只保存在用户设备。</p>
                    <p>开启公共 LLM 时，用户可在平台模型与客户端直连之间选择。客户端直连使用用户自己的 Key，不消耗平台模型额度。</p>
                    <p>这类对话和 Campaign 不发放任何奖励，重新生成不消耗额度。</p>
                    <p>发给模型的完整提示词（含隐藏剧情）在用户浏览器中可见。</p>
                    <p>是否可用取决于各模型服务商是否允许浏览器跨域访问。</p>
                  </FieldHelp>
                </template>
                <NSwitch
                  :value="settings.client_llm_route_enabled"
                  :disabled="!auth.isOwner"
                  @update:value="setClientRouteEnabled"
                >
                  <template #checked>已允许</template>
                  <template #unchecked>未允许</template>
                </NSwitch>
                <template #feedback>
                  默认关闭。开启后用户自行决定是否直连；关闭会立即中断进行中的直连回合，已有直连对话变为只读，重新开启后恢复。
                </template>
              </NFormItem>
            </NForm>
          </NSpin>
          <NCollapse
            v-if="settings && settings.llm_access_mode === 'platform_managed'"
            v-model:expanded-names="publicLLMExpandedNames"
            class="admin-subgroup public-llm-collapse"
          >
            <NCollapseItem name="public-llm">
              <template #header>
                <div>
                  <h3 id="public-llm-heading">公共 LLM 配置</h3>
                  <p class="section-sub">由上方开关启用，配置只在平台公共模式下生效。</p>
                </div>
              </template>
              <template #header-extra>
                <NTag v-if="settings.public_llm_has_api_key" type="success">已配置 Key</NTag>
              </template>
              <div class="public-llm-content">
                <NForm label-placement="top" size="small" :disabled="!auth.isOwner">
                  <NFormItem label="公共 Base URL">
                    <div class="protocol-setting">
                      <NInput
                        v-model:value="settings.public_llm_base_url"
                        placeholder="https://api.openai.com/v1"
                      />
                      <div class="protocol-setting__hint">
                        填谁的地址就用谁的服务（官方、OpenRouter、中转站、本地模型均可）。OpenAI、DeepSeek、OpenRouter
                        官方地址会自动适配。
                      </div>
                    </div>
                  </NFormItem>
                  <NFormItem label="接口协议">
                    <div class="protocol-setting">
                      <NSelect
                        v-model:value="settings.public_llm_protocol"
                        :options="publicProtocolOptions"
                      />
                      <div class="protocol-setting__hint">
                        <template v-if="publicRecognizedHint">{{ publicRecognizedHint }}</template>
                        <template v-else>
                          按所选协议请求。不确定时先用“测试连接”或能力诊断确认。
                        </template>
                      </div>
                    </div>
                  </NFormItem>
                  <NFormItem label="公共 API Key">
                    <div class="public-key-control">
                      <NInput
                        v-model:value="publicLLMKey"
                        :disabled="clearPublicLLMKey"
                        type="password"
                        show-password-on="click"
                        placeholder="留空保持现有 Key"
                      />
                      <NButton
                        v-if="settings.public_llm_has_api_key"
                        type="error"
                        secondary
                        @click="clearPublicLLMKey = !clearPublicLLMKey"
                      >
                        {{ clearPublicLLMKey ? '取消清除' : '清除 Key' }}
                      </NButton>
                    </div>
                  </NFormItem>
                  <div class="model-setting-list">
                    <LLMModelSetting
                      v-model="settings.public_llm_model"
                      v-model:reasoning-effort="settings.public_llm_reasoning_effort"
                      :options="publicModels"
                      role="narrative"
                      label="公共叙事模型"
                      :diagnostic="publicDiagnostics.narrative"
                      :checking="checkingPublicRoles.narrative"
                      placeholder="可直接输入模型名称"
                      @detect="checkPublicCapability('narrative')"
                    >
                      <template #model-label-extra>
                        <NButton
                          text
                          type="primary"
                          size="tiny"
                          :disabled="!auth.isOwner"
                          :loading="fetchingPublicModels"
                          @click="fetchPublicModels"
                        >
                          获取模型列表
                        </NButton>
                      </template>
                    </LLMModelSetting>

                    <LLMModelSetting
                      v-model="settings.public_llm_memory_model"
                      v-model:reasoning-effort="settings.public_llm_memory_reasoning_effort"
                      :options="publicModels"
                      role="memory"
                      label="公共长期记忆模型"
                      :diagnostic="publicDiagnostics.memory"
                      :checking="checkingPublicRoles.memory"
                      placeholder="留空继承叙事模型"
                      @detect="checkPublicCapability('memory')"
                    />

                    <LLMModelSetting
                      v-model="settings.public_llm_inquiry_model"
                      v-model:reasoning-effort="settings.public_llm_inquiry_reasoning_effort"
                      :options="publicModels"
                      role="character_inquiry"
                      label="公共角色问答模型"
                      :diagnostic="publicDiagnostics.character_inquiry"
                      :checking="checkingPublicRoles.character_inquiry"
                      placeholder="留空继承叙事模型"
                      @detect="checkPublicCapability('character_inquiry')"
                    />
                  </div>
                  <div class="admin-llm-grid">
                    <NFormItem>
                      <template #label>
                        <FieldHelp label="最大上下文 token">
                          <p>
                            限制一次请求可使用的上下文总预算，包括系统提示、世界书、历史消息和模型回复。过小会丢失较早内容，过大可能超出模型窗口并增加成本。
                          </p>
                        </FieldHelp>
                      </template>
                      <NInputNumber
                        v-model:value="settings.public_llm_max_context_tokens"
                        :min="4096"
                        :max="2000000"
                      />
                    </NFormItem>
                    <NFormItem>
                      <template #label>
                        <FieldHelp label="最大回复 token">
                          <p>
                            限制模型单次回复的最大
                            token，不代表模型一定会生成这么长。该值需要小于上下文预算，并为协议和上下文留出空间。
                          </p>
                        </FieldHelp>
                      </template>
                      <NInputNumber
                        v-model:value="settings.public_llm_max_response_tokens"
                        :min="128"
                      />
                    </NFormItem>
                    <NFormItem>
                      <template #label>
                        <FieldHelp label="Temperature">
                          <p>
                            控制输出的随机性。值越高，输出越有创造力和不可预测性；值越低，输出越稳定和确定。
                          </p>
                          <p>推荐范围：0.7-1.0（叙事）/ 0.2-0.5（逻辑任务）。</p>
                        </FieldHelp>
                      </template>
                      <NInputNumber
                        v-model:value="settings.public_llm_temperature"
                        :min="0"
                        :max="2"
                        :step="0.1"
                      />
                    </NFormItem>
                    <NFormItem>
                      <template #label>
                        <FieldHelp label="Top P">
                          <p>
                            核采样（Nucleus
                            Sampling）控制候选词汇的范围。值越小，模型越倾向于选择最可能的词汇；值越大，候选范围越广。
                          </p>
                          <p>推荐范围：0.9-1.0，通常不要和 Temperature 同时大幅调整。</p>
                        </FieldHelp>
                      </template>
                      <NInputNumber
                        v-model:value="settings.public_llm_top_p"
                        :min="0"
                        :max="1"
                        :step="0.05"
                      />
                    </NFormItem>
                  </div>
                  <NFormItem>
                    <template #label>
                      <FieldHelp label="流式输出">
                        <p>
                          开启后模型会边生成边返回内容，界面可以逐步显示；关闭后等待完整回复再展示。
                        </p>
                        <p>
                          流式输出不会改变模型能力或最大
                          token；如果服务商的流式接口不稳定，可以关闭。
                        </p>
                      </FieldHelp>
                    </template>
                    <NSwitch v-model:value="settings.public_llm_stream_enabled" />
                  </NFormItem>
                </NForm>
              </div>
            </NCollapseItem>
          </NCollapse>
          <div class="actions">
            <NButton type="primary" :loading="savingSettings" @click="saveSettings"
              >保存全局设置</NButton
            >
          </div>
        </section>
      </NTabPane>

      <NTabPane v-if="sectionVisible('logs')" name="logs" tab="诊断日志">
        <section class="admin-section">
          <div class="section-toolbar">
            <div>
              <h3>模型请求明细</h3>
              <p class="section-sub">最近 50 次模型调用，流式降级与重试会分别记录。</p>
            </div>
            <NButton secondary :loading="loadingLLMLogs" @click="loadLLMLogs">刷新</NButton>
          </div>
          <NSpin :show="loadingLLMLogs">
            <div v-if="!llmLogs.length" class="empty-copy">暂无模型请求日志。</div>
            <div v-for="row in llmLogs" :key="row.id" class="llm-log-row">
              <div class="llm-log-main">
                <div class="llm-log-title">
                  <NTag size="small" :bordered="false">{{ llmLogPurposeLabel(row.purpose) }}</NTag>
                  <strong>{{ row.model }}</strong>
                </div>
                <span class="muted">
                  {{ new Date(row.created_at).toLocaleString() }} ·
                  {{ row.stream ? '流式' : '非流式' }} · {{ row.duration_ms }} ms
                  <template v-if="row.response_status"> · HTTP {{ row.response_status }}</template>
                  <template v-if="row.finish_reason"> · {{ row.finish_reason }}</template>
                </span>
                <span v-if="row.conversation_id" class="hash-line">
                  对话 {{ row.conversation_id }}
                </span>
                <span v-if="row.error" class="llm-log-error">{{ row.error }}</span>
              </div>
              <div class="llm-log-actions">
                <NTag size="small" :type="llmLogStatusType(row)">{{ llmLogStatus(row) }}</NTag>
                <NButton size="small" secondary @click="openLLMLog(row)">查看</NButton>
              </div>
            </div>
          </NSpin>
        </section>

        <section class="admin-section">
          <div class="section-toolbar">
            <div>
              <h3>上下文构建明细</h3>
              <p class="section-sub">最近 50 次叙事请求的 Prompt 分段、裁剪和引用诊断。</p>
            </div>
            <NButton secondary :loading="loadingContextLogs" @click="loadContextLogs">刷新</NButton>
          </div>
          <NSpin :show="loadingContextLogs">
            <div v-if="!contextLogs.length" class="empty-copy">暂无上下文构建记录。</div>
            <div v-for="row in contextLogs" :key="row.id" class="llm-log-row">
              <div class="llm-log-main">
                <div class="llm-log-title">
                  <NTag size="small" :bordered="false">Prompt</NTag>
                  <strong>{{ row.model }}</strong>
                </div>
                <span class="muted">
                  {{ new Date(row.created_at).toLocaleString() }} · {{ row.used_prompt_tokens }} /
                  {{ row.max_context_tokens }} token · 历史 {{ row.included_message_count }} 条 ·
                  世界书 {{ row.activated_world_entry_count }} 条
                </span>
                <span class="hash-line">对话 {{ row.conversation_id }}</span>
              </div>
              <div class="llm-log-actions">
                <NTag v-if="row.memory_lag" size="small" type="warning">记忆滞后</NTag>
                <NTag v-else size="small" type="success">完整</NTag>
                <NButton size="small" secondary @click="openContextLog(row)">查看</NButton>
              </div>
            </div>
          </NSpin>
        </section>
      </NTabPane>

      <NTabPane v-if="sectionVisible('gameplay')" name="gameplay" tab="玩法能力">
        <NSpin :show="loadingGameplay">
          <section class="admin-section">
            <div class="section-toolbar">
              <div>
                <h3>已安装玩法模块</h3>
                <p class="section-sub">挑战版本只能引用这里列出的可信验证器和计算器。</p>
              </div>
              <NButton secondary @click="loadGameplayCatalog">刷新</NButton>
            </div>
            <div
              v-for="module in modules"
              :key="`${module.key}@${module.version}`"
              class="catalog-row"
            >
              <div>
                <strong>{{ module.name }}</strong>
                <span class="muted">{{ module.key }}@{{ module.version }}</span>
                <p>{{ module.description }}</p>
              </div>
              <div class="catalog-tags">
                <NTag v-for="key in module.verifiers" :key="key" size="small">{{ key }}</NTag>
                <NTag v-for="key in module.calculators" :key="key" size="small" type="success">{{
                  key
                }}</NTag>
              </div>
            </div>
          </section>

          <section class="admin-section">
            <div class="section-toolbar">
              <div>
                <h3>奖励定义</h3>
                <p class="section-sub">稳定 key 被挑战引用后，不可改变类型、堆叠和消费语义。</p>
              </div>
              <NButton type="primary" @click="showRewardModal = true">新建奖励</NButton>
            </div>
            <div
              v-for="reward in rewardDefinitions"
              :key="reward.id"
              class="catalog-row reward-row-admin"
            >
              <div>
                <strong>{{ reward.name }}</strong>
                <span class="muted">{{ reward.key }}</span>
                <p>{{ reward.description || '暂无说明' }}</p>
              </div>
              <div class="catalog-tags">
                <NTag size="small">{{ reward.kind }}</NTag>
                <NTag v-if="reward.stackable" size="small" type="info">可堆叠</NTag>
                <NTag v-if="reward.consumable" size="small" type="warning">可消费</NTag>
                <NTag v-if="reward.featured" size="small" type="success">顶栏展示</NTag>
              </div>
            </div>
          </section>
        </NSpin>
      </NTabPane>

      <NTabPane v-if="sectionVisible('auth')" name="auth" tab="账号与注册">
        <NSpin :show="loadingAuthSettings">
          <template v-if="authSettings">
            <section class="admin-section">
              <h3>登录与注册策略</h3>
              <p class="section-sub">设置保存后立即作用于登录和注册接口。</p>
              <div class="setting-list">
                <div class="setting-row">
                  <div>
                    <strong>本地密码登录</strong>
                    <span class="muted">允许已有用户名或邮箱账号使用密码登录</span>
                  </div>
                  <NSwitch v-model:value="authSettings.email_auth_enabled" />
                </div>
                <div class="setting-row">
                  <div>
                    <strong>LinuxDo 登录</strong>
                    <span class="muted">允许使用已绑定的 LinuxDo 账号登录</span>
                  </div>
                  <NSwitch v-model:value="authSettings.linuxdo_login_enabled" />
                </div>
                <div class="setting-row">
                  <div>
                    <strong>邮箱注册</strong>
                    <span class="muted">允许访客创建新的邮箱账号</span>
                  </div>
                  <NSwitch v-model:value="authSettings.email_registration_enabled" />
                </div>
                <div class="setting-row">
                  <div>
                    <strong>邮箱验证码</strong>
                    <span class="muted">注册时验证邮箱所有权</span>
                  </div>
                  <NSwitch v-model:value="authSettings.email_verification_enabled" />
                </div>
                <div class="setting-row">
                  <div>
                    <strong>必须使用邀请码</strong>
                    <span class="muted">注册时消耗一个有效的邀请码名额</span>
                  </div>
                  <NSwitch v-model:value="authSettings.invitation_required" />
                </div>
              </div>
            </section>

            <section class="admin-section">
              <h3>发信邮箱</h3>
              <p class="section-sub">SMTP 密码以密文保存，读取页面时不会回显。</p>
              <NForm class="auth-settings-form" label-placement="top" :disabled="!auth.isOwner">
                <div class="form-grid three-columns">
                  <NFormItem label="SMTP 主机">
                    <NInput v-model:value="authSettings.smtp_host" placeholder="smtp.example.com" />
                  </NFormItem>
                  <NFormItem label="端口">
                    <NInputNumber
                      v-model:value="authSettings.smtp_port"
                      :min="1"
                      :max="65535"
                      style="width: 100%"
                    />
                  </NFormItem>
                  <NFormItem label="连接安全">
                    <NSelect
                      v-model:value="authSettings.smtp_security"
                      :options="smtpSecurityOptions"
                    />
                  </NFormItem>
                </div>
                <div class="form-grid two-columns">
                  <NFormItem label="SMTP 用户名">
                    <NInput v-model:value="authSettings.smtp_username" autocomplete="off" />
                  </NFormItem>
                  <NFormItem label="SMTP 密码">
                    <div class="password-setting">
                      <NInput
                        :value="smtpPassword"
                        type="password"
                        show-password-on="click"
                        autocomplete="new-password"
                        :placeholder="
                          authSettings.has_smtp_password && !clearSMTPPassword
                            ? '已配置，留空不修改'
                            : '输入 SMTP 密码或授权码'
                        "
                        @update:value="onSMTPPasswordInput"
                      />
                      <NButton
                        secondary
                        :disabled="!authSettings.has_smtp_password || clearSMTPPassword"
                        @click="markSMTPPasswordForClearing"
                      >
                        {{ clearSMTPPassword ? '待清除' : '清除' }}
                      </NButton>
                    </div>
                  </NFormItem>
                  <NFormItem label="发件邮箱">
                    <NInput
                      v-model:value="authSettings.sender_email"
                      inputmode="email"
                      placeholder="noreply@example.com"
                    />
                  </NFormItem>
                  <NFormItem label="发件人名称">
                    <NInput v-model:value="authSettings.sender_name" placeholder="Fatecraft" />
                  </NFormItem>
                </div>
              </NForm>
            </section>

            <section class="admin-section">
              <h3>验证码邮件</h3>
              <NForm class="auth-settings-form" label-placement="top" :disabled="!auth.isOwner">
                <NFormItem label="邮件主题">
                  <NInput v-model:value="authSettings.verification_subject" />
                </NFormItem>
                <NFormItem label="邮件正文">
                  <NInput
                    v-model:value="authSettings.verification_template"
                    type="textarea"
                    :autosize="{ minRows: 5, maxRows: 10 }"
                  />
                  <template #feedback>
                    <span v-pre>可用占位符：{{ code }}、{{ expires_minutes }}</span>
                  </template>
                </NFormItem>
                <div class="form-grid two-columns">
                  <NFormItem label="有效期（分钟）">
                    <NInputNumber
                      v-model:value="authSettings.verification_code_ttl_minutes"
                      :min="1"
                      :max="60"
                      style="width: 100%"
                    />
                  </NFormItem>
                  <NFormItem label="重发间隔（秒）">
                    <NInputNumber
                      v-model:value="authSettings.verification_resend_seconds"
                      :min="30"
                      :max="3600"
                      style="width: 100%"
                    />
                  </NFormItem>
                </div>
              </NForm>
              <div class="section-toolbar auth-actions">
                <div class="test-email-row">
                  <NInput v-model:value="testEmail" inputmode="email" placeholder="测试收件邮箱" />
                  <NButton
                    secondary
                    :disabled="!auth.isOwner"
                    :loading="testingEmail"
                    @click="sendTestEmail"
                    >发送测试邮件</NButton
                  >
                </div>
                <NButton
                  type="primary"
                  :disabled="!auth.isOwner"
                  :loading="savingAuthSettings"
                  @click="saveAuthSettings"
                >
                  保存账号设置
                </NButton>
              </div>
            </section>
          </template>
        </NSpin>

        <section class="admin-section">
          <div class="section-toolbar">
            <div>
              <h3>邀请码</h3>
              <p class="section-sub">邀请码原文仅在创建后显示一次。</p>
            </div>
            <NButton type="primary" @click="showInvitationModal = true">生成邀请码</NButton>
          </div>
          <NSpin :show="loadingInvitations">
            <div v-if="!invitations.length" class="empty-copy">还没有邀请码。</div>
            <div v-for="item in invitations" :key="item.id" class="invitation-row">
              <div class="invitation-main">
                <div class="invitation-title">
                  <strong>{{ item.label || '未命名邀请码' }}</strong>
                  <NTag
                    size="small"
                    :type="invitationStatus(item) === '可使用' ? 'success' : 'default'"
                  >
                    {{ invitationStatus(item) }}
                  </NTag>
                </div>
                <span class="muted">
                  尾号 {{ item.code_hint }} · 已使用 {{ item.used_count }} /
                  {{ item.max_uses === 0 ? '不限' : item.max_uses }}
                  <template v-if="item.expires_at">
                    · {{ new Date(item.expires_at).toLocaleString() }} 到期
                  </template>
                </span>
              </div>
              <NSwitch
                :value="item.enabled"
                :loading="updatingInvitation === item.id"
                @update:value="(value) => updateInvitation(item, value)"
              />
            </div>
          </NSpin>
        </section>
      </NTabPane>

      <NTabPane v-if="sectionVisible('identities')" name="identities" tab="第三方账户">
        <section class="admin-section">
          <div class="section-toolbar">
            <div>
              <h3>第三方账户</h3>
              <p class="section-sub">
                这里只审计站内账号与第三方身份的绑定关系；绑定和解绑由用户本人完成。
              </p>
            </div>
            <NButton secondary :loading="loadingAuthIdentities" @click="loadAuthIdentities"
              >刷新</NButton
            >
          </div>
          <NSpin :show="loadingAuthIdentities">
            <div v-if="!authIdentities.length" class="empty-copy">暂时没有第三方账户绑定。</div>
            <div v-for="identity in authIdentities" :key="identity.id" class="invitation-row">
              <div class="invitation-main">
                <div class="invitation-title">
                  <strong>{{ identity.user_name }}</strong>
                  <NTag size="small" type="info">{{
                    identity.provider === 'linuxdo' ? 'LinuxDo' : identity.provider
                  }}</NTag>
                </div>
                <span class="muted">
                  站内账号 {{ identity.user_email || identity.user_id }} · 第三方账号
                  {{ identity.username || identity.provider_user_id }}
                </span>
              </div>
              <span class="muted">{{ new Date(identity.created_at).toLocaleString() }}</span>
            </div>
          </NSpin>
        </section>
      </NTabPane>

      <NTabPane v-if="sectionVisible('content')" name="builtins" tab="内置内容">
        <section class="admin-section">
          <div class="section-toolbar">
            <div>
              <h3>仓库内置挑战</h3>
              <p class="section-sub">
                导入和覆盖都会经过完整发布校验；已使用内容请提升版本号。发布新版本后，旧普通对话会自动封存并可只读浏览。
              </p>
            </div>
            <NButton secondary :loading="loadingBuiltins" @click="loadBuiltins">刷新状态</NButton>
          </div>
          <NSpin :show="loadingBuiltins">
            <div v-if="!builtins.length" class="empty-copy">未发现内置挑战材料。</div>
            <div v-for="item in builtins" :key="item.slug" class="builtin-row">
              <div class="builtin-main">
                <div class="builtin-title">
                  <strong>{{ item.name }}</strong>
                  <NTag size="small" :type="builtinStatusType(item.status)">{{
                    builtinStatusLabel(item.status)
                  }}</NTag>
                </div>
                <span class="muted"
                  >{{ item.slug }} · 仓库 v{{ item.version }} · 数据库
                  {{ item.current_version ? `v${item.current_version}` : '未导入' }}</span
                >
                <p class="builtin-description">{{ item.description }}</p>
                <span class="hash-line">SHA-256：{{ item.content_hash }}</span>
              </div>
              <NButton
                type="primary"
                :disabled="!item.importable && !item.overwritable"
                :loading="importingBuiltin === item.slug"
                @click="importBuiltin(item)"
              >
                {{
                  item.status === 'imported'
                    ? '已发布'
                    : item.status === 'update_available'
                      ? '覆盖现有内容'
                      : '导入并发布'
                }}
              </NButton>
            </div>
          </NSpin>
        </section>
      </NTabPane>

      <NTabPane v-if="sectionVisible('challenges')" name="challenges" tab="挑战内容">
        <section class="admin-section challenge-admin">
          <div class="section-toolbar">
            <div>
              <h3>挑战包与版本</h3>
              <p class="section-sub">先保存草稿，再通过发布校验。新版本不会影响已有对话。</p>
            </div>
            <NButton type="primary" @click="showPackModal = true">新建挑战包</NButton>
          </div>
          <NSpin :show="loadingChallenges">
            <div v-if="!packs.length" class="empty-copy">还没有挑战包，请先创建挑战包。</div>
            <div v-for="item in packs" :key="item.pack.id" class="pack-row">
              <div class="pack-head">
                <div>
                  <strong>{{ item.pack.name }}</strong>
                  <span class="muted">{{ item.pack.slug }} · {{ item.pack.status }}</span>
                </div>
                <NButton size="small" secondary @click="createVersion(item.pack.id)"
                  >复制新版本</NButton
                >
              </div>
              <div class="version-list">
                <button
                  v-for="version in item.versions"
                  :key="version.id"
                  class="version-button"
                  :class="{ selected: selectedVersionId === version.id }"
                  type="button"
                  @click="openVersion(version.id)"
                >
                  <span>v{{ version.version }}</span>
                  <NTag
                    size="small"
                    :type="version.status === 'published' ? 'success' : 'warning'"
                    >{{ version.status }}</NTag
                  >
                </button>
              </div>
            </div>
          </NSpin>
        </section>

        <section v-if="versionDoc" class="editor-section">
          <div class="section-toolbar">
            <div>
              <h3>{{ versionDoc.version.version }} · 版本内容</h3>
              <p class="section-sub">
                结构化维护入口、运行契约、状态展示、结局与奖励；高级 JSON 仅用于导入导出。
              </p>
            </div>
            <div class="editor-actions">
              <NButton
                v-if="versionDoc.version.status === 'draft'"
                :loading="savingVersion"
                @click="saveVersion"
                >保存草稿</NButton
              >
              <NButton
                v-if="versionDoc.version.status === 'draft'"
                type="primary"
                :loading="publishingVersion"
                @click="publishVersion"
                >校验并发布</NButton
              >
              <NButton type="error" :loading="deletingVersion" @click="deleteVersion"
                >删除版本</NButton
              >
            </div>
          </div>
          <ChallengeVersionEditor
            v-if="versionPayload"
            v-model="versionPayload"
            :modules="modules"
            :rewards="rewardDefinitions"
            :readonly="versionDoc.version.status !== 'draft'"
          />
          <p v-if="versionDoc.version.content_hash" class="hash-line">
            内容哈希：{{ versionDoc.version.content_hash }}
          </p>
        </section>
      </NTabPane>
      <NTabPane v-if="sectionVisible('sharing')" name="sharing" tab="分享治理">
        <NSpin :show="loadingShareGovernance">
          <section class="admin-section">
            <div class="section-toolbar">
              <div>
                <h3>公开分享</h3>
                <p class="section-sub">仅显示玩家主动发布的快照，不提供未分享对话的读取入口。</p>
              </div>
              <NButton @click="loadShareGovernance">刷新</NButton>
            </div>
            <div v-if="publicShares.length" class="admin-list">
              <div v-for="item in publicShares" :key="item.id" class="admin-list-row">
                <div>
                  <strong>{{ item.title }}</strong>
                  <p class="section-sub">
                    {{ item.message_count }} 条 · {{ item.status }} ·
                    {{ new Date(item.created_at).toLocaleString() }}
                  </p>
                </div>
                <NButton
                  size="small"
                  type="error"
                  :disabled="item.status !== 'active'"
                  @click="moderateShare(item)"
                  >下架</NButton
                >
              </div>
            </div>
            <p v-else class="section-sub">暂无公开分享。</p>
          </section>
          <section class="admin-section">
            <h3>待处理举报</h3>
            <div v-if="shareReports.length" class="admin-list">
              <div v-for="item in shareReports" :key="item.id" class="admin-list-row">
                <div>
                  <strong>{{ item.reason }}</strong>
                  <p class="section-sub">{{ item.details || '未填写说明' }}</p>
                </div>
                <div class="row" style="gap: 8px">
                  <NButton size="small" @click="resolveShareReport(item, 'dismissed')"
                    >驳回</NButton
                  >
                  <NButton size="small" type="primary" @click="resolveShareReport(item, 'resolved')"
                    >完成</NButton
                  >
                </div>
              </div>
            </div>
            <p v-else class="section-sub">没有待处理举报。</p>
          </section>
        </NSpin>
      </NTabPane>
    </NTabs>

    <NModal
      v-model:show="showPackModal"
      preset="dialog"
      title="新建挑战包"
      style="width: min(520px, calc(100vw - 24px))"
    >
      <NForm label-placement="top">
        <NFormItem label="Slug"
          ><NInput v-model:value="packForm.slug" placeholder="例如 spirit-trials"
        /></NFormItem>
        <NFormItem label="名称"><NInput v-model:value="packForm.name" /></NFormItem>
        <NFormItem label="描述"
          ><NInput v-model:value="packForm.description" type="textarea"
        /></NFormItem>
      </NForm>
      <template #action>
        <NButton @click="showPackModal = false">取消</NButton>
        <NButton type="primary" :loading="creatingPack" @click="createPack">创建</NButton>
      </template>
    </NModal>

    <NModal
      v-model:show="showRewardModal"
      preset="dialog"
      title="新建奖励定义"
      style="width: min(620px, calc(100vw - 24px)); max-height: calc(100dvh - 24px)"
    >
      <NForm label-placement="top">
        <div class="form-grid two-columns">
          <NFormItem label="稳定 Key"
            ><NInput v-model:value="rewardForm.key" placeholder="pack-slug.reward-key"
          /></NFormItem>
          <NFormItem label="类型"
            ><NSelect v-model:value="rewardForm.kind" :options="rewardKindOptions"
          /></NFormItem>
          <NFormItem label="名称"><NInput v-model:value="rewardForm.name" /></NFormItem>
          <NFormItem label="单位"><NInput v-model:value="rewardForm.unit" /></NFormItem>
          <NFormItem label="图标 Key"
            ><NInput v-model:value="rewardForm.icon" placeholder="例如 gem"
          /></NFormItem>
          <NFormItem label="排序"
            ><NInputNumber v-model:value="rewardForm.sort_order" style="width: 100%"
          /></NFormItem>
        </div>
        <NFormItem label="说明"
          ><NInput v-model:value="rewardForm.description" type="textarea"
        /></NFormItem>
        <div class="setting-list compact-settings">
          <div class="setting-row">
            <span>可堆叠</span><NSwitch v-model:value="rewardForm.stackable" />
          </div>
          <div class="setting-row">
            <span>可消费</span><NSwitch v-model:value="rewardForm.consumable" />
          </div>
          <div class="setting-row">
            <span>顶栏展示</span><NSwitch v-model:value="rewardForm.featured" />
          </div>
        </div>
      </NForm>
      <template #action>
        <NButton @click="showRewardModal = false">取消</NButton>
        <NButton type="primary" :loading="creatingReward" @click="createRewardDefinition"
          >创建</NButton
        >
      </template>
    </NModal>

    <NModal
      v-model:show="showInvitationModal"
      preset="dialog"
      title="生成邀请码"
      style="width: min(520px, calc(100vw - 24px))"
    >
      <NForm label-placement="top">
        <NFormItem label="备注">
          <NInput v-model:value="invitationForm.label" placeholder="例如：首批测试用户" />
        </NFormItem>
        <NFormItem label="最大使用次数">
          <NInputNumber
            v-model:value="invitationForm.max_uses"
            :min="0"
            :max="1000000"
            style="width: 100%"
          />
          <template #feedback>0 表示不限次数</template>
        </NFormItem>
        <NFormItem label="过期时间">
          <NDatePicker
            v-model:value="invitationForm.expires_at"
            type="datetime"
            clearable
            style="width: 100%"
          />
        </NFormItem>
      </NForm>
      <template #action>
        <NButton @click="showInvitationModal = false">取消</NButton>
        <NButton type="primary" :loading="creatingInvitation" @click="createInvitation"
          >生成</NButton
        >
      </template>
    </NModal>

    <NModal
      v-model:show="showCreatedInvitationModal"
      preset="dialog"
      title="邀请码已生成"
      style="width: min(520px, calc(100vw - 24px))"
    >
      <NAlert type="warning" :show-icon="false"> 关闭后无法再次查看完整邀请码。 </NAlert>
      <div class="created-code-row">
        <NInput :value="createdInvitationCode" readonly />
        <NButton type="primary" @click="copyInvitationCode">复制</NButton>
      </div>
    </NModal>

    <NModal
      v-model:show="showLLMLogModal"
      preset="card"
      title="模型请求明细"
      class="llm-log-modal"
      style="width: min(960px, calc(100vw - 24px)); max-height: calc(100dvh - 24px)"
    >
      <div v-if="selectedLLMLog" class="llm-log-detail">
        <dl class="llm-log-meta">
          <div>
            <dt>用途</dt>
            <dd>{{ llmLogPurposeLabel(selectedLLMLog.purpose) }}</dd>
          </div>
          <div>
            <dt>模型</dt>
            <dd>{{ selectedLLMLog.model }}</dd>
          </div>
          <div>
            <dt>Base URL</dt>
            <dd>{{ selectedLLMLog.base_url }}</dd>
          </div>
          <div>
            <dt>时间</dt>
            <dd>{{ new Date(selectedLLMLog.created_at).toLocaleString() }}</dd>
          </div>
          <div>
            <dt>耗时</dt>
            <dd>{{ selectedLLMLog.duration_ms }} ms</dd>
          </div>
          <div v-if="selectedLLMLog.response_status">
            <dt>HTTP</dt>
            <dd>{{ selectedLLMLog.response_status }}</dd>
          </div>
          <div v-if="selectedLLMLog.finish_reason">
            <dt>结束原因</dt>
            <dd>{{ selectedLLMLog.finish_reason }}</dd>
          </div>
          <div v-if="selectedLLMLog.output_mode">
            <dt>结构化输出模式</dt>
            <dd>{{ selectedLLMLog.output_mode }}</dd>
          </div>
          <div v-if="selectedLLMLog.contract_name">
            <dt>输出契约</dt>
            <dd>{{ selectedLLMLog.contract_name }}@{{ selectedLLMLog.contract_version }}</dd>
          </div>
          <div v-if="selectedLLMLog.contract_fingerprint">
            <dt>契约指纹</dt>
            <dd>{{ selectedLLMLog.contract_fingerprint }}</dd>
          </div>
          <div>
            <dt>传输 / 结构</dt>
            <dd>
              {{ selectedLLMLog.transport_success ? '成功' : '失败' }} /
              {{ selectedLLMLog.structured_success ? '成功' : '失败' }}
            </dd>
          </div>
          <div v-if="selectedLLMLog.structured_error_code">
            <dt>结构错误码</dt>
            <dd>{{ selectedLLMLog.structured_error_code }}</dd>
          </div>
          <div v-if="selectedLLMLog.conversation_id">
            <dt>对话</dt>
            <dd>{{ selectedLLMLog.conversation_id }}</dd>
          </div>
          <div v-if="selectedLLMLog.message_id">
            <dt>消息</dt>
            <dd>{{ selectedLLMLog.message_id }}</dd>
          </div>
          <div v-if="selectedLLMLog.attempt_id">
            <dt>生成尝试</dt>
            <dd>{{ selectedLLMLog.attempt_id }}</dd>
          </div>
          <div v-if="selectedLLMLog.memory_job_id">
            <dt>Memory Job</dt>
            <dd>{{ selectedLLMLog.memory_job_id }}</dd>
          </div>
          <div v-if="selectedLLMLog.memory_id">
            <dt>Memory 版本</dt>
            <dd>{{ selectedLLMLog.memory_id }}</dd>
          </div>
          <div v-if="selectedLLMLog.inquiry_id">
            <dt>询问消息</dt>
            <dd>{{ selectedLLMLog.inquiry_id }}</dd>
          </div>
          <div v-if="selectedLLMLog.inquiry_snapshot_id">
            <dt>询问快照</dt>
            <dd>{{ selectedLLMLog.inquiry_snapshot_id }}</dd>
          </div>
          <div v-if="selectedLLMLog.inquiry_snapshot_id">
            <dt>快照版本</dt>
            <dd>
              branch {{ selectedLLMLog.inquiry_snapshot_branch_epoch }} / revision
              {{ selectedLLMLog.inquiry_snapshot_revision }}
            </dd>
          </div>
        </dl>
        <NAlert v-if="selectedLLMLog.error" type="error" :show-icon="false">
          {{ selectedLLMLog.error }}
        </NAlert>
        <h4>请求 JSON</h4>
        <pre>{{ JSON.stringify(selectedLLMLog.request_body, null, 2) }}</pre>
        <h4>原始响应</h4>
        <pre>{{ selectedLLMLog.raw_response || '（空响应）' }}</pre>
        <h4>合并后的模型正文</h4>
        <pre>{{ selectedLLMLog.response_text || '（未提取到正文）' }}</pre>
      </div>
    </NModal>

    <NModal
      v-model:show="showContextLogModal"
      preset="card"
      title="上下文构建明细"
      class="llm-log-modal"
      style="width: min(960px, calc(100vw - 24px)); max-height: calc(100dvh - 24px)"
    >
      <div v-if="selectedContextLog" class="llm-log-detail">
        <dl class="llm-log-meta">
          <div>
            <dt>模型</dt>
            <dd>{{ selectedContextLog.model }}</dd>
          </div>
          <div>
            <dt>时间</dt>
            <dd>{{ new Date(selectedContextLog.created_at).toLocaleString() }}</dd>
          </div>
          <div>
            <dt>Prompt Token</dt>
            <dd>
              {{ selectedContextLog.used_prompt_tokens }} /
              {{ selectedContextLog.max_context_tokens }}
            </dd>
          </div>
          <div>
            <dt>预留回复</dt>
            <dd>{{ selectedContextLog.reserved_response_tokens }}</dd>
          </div>
          <div>
            <dt>对话</dt>
            <dd>{{ selectedContextLog.conversation_id }}</dd>
          </div>
          <div v-if="selectedContextLog.attempt_id">
            <dt>生成尝试</dt>
            <dd>{{ selectedContextLog.attempt_id }}</dd>
          </div>
          <div>
            <dt>分支版本</dt>
            <dd>{{ selectedContextLog.branch_epoch }}</dd>
          </div>
          <div>
            <dt>记忆覆盖</dt>
            <dd>
              {{ selectedContextLog.memory_lag ? '存在滞后' : '正常' }} · 至回合
              {{ selectedContextLog.memory_covered_until_turn_seq }}
            </dd>
          </div>
        </dl>

        <h4>Prompt 段明细</h4>
        <div class="context-segments">
          <div
            v-for="(segment, index) in selectedContextLog.segments"
            :key="`${index}-${segment.key}-${segment.role}`"
            class="context-segment"
            :class="{ skipped: !segment.included }"
          >
            <div class="context-segment-head">
              <strong>{{ segment.key }}</strong>
              <span class="context-segment-tags">
                <NTag v-if="segment.required" size="small" type="warning">必选</NTag>
                <NTag size="small" :type="segment.included ? 'success' : 'default'">
                  {{ segment.included ? '已纳入' : '已裁剪' }}
                </NTag>
                <span class="muted">{{ segment.tokens }} token</span>
              </span>
            </div>
            <span class="muted">{{ segment.role }} · {{ segment.budget_key }}</span>
            <span v-if="segment.skip_reason" class="llm-log-error">
              {{ segment.skip_reason }}
            </span>
            <span v-if="segment.compacted" class="muted">
              压缩前 {{ segment.original_tokens || segment.tokens }} token
              <template v-if="segment.omitted_keys?.length">
                · 省略 {{ segment.omitted_keys.join('、') }}
              </template>
            </span>
          </div>
        </div>

        <h4>纳入的历史消息 ID</h4>
        <pre>{{ selectedContextLog.included_message_ids.join('\n') || '（无）' }}</pre>
        <h4>激活的世界书条目 ID</h4>
        <pre>{{ selectedContextLog.activated_world_entry_ids.join('\n') || '（无）' }}</pre>
      </div>
    </NModal>

    <NModal
      v-model:show="showModerateModal"
      preset="dialog"
      title="下架分享"
      style="width: min(520px, calc(100vw - 24px))"
    >
      <NForm label-placement="top">
        <NFormItem label="下架原因（仅管理员可见）"
          ><NInput v-model:value="moderateReason" placeholder="例如 policy_violation"
        /></NFormItem>
      </NForm>
      <template #action>
        <NButton @click="showModerateModal = false">取消</NButton>
        <NButton type="primary" :loading="moderatingShare" @click="submitModerateShare"
          >下架</NButton
        >
      </template>
    </NModal>

    <NModal
      v-model:show="showVersionModal"
      preset="dialog"
      title="新建草稿版本"
      style="width: min(520px, calc(100vw - 24px))"
    >
      <NForm label-placement="top">
        <NFormItem label="新版本号"
          ><NInput
            v-model:value="newVersion"
            placeholder="例如 1.0.0"
            @keydown.enter="submitCreateVersion"
        /></NFormItem>
      </NForm>
      <template #action>
        <NButton @click="showVersionModal = false">取消</NButton>
        <NButton type="primary" :loading="creatingVersion" @click="submitCreateVersion"
          >创建</NButton
        >
      </template>
    </NModal>
  </div>
</template>

<style scoped>
.page {
  max-width: 1080px;
  margin: 0 auto;
}

.admin-section,
.editor-section {
  padding: 24px 0;
  border-top: 1px solid var(--border-soft);
}

.admin-list {
  display: grid;
}

.admin-list-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 12px 0;
  border-bottom: 1px solid var(--border-soft);
}

h3,
.section-sub {
  margin: 0;
}

h3 {
  font-size: 16px;
}

.section-sub,
.muted,
.hash-line {
  color: var(--text-faint);
  font-size: 12px;
  line-height: 1.7;
}

.section-sub {
  margin-top: 5px;
}

.public-llm-collapse {
  margin-top: 18px;
  padding: 18px 0 0 18px;
  border-top: 1px solid var(--border-soft);
  border-left: 3px solid var(--accent);
}

.public-llm-collapse :deep(.n-collapse-item__header) {
  padding: 0 0 16px;
}

.public-llm-collapse :deep(.n-collapse-item__header-main) {
  min-width: 0;
}

.public-llm-collapse :deep(.n-collapse-item__header-extra) {
  margin-left: 12px;
}

.public-llm-collapse :deep(.n-collapse-item__content-inner) {
  padding: 0 0 2px;
}

.public-llm-content {
  padding-bottom: 2px;
}

.public-key-control {
  display: flex;
  gap: 8px;
  width: 100%;
}

.public-key-control :deep(.n-input) {
  flex: 1;
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

.model-setting-list {
  margin-bottom: 14px;
  border-top: 1px solid var(--border-soft);
}

.admin-llm-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 14px;
}

@media (max-width: 720px) {
  .admin-llm-grid {
    grid-template-columns: 1fr;
  }
  .public-key-control {
    align-items: stretch;
    flex-direction: column;
  }
}

.actions,
.section-toolbar,
.pack-head,
.editor-actions {
  display: flex;
  align-items: center;
}

.actions,
.section-toolbar,
.pack-head {
  justify-content: space-between;
}

.actions {
  justify-content: flex-end;
  margin-top: 18px;
}

.setting-list {
  margin-top: 18px;
  border-bottom: 1px solid var(--border-soft);
}

.setting-row,
.invitation-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  min-height: 64px;
  border-top: 1px solid var(--border-soft);
}

.setting-row > div,
.invitation-main {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 3px;
}

.auth-settings-form {
  margin-top: 18px;
}

.form-grid {
  display: grid;
  gap: 0 18px;
}

.two-columns {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.three-columns {
  grid-template-columns: minmax(0, 1.4fr) minmax(120px, 0.5fr) minmax(160px, 0.7fr);
}

.password-setting,
.test-email-row,
.created-code-row {
  display: flex;
  width: 100%;
  gap: 8px;
}

.test-email-row {
  max-width: 500px;
}

.auth-actions {
  gap: 18px;
  margin-top: 8px;
}

.invitation-row {
  padding: 12px 0;
}

.llm-log-row {
  display: flex;
  min-height: 76px;
  align-items: center;
  justify-content: space-between;
  gap: 18px;
  padding: 12px 0;
  border-bottom: 1px solid var(--border-soft);
}

.llm-log-main,
.llm-log-actions {
  display: flex;
}

.llm-log-main {
  min-width: 0;
  flex-direction: column;
  gap: 4px;
}

.llm-log-title {
  display: flex;
  align-items: center;
  gap: 8px;
}

.llm-log-actions {
  flex: none;
  align-items: center;
  gap: 8px;
}

.llm-log-error {
  overflow: hidden;
  max-width: 760px;
  color: var(--danger);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.llm-log-modal :deep(.n-card__content) {
  min-height: 0;
  overflow-y: auto;
}

.llm-log-detail {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.llm-log-meta {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px 20px;
  font-size: 12px;
}

.llm-log-meta > div {
  min-width: 0;
}

.llm-log-meta dt {
  color: var(--text-dim);
}

.llm-log-meta dd {
  margin: 2px 0 0;
  overflow-wrap: anywhere;
}

.llm-log-detail h4 {
  margin: 8px 0 0;
  font-size: 13px;
}

.llm-log-detail pre {
  max-height: 360px;
  margin: 0;
  padding: 12px;
  overflow: auto;
  border: 1px solid var(--border-soft);
  border-radius: 4px;
  color: var(--text);
  background: var(--bg-soft);
  font-size: 11px;
  line-height: 1.6;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}

.context-segments {
  display: flex;
  flex-direction: column;
  border-top: 1px solid var(--border-soft);
}

.context-segment {
  display: flex;
  flex-direction: column;
  gap: 5px;
  padding: 10px 0;
  border-bottom: 1px solid var(--border-soft);
  font-size: 12px;
}

.context-segment.skipped {
  opacity: 0.58;
}

.context-segment-head,
.context-segment-tags {
  display: flex;
  align-items: center;
  gap: 8px;
}

.context-segment-head {
  justify-content: space-between;
}

.context-segment-tags {
  flex-wrap: wrap;
  justify-content: flex-end;
}

.catalog-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(220px, auto);
  gap: 18px;
  padding: 16px 0;
  border-bottom: 1px solid var(--border-soft);
}

.catalog-row > div:first-child {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 4px;
}

.catalog-row p {
  margin: 2px 0 0;
  color: var(--text-dim);
  font-size: 12px;
}

.catalog-tags {
  display: flex;
  max-width: 420px;
  align-content: flex-start;
  justify-content: flex-end;
  flex-wrap: wrap;
  gap: 6px;
}

.compact-settings .setting-row {
  min-height: 46px;
}

.invitation-title {
  display: flex;
  align-items: center;
  gap: 10px;
}

.created-code-row {
  margin-top: 16px;
}

.pack-row {
  padding: 16px 0;
  border-top: 1px solid var(--border-soft);
}

.builtin-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 20px;
  align-items: center;
  padding: 18px 0;
  border-top: 1px solid var(--border-soft);
}

.builtin-main,
.builtin-title {
  display: flex;
}

.builtin-main {
  min-width: 0;
  flex-direction: column;
  gap: 5px;
}

.builtin-title {
  align-items: center;
  gap: 10px;
}

.builtin-description {
  max-width: 760px;
  margin: 2px 0;
  color: var(--text-dim);
  font-size: 13px;
  line-height: 1.6;
}

.pack-head {
  gap: 16px;
}

.pack-head > div {
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.version-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 14px;
}

.version-button {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 7px 10px;
  border: 1px solid var(--border-soft);
  border-radius: 4px;
  color: var(--text-dim);
  background: transparent;
  cursor: pointer;
}

.version-button.selected {
  border-color: var(--accent);
  color: var(--text);
}

.editor-actions {
  gap: 8px;
}

.json-editor {
  margin-top: 18px;
  font-size: 12px;
}

.empty-copy {
  padding: 32px 0;
  color: var(--text-faint);
}

.hash-line {
  overflow-wrap: anywhere;
}

@media (max-width: 680px) {
  .admin-section,
  .editor-section {
    padding: 18px 0;
  }

  .section-toolbar,
  .pack-head {
    align-items: flex-start;
    flex-direction: column;
  }

  .two-columns,
  .three-columns {
    grid-template-columns: minmax(0, 1fr);
  }

  .auth-actions,
  .test-email-row,
  .password-setting {
    align-items: stretch;
    flex-direction: column;
    max-width: none;
  }

  .setting-row,
  .invitation-row {
    gap: 12px;
  }

  .editor-actions {
    width: 100%;
    flex-wrap: wrap;
  }

  .editor-actions > .n-button {
    flex: 1;
  }

  .section-toolbar > .n-button,
  .pack-head > .n-button,
  .actions > .n-button {
    width: 100%;
  }

  .builtin-row {
    grid-template-columns: minmax(0, 1fr);
    gap: 12px;
  }

  .catalog-row {
    grid-template-columns: minmax(0, 1fr);
    gap: 10px;
  }

  .catalog-tags {
    max-width: none;
    justify-content: flex-start;
  }

  .builtin-row > .n-button {
    width: 100%;
  }

  .version-list {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .version-button {
    justify-content: space-between;
    min-width: 0;
  }

  .json-editor :deep(textarea) {
    font-size: 12px;
  }

  .llm-log-row {
    align-items: stretch;
    flex-direction: column;
  }

  .llm-log-actions {
    justify-content: space-between;
  }

  .llm-log-meta {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
