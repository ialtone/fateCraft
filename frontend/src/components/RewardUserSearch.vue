<script setup lang="ts">
import { ref } from 'vue'
import { NAlert, NButton, NInput, NSpin, NEmpty } from 'naive-ui'
import { adminApi } from '@/api'
import type { AdminUser } from '@/types'
import { rewardError } from '@/lib/rewards'
const emit = defineEmits<{ select: [user: AdminUser] }>()
const query = ref(''),
  submitted = ref(''),
  error = ref('')
const users = ref<AdminUser[]>([])
const loading = ref(false),
  searched = ref(false),
  hasMore = ref(false),
  offset = ref(0)
let sequence = 0
async function search(page = 0, fresh = false) {
  if (fresh) submitted.value = query.value.trim()
  const token = ++sequence
  loading.value = true
  error.value = ''
  try {
    const result = await adminApi.listUsers({ q: submitted.value, limit: 20, offset: page })
    if (token !== sequence) return
    users.value = result.users
    hasMore.value = result.has_more
    offset.value = page
    searched.value = true
  } catch (err) {
    if (token === sequence) error.value = rewardError(err)
  } finally {
    if (token === sequence) loading.value = false
  }
}
</script>
<template>
  <div class="user-search">
    <form class="search-bar" @submit.prevent="search(0, true)">
      <NInput
        v-model:value="query"
        aria-label="查找用户"
        placeholder="用户名、显示名称、完整邮箱或用户 ID"
        :maxlength="256"
      />
      <NButton attr-type="submit" type="primary" :loading="loading">查找用户</NButton>
    </form>
    <NAlert v-if="error" type="error">{{ error }}</NAlert>
    <NSpin :show="loading">
      <div v-for="user in users" :key="user.id" class="search-user">
        <div>
          <strong>{{ user.display_name }} · @{{ user.username }}</strong
          ><small
            >{{ user.email || '无邮箱' }} · {{ user.id }} ·
            {{ user.status === 'active' ? '正常' : '已停用' }}</small
          >
        </div>
        <NButton @click="emit('select', user)">奖励与点数</NButton>
      </div>
      <NEmpty v-if="searched && !loading && !error && !users.length" description="没有匹配的用户" />
    </NSpin>
    <div v-if="searched" class="search-bar">
      <NButton :disabled="loading || offset === 0" @click="search(offset - 20)">上一页</NButton>
      <NButton :disabled="loading || !hasMore" @click="search(offset + 20)">下一页</NButton>
    </div>
  </div>
</template>
<style scoped>
.user-search {
  display: grid;
  gap: 12px;
}
.search-bar,
.search-user {
  display: flex;
  gap: 12px;
  align-items: center;
}
.search-user {
  justify-content: space-between;
  padding: 14px 0;
  border-bottom: 1px solid var(--border-soft);
}
.search-user small {
  display: block;
  color: var(--text-dim);
  overflow-wrap: anywhere;
  margin-top: 6px;
}
@media (max-width: 640px) {
  .search-user {
    align-items: flex-start;
    flex-direction: column;
  }
}
</style>
