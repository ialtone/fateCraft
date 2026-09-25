<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { NAlert, NButton, NIcon, NInput, useDialog, useMessage } from 'naive-ui'
import {
  ArrowUpOutline,
  FlashOutline,
  PlayForwardOutline,
  RefreshOutline,
  StopOutline,
  TimeOutline,
  TrashOutline,
} from '@vicons/ionicons5'
import { useChatStore } from '@/stores/chat'
import type { ChatMessage } from '@/stores/chat'
import { useSocketStore } from '@/stores/socket'
import { useContextStore } from '@/stores/context'
import { messagesApi } from '@/api'
import GuidanceRenderer from '@/components/GuidanceRenderer.vue'
import { useRuntimeStore } from '@/stores/runtime'
import { useStateStore } from '@/stores/state'
import { useCampaignStore } from '@/stores/campaign'
import { useRewardStore } from '@/stores/rewards'
import type { GuidanceItem } from '@/types'
import { PLATFORM_CAPABILITY_MESSAGE_REWRITE } from '@/lib/platformCapabilities'

const props = defineProps<{ convId: string; disabled: boolean }>()
const chat = useChatStore()
const socket = useSocketStore()
const context = useContextStore()
const runtime = useRuntimeStore()
const state = useStateStore()
const campaign = useCampaignStore()
const rewards = useRewardStore()
const message = useMessage()
const dialog = useDialog()
const text = ref('')

// 断线 REST 降级期间的"等待中"标记：WS 断开时后端 202 立即返回、
// 生成进度只经 WS 广播（本端收不到），isGenerating 不会置真。
// 不拦截的话用户可对同一对话连点发送，触发后端生成锁竞争、多余请求全部失败。
// 重连后交还给 WS 事件 / resync 驱动，故 connected 恢复即清除。
const awaitingRestGen = ref(false)

// 继续 / 重新生成的操作锁：保持到终态事件或快照确认，避免 REST 202
// 与 agent.turn.started 之间、以及慢连接期间的重复提交。
const actionPending = ref(false)
const guidanceGroups = computed(() => runtime.presentation?.guidance?.groups ?? [])
const campaignSnapshot = computed(() => campaign.snapshot)
// 剧情改写是平台能力，不属于当前玩法 Runtime；玩法只能声明自己的动作。
const rewriteCapability = computed(() =>
  runtime.platformCapability(PLATFORM_CAPABILITY_MESSAGE_REWRITE),
)

watch(
  () => socket.connected,
  (connected) => {
    if (connected) awaitingRestGen.value = false
  },
)

// 切换对话时复位，避免上一个对话的等待标记锁死新对话的输入框
watch(
  () => props.convId,
  () => {
    awaitingRestGen.value = false
  },
)

const isEmpty = computed(() => {
  // 新对话：没有用户消息且没有正在生成
  // 允许有 first_message（角色开场白），只要用户还没发言就显示开始按钮
  const hasUserMessage = chat.messages.some((m: ChatMessage) => m.type === 'user')
  return !hasUserMessage && !chat.isGenerating
})

const canContinue = computed(() => {
  return (
    context.activity.can_continue &&
    !chat.isGenerating &&
    !awaitingRestGen.value &&
    !actionPending.value &&
    !props.disabled
  )
})

const canRegenerate = computed(() => {
  const capability = rewriteCapability.value
  if (!capability?.enabled) return false
  const cost = context.activity.regenerate_cost_quantity ?? capability.cost_quantity ?? 0
  const affordable =
    !rewards.loaded || cost <= 0 || rewards.balanceFor(capability.cost_reward_key) >= cost
  return (
    context.activity.can_regenerate &&
    affordable &&
    !chat.isGenerating &&
    !awaitingRestGen.value &&
    !actionPending.value &&
    !props.disabled
  )
})

