<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  NButton,
  NEmpty,
  NIcon,
  NInput,
  NPagination,
  NSelect,
  NSpin,
  NTag,
  useDialog,
  useMessage,
} from 'naive-ui'
import {
  ChatbubbleEllipsesOutline,
  CopyOutline,
  OpenOutline,
  RefreshOutline,
  SearchOutline,
  TrashOutline,
} from '@vicons/ionicons5'
import ConversationShareDialog from '@/components/ConversationShareDialog.vue'
import { sharesApi } from '@/api'
import type { ConversationShare, ConversationShareStatus } from '@/types'

const router = useRouter()
const dialog = useDialog()
const message = useMessage()
const loading = ref(false)
const shares = ref<ConversationShare[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = 20
const status = ref('')
const searchInput = ref('')
const search = ref('')
const replacement = ref<ConversationShare | null>(null)
const replacementOpen = ref(false)

const statusOptions = [
  { label: '全部状态', value: '' },
  { label: '有效', value: 'active' },
  { label: '已过期', value: 'expired' },
  { label: '已撤销', value: 'revoked' },
  { label: '已下架', value: 'moderated' },
]

const statusPresentation: Record<
  ConversationShareStatus,
  { label: string; type: 'success' | 'warning' | 'default' | 'error' }
> = {
  active: { label: '有效', type: 'success' },
  expired: { label: '已过期', type: 'warning' },
  revoked: { label: '已撤销', type: 'default' },
  moderated: { label: '已下架', type: 'error' },
}

const pageCount = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))

onMounted(() => load())

async function load() {
  loading.value = true
  try {
    const result = await sharesApi.listOwned({
      status: status.value || undefined,
      search: search.value || undefined,
      page: page.value,
      page_size: pageSize,
    })
    shares.value = result.shares
    total.value = result.total
    if (page.value > pageCount.value) {
      page.value = pageCount.value
      await load()
    }
  } catch (error: any) {
    message.error(error?.message || '加载分享记录失败')
  } finally {
    loading.value = false
  }
}

function applyFilters() {
  search.value = searchInput.value.trim()
  page.value = 1
  void load()
}

function changeStatus() {
  page.value = 1
  void load()
}

function changePage(next: number) {
  page.value = next
  void load()
}

function absoluteURL(path: string) {
  return new URL(path, window.location.origin).toString()
}

async function copyURL(item: ConversationShare) {
  const url = absoluteURL(item.public_url)
  try {
    await navigator.clipboard.writeText(url)
    message.success('分享链接已复制')
  } catch {
    window.prompt('复制分享链接', url)
  }
}

function openShare(item: ConversationShare) {
  window.open(absoluteURL(item.public_url), '_blank', 'noopener')
}

function openReplacement(item: ConversationShare) {
  replacement.value = item
  replacementOpen.value = true
}

function revoke(item: ConversationShare) {
  dialog.warning({
    title: '撤销分享',
    content: `撤销“${item.title}”后，持有链接的人将无法继续访问。`,
    positiveText: '撤销',
    negativeText: '取消',
    onPositiveClick: async () => {
      try {
        await sharesApi.revoke(item.id)
        message.success('分享已撤销')
        await load()
      } catch (error: any) {
        message.error(error?.message || '撤销失败')
      }
    },
  })
}

async function replacementPublished() {
  replacement.value = null
  await load()
}

const fmt = (value: string) => new Date(value).toLocaleString('zh-CN')
</script>

