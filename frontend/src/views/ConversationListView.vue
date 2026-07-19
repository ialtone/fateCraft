<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NButton, NEmpty, NIcon, NInput, NSelect, NSpin, useDialog } from 'naive-ui'
import { AddOutline, SearchOutline } from '@vicons/ionicons5'
import ConversationStatusBadge from '@/components/ConversationStatusBadge.vue'
import CreateConversationModal from '@/components/CreateConversationModal.vue'
import { useConversationStore } from '@/stores/conversations'
import { useTemplateStore } from '@/stores/templates'
import { usePromptStore } from '@/stores/prompts'
import { useWorldBookStore } from '@/stores/worldbooks'
import { usePresetPackStore } from '@/stores/presetPacks'
import type { Conversation } from '@/types'

const route = useRoute()
const router = useRouter()
const dialog = useDialog()
const convStore = useConversationStore()
const templates = useTemplateStore()
const prompts = usePromptStore()
const worldbooks = useWorldBookStore()
const presetPacks = usePresetPackStore()

const filter = ref<string>('')
const search = ref('')
const showCreate = ref(false)

const filterOptions = [
  { label: '全部', value: '' },
  { label: '进行中', value: 'active' },
  { label: '已结束', value: 'ended' },
  { label: '已归档', value: 'archived' },
]

async function refresh() {
  await convStore.loadList({ status: filter.value || undefined, search: search.value || undefined })
}

onMounted(async () => {
  await refresh()
  // 预取新建弹窗依赖的模板/预设资源，保证 /?action=new 打开弹窗时预设包已就绪
  await Promise.all([
    templates.loadAll().catch(() => {}),
    prompts.loadList().catch(() => {}),
    worldbooks.loadList().catch(() => {}),
    presetPacks.loadPacks().catch(() => {}),
  ])
  // 处理 /?action=new：自动打开新建弹窗并清掉查询参数
  if (route.query.action === 'new') {
    showCreate.value = true
    router.replace({ query: { ...route.query, action: undefined } })
  }
})

// 创建成功后跳转到对话页
function onCreated(conv: Conversation) {
  router.push(`/conversations/${conv.id}`)
}

async function archive(id: string) {
  await convStore.archive(id)
  await refresh()
}

function remove(id: string) {
  dialog.warning({
    title: '删除对话',
    content: '删除该对话？其消息与状态将一并隐藏。',
    positiveText: '删除',
    negativeText: '取消',
    onPositiveClick: async () => {
      await convStore.remove(id)
      await refresh()
    },
  })
}

const characterName = (id?: string | null) =>
  templates.characters.find((c) => c.id === id)?.name || '—'
const fmt = (s: string) => new Date(s).toLocaleString('zh-CN')
</script>

<template>
  <div class="list-page">
    <div class="page-title">
      <h2>对话</h2>
      <span class="ornament">◆</span>
    </div>
    <p class="page-sub">每段对话是一方独立的小世界，互不相扰。</p>

    <div class="toolbar row">
      <NSelect
        v-model:value="filter"
        :options="filterOptions"
        style="width: 130px"
        @update:value="refresh"
      />
      <NInput v-model:value="search" placeholder="搜索标题…" clearable @keyup.enter="refresh">
        <template #prefix><NIcon :component="SearchOutline" /></template>
      </NInput>
      <NButton type="primary" @click="showCreate = true">
        <template #icon><NIcon :component="AddOutline" /></template>
        新建对话
      </NButton>
    </div>

    <div v-if="convStore.loading" class="center-pad"><NSpin size="medium" /></div>
    <NEmpty
      v-else-if="!convStore.list.length"
      description="还没有对话，点击右上角新建一个。"
      class="center-pad"
    />
    <div v-else class="grid">
      <div
        v-for="(c, i) in convStore.list"
        :key="c.id"
        class="card fade-up"
        :style="{ animationDelay: Math.min(i * 45, 360) + 'ms' }"
        @click="router.push(`/conversations/${c.id}`)"
      >
        <div class="row spread">
          <span class="card-title">{{ c.title }}</span>
          <ConversationStatusBadge :status="c.status" />
        </div>
        <div class="faint sm meta">
          {{ characterName(c.character_id) }} · {{ fmt(c.updated_at) }}
        </div>
        <div class="card-actions row" @click.stop>
          <NButton quaternary size="tiny" @click="archive(c.id)">归档</NButton>
          <NButton quaternary size="tiny" type="error" @click="remove(c.id)">删除</NButton>
        </div>
      </div>
    </div>

    <!-- 新建对话弹窗（表单与预设包联动逻辑均在组件内部） -->
    <CreateConversationModal v-model:show="showCreate" @created="onCreated" />
  </div>
</template>

<style scoped>
.list-page {
  max-width: 1000px;
  margin: 0 auto;
}

.toolbar {
  gap: 10px;
  margin-bottom: 22px;
}

.toolbar :deep(.n-input) {
  flex: 1;
}

.center-pad {
  display: flex;
  justify-content: center;
  padding: 56px 0;
}

.grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(290px, 1fr));
  gap: 14px;
}

.card {
  position: relative;
  background: var(--bg-panel);
  border: 1px solid var(--border-soft);
  border-radius: var(--radius);
  padding: 16px 16px 12px 18px;
  cursor: pointer;
  transition:
    border-color 0.2s ease,
    transform 0.2s ease,
    box-shadow 0.2s ease;
  overflow: hidden;
}

/* 左侧琥珀饰条，悬停点亮 */
.card::before {
  content: '';
  position: absolute;
  left: 0;
  top: 0;
  bottom: 0;
  width: 3px;
  background: var(--border);
  transition: background 0.2s ease;
}

.card:hover {
  border-color: var(--border);
  transform: translateY(-2px);
  box-shadow: var(--shadow-card);
}

.card:hover::before {
  background: var(--accent);
}

.card-title {
  font-weight: 600;
  font-size: 15px;
  letter-spacing: 0.5px;
}

.meta {
  margin-top: 4px;
}

.sm {
  font-size: 12px;
}

.card-actions {
  margin-top: 10px;
  gap: 4px;
  opacity: 0;
  transition: opacity 0.15s ease;
}

.card:hover .card-actions {
  opacity: 1;
}

/* 移动端适配 */
@media (max-width: 768px) {
  .list-page {
    max-width: 100%;
  }

  .toolbar {
    flex-wrap: wrap;
    gap: 8px;
  }

  .toolbar .row {
    width: 100%;
  }

  .cards {
    grid-template-columns: 1fr; /* 单栏布局 */
    gap: 12px;
  }

  .card {
    padding: 14px;
  }

  .card-title {
    font-size: 15px;
  }

  .card-summary {
    font-size: 12px;
  }

  .card-meta {
    font-size: 11px;
  }
}
</style>
