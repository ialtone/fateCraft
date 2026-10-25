<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import {
  NAlert,
  NButton,
  NEmpty,
  NInput,
  NInputNumber,
  NSelect,
  NSpin,
  NSwitch,
  useDialog,
  useMessage,
} from 'naive-ui'
import axios from 'axios'
import { moduleActionsApi } from '@/api'
import { usePlayerActionStore } from '@/stores/playerActions'
import { storeToRefs } from 'pinia'
import type { ModuleActionInput, ModuleActionSnapshot } from '@/types/module-actions'
import { useChatStore } from '@/stores/chat'
import { useConversationStore } from '@/stores/conversations'
import { useStateStore } from '@/stores/state'
import { useCampaignStore } from '@/stores/campaign'
import { useSocketStore } from '@/stores/socket'
import { useGameplayVersionStore } from '@/stores/gameplayVersion'
import { useRuntimeStore } from '@/stores/runtime'
import { useContextStore } from '@/stores/context'
import { actionBlock } from '@/lib/action-gate'
import { withoutEntityBoundActions } from '@/lib/campaign-guidance'
import { stageProgressNotice } from '@/lib/stage-rewards'

const props = defineProps<{ convId: string; visible: boolean }>()
const chat = useChatStore()
const conversations = useConversationStore()
const state = useStateStore()
const campaign = useCampaignStore()
const socket = useSocketStore()
const gameplay = useGameplayVersionStore()
const runtime = useRuntimeStore()
const context = useContextStore()
const snapshot = ref<ModuleActionSnapshot | null>(null)
const selectedKey = ref<string | null>(null)
const planIndex = ref<number | null>(null)
const argumentsValue = ref<Record<string, string | number | boolean>>({})
const actions = usePlayerActionStore()
const dialog = useDialog()
const message = useMessage()
const { pending, acting: submitting, error } = storeToRefs(actions)
const loading = ref(false)
const checkingGameplay = ref(false)
let generation = 0
let alive = true
let inFlight: Promise<void> | null = null
let queued = false
let dirty = true
let scheduled: ReturnType<typeof setTimeout> | null = null
// campaign_view.actions 中带 source_path 的动作由 Campaign 侧栏按实体渲染；
// 行动页只保留需要玩家填写参数或方案的动作，避免同一 (action_key, entity)
// 出现两个提交入口。
const visibleCapabilities = computed(() =>
  withoutEntityBoundActions(
    snapshot.value?.action_capabilities,
    runtime.presentation?.campaign_view?.actions,
  ),
)
const hiddenOnly = computed(
  () => (snapshot.value?.action_capabilities.length ?? 0) > 0 && !visibleCapabilities.value.length,
)
const active = computed(() => visibleCapabilities.value.find((a) => a.key === selectedKey.value))
const plan = computed(() =>
  planIndex.value === null ? undefined : active.value?.plans?.[planIndex.value],
)
const planBlocked = computed(() => !!plan.value?.blocked_reason)
const planOptions = computed(() =>
  (active.value?.plans ?? []).map((p, value) => ({
    label: p.blocked_reason ? `${p.label}（${p.blocked_reason}）` : p.label,
    value,
    disabled: !!p.blocked_reason,
  })),
)
const costs = computed(() =>
  Object.entries(plan.value?.costs ?? active.value?.metadata?.costs ?? {}).map(
    ([key, quantity]) => `${active.value?.cost_labels?.[key] ?? '资源'} × ${quantity}`,
  ),
)
const minutes = computed(() => plan.value?.minutes ?? active.value?.metadata?.minutes ?? 0)
// 忙碌门禁只认服务端租约与本端乐观锁；conversations.current.is_processing
// 可能因终止事件丢失而长期为真，不能参与点击判定。
const gate = computed(() =>
  actionBlock({
    ended: conversations.current?.status !== undefined && conversations.current.status !== 'active',
    connectionBlocked: socket.writeBlocked,
    resultPending: !!pending.value,
    generating: context.activity.is_processing || chat.isGenerating,
    syncing: loading.value || checkingGameplay.value,
  }),
)
// Keep the reason close to the disabled execution button as well as the header.
const disabledHint = computed(() => {
  if (planBlocked.value) return plan.value?.blocked_reason ?? ''
  if (gate.value) return gate.value.message
  return ''
})