// REST 202 only acknowledges durable-run acceptance. Keep the action lock
// until the stream reaches a terminal state (or an error path stops it), so a
// slow WebSocket cannot turn the acknowledgement window into a double submit.
watch(
  () => chat.isGenerating,
  (generating) => {
    if (!generating) actionPending.value = false
  },
)

const canQueue = computed(() => {
  return chat.isGenerating && !props.disabled && socket.connected && !!text.value.trim()
})

const generationLabel = computed(() => {
  if (context.activity.narrative === 'tool') {
    const labels: Record<string, string> = {
      state: '正在更新状态',
      roll: '正在执行判定',
      settlement: '正在结算玩法',
      action: '正在执行行动',
      guidance: '正在整理行动提示',
    }
    return labels[context.activity.tool_category || 'action'] ?? '正在执行生成工具'
  }
  if (context.activity.narrative === 'preparing') {
    return '正在准备上下文'
  }
  return '叙事生成中'
})

// 发送用户输入：优先走 WebSocket，未连接时回退到 REST。
// REST 失败时提示错误并回滚乐观插入的消息。
async function dispatchUserInput(content: string) {
  // 用户继续对话即视为已读上一轮错误，立即清掉提示横幅
  socket.clearError()
  const tempId = chat.addOptimisticUser(content)
  chat.markGenerating()
  const requestId = crypto.randomUUID()
  if (socket.sendUserInput(content, requestId)) return
  // 走 REST 降级：标记"等待中"，避免断线期间对同一对话连点造成锁竞争
  awaitingRestGen.value = true
  try {
    await messagesApi.send(props.convId, content, requestId)
  } catch (err: any) {
    chat.removeById(tempId)
    chat.stopGenerating()
    awaitingRestGen.value = false
    message.error(`发送失败：${err.message || '未知错误'}`)
  }
}

function dispatchGuidanceMessage(content: string) {
  if (
    !content.trim() ||
    props.disabled ||
    chat.isGenerating ||
    awaitingRestGen.value ||
    actionPending.value
  )
    return
  dispatchUserInput(content)
}

function dispatchGuidanceAction(item: GuidanceItem, args: Record<string, unknown>) {
  if (
    item.mode !== 'action' ||
    !item.action_key ||
    props.disabled ||
    chat.isGenerating ||
    actionPending.value
  )
    return
  campaign.execute(item.action_key, args).catch(() => {})
}

function send() {
  const content = text.value.trim()
  if (
    !content ||
    props.disabled ||
    chat.isGenerating ||
    awaitingRestGen.value ||
    actionPending.value
  )
    return
  dispatchUserInput(content)
  text.value = ''
}

function startJourney() {
  dispatchUserInput('开始。')
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    send()
  }
}

async function submitRegenerate() {
  socket.clearError()
  actionPending.value = true
  chat.markGenerating()
  const requestId = crypto.randomUUID()
  try {
    await messagesApi.regenerate(props.convId, requestId)
  } catch (err: any) {
    chat.stopGenerating()
    message.error(`重新生成失败：${err.message || '未知错误'}`)
  }
}

async function regenerate() {
  if (!canRegenerate.value) return
  const capability = rewriteCapability.value
  const cost = context.activity.regenerate_cost_quantity ?? capability?.cost_quantity ?? 0

  // 免费恢复失败/取消的回合，不需要确认，也不读取额度余额。
  if (cost <= 0) {
    await submitRegenerate()
    return
  }

  if (!rewards.loaded) {
    try {
      await rewards.load()
    } catch (err: any) {
      message.error(`读取剧情改写额度失败：${err.message || '未知错误'}`)
      return
    }
  }
  const balance = rewards.balanceFor(capability?.cost_reward_key)
  if (balance < cost) {
    dialog.warning({
      title: '额度不足',
      content: `本次重新生成需要消耗 ${cost} 次剧情改写额度，当前剩余 ${balance} 次。`,
      positiveText: '关闭',
    })
    return
  }

  dialog.warning({
    title: '重新生成',
    content: `重新生成当前叙事。本次消耗 ${cost} 次剧情改写额度，当前剩余 ${balance} 次；确认后将剩余 ${balance - cost} 次。是否继续？`,
    positiveText: '确认重新生成',
    negativeText: '取消',
    onPositiveClick: () => submitRegenerate(),
  })
}
async function continueGen() {
  if (!canContinue.value) return
  socket.clearError()
  actionPending.value = true
  chat.markGenerating()
  const requestId = crypto.randomUUID()
  try {
    await messagesApi.continue(props.convId, requestId)
  } catch (err: any) {
    chat.stopGenerating()
    message.error(`继续生成失败：${err.message || '未知错误'}`)
  }
}

