<script setup lang="ts">
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NButton, NIcon, useDialog } from 'naive-ui'
import {
  ChatbubblesOutline,
  ChevronBackOutline,
  DocumentTextOutline,
  StatsChartOutline,
  ShareSocialOutline,
} from '@vicons/ionicons5'
import CharacterSidebar from '@/components/CharacterSidebar.vue'
import MessageTimeline from '@/components/MessageTimeline.vue'
import ContextInspector from '@/components/ContextInspector.vue'
import ActionInput from '@/components/ActionInput.vue'
import ConversationGuidanceBar from '@/components/ConversationGuidanceBar.vue'
import { useGuidanceStore } from '@/stores/guidance'
import ConversationStatusBadge from '@/components/ConversationStatusBadge.vue'
import ThemeToggle from '@/components/ThemeToggle.vue'
import { useConversationStore } from '@/stores/conversations'
import { useChatStore } from '@/stores/chat'
import { useStateStore } from '@/stores/state'
import { useContextStore } from '@/stores/context'
import { useSocketStore } from '@/stores/socket'
import { useRuntimeStore } from '@/stores/runtime'
import { useRewardStore } from '@/stores/rewards'
import { useCampaignStore } from '@/stores/campaign'
import { useInquiryStore } from '@/stores/inquiry'
import type { CompletionInfo } from '@/stores/conversations'

const ConversationShareDialog = defineAsyncComponent(
  () => import('@/components/ConversationShareDialog.vue'),
)

const route = useRoute()
const router = useRouter()
const dialog = useDialog()
const convStore = useConversationStore()
const chat = useChatStore()
const stateStore = useStateStore()
const context = useContextStore()
const socket = useSocketStore()
const runtime = useRuntimeStore()
const guidance = useGuidanceStore()
const rewards = useRewardStore()
const campaign = useCampaignStore()
const inquiry = useInquiryStore()
const forking = ref(false)
const showShare = ref(false)

const showInspector = ref(true)
const mobilePane = ref<'state' | 'chat' | 'context'>('chat')
const convId = computed(() => route.params.id as string)
const editable = computed(() => convStore.current?.status === 'active')

watch(
  () => convStore.completion,
  (completion) => completion && showCompletion(completion),
)

function showCompletion(completion: CompletionInfo) {
  const presentation = runtime.presentation?.completion_view
  const rewardLines = completion.rewards.map(
    (reward) =>
      `${reward.name}：${reward.quantity.toLocaleString()} ${reward.unit}（余额 ${reward.balance_after.toLocaleString()}）`,
  )
  const metricLines = completion.metrics.map(
    (metric) => `${metric.label}：${String(metric.value)}${metric.unit ? ` ${metric.unit}` : ''}`,
  )
  const details = [
    completion.summary,
    rewardLines.length ? `获得奖励\n${rewardLines.join('\n')}` : presentation?.empty_reward_text,
    metricLines.length ? metricLines.join('\n') : '',
  ].filter(Boolean)
  const replay = completion.nextActions.includes('replay')
  const back = completion.nextActions.includes('back_to_challenges')
  const options = {
    title: completion.title || '挑战完成',
    content: details.join('\n\n'),
    positiveText: replay
      ? presentation?.replay_label || '再试一次'
      : back
        ? presentation?.back_label || '返回列表'
        : '关闭',
    negativeText: replay && back ? presentation?.back_label || '返回列表' : undefined,
    style: { maxWidth: '520px' },
    onPositiveClick: () => {
      convStore.clearEnding()
      if (replay) router.push('/?action=new')
      else if (back) router.push('/')
    },
    onNegativeClick: () => {
      convStore.clearEnding()
      if (back) router.push('/')
    },
    onMaskClick: () => convStore.clearEnding(),
  }
  if (completion.category === 'success') dialog.success(options)
  else if (completion.category === 'failure') dialog.error(options)
  else dialog.info(options)
}

// 组件是否已卸载：卸载后进行中的 open() 不得再触碰 store / socket
let disposed = false
let activityTimer: ReturnType<typeof setInterval> | null = null

// 最新 id 守卫：open() 的每个 await 期间用户可能已切换对话或离开本页，
// 陈旧的 open 继续跑会用旧对话数据覆盖新对话、其尾部的 socket.connect
// 还会杀掉新对话的连接；发生在卸载后则留下无限重连的定时器泄漏。
function stale(id: string) {
  return disposed || id !== convId.value
}