function invalidate() {
  dirty = true
  if (!alive || !props.visible || socket.writeBlocked) return
  schedule()
}

function schedule(immediate = false) {
  if (!alive || !props.visible || socket.writeBlocked) return
  if (scheduled) return
  if (immediate) {
    void load()
    return
  }
  scheduled = setTimeout(() => {
    scheduled = null
    if (alive && props.visible && dirty) void load()
  }, 50)
}

async function load() {
  if (!alive || !props.visible || socket.writeBlocked) {
    dirty = true
    return
  }
  if (inFlight) {
    queued = true
    return inFlight
  }
  dirty = false
  const id = props.convId
  const version = generation
  loading.value = true
  const request = (async () => {
    try {
      const next = await moduleActionsApi.get(id)
      if (!alive || version !== generation || id !== props.convId || socket.writeBlocked) return
      snapshot.value = next
      if (!visibleCapabilities.value.some((a) => a.key === selectedKey.value))
        selectedKey.value = visibleCapabilities.value[0]?.key ?? null
    } catch (cause) {
      if (alive && version === generation)
        error.value = axios.isAxiosError(cause)
          ? cause.response?.data?.error || '暂时无法读取行动，请刷新重试。'
          : '读取行动失败。'
    } finally {
      if (alive && version === generation) loading.value = false
    }
  })()
  inFlight = request
  try {
    await request
  } finally {
    if (inFlight === request) inFlight = null
    if (alive && (queued || dirty)) {
      queued = false
      schedule()
    }
  }
}
watch(active, (action) => {
  const availablePlan = action?.plans?.findIndex((p) => !p.blocked_reason) ?? -1
  planIndex.value = action?.plans?.length ? Math.max(availablePlan, 0) : null
  argumentsValue.value = {}
  for (const parameter of action?.parameters ?? []) {
    argumentsValue.value[parameter.key] =
      parameter.type === 'integer'
        ? (parameter.minimum ?? 0)
        : parameter.type === 'boolean'
          ? false
          : (parameter.options?.[0] ?? '')
  }
})
watch(visibleCapabilities, (list) => {
  if (!list.some((a) => a.key === selectedKey.value)) selectedKey.value = list[0]?.key ?? null
})
watch(
  () => props.convId,
  () => {
    generation++
    snapshot.value = null
    selectedKey.value = null
    loading.value = false
    checkingGameplay.value = false
    inFlight = null
    queued = false
    dirty = true
    if (props.visible) schedule(true)
  },
  { immediate: true },
)
watch(
  () => props.visible,
  (visible) => {
    if (visible) {
      dirty = true
      schedule(true)
    }
  },
)
// The same state boundary covers model tools, rewinds and other browser tabs.
// Hidden panels only mark dirty; they do not issue requests.
watch(
  () => state.state,
  () => {
    if (!submitting.value) invalidate()
  },
)
watch(
  () => chat.isGenerating,
  (value) => {
    if (!value) invalidate()
  },
)
watch(
  () => campaign.snapshot?.campaign.revision,
  () => {
    if (!submitting.value) invalidate()
  },
)
onBeforeUnmount(() => {
  alive = false
  generation++
  if (scheduled) clearTimeout(scheduled)
  scheduled = null
})

async function ensureGameplayFresh(id: string) {
  try {
    const info = await gameplay.ensureFresh(id)
    if (!alive || props.convId !== id || socket.writeBlocked) return false
    if (!info.run_allowed) {
      error.value = info.reason || '当前玩法版本已暂停运行，无法提交行动。'
      return false
    }
    return true
  } catch {
    error.value = '玩法版本状态暂时无法读取，请刷新后重试。'
    return false
  }
}

