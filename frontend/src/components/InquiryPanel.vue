<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { NAlert, NButton, NIcon, NInput, NPopconfirm, NSpin, NTag, NTooltip } from 'naive-ui'
import { CloseCircleOutline, SendOutline, TrashOutline } from '@vicons/ionicons5'
import { useInquiryStore } from '@/stores/inquiry'

const props = defineProps<{ convId: string }>()
const inquiry = useInquiryStore()
const question = ref('')
const timeline = ref<HTMLElement | null>(null)

watch(
  () => inquiry.messages.length,
  async () => {
    await nextTick()
    if (timeline.value) timeline.value.scrollTop = timeline.value.scrollHeight
  },
)

async function submit() {
  const content = question.value.trim()
  if (!content || inquiry.asking) return
  question.value = ''
  await inquiry.ask(props.convId, content).catch(() => {})
}
</script>

<template>
  <section class="inquiry-panel">
    <div ref="timeline" class="inquiry-timeline">
      <NSpin v-if="inquiry.loading" class="loading" size="small" />
      <div v-else-if="!inquiry.messages.length" class="empty">暂无询问记录</div>
      <template v-else>
        <article
          v-for="message in inquiry.messages"
          :key="message.id"
          class="inquiry-message"
          :class="message.role"
        >
          <div v-if="message.status === 'completed'" class="bubble">{{ message.content }}</div>
          <div v-else-if="message.role === 'assistant'" class="failed">本次询问未完成</div>
          <div v-if="message.role === 'assistant' && message.source_refs.length" class="sources">
            <NTag
              v-for="source in message.source_refs"
              :key="`${message.id}-${source.source_id}`"
              size="tiny"
              :bordered="false"
            >
              {{ source.title }}
            </NTag>
          </div>
        </article>
      </template>
      <div v-if="inquiry.asking" class="thinking">正在查询当前记录…</div>
    </div>

    <NAlert v-if="inquiry.stale" class="notice" type="info" :show-icon="false">
      回答期间状态已更新
    </NAlert>
    <NAlert v-if="inquiry.error" class="notice" type="error" :show-icon="false" closable>
      {{ inquiry.error }}
    </NAlert>

    <div class="composer">
      <NInput
        v-model:value="question"
        type="textarea"
        :autosize="{ minRows: 2, maxRows: 5 }"
        maxlength="1000"
        placeholder="询问当前状态、任务或已发生事件"
        :disabled="inquiry.asking"
        @keydown.enter.exact.prevent="submit"
      />
      <div class="composer-actions">
        <NPopconfirm
          positive-text="清空"
          negative-text="取消"
          @positive-click="inquiry.clear(convId).catch(() => {})"
        >
          <template #trigger>
            <NTooltip>
              <template #trigger>
                <NButton
                  quaternary
                  circle
                  size="small"
                  aria-label="清空询问记录"
                  :disabled="!inquiry.messages.length || inquiry.asking"
                >
                  <template #icon><NIcon :component="TrashOutline" /></template>
                </NButton>
              </template>
              清空询问记录
            </NTooltip>
          </template>
          清空当前询问记录？
        </NPopconfirm>
        <div class="send-actions">
          <NTooltip v-if="inquiry.asking">
            <template #trigger>
              <NButton
                quaternary
                circle
                size="small"
                aria-label="取消询问"
                :loading="inquiry.cancelling"
                @click="inquiry.cancel(convId)"
              >
                <template #icon><NIcon :component="CloseCircleOutline" /></template>
              </NButton>
            </template>
            取消询问
          </NTooltip>
          <NTooltip>
            <template #trigger>
              <NButton
                type="primary"
                circle
                size="small"
                aria-label="发送询问"
                :loading="inquiry.asking"
                :disabled="!question.trim()"
                @click="submit"
              >
                <template #icon><NIcon :component="SendOutline" /></template>
              </NButton>
            </template>
            发送询问
          </NTooltip>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.inquiry-panel {
  display: flex;
  flex: 1;
  min-height: 0;
  flex-direction: column;
  background: var(--bg-panel);
}

.inquiry-timeline {
  display: flex;
  flex: 1;
  min-height: 0;
  flex-direction: column;
  gap: 12px;
  padding: 16px 14px;
  overflow: auto;
}

.loading,
.empty {
  margin: auto;
  color: var(--text-faint);
  font-size: 12px;
}

.inquiry-message {
  display: grid;
  max-width: 92%;
  gap: 5px;
}

.inquiry-message.user {
  align-self: end;
}

.inquiry-message.assistant {
  align-self: start;
}

.bubble {
  padding: 9px 11px;
  border-radius: 6px;
  color: var(--text);
  background: var(--bg-soft);
  font-size: 12px;
  line-height: 1.6;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}

.user .bubble {
  color: var(--accent);
  background: var(--accent-soft);
}

.sources {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

.failed,
.thinking {
  color: var(--text-faint);
  font-size: 11px;
}

.notice {
  margin: 0 12px 8px;
}

.composer {
  display: grid;
  gap: 8px;
  padding: 12px;
  border-top: 1px solid var(--border);
  background: var(--bg-soft);
}

.composer-actions {
  display: flex;
  justify-content: space-between;
}

.send-actions {
  display: flex;
  gap: 6px;
}
</style>
