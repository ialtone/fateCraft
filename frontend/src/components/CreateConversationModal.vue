<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import {
  NAlert,
  NButton,
  NForm,
  NFormItem,
  NInput,
  NModal,
  NSelect,
  NSpin,
  NTag,
  useMessage,
} from 'naive-ui'
import { useChallengeStore } from '@/stores/challenges'
import { useConversationStore } from '@/stores/conversations'
import { useSettingsStore } from '@/stores/settings'
import { RouterLink } from 'vue-router'
import { campaignsApi } from '@/api'
import type { Conversation, CreateConversationInput } from '@/types'

const props = defineProps<{ show: boolean; challengeSlug?: string }>()
const emit = defineEmits<{
  (e: 'update:show', value: boolean): void
  (e: 'created', conv: Conversation): void
}>()

const message = useMessage()
const challenges = useChallengeStore()
const conversations = useConversationStore()
const settings = useSettingsStore()
const creating = ref(false)
const checkingConfiguration = ref(false)
const configurationChecked = ref(false)
const configurationIssue = ref('')
const form = ref({ title: '', challenge_slug: '', persona_slug: '' })

// 本次创建将固化的路由（只读展示），有效路由由后端计算；server 名称随接入模式变化。
const pendingRoute = computed<'server' | 'client'>(() =>
  settings.effectiveRoute === 'client' ? 'client' : 'server',
)
const pendingRouteName = computed(() => {
  if (pendingRoute.value === 'client') return '客户端直连（不计奖励）'
  return settings.settings?.access_mode === 'platform_managed' ? '平台模型' : '服务端转发'
})

const selected = computed(() =>
  challenges.list.find((challenge) => challenge.slug === form.value.challenge_slug),
)
const challengeOptions = computed(() =>
  challenges.list.map((challenge) => ({
    label: `${challenge.name} · ${challenge.version}`,
    value: challenge.slug,
  })),
)
const personaOptions = computed(() =>
  (selected.value?.personas ?? []).map((persona) => ({
    label: persona.name,
    value: persona.slug,
  })),
)
const canCreate = computed(
  () =>
    Boolean(selected.value?.available && form.value.persona_slug) &&
    configurationChecked.value &&
    !checkingConfiguration.value &&
    !configurationIssue.value &&
    !creating.value,
)

async function checkConfiguration() {
  checkingConfiguration.value = true
  configurationChecked.value = false
  try {
    configurationIssue.value = await settings.checkPlayReadiness()
    configurationChecked.value = true
    return !configurationIssue.value
  } finally {
    checkingConfiguration.value = false
  }
}

async function reset() {
  const configurationCheck = checkConfiguration()
  if (!challenges.list.length) {
    try {
      await challenges.load()
    } catch (err: any) {
      message.error(`读取玩法失败：${err.message || '未知错误'}`)
    }
  }
  await configurationCheck
  const target =
    challenges.list.find((item) => item.slug === props.challengeSlug) ?? challenges.list[0]
  form.value = {
    title: '',
    challenge_slug: target?.slug ?? '',
    persona_slug: target?.personas[0]?.slug ?? '',
  }
}

watch(
  () => props.show,
  (show) => {
    if (show) void reset()
  },
  { immediate: true },
)

watch(
  () => form.value.challenge_slug,
  () => {
    if (!selected.value?.personas.some((persona) => persona.slug === form.value.persona_slug)) {
      form.value.persona_slug = selected.value?.personas[0]?.slug ?? ''
    }
  },
)

async function submit() {
  if (!canCreate.value) return
  creating.value = true
  try {
    if (!(await checkConfiguration())) return
    const body: CreateConversationInput = {
      challenge_slug: form.value.challenge_slug,
      persona_slug: form.value.persona_slug,
    }
    if (form.value.title.trim()) body.title = form.value.title.trim()
    const conv =
      selected.value?.runtime.mode === 'campaign'
        ? await campaignsApi
            .create(body)
            .then((campaign) => campaignsApi.createConversation(campaign.id))
        : await conversations.create(body)
    emit('created', conv)
    emit('update:show', false)
  } catch (err: any) {
    message.error(`创建玩法失败：${err.message || '未知错误'}`)
  } finally {
    creating.value = false
  }
}
</script>

