<script setup lang="ts">
import { computed, h, ref, watch } from 'vue'
import { outcomeRewardBudgets } from '@/lib/reward-budget'
import { useRouter } from 'vue-router'
import {
  NAlert,
  NButton,
  NEmpty,
  NInput,
  NModal,
  NSelect,
  NTag,
  useDialog,
  useMessage,
} from 'naive-ui'
import { adminApi } from '@/api'
import { gameplayAdminApi as api, gameplayError, type VersionReferences } from '@/api/gameplay'
import type { AdminChallengePack, ChallengeVersion } from '@/types'
const props = defineProps<{ packs: AdminChallengePack[]; packId?: string }>()
const emit = defineEmits<{ changed: []; edit: [id: string]; create: [packId: string] }>()
const dialog = useDialog(),
  message = useMessage()
const router = useRouter()
const selected = ref(''),
  busy = ref(''),
  error = ref(''),
  policyOpen = ref(false),
  reason = ref('')
const target = ref<ChallengeVersion>(),
  refs = ref<VersionReferences>()
const pack = computed(() => props.packs.find((p) => p.pack.id === selected.value))
watch(
  () => props.packId,
  (id) => {
    if (id) selected.value = id
  },
  { immediate: true },
)
watch(
  () => props.packs,
  (rows) => {
    if (!rows.some((p) => p.pack.id === selected.value)) selected.value = rows[0]?.pack.id || ''
  },
  { immediate: true },
)
const options = computed(() =>
  props.packs.map((p) => ({ label: `${p.pack.name} · ${p.pack.slug}`, value: p.pack.id })),
)
const status = (v: ChallengeVersion) =>
  v.status === 'draft' ? '草稿' : v.status === 'retired' ? '供旧局继续' : '已发布'
