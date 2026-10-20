<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { NButton, useDialog, useMessage } from 'naive-ui'
import { useRoute } from 'vue-router'
import MessageItem from './MessageItem.vue'
import { CHAT_CONTENT_CHANGED_EVENT, useChatStore } from '@/stores/chat'
import { useRewardStore } from '@/stores/rewards'
import { useRuntimeStore } from '@/stores/runtime'
import { useSocketStore } from '@/stores/socket'
import { useGameplayVersionStore } from '@/stores/gameplayVersion'
import { messagesApi } from '@/api'
import { PLATFORM_CAPABILITY_MESSAGE_REWRITE } from '@/lib/platformCapabilities'

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

// 用户距底部小于该值时才自动滚底，避免打断上翻阅读。
const AUTO_SCROLL_THRESHOLD = 80
// 思考折叠面板的高度会在 DOM 更新后继续过渡一小段时间；在过渡完成
// 后再补一次滚动，确保最后一段 reasoning 不会把视口顶到上方。
const AUTO_SCROLL_SETTLE_DELAY = 400
const stickToBottom = ref(true)
let settleTimer: ReturnType<typeof setTimeout> | null = null
let settleFrame: number | null = null
let settleQueued = false

function isNearBottom(el: HTMLElement) {
  return el.scrollHeight - el.scrollTop - el.clientHeight < AUTO_SCROLL_THRESHOLD
}

function scrollToBottom() {
  const el = scroller.value
  if (el) el.scrollTop = el.scrollHeight
}

function scheduleSettledScroll() {
  if (!stickToBottom.value) return
  if (settleQueued) return
  settleQueued = true
  if (settleFrame !== null) cancelAnimationFrame(settleFrame)
  if (settleTimer !== null) clearTimeout(settleTimer)

  // 先跟随当前 DOM 帧，再跟随下一帧，覆盖普通文本更新和折叠组件的
  // 首次布局；最后的短延迟用于等待高度 transition 完成。
  void nextTick(() => {
    settleQueued = false
    if (!stickToBottom.value) return
    scrollToBottom()
    settleFrame = requestAnimationFrame(() => {
      settleFrame = null
      if (stickToBottom.value) scrollToBottom()
    })
    settleTimer = setTimeout(() => {
      settleTimer = null
      if (stickToBottom.value) scrollToBottom()
    }, AUTO_SCROLL_SETTLE_DELAY)
  })
}

function onScroll() {
  const el = scroller.value
  if (!el) return
  stickToBottom.value = isNearBottom(el)
}

// 新消息或流式增量到达时，仅当用户已在底部附近才滚动到底部。
// 通知由 Chat Store 在实际写入内容时发出，避免 watch 整个 messages 数组。
function onChatContentChanged() {
  const el = scroller.value
  if (!el) return
  // 在内容高度变化前记录用户是否贴近底部。后续 reasoning 折叠动画
  // 会改变 scrollHeight，不能等到 nextTick 后才判断这个条件。
  if (!isNearBottom(el)) {
    stickToBottom.value = false
    return
  }
  stickToBottom.value = true
  scheduleSettledScroll()
}

onMounted(() => window.addEventListener(CHAT_CONTENT_CHANGED_EVENT, onChatContentChanged))

onBeforeUnmount(() => {
  window.removeEventListener(CHAT_CONTENT_CHANGED_EVENT, onChatContentChanged)
  if (settleFrame !== null) cancelAnimationFrame(settleFrame)
  if (settleTimer !== null) clearTimeout(settleTimer)
})

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
  await nextTick()
  if (el) el.scrollTop = el.scrollHeight - prevHeight + prevTop
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
  <div class="timeline scroll" ref="scroller" @scroll="onScroll">
    <div v-if="chat.hasEarlier && chat.messages.length" class="load-earlier">
      <NButton quaternary size="tiny" :loading="chat.loadingEarlier" @click="loadEarlier">
        加载更早的消息
      </NButton>
    </div>
    <div v-if="!chat.messages.length" class="empty">
      <div class="glyph">🏮</div>
      <p class="faint">茶尚温，故事未启。<br />落笔之处，即是开端。</p>
    </div>
    <MessageItem
      v-for="m in chat.messages"
      :key="m.id"
      :message="m"
      :editable="rewriteEnabled"
      @edit="onEdit"
    />
  </div>
</template>

<style scoped>
.timeline {
  flex: 1;
  min-height: 0;
  min-width: 0;
  padding: 24px 28px;
  overflow-y: auto;
}

.load-earlier {
  text-align: center;
  margin-bottom: 10px;
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
