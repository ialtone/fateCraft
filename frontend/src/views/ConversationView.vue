<script setup lang="ts">
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NButton, NIcon, NTooltip, useDialog, useMessage } from 'naive-ui'
import type { DialogOptions } from 'naive-ui'
import {
  ChatbubblesOutline,
  ChevronBackOutline,
  DocumentTextOutline,
  StatsChartOutline,
  ShareSocialOutline,
} from '@vicons/ionicons5'
import CharacterSidebar from '@/components/CharacterSidebar.vue'
import SavedGameplayVersion from '@/components/SavedGameplayVersion.vue'
import MessageTimeline from '@/components/MessageTimeline.vue'
import ContextInspector from '@/components/ContextInspector.vue'
import ActionInput from '@/components/ActionInput.vue'
import ConversationGuidanceBar from '@/components/ConversationGuidanceBar.vue'
import { useGuidanceStore } from '@/stores/guidance'
import ConversationStatusBadge from '@/components/ConversationStatusBadge.vue'
import ThemeToggle from '@/components/ThemeToggle.vue'
import { useConversationStore } from '@/stores/conversations'
import { useSettingsStore } from '@/stores/settings'
import { useLlmRelayStore } from '@/stores/llmRelay'
import LlmRouteTag from '@/components/LlmRouteTag.vue'
import { useChatStore } from '@/stores/chat'
import { useStateStore } from '@/stores/state'
import { useContextStore } from '@/stores/context'
import { useSocketStore } from '@/stores/socket'
import { useRuntimeStore } from '@/stores/runtime'
import { useRewardStore } from '@/stores/rewards'
import { usePlayerActionStore } from '@/stores/playerActions'
import { useCampaignStore } from '@/stores/campaign'
import { useInquiryStore } from '@/stores/inquiry'
import type { CompletionInfo } from '@/stores/conversations'

const ConversationShareDialog = defineAsyncComponent(
  () => import('@/components/ConversationShareDialog.vue'),
)

const route = useRoute()
const router = useRouter()
const dialog = useDialog()
const message = useMessage()
const convStore = useConversationStore()
const chat = useChatStore()
const stateStore = useStateStore()
const context = useContextStore()
const socket = useSocketStore()
const runtime = useRuntimeStore()
const guidance = useGuidanceStore()
const rewards = useRewardStore()
const campaign = useCampaignStore()
const playerActions = usePlayerActionStore()
const inquiry = useInquiryStore()
const settings = useSettingsStore()
const relay = useLlmRelayStore()
const forking = ref(false)
const showShare = ref(false)

const showInspector = ref(true)
const mobilePane = ref<'state' | 'chat' | 'context'>('chat')
const convId = computed(() => route.params.id as string)
const gameplayAllowed = ref(false)
const editable = computed(() => convStore.current?.status === 'active' && gameplayAllowed.value)
// 路由不匹配：对话固化路由 ≠ 当前有效路由时禁用全部生成入口。
// 设置未加载完成前不误判，后端闸门兜底。
const routeMismatch = computed(() => {
  const conv = convStore.current
  if (!conv || !settings.settings) return false
  return conv.llm_route !== settings.effectiveRoute
})
const mismatchText = computed(() => {
  const conv = convStore.current
  if (
    conv?.llm_route === 'client' &&
    settings.settings &&
    !settings.settings.client_route_enabled
  ) {
    return '平台已关闭客户端直连，此对话无法继续生成，可新建对话继续游玩。'
  }
  // server 路由随接入模式显示为“平台模型”或“服务端转发”。
  const name = (route?: string) => {
    if (route === 'client') return '客户端直连'
    return settings.settings?.access_mode === 'platform_managed' ? '平台模型' : '服务端转发'
  }
  return `此对话使用「${name(conv?.llm_route)}」模式创建，当前设置为「${name(settings.effectiveRoute)}」，无法继续生成。请切回原设置或新建对话。`
})
// 平台关闭开关导致的不匹配：只保留“新建对话”（去设置也选不了直连）。
const mismatchCausedBySwitchOff = computed(
  () =>
    convStore.current?.llm_route === 'client' &&
    !!settings.settings &&
    !settings.settings.client_route_enabled,
)
// 客户端直连且路由一致，但 Relay 未连接时不能发送。
const relayNotReady = computed(
  () =>
    convStore.current?.llm_route === 'client' &&
    settings.effectiveRoute === 'client' &&
    !relay.connected,
)
const inputDisabled = computed(() => !editable.value || routeMismatch.value || relayNotReady.value)

