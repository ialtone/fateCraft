<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import {
  NAlert,
  NButton,
  NInput,
  NModal,
  NSelect,
  NSpin,
  NSwitch,
  NTabs,
  NTabPane,
  useDialog,
  useMessage,
} from 'naive-ui'
import AdminRewardUserPanel from './AdminRewardUserPanel.vue'
import AdminUserDetailModal from './AdminUserDetailModal.vue'
import { adminApi } from '@/api'
import { useAuthStore } from '@/stores/auth'
import { formatTime, registrationLabel, roleLabel } from '@/lib/adminUsers'
import type { AdminAuditLog, AdminUser } from '@/types'

const auth = useAuthStore()
const message = useMessage()
const dialog = useDialog()
const users = ref<AdminUser[]>([])
const rewardUser = ref<AdminUser>()
const showRewards = ref(false)
const detailUserId = ref<string | null>(null)
const auditLogs = ref<AdminAuditLog[]>([])
const loading = ref(false)
const savingID = ref('')
const error = ref('')
const tab = ref('users')
const auditError = ref('')
const loadingAudit = ref(false)
const isOwner = computed(() => auth.isOwner)
const USER_PAGE_LIMIT = 30
const query = ref('')
const submittedQuery = ref('')
const filters = reactive({
  status: null as string | null,
  role: null as string | null,
  registration: null as string | null,
  sort: 'created_at' as 'created_at' | 'last_login_at',
  order: 'desc' as 'asc' | 'desc',
})
const offset = ref(0)
const hasMore = ref(false)
let loadSequence = 0
const statusOptions = [
  { label: '正常', value: 'active' },
  { label: '已停用', value: 'disabled' },
]
const registrationOptions = [
  { label: '邮箱注册', value: 'email' },
  { label: 'LinuxDo', value: 'linuxdo' },
]
const sortOptions = [
  { label: '按注册时间', value: 'created_at' },
  { label: '按最后登录', value: 'last_login_at' },
]
const orderOptions = [
  { label: '新到旧', value: 'desc' },
  { label: '旧到新', value: 'asc' },
]
const roleOptions = [
  { label: '普通用户', value: 'user' },
  { label: '管理员', value: 'admin' },
  { label: '拥有者', value: 'owner' },
]

async function load() {
  const sequence = ++loadSequence
  loading.value = true
  error.value = ''
  try {
    const nextUsers = await adminApi.listUsers({
      q: submittedQuery.value,
      status: filters.status || undefined,
      role: filters.role || undefined,
      registration: filters.registration || undefined,
      sort: filters.sort,
      order: filters.order,
      limit: USER_PAGE_LIMIT,
      offset: offset.value,
    })
    if (sequence !== loadSequence) return
    users.value = nextUsers.users
    hasMore.value = nextUsers.has_more
  } catch (err: any) {
    if (sequence === loadSequence) error.value = err?.message || '读取用户管理数据失败'
  } finally {
    if (sequence === loadSequence) loading.value = false
  }
}

async function loadAudits() {
  if (loadingAudit.value) return
  loadingAudit.value = true
  auditError.value = ''
  try {
    auditLogs.value = (await adminApi.listAuditLogs({ limit: 30 })).logs
  } catch (err: any) {
    auditError.value = err?.message || '读取管理操作失败'
  } finally {
    loadingAudit.value = false
  }
}
watch(tab, (value) => {
  if (value === 'audit') void loadAudits()
})

function searchUsers() {
  submittedQuery.value = query.value.trim()
  offset.value = 0
  void load()
}

function pageUsers(delta: number) {
  offset.value = Math.max(0, offset.value + delta * USER_PAGE_LIMIT)
  void load()
}

async function changeRole(user: AdminUser, role: string) {
  if (!isOwner.value || role === user.role) return
  if (user.role === 'owner' && role !== 'owner') {
    message.warning('请先将 owner 转交给其他用户')
    return
  }
  const apply = async () => {
    savingID.value = user.id
    try {
      const updated = await adminApi.updateUserRole(user.id, role as AdminUser['role'])
      const index = users.value.findIndex((item) => item.id === user.id)
      if (index >= 0) users.value[index] = updated
      await auth.refreshUser()
      await load()
      message.success('角色已更新')
    } catch (err: any) {
      message.error(err?.message || '角色更新失败')
    } finally {
      savingID.value = ''
    }
  }
  if (role === 'owner') {
    dialog.warning({
      title: '转让平台拥有者？',
      content: `确认将 ${user.display_name} 设为新的平台拥有者？当前 owner 会降为 admin。`,
      positiveText: '确认转让',
      negativeText: '取消',
      onPositiveClick: () => void apply(),
    })
    return
  }
  await apply()
}

async function changeStatus(user: AdminUser, enabled: boolean) {
  if (!isOwner.value || user.role === 'owner') return
  savingID.value = user.id
  try {
    await adminApi.updateUserStatus(user.id, enabled ? 'active' : 'disabled')
    await load()
    message.success(enabled ? '账号已启用' : '账号已停用')
  } catch (err: any) {
    message.error(err?.message || '账号状态更新失败')
  } finally {
    savingID.value = ''
  }
}

onMounted(() => void load())
function openRewards(user: AdminUser) {
  rewardUser.value = user
  showRewards.value = true
}
</script>

