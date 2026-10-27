<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import {
  NAlert,
  NButton,
  NCard,
  NModal,
  NSpace,
  NSpin,
  NTabPane,
  NTabs,
  NTag,
  useDialog,
  useMessage,
} from 'naive-ui'
import { adminApi } from '@/api'
import { useAuthStore } from '@/stores/auth'
import { canChangeStatus, formatTime, registrationLabel, roleLabel } from '@/lib/adminUsers'
import type {
  AdminConversationRecord,
  AdminUserConversation,
  AdminUserDetail,
  AdminUserLoggingCoverage,
  PlayerReward,
} from '@/types'

// userId 为 null 时关闭；弹窗内始终以用户 ID（username）标识用户。
const props = defineProps<{ userId: string | null }>()
const emit = defineEmits<{ close: []; changed: [] }>()
const auth = useAuthStore()
const message = useMessage()
const dialog = useDialog()
const user = ref<AdminUserDetail>()
const rewards = ref<PlayerReward[]>([])
const coverage = ref<AdminUserLoggingCoverage>()
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const tab = ref('detail')
const conversations = ref<AdminUserConversation[]>([])
const records = ref<AdminConversationRecord[]>([])
const activeConversation = ref('')
const recordsLoading = ref(false)
const recordsError = ref('')
let sequence = 0

const show = computed(() => props.userId !== null)
const covered = computed(() => coverage.value?.covered === true)
const statusAllowed = computed(() => !!user.value && canChangeStatus(auth.isOwner, user.value))
const basics = computed<string[][]>(() => {
  const u = user.value
  if (!u) return []
  return [
    ['用户 ID', u.username],
    ['显示名称', u.display_name],
    ['邮箱', u.email || '—'],
    ['角色', roleLabel(u.role)],
    ['状态', u.status === 'active' ? '正常' : '已停用'],
    ['注册时间', formatTime(u.created_at)],
    ['最后登录', formatTime(u.last_login_at, '从未登录')],
  ]
})
const account = computed<string[][]>(() => {
  const u = user.value
  if (!u) return []
  const rows = [['注册方式', registrationLabel(u.registration_method)]]
  if (u.linuxdo) {
    rows.push(
      ['LinuxDo 用户名', u.linuxdo.username],
      ['LinuxDo 信任等级', u.linuxdo.trust_level == null ? '—' : String(u.linuxdo.trust_level)],
      ['LinuxDo 绑定时间', formatTime(u.linuxdo.bound_at)],
    )
  }
  return rows
})
const play = computed<string[][]>(() => {
  const s = user.value?.stats
  if (!s) return []
  return [
    ['开始的对话', String(s.started)],
    ['完成的挑战', `${s.completed}（成功 ${s.success} / 失败 ${s.failure} / 其他 ${s.neutral}）`],
    ...rewards.value.map((r) => [r.asset.name, `${r.balance} ${r.asset.unit}`]),
  ]
})

const groups = computed(() => [
  { title: '基本信息', rows: basics.value },
  { title: '账号', rows: account.value },
  { title: '游玩', rows: play.value },
])

async function load(id: string) {
  const request = ++sequence
  loading.value = true
  error.value = ''
  try {
    const data = await adminApi.getUser(id)
    if (request !== sequence) return
    user.value = data.user
    rewards.value = data.rewards || []
    coverage.value = data.detailed_logging
  } catch (err: any) {
    if (request === sequence) error.value = err?.message || '读取用户详情失败'
  } finally {
    if (request === sequence) loading.value = false
  }
}

async function loadConversations(id: string) {
  recordsLoading.value = true
  recordsError.value = ''
  try {
    conversations.value = await adminApi.listUserConversations(id)
  } catch (err: any) {
    recordsError.value = err?.message || '读取对话记录失败'
  } finally {
    recordsLoading.value = false
  }
}

async function openConversation(conversationID: string) {
  if (!props.userId) return
  activeConversation.value = conversationID
  records.value = []
  recordsLoading.value = true
  recordsError.value = ''
  try {
    records.value = await adminApi.listUserConversationRecords(props.userId, conversationID)
  } catch (err: any) {
    recordsError.value = err?.message || '读取对话记录失败'
  } finally {
    recordsLoading.value = false
  }
}

watch(
  () => props.userId,
  (id) => {
    tab.value = 'detail'
    user.value = undefined
    conversations.value = []
    records.value = []
    activeConversation.value = ''
    recordsError.value = ''
    if (id) void load(id)
  },
  { immediate: true },
)
watch(tab, (value) => {
  if (value === 'records' && props.userId && !conversations.value.length) {
    void loadConversations(props.userId)
  }
})

function toggleStatus() {
  const u = user.value
  if (!u || !statusAllowed.value) return
  const enable = u.status !== 'active'
  dialog.warning({
    title: enable ? '启用该账号？' : '禁用该账号？',
    content: enable
      ? `确认启用用户 ${u.username}？该用户将可以重新登录。`
      : `确认禁用用户 ${u.username}？该用户将无法登录。`,
    positiveText: enable ? '确认启用' : '确认禁用',
    negativeText: '取消',
    onPositiveClick: async () => {
      busy.value = true
      try {
        await adminApi.updateUserStatus(u.id, enable ? 'active' : 'disabled')
        message.success(enable ? '账号已启用' : '账号已禁用')
        emit('changed')
        await load(u.id)
      } catch (err: any) {
        message.error(err?.message || '账号状态更新失败')
      } finally {
        busy.value = false
      }
    },
  })
}

