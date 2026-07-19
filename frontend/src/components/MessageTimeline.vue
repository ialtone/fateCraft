<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { useDialog, useMessage } from 'naive-ui'
import { useRoute } from 'vue-router'
import MessageItem from './MessageItem.vue'
import { useChatStore } from '@/stores/chat'
import { useAccountStore } from '@/stores/account'
import { messagesApi } from '@/api'

const props = defineProps<{ editable: boolean }>()
const chat = useChatStore()
const account = useAccountStore()
const dialog = useDialog()
const message = useMessage()
const route = useRoute()
const scroller = ref<HTMLElement | null>(null)

// 用户距底部小于该值时才自动滚底，避免打断上翻阅读。
const AUTO_SCROLL_THRESHOLD = 80

// 新消息或流式增量到达时，仅当用户已在底部附近才滚动到底部。
watch(
  () => [chat.messages.length, chat.messages[chat.messages.length - 1]?.markdown],
  async () => {
    const el = scroller.value
    if (!el) return
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < AUTO_SCROLL_THRESHOLD
    await nextTick()
    if (nearBottom) el.scrollTop = el.scrollHeight
  },
)

async function onEdit(id: string, content: string) {
  // 检查是否有回溯次数（后端为准，这里仅做前置提示）
  if (account.loaded && account.rewindCount <= 0) {
    dialog.warning({
      title: '天道已定',
      content: '你已无可用的天道回溯。命运长河奔流不息，无法再逆转时光。',
      positiveText: '知晓',
    })
    return
  }

  // 二次确认
  dialog.warning({
    title: '天道回溯',
    content: `逆转时光，重书命数。此举将消耗一次天道回溯机会。\n\n"若能重来，你可会作出不同的选择？"\n\n当前剩余：${account.rewindCount} 次 → 使用后：${account.rewindCount - 1} 次`,
    positiveText: '改天换命',
    negativeText: '顺其自然',
    onPositiveClick: async () => {
      // 编辑用户消息后，删除后续的所有消息并重新生成。
      // 回溯次数由后端在 edit-and-regenerate 接口内原子扣减，
      // 次数不足时返回 409/400，前端不再先行扣减。
      const edited = chat.messages.find((m) => m.id === id)
      if (edited && edited.type === 'user') {
        // 乐观截断：删除该消息之后的所有消息（收编进 chat store action）
        chat.truncateAfter(id)

        try {
          await messagesApi.editAndRegenerate(id, content)
          // 更新编辑后的消息内容
          chat.updateMarkdown(id, content)
          // 后端已扣减次数，刷新账号数据
          await account.load().catch(() => {})
        } catch (err: any) {
          const status = err.response?.status
          if (status === 409 || status === 400) {
            message.error(err.message || '天道回溯次数不足')
          } else {
            message.error(`编辑消息失败：${err.message || '未知错误'}`)
          }
          // 失败后刷新账号与消息列表，恢复真实状态
          await account.load().catch(() => {})
          const convId = route.params.id as string
          await chat.load(convId)
        }
      }
    },
  })
}
</script>

<template>
  <div class="timeline scroll" ref="scroller">
    <div v-if="!chat.messages.length" class="empty">
      <div class="glyph">🏮</div>
      <p class="faint">茶尚温，故事未启。<br />落笔之处，即是开端。</p>
    </div>
    <MessageItem
      v-for="m in chat.messages"
      :key="m.id"
      :message="m"
      :editable="props.editable"
      @edit="onEdit"
    />
  </div>
</template>

<style scoped>
.timeline {
  flex: 1;
  padding: 24px 28px;
  overflow-y: auto;
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
</style>
