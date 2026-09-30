<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import {
  NAlert,
  NButton,
  NCard,
  NDatePicker,
  NEmpty,
  NInput,
  NSelect,
  NSpace,
  NSpin,
  NTabPane,
  NTabs,
  NTag,
  useMessage,
} from 'naive-ui'
import { adminApi, type DiagnosticQuery, type DiagnosticStatus } from '@/api'
import type { AdminLLMRequestLogSummary, AdminContextBuildLogSummary, AdminAuditLog } from '@/types'

const emit = defineEmits<{
  'open-llm': [row: AdminLLMRequestLogSummary]
  'open-context': [row: AdminContextBuildLogSummary]
}>()
const message = useMessage()
const tab = ref('llm')
const busy = ref(false)
const error = ref('')
const statusError = ref('')
const status = ref<DiagnosticStatus | null>(null)
const range = ref<[number, number] | null>(null)
const filters = reactive({
  conversation: '',
  attempt: '',
  trace: '',
  model: '',
  purpose: null as string | null,
  success: null as string | null,
  actor: '',
  action: '',
})
const models = ref<AdminLLMRequestLogSummary[]>([])
const contexts = ref<AdminContextBuildLogSummary[]>([])
const audits = ref<AdminAuditLog[]>([])
const cursor = ref('')
const more = ref(false)
let applied: DiagnosticQuery = {}
let sequence = 0
const purposeOptions = [
  { label: '叙事', value: 'narrative' },
  { label: '记忆', value: 'memory' },
  { label: '角色问答', value: 'character_inquiry' },
  { label: '能力诊断', value: 'protocol_test' },
]
const resultOptions = [
  { label: '成功', value: 'true' },
  { label: '失败', value: 'false' },
]
const bodyLabels: Record<string, string> = {
  disabled: '未采集正文',
  captured: '正文已采集',
  truncated: '正文已截断',
  invalid: '异常正文预览',
}
const actionLabels: Record<string, string> = {
  system_settings_change: '全局设置变更',
  auth_settings_change: '认证设置变更',
  role_change: '角色变更',
  status_change: '账号状态变更',
  installation_complete: '首次安装',
}
const totalDropped = computed(() =>
  Object.values(status.value?.writer.dropped || {}).reduce((sum, value) => sum + value, 0),
)
const empty = computed(() =>
  tab.value === 'llm'
    ? !models.value.length
    : tab.value === 'context'
      ? !contexts.value.length
      : !audits.value.length,
)
const time = (value?: string) => (value ? new Date(value).toLocaleString() : '尚无记录')
const mib = (value: number) => (value / 1048576).toFixed(2)
function failure(err: unknown): string {
  const value = err as { response?: { data?: { error?: string } }; message?: string }
  return value.response?.data?.error || value.message || '读取失败，请重试'
}
function query(): DiagnosticQuery {
  const q: DiagnosticQuery = { limit: 50 }
  if (range.value) {
    q.from = new Date(range.value[0]).toISOString()
    q.until = new Date(range.value[1]).toISOString()
  }
  if (tab.value === 'audit') {
    q.actor_id = filters.actor.trim() || undefined
    q.action = filters.action.trim() || undefined
  } else {
    q.conversation_id = filters.conversation.trim() || undefined
    q.attempt_id = filters.attempt.trim() || undefined
  }
  if (tab.value === 'llm') {
    q.trace_id = filters.trace.trim() || undefined
    q.model = filters.model.trim() || undefined
    q.purpose = filters.purpose || undefined
    q.success = filters.success === null ? undefined : filters.success === 'true'
  }
  return q
}
async function load(append = false) {
  const request = ++sequence
  busy.value = true
  error.value = ''
  if (!append) {
    applied = query()
    cursor.value = ''
    more.value = false
    models.value = []
    contexts.value = []
    audits.value = []
  }
  const params = { ...applied, cursor: append ? cursor.value : undefined }
  try {
    if (tab.value === 'llm') {
      const page = await adminApi.listLLMRequestLogs(params)
      if (request !== sequence) return
      models.value = append ? [...models.value, ...page.logs] : page.logs
      cursor.value = page.next_cursor
      more.value = page.has_more
    } else if (tab.value === 'context') {
      const page = await adminApi.listContextBuildLogs(params)
      if (request !== sequence) return
      contexts.value = append ? [...contexts.value, ...page.logs] : page.logs
      cursor.value = page.next_cursor
      more.value = page.has_more
    } else if (tab.value === 'audit') {
      const page = await adminApi.listAuditLogs(params)
      if (request !== sequence) return
      audits.value = append ? [...audits.value, ...page.logs] : page.logs
      cursor.value = page.next_cursor
      more.value = page.has_more
    }
  } catch (err) {
    if (request === sequence) error.value = failure(err)
  } finally {
    if (request === sequence) busy.value = false
  }
}
async function loadStatus() {
  statusError.value = ''
  try {
    status.value = await adminApi.getDiagnosticStatus()
  } catch (err) {
    statusError.value = failure(err)
  }
}
async function copy(value?: string) {
  if (!value) return
  try {
    await navigator.clipboard.writeText(value)
    message.success('已复制关联 ID')
  } catch {
    message.error('无法复制，请手动选择关联 ID')
  }
}
watch(tab, () => {
  ++sequence
  busy.value = false
  if (tab.value === 'status') void loadStatus()
  else void load()
})
onMounted(() => {
  void load()
  void loadStatus()
})
</script>

