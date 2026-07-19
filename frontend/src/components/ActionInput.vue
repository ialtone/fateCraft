<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { NAlert, NButton, NIcon, NInput, useMessage } from 'naive-ui'
import { ArrowUpOutline, PlayForwardOutline, RefreshOutline, StopOutline } from '@vicons/ionicons5'
import { useChatStore } from '@/stores/chat'
import type { ChatMessage } from '@/stores/chat'
import { useSocketStore } from '@/stores/socket'
import { messagesApi } from '@/api'

const props = defineProps<{ convId: string; disabled: boolean }>()
const chat = useChatStore()
const socket = useSocketStore()
const message = useMessage()
const text = ref('')

// 断线 REST 降级期间的"等待中"标记：WS 断开时后端 202 立即返回、
// 生成进度只经 WS 广播（本端收不到），isGenerating 不会置真。
// 不拦截的话用户可对同一对话连点发送，触发后端生成锁竞争、多余请求全部失败。
// 重连后交还给 WS 事件 / resync 驱动，故 connected 恢复即清除。
const awaitingRestGen = ref(false)

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
  // 只有最后一条消息是 assistant 类型时才能"继续"；REST 降级等待期间同样禁用
  const lastMsg = chat.messages[chat.messages.length - 1]
  return (
    lastMsg &&
    lastMsg.type === 'assistant' &&
    !chat.isGenerating &&
    !awaitingRestGen.value &&
    !props.disabled
  )
})

// 发送用户输入：优先走 WebSocket，未连接时回退到 REST。
// REST 失败时提示错误并回滚乐观插入的消息。
async function dispatchUserInput(content: string) {
  const tempId = chat.addOptimisticUser(content)
  if (socket.sendUserInput(content)) return
  // 走 REST 降级：标记"等待中"，避免断线期间对同一对话连点造成锁竞争
  awaitingRestGen.value = true
  try {
    await messagesApi.send(props.convId, content)
  } catch (err: any) {
    chat.removeById(tempId)
    awaitingRestGen.value = false
    message.error(`发送失败：${err.message || '未知错误'}`)
  }
}

function send() {
  const content = text.value.trim()
  if (!content || props.disabled || chat.isGenerating || awaitingRestGen.value) return
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

async function regenerate() {
  try {
    // 先调用后端 API，等待确认删除成功
    await messagesApi.regenerate(props.convId)

    // 后端成功后，前端同步移除最后一条 assistant 消息（收编进 chat store action）
    chat.removeLastAssistant()
  } catch (err: any) {
    // 失败时显示错误提示，不删除消息
    message.error(`重新生成失败：${err.message || '未知错误'}`)
  }
}
async function continueGen() {
  try {
    await messagesApi.continue(props.convId)
  } catch (err: any) {
    message.error(`继续生成失败：${err.message || '未知错误'}`)
  }
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
      <div class="toolbar row">
        <NButton
          quaternary
          size="tiny"
          :disabled="disabled || chat.isGenerating || awaitingRestGen"
          @click="regenerate"
        >
          <template #icon><NIcon :component="RefreshOutline" /></template>
          重新生成
        </NButton>
        <NButton v-if="canContinue" quaternary size="tiny" @click="continueGen">
          <template #icon><NIcon :component="PlayForwardOutline" /></template>
          继续
        </NButton>
        <NButton v-if="chat.isGenerating" quaternary size="tiny" type="error" @click="cancel">
          <template #icon><NIcon :component="StopOutline" /></template>
          停止
        </NButton>
        <span v-if="chat.isGenerating" class="generating faint"
          >叙事织就中<span class="dots"></span
        ></span>
        <span v-else-if="awaitingRestGen" class="generating faint"
          >已提交，等待重连接收结果<span class="dots"></span
        ></span>
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
              : '输入你的行动…（Enter 发送，Shift+Enter 换行）'
          "
          :autosize="{ minRows: 2, maxRows: 8 }"
          @keydown="onKeydown"
        />
        <NButton
          type="primary"
          class="send"
          :disabled="disabled || chat.isGenerating || awaitingRestGen || !text.trim()"
          @click="send"
        >
          <template #icon><NIcon :component="ArrowUpOutline" /></template>
          落笔
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

  .box button {
    padding: 8px 12px;
  }
}
</style>