<template>
  <div class="shares-panel">
    <div class="share-toolbar">
      <NSelect
        v-model:value="status"
        class="status-filter"
        :options="statusOptions"
        @update:value="changeStatus"
      />
      <NInput
        v-model:value="searchInput"
        clearable
        placeholder="搜索分享或来源对话"
        @keyup.enter="applyFilters"
        @clear="applyFilters"
      >
        <template #prefix><NIcon :component="SearchOutline" /></template>
      </NInput>
      <NButton @click="applyFilters">搜索</NButton>
      <NButton quaternary circle aria-label="刷新分享列表" @click="load">
        <template #icon><NIcon :component="RefreshOutline" /></template>
      </NButton>
    </div>

    <div v-if="loading" class="loading-state"><NSpin /></div>
    <NEmpty v-else-if="!shares.length" class="empty-state" description="没有符合条件的分享" />
    <section v-else class="share-list" aria-label="分享记录">
      <article v-for="item in shares" :key="item.id" class="share-row">
        <div class="share-main">
          <div class="share-title-line">
            <strong>{{ item.title }}</strong>
            <NTag size="small" :bordered="false" :type="statusPresentation[item.status].type">
              {{ statusPresentation[item.status].label }}
            </NTag>
          </div>
          <button
            class="source-link"
            type="button"
            @click="router.push(`/conversations/${item.conversation_id}`)"
          >
            <NIcon :component="ChatbubbleEllipsesOutline" />
            {{ item.conversation_title || '来源对话' }}
          </button>
          <div class="share-meta">
            <span>{{ item.message_count }} 条消息</span>
            <span>{{ item.scope === 'all' ? '全部消息' : '连续片段' }}</span>
            <span>{{ item.show_username ? item.public_attribution : '匿名' }}</span>
            <span>发布于 {{ fmt(item.created_at) }}</span>
            <span v-if="item.expires_at">到期于 {{ fmt(item.expires_at) }}</span>
            <span v-else>永久有效</span>
          </div>
        </div>
        <div class="share-actions">
          <NButton size="small" :disabled="item.status !== 'active'" @click="copyURL(item)">
            <template #icon><NIcon :component="CopyOutline" /></template>
            复制
          </NButton>
          <NButton size="small" :disabled="item.status !== 'active'" @click="openShare(item)">
            <template #icon><NIcon :component="OpenOutline" /></template>
            浏览
          </NButton>
          <NButton
            size="small"
            :disabled="item.status !== 'active' && item.status !== 'expired'"
            @click="openReplacement(item)"
          >
            <template #icon><NIcon :component="RefreshOutline" /></template>
            重新发布
          </NButton>
          <NButton
            size="small"
            type="error"
            :disabled="item.status !== 'active'"
            @click="revoke(item)"
          >
            <template #icon><NIcon :component="TrashOutline" /></template>
            撤销
          </NButton>
        </div>
      </article>
    </section>

    <NPagination
      v-if="total > pageSize"
      class="pagination"
      :page="page"
      :page-count="pageCount"
      @update:page="changePage"
    />

    <ConversationShareDialog
      v-if="replacement"
      v-model:show="replacementOpen"
      :conversation-id="replacement.conversation_id"
      :conversation-title="replacement.conversation_title || replacement.title"
      :replace-target="replacement"
      @published="replacementPublished"
    />
  </div>
</template>

<style scoped>
.share-toolbar {
  display: grid;
  grid-template-columns: 160px minmax(240px, 1fr) auto auto;
  gap: 10px;
  margin-bottom: 20px;
}
.loading-state,
.empty-state {
  display: grid;
  min-height: 260px;
  place-items: center;
}
.share-list {
  border-top: 1px solid var(--border-soft);
}
.share-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: 24px;
  padding: 18px 4px;
  border-bottom: 1px solid var(--border-soft);
}
.share-main {
  min-width: 0;
}
.share-title-line {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 10px;
}
.share-title-line strong {
  overflow: hidden;
  font-size: 15px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.source-link {
  display: inline-flex;
  max-width: 100%;
  align-items: center;
  gap: 5px;
  margin: 7px 0;
  padding: 0;
  overflow: hidden;
  border: 0;
  background: transparent;
  color: var(--accent);
  cursor: pointer;
  font: inherit;
  font-size: 13px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.source-link .n-icon {
  flex: 0 0 auto;
}
.share-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 4px 14px;
  color: var(--text-faint);
  font-size: 12px;
}
.share-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 8px;
}
.pagination {
  display: flex;
  justify-content: center;
  margin-top: 24px;
}
@media (max-width: 760px) {
  .share-toolbar {
    grid-template-columns: minmax(0, 1fr) auto auto;
  }
  .status-filter {
    grid-column: 1 / -1;
  }
  .share-row {
    grid-template-columns: 1fr;
    gap: 14px;
  }
  .share-actions {
    justify-content: flex-start;
  }
}
</style>
