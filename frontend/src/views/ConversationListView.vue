<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import {
  NButton,
  NEmpty,
  NIcon,
  NInput,
  NSelect,
  NSpin,
  NTag,
  useDialog,
  useMessage,
} from 'naive-ui'
import { AddOutline, LockClosedOutline, SearchOutline, SparklesOutline } from '@vicons/ionicons5'
import ConversationStatusBadge from '@/components/ConversationStatusBadge.vue'
import CreateConversationModal from '@/components/CreateConversationModal.vue'
import MySharesView from '@/views/MySharesView.vue'
import { useChallengeStore } from '@/stores/challenges'
import { useConversationStore } from '@/stores/conversations'
import type { Conversation } from '@/types'

const route = useRoute()
const router = useRouter()
const dialog = useDialog()
const message = useMessage()
const convStore = useConversationStore()
const challenges = useChallengeStore()
const filter = ref('')
const search = ref('')
const showCreate = ref(false)
const initialChallenge = ref('')
const isSharesTab = computed(() => route.name === 'shares')

const filterOptions = [
  { label: '全部', value: '' },
  { label: '进行中', value: 'active' },
  { label: '已结束', value: 'ended' },
  { label: '已归档', value: 'archived' },
]

async function refresh() {
  await convStore.loadList({ status: filter.value || undefined, search: search.value || undefined })
}

function openChallenge(slug = '') {
  initialChallenge.value = slug
  showCreate.value = true
}

onMounted(async () => {
  const results = await Promise.allSettled([refresh(), challenges.load()])
  for (const result of results) {
    if (result.status === 'rejected') message.error(result.reason?.message || '页面数据加载失败')
  }
  if (route.query.action === 'new') {
    openChallenge()
    await router.replace({ query: { ...route.query, action: undefined } })
  }
})

function onCreated(conv: Conversation) {
  void router.push(`/conversations/${conv.id}`)
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

const fmt = (value: string) => new Date(value).toLocaleString('zh-CN')
</script>

<template>
  <div class="list-page">
    <section class="challenge-band">
      <div class="section-heading">
        <div>
          <h2>玩法</h2>
          <p>选择单局挑战或长期世界，开始一段新的经历。</p>
        </div>
        <NButton type="primary" @click="openChallenge()">
          <template #icon><NIcon :component="AddOutline" /></template>
          开始玩法
        </NButton>
      </div>

      <div v-if="challenges.loading" class="center-pad"><NSpin /></div>
      <NEmpty v-else-if="!challenges.list.length" description="管理员尚未发布玩法" />
      <div v-else class="challenge-grid">
        <article v-for="challenge in challenges.list" :key="challenge.id" class="challenge-item">
          <div class="challenge-title">
            <NIcon class="challenge-icon" :component="SparklesOutline" />
            <div>
              <h3>{{ challenge.name }}</h3>
              <div class="challenge-meta">
                <NTag size="small" :bordered="false" type="info">
                  {{ challenge.runtime.mode === 'campaign' ? '长期世界' : '单局挑战' }}
                </NTag>
                <span>v{{ challenge.version }}</span>
              </div>
            </div>
          </div>
          <p>{{ challenge.description }}</p>
          <div class="tag-row">
            <NTag
              v-for="tag in challenge.tags.slice(0, 4)"
              :key="tag"
              size="small"
              :bordered="false"
            >
              {{ tag }}
            </NTag>
          </div>
          <div class="challenge-footer">
            <span>{{
              challenge.available
                ? `${challenge.personas.length} 种人设`
                : challenge.unavailable_reason
            }}</span>
            <NButton
              secondary
              type="primary"
              size="small"
              :disabled="!challenge.available"
              @click="openChallenge(challenge.slug)"
            >
              <template v-if="!challenge.available" #icon
                ><NIcon :component="LockClosedOutline"
              /></template>
              {{ challenge.available ? '进入' : '未解锁' }}
            </NButton>
          </div>
        </article>
      </div>
    </section>

    <section class="conversation-band">
      <div class="section-heading compact">
        <div>
          <nav class="section-tabs" aria-label="我的内容">
            <RouterLink to="/">我的对话</RouterLink>
            <RouterLink to="/shares">我的分享</RouterLink>
          </nav>
          <p>
            {{
              isSharesTab
                ? '查看和管理已经发布的故事快照。'
                : '继续进行中的旅程，或查看已经完成的记录。'
            }}
          </p>
        </div>
      </div>
      <MySharesView v-if="isSharesTab" />
      <template v-else>
        <div class="toolbar">
          <NSelect
            v-model:value="filter"
            class="filter-select"
            :options="filterOptions"
            @update:value="refresh"
          />
          <NInput v-model:value="search" placeholder="搜索标题" clearable @keyup.enter="refresh">
            <template #prefix><NIcon :component="SearchOutline" /></template>
          </NInput>
        </div>

        <div v-if="convStore.loading" class="center-pad"><NSpin /></div>
        <NEmpty
          v-else-if="!convStore.list.length"
          description="还没有挑战记录"
          class="center-pad"
        />
        <div v-else class="conversation-grid">
          <article
            v-for="conversation in convStore.list"
            :key="conversation.id"
            class="conversation-item"
            @click="router.push(`/conversations/${conversation.id}`)"
          >
            <div class="item-head">
              <strong>{{ conversation.title }}</strong>
              <ConversationStatusBadge :status="conversation.status" />
            </div>
            <div class="meta">{{ fmt(conversation.updated_at) }}</div>
            <div class="item-actions" @click.stop>
              <NButton
                v-if="conversation.status === 'active'"
                quaternary
                size="tiny"
                @click="archive(conversation.id)"
                >归档</NButton
              >
              <NButton quaternary size="tiny" type="error" @click="remove(conversation.id)"
                >删除</NButton
              >
            </div>
          </article>
        </div>
      </template>
    </section>

    <CreateConversationModal
      v-model:show="showCreate"
      :challenge-slug="initialChallenge"
      @created="onCreated"
    />
  </div>
</template>

<style scoped>
.list-page {
  max-width: 1080px;
  margin: 0 auto;
}

.challenge-band,
.conversation-band {
  padding-bottom: 30px;
}

.conversation-band {
  padding-top: 26px;
  border-top: 1px solid var(--border-soft);
}

.section-heading,
.toolbar,
.item-head,
.challenge-footer,
.challenge-title,
.tag-row,
.item-actions {
  display: flex;
  align-items: center;
}

.section-heading {
  justify-content: space-between;
  gap: 20px;
  margin-bottom: 18px;
}

.section-heading h2,
.section-heading p,
.challenge-title h3,
.challenge-item p {
  margin: 0;
}

.section-heading h2 {
  font-size: 20px;
  letter-spacing: 0;
}

.section-tabs {
  display: flex;
  align-items: center;
  gap: 20px;
  margin-bottom: 5px;
}

.section-tabs a {
  position: relative;
  padding-bottom: 5px;
  color: var(--text-faint);
  font-size: 15px;
  font-weight: 600;
}

.section-tabs a::after {
  content: '';
  position: absolute;
  right: 0;
  bottom: 0;
  left: 0;
  height: 2px;
  background: var(--accent);
  opacity: 0;
}

.section-tabs a.router-link-exact-active {
  color: var(--text);
}

.section-tabs a.router-link-exact-active::after {
  opacity: 1;
}

.section-heading p,
.challenge-item p,
.meta,
.challenge-footer span,
.challenge-title span {
  color: var(--text-faint);
  font-size: 12px;
}

.challenge-grid,
.conversation-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(290px, 1fr));
  gap: 14px;
}