function steer() {
  const content = text.value.trim()
  if (!canQueue.value || !socket.steer(content, crypto.randomUUID())) return
  text.value = ''
}

function followUp() {
  const content = text.value.trim()
  if (!canQueue.value || !socket.followUp(content, crypto.randomUUID())) return
  text.value = ''
}

function cancel() {
  socket.cancel()
}
</script>

<template>
  <div class="input-area">
    <!-- 断线/重连提示横幅 -->
    <NAlert
      v-if="!disabled && !socket.connected"
      :type="socket.reconnecting ? 'warning' : 'error'"
      style="margin-bottom: 10px"
    >
      {{
        socket.reconnecting ? '连接已断开，正在尝试重连…' : '连接已断开，消息将通过普通请求发送。'
      }}
    </NAlert>

    <NAlert
      v-if="socket.lastError"
      type="error"
      closable
      style="margin-bottom: 10px"
      @close="socket.clearError()"
    >
      {{ socket.lastError }}
    </NAlert>

    <!-- 新对话：显示开始按钮 -->
    <div v-if="isEmpty && !chat.isGenerating" class="start-container">
      <div class="start-hint">
        <div class="hint-icon">◆</div>
        <div class="hint-text">夜色渐深，风雨欲来。前方是一扇半掩的木门...</div>
      </div>
      <NButton type="primary" size="large" @click="startJourney">
        <template #icon><NIcon :component="ArrowUpOutline" /></template>
        推开木门，开始旅程
      </NButton>
    </div>

    <!-- 已有消息：显示正常输入框 -->
    <template v-else>
      <GuidanceRenderer
        :groups="guidanceGroups"
        :state="state.state"
        :snapshot="campaignSnapshot"
        :placement="['composer', 'after_message']"
        :disabled="disabled || chat.isGenerating || awaitingRestGen || actionPending"
        :loading="campaign.acting"
        @message="dispatchGuidanceMessage"
        @action="dispatchGuidanceAction"
      />
      <div class="toolbar row">
        <NButton quaternary size="tiny" :disabled="!canRegenerate" @click="regenerate">
          <template #icon><NIcon :component="RefreshOutline" /></template>
          重新生成
        </NButton>
        <NButton v-if="canContinue" quaternary size="tiny" @click="continueGen">
          <template #icon><NIcon :component="PlayForwardOutline" /></template>
          继续
        </NButton>
        <template v-if="chat.isGenerating">
          <NButton quaternary size="tiny" :disabled="!canQueue" @click="steer">
            <template #icon><NIcon :component="FlashOutline" /></template>
            插入
          </NButton>
          <NButton quaternary size="tiny" :disabled="!canQueue" @click="followUp">
            <template #icon><NIcon :component="TimeOutline" /></template>
            排队
          </NButton>
          <span v-if="socket.queuedCount" class="queue-count">
            已排队 {{ socket.queuedCount }}
          </span>
          <NButton
            v-if="socket.queuedCount"
            quaternary
            circle
            size="tiny"
            title="清空排队"
            aria-label="清空排队"
            @click="socket.clearQueue"
          >
            <template #icon><NIcon :component="TrashOutline" /></template>
          </NButton>
        </template>
        <NButton v-if="chat.isGenerating" quaternary size="tiny" type="error" @click="cancel">
          <template #icon><NIcon :component="StopOutline" /></template>
          停止
        </NButton>
        <span v-if="chat.isGenerating" class="generating faint">
          {{ generationLabel }}<span class="dots"></span>
        </span>
        <span v-else-if="awaitingRestGen" class="generating faint">
          已提交，等待重连接收结果<span class="dots"></span>
        </span>
        <span
          class="conn"
          :class="socket.connected ? 'on' : 'off'"
          :title="socket.connected ? '已连接' : '未连接'"
        ></span>
      </div>

      <div class="box">
        <NInput
          v-model:value="text"
          type="textarea"
          :disabled="disabled"
          :placeholder="
            disabled
              ? '此对话已结束，可返回列表或新建对话。'
              : chat.isGenerating
                ? '输入要插入或排队的指令…'
                : '输入你的行动…（Enter 发送，Shift+Enter 换行）'
          "
          :autosize="{ minRows: 2, maxRows: 8 }"
          @keydown="onKeydown"
        />
        <NButton
          type="primary"
          class="send"
          :disabled="
            disabled || chat.isGenerating || awaitingRestGen || actionPending || !text.trim()
          "
          @click="send"
        >
          <template #icon><NIcon :component="ArrowUpOutline" /></template>
          <span class="send-label">落笔</span>
        </NButton>
      </div>
    </template>
  </div>