function enableLogging() {
  const u = user.value
  if (!u) return
  dialog.warning({
    title: '对该用户开启详细日志？',
    content: `将把详细日志采集范围设为用户 ${u.username}，之后该用户的模型请求和响应正文会被保存，管理员查看其对话记录会留下审计。排查完成后请在「日志策略」中关闭。`,
    positiveText: '确认开启',
    negativeText: '取消',
    onPositiveClick: async () => {
      busy.value = true
      try {
        coverage.value = await adminApi.enableUserDetailedLogging(u.id)
        message.success('已对该用户开启详细日志')
      } catch (err: any) {
        message.error(err?.message || '开启详细日志失败')
      } finally {
        busy.value = false
      }
    },
  })
}

const pretty = (value: unknown) => JSON.stringify(value, null, 2)
</script>

<template>
  <NModal
    :show="show"
    preset="card"
    :title="user ? `用户 ID：${user.username}` : '用户详情'"
    style="width: min(860px, 96vw)"
    content-style="max-height: calc(100vh - 160px); max-height: calc(100dvh - 160px); overflow: auto"
    @update:show="(value) => !value && emit('close')"
  >
    <NAlert v-if="error" type="error">{{ error }}</NAlert>
    <NSpin :show="loading">
      <NTabs v-if="user" v-model:value="tab" type="line">
        <NTabPane name="detail" tab="详情">
          <section v-for="group in groups" :key="group.title" class="group">
            <h4>{{ group.title }}</h4>
            <dl>
              <template v-for="[label, value] in group.rows" :key="label">
                <dt>{{ label }}</dt>
                <dd>{{ value }}</dd>
              </template>
            </dl>
          </section>
          <section class="group">
            <h4>操作</h4>
            <NSpace>
              <NButton
                :type="user.status === 'active' ? 'error' : 'primary'"
                :disabled="!statusAllowed"
                :loading="busy"
                @click="toggleStatus"
                >{{ user.status === 'active' ? '禁用账号' : '启用账号' }}</NButton
              >
              <NButton :disabled="covered" :loading="busy" @click="enableLogging">{{
                covered ? '详细日志已覆盖该用户' : '对该用户开启详细日志'
              }}</NButton>
            </NSpace>
            <p v-if="!auth.isOwner" class="muted">只有拥有者可以禁用或启用账号。</p>
            <p v-else-if="user.role === 'owner'" class="muted">拥有者账号不能被禁用。</p>
          </section>
        </NTabPane>
        <NTabPane v-if="covered" name="records" tab="对话记录">
          <NAlert type="warning" :show-icon="false"
            >仅展示详细日志采集到的模型请求与响应；每次查看都会记入管理审计。</NAlert
          >
          <NAlert v-if="recordsError" type="error">{{ recordsError }}</NAlert>
          <NSpin :show="recordsLoading">
            <div class="convs">
              <NButton
                v-for="conv in conversations"
                :key="conv.id"
                size="small"
                :type="conv.id === activeConversation ? 'primary' : 'default'"
                @click="openConversation(conv.id)"
                >{{ conv.title || '未命名对话' }} · {{ conv.record_count }} 条 ·
                {{ formatTime(conv.last_record_at) }}</NButton
              >
              <p v-if="!conversations.length && !recordsLoading && !recordsError" class="muted">
                暂无已采集的对话记录
              </p>
            </div>
            <NCard v-for="record in records" :key="record.id" size="small" class="record">
              <div class="record-head">
                <strong>{{ record.model }}</strong>
                <NTag :type="record.success ? 'success' : 'error'">{{
                  record.success ? '成功' : '失败'
                }}</NTag>
                <span class="muted"
                  >{{ record.purpose }} · {{ formatTime(record.created_at) }}</span
                >
              </div>
              <NAlert v-if="record.error" type="error" :show-icon="false">{{
                record.error
              }}</NAlert>
              <h5>请求</h5>
              <pre>{{ pretty(record.request_body) }}</pre>
              <h5>响应</h5>
              <pre>{{ record.response_text }}</pre>
            </NCard>
          </NSpin>
        </NTabPane>
      </NTabs>
    </NSpin>
  </NModal>
</template>

<style scoped>
.group + .group {
  margin-top: 20px;
}
h4,
h5 {
  margin: 12px 0 8px;
}
dl {
  display: grid;
  grid-template-columns: minmax(110px, 1fr) 3fr;
  gap: 8px 12px;
  margin: 0;
}
dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.muted {
  color: var(--text-faint);
  font-size: 12px;
}
.convs {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin: 12px 0;
}
.record {
  margin-top: 12px;
}
.record-head {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
}
pre {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  max-height: 280px;
  overflow: auto;
}
</style>