<template>
  <section class="diagnostics">
    <NAlert type="info" :show-icon="false"
      >模型调用默认保留结果摘要；详细正文仅在全局设置中开启且匹配采集范围时保存。诊断日志可能按保留策略清理，奖励与管理审计独立保留。</NAlert
    >
    <NTabs v-model:value="tab" type="line">
      <NTabPane name="llm" tab="模型调用" />
      <NTabPane name="context" tab="上下文构建" />
      <NTabPane name="audit" tab="管理审计" />
      <NTabPane name="status" tab="诊断状态" />
    </NTabs>
    <template v-if="tab !== 'status'">
      <form class="filters" @submit.prevent="load()">
        <NDatePicker
          v-model:value="range"
          type="datetimerange"
          clearable
          start-placeholder="开始时间"
          end-placeholder="结束时间（不含）"
        />
        <template v-if="tab !== 'audit'">
          <NInput
            v-model:value="filters.conversation"
            aria-label="对话 ID"
            placeholder="对话 ID"
            clearable
          />
          <NInput
            v-model:value="filters.attempt"
            aria-label="生成尝试 ID"
            placeholder="生成尝试 ID"
            clearable
          />
        </template>
        <template v-if="tab === 'llm'">
          <NInput
            v-model:value="filters.trace"
            aria-label="关联 ID"
            placeholder="关联 ID"
            clearable
          />
          <NInput
            v-model:value="filters.model"
            aria-label="模型名称"
            placeholder="模型名称（精确匹配）"
            clearable
          />
          <NSelect
            v-model:value="filters.purpose"
            :options="purposeOptions"
            placeholder="全部用途"
            clearable
          />
          <NSelect
            v-model:value="filters.success"
            :options="resultOptions"
            placeholder="全部结果"
            clearable
          />
        </template>
        <template v-if="tab === 'audit'">
          <NInput
            v-model:value="filters.actor"
            aria-label="操作者 ID"
            placeholder="操作者 ID"
            clearable
          />
          <NInput
            v-model:value="filters.action"
            aria-label="动作类型"
            placeholder="动作类型，如 system_settings_change"
            clearable
          />
        </template>
        <NButton attr-type="submit" type="primary" :loading="busy">查询</NButton>
      </form>
      <NAlert v-if="error" type="error">{{ error }}</NAlert>
      <NSpin :show="busy">
        <NEmpty
          v-if="empty && !error && !busy"
          description="没有符合筛选条件的记录；较早的诊断可能已按策略清理。"
        />
        <div class="records">
          <template v-if="tab === 'llm'">
            <NCard v-for="row in models" :key="row.id" size="small">
              <div class="record-head">
                <strong>{{ row.model }}</strong
                ><NTag :type="row.success ? 'success' : 'error'">{{
                  row.success ? '成功' : '失败'
                }}</NTag
                ><span>{{ bodyLabels[row.body_status] || row.body_status }}</span>
              </div>
              <p>
                {{ time(row.created_at) }} · {{ row.purpose }} · {{ row.duration_ms }} ms · 网络尝试
                {{ row.transport_attempts }} 次
              </p>
              <p class="identifier">
                关联 {{ row.trace_id || '无' }} · 对话 {{ row.conversation_id || '无' }}
              </p>
              <NAlert v-if="row.error" type="error" :show-icon="false">{{ row.error }}</NAlert>
              <NSpace
                ><NButton size="small" @click="emit('open-llm', row)">查看详情</NButton
                ><NButton size="small" :disabled="!row.trace_id" @click="copy(row.trace_id)"
                  >复制关联 ID</NButton
                ></NSpace
              >
            </NCard>
          </template>
          <template v-if="tab === 'context'">
            <NCard v-for="row in contexts" :key="row.id" size="small">
              <div class="record-head">
                <strong>{{ row.model }}</strong
                ><NTag :type="row.memory_lag ? 'warning' : 'success'">{{
                  row.memory_lag ? '记忆滞后' : '记忆正常'
                }}</NTag>
              </div>
              <p>
                {{ time(row.created_at) }} · {{ row.used_prompt_tokens }} /
                {{ row.max_context_tokens }} token
              </p>
              <p class="identifier">
                对话 {{ row.conversation_id }} · 尝试 {{ row.attempt_id || '无' }}
              </p>
              <NButton size="small" @click="emit('open-context', row)">查看预算与裁剪</NButton>
            </NCard>
          </template>
          <template v-if="tab === 'audit'">
            <NCard v-for="row in audits" :key="row.id" size="small">
              <strong>{{ actionLabels[row.action] || row.action }}</strong>
              <p>{{ time(row.created_at) }} · {{ row.result }}</p>
              <p class="identifier">操作者 {{ row.actor_user_id || '系统初始化' }}</p>
              <p class="identifier">关联 {{ row.trace_id || '无' }}</p>
              <NButton v-if="row.trace_id" size="small" @click="copy(row.trace_id)"
                >复制关联 ID</NButton
              >
              <pre>{{ JSON.stringify(row.metadata, null, 2) }}</pre>
            </NCard>
          </template>
        </div>
        <NButton v-if="more" class="more" :loading="busy" @click="load(true)">加载更多</NButton>
      </NSpin>
    </template>
    <template v-else>
      <NButton @click="loadStatus">刷新状态</NButton>
      <NAlert v-if="statusError" type="error">{{ statusError }}</NAlert>
      <template v-if="status">
        <p class="identifier">
          当前实例：{{ status.instance_id }}。队列统计在进程重启后归零，多副本数据不在此汇总。
        </p>
        <NCard title="模型诊断写入">
          <dl>
            <dt>待处理（含正在写入）</dt>
            <dd>{{ status.writer.pending }} / {{ status.writer.capacity }} 条</dd>
            <dt>载荷占用</dt>
            <dd>{{ mib(status.writer.bytes) }} / {{ mib(status.writer.byte_limit) }} MiB</dd>
            <dt>已写入 / 写入失败 / 丢弃</dt>
            <dd>{{ status.writer.written }} / {{ status.writer.failed }} / {{ totalDropped }}</dd>
            <dt>最近失败</dt>
            <dd>{{ time(status.writer.last_failure_at) }}</dd>
            <dt>关闭时未排空</dt>
            <dd>{{ status.writer.unflushed }}</dd>
          </dl>
          <p v-for="(count, reason) in status.writer.dropped" :key="reason">
            {{ reason }}：{{ count }} 条
          </p>
          <NAlert v-if="totalDropped || status.writer.failed" type="warning"
            >部分诊断未保存。缺少日志不能作为业务未执行的依据，请结合奖励记录和业务状态核对。</NAlert
          >
        </NCard>
        <NCard title="诊断清理">
          <dl>
            <dt>最近执行</dt>
            <dd>{{ time(status.cleanup.last_run) }}</dd>
            <dt>删除模型 / 上下文记录</dt>
            <dd>{{ status.cleanup.llm_requests }} / {{ status.cleanup.context_builds }}</dd>
            <dt>耗时</dt>
            <dd>{{ status.cleanup.duration_ms }} ms</dd>
            <dt>过期积压</dt>
            <dd>
              {{
                !status.cleanup.backlog_known
                  ? '尚未确认'
                  : status.cleanup.has_more
                    ? '仍有积压，将继续分批清理'
                    : '已处理完'
              }}
            </dd>
          </dl>
          <NAlert v-if="status.cleanup.error" type="error">{{ status.cleanup.error }}</NAlert>
        </NCard>
      </template>
    </template>
  </section>
</template>

<style scoped>
.diagnostics {
  display: grid;
  gap: 16px;
}
.filters {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 12px;
}
.records {
  display: grid;
  gap: 12px;
}
.record-head {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
}
.identifier,
pre {
  overflow-wrap: anywhere;
  word-break: break-word;
}
pre {
  white-space: pre-wrap;
  max-height: 280px;
  overflow: auto;
}
p {
  color: var(--text-secondary);
}
dl {
  display: grid;
  grid-template-columns: minmax(120px, 1fr) 2fr;
  gap: 10px;
}
dd {
  margin: 0;
}
.more {
  margin-top: 16px;
}
@media (max-width: 600px) {
  .filters {
    grid-template-columns: 1fr;
  }
  dl {
    grid-template-columns: 1fr;
  }
}
</style>
