<script setup lang="ts">
import { computed } from 'vue'
import { renderSharedMarkdown } from '@/utils/markdown'
import { parseRollResultDetails } from '@/lib/roll'
import RollResultDetails from '@/components/RollResultDetails.vue'
import type { SharedMessage } from '@/types'

const props = defineProps<{ message: SharedMessage; selected?: boolean; endpoint?: boolean }>()
const html = computed(() => renderSharedMarkdown(props.message.markdown))
const rollDetails = computed(() =>
  props.message.type === 'roll_result' ? parseRollResultDetails(props.message.metadata) : null,
)
const roleLabel = computed(() => {
  const labels: Partial<Record<SharedMessage['type'], string>> = {
    user: '旅人',
    assistant: '叙事',
    system: '系统',
    state: '状态',
    event: '事件',
    roll_result: '天命',
  }
  return labels[props.message.type] || props.message.type
})
</script>

<template>
  <article class="shared-message" :class="[message.type, { selected, endpoint }]">
    <div class="shared-message-meta">
      <span>{{ roleLabel }}</span>
      <time>{{ new Date(message.created_at).toLocaleString() }}</time>
    </div>
    <div class="markdown shared-message-content" v-html="html"></div>
    <RollResultDetails v-if="rollDetails" :details="rollDetails" />
  </article>
</template>

<style scoped>
.shared-message {
  position: relative;
  padding: 12px 16px;
  border-left: 2px solid var(--border-soft);
}
.shared-message.user {
  border: 1px solid var(--border-soft);
  border-radius: 6px;
  background: var(--user-bubble);
}
.shared-message.system,
.shared-message.state,
.shared-message.event,
.shared-message.roll_result {
  color: var(--text-dim);
  text-align: center;
}
.shared-message.selected {
  background: color-mix(in srgb, var(--accent-soft) 55%, transparent);
}
.shared-message.endpoint {
  box-shadow: inset 3px 0 var(--accent);
}
.shared-message-meta {
  display: flex;
  min-height: 22px;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 5px;
  color: var(--text-faint);
  font-size: 11px;
}
.shared-message-meta span {
  color: var(--accent);
  font-weight: 600;
}
.shared-message-meta time {
  overflow-wrap: anywhere;
}
.shared-message-content {
  line-height: 1.85;
}
</style>