async function open(id: string) {
  // 切换对话时清空所有缓存（doc 05）。
  chat.reset()
  guidance.reset()
  stateStore.reset()
  context.reset()
  runtime.reset()
  campaign.reset()
  inquiry.reset()
  socket.disconnect()

  await convStore.load(id)
  if (stale(id)) return
  // 进入正在生成的对话（刷新页面 / 从列表点入）：agent.turn.started 已经
  // 错过，不标记的话输入框不锁、也没有任何“生成中”指示。占位与增量由
  // 后续 WS 事件补齐，结束兜底见 activity 轮询。
  if (convStore.current?.is_processing) chat.markGenerating()
  await Promise.all([
    chat.load(id),
    guidance.load(id),
    stateStore.load(id),
    context.loadRuntimeInspector(id),
    runtime.load(id),
    convStore.current?.campaign_id
      ? campaign.load(convStore.current.campaign_id)
      : Promise.resolve(null),
  ])
  if (stale(id)) return
  socket.connect(id)
}

onMounted(async () => {
  await rewards.load().catch(() => {})
  await open(convId.value)
  activityTimer = setInterval(async () => {
    if (disposed || !convId.value) return
    await context.loadActivity(convId.value)
    // 兜底复位：open() 因 is_processing 标记的“生成中”在服务端结束、而
    // 本端又错过了收尾事件时（narrative 回到 idle 且没有流式占位），把
    // 状态收回来，避免输入框永久锁死。有流式占位说明生成仍在事件流内，
    // 交由 turn.completed / failed / error 正常收尾。
    if (
      !disposed &&
      chat.isGenerating &&
      !chat.streamingId &&
      context.activity.narrative === 'idle'
    ) {
      chat.stopGenerating()
      guidance.load(convId.value)
      chat.load(convId.value).catch(() => {})
    }
  }, 5000)
})

watch(convId, (id, prev) => {
  if (id && id !== prev) open(id)
})

onBeforeUnmount(() => {
  // 先置卸载标记，让进行中的 open() 在下一个 await 后自行中止，
  // 避免陈旧续体在列表页重新 connect 出无限重连定时器
  disposed = true
  if (activityTimer) {
    clearInterval(activityTimer)
    activityTimer = null
  }
  socket.disconnect()
  convStore.clearCurrent()
  campaign.reset()
  inquiry.reset()
})

function endConversation() {
  dialog.warning({
    title: '结束对话',
    content: '确定结束这个对话？结束后将无法继续生成。',
    positiveText: '结束',
    negativeText: '取消',
    onPositiveClick: () => convStore.end(convId.value),
  })
}

async function forkConversation() {
  if (forking.value || !convStore.current || convStore.current.campaign_id) return
  forking.value = true
  try {
    const created = await convStore.fork(convId.value)
    await router.push(`/conversations/${created.id}`)
  } catch (error: any) {
    // ActionInput already owns generation errors; this action is local to the
    // page, so keep its failure visible without mutating the source record.
    dialog.error({ title: '无法创建新对话', content: error?.message || '请稍后重试' })
  } finally {
    forking.value = false
  }
}
</script>

<template>
  <div class="conv-page">
    <header class="bar row spread">
      <div class="bar-main row">
        <NButton
          class="back-button"
          quaternary
          size="small"
          aria-label="返回挑战列表"
          @click="router.push('/')"
        >
          <template #icon><NIcon :component="ChevronBackOutline" /></template>
          <span class="back-label">列表</span>
        </NButton>
        <span class="title">{{ convStore.current?.title || '加载中…' }}</span>
        <ConversationStatusBadge
          v-if="convStore.current"
          class="conversation-status"
          :status="convStore.current.status"
        />
      </div>
      <div class="bar-actions row">
        <NButton
          quaternary
          circle
          size="small"
          aria-label="分享对话"
          :disabled="
            !convStore.current ||
            convStore.current.status === 'deleted' ||
            convStore.current.is_processing
          "
          @click="showShare = true"
        >
          <template #icon><NIcon :component="ShareSocialOutline" /></template>
        </NButton>
        <NButton
          class="inspector-toggle"
          quaternary
          size="small"
          @click="showInspector = !showInspector"
        >
          {{ showInspector ? '隐藏检查器' : '显示检查器' }}
        </NButton>
        <NButton v-if="editable" quaternary size="small" type="error" @click="endConversation"
          >结束</NButton
        >
        <NButton
          v-else-if="
            convStore.current &&
            !convStore.current.campaign_id &&
            convStore.current.status !== 'deleted'
          "
          quaternary
          size="small"
          :loading="forking"
          @click="forkConversation"
          >基于新版本继续</NButton
        >
        <ThemeToggle />
      </div>
    </header>

    <ConversationShareDialog
      v-if="showShare && convStore.current"
      v-model:show="showShare"
      :conversation-id="convId"
      :conversation-title="convStore.current.title"
      :disabled="convStore.current.is_processing"
    />

    <div v-if="!editable && convStore.current" class="ended-banner">
      {{
        convStore.current.status === 'archived'
          ? '此版本已封存，只能浏览；可基于新版本继续。'
          : '此挑战已结束'
      }}
    </div>

    <!-- 错误提示统一由输入区（ActionInput）的可关闭警示条展示，页头不再重复挂一份 -->

    <nav class="mobile-pane-tabs" aria-label="对话视图">
      <button
        type="button"
        :class="{ active: mobilePane === 'state' }"
        @click="mobilePane = 'state'"
      >
        <NIcon :component="StatsChartOutline" />
        <span>状态</span>
      </button>
      <button type="button" :class="{ active: mobilePane === 'chat' }" @click="mobilePane = 'chat'">
        <NIcon :component="ChatbubblesOutline" />
        <span>叙事</span>
      </button>
      <button
        type="button"
        :class="{ active: mobilePane === 'context' }"
        @click="mobilePane = 'context'"
      >
        <NIcon :component="DocumentTextOutline" />
        <span>上下文</span>
      </button>
    </nav>

    <div class="three-pane" :class="[{ 'no-inspector': !showInspector }, `mobile-${mobilePane}`]">
      <CharacterSidebar
        class="pane-state"
        :conv-id="convId"
        :campaign="Boolean(convStore.current?.campaign_id)"
      />
      <div class="center pane-chat">
        <MessageTimeline :editable="editable" />
        <ConversationGuidanceBar :conv-id="convId" :disabled="!editable" />
        <ActionInput :conv-id="convId" :disabled="!editable" />
      </div>
      <ContextInspector class="pane-context" />
    </div>
  </div>