.challenge-item,
.conversation-item {
  border: 1px solid var(--border-soft);
  border-radius: 6px;
  background: var(--bg-panel);
}

.challenge-item {
  display: flex;
  min-height: 210px;
  flex-direction: column;
  padding: 18px;
}

.challenge-title {
  gap: 11px;
}

.challenge-meta {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 4px;
}

.challenge-title h3 {
  font-size: 16px;
}

.challenge-icon {
  display: grid;
  width: 38px;
  height: 38px;
  place-items: center;
  color: var(--accent);
  border: 1px solid var(--border-soft);
}

.challenge-item p {
  margin: 14px 0;
  line-height: 1.7;
}

.tag-row {
  flex-wrap: wrap;
  gap: 6px;
}

.challenge-footer {
  justify-content: space-between;
  margin-top: auto;
  padding-top: 16px;
}

.toolbar {
  gap: 10px;
  margin-bottom: 16px;
}

.filter-select {
  width: 130px;
}

.toolbar :deep(.n-input) {
  flex: 1;
}

.conversation-item {
  padding: 15px 16px;
  cursor: pointer;
  transition:
    border-color 0.2s ease,
    transform 0.2s ease;
}

.conversation-item:hover {
  border-color: var(--accent);
  transform: translateY(-1px);
}

.item-head {
  justify-content: space-between;
  gap: 12px;
}

.meta {
  margin-top: 6px;
}

.item-actions {
  justify-content: flex-end;
  margin-top: 8px;
  opacity: 0;
}

.conversation-item:hover .item-actions {
  opacity: 1;
}

.center-pad {
  display: flex;
  justify-content: center;
  padding: 40px 0;
}

@media (max-width: 680px) {
  .section-heading {
    align-items: flex-start;
    flex-direction: column;
    gap: 12px;
  }

  .section-heading > .n-button {
    width: 100%;
  }

  .toolbar {
    align-items: stretch;
    flex-direction: column;
  }

  .filter-select {
    width: 100%;
  }

  .challenge-grid,
  .conversation-grid {
    grid-template-columns: 1fr;
  }

  .item-actions {
    opacity: 1;
  }

  .challenge-item {
    min-height: 0;
    padding: 15px;
  }
}
</style>
