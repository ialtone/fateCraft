<script setup lang="ts">
import { computed, ref } from 'vue'
import { useMessage } from 'naive-ui'
import GuidanceRenderer from './GuidanceRenderer.vue'
import { useGuidanceStore } from '@/stores/guidance'
import { useChatStore } from '@/stores/chat'
import { useSocketStore } from '@/stores/socket'
import { guidanceApi } from '@/api'
import type { GuidanceGroup, GuidanceItem } from '@/types'

const props = defineProps<{ convId: string; disabled: boolean }>()
const guidance = useGuidanceStore()
const chat = useChatStore()
const socket = useSocketStore()
const message = useMessage()
const pending = ref(false)
let retry: { selection: string; requestId: string } | null = null

const groups = computed<GuidanceGroup[]>(() =>
  guidance.active
    ? [
        {
          key: guidance.active.id,
          title: guidance.active.title,
          items: guidance.active.items.map((item) => ({ ...item, mode: 'message' })),
        },
      ]
    : [],
)

async function select(item: GuidanceItem) {
  const active = guidance.active
  if (!active || props.disabled || socket.writeBlocked || chat.isGenerating || pending.value) return
  const convId = props.convId
  const selection = `${convId}:${active.id}:${item.key}`
  if (retry?.selection !== selection) retry = { selection, requestId: crypto.randomUUID() }
  pending.value = true
  socket.clearError()
  chat.markGenerating()
  try {
    await guidanceApi.select(convId, active.id, item.key, retry.requestId)
    // Acceptance is not completion. Server events own the generation lock.
  } catch (err: any) {
    if (props.convId !== convId) return
    chat.stopGenerating()
    message.error(err.message || '提交行动提示失败')
    if (!socket.writeBlocked) await guidance.load(convId).catch(() => {})
  } finally {
    pending.value = false
  }
}
</script>

<template>
  <section
    v-if="!disabled && guidance.active && !chat.isGenerating"
    class="conversation-guidance-bar"
    aria-label="当前行动选项"
  >
    <GuidanceRenderer :groups="groups" :disabled="pending" :loading="pending" @select="select" />
  </section>
</template>

<style scoped>
.conversation-guidance-bar {
  flex: 0 0 auto;
  min-width: 0;
  max-height: 180px;
  overflow-y: auto;
  padding: 10px 28px;
  border-top: 1px solid var(--border-soft);
  background: var(--bg-soft);
}
.conversation-guidance-bar :deep(.guidance) {
  margin-bottom: 0;
}
@media (max-width: 768px) {
  .conversation-guidance-bar {
    padding: 8px 14px;
    max-height: 144px;
  }
}
</style>
