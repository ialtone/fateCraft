<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import {
  NButton,
  NButtonGroup,
  NAvatar,
  NCard,
  NEmpty,
  NInput,
  NModal,
  NSelect,
  NSpin,
  NSwitch,
  NTag,
  useDialog,
  useMessage,
} from 'naive-ui'
import { messagesApi, sharesApi } from '@/api'
import SharedMessageItem from '@/components/SharedMessageItem.vue'
import { useAuthStore } from '@/stores/auth'
import type {
  ConversationShare,
  ConversationShareScope,
  DisplayMessage,
  SharedMessage,
} from '@/types'

const props = defineProps<{
  conversationId: string
  conversationTitle: string
  disabled?: boolean
  replaceTarget?: ConversationShare | null
}>()
const emit = defineEmits<{ published: [] }>()
const show = defineModel<boolean>('show', { default: false })
const message = useMessage()
const dialog = useDialog()
const auth = useAuthStore()

const loadingMessages = ref(false)
const publishing = ref(false)
const visibleMessageCount = ref(100)
const sourceMessages = ref<DisplayMessage[]>([])
const hasEarlierMessages = ref(false)
const scope = ref<ConversationShareScope>('all')
const startIndex = ref<number | null>(null)
const endIndex = ref<number | null>(null)
const title = ref('')
const showUsername = ref(false)
const expiresInDays = ref<0 | 7 | 30>(30)

const expiryOptions = [
  { label: '7 天', value: 7 },
  { label: '30 天', value: 30 },
  { label: '永久', value: 0 },
]

const sharedMessages = computed<SharedMessage[]>(() =>
  sourceMessages.value.map((item, index) => ({
    position: index + 1,
    type: item.type,
    markdown: item.markdown,
    metadata: item.metadata,
    created_at: item.created_at,
  })),
)
const messageOptions = computed(() =>
  sharedMessages.value.map((item, index) => ({
    label: `#${index + 1} · ${messageRole(item.type)} · ${messageExcerpt(item.markdown)}`,
    value: index,
  })),
)
const endMessageOptions = computed(() =>
  startIndex.value === null
    ? []
    : messageOptions.value.filter((option) => option.value >= startIndex.value!),
)

const selectedBounds = computed<[number, number] | null>(() => {
  if (scope.value === 'all') {
    return sharedMessages.value.length ? [0, sharedMessages.value.length - 1] : null
  }
  if (startIndex.value === null) return null
  return [startIndex.value, endIndex.value ?? startIndex.value]
})

const selectedMessages = computed(() => {
  const bounds = selectedBounds.value
  return bounds ? sharedMessages.value.slice(bounds[0], bounds[1] + 1) : []
})

const previewMessages = computed(() => {
  const bounds = selectedBounds.value
  const start = scope.value === 'range' ? bounds?.[0] : 0
  if (start === undefined) return []

  const messages =
    scope.value === 'range' && bounds
      ? sharedMessages.value.slice(bounds[0], bounds[1] + 1)
      : sharedMessages.value.slice(0, visibleMessageCount.value)

  return messages.map((message, offset) => {
    const sourceIndex = start + offset
    return {
      id: sourceMessages.value[sourceIndex]?.id ?? sourceIndex,
      message,
      sourceIndex,
    }
  })
})

const publicAttribution = computed(() => {
  if (!showUsername.value) return '匿名旅人'
  const displayName = auth.user?.display_name.trim() || '当前用户'
  return Array.from(displayName).slice(0, 48).join('')
})
const publicAvatarURL = computed(() => {
  if (!showUsername.value || !auth.user?.avatar_url) return undefined
  return auth.user.avatar_url
})

watch(
  show,
  async (visible) => {
    if (!visible) return
    title.value = props.conversationTitle.slice(0, 120)
    scope.value = 'all'
    startIndex.value = null
    endIndex.value = null
    showUsername.value = false
    hasEarlierMessages.value = false
    visibleMessageCount.value = 100
    await loadMessages()
    if (props.replaceTarget) applyReplacement(props.replaceTarget)
  },
  { immediate: true },
)