watch(
  () => convStore.completion,
  (completion, _previous, onCleanup) => {
    if (completion) {
      const popup = showCompletion(completion)
      onCleanup(() => popup.destroy())
    }
  },
  { immediate: true },
)

// 模型提出的中止/失败结局必须先经过明确的确认弹窗，不能以内联横幅
// 伪装成普通状态。确认后由后端执行并通过 completion 统一弹出最终结局。
let shownEndingProposalId: string | null = null
watch(
  () =>
    [
      convStore.endingState?.proposal,
      convStore.endingState?.is_processing,
      editable.value,
      chat.isGenerating,
      playerActions.acting,
      !!playerActions.pending,
    ] as const,
  ([proposal, isProcessing, canEdit, isGenerating, isActing, hasPending]) => {
    if (
      !proposal ||
      !proposal.available ||
      isProcessing ||
      !canEdit ||
      isGenerating ||
      isActing ||
      hasPending ||
      proposal.id === shownEndingProposalId
    )
      return
    if (confirmEnding(proposal.key, proposal.arguments, proposal.label, proposal.id)) {
      shownEndingProposalId = proposal.id
    }
  },
  { immediate: true },
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
  const chronicleLines = (completion.chronicle || [])
    .filter((line): line is string => typeof line === 'string' && !!line.trim())
    .map((line) => `· ${line}`)
  const details = [
    completion.summary,
    chronicleLines.length ? `本世经历\n${chronicleLines.join('\n')}` : '',
    completion.rewardsDisabledReason
      ? '客户端直连模式不发放奖励'
      : rewardLines.length
        ? `获得奖励\n${rewardLines.join('\n')}`
        : '本次获得 0 积分',
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
    onPositiveClick: () => {
      convStore.clearEnding()
      if (replay) router.push('/?action=new')
      else if (back) router.push('/')
    },
    onNegativeClick: () => {
      convStore.clearEnding()
      if (back) router.push('/')
    },
    onClose: () => convStore.clearEnding(),
    onMaskClick: () => convStore.clearEnding(),
  }
  return showEndingDialog(completion.category, options)
}

// 组件是否已卸载：卸载后进行中的 open() 不得再触碰 store / socket
let disposed = false

// 最新 id 守卫：open() 的每个 await 期间用户可能已切换对话或离开本页，
// 陈旧的 open 继续跑会用旧对话数据覆盖新对话、其尾部的 socket.connect
// 还会杀掉新对话的连接；发生在卸载后则留下无限重连的定时器泄漏。
function stale(id: string) {
  return disposed || id !== convId.value
}

async function open(id: string) {
  // 切换对话时清空所有缓存（doc 05）。
  playerActions.enter(id)
  convStore.clearCurrent()
  chat.reset()
  guidance.reset()
  stateStore.reset()
  context.reset()
  runtime.reset()
  campaign.reset()
  inquiry.reset()
  socket.disconnect()

  try {
    await convStore.load(id)
  } catch (err: any) {
    if (!stale(id)) {
      message.error(`加载对话失败：${err?.message || '未知错误'}`)
      socket.connect(id)
    }
    return
  }
  if (stale(id)) return
  // 订阅就绪后的统一恢复负责首屏快照、消息和生成状态。
  if (convStore.current?.is_processing) chat.markGenerating()
  socket.connect(id)
}

onMounted(async () => {
  window.addEventListener('resize', updateInspectorVisibility)
  document.addEventListener('visibilitychange', updateInspectorVisibility)
  await rewards.load().catch(() => {})
  // 设置决定路由匹配判断与直连徽标；失败时不阻断页面，后端闸门兜底。
  await settings.load().catch(() => {})
  await open(convId.value)
})

