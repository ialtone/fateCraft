<script setup lang="ts">
import { ref, watch } from 'vue'
import { NButton, NCheckbox, NDynamicTags, NForm, NFormItem, NInput, NInputNumber } from 'naive-ui'
import type { WorldBookEntry } from '@/types'

const props = defineProps<{ modelValue: Partial<WorldBookEntry> }>()
const emit = defineEmits<{
  (e: 'save', value: Record<string, any>): void
  (e: 'cancel'): void
}>()

const form = ref<Record<string, any>>({})
const keywords = ref<string[]>([])

watch(
  () => props.modelValue,
  (v) => {
    form.value = { constant: false, enabled: true, priority: 100, ...v }
    keywords.value = [...(v.keywords || [])]
  },
  { immediate: true },
)

function save() {
  emit('save', {
    title: form.value.title || '未命名条目',
    content: form.value.content || '',
    keywords: keywords.value,
    constant: !!form.value.constant,
    enabled: !!form.value.enabled,
    priority: Number(form.value.priority) || 100,
    notes: form.value.notes || '',
  })
}
</script>

<template>
  <div class="editor">
    <div class="editor-head">{{ modelValue.id ? '编辑条目' : '新建条目' }}</div>
    <NForm label-placement="top" size="small">
      <NFormItem label="标题">
        <NInput v-model:value="form.title" placeholder="条目标题" />
      </NFormItem>
      <NFormItem label="内容">
        <NInput
          v-model:value="form.content"
          type="textarea"
          :autosize="{ minRows: 4, maxRows: 12 }"
        />
      </NFormItem>
      <NFormItem label="关键词（命中任一即激活）">
        <NDynamicTags v-model:value="keywords" />
      </NFormItem>
      <div class="row" style="gap: 18px; flex-wrap: wrap; margin-bottom: 8px">
        <NCheckbox v-model:checked="form.constant">常驻条目</NCheckbox>
        <NCheckbox v-model:checked="form.enabled">启用</NCheckbox>
      </div>
      <NFormItem label="优先级（小 = 优先）">
        <NInputNumber v-model:value="form.priority" :min="0" style="width: 160px" />
      </NFormItem>
    </NForm>
    <div class="row" style="justify-content: flex-end; gap: 8px">
      <NButton quaternary @click="emit('cancel')">取消</NButton>
      <NButton type="primary" @click="save">保存</NButton>
    </div>
  </div>
</template>

<style scoped>
.editor {
  background: var(--bg-panel);
  border: 1px solid var(--border-soft);
  border-radius: var(--radius);
  padding: 18px 20px;
  box-shadow: var(--shadow-card);
}

.editor-head {
  font-weight: 600;
  letter-spacing: 2px;
  margin-bottom: 14px;
  padding-bottom: 10px;
  border-bottom: 1px solid var(--border-soft);
}
</style>
