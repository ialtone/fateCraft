<script setup lang="ts">
import { NTooltip } from 'naive-ui'

withDefaults(
  defineProps<{
    label: string
    ariaLabel?: string
    placement?: 'top' | 'top-start' | 'top-end' | 'bottom' | 'bottom-start' | 'bottom-end'
  }>(),
  {
    ariaLabel: undefined,
    placement: 'top-start',
  },
)
</script>

<template>
  <span class="field-help">
    <span>{{ label }}</span>
    <NTooltip :placement="placement" trigger="hover">
      <template #trigger>
        <button type="button" class="help-mark" :aria-label="ariaLabel || `${label}说明`">?</button>
      </template>
      <div class="help-content">
        <slot />
      </div>
    </NTooltip>
  </span>
</template>

<style scoped>
.field-help {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.help-mark {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 16px;
  height: 16px;
  padding: 0;
  border: 1px solid var(--text-faint);
  border-radius: 50%;
  background: transparent;
  color: var(--text-faint);
  font-size: 11px;
  font-weight: 600;
  line-height: 1;
  cursor: help;
}

.help-mark:hover,
.help-mark:focus-visible {
  border-color: var(--accent);
  color: var(--accent);
  outline: none;
}

.help-content {
  max-width: 360px;
  line-height: 1.55;
}

.help-content :deep(strong) {
  display: block;
  margin-bottom: 5px;
}

.help-content :deep(p) {
  margin: 5px 0 0;
}
</style>
