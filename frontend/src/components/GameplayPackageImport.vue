<script setup lang="ts">
import { outcomeRewardBudgets } from '@/lib/reward-budget'
import { ref, watch } from 'vue'
import { NAlert, NButton, NCheckbox, NEmpty, NInput, NSelect } from 'naive-ui'
import { gameplayAdminApi as api, gameplayError, type GameplayImportRecord } from '@/api/gameplay'
const props = defineProps<{ mode: 'upload' | 'records' }>()
const emit = defineEmits<{ imported: []; openVersion: [id: string] }>()
const file = ref<File>(),
  strategy = ref('update'),
  target = ref(''),
  trusted = ref(false)
const preview = ref<GameplayImportRecord>(),
  records = ref<GameplayImportRecord[]>([])
const busy = ref(false),
  error = ref(''),
  notice = ref(''),
  request = ref(''),
  cursor = ref('')
const options = [
  { label: '导入新包或新版本', value: 'update' },
  { label: '替换同号草稿', value: 'replace_draft' },
  { label: '另存为独立玩法包', value: 'fork' },
]
function invalidate() {
  if (!request.value) {
    preview.value = undefined
    notice.value = ''
    error.value = ''
  }
}
watch([strategy, target, trusted], invalidate)
function choose(event: Event) {
  file.value = (event.target as HTMLInputElement).files?.[0]
  invalidate()
}
async function upload() {
  if (!file.value || busy.value) return
  if (file.value.size > 16 * 1024 * 1024) {
    error.value = '文件超过 16 MiB'
    return
  }
  busy.value = true
  error.value = ''
  notice.value = ''
  try {
    if (preview.value?.status === 'preview') await api.cancel(preview.value.id)
    preview.value = undefined
    preview.value = await api.preview(file.value, strategy.value, target.value, trusted.value)
  } catch (err) {
    error.value = gameplayError(err)
  } finally {
    busy.value = false
  }
}
async function confirm() {
  if (!preview.value || busy.value) return
  busy.value = true
  error.value = ''
  request.value ||= preview.value.id
  try {
    const row = await api.confirm(preview.value.id, request.value)
    preview.value = row
    request.value = ''
    notice.value = '导入已完成。请检查版本内容，发布后再选择新开局版本与启用状态。'
    emit('imported')
  } catch (err) {
    try {
      const row = await api.record(preview.value.id)
      if (row.status === 'imported' && row.request_id === request.value) {
        preview.value = row
        request.value = ''
        notice.value = '已确认导入成功'
        emit('imported')
      } else {
        error.value = gameplayError(err)
        if (
          row.status !== 'preview' ||
          (((err as { response?: { status?: number } }).response?.status || 0) >= 400 &&
            ((err as { response?: { status?: number } }).response?.status || 0) < 500)
        )
          request.value = ''
      }
    } catch {
      error.value = `${gameplayError(err)}；结果未确认，请重试同次导入。`
    }
  } finally {
    busy.value = false
  }
}
async function confirmRecord(row: GameplayImportRecord) {
  if (request.value && preview.value?.id !== row.id) return
  preview.value = row
  await confirm()
}
async function load(more = false) {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    const page = await api.records(more ? cursor.value : undefined)
    records.value = more ? [...records.value, ...page.imports] : page.imports
    cursor.value = page.next_cursor
  } catch (err) {
    error.value = gameplayError(err)
  } finally {
    busy.value = false
  }
}
watch(
  () => props.mode,
  (mode) => {
    if (mode === 'records') void load()
  },
  { immediate: true },
)
</script>
<template>
  <section class="package-import">
    <NAlert v-if="error" type="error">{{ error }}</NAlert>
    <NAlert v-if="notice" type="success">{{ notice }}</NAlert>
    <template v-if="mode === 'upload'">
      <p>上传 JSON 或 ZIP（最多 16 MiB）。导入为草稿，第三方新包默认未开放。</p>
      <input
        type="file"
        accept=".json,.zip"
        :disabled="busy || !!request"
        aria-label="选择玩法包"
        @change="choose"
      />
      <NSelect v-model:value="strategy" :options="options" :disabled="busy || !!request" />
      <NInput
        v-if="strategy === 'fork'"
        v-model:value="target"
        placeholder="新的玩法包标识，例如 harbor-escape"
        :disabled="busy || !!request"
      />
      <NCheckbox v-if="strategy !== 'fork'" v-model:checked="trusted" :disabled="busy || !!request"
        >明确确认上传来源可信，可更新已有官方或编辑器玩法包（仍不可覆盖已发布版本）</NCheckbox
      >
      <NButton :disabled="!file || !!request" :loading="busy" @click="upload">解析并预览</NButton>
      <article v-if="preview">
        <h3>{{ preview.preview.manifest.name }} · {{ preview.preview.manifest.version }}</h3>
        <p>
          {{ preview.target_slug }} · {{ preview.preview.engine.key }}@{{
            preview.preview.engine.version
          }}
          · {{ preview.preview.mode }}
        </p>
        <p>
          当前入口：{{ preview.preview.current?.version || '无' }}；预览有效至
          {{ new Date(preview.expires_at).toLocaleString() }}
        </p>
        <p v-if="preview.preview.references">
          当前版本：{{ preview.preview.references.active_conversations }} 个进行中对话、{{
            preview.preview.references.active_campaigns
          }}
          个进行中长期存档。它们继续使用原版。
        </p>
        <p v-if="!preview.preview.integrity_declared">源文件未声明内容哈希，已按实际内容计算。</p>
        <p class="hash">内容哈希：{{ preview.content_hash }}</p>
        <h4>结局积分</h4>
        <div v-for="rule in preview.preview.outcomes" :key="rule.outcome_key">
          <p>{{ rule.title }} · {{ rule.reward_points }} 积分</p>
          <p
            v-for="(budget, index) in outcomeRewardBudgets(
              preview.preview.definitions,
              preview.preview.definition_schemas,
              rule.outcome_key,
              rule.reward_points,
            )"
            :key="index"
          >
            {{ budget.text }}
          </p>
        </div>
        <h4>完整内容差异</h4>
        <details v-for="diff in preview.preview.differences" :key="diff.path">
          <summary>{{ diff.path }}</summary>
          <p>原内容</p>
          <pre>{{ JSON.stringify(diff.before, null, 2) }}</pre>
          <p>导入内容</p>
          <pre>{{ JSON.stringify(diff.after, null, 2) }}</pre>
        </details>
        <p v-if="request">导入请求 {{ request }}：结果尚未确认，重试将沿用同一请求。</p>
        <NButton
          v-if="preview.status === 'preview'"
          type="primary"
          :loading="busy"
          @click="confirm"
          >{{ request ? '重试同次导入' : '确认导入草稿' }}</NButton
        >
        <NButton
          v-if="preview.result_version_id"
          @click="emit('openVersion', preview.result_version_id)"
          >查看导入版本</NButton
        >
      </article>
    </template>
    <template v-else>
      <NButton :loading="busy" @click="load()">刷新导入记录</NButton>
      <NEmpty v-if="!busy && !records.length" description="暂无导入记录" />
      <article v-for="row in records" :key="row.id">
        <strong>{{ row.preview.manifest.name }} · {{ row.preview.manifest.version }}</strong>
        <p>
          {{ row.file_name }} · {{ row.status }} · {{ new Date(row.created_at).toLocaleString() }}
        </p>
        <NButton v-if="row.status === 'preview'" :disabled="busy" @click="confirmRecord(row)"
          >确认导入草稿</NButton
        ><NButton v-if="row.result_version_id" @click="emit('openVersion', row.result_version_id)"
          >查看版本</NButton
        >
        <details>
          <summary>导入详情</summary>
          <pre>{{ JSON.stringify(row, null, 2) }}</pre>
        </details>
      </article>
      <NButton v-if="cursor" :loading="busy" @click="load(true)">加载更多</NButton>
    </template>
  </section>
</template>
<style scoped>
.package-import {
  display: grid;
  gap: 14px;
  max-width: 100%;
}
article {
  padding: 16px 0;
  border-bottom: 1px solid var(--border-soft);
}
pre {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  max-height: 480px;
  overflow: auto;
}
.hash {
  overflow-wrap: anywhere;
}
p {
  color: var(--text-dim);
}
</style>