async function execute() {
  if (submitting.value) {
    message.warning('动作正在提交，请稍候。')
    return
  }
  if (!pending.value) {
    const blocked = gate.value
    if (blocked) {
      message.warning(blocked.message)
      return
    }
    if (!snapshot.value || !active.value) {
      message.warning('当前没有可执行行动。')
      return
    }
    if (!active.value.available) {
      message.warning(
        active.value.violations?.map((v) => v.message).join('；') || '当前行动不可执行。',
      )
      return
    }
    if (planBlocked.value) {
      message.warning(plan.value?.blocked_reason || '当前方案不可执行。')
      return
    }
  } else if (socket.writeBlocked) {
    message.warning('连接已中断，请刷新页面后重试。')
    return
  }
  const id = props.convId
  const version = generation
  const input: ModuleActionInput = pending.value ?? {
    request_id: crypto.randomUUID(),
    branch_epoch: snapshot.value!.branch_epoch,
    expected_revision: snapshot.value!.revision,
    key: active.value!.key,
    arguments: { ...(plan.value?.arguments ?? argumentsValue.value) },
  }
  const run = async () => {
    if (checkingGameplay.value || version !== generation || !alive) return
    // 查询上次操作结果不应该被自己的 resultPending 拦下；其他门禁仍生效。
    const blocked = pending.value
      ? actionBlock({ connectionBlocked: socket.writeBlocked })
      : gate.value
    if (blocked) {
      message.warning(blocked.message)
      return
    }
    checkingGameplay.value = true
    try {
      if (!(await ensureGameplayFresh(id))) return
      if (
        version !== generation ||
        props.convId !== id ||
        socket.writeBlocked ||
        chat.isGenerating ||
        context.activity.is_processing
      ) {
        message.warning('状态已变化，请等待当前操作完成后重试。')
        return
      }
      const result = await actions.execute(id, input)
      if (!result) {
        message.warning(actions.error || '操作未提交，请稍后重试。')
        return
      }
      const notice = stageProgressNotice(result.stage_progress)
      if (notice) message.success(`阶段已达成：${notice}，结束时统一结算`)
      if (alive && version === generation && props.convId === id) await load()
    } finally {
      if (version === generation) checkingGameplay.value = false
    }
  }
  if (active.value?.ending && !pending.value) {
    dialog.warning({
      title: `确认${active.value.metadata?.label || active.value.description}`,
      content: [
        active.value.description,
        costs.value.join('，'),
        minutes.value ? `耗时 ${minutes.value} 分钟` : '',
        plan.value?.risk || active.value.metadata?.risk,
        '确认后按当前方案执行；达到结局后无法继续本局。主动终止会结算已达成阶段积分，不额外发放通关积分。',
      ]
        .filter(Boolean)
        .join('\n\n'),
      positiveText: '确认执行',
      negativeText: '取消',
      onPositiveClick: run,
    })
  } else await run()
}
</script>