function updateInspectorVisibility() {
  context.setInspectorVisible(
    document.visibilityState !== 'hidden' &&
      (window.innerWidth > 768 ? showInspector.value : mobilePane.value === 'context'),
  )
}

watch(
  () => [showInspector.value, mobilePane.value, socket.currentConvId] as const,
  updateInspectorVisibility,
  { immediate: true },
)

watch(convId, (id, prev) => {
  if (id && id !== prev) open(id)
})

onBeforeUnmount(() => {
  // 先置卸载标记，让进行中的 open() 在下一个 await 后自行中止，
  // 避免陈旧续体在列表页重新 connect 出无限重连定时器
  disposed = true
  window.removeEventListener('resize', updateInspectorVisibility)
  document.removeEventListener('visibilitychange', updateInspectorVisibility)
  context.setInspectorVisible(false)
  socket.disconnect()
  convStore.clearCurrent()
  playerActions.reset()
  campaign.reset()
  inquiry.reset()
})

function showEndingDialog(category: CompletionInfo['category'], options: DialogOptions) {
  const sharedOptions = { ...options, style: { maxWidth: '520px', whiteSpace: 'pre-line' } }
  if (category === 'success') return dialog.success(sharedOptions)
  if (category === 'failure') return dialog.error(sharedOptions)
  return dialog.info(sharedOptions)
}

function confirmEnding(
  key: string,
  arguments_: Record<string, string | number | boolean>,
  label: string,
  proposalId?: string,
): boolean {
  if (playerActions.pending || playerActions.acting) return false
  const id = convId.value
  const snapshot = convStore.endingState
  if (!snapshot || snapshot.is_processing || chat.isGenerating) return false
  const capability = snapshot.ending_capabilities.find((item) => item.key === key)
  if (!capability?.available) {
    message.warning(capability?.violations?.[0]?.message || '当前条件不满足，请刷新状态。')
    return false
  }
  const plan = capability.plans?.find(
    (item) => JSON.stringify(item.arguments) === JSON.stringify(arguments_),
  )
  const metadata = plan || capability.metadata
  const costs = Object.entries(metadata?.costs || {})
    .map(([name, value]) => `${capability.cost_labels?.[name] || name}：${value}`)
    .join('，')
  const request = {
    request_id: crypto.randomUUID(),
    branch_epoch: snapshot.branch_epoch,
    expected_revision: snapshot.revision,
    key,
    arguments: arguments_,
  }
  const abandoning = key === snapshot.abandon_action
  showEndingDialog('failure', {
    title: abandoning ? '确认放弃本局' : label,
    content: [
      abandoning
        ? '这段旅程将在此结束。确认放弃后，将显示本局结局，无法继续游玩。'
        : capability.description,
      costs,
      metadata?.minutes ? `耗时 ${metadata.minutes} 分钟` : '',
      metadata?.risk,
      abandoning
        ? '已获得的阶段积分会保留，本次放弃不额外获得通关积分。'
        : '确认后将结束本局，无法继续游玩。结局与积分以最终结算为准。',
    ]
      .filter(Boolean)
      .join('\n\n'),
    positiveText: abandoning ? '确认放弃' : '确认结局',
    negativeText: '继续游玩',
    onPositiveClick: () => playerActions.execute(id, request),
    onNegativeClick: () => {
      if (proposalId) convStore.dismissProposal(proposalId)
    },
  })
  return true
}

async function endConversation() {
  const id = convId.value
  if (playerActions.pending) {
    await playerActions.execute(convId.value)
    return
  }
  try {
    const snapshot = await convStore.loadEndingState(id)
    if (stale(id)) return
    if (snapshot?.abandon_action) {
      confirmEnding(snapshot.abandon_action, {}, '中止本局')
      return
    }
    if (!snapshot || snapshot.has_trusted_actions) {
      message.warning('此玩法尚未提供中止动作，请使用玩法行动面板。')
      return
    }
    dialog.warning({
      title: '结束对话',
      content: '结束后无法继续生成。',
      positiveText: '结束',
      negativeText: '取消',
      onPositiveClick: () => {
        if (!stale(id)) return convStore.end(id)
      },
    })
  } catch (cause: any) {
    message.error(cause?.message || '读取结局条件失败，请重试')
  }
}

