<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { NButton, useDialog, useMessage } from 'naive-ui'
import { useRoute } from 'vue-router'
import MessageItem from './MessageItem.vue'
import { useChatStore } from '@/stores/chat'
import { useRewardStore } from '@/stores/rewards'
import { useRuntimeStore } from '@/stores/runtime'
import { useSocketStore } from '@/stores/socket'
import { useGameplayVersionStore } from '@/stores/gameplayVersion'
import { messagesApi } from '@/api'
import { nextPinned } from '@/lib/scrollPin'
import { PLATFORM_CAPABILITY_MESSAGE_REWRITE } from '@/lib/platformCapabilities'
import { groupTimeline, type TimelineGroup } from '@/lib/receipt-groups'

const props = defineProps<{ editable: boolean }>()
const chat = useChatStore()
const rewards = useRewardStore()
const runtime = useRuntimeStore()
const socket = useSocketStore()
const gameplay = useGameplayVersionStore()
const checkingRewrite = ref(false)
const dialog = useDialog()
const message = useMessage()
const route = useRoute()
const scroller = ref<HTMLElement | null>(null)
// 剧情改写是平台能力，不从玩法 Runtime 的动作目录读取。
const rewrite = computed(() => runtime.platformCapability(PLATFORM_CAPABILITY_MESSAGE_REWRITE))
const rewriteEnabled = computed(() => props.editable && rewrite.value?.enabled === true)

// 面板动作的系统回执折叠：连续的“已执行操作：X”展示消息合并为一条可展开
// 的“面板操作”条目，避免长局里几十条回执冲刷时间线；详细事实在动作后的
// 叙事回合正文中呈现。分组逻辑见 lib/receipt-groups.ts。
const timelineGroups = computed<TimelineGroup[]>(() => groupTimeline(chat.messages))

const expandedReceipts = ref<Set<string>>(new Set())

function toggleReceipts(key: string) {
  const next = new Set(expandedReceipts.value)
  if (next.has(key)) next.delete(key)
  else next.add(key)
  expandedReceipts.value = next
}

// 贴底跟随：只由用户输入（滚轮、键盘翻页、scrollTop 变小）改变"是否贴底"，内容高度变化（流式增量、节流后的
// Markdown 渲染、思考面板动画、图片加载）一律由 ResizeObserver 跟随，
// 不依赖数据事件与渲染时机的先后。
// 不额外设置 overflow-anchor（保持浏览器默认）：底部哨兵锚点会在用户上翻后
// 仍把视口随内容增长往下推，与"解除贴底"冲突；跟随完全交给 ResizeObserver。
const PAGING_KEYS = new Set(['PageUp', 'ArrowUp', 'Home'])
const content = ref<HTMLElement | null>(null)
const pinned = ref(true)
let lastTop = 0
let resizeObserver: ResizeObserver | null = null

function scrollToBottom() {
  const el = scroller.value
  if (el) el.scrollTop = el.scrollHeight
}

// 只在确实还能往上滚时解除，避免内容未溢出时的上滚手势让后续流式内容失去跟随。
function unpin() {
  if ((scroller.value?.scrollTop ?? 0) > 0) pinned.value = false
}

function onKeydown(e: KeyboardEvent) {
  if (e.target === e.currentTarget && PAGING_KEYS.has(e.key)) unpin()
}

function onWheel(e: WheelEvent) {
  if (e.deltaY < 0) unpin()
}

function onScroll() {
  const el = scroller.value
  if (!el) return
  const top = el.scrollTop
  pinned.value = nextPinned(pinned.value, lastTop, top, el.scrollHeight - top - el.clientHeight)
  lastTop = top
}

onMounted(() => {
  resizeObserver = new ResizeObserver(() => {
    if (pinned.value) scrollToBottom()
  })
  if (content.value) resizeObserver.observe(content.value)
  scrollToBottom()
})

onBeforeUnmount(() => resizeObserver?.disconnect())

// 切换会话时组件复用，新会话从底部开始跟随。
watch(
  () => route.params.id,
  () => {
    pinned.value = true
    lastTop = 0
  },
)

// 顶部"加载更早"：前插一页历史后把视口锚定在原来的第一条消息上，
// 否则浏览器保持 scrollTop 不变、内容整体下移，用户会失去阅读位置。
async function loadEarlier() {
  const el = scroller.value
  const prevHeight = el?.scrollHeight ?? 0
  const prevTop = el?.scrollTop ?? 0
  try {
    await chat.loadEarlier(route.params.id as string)
  } catch (err: any) {
    message.error(`加载更早的消息失败：${err.message || '未知错误'}`)
    return
  }
  // 用户在看历史，前插内容不能被 ResizeObserver 拉回底部。
  pinned.value = false
  await nextTick()
  if (el) el.scrollTop = lastTop = el.scrollHeight - prevHeight + prevTop
}