<template>
  <NModal
    :show="show"
    preset="dialog"
    title="开始玩法"
    :mask-closable="!creating"
    class="create-dialog"
    style="width: min(620px, calc(100vw - 24px)); max-height: calc(100dvh - 24px)"
    @update:show="emit('update:show', $event)"
  >
    <NSpin :show="challenges.loading || checkingConfiguration">
      <NForm label-placement="top" class="form">
        <NAlert v-if="configurationIssue" type="warning" class="configuration-note">
          {{ configurationIssue }}
          <div class="configuration-actions">
            <RouterLink to="/settings" @click="emit('update:show', false)">前往设置</RouterLink>
            <NButton text :disabled="checkingConfiguration" @click="checkConfiguration">
              重新检查
            </NButton>
          </div>
        </NAlert>
        <NFormItem label="玩法">
          <NSelect v-model:value="form.challenge_slug" :options="challengeOptions" />
        </NFormItem>

        <div v-if="selected" class="challenge-summary">
          <div class="summary-title">
            <strong>{{ selected.name }}</strong>
            <NTag size="small" :bordered="false" type="info">
              {{ selected.runtime.mode === 'campaign' ? '长期世界' : '单局挑战' }}
            </NTag>
            <NTag size="small" :bordered="false">v{{ selected.version }}</NTag>
          </div>
          <p>{{ selected.description }}</p>
          <NTag v-if="!selected.available" size="small" type="error">
            {{ selected.unavailable_reason || '尚未满足进入条件' }}
          </NTag>
          <div class="reward-row">
            <NTag
              v-for="reward in selected.reward_overview"
              :key="reward.outcome_key"
              size="small"
              type="warning"
            >
              {{ reward.outcome_title }}：{{ reward.points }} 积分
            </NTag>
          </div>
        </div>

        <NFormItem label="玩家人设">
          <NSelect
            v-model:value="form.persona_slug"
            :options="personaOptions"
            :disabled="!selected"
          />
        </NFormItem>
        <p v-if="selected" class="persona-description">
          {{ selected.personas.find((item) => item.slug === form.persona_slug)?.description }}
        </p>

        <NFormItem :label="selected?.runtime.mode === 'campaign' ? '存档名称' : '对话标题'">
          <NInput
            v-model:value="form.title"
            maxlength="80"
            :placeholder="
              selected?.runtime.mode === 'campaign' ? '留空使用玩法名称' : '留空使用默认标题'
            "
          />
        </NFormItem>
        <p class="faint sm route-hint">
          本对话将使用：{{ pendingRouteName }}
          <RouterLink to="/settings">去设置修改</RouterLink>
        </p>
      </NForm>
    </NSpin>

    <template #action>
      <NButton :disabled="creating" @click="emit('update:show', false)">取消</NButton>
      <NButton type="primary" :loading="creating" :disabled="!canCreate" @click="submit">
        {{ selected?.runtime.mode === 'campaign' ? '开始旅程' : '开始试炼' }}
      </NButton>
    </template>
  </NModal>
</template>

<style scoped>
.form {
  margin-top: 8px;
}

.configuration-note {
  margin-bottom: 18px;
}

.configuration-actions {
  display: flex;
  gap: 16px;
  margin-top: 8px;
}

.challenge-summary {
  margin: -4px 0 18px;
  padding: 14px 16px;
  border-left: 3px solid var(--accent);
  background: var(--bg-soft);
}

.summary-title,
.reward-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}

.challenge-icon {
  color: var(--accent);
  font-size: 18px;
}

.challenge-summary p,
.persona-description {
  color: var(--text-dim);
  font-size: 12px;
  line-height: 1.7;
}

.persona-description {
  margin: -14px 0 18px;
}

.route-hint {
  margin: -6px 0 6px;
}

.create-dialog :deep(.n-dialog__content) {
  min-height: 0;
  overflow-y: auto;
}

@media (max-width: 480px) {
  .challenge-summary {
    padding: 12px;
  }
}
</style>
