<script setup lang="ts">
import { ref } from 'vue'
import { NButton, NIcon, NInput, NSelect, NTag } from 'naive-ui'
import { ArrowDownOutline, ArrowUpOutline } from '@vicons/ionicons5'
import type { PromptSegment } from '@/types'

const props = defineProps<{ segments: PromptSegment[] }>()
const emit = defineEmits<{
  (e: 'update', id: string, body: Record<string, any>): void
  (e: 'remove', id: string): void
  (e: 'add', body: Record<string, any>): void
}>()

const showAdd = ref(false)
const draft = ref<Record<string, any>>({
  key: '',
  role: 'system',
  content: '',
  position: props.segments.length,
})

const roleOptions = [
  { label: 'system', value: 'system' },
  { label: 'user', value: 'user' },
  { label: 'assistant', value: 'assistant' },
]

function toggle(seg: PromptSegment) {
  emit('update', seg.id, { enabled: !seg.enabled })
}
function move(seg: PromptSegment, dir: -1 | 1) {
  emit('update', seg.id, { position: seg.position + dir })
}
function addSegment() {
  emit('add', { ...draft.value })
  showAdd.value = false
  draft.value = { key: '', role: 'system', content: '', position: props.segments.length }
}
</script>

<template>
  <div class="seg-list">
    <div v-for="seg in segments" :key="seg.id" class="seg" :class="{ off: !seg.enabled }">
      <div class="row spread" style="margin-bottom: 8px">
        <span class="row" style="gap: 8px">
          <strong class="seg-key">{{ seg.key }}</strong>
          <NTag size="tiny" round :bordered="false">{{ seg.role }}</NTag>
          <NTag v-if="seg.required" size="tiny" round type="warning" :bordered="false">必选</NTag>
        </span>
        <div class="row" style="gap: 2px">
          <NButton quaternary size="tiny" title="上移" @click="move(seg, -1)">
            <template #icon><NIcon :component="ArrowUpOutline" /></template>
          </NButton>
          <NButton quaternary size="tiny" title="下移" @click="move(seg, 1)">
            <template #icon><NIcon :component="ArrowDownOutline" /></template>
          </NButton>
          <NButton quaternary size="tiny" @click="toggle(seg)">{{
            seg.enabled ? '禁用' : '启用'
          }}</NButton>
          <NButton
            quaternary
            size="tiny"
            type="error"
            :disabled="seg.required"
            @click="emit('remove', seg.id)"
          >
            删除
          </NButton>
        </div>
      </div>
      <NInput
        :default-value="seg.content"
        type="textarea"
        :autosize="{ minRows: 3, maxRows: 10 }"
        @change="(v: string) => emit('update', seg.id, { content: v })"
      />
    </div>

    <div v-if="showAdd" class="seg">
      <div class="row" style="gap: 8px; margin-bottom: 8px">
        <NInput v-model:value="draft.key" placeholder="key（如 style_note）" />
        <NSelect v-model:value="draft.role" :options="roleOptions" style="width: 140px" />
      </div>
      <NInput
        v-model:value="draft.content"
        type="textarea"
        :autosize="{ minRows: 3, maxRows: 10 }"
        placeholder="分段内容"
      />
      <div class="row" style="justify-content: flex-end; gap: 8px; margin-top: 10px">
        <NButton quaternary @click="showAdd = false">取消</NButton>
        <NButton type="primary" @click="addSegment">添加</NButton>
      </div>
    </div>

    <NButton v-else dashed style="align-self: flex-start" @click="showAdd = true"
      >＋ 添加分段</NButton
    >
  </div>
</template>

<style scoped>
.seg-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.seg {
  background: var(--bg-panel);
  border: 1px solid var(--border-soft);
  border-radius: var(--radius);
  padding: 14px 16px;
  transition: opacity 0.2s ease;
}

.seg.off {
  opacity: 0.5;
}

.seg-key {
  letter-spacing: 0.5px;
}
</style>
