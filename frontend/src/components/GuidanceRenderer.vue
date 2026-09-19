<script setup lang="ts">
import { computed } from 'vue'
import { NButton } from 'naive-ui'
import type { GuidanceItem, GuidanceGroup } from '@/types'
import {
  guidanceConditionMatches,
  resolveGuidanceArguments,
  resolveGuidanceGroups,
  type GuidanceContext,
} from '@/lib/guidance'

const props = withDefaults(
  defineProps<{
    groups?: GuidanceGroup[]
    state?: unknown
    snapshot?: unknown
    placement?: string | string[]
    disabled?: boolean
    loading?: boolean
  }>(),
  { groups: () => [], state: undefined, snapshot: undefined, placement: undefined, disabled: false, loading: false },
)

const emit = defineEmits<{
  select: [item: GuidanceItem]
  message: [content: string]
  action: [item: GuidanceItem, args: Record<string, unknown>]
}>()

const context = computed<GuidanceContext>(() => ({ state: props.state, snapshot: props.snapshot }))
const resolved = computed(() => resolveGuidanceGroups(props.groups, context.value, props.placement))

function enabled(item: GuidanceItem) {
  // Server-side `enabled` is a projection hint.  When an authored condition
  // exists, evaluate it against the latest local state so state_update events
  // take effect without reloading the runtime document.  Legacy Campaign
  // adapters use the flag without a condition to preserve their disabled state.
  const legacyDisabled = item.enabled === false && !item.enabled_when
  return !legacyDisabled && guidanceConditionMatches(item.enabled_when, context.value)
}

function select(item: GuidanceItem) {
  if (props.disabled || props.loading || !enabled(item)) return
  emit('select', item)
  if (item.mode === 'message' && item.message) {
    emit('message', item.message)
    return
  }
  if (item.mode === 'action') {
    emit('action', item, resolveGuidanceArguments(item.arguments, context.value))
  }
}
</script>

<template>
  <div v-if="resolved.length" class="guidance" aria-label="玩法指引">
    <section v-for="entry in resolved" :key="entry.group.key" class="guidance-group">
      <div class="guidance-title">{{ entry.group.title }}</div>
      <div class="guidance-items">
        <NButton
          v-for="item in entry.items"
          :key="item.key"
          size="small"
          secondary
          :disabled="disabled || loading || !enabled(item)"
          :loading="loading"
          :title="item.description"
          @click="select(item)"
        >
          {{ item.label }}
        </NButton>
      </div>
    </section>
  </div>
</template>

<style scoped>
.guidance {
  display: grid;
  gap: 8px;
  margin-bottom: 9px;
}

.guidance-group {
  display: grid;
  gap: 6px;
}

.guidance-title {
  color: var(--text-faint);
  font-size: 11px;
  letter-spacing: 0;
}

.guidance-items {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.guidance-items :deep(.n-button) {
  max-width: 100%;
  min-height: 32px;
  height: auto;
  padding-top: 6px;
  padding-bottom: 6px;
}

.guidance-items :deep(.n-button__content) {
  white-space: normal;
  overflow-wrap: anywhere;
  text-align: left;
}
</style>
