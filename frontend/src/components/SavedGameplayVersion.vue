<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { NButton } from 'naive-ui'
import http from '@/api'
const props = defineProps<{ conversationId: string }>()
const emit = defineEmits<{ allowed: [value: boolean] }>()
const router = useRouter()
interface VersionInfo {
  pack_name: string
  pack_slug: string
  version: string
  current_version?: string
  new_start_available: boolean
  run_allowed: boolean
  reason?: string
}
const info = ref<VersionInfo>(),
  error = ref('')
let sequence = 0,
  disposed = false,
  pending = false
async function load() {
  if (pending || disposed) return
  pending = true
  const id = props.conversationId,
    token = sequence
  try {
    const { data } = await http.get<VersionInfo>(`/conversations/${id}/gameplay-version`)
    if (!disposed && token === sequence) {
      info.value = data
      error.value = ''
      emit('allowed', data.run_allowed)
    }
  } catch {
    if (!disposed && token === sequence) error.value = '玩法版本状态暂时无法读取，请稍后刷新。'
  } finally {
    pending = false
  }
}
watch(
  () => props.conversationId,
  () => {
    sequence++
    info.value = undefined
    error.value = ''
    emit('allowed', false)
    void load()
  },
  { immediate: true },
)
const timer = setInterval(load, 15000)
onBeforeUnmount(() => {
  disposed = true
  sequence++
  clearInterval(timer)
})
</script>
<template>
  <span class="saved-version" v-if="info || error">
    <span v-if="error" class="version-tag version-error">版本状态读取失败</span>
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
