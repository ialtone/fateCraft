<script setup lang="ts">
import { computed } from 'vue'
import { NFormItem } from 'naive-ui'
import FieldHelp from '@/components/FieldHelp.vue'
import LLMModelControl from '@/components/LLMModelControl.vue'
import LLMModelLabel from '@/components/LLMModelLabel.vue'
import ModelCombo from '@/components/ModelCombo.vue'
import type { LLMCapabilityDiagnostic, LLMCapabilityRole } from '@/types'

const props = withDefaults(
  defineProps<{
    modelValue?: string
    reasoningEffort?: string
    options: string[]
    role: LLMCapabilityRole
    label?: string
    diagnostic?: LLMCapabilityDiagnostic
    checking?: boolean
    disabled?: boolean
    placeholder?: string
    modelFeedback?: string
  }>(),
  {
    modelValue: '',
    reasoningEffort: '',
    label: undefined,
    diagnostic: undefined,
    checking: false,
    disabled: false,
    placeholder: '可直接输入模型名称',
    modelFeedback: '',
  },
)

const emit = defineEmits<{
  'update:modelValue': [value: string]
  'update:reasoningEffort': [value: string]
  detect: []
}>()

const reasoningCopy: Record<
  LLMCapabilityRole,
  { label: string; emptyOptionLabel: string; placeholder: string; paragraphs: string[] }
> = {
  narrative: {
    label: '叙事思考强度',
    emptyOptionLabel: '默认（不传）',
    placeholder: '默认（不传）',
    paragraphs: [
      '控制推理型模型花在“思考”上的力度。留空时请求不传思考强度，由供应商决定默认行为；low、medium、high 为常见档位，也可以直接输入服务商支持的自定义档位（如 ultra、minimal）。',
      '档位越高，回复越慢、消耗越多；模型不支持思考强度时请留空。',
    ],
  },
  memory: {
    label: '记忆思考强度',
    emptyOptionLabel: '默认（不传）',
    placeholder: '留空继承叙事强度，可直接输入',
    paragraphs: [
      '仅作用于长期记忆归档请求。留空时自动使用叙事思考强度；如果叙事思考强度也留空，最终请求则不传思考强度。',
      '需要单独调整时，可从下拉列表选择，也可直接输入服务商支持的自定义档位。',
    ],
  },
  character_inquiry: {
    label: '问答思考强度',
    emptyOptionLabel: '默认（不传）',
    placeholder: '留空继承叙事强度，可直接输入',
    paragraphs: [
      '仅作用于角色问答请求。留空时自动使用叙事思考强度；如果叙事思考强度也留空，最终请求则不传思考强度。',
      '需要单独调整时，可从下拉列表选择，也可直接输入服务商支持的自定义档位。',
    ],
  },
}

const effortCopy = computed(() => reasoningCopy[props.role])
const reasoningEffortOptions = computed(() => [
  { label: effortCopy.value.emptyOptionLabel, value: '' },
  { label: 'low', value: 'low' },
  { label: 'medium', value: 'medium' },
  { label: 'high', value: 'high' },
])
</script>

<template>
  <div class="llm-model-setting">
    <NFormItem>
      <template #label>
        <div class="model-label-row">
          <LLMModelLabel :role="role" :label="label" />
          <slot name="model-label-extra"></slot>
        </div>
      </template>
      <LLMModelControl
        :model-value="modelValue"
        :options="options"
        :role="role"
        :diagnostic="diagnostic"
        :checking="checking"
        :disabled="disabled"
        :placeholder="placeholder"
        @update:model-value="emit('update:modelValue', $event)"
        @detect="emit('detect')"
      />
      <template v-if="modelFeedback" #feedback>{{ modelFeedback }}</template>
    </NFormItem>

    <NFormItem>
      <template #label>
        <FieldHelp :label="effortCopy.label">
          <p v-for="paragraph in effortCopy.paragraphs" :key="paragraph">{{ paragraph }}</p>
        </FieldHelp>
      </template>
      <ModelCombo
        :model-value="reasoningEffort"
        :options="reasoningEffortOptions"
        :disabled="disabled"
        :placeholder="effortCopy.placeholder"
        @update:model-value="emit('update:reasoningEffort', $event)"
      />
    </NFormItem>
  </div>
</template>

<style scoped>
.llm-model-setting {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(190px, 0.42fr);
  gap: 18px;
  align-items: start;
  padding: 14px 0;
  border-bottom: 1px solid var(--border-soft);
}

.llm-model-setting :deep(.n-form-item) {
  min-width: 0;
  margin-bottom: 0;
}

.model-label-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  width: 100%;
  min-width: 0;
}
</style>
