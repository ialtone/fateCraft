<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NButton, NIcon, useDialog } from 'naive-ui'
import { ChevronBackOutline } from '@vicons/ionicons5'
import StatePanel from '@/components/StatePanel.vue'
import MessageTimeline from '@/components/MessageTimeline.vue'
import ContextInspector from '@/components/ContextInspector.vue'
import ActionInput from '@/components/ActionInput.vue'
import ConversationStatusBadge from '@/components/ConversationStatusBadge.vue'
import ThemeToggle from '@/components/ThemeToggle.vue'
import { useConversationStore } from '@/stores/conversations'
import { useChatStore } from '@/stores/chat'
import { useStateStore } from '@/stores/state'
import { useContextStore } from '@/stores/context'
import { useSocketStore } from '@/stores/socket'
import { useSettingsStore } from '@/stores/settings'
import { useAccountStore } from '@/stores/account'

const route = useRoute()
const router = useRouter()
const dialog = useDialog()
const convStore = useConversationStore()
const chat = useChatStore()
const stateStore = useStateStore()
const context = useContextStore()
const socket = useSocketStore()
const settings = useSettingsStore()
const account = useAccountStore()

const showInspector = ref(true)
const convId = computed(() => route.params.id as string)
const editable = computed(() => convStore.current?.status === 'active')

// 监听结局触发（结局信息已从 socket store 挪到对话 store）
watch(
  () => convStore.endingTriggered,
  (ending) => {
    if (!ending) return

    if (ending.type === 'bad') {
      // 失败结局，显示天道回溯提示
      showDeathDialog(ending)
    } else if (ending.type === 'good') {
      // 成功结局，显示破碎虚空庆祝
      showSuccessDialog(ending)
    } else {
      // neutral 及未知类型：展示中性结局摘要
      showNeutralDialog(ending)
    }
  },
)

function showDeathDialog(ending: { type: string; title: string; summary: string }) {
  // 账号数据未加载完成时不用假数据判断，只展示结局摘要
  const rewindHint = account.loaded
    ? account.rewindCount > 0
      ? `天道尚有转机——你还剩余 ${account.rewindCount} 次天道回溯。\n\n点击你曾落笔之处的「编辑」按钮，可逆转时光，重书命数。`
      : '天道已定，时光不可逆转。你已无可用的天道回溯。'
    : '账号数据加载中，回溯次数请稍后在顶栏查看。'

  dialog.error({
    title: '◆ 道消身殒 ◆',
    content: `${ending.summary}\n\n${rewindHint}`,
    positiveText: '知晓',
    style: {
      maxWidth: '500px',
    },
    onPositiveClick: () => {
      convStore.clearEnding()
    },
    onMaskClick: () => {
      convStore.clearEnding()
    },
  })
}

function showNeutralDialog(ending: { type: string; title: string; summary: string }) {
  dialog.info({
    title: `◆ ${ending.title || '故事终章'} ◆`,
    content: ending.summary || '这一段故事，就此落幕。',
    positiveText: '知晓',
    style: {
      maxWidth: '500px',
    },
    onPositiveClick: () => {
      convStore.clearEnding()
    },
    onMaskClick: () => {
      convStore.clearEnding()
    },
  })
}

function showSuccessDialog(ending: {
  type: string
  title: string
  summary: string
  spiritStones?: number
  survivalTurns?: number
}) {
  const spiritStones = ending.spiritStones || 0
  const survivalTurns = ending.survivalTurns || 0
  const totalStones = account.totalSpiritStones + spiritStones
  const successCount = account.successfulTrials + 1

  dialog.success({
    title: '◆ 破碎虚空 ◆',
    content: `${ending.summary}\n\n━━━━━━━━━━━━━━━━━━━━━\n\n🌟 本次带出\n    ${spiritStones.toLocaleString()} 颗灵石\n\n📊 累计成就\n    • 历史总灵石：${totalStones.toLocaleString()} 颗\n    • 成功轮回：${successCount} 次\n    • 本次存活：${survivalTurns} 回合\n\n━━━━━━━━━━━━━━━━━━━━━\n\n功德圆满！是否开启新的轮回？`,
    positiveText: '再来一次',
    negativeText: '返回列表',
    style: {
      maxWidth: '520px',
    },
    onPositiveClick: () => {
      convStore.clearEnding()
      // 跳转到新建对话，并预选相同配置
      router.push('/?action=new')
    },
    onNegativeClick: () => {
      convStore.clearEnding()
      router.push('/')
    },
    onMaskClick: () => {
      convStore.clearEnding()
    },
  })
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
  chat.reset()
  stateStore.reset()
  context.reset()
  socket.disconnect()

  await convStore.load(id)
  if (stale(id)) return
  await Promise.all([chat.load(id), stateStore.load(id), context.loadLast(id)])
  if (stale(id)) return
  socket.connect(id)
}

