<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { NAlert, NSelect, NSpin, NSwitch, useMessage } from 'naive-ui'
import { adminApi } from '@/api'
import { useAuthStore } from '@/stores/auth'
import type { AdminAuditLog, AdminUser } from '@/types'

const auth = useAuthStore()
const message = useMessage()
const users = ref<AdminUser[]>([])
const auditLogs = ref<AdminAuditLog[]>([])
const loading = ref(false)
const savingID = ref('')
const error = ref('')
const isOwner = computed(() => auth.isOwner)
// 后端单页上限；返回恰好这么多条时提示列表可能不完整。
const USER_PAGE_LIMIT = 200
const reachedPageLimit = computed(() => users.value.length >= USER_PAGE_LIMIT)
const roleOptions = [
  { label: '普通用户', value: 'user' },
  { label: '管理员', value: 'admin' },
  { label: '拥有者', value: 'owner' },
]

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [nextUsers, nextLogs] = await Promise.all([
      adminApi.listUsers({ limit: USER_PAGE_LIMIT }),
      adminApi.listAuditLogs(30),
    ])
    users.value = nextUsers
    auditLogs.value = nextLogs
  } catch (err: any) {
    error.value = err?.message || '读取用户管理数据失败'
  } finally {
    loading.value = false
  }
}

async function changeRole(user: AdminUser, role: string) {
  if (!isOwner.value || role === user.role) return
  if (
    role === 'owner' &&
    !window.confirm(`确认将 ${user.display_name} 设为新的平台拥有者？当前 owner 会降为 admin。`)
  )
    return
  if (user.role === 'owner' && role !== 'owner') {
    message.warning('请先将 owner 转交给其他用户')
    return
  }
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

function roleLabel(role: string) {
  return role === 'owner' ? '拥有者' : role === 'admin' ? '管理员' : '普通用户'
}

function formatTime(value?: string) {
  return value ? new Date(value).toLocaleString() : '—'
}

onMounted(() => void load())
</script>

<template>
  <div class="users-panel">
    <p class="section-sub">
      首次安装完成后，所有新账号默认为普通用户。只有 owner 可以调整角色和停用管理员。
    </p>
    <NAlert v-if="error" type="error" :show-icon="true">{{ error }}</NAlert>
    <NAlert v-if="reachedPageLimit" type="info" :show-icon="true">
      只显示最新注册的 {{ USER_PAGE_LIMIT }} 个账号。
    </NAlert>
    <NSpin :show="loading">
      <div class="user-list">
        <div v-for="user in users" :key="user.id" class="user-row">
          <div class="user-main">
            <strong
              >{{ user.display_name }} <span class="username">@{{ user.username }}</span></strong
            >
            <span class="muted"
              >{{ user.email || '外部登录账号' }} · {{ roleLabel(user.role) }}</span
            >
          </div>
          <div class="user-actions">
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
        <p v-if="!users.length && !loading" class="muted">暂无用户</p>
      </div>
    </NSpin>

    <h4>最近管理员操作</h4>
    <div class="audit-list">
      <div v-for="log in auditLogs" :key="log.id" class="audit-row">
        <span>{{ log.action }}</span>
        <span class="muted">{{ log.result }} · {{ formatTime(log.created_at) }}</span>
      </div>
      <p v-if="!auditLogs.length" class="muted">暂无审计记录</p>
    </div>
  </div>
</template>

<style scoped>
.users-panel {
  padding: 24px 0;
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
