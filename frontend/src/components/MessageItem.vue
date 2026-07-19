<script setup lang="ts">
import { computed, ref } from 'vue'
import { NButton, NInput } from 'naive-ui'
import { renderMarkdown } from '@/utils/markdown'
import type { ChatMessage } from '@/stores/chat'

const props = defineProps<{ message: ChatMessage; editable: boolean }>()
const emit = defineEmits<{
  (e: 'edit', id: string, content: string): void
}>()

const editing = ref(false)
const draft = ref('')

const html = computed(() => renderMarkdown(props.message.markdown))
const roleLabel = computed(
  () =>
    ({
      user: '你',
      assistant: '叙事',
      system: '系统',
      state: '状态',
      event: '事件',
      roll_result: '天命',
    })[props.message.type] || props.message.type,
)
const isTemp = computed(() => props.message.id.startsWith('temp-'))
const isNarrative = computed(() => props.message.type === 'assistant')
const isUser = computed(() => props.message.type === 'user')
// 用户消息始终可以编辑（用于天道回溯），不受 editable 限制
const canEdit = computed(() => !isTemp.value && isUser.value)

function startEdit() {
  draft.value = props.message.markdown
  editing.value = true
}
function save() {
  emit('edit', props.message.id, draft.value)
  editing.value = false
}
</script>

<template>
  <article class="msg fade-up" :class="message.type">
    <div class="meta row spread">
      <span class="role"><span v-if="isNarrative" class="orn">◆</span>{{ roleLabel }}</span>
      <div v-if="canEdit" class="actions row" style="gap: 2px">
        <NButton quaternary size="tiny" @click="startEdit">编辑</NButton>
      </div>
    </div>
    <template v-if="editing">
      <NInput v-model:value="draft" type="textarea" :autosize="{ minRows: 4, maxRows: 16 }" />
      <div class="row" style="margin-top: 8px; justify-content: flex-end; gap: 6px">
        <NButton quaternary size="small" @click="editing = false">取消</NButton>
        <NButton type="primary" size="small" @click="save">保存</NButton>
      </div>
    </template>
    <div v-else class="markdown content" v-html="html"></div>
    <span v-if="message.streaming" class="cursor">▍</span>
  </article>
</template>

<style scoped>
.msg {
  position: relative;
  margin-bottom: 18px;
  max-width: 100%; /* 全宽显示 */
}

/* 叙事：书页式，无气泡，衬线正文 + 左侧饰线 */
.msg.assistant {
  padding: 4px 0 4px 18px;
  border-left: 2px solid var(--border-soft);
  transition: border-color 0.25s ease;
}

.msg.assistant:hover {
  border-left-color: var(--accent);
}

.msg.assistant .content {
  font-size: 14.5px;
  line-height: 1.85;
}

/* 玩家：全宽显示，琥珀色背景 */
.msg.user {
  width: 100%; /* 全宽 */
  background: var(--user-bubble);
  border: 1px solid var(--border-soft);
  border-radius: 8px;
  padding: 12px 16px;
}

/* 系统/事件/状态：居中淡化 */
.msg.system,
.msg.event,
.msg.state {
  margin-left: auto;
  margin-right: auto;
  text-align: center;
  color: var(--text-dim);
  font-size: 13px;
  padding: 6px 0;
}

/* 天命判定结果：居中窄幅卡片，主题色点缀以便一眼认出判定输赢 */
.msg.roll_result {
  margin-left: auto;
  margin-right: auto;
  max-width: 480px;
  text-align: center;
  font-size: 13px;
  padding: 8px 16px;
  background: var(--accent-soft);
  border: 1px solid color-mix(in srgb, var(--accent) 30%, transparent);
  border-radius: 8px;
}

.msg.roll_result .content {
  color: var(--text-dim);
  line-height: 1.7;
}

.meta {
  margin-bottom: 4px;
  min-height: 22px;
}

.role {
  font-size: 11px;
  font-weight: 600;
  color: var(--accent);
  letter-spacing: 2.5px;
}

.orn {
  font-size: 8px;
  margin-right: 5px;
  vertical-align: 2px;
}

.msg.user .role {
  color: var(--text-faint);
}

.actions {
  opacity: 0;
  transition: opacity 0.15s;
}

.msg:hover .actions {
  opacity: 1;
}

.cursor {
  color: var(--accent);
  animation: blink 1s steps(2) infinite;
}

@keyframes blink {
  50% {
    opacity: 0;
  }
}

/* 移动端适配 */
@media (max-width: 768px) {
  .msg {
    margin-bottom: 14px;
  }

  .msg.assistant {
    padding: 4px 0 4px 12px;
  }

  .msg.user {
    padding: 10px 14px;
    font-size: 14px;
  }

  .actions {
    gap: 4px;
  }

  .actions button {
    padding: 4px 8px;
    font-size: 11px;
  }
}
</style>
