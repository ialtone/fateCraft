<script setup lang="ts">
import { ref } from 'vue'
import { NInput, NSelect } from 'naive-ui'
import RewardUserSearch from '@/components/RewardUserSearch.vue'
import type { AdminUser, DetailedLLMLoggingScope } from '@/types'

defineProps<{ enabled: boolean }>()
const scope = defineModel<DetailedLLMLoggingScope>('scope', { required: true })
const targetId = defineModel<string | null>('targetId', { required: true })
const selectedUser = ref<AdminUser | null>(null)
const options = [
  { label: '全部模型调用', value: 'all' },
  { label: '指定用户', value: 'user' },
  { label: '指定对话', value: 'conversation' },
]

function changeScope(value: DetailedLLMLoggingScope) {
  if (scope.value === value) return
  scope.value = value
  targetId.value = null
  selectedUser.value = null
}

function selectUser(user: AdminUser) {
  selectedUser.value = user
  targetId.value = user.id
}
</script>

<template>
  <div class="log-scope">
    <NSelect
      :value="scope"
      :options="options"
      :disabled="!enabled"
      aria-label="详细日志采集范围"
      @update:value="changeScope"
    />
    <template v-if="scope === 'user'">
      <div class="scope-hint">
        <template v-if="targetId">
          已选用户：
          <template v-if="selectedUser?.id === targetId">
            {{ selectedUser.display_name }} · @{{ selectedUser.username }} ·
          </template>
          {{ targetId }}
        </template>
        <template v-else>请查找并选择一个用户。</template>
      </div>
      <RewardUserSearch v-if="enabled" select-label="采集该用户" @select="selectUser" />
      <p class="scope-hint">包含该用户的叙事、记忆、角色询问和模型诊断调用。</p>
    </template>
    <template v-else-if="scope === 'conversation'">
      <NInput
        v-model:value="targetId"
        :disabled="!enabled"
        :maxlength="36"
        aria-label="采集对话 ID"
        placeholder="粘贴诊断日志中的完整对话 ID"
      />
      <p class="scope-hint">包含该对话的叙事、记忆和角色询问；无对话归属的模型诊断只留摘要。</p>
    </template>
    <p class="scope-hint">
      {{ enabled ? '保存后应用于后续开始的模型调用。' : '正文采集已关闭，已保存的范围会保留。' }}
      范围外仍保留调用摘要；正文按大小上限保存。
    </p>
  </div>
</template>

<style scoped>
.log-scope {
  display: grid;
  gap: 12px;
  width: 100%;
}
.scope-hint {
  margin: 0;
  color: var(--text-dim);
  overflow-wrap: anywhere;
}
</style>
