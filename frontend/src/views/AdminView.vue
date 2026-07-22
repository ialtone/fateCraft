<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  NButton,
  NAlert,
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
  useMessage,
} from 'naive-ui'
import { adminApi } from '@/api'
import type {
  AdminBuiltinChallenge,
  AdminAuthSettings,
  AdminChallengePack,
  AdminRollSettings,
  ChallengeVersionDocument,
  DifficultyModeEnum,
  InvitationCode,
} from '@/types'

const message = useMessage()
const settings = ref<AdminRollSettings | null>(null)
const authSettings = ref<AdminAuthSettings | null>(null)
const smtpPassword = ref('')
const clearSMTPPassword = ref(false)
const testEmail = ref('')
const invitations = ref<InvitationCode[]>([])
const packs = ref<AdminChallengePack[]>([])
const builtins = ref<AdminBuiltinChallenge[]>([])
const loadingSettings = ref(false)
const loadingAuthSettings = ref(false)
const loadingInvitations = ref(false)
const loadingChallenges = ref(false)
const loadingBuiltins = ref(false)
const savingSettings = ref(false)
const savingAuthSettings = ref(false)
const testingEmail = ref(false)
const creatingInvitation = ref(false)
const updatingInvitation = ref('')
const savingVersion = ref(false)
const publishingVersion = ref(false)
const importingBuiltin = ref('')
const versionDoc = ref<ChallengeVersionDocument | null>(null)
const selectedVersionId = ref('')
const editor = ref('')
const showPackModal = ref(false)
const showInvitationModal = ref(false)
const showCreatedInvitationModal = ref(false)
const createdInvitationCode = ref('')
const creatingPack = ref(false)
const packForm = ref({ slug: '', name: '', description: '' })
const invitationForm = ref<{ label: string; max_uses: number; expires_at: number | null }>({
  label: '',
  max_uses: 1,
  expires_at: null,
})

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