async function onEdit(id: string, content: string) {
  if (!rewriteEnabled.value || socket.writeBlocked || chat.isGenerating) return
  const convId = route.params.id as string
  const capability = rewrite.value!
  const cost = capability.cost_quantity ?? 0
  const balance = rewards.balanceFor(capability.cost_reward_key)
  if (cost > 0 && rewards.loaded && balance < cost) {
    dialog.warning({
      title: '额度不足',
      content: `当前无法使用「${capability.label}」。`,
      positiveText: '关闭',
    })
    return
  }

  dialog.warning({
    title: capability.label,
    content: `${capability.description || '编辑该消息并从此处重新生成。'}${cost > 0 ? `\n\n本次消耗 ${cost}，当前余额 ${balance}。` : ''}`,
    positiveText: '确认改写',
    negativeText: '取消',
    onPositiveClick: async () => {
      if (
        checkingRewrite.value ||
        socket.writeBlocked ||
        chat.isGenerating ||
        !rewriteEnabled.value ||
        route.params.id !== convId
      )
        return false
      checkingRewrite.value = true
      try {
        const info = await gameplay.ensureFresh(convId)
        if (!info.run_allowed) {
          message.warning(info.reason || '当前玩法版本已暂停运行。')
          return false
        }
      } catch {
        message.error('玩法版本状态暂时无法读取，请刷新后重试。')
        return false
      } finally {
        checkingRewrite.value = false
      }
      if (
        socket.writeBlocked ||
        chat.isGenerating ||
        !rewriteEnabled.value ||
        route.params.id !== convId
      )
        return false
      const edited = chat.messages.find((m) => m.id === id)
      if (edited && edited.type === 'user') {
        chat.markGenerating()
        const requestId = crypto.randomUUID()
        try {
          await messagesApi.editAndRegenerate(id, content, requestId)
          if (!socket.writeBlocked && route.params.id === convId)
            await rewards.load().catch(() => {})
        } catch (err: any) {
          if (route.params.id !== convId) return false
          chat.stopGenerating()
          const status = err.response?.status
          if (status === 409 || status === 400) {
            message.error(err.message || '改写额度不足')
          } else {
            message.error(`编辑消息失败：${err.message || '未知错误'}`)
          }
          if (!socket.writeBlocked) await rewards.load().catch(() => {})
        }
      }
    },
  })
}
</script>

<template>
  <div
    class="timeline scroll"
    ref="scroller"
    tabindex="-1"
    @scroll="onScroll"
    @wheel.passive="onWheel"
    @keydown="onKeydown"
  >
    <div ref="content" class="timeline-content">
      <div v-if="chat.hasEarlier && chat.messages.length" class="load-earlier">
        <NButton quaternary size="tiny" :loading="chat.loadingEarlier" @click="loadEarlier">
          加载更早的消息
        </NButton>
      </div>
      <div v-if="!chat.messages.length" class="empty">
        <div class="glyph">🏮</div>
        <p class="faint">茶尚温，故事未启。<br />落笔之处，即是开端。</p>
      </div>
      <template v-for="group in timelineGroups" :key="group.key">
        <div v-if="group.receipts" class="receipt-group">
          <button class="receipt-toggle" type="button" @click="toggleReceipts(group.key)">
            <span class="receipt-glyph">◈</span>
            面板操作 × {{ group.receipts.length }}
            <span class="receipt-arrow">{{ expandedReceipts.has(group.key) ? '▾' : '▸' }}</span>
          </button>
          <template v-if="expandedReceipts.has(group.key)">
            <MessageItem v-for="m in group.receipts" :key="m.id" :message="m" :editable="false" />
          </template>
        </div>
        <MessageItem
          v-else-if="group.message"
          :key="group.message.id"
          :message="group.message"
          :editable="rewriteEnabled"
          @edit="onEdit"
        />
      </template>
    </div>
  </div>
</template>

<style scoped>
.timeline {
  flex: 1;
  min-height: 0;
  min-width: 0;
  padding: 24px 28px;
  overflow-y: auto;
  outline: none;
}

.load-earlier {
  text-align: center;
  margin-bottom: 10px;
}

.receipt-group {
  margin-bottom: 10px;
}

.receipt-toggle {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 12px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--bg-soft);
  color: var(--text-muted);
  font-size: 12px;
  cursor: pointer;
}

.receipt-toggle:hover {
  color: var(--text);
}

.receipt-glyph,
.receipt-arrow {
  font-size: 11px;
}

.empty {
  text-align: center;
  margin-top: 12vh;
}

.glyph {
  font-size: 36px;
  margin-bottom: 12px;
  filter: drop-shadow(0 0 12px var(--accent-glow));
}

.empty p {
  line-height: 2;
  letter-spacing: 1px;
}

@media (max-width: 768px) {
  .timeline {
    padding: 16px 14px;
    overscroll-behavior: contain;
  }

  .empty {
    margin-top: 8vh;
  }
}
</style>
