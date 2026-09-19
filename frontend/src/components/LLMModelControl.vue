<script setup lang="ts">
import { NButton, NTooltip } from 'naive-ui'
import ModelCombo from '@/components/ModelCombo.vue'
import type { LLMCapabilityDiagnostic, LLMCapabilityRole } from '@/types'

const props = withDefaults(
  defineProps<{
    modelValue?: string
    options: string[]
    role: LLMCapabilityRole
    diagnostic?: LLMCapabilityDiagnostic
    checking?: boolean
    disabled?: boolean
    placeholder?: string
  }>(),
  {
    modelValue: '',
    diagnostic: undefined,
    checking: false,
    disabled: false,
    placeholder: '可直接输入模型名称',
  },
)

const emit = defineEmits<{
  'update:modelValue': [value: string]
  detect: []
}>()

function diagnosticLabel() {
  const item = props.diagnostic
  if (!item) return '尚未检测'
  if (item.status === 'unavailable') return '暂时无法检测'
  if (item.status === 'inconclusive') return '检测结果不确定'
  if (item.status === 'unsupported') {
    return props.role === 'narrative' ? '当前接口不支持工具调用' : '当前接口不支持原生 JSON Schema'
  }
  return props.role === 'narrative' ? '工具循环可用' : 'JSON Schema 可用'
}

function diagnosticDetail() {
  const item = props.diagnostic
  if (!item) return ''
  const protocol = item.protocol === 'responses' ? 'Responses' : 'Chat Completions'
  const lines = [
    '实际检测：' + item.model + ' · ' + protocol + ' · ' + (item.stream ? '流式' : '非流式'),
  ]
  if (item.summary) lines.push(item.summary)
  return lines.join('\n')
}

function diagnosticClass() {
  const status = props.diagnostic?.status
  if (status === 'supported') return 'capability-ok'
  if (status === 'unsupported') return 'capability-error'
  return status === 'unavailable' || status === 'inconclusive' ? 'capability-warn' : ''
}
</script>

<template>
  <div class="model-control">
    <ModelCombo
      :model-value="modelValue"
      :options="options"
      :disabled="disabled"
      :placeholder="placeholder"
      @update:model-value="emit('update:modelValue', $event)"
    />
    <div class="capability-status" aria-live="polite">
      <div class="capability-result" :class="diagnosticClass()">
        <span class="status-dot" aria-hidden="true"></span>
        <NTooltip v-if="diagnostic" placement="top-start" trigger="hover">
          <template #trigger>
            <span class="diagnostic-label" tabindex="0">{{ diagnosticLabel() }}</span>
          </template>
          <div class="diagnostic-detail">{{ diagnosticDetail() }}</div>
        </NTooltip>
        <span v-else>{{ diagnosticLabel() }}</span>
      </div>
      <div class="capability-action">
        <NButton text type="primary" size="tiny" :loading="checking" @click="emit('detect')">
          <template #icon>
            <span class="detect-icon-placeholder" aria-hidden="true"></span>
          </template>
          检测
        </NButton>
      </div>
    </div>
  </div>
</template>

<style scoped>
.model-control {
  width: 100%;
  min-width: 0;
}

.capability-status {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  width: 100%;
  margin-top: 6px;
  color: var(--text-faint);
  font-size: 12px;
  line-height: 1.5;
}

.capability-result {
  display: inline-flex;
  align-items: center;
  min-width: 0;
  gap: 6px;
}

.status-dot {
  width: 6px;
  height: 6px;
  flex: 0 0 6px;
  border-radius: 50%;
  background: currentColor;
}

.diagnostic-label {
  min-width: 0;
  cursor: help;
}

.diagnostic-label:focus-visible {
  border-radius: 2px;
  outline: 1px solid currentColor;
  outline-offset: 2px;
}

.diagnostic-detail {
  max-width: min(420px, calc(100vw - 40px));
  white-space: pre-line;
  overflow-wrap: anywhere;
  line-height: 1.55;
}

.capability-action {
  width: 56px;
  flex: 0 0 56px;
}

.capability-action > .n-button {
  width: 100%;
  justify-content: center;
}

.detect-icon-placeholder {
  display: block;
  width: 14px;
  height: 14px;
  visibility: hidden;
}

.capability-ok {
  color: var(--success-color, #2f7d52);
}

.capability-warn {
  color: var(--warning-color, #9a6a12);
}

.capability-error {
  color: var(--error-color, #b04444);
}
</style>