async function loadSettings() {
  loadingSettings.value = true
  try {
    settings.value = await adminApi.getRollSettings()
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
    settings.value = await adminApi.updateRollSettings(settings.value)
    message.success('玩法设置已保存')
  } catch (err: any) {
    message.error(`保存管理设置失败：${err.message || '未知错误'}`)
  } finally {
    savingSettings.value = false
  }
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

async function saveAuthSettings() {
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
  if (status === 'conflict') return '版本冲突'
  return '可导入'
}

function builtinStatusType(
  status: AdminBuiltinChallenge['status'],
): 'success' | 'warning' | 'error' {
  if (status === 'imported') return 'success'
  if (status === 'conflict') return 'error'
  return 'warning'
}

async function importBuiltin(item: AdminBuiltinChallenge) {
  if (!item.importable) return
  if (!window.confirm(`导入并发布「${item.name}」v${item.version}？`)) return
  importingBuiltin.value = item.slug
  try {
    await adminApi.importBuiltin(item.slug)
    await Promise.all([loadBuiltins(), loadChallenges()])
    message.success(`已导入并发布 ${item.name} v${item.version}`)
  } catch (err: any) {
    message.error(`导入失败：${err.message || '未知错误'}`)
  } finally {
    importingBuiltin.value = ''
  }
}

async function openVersion(versionId: string) {
  selectedVersionId.value = versionId
  try {
    versionDoc.value = await adminApi.getChallengeVersion(versionId)
    editor.value = JSON.stringify(
      {
        manifest: versionDoc.value.version.manifest,
        assets: versionDoc.value.assets,
        reward_rules: versionDoc.value.reward_rules,
      },
      null,
      2,
    )
  } catch (err: any) {
    message.error(`读取版本失败：${err.message || '未知错误'}`)
  }
}

async function saveVersion() {
  if (!versionDoc.value) return
  let payload: any
  try {
    payload = JSON.parse(editor.value)
  } catch {
    message.error('版本 JSON 格式无效')
    return
  }
  if (!payload.manifest || !Array.isArray(payload.assets) || !Array.isArray(payload.reward_rules)) {
    message.error('版本 JSON 必须包含 manifest、assets、reward_rules')
    return
  }
  savingVersion.value = true
  try {
    versionDoc.value = await adminApi.saveChallengeVersion(versionDoc.value.version.id, payload)
    editor.value = JSON.stringify(
      {
        manifest: versionDoc.value.version.manifest,
        assets: versionDoc.value.assets,
        reward_rules: versionDoc.value.reward_rules,
      },
      null,
      2,
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
  if (!versionDoc.value) return
  publishingVersion.value = true
  try {
    versionDoc.value = await adminApi.publishChallengeVersion(versionDoc.value.version.id)
    editor.value = JSON.stringify(
      {
        manifest: versionDoc.value.version.manifest,
        assets: versionDoc.value.assets,
        reward_rules: versionDoc.value.reward_rules,
      },
      null,
      2,
    )
    await loadChallenges()
    message.success('版本已发布，新对话将使用它')
  } catch (err: any) {
    message.error(`发布失败：${err.message || '未知错误'}`)
  } finally {
    publishingVersion.value = false
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

async function createVersion(packId: string) {
  const version = window.prompt('输入新版本号，例如 1.0.0')?.trim()
  if (!version) return
  try {
    const doc = await adminApi.createChallengeVersion(packId, version)
    await loadChallenges()
    await openVersion(doc.version.id)
    message.success('草稿版本已创建')
  } catch (err: any) {
    message.error(`创建版本失败：${err.message || '未知错误'}`)
  }
}

onMounted(() => {
  void loadSettings()
  void loadAuthSettings()
  void loadInvitations()
  void loadChallenges()
  void loadBuiltins()
})
</script>

<template>
  <div class="page">
    <div class="page-title">
      <h2>管理</h2>
      <span class="ornament">◆</span>
    </div>
    <p class="page-sub">平台规则与挑战内容都由管理员发布，已发布版本不可原地修改。</p>

    <NTabs type="line" animated>
      <NTabPane name="settings" tab="玩法设置">
        <section class="admin-section">
          <h3>天命判定</h3>
          <p class="section-sub">普通用户只能参与判定，无法覆盖平台难度或开关。</p>
          <NSpin :show="loadingSettings">
            <NForm v-if="settings" label-placement="top">
              <NFormItem label="判定系统">
                <NSwitch v-model:value="settings.roll_system_enabled">
                  <template #checked>已启用</template>
                  <template #unchecked>已停用</template>
                </NSwitch>
              </NFormItem>
              <NFormItem label="挑战难度">
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
            </NForm>
          </NSpin>
          <div class="actions">
            <NButton type="primary" :loading="savingSettings" @click="saveSettings"
              >保存玩法设置</NButton
            >
          </div>
        </section>
      </NTabPane>

      <NTabPane name="auth" tab="账号与注册">
        <NSpin :show="loadingAuthSettings">
          <template v-if="authSettings">
            <section class="admin-section">
              <h3>登录与注册策略</h3>
              <p class="section-sub">设置保存后立即作用于登录和注册接口。</p>
              <div class="setting-list">
                <div class="setting-row">
                  <div>
                    <strong>邮箱登录</strong>
                    <span class="muted">允许已有邮箱账号使用密码登录</span>
                  </div>
                  <NSwitch v-model:value="authSettings.email_auth_enabled" />
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
              <NForm class="auth-settings-form" label-placement="top">
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
              <NForm class="auth-settings-form" label-placement="top">
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
                  <NButton secondary :loading="testingEmail" @click="sendTestEmail"
                    >发送测试邮件</NButton
                  >
                </div>
                <NButton type="primary" :loading="savingAuthSettings" @click="saveAuthSettings">
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

      <NTabPane name="builtins" tab="内置内容">
        <section class="admin-section">
          <div class="section-toolbar">
            <div>
              <h3>仓库内置挑战</h3>
              <p class="section-sub">
                导入会经过完整发布校验；同版本内容不同必须先提升仓库版本号。
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
                :disabled="!item.importable"
                :loading="importingBuiltin === item.slug"
                @click="importBuiltin(item)"
              >
                {{
                  item.status === 'imported'
                    ? '已发布'
                    : item.status === 'conflict'
                      ? '需提升版本'
                      : '导入并发布'
                }}
              </NButton>
            </div>
          </NSpin>
        </section>
      </NTabPane>

      <NTabPane name="challenges" tab="挑战内容">
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
                编辑 manifest、assets 和 reward_rules。发布后编辑器变为只读。
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
            </div>
          </div>
          <NInput
            v-model:value="editor"
            type="textarea"
            :autosize="{ minRows: 18, maxRows: 36 }"
            :readonly="versionDoc.version.status !== 'draft'"
            class="json-editor"
          />
          <p v-if="versionDoc.version.content_hash" class="hash-line">
            内容哈希：{{ versionDoc.version.content_hash }}
          </p>
        </section>
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
  font-family: 'Cascadia Code', Consolas, monospace;
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
}
</style>
