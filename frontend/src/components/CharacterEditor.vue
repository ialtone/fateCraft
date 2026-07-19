<script setup lang="ts">
import { ref, watch } from 'vue'
import { NButton, NDynamicTags, NForm, NFormItem, NInput } from 'naive-ui'
import type { Character } from '@/types'

const props = defineProps<{ modelValue: Partial<Character> }>()
const emit = defineEmits<{
  (e: 'save', value: Record<string, any>): void
  (e: 'cancel'): void
}>()

const form = ref<Record<string, any>>({})
const tags = ref<string[]>([])

watch(
  () => props.modelValue,
  (v) => {
    form.value = { ...v }
    tags.value = [...(v.tags || [])]
  },
  { immediate: true },
)

function save() {
  emit('save', {
    name: form.value.name || '未命名角色',
    description: form.value.description || '',
    personality: form.value.personality || '',
    scenario: form.value.scenario || '',
    first_message: form.value.first_message || '',
    example_dialogues: form.value.example_dialogues || '',
    system_prompt_override: form.value.system_prompt_override || '',
    tags: tags.value,
  })
}
</script>

<template>
  <div class="editor">
    <div class="editor-head">{{ modelValue.id ? '编辑角色' : '新建角色' }}</div>
    <NForm label-placement="top" size="small">
      <NFormItem label="名称">
        <NInput v-model:value="form.name" placeholder="角色名称" />
      </NFormItem>
      <NFormItem label="描述">
        <NInput
          v-model:value="form.description"
          type="textarea"
          :autosize="{ minRows: 3, maxRows: 8 }"
        />
      </NFormItem>
      <NFormItem label="性格">
        <NInput
          v-model:value="form.personality"
          type="textarea"
          :autosize="{ minRows: 2, maxRows: 6 }"
        />
      </NFormItem>
      <NFormItem label="场景">
        <NInput
          v-model:value="form.scenario"
          type="textarea"
          :autosize="{ minRows: 2, maxRows: 6 }"
        />
      </NFormItem>
      <NFormItem label="开场白">
        <NInput
          v-model:value="form.first_message"
          type="textarea"
          :autosize="{ minRows: 3, maxRows: 8 }"
        />
      </NFormItem>
      <NFormItem label="示例对话">
        <NInput
          v-model:value="form.example_dialogues"
          type="textarea"
          :autosize="{ minRows: 3, maxRows: 8 }"
        />
      </NFormItem>
      <NFormItem label="系统提示覆盖">
        <NInput
          v-model:value="form.system_prompt_override"
          type="textarea"
          :autosize="{ minRows: 2, maxRows: 6 }"
        />
      </NFormItem>
      <NFormItem label="标签">
        <NDynamicTags v-model:value="tags" />
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
