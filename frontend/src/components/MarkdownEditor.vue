<script setup lang="ts">
// Markdown 输入与只读展示；renderMarkdown 经 DOMPurify 净化，不执行脚本。
import { computed, ref } from 'vue'
import { NButton, NInput } from 'naive-ui'
import { renderMarkdown } from '@/utils/markdown'
const props = defineProps<{
  modelValue: string
  readonly?: boolean
  placeholder?: string
  maxlength?: number
}>()
defineEmits<{ 'update:modelValue': [value: string] }>()
const previewing = ref(false)
const html = computed(() => renderMarkdown(props.modelValue))
</script>
<template>
  <!-- eslint-disable vue/no-v-html -->
  <div v-if="readonly" class="markdown" v-html="html" />
  <div v-else class="editor">
    <div class="bar">
      <span>支持 Markdown</span>
      <NButton size="small" secondary class="toggle" @click="previewing = !previewing">{{
        previewing ? '编辑' : '预览'
      }}</NButton>
    </div>
    <div v-if="previewing" class="markdown preview" v-html="html || '<p>暂无内容</p>'" />
    <NInput
      v-else
      :value="modelValue"
      type="textarea"
      :rows="8"
      :maxlength="maxlength"
      :placeholder="placeholder ?? '支持 Markdown'"
      @update:value="$emit('update:modelValue', $event)"
    />
  </div>
</template>
<style scoped>
.editor {
  display: grid;
  min-width: 0;
  gap: 6px;
  width: 100%;
}
.bar {
  display: flex;
  min-height: 28px;
  gap: 8px;
  justify-content: space-between;
  align-items: center;
  color: var(--text-dim);
  font-size: 12px;
}
.toggle {
  flex-shrink: 0;
}
.markdown {
  overflow-wrap: anywhere;
}
.markdown :deep(img) {
  max-width: 100%;
}
.markdown :deep(p) {
  margin: 4px 0;
}
.preview {
  min-height: 178px;
  max-height: 480px;
  overflow: auto;
  padding: 8px 12px;
  border: 1px solid var(--border-soft);
  border-radius: 6px;
}
</style>
