<script setup lang="ts">
import { computed } from 'vue'
import type { ConversationStatusEnum } from '@/types'

const props = defineProps<{ status: ConversationStatusEnum }>()

const label = computed(
  () =>
    ({
      active: '进行中',
      ended: '已结束',
      archived: '已归档',
      deleted: '已删除',
    })[props.status] || props.status,
)
</script>

<template>
  <span class="badge" :class="status">
    <span class="dot"></span>
    {{ label }}
  </span>
</template>

<style scoped>
.badge {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 11px;
  padding: 2px 9px;
  border-radius: 999px;
  border: 1px solid var(--border);
  color: var(--text-dim);
  letter-spacing: 1px;
  white-space: nowrap;
}

.dot {
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: var(--text-faint);
}

.badge.active {
  color: var(--ok);
  border-color: color-mix(in srgb, var(--ok) 55%, transparent);
}

.badge.active .dot {
  background: var(--ok);
  box-shadow: 0 0 5px var(--ok);
}

.badge.ended {
  color: var(--text-faint);
}

.badge.archived {
  color: var(--accent);
  border-color: color-mix(in srgb, var(--accent) 55%, transparent);
}

.badge.archived .dot {
  background: var(--accent);
}
</style>
