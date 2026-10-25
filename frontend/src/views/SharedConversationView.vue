<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import {
  NAvatar,
  NButton,
  NIcon,
  NInput,
  NModal,
  NCard,
  NSelect,
  NSpin,
  useDialog,
  useMessage,
} from 'naive-ui'
import { ArrowUpOutline, CopyOutline, FlagOutline } from '@vicons/ionicons5'
import SharedConversationContent from '@/components/SharedConversationContent.vue'
import LlmRouteTag from '@/components/LlmRouteTag.vue'
import ThemeToggle from '@/components/ThemeToggle.vue'
import { sharesApi } from '@/api'
import { useShareStore } from '@/stores/shares'

const route = useRoute()
const store = useShareStore()
const message = useMessage()
const dialog = useDialog()
const page = ref<HTMLElement | null>(null)
const reportOpen = ref(false)
const reporting = ref(false)
const reportReason = ref('abuse')
const reportDetails = ref('')
const token = computed(() => route.params.token as string)
const snapshot = computed(() => store.current?.snapshot || null)
const avatarURL = computed(() => {
  if (!snapshot.value?.show_username || !snapshot.value.avatar_url) return undefined
  return snapshot.value.avatar_url
})
const reportOptions = [
  { label: '隐私泄漏', value: 'privacy' },
  { label: '滥用或不当内容', value: 'abuse' },
  { label: '版权问题', value: 'copyright' },
  { label: '其他', value: 'other' },
]

let robotsMeta: HTMLMetaElement | null = null
let referrerMeta: HTMLMetaElement | null = null

onMounted(async () => {
  robotsMeta = addMeta('robots', 'noindex,nofollow')
  referrerMeta = addMeta('referrer', 'no-referrer')
  if (route.path.startsWith('/shared/')) {
    window.history.replaceState(window.history.state, '', `/s/${encodeURIComponent(token.value)}`)
  }
  await load()
})

watch(token, () => load())

onBeforeUnmount(() => {
  robotsMeta?.remove()
  referrerMeta?.remove()
  store.reset()
})

async function load() {
  try {
    await store.load(token.value)
    if (snapshot.value) document.title = `${snapshot.value.title} · 命运茶馆`
  } catch {
    document.title = '分享已失效 · 命运茶馆'
  }
}

function addMeta(name: string, content: string) {
  const element = document.createElement('meta')
  element.name = name
  element.content = content
  document.head.appendChild(element)
  return element
}

async function copyLink() {
  const url = canonicalURL()
  try {
    await navigator.clipboard.writeText(url)
    message.success('链接已复制')
  } catch {
    dialog.info({
      title: '复制分享链接',
      content: url,
      positiveText: '知道了',
    })
  }
}

function canonicalURL() {
  return new URL(`/s/${encodeURIComponent(token.value)}`, window.location.origin).toString()
}

async function submitReport() {
  reporting.value = true
  try {
    await sharesApi.report(token.value, {
      reason: reportReason.value,
      details: reportDetails.value.trim(),
    })
    message.success('举报已提交')
    reportOpen.value = false
    reportDetails.value = ''
  } catch (error: any) {
    message.error(error?.message || '提交举报失败')
  } finally {
    reporting.value = false
  }
}

function scrollToTop() {
  page.value?.scrollTo({ top: 0, behavior: 'smooth' })
}
</script>