<template>
  <section class="action-panel" aria-label="玩法行动">
    <div class="panel-heading">
      <strong>行动</strong>
      <span class="heading-tools">
        <span v-if="loading && snapshot" class="refreshing">正在刷新…</span>
        <NButton text size="small" :loading="loading" :disabled="submitting" @click="load"
          >刷新</NButton
        >
      </span>
    </div>
    <p class="hint">先选择行动，查看代价后执行。结果以当前存档为准。</p>
    <NAlert v-if="error" type="warning" :show-icon="false">{{ error }}</NAlert>
    <NButton v-if="pending" block type="primary" :loading="submitting" @click="execute"
      >查询上次操作结果</NButton
    >
    <!-- 首次加载才阻塞展示；后续刷新一律非阻塞，避免遮罩吞掉点击 -->
    <NSpin v-if="!snapshot && loading" class="panel-spin" />
    <template v-else-if="visibleCapabilities.length">
      <label class="field-label">选择行动</label>
      <NSelect
        v-model:value="selectedKey"
        :virtual-scroll="false"
        :disabled="submitting || !!pending"
        :options="
          visibleCapabilities.map((a) => ({
            label: a.metadata?.label || a.description,
            value: a.key,
          }))
        "
      />
      <template v-if="active">
        <p class="hint">{{ active.description }}</p>
        <template v-if="active.plans?.length">
          <label class="field-label">行动方案</label>
          <NSelect
            v-model:value="planIndex"
            :virtual-scroll="false"
            :disabled="submitting || !!pending"
            :options="planOptions"
          />
        </template>
        <template v-else v-for="parameter in active.parameters" :key="parameter.key">
          <label class="field-label">{{ parameter.label }}</label>
          <NInputNumber
            v-if="parameter.type === 'integer'"
            :value="Number(argumentsValue[parameter.key])"
            :min="parameter.minimum"
            :max="parameter.maximum"
            :precision="0"
            :disabled="submitting || !!pending"
            @update:value="
              (value) => (argumentsValue[parameter.key] = value ?? parameter.minimum ?? 0)
            "
          />
          <NSwitch
            v-else-if="parameter.type === 'boolean'"
            :value="Boolean(argumentsValue[parameter.key])"
            :disabled="submitting || !!pending"
            @update:value="(value) => (argumentsValue[parameter.key] = value)"
          />
          <NSelect
            v-else-if="parameter.options?.length"
            :virtual-scroll="false"
            :value="String(argumentsValue[parameter.key])"
            :disabled="submitting || !!pending"
            :options="
              parameter.options.map((value) => ({
                value,
                label: parameter.option_labels?.[value] ?? parameter.label,
              }))
            "
            @update:value="(value) => (argumentsValue[parameter.key] = value)"
          />
          <NInput
            v-else
            :value="String(argumentsValue[parameter.key])"
            :maxlength="128"
            :disabled="submitting || !!pending"
            @update:value="(value) => (argumentsValue[parameter.key] = value)"
          />
        </template>
        <dl class="action-costs">
          <dt>耗时</dt>
          <dd>{{ minutes ? `${minutes} 分钟` : '立即' }}</dd>
          <dt>消耗</dt>
          <dd>{{ costs.join('、') || '无额外消耗' }}</dd>
        </dl>
        <p v-if="plan?.risk || active.metadata?.risk" class="hint">
          {{ plan?.risk || active.metadata?.risk }}
        </p>
        <NAlert v-if="!active.available" type="info" :show-icon="false">{{
          active.violations?.map((v) => v.message).join('；') || '当前不可执行'
        }}</NAlert>
        <NAlert v-if="planBlocked" type="warning" :show-icon="false">{{
          plan?.blocked_reason
        }}</NAlert>
        <NButton
          block
          type="primary"
          :loading="submitting"
          :disabled="!!gate || !active.available || planBlocked || !!pending"
          @click="execute"
          >执行{{ active.metadata?.label || active.description }}</NButton
        >
        <p v-if="disabledHint" class="hint">{{ disabledHint }}</p>
      </template>
    </template>
    <NEmpty
      v-else-if="hiddenOnly"
      description="这些行动已在左侧实体列表提供，请从任务、人物、地点或功法条目上操作"
    />
    <NEmpty
      v-else-if="!loading"
      :description="
        conversations.current?.status === 'ended' ? '本局已结束' : '当前没有可执行的行动'
      "
    />
  </section>
</template>

<style scoped>
.action-panel {
  overflow-y: auto;
  padding: 16px;
  box-sizing: border-box;
  color: var(--text);
}
.panel-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.heading-tools {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}
.refreshing {
  font-size: 12px;
  color: var(--text-muted);
}
.panel-spin {
  display: block;
  min-height: 120px;
}
.hint {
  font-size: 12px;
  line-height: 1.7;
  color: var(--text-muted);
  overflow-wrap: anywhere;
}
.field-label {
  display: block;
  margin: 14px 0 6px;
  font-size: 12px;
}
.action-costs {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 8px 12px;
  padding: 12px;
  background: var(--bg-soft);
  border: 1px solid var(--border);
  border-radius: 8px;
  font-size: 12px;
}
.action-costs dt {
  color: var(--text-muted);
}
.action-costs dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.n-alert,
.n-button--block {
  margin: 12px 0;
}
</style>