async function loadMessages() {
  loadingMessages.value = true
  try {
    const loaded = await messagesApi.list(props.conversationId, { limit: 500 })
    sourceMessages.value = loaded
    if (loaded.length === 500) {
      const earlier = await messagesApi.list(props.conversationId, {
        before: loaded[0].id,
        limit: 1,
      })
      hasEarlierMessages.value = earlier.length > 0
      if (hasEarlierMessages.value) scope.value = 'range'
    }
  } catch (error: any) {
    message.error(error?.message || '加载可分享消息失败')
  } finally {
    loadingMessages.value = false
  }
}

function selectStart(index: number | null) {
  startIndex.value = index
  if (index === null) {
    endIndex.value = null
    return
  }
  if (endIndex.value === null || endIndex.value < index) endIndex.value = index
  revealMessage(index)
}

function selectEnd(index: number | null) {
  if (startIndex.value === null || index === null) {
    endIndex.value = null
    return
  }
  endIndex.value = Math.max(index, startIndex.value)
  revealMessage(endIndex.value)
}

function revealMessage(index: number) {
  visibleMessageCount.value = Math.max(visibleMessageCount.value, index + 1)
}

function messageRole(type: SharedMessage['type']) {
  return {
    user: '旅人',
    assistant: '叙事',
    system: '系统',
    event: '事件',
    state: '状态',
    roll_result: '天命',
  }[type]
}

function messageExcerpt(markdown: string) {
  const excerpt = markdown.replace(/\s+/g, ' ').trim()
  return excerpt.length > 42 ? `${excerpt.slice(0, 42)}...` : excerpt || '空消息'
}

function endpointLabel(index: number) {
  if (index === startIndex.value && index === endIndex.value) return '起点 / 终点'
  if (index === startIndex.value) return '起点'
  if (index === endIndex.value) return '终点'
  return ''
}

function selectScope(next: ConversationShareScope) {
  if (next === 'all' && hasEarlierMessages.value) return
  scope.value = next
  if (next === 'range') {
    startIndex.value = null
    endIndex.value = null
  }
}

function isEndpoint(index: number) {
  const bounds = selectedBounds.value
  return !!bounds && (index === bounds[0] || index === bounds[1])
}

function requestBody() {
  const bounds = selectedBounds.value
  if (!bounds || selectedMessages.value.length === 0) throw new Error('请选择要分享的连续消息')
  return {
    scope: scope.value,
    start_message_id: scope.value === 'range' ? sourceMessages.value[bounds[0]].id : null,
    end_message_id: scope.value === 'range' ? sourceMessages.value[bounds[1]].id : null,
    title: title.value.trim(),
    show_username: showUsername.value,
    expires_in_days: expiresInDays.value,
    request_id: crypto.randomUUID(),
  } as const
}

async function publish() {
  if (props.disabled || publishing.value) return
  publishing.value = true
  try {
    const target = props.replaceTarget
    const share = await (target
      ? sharesApi.replace(target.id, requestBody())
      : sharesApi.create(props.conversationId, requestBody()))
    const prefix = target ? '分享已重新发布' : '分享已发布'
    show.value = false
    emit('published')
    const url = new URL(share.public_url, window.location.origin).toString()
    try {
      await navigator.clipboard.writeText(url)
      message.success(`${prefix}，链接已复制`)
    } catch {
      dialog.success({ title: prefix, content: url, positiveText: '知道了' })
    }
  } catch (error: any) {
    message.error(error?.message || '发布分享失败')
  } finally {
    publishing.value = false
  }
}