<template>
  <div ref="page" class="shared-page">
    <header class="shared-toolbar">
      <a class="shared-brand" href="/">命运茶馆 <span>fateCraft</span></a>
      <div class="shared-actions">
        <NButton quaternary circle aria-label="复制分享链接" @click="copyLink">
          <template #icon><NIcon :component="CopyOutline" /></template>
        </NButton>
        <ThemeToggle />
      </div>
    </header>

    <main class="shared-main">
      <NSpin v-if="store.loading" class="shared-loading" size="large" />
      <section v-else-if="store.error" class="shared-state">
        <h1>分享已失效</h1>
        <p>{{ store.error }}</p>
        <a href="/">返回命运茶馆</a>
      </section>
      <template v-else-if="snapshot">
        <header class="story-header">
          <p class="challenge-name">
            {{ snapshot.challenge.name }} · {{ snapshot.challenge.version }}
          </p>
          <h1>{{ snapshot.title }}</h1>
          <div class="story-byline">
            <LlmRouteTag :route="snapshot.llm_route" />
            <NAvatar
              v-if="snapshot.show_username && avatarURL"
              round
              :size="34"
              :src="avatarURL"
              :alt="snapshot.attribution"
            />
            <NAvatar
              v-else-if="snapshot.show_username"
              round
              :size="34"
              :alt="snapshot.attribution"
            >
              {{ Array.from(snapshot.attribution)[0] }}
            </NAvatar>
            <p>
              {{ snapshot.attribution || '匿名旅人' }} ·
              {{ new Date(snapshot.created_at).toLocaleString() }}
            </p>
          </div>
        </header>
        <SharedConversationContent :snapshot="snapshot" />
        <footer class="story-footer">
          <NButton quaternary @click="scrollToTop">
            <template #icon><NIcon :component="ArrowUpOutline" /></template>
            返回顶部
          </NButton>
          <NButton quaternary @click="reportOpen = true">
            <template #icon><NIcon :component="FlagOutline" /></template>
            举报
          </NButton>
        </footer>
      </template>
    </main>

    <NModal v-model:show="reportOpen">
      <NCard
        class="report-dialog"
        title="举报分享"
        closable
        content-style="max-height: calc(100vh - 160px); max-height: calc(100dvh - 160px); overflow: auto"
        @close="reportOpen = false"
      >
        <div class="report-form">
          <NSelect v-model:value="reportReason" :options="reportOptions" />
          <NInput
            v-model:value="reportDetails"
            type="textarea"
            maxlength="500"
            show-count
            placeholder="补充说明（可选）"
          />
          <NButton type="primary" :loading="reporting" @click="submitReport">提交</NButton>
        </div>
      </NCard>
    </NModal>
  </div>
</template>

<style scoped>
.shared-page {
  height: 100%;
  overflow-y: auto;
  background: var(--bg);
}
.shared-toolbar {
  position: sticky;
  z-index: 5;
  top: 0;
  display: flex;
  min-height: 56px;
  align-items: center;
  justify-content: space-between;
  padding: 0 24px;
  border-bottom: 1px solid var(--border-soft);
  background: color-mix(in srgb, var(--bg) 92%, transparent);
  backdrop-filter: blur(8px);
}
.shared-brand {
  color: var(--text);
  font-weight: 700;
}
.shared-brand span {
  margin-left: 8px;
  color: var(--text-faint);
  font-size: 11px;
}
.shared-actions {
  display: flex;
  align-items: center;
  gap: 4px;
}
.shared-main {
  width: min(760px, calc(100% - 32px));
  min-height: calc(100vh - 56px);
  margin: 0 auto;
  padding: 42px 0 64px;
}
.shared-loading {
  display: block;
  margin-top: 25vh;
}
.shared-state {
  margin-top: 20vh;
  text-align: center;
}
.story-header {
  margin-bottom: 36px;
  padding-bottom: 24px;
  border-bottom: 1px solid var(--border-soft);
}
.story-header h1 {
  margin: 8px 0 10px;
  overflow-wrap: anywhere;
  font-size: 30px;
  letter-spacing: 0;
}
.story-header p,
.challenge-name {
  margin: 0;
  color: var(--text-faint);
}
.story-byline {
  display: flex;
  align-items: center;
  gap: 10px;
}
.challenge-name {
  color: var(--accent);
  font-size: 12px;
}
.story-footer {
  display: flex;
  justify-content: space-between;
  margin-top: 40px;
  padding-top: 20px;
  border-top: 1px solid var(--border-soft);
}
.report-dialog {
  width: min(480px, calc(100vw - 24px));
}
.report-form {
  display: grid;
  gap: 12px;
}
@media (max-width: 600px) {
  .shared-toolbar {
    padding: 0 12px;
  }
  .shared-main {
    width: min(100% - 24px, 760px);
    padding-top: 28px;
  }
  .story-header h1 {
    font-size: 25px;
  }
}
</style>
