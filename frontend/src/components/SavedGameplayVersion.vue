<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, watch } from 'vue'
import { useRouter } from 'vue-router'
import { NButton } from 'naive-ui'
import { useGameplayVersionStore } from '@/stores/gameplayVersion'
import { useSocketStore } from '@/stores/socket'
const props = defineProps<{ conversationId: string }>()
const emit = defineEmits<{ allowed: [value: boolean] }>()
const router = useRouter()
const store = useGameplayVersionStore()
const socket = useSocketStore()
const info = computed(() => store.versions[props.conversationId])
const error = computed(() => store.errors[props.conversationId] ?? '')
watch([info, error], () => emit('allowed', !!info.value?.run_allowed && !error.value))
let disposed = false

async function refresh(force = false) {
  if (socket.writeBlocked) return
  const id = props.conversationId
  try {
    const next = await store.load(id, force)
    if (!disposed && id === props.conversationId) emit('allowed', next.run_allowed)
  } catch {
    if (!disposed && id === props.conversationId) emit('allowed', false)
  }
}

watch(
  () => props.conversationId,
  () => {
    emit('allowed', false)
    void refresh(true)
  },
  { immediate: true },
)

// 取消周期轮询：只在重新可见且缓存超过 60 秒时刷新。
function visibleAgain() {
  if (disposed || document.visibilityState === 'hidden') return
  if (!store.fresh(props.conversationId)) void refresh()
}
onMounted(() => {
  document.addEventListener('visibilitychange', visibleAgain)
  window.addEventListener('focus', visibleAgain)
})
onBeforeUnmount(() => {
  disposed = true
  document.removeEventListener('visibilitychange', visibleAgain)
  window.removeEventListener('focus', visibleAgain)
})
</script>
<template>
  <span class="saved-version" v-if="info || error">
    <span v-if="error && !info" class="version-tag version-error">版本状态读取失败</span>
    <template v-if="info">
      <span
        class="version-tag"
        :class="{ 'version-tag-paused': !info.run_allowed }"
        :title="
          !info.run_allowed
            ? `暂停推进：${info.reason || '管理员已暂停此版本'}`
            : `${info.pack_name} · 本局固定版本 ${info.version}`
        "
      >
        {{ info.version }}
      </span>
      <NButton
        v-if="
          info.new_start_available && (!info.run_allowed || info.current_version !== info.version)
        "
        size="small"
        @click="router.push({ path: '/', query: { action: 'new' } })"
        >使用当前版本重新开局</NButton
      >
      <small
        v-if="
          info.new_start_available && (!info.run_allowed || info.current_version !== info.version)
        "
        >新开局不继承当前进度，原对话保留。</small
      >
    </template>
  </span>
</template>
<style scoped>
.saved-version {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  vertical-align: middle;
}

.version-tag {
  display: inline-flex;
  align-items: center;
  max-width: 96px;
  min-height: 22px;
  padding: 0 8px;
  border: 1px solid var(--border-soft);
  border-radius: 999px;
  color: var(--text-dim);
  background: var(--bg-soft);
  font-size: 11px;
  line-height: 20px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.version-tag::before {
  content: 'v';
  margin-right: 1px;
  color: var(--text-faint);
}

.version-tag-paused,
.version-error {
  color: var(--warning);
  border-color: color-mix(in srgb, var(--warning) 45%, var(--border-soft));
}
</style>