function applyReplacement(item: ConversationShare) {
  title.value = item.title
  showUsername.value = item.show_username
  startIndex.value = null
  endIndex.value = null
  if (item.scope === 'all' && hasEarlierMessages.value) {
    scope.value = 'range'
    message.warning('来源对话已超过 500 条消息，请重新选择要发布的连续片段')
    return
  }
  scope.value = item.scope
  if (item.scope === 'range') {
    const start = sourceMessages.value.findIndex(
      (message) => message.id === item.source_start_message_id,
    )
    const end = sourceMessages.value.findIndex(
      (message) => message.id === item.source_end_message_id,
    )
    if (start < 0 || end < start) {
      message.error('原分享范围已不在当前可选消息中，请手动选择连续片段')
    } else {
      startIndex.value = start
      endIndex.value = end
      visibleMessageCount.value = Math.max(visibleMessageCount.value, end + 1)
    }
  }
}
</script>

<template>
  <NModal v-model:show="show">
    <NCard
      class="share-dialog"
      :title="replaceTarget ? '重新发布分享' : '分享对话'"
      closable
      content-style="max-height: calc(100vh - 160px); max-height: calc(100dvh - 160px); overflow: auto"
      @close="show = false"
    >
      <NSpin :show="loadingMessages">
        <div class="share-form">
          <NButtonGroup>
            <NButton
              :type="scope === 'all' ? 'primary' : 'default'"
              @click="selectScope('all')"
              :disabled="hasEarlierMessages"
              >全部消息</NButton
            >
            <NButton :type="scope === 'range' ? 'primary' : 'default'" @click="selectScope('range')"
              >选择连续片段</NButton
            >
          </NButtonGroup>
          <div class="field-row">
            <label>分享标题<NInput v-model:value="title" maxlength="120" show-count /></label>
            <div class="identity-field">
              <span>公开身份</span>
              <div class="identity-control">
                <NSwitch v-model:value="showUsername">
                  <template #checked>显示用户名</template>
                  <template #unchecked>匿名</template>
                </NSwitch>
                <NAvatar
                  v-if="showUsername && publicAvatarURL"
                  round
                  :size="28"
                  :src="publicAvatarURL"
                  :alt="publicAttribution"
                />
                <NAvatar v-else-if="showUsername" round :size="28" :alt="publicAttribution">
                  {{ Array.from(publicAttribution)[0] }}
                </NAvatar>
                <span>{{ publicAttribution }}</span>
              </div>
            </div>
            <label>有效期<NSelect v-model:value="expiresInDays" :options="expiryOptions" /></label>
          </div>
          <p v-if="scope === 'range'" class="range-status">
            {{
              startIndex === null
                ? '请选择片段起点和终点'
                : `已选择 ${selectedMessages.length} 条连续消息`
            }}
          </p>
          <div v-if="scope === 'range'" class="range-controls">
            <label>
              片段起点
              <NSelect
                :value="startIndex"
                :options="messageOptions"
                filterable
                clearable
                placeholder="选择第一条消息"
                @update:value="selectStart"
              />
            </label>
            <label>
              片段终点
              <NSelect
                :value="endIndex"
                :options="endMessageOptions"
                :disabled="startIndex === null"
                filterable
                clearable
                placeholder="选择最后一条消息"
                @update:value="selectEnd"
              />
            </label>
          </div>
          <p v-if="hasEarlierMessages" class="range-status">
            当前对话超过 500 条消息；完整分享已禁用，可从最新 500 条中选择连续片段。
          </p>
          <div v-if="scope === 'range'" class="selection-preview-head">
            <strong>已选片段</strong>
            <span v-if="selectedBounds">
              #{{ selectedBounds[0] + 1 }} 至 #{{ selectedBounds[1] + 1 }} ·
              {{ selectedMessages.length }} 条
            </span>
            <span v-else>选择后仅显示片段内的消息</span>
          </div>
          <NEmpty
            v-if="scope === 'range' && !previewMessages.length"
            class="selection-empty"
            description="选择片段起点和终点后，这里将只显示选中的消息"
          />
          <div v-else class="share-message-picker">
            <div v-for="item in previewMessages" :key="item.id" class="message-choice">
              <span class="message-choice-head">
                <span>#{{ item.sourceIndex + 1 }}</span>
                <NTag
                  v-if="scope === 'range' && endpointLabel(item.sourceIndex)"
                  size="small"
                  :bordered="false"
                  type="warning"
                >
                  {{ endpointLabel(item.sourceIndex) }}
                </NTag>
              </span>
              <SharedMessageItem
                :message="item.message"
                :selected="scope === 'range'"
                :endpoint="scope === 'range' && isEndpoint(item.sourceIndex)"
              />
            </div>
            <NButton
              v-if="scope === 'all' && visibleMessageCount < sharedMessages.length"
              class="load-more"
              block
              quaternary
              @click="visibleMessageCount += 100"
            >
              加载更多消息（剩余 {{ sharedMessages.length - visibleMessageCount }} 条）
            </NButton>
          </div>
          <div v-if="replaceTarget" class="replace-notice">
            <span>发布成功后将撤销旧链接“{{ replaceTarget.title }}”。</span>
          </div>
          <div class="share-summary">
            将公开
            {{ selectedMessages.length }}
            条消息。公开链接不包含思考过程、状态、Memory、工具记录或账号资料。
          </div>
          <div class="dialog-actions">
            <NButton
              type="primary"
              :loading="publishing"
              :disabled="disabled || !selectedMessages.length"
              @click="publish"
              >{{ replaceTarget ? '重新发布并替换' : '发布分享' }}</NButton
            >
          </div>
        </div>
      </NSpin>
    </NCard>
  </NModal>