onMounted(async () => {
  if (!settings.settings) await settings.load().catch(() => {})
  await account.load().catch(() => {}) // 加载账号数据
  await open(convId.value)
})

watch(convId, (id, prev) => {
  if (id && id !== prev) open(id)
})

onBeforeUnmount(() => {
  // 先置卸载标记，让进行中的 open() 在下一个 await 后自行中止，
  // 避免陈旧续体在列表页重新 connect 出无限重连定时器
  disposed = true
  socket.disconnect()
  convStore.clearCurrent()
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
</script>

<template>
  <div class="conv-page">
    <header class="bar row spread">
      <div class="row" style="gap: 10px; min-width: 0">
        <NButton quaternary size="small" @click="router.push('/')">
          <template #icon><NIcon :component="ChevronBackOutline" /></template>
          列表
        </NButton>
        <span class="title">{{ convStore.current?.title || '加载中…' }}</span>
        <ConversationStatusBadge v-if="convStore.current" :status="convStore.current.status" />
      </div>
      <div class="row" style="gap: 6px">
        <span class="faint sm mono">{{ settings.settings?.model }}</span>
        <NButton quaternary size="small" @click="showInspector = !showInspector">
          {{ showInspector ? '隐藏检查器' : '显示检查器' }}
        </NButton>
        <NButton v-if="editable" quaternary size="small" type="error" @click="endConversation"
          >结束对话</NButton
        >
        <ThemeToggle />
      </div>
    </header>

    <div v-if="!editable && convStore.current" class="ended-banner">
      ◆ 此话本已合卷 —— 可返回列表或另启新篇 ◆
    </div>

    <!-- 错误提示统一由输入区（ActionInput）的可关闭警示条展示，页头不再重复挂一份 -->

    <div class="three-pane" :class="{ 'no-inspector': !showInspector }">
      <StatePanel :conv-id="convId" />
      <div class="center">
        <MessageTimeline :editable="editable" />
        <ActionInput :conv-id="convId" :disabled="!editable" />
      </div>
      <ContextInspector v-if="showInspector" />
    </div>
  </div>
</template>

<style scoped>
.conv-page {
  height: 100%;
  display: flex;
  flex-direction: column;
}

.bar {
  height: 54px;
  padding: 0 14px;
  background: color-mix(in srgb, var(--bg-soft) 88%, transparent);
  border-bottom: 1px solid var(--border-soft);
  flex-shrink: 0;
}

.title {
  font-weight: 600;
  letter-spacing: 1px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.sm {
  font-size: 12px;
}

.mono {
  font-family: Consolas, Menlo, monospace;
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
  grid-template-columns: 290px 1fr 330px;
  min-height: 0;
}

.three-pane.no-inspector {
  grid-template-columns: 290px 1fr;
}

.center {
  display: flex;
  flex-direction: column;
  min-height: 0;
}

/* 移动端适配 */
@media (max-width: 768px) {
  .bar {
    padding: 8px 12px;
    flex-wrap: wrap;
    gap: 8px;
  }

  .title {
    font-size: 14px;
    max-width: 150px;
  }

  .three-pane,
  .three-pane.no-inspector {
    grid-template-columns: 1fr; /* 单栏布局 */
  }

  /* 移动端隐藏侧边栏 */
  .three-pane > :first-child,
  .three-pane > :last-child {
    display: none;
  }
}
</style>