async function forkConversation() {
  if (forking.value || !convStore.current || convStore.current.campaign_id) return
  forking.value = true
  try {
    const issue = await settings.checkPlayReadiness()
    if (issue) {
      dialog.warning({
        title: '请先完成模型配置',
        content: issue,
        positiveText: '前往设置',
        negativeText: '取消',
        onPositiveClick: () => router.push('/settings'),
      })
      return
    }
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
        <SavedGameplayVersion
          v-if="convStore.current"
          :key="convId"
          :conversation-id="convId"
          @allowed="gameplayAllowed = $event"
        />
        <ConversationStatusBadge
          v-if="convStore.current"
          class="conversation-status"
          :status="convStore.current.status"
        />
        <LlmRouteTag v-if="convStore.current" :route="convStore.current.llm_route" />
        <NTooltip v-if="convStore.current?.llm_route === 'client'">
          <template #trigger>
            <span class="relay-dot" :class="{ on: relay.connected }" />
          </template>
          {{ relay.connected ? '客户端直连：Relay 已连接' : '客户端直连：Relay 未连接' }}
        </NTooltip>
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
        <NButton
          v-if="convStore.lastCompletion"
          quaternary
          size="small"
          @click="convStore.showLastCompletion()"
          >查看结局</NButton
        >
        <NButton
          v-if="editable"
          :disabled="chat.isGenerating || playerActions.acting"
          quaternary
          size="small"
          type="error"
          @click="endConversation"
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

    <div v-if="routeMismatch && convStore.current" class="route-banner">
      <span>{{ mismatchText }}</span>
      <span class="route-banner-actions">
        <NButton size="small" secondary @click="router.push('/?action=new')">新建对话</NButton>
        <NButton
          v-if="!mismatchCausedBySwitchOff"
          size="small"
          secondary
          @click="router.push('/settings')"
          >前往设置</NButton
        >
      </span>
    </div>
    <div v-else-if="relayNotReady" class="route-banner faint-banner">
      <span>客户端直连未就绪，请保持页面打开并检查网络后重新生成。</span>
    </div>

    <div v-if="convStore.current && convStore.current.status !== 'active'" class="ended-banner">
      {{
        convStore.current.status === 'archived'
          ? '此对话已归档，恢复后可继续原版玩法。'
          : '此挑战已结束'
      }}
    </div>

    <div v-if="playerActions.pending || playerActions.error" class="route-banner">
      <span>{{ playerActions.error || '上次操作结果尚未确认' }}</span>
      <NButton
        v-if="playerActions.pending"
        size="small"
        :loading="playerActions.acting"
        @click="playerActions.execute(convId)"
        >查询原请求</NButton
      >
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
        <MessageTimeline :editable="editable && !routeMismatch" />
        <ConversationGuidanceBar :conv-id="convId" :disabled="!editable || routeMismatch" />
        <ActionInput :conv-id="convId" :disabled="inputDisabled" />
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

/* 客户端直连 Relay 状态点：绿=已连接，灰=未连接。 */
.relay-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--text-faint);
  flex-shrink: 0;
}

.relay-dot.on {
  background: #52c41a;
  box-shadow: 0 0 6px 1px rgb(82 196 26 / 50%);
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

.route-banner {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 12px;
  flex-wrap: wrap;
  background: var(--accent-soft);
  color: var(--accent);
  text-align: center;
  padding: 7px 12px;
  font-size: 13px;
  flex-shrink: 0;
}

.route-banner-actions {
  display: inline-flex;
  gap: 8px;
}

.faint-banner {
  background: var(--bg-soft);
  color: var(--text-dim);
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