</template>

<style scoped>
.input-area {
  border-top: 1px solid var(--border-soft);
  background: var(--bg-soft);
  padding: 10px 20px 16px;
  flex-shrink: 0;
}

.start-container {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 20px;
  padding: 30px 20px;
}

.start-hint {
  text-align: center;
  color: var(--text-faint);
  max-width: 400px;
}

.hint-icon {
  font-size: 24px;
  color: var(--accent);
  margin-bottom: 10px;
  animation: pulse 2s ease-in-out infinite;
}

@keyframes pulse {
  0%,
  100% {
    opacity: 0.6;
  }
  50% {
    opacity: 1;
  }
}

.hint-text {
  font-size: 14px;
  line-height: 1.8;
  letter-spacing: 1px;
}

.toolbar {
  margin-bottom: 8px;
  gap: 4px;
}

.generating {
  font-size: 12px;
  letter-spacing: 1px;
}

.queue-count {
  color: var(--text-faint);
  font-size: 12px;
  margin-left: 4px;
  white-space: nowrap;
}

.dots::after {
  content: '…';
  animation: dots 1.4s steps(4) infinite;
  display: inline-block;
  width: 1.2em;
  overflow: hidden;
  vertical-align: bottom;
}

@keyframes dots {
  0% {
    width: 0;
  }
  100% {
    width: 1.2em;
  }
}

.box {
  display: flex;
  gap: 10px;
  align-items: flex-end;
}

.send {
  height: 40px;
  padding: 0 22px;
  letter-spacing: 3px;
  flex-shrink: 0;
}

.conn {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  margin-left: auto;
  flex-shrink: 0;
}

.conn.on {
  background: var(--ok);
  box-shadow: 0 0 6px var(--ok);
}

.conn.off {
  background: var(--text-faint);
}

/* 移动端适配 */
@media (max-width: 768px) {
  .start-hint {
    max-width: 100%;
    padding: 0 12px;
  }

  .hint-icon {
    font-size: 20px;
  }

  .hint-text {
    font-size: 13px;
  }

  .toolbar {
    gap: 6px;
    flex-wrap: wrap;
  }

  .box {
    gap: 8px;
  }

  .input-area {
    padding: 8px max(12px, env(safe-area-inset-right)) calc(10px + env(safe-area-inset-bottom))
      max(12px, env(safe-area-inset-left));
  }

  .box :deep(.n-input) {
    min-width: 0;
  }

  .send {
    width: 44px;
    padding: 0;
    letter-spacing: 0;
  }

  .send-label {
    display: none;
  }
}
</style>