<template>
  <div class="users-panel">
    <NTabs v-model:value="tab" type="line" animated
      ><NTabPane name="users" tab="用户管理" display-directive="show:lazy"
        ><p class="section-sub">
          首次安装完成后，所有新账号默认为普通用户。只有 owner 可以调整角色和停用管理员。
        </p>
        <NAlert v-if="error" type="error" :show-icon="true">{{ error }}</NAlert>
        <form class="user-search" @submit.prevent="searchUsers">
          <NInput
            v-model:value="query"
            :maxlength="256"
            aria-label="查找用户"
            placeholder="用户名、显示名称、完整邮箱或用户 ID"
          />
          <NButton attr-type="submit" :loading="loading">查找用户</NButton>
        </form>
        <div class="user-filters">
          <NSelect
            v-model:value="filters.status"
            :options="statusOptions"
            placeholder="全部状态"
            clearable
            size="small"
            @update:value="searchUsers"
          />
          <NSelect
            v-model:value="filters.role"
            :options="roleOptions"
            placeholder="全部角色"
            clearable
            size="small"
            @update:value="searchUsers"
          />
          <NSelect
            v-model:value="filters.registration"
            :options="registrationOptions"
            placeholder="全部注册方式"
            clearable
            size="small"
            @update:value="searchUsers"
          />
          <NSelect
            v-model:value="filters.sort"
            :options="sortOptions"
            size="small"
            @update:value="searchUsers"
          />
          <NSelect
            v-model:value="filters.order"
            :options="orderOptions"
            size="small"
            @update:value="searchUsers"
          />
        </div>
        <NSpin :show="loading">
          <div class="user-list">
            <div v-for="user in users" :key="user.id" class="user-row">
              <div class="user-main">
                <strong
                  >{{ user.display_name }}
                  <a class="username" href="#" @click.prevent="detailUserId = user.id"
                    >@{{ user.username }}</a
                  ></strong
                >
                <span class="muted"
                  >{{ user.email || '外部登录账号' }} · {{ roleLabel(user.role) }} ·
                  {{ registrationLabel(user.registration_method) }}
                  <template v-if="user.status !== 'active'"> · 已停用</template></span
                >
                <span class="muted"
                  >注册时间 {{ formatTime(user.created_at) }} · 最后登录
                  {{ formatTime(user.last_login_at, '从未登录') }}</span
                >
              </div>
              <div class="user-actions">
                <NButton size="small" @click="openRewards(user)">账户资产</NButton>
                <NSelect
                  v-if="isOwner"
                  :value="user.role"
                  :options="roleOptions"
                  size="small"
                  :loading="savingID === user.id"
                  :disabled="user.role === 'owner' && user.id === auth.user?.id"
                  @update:value="(value) => changeRole(user, value)"
                />
                <NSwitch
                  v-if="isOwner && user.role !== 'owner'"
                  :value="user.status === 'active'"
                  :loading="savingID === user.id"
                  @update:value="(value) => changeStatus(user, value)"
                />
              </div>
            </div>
            <p v-if="!users.length && !loading && !error" class="muted">没有匹配的用户</p>
          </div>
        </NSpin>
        <div class="user-search">
          <NButton :disabled="loading || offset === 0" @click="pageUsers(-1)">上一页</NButton>
          <NButton :disabled="loading || !hasMore" @click="pageUsers(1)">下一页</NButton>
        </div> </NTabPane
      ><NTabPane name="audit" tab="最近操作" display-directive="show:lazy"
        ><div class="user-search">
          <NButton :loading="loadingAudit" @click="loadAudits">刷新操作记录</NButton
          ><NButton @click="$router.push('/admin/logs')">前往完整审计</NButton>
        </div>
        <NAlert v-if="auditError" type="error">{{ auditError }}</NAlert
        ><NSpin :show="loadingAudit"
          ><h4>最近管理员操作</h4>
          <div class="audit-list">
            <div v-for="log in auditLogs" :key="log.id" class="audit-row">
              <span>{{ log.action }}</span>
              <span class="muted">{{ log.result }} · {{ formatTime(log.created_at) }}</span>
            </div>
            <p v-if="!auditLogs.length && !auditError && !loadingAudit" class="muted">
              暂无审计记录
            </p>
          </div></NSpin
        ></NTabPane
      ></NTabs
    >
    <AdminUserDetailModal :user-id="detailUserId" @close="detailUserId = null" @changed="load" />
    <NModal
      v-model:show="showRewards"
      preset="card"
      title="用户账户资产"
      style="width: min(1040px, 96vw)"
      content-style="max-height: calc(100vh - 160px); max-height: calc(100dvh - 160px); overflow: auto"
      ><AdminRewardUserPanel v-if="rewardUser" :key="rewardUser.id" :user-id="rewardUser.id"
    /></NModal>
  </div>
</template>

<style scoped>
.users-panel {
  padding: 24px 0;
}
.user-search {
  display: flex;
  gap: 12px;
  margin: 16px 0;
}

.user-filters {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 12px;
}

.user-list,
.audit-list {
  display: grid;
  margin-top: 18px;
}

.user-row,
.audit-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 12px 0;
  border-bottom: 1px solid var(--border-soft);
}

.user-main {
  display: grid;
  min-width: 0;
  gap: 3px;
}

.user-actions {
  display: flex;
  align-items: center;
  gap: 12px;
}

.muted {
  color: var(--text-faint);
  font-size: 12px;
}

.username {
  color: var(--text-faint);
  font-size: 12px;
  font-weight: 400;
}

h4 {
  margin: 28px 0 0;
}

@media (max-width: 640px) {
  .user-row {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