</template>

<style scoped>
.conv-page {
  height: 100%;
  min-height: 0;
  min-width: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.bar {
  height: 54px;
  padding: 0 14px;
  background: color-mix(in srgb, var(--bg-soft) 88%, transparent);
  border-bottom: 1px solid var(--border-soft);
  flex-shrink: 0;
}

.bar-main {
  min-width: 0;
  gap: 10px;
}

.bar-actions {
  flex-shrink: 0;
  gap: 6px;
}

.title {
  font-weight: 600;
  letter-spacing: 1px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.ended-banner {
  background: var(--accent-soft);
  color: var(--accent);
  text-align: center;
  padding: 7px;
  font-size: 13px;
  letter-spacing: 2px;
  flex-shrink: 0;
}

.three-pane {
  flex: 1;
  display: grid;
  grid-template-columns: 290px minmax(0, 1fr) 330px;
  min-height: 0;
  min-width: 0;
  overflow: hidden;
}

.three-pane.no-inspector {
  grid-template-columns: 290px minmax(0, 1fr);
}

.three-pane.no-inspector > .pane-context {
  display: none;
}

.center {
  display: flex;
  flex-direction: column;
  min-height: 0;
  min-width: 0;
  overflow: hidden;
}

.mobile-pane-tabs {
  display: none;
}

/* 移动端适配 */
@media (max-width: 768px) {
  .bar {
    height: calc(52px + env(safe-area-inset-top));
    padding: env(safe-area-inset-top) max(8px, env(safe-area-inset-right)) 0
      max(8px, env(safe-area-inset-left));
    flex-wrap: nowrap;
    gap: 4px;
  }

  .bar-main {
    flex: 1;
    gap: 5px;
    overflow: hidden;
  }

  .bar-actions {
    gap: 2px;
  }

  .inspector-toggle {
    display: none;
  }

  .title {
    flex: 1;
    min-width: 0;
    font-size: 14px;
  }

  .mobile-pane-tabs {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    min-height: 44px;
    padding: 0 max(8px, env(safe-area-inset-right)) 0 max(8px, env(safe-area-inset-left));
    background: var(--bg-soft);
    border-bottom: 1px solid var(--border-soft);
    flex-shrink: 0;
  }

  .mobile-pane-tabs button {
    position: relative;
    display: flex;
    min-width: 0;
    min-height: 44px;
    align-items: center;
    justify-content: center;
    gap: 6px;
    padding: 0 8px;
    border: 0;
    color: var(--text-faint);
    background: transparent;
    font: inherit;
    cursor: pointer;
  }

  .mobile-pane-tabs button::after {
    content: '';
    position: absolute;
    right: 12px;
    bottom: 0;
    left: 12px;
    height: 2px;
    background: transparent;
  }

  .mobile-pane-tabs button.active {
    color: var(--accent);
  }

  .mobile-pane-tabs button.active::after {
    background: var(--accent);
  }

  .mobile-pane-tabs :deep(.n-icon) {
    font-size: 16px;
  }

  .three-pane,
  .three-pane.no-inspector {
    display: block;
  }

  .three-pane > .pane-state,
  .three-pane > .pane-chat,
  .three-pane > .pane-context {
    display: none;
    width: 100%;
    height: 100%;
    min-width: 0;
    max-width: none;
  }

  .three-pane.mobile-state > .pane-state,
  .three-pane.mobile-chat > .pane-chat,
  .three-pane.mobile-context > .pane-context,
  .three-pane.no-inspector.mobile-context > .pane-context {
    display: flex;
  }
}

@media (max-width: 480px) {
  .conversation-status {
    display: none;
  }
}

@media (max-width: 380px) {
  .back-label {
    display: none;
  }
}
</style>