async function download(v: ChallengeVersion, format: 'json' | 'zip') {
  if (busy.value) return
  busy.value = v.id
  error.value = ''
  try {
    await api.download(v.id, `${pack.value?.pack.slug}-${v.version}`, format)
  } catch (err) {
    error.value = gameplayError(err)
  } finally {
    busy.value = ''
  }
}
async function current(v: ChallengeVersion) {
  if (!pack.value || busy.value) return
  const p = pack.value.pack
  busy.value = v.id
  error.value = ''
  try {
    const [doc, reference, old, oldReferences, modules] = await Promise.all([
      adminApi.getChallengeVersion(v.id),
      api.references(v.id),
      p.current_version_id
        ? adminApi.getChallengeVersion(p.current_version_id)
        : Promise.resolve(undefined),
      p.current_version_id ? api.references(p.current_version_id) : Promise.resolve(undefined),
      adminApi.listGameplayModules(),
    ])
    const summary = (snapshot: typeof doc | undefined) => {
      if (!snapshot) return '无'
      const module = modules.find(
        (m) =>
          m.key === snapshot.runtime.engine_key && m.version === snapshot.runtime.engine_version,
      )
      return (
        snapshot.outcomes
          .map(
            (o) =>
              `${o.title}：终局 ${o.reward_points} 积分；${outcomeRewardBudgets(
                snapshot.definitions,
                module?.definition_schemas || {},
                o.outcome_key,
                o.reward_points,
              )
                .map((b) => b.text)
                .join('；')}`,
          )
          .join('；') || '无终局积分'
      )
    }
    const content = (snapshot: typeof doc | undefined): Record<string, unknown> => {
      if (!snapshot) return {}
      const omitDB = (row: Record<string, unknown>) =>
        Object.fromEntries(
          Object.entries(row).filter(
            ([k]) => !['id', 'challenge_version_id', 'created_at', 'updated_at'].includes(k),
          ),
        )
      return {
        manifest: snapshot.version.manifest,
        runtime: omitDB(snapshot.runtime as unknown as Record<string, unknown>),
        assets: snapshot.assets.map((a) => omitDB(a as unknown as Record<string, unknown>)),
        definitions: snapshot.definitions.map((a) =>
          omitDB(a as unknown as Record<string, unknown>),
        ),
        outcomes: snapshot.outcomes.map((a) => omitDB(a as unknown as Record<string, unknown>)),
      }
    }
    const before = content(old),
      after = content(doc)
    const differences = Object.keys(after).filter(
      (key) => JSON.stringify(before[key]) !== JSON.stringify(after[key]),
    )
    dialog.warning({
      title: `将 ${v.version} 设为新开局版本？`,
      style: 'width:min(900px,94vw)',
      content: () =>
        h('div', { style: 'max-height:65vh;overflow:auto' }, [
          h(
            'p',
            `当前：${old?.version.version || '无'} → ${v.version}。原版有 ${oldReferences?.active_conversations || 0} 个进行中对话、${oldReferences?.active_campaigns || 0} 个长期存档；目标有 ${reference.active_conversations} 个进行中对话、${reference.active_campaigns} 个长期存档。已有游戏按原版本继续，积分不回滚，启用状态保持不变。`,
          ),
          h('p', `原奖励：${summary(old)}`),
          h('p', `目标奖励：${summary(doc)}`),
          ...differences.map((key) =>
            h('details', [
              h('summary', `${key} 内容差异`),
              h(
                'pre',
                { style: 'white-space:pre-wrap;overflow-wrap:anywhere' },
                JSON.stringify({ before: before[key], after: after[key] }, null, 2),
              ),
            ]),
          ),
        ]),
      positiveText: '确认切换（旧版目标视为回退）',
      negativeText: '取消',
      onPositiveClick: async () => {
        busy.value = v.id
        try {
          await api.current(p.id, v.id, p.management_revision, true)
          message.success('新开局版本已切换')
          emit('changed')
        } catch (err) {
          error.value = gameplayError(err)
          return false
        } finally {
          busy.value = ''
        }
      },
    })
  } catch (err) {
    error.value = gameplayError(err)
  } finally {
    busy.value = ''
  }
}
async function pause(v: ChallengeVersion) {
  if (busy.value) return
  busy.value = v.id
  error.value = ''
  try {
    refs.value = await api.references(v.id)
    target.value = v
    reason.value = ''
    policyOpen.value = true
  } catch (err) {
    error.value = gameplayError(err)
  } finally {
    busy.value = ''
  }
}
async function policy() {
  const v = target.value
  if (!v || busy.value) return
  const next = v.run_policy === 'blocked' ? 'allowed' : 'blocked'
  if (next === 'blocked' && !reason.value.trim()) return
  busy.value = v.id
  error.value = ''
  try {
    await api.policy(v.id, next, reason.value.trim(), v.policy_revision)
    policyOpen.value = false
    message.success(next === 'blocked' ? '该版本运行已暂停' : '运行暂停已解除')
    emit('changed')
  } catch (err) {
    error.value = gameplayError(err)
  } finally {
    busy.value = ''
  }
}
async function retire(v: ChallengeVersion) {
  dialog.warning({
    title: `退役 ${v.version}？`,
    content: '旧局仍可继续，已有结算和奖励保留。当前入口必须先切换后才能退役。',
    positiveText: '退役',
    negativeText: '取消',
    onPositiveClick: async () => {
      busy.value = v.id
      error.value = ''
      try {
        await api.retire(v.id, v.policy_revision)
        message.success('版本已退役')
        emit('changed')
      } catch (err) {
        error.value = gameplayError(err)
        return false
      } finally {
        busy.value = ''
      }
    },
  })
}
async function showReferences(v: ChallengeVersion) {
  busy.value = v.id
  error.value = ''
  try {
    const r = await api.references(v.id)
    dialog.info({
      title: `${v.version} 引用情况`,
      content: `对话 ${r.conversations}（进行中 ${r.active_conversations}），长期存档 ${r.campaigns}（进行中 ${r.active_campaigns}），生成中 ${r.generating}，奖励流水 ${r.rewards}。对话数量包含归档及软删除记录。`,
    })
  } catch (err) {
    error.value = gameplayError(err)
  } finally {
    busy.value = ''
  }
}
</script>
<template>
  <section class="version-manager">
    <NAlert v-if="error" type="error">{{ error }}</NAlert>
    <NSelect
      v-model:value="selected"
      :options="options"
      placeholder="选择玩法包"
      :disabled="!!busy"
    />
    <template v-if="pack">
      <p>
        {{ pack.pack.name }} · 来源 {{ pack.pack.source }} ·
        {{ pack.pack.status === 'active' ? '新开局已启用' : '新开局未开放' }}
      </p>
      <NButton :disabled="!!busy" @click="emit('create', pack.pack.id)"
        >复制当前入口为新版草稿</NButton
      >
      <NEmpty v-if="!pack.versions.length" description="暂无版本，请创建草稿" />
      <article v-for="version in pack.versions" :key="version.id">
        <div class="title">
          <strong>{{ version.version }}</strong
          ><NTag>{{ status(version) }}</NTag
          ><NTag v-if="pack.pack.current_version_id === version.id" type="success">当前新开局</NTag
          ><NTag v-if="version.run_policy === 'blocked'" type="error">运行已暂停</NTag>
        </div>
        <p v-if="version.run_policy_reason">暂停说明：{{ version.run_policy_reason }}</p>
        <p class="hash">{{ version.content_hash || '草稿尚未发布' }}</p>
        <div class="actions">
          <NButton size="small" :disabled="!!busy" @click="emit('edit', version.id)">{{
            version.status === 'draft' ? '编辑草稿' : '查看快照'
          }}</NButton>
          <NButton size="small" :disabled="!!busy" @click="download(version, 'json')"
            >导出 JSON</NButton
          ><NButton size="small" :disabled="!!busy" @click="download(version, 'zip')"
            >导出 ZIP</NButton
          >
          <NButton size="small" :disabled="!!busy" @click="showReferences(version)"
            >引用情况</NButton
          >
          <NButton
            size="small"
            :disabled="!!busy"
            @click="
              router.push({
                path: '/admin/rewards',
                query: { pack: pack.pack.id, version: version.id },
              })
            "
            >奖励记录</NButton
          >
          <NButton
            v-if="version.status !== 'draft' && pack.pack.current_version_id !== version.id"
            size="small"
            :disabled="!!busy || version.run_policy === 'blocked'"
            @click="current(version)"
            >设为新开局版本 / 回退</NButton
          >
          <NButton
            v-if="version.status === 'published' && pack.pack.current_version_id !== version.id"
            size="small"
            :disabled="!!busy"
            @click="retire(version)"
            >退役</NButton
          >
          <NButton
            v-if="version.status !== 'draft'"
            size="small"
            :type="version.run_policy === 'blocked' ? 'warning' : 'error'"
            :disabled="!!busy"
            @click="pause(version)"
            >{{ version.run_policy === 'blocked' ? '解除运行暂停' : '紧急暂停运行' }}</NButton
          >
        </div>
      </article>
    </template>
    <NModal
      v-model:show="policyOpen"
      preset="card"
      title="版本运行策略"
      :mask-closable="!busy"
      :closable="!busy"
      :close-on-esc="!busy"
      style="width: min(680px, 94vw)"
      content-style="max-height: calc(100vh - 160px); max-height: calc(100dvh - 160px); overflow: auto"
    >
      <NAlert v-if="error" type="error">{{ error }}</NAlert>
      <p>
        {{ target?.version }}：{{ refs?.active_conversations }} 个进行中对话，{{
          refs?.active_campaigns
        }}
        个进行中长期存档，{{ refs?.generating }} 个生成任务。
      </p>
      <p>
        暂停会阻止该版本的新动作和生成提交，已提交动作、结局和积分保留。解除暂停不会复活已结束、归档或删除的游戏。
      </p>
      <NInput
        v-model:value="reason"
        type="textarea"
        placeholder="填写暂停原因（暂停时必填）"
        :maxlength="2000"
        :disabled="!!busy"
      />
      <NButton
        :loading="!!busy"
        :disabled="target?.run_policy !== 'blocked' && !reason.trim()"
        type="primary"
        @click="policy"
        >确认{{ target?.run_policy === 'blocked' ? '解除暂停' : '暂停运行' }}</NButton
      >
    </NModal>
  </section>
</template>
<style scoped>
.version-manager {
  display: grid;
  gap: 14px;
}
article {
  border-bottom: 1px solid var(--border-soft);
  padding: 16px 0;
}
.actions,
.title {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
}
.hash {
  overflow-wrap: anywhere;
  font-size: 12px;
}
p {
  color: var(--text-dim);
}
</style>