</template>

<style scoped>
.share-dialog {
  width: min(920px, calc(100vw - 24px));
}
.share-form {
  display: grid;
  gap: 16px;
}
.field-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) 150px;
  gap: 12px;
}
.field-row label,
.identity-field {
  display: grid;
  gap: 6px;
  color: var(--text-dim);
  font-size: 12px;
}
.identity-control {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 8px;
  min-height: 34px;
}
.identity-control > span {
  min-width: 0;
  overflow: hidden;
  color: var(--text-dim);
  text-overflow: ellipsis;
  white-space: nowrap;
}
.range-controls {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}
.range-controls label {
  display: grid;
  min-width: 0;
  gap: 6px;
  color: var(--text-dim);
  font-size: 12px;
}
.share-message-picker {
  max-height: 48vh;
  overflow-y: auto;
  border-block: 1px solid var(--border-soft);
  padding: 12px 4px;
}
.selection-preview-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 10px 12px;
  border-block: 1px solid var(--border-soft);
  background: var(--bg-soft);
}
.selection-preview-head strong {
  font-size: 13px;
}
.selection-preview-head span {
  color: var(--text-faint);
  font-size: 12px;
}
.selection-empty {
  padding: 32px 12px;
  border-bottom: 1px solid var(--border-soft);
}
.message-choice {
  display: block;
  width: 100%;
  padding: 0;
  border: 0;
  background: transparent;
  color: inherit;
  text-align: inherit;
}
.message-choice + .message-choice {
  margin-top: 8px;
}
.message-choice-head {
  display: flex;
  min-height: 24px;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 0 12px 4px;
  color: var(--text-faint);
  font-size: 11px;
}
.range-status,
.share-summary {
  margin: 0;
  color: var(--text-faint);
  font-size: 12px;
}
.replace-notice {
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  color: var(--text-dim);
  font-size: 12px;
}
.replace-notice span {
  overflow-wrap: anywhere;
}
.load-more {
  margin-top: 8px;
}
.dialog-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
@media (max-width: 720px) {
  .field-row {
    grid-template-columns: 1fr;
  }
  .range-controls {
    grid-template-columns: 1fr;
  }
}
</style>
