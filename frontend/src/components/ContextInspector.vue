<script setup lang="ts">
import { computed } from 'vue'
import { NProgress } from 'naive-ui'
import { useContextStore } from '@/stores/context'
import { useChatStore } from '@/stores/chat'
import { useInquiryStore } from '@/stores/inquiry'

const context = useContextStore()
const chat = useChatStore()
const inquiry = useInquiryStore()
const log = computed(() => context.lastBuild)
const performance = computed(() => context.runtimeInspector?.performance ?? null)
const cache = computed(() => context.runtimeInspector?.cache ?? null)

function formatDuration(ms: number) {
  if (ms < 0) return '—'
  if (ms < 1000) return `${Math.max(0, ms)} ms`
  return `${(ms / 1000).toFixed(ms < 10000 ? 1 : 0)} s`
}

function formatTPS(value: number) {
  if (!Number.isFinite(value) || value <= 0) return '—'
  return value < 100 ? value.toFixed(1) : value.toFixed(0)
}

function formatBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) return '0 MiB'
  return `${(value / 1024 / 1024).toFixed(value < 10 * 1024 * 1024 ? 1 : 0)} MiB`
}

function formatPercent(value: number) {
  if (!Number.isFinite(value) || value <= 0) return '0%'
  return `${Math.round(value * 100)}%`
}

function performanceStatus(status: string) {
  return (
    {
      running: '运行中',
      completed: '已完成',
      partial: '部分完成',
      failed: '失败',
      cancelled: '已取消',
    }[status] ?? status
  )
}

const toolActivityCopy: Record<string, readonly [string, string]> = {
  state: ['正在更新角色状态', '服务端正在校验并提交可信状态'],
  roll: ['正在执行判定', '随机结果与效果由服务端生成'],
  settlement: ['正在结算玩法', '服务端正在复核结局与奖励'],
  action: ['正在执行玩法行动', '领域规则正在提交本次行动'],
  guidance: ['正在整理行动提示', '服务端正在生成当前可选方向'],
}

const activities = computed(() => {
  const items: Array<{ key: string; label: string; detail: string }> = []
  const narrative =
    context.activity.narrative === 'idle' && chat.isGenerating
      ? 'model'
      : context.activity.narrative
  if (narrative === 'preparing') {
    items.push({ key: 'narrative', label: '正在准备叙事', detail: '装配上下文与当前可用能力' })
  } else if (narrative === 'model') {
    items.push({ key: 'narrative', label: '模型正在思考', detail: '等待当前 Agent 步骤完成' })
  } else if (narrative === 'streaming') {
    items.push({ key: 'narrative', label: '正在生成叙事', detail: '文本正在实时写入当前回复' })
  } else if (narrative === 'tool') {
    const tool = toolActivityCopy[context.activity.tool_category || 'action'] ?? [
      '正在执行生成工具',
      '服务端正在处理当前步骤',
    ]
    items.push({ key: 'narrative', label: tool[0], detail: tool[1] })
  }
  if (inquiry.asking) {
    items.push({ key: 'inquiry', label: '正在回答角色询问', detail: '只读查询，不影响叙事流程' })
  }
  if (context.activity.memory === 'running') {
    items.push({ key: 'memory', label: '正在执行记忆归档', detail: '后台整理已完成的历史回合' })
  } else if (context.activity.memory === 'queued') {
    items.push({ key: 'memory', label: '记忆归档等待中', detail: '后台任务已进入处理队列' })
  }
  return items
})

const usagePct = computed(() => {
  if (!log.value || !log.value.max_context_tokens) return 0
  return Math.min(
    100,
    Math.round((log.value.used_prompt_tokens / log.value.max_context_tokens) * 100),
  )
})
</script>

<template>
  <aside class="inspector scroll">
    <div class="head"><span class="orn">◆</span> 回合运行面板</div>
    <div class="section-title first-section">当前活动</div>
    <div class="pad activity-list">
      <div v-for="item in activities" :key="item.key" class="activity-row">
        <span class="activity-dot active"></span>
        <div class="activity-copy">
          <strong>{{ item.label }}</strong>
          <span>{{ item.detail }}</span>
        </div>
      </div>
      <div v-if="!activities.length" class="activity-row idle">
        <span class="activity-dot"></span>
        <div class="activity-copy">
          <strong>当前无运行任务</strong>
          <span>叙事与后台服务均处于空闲状态</span>
        </div>
      </div>
    </div>
    <template v-if="performance">
      <div class="section-title">本回合性能</div>
      <div class="pad performance-panel">
        <div class="performance-head">
          <div>
            <span class="performance-value">
              <span v-if="performance.usage_estimated" class="estimate-mark">≈</span
              >{{ formatTPS(performance.tokens_per_second) }}
            </span>
            <span class="performance-unit">tok/s</span>
          </div>
          <span class="performance-status">{{ performanceStatus(performance.status) }}</span>
        </div>
        <div class="performance-label faint">TPS · 模型输出速度</div>
        <div class="metric-grid">
          <div class="metric-cell">
            <span class="faint">平均首响应</span>
            <strong class="mono">{{
              formatDuration(performance.average_first_response_ms)
            }}</strong>
          </div>
          <div class="metric-cell">
            <span class="faint">模型耗时</span>
            <strong class="mono">{{ formatDuration(performance.model_duration_ms) }}</strong>
          </div>
          <div class="metric-cell">
            <span class="faint">输入 / 输出</span>
            <strong class="mono"
              >{{ performance.prompt_tokens }} / {{ performance.completion_tokens }}</strong
            >
          </div>
          <div class="metric-cell">
            <span class="faint">步骤 / 工具</span>
            <strong class="mono"
              >{{ performance.step_count }} / {{ performance.tool_call_count }}</strong
            >
          </div>
        </div>
        <div class="row spread performance-foot">
          <span class="faint">回合总耗时</span>
          <span class="mono">{{ formatDuration(performance.attempt_duration_ms) }}</span>
        </div>
        <div v-if="performance.usage_estimated" class="estimate-note faint">
          Token 数由服务端估算
        </div>
      </div>
    </template>
    <template v-if="cache">
      <div class="section-title">只读内容缓存</div>
      <div class="pad">
        <div class="metric-grid">
          <div class="metric-cell">
            <span class="faint">命中率</span>
            <strong class="mono">{{ formatPercent(cache.hit_rate) }}</strong>
          </div>
          <div class="metric-cell">
            <span class="faint">版本</span>
            <strong class="mono">{{ cache.entries }} / {{ cache.max_versions }}</strong>
          </div>
          <div class="metric-cell">
            <span class="faint">估算内存</span>
            <strong class="mono">{{ formatBytes(cache.estimated_bytes) }}</strong>
          </div>
          <div class="metric-cell">
            <span class="faint">淘汰 / 失败</span>
            <strong class="mono">{{ cache.evictions }} / {{ cache.load_failures }}</strong>
          </div>
        </div>
      </div>
    </template>
    <div v-if="!log" class="faint pad empty">
      尚无上下文记录。<br />发送一条消息后，这里会显示本次请求的上下文占用情况。
    </div>
    <template v-else>
      <div class="section-title">最近一次模型请求</div>
      <div class="pad">
        <div class="row spread sm">
          <span class="faint">模型</span><span class="mono">{{ log.model }}</span>
        </div>
        <div class="row spread sm">
          <span class="faint">已用 / 上限</span>
          <span class="mono">{{ log.used_prompt_tokens }} / {{ log.max_context_tokens }}</span>
        </div>
        <div class="row spread sm">
          <span class="faint">预留回复</span
          ><span class="mono">{{ log.reserved_response_tokens }}</span>
        </div>
        <NProgress
          type="line"
          :percentage="usagePct"
          :height="6"
          :border-radius="3"
          :show-indicator="false"
          style="margin-top: 10px"
        />
        <div class="pct faint">{{ usagePct }}%</div>
      </div>

      <div class="section-title">上下文组成</div>
      <div class="pad sm">
        <div class="row spread">
          <span class="faint">纳入历史消息</span
          ><span class="mono">{{ log.included_message_count }}</span>
        </div>
        <div class="row spread">
          <span class="faint">激活世界书条目</span
          ><span class="mono">{{ log.activated_world_entry_count }}</span>
        </div>
        <div v-if="log.memory_lag" class="row spread warning-row">
          <span>较早内容</span><span>等待记忆归档</span>
        </div>
      </div>
      <div class="section-title">运行说明</div>
      <div class="pad sm faint">
        <div>回合状态：{{ context.activity.turn_status || 'idle' }}</div>
        <div v-if="context.activity.step_no">当前步骤：第 {{ context.activity.step_no }} 步</div>
        <div v-if="context.activity.tool_category">
          工具类别：{{ context.activity.tool_category }}
        </div>
        <div v-if="context.activity.memory !== 'idle'">记忆归档：{{ context.activity.memory }}</div>
      </div>
    </template>
  </aside>
</template>

<style scoped>
.inspector {
  height: 100%;
  min-height: 0;
  min-width: 0;
  overflow-y: auto;
  background: var(--bg-panel);
  border-left: 1px solid var(--border-soft);
  display: flex;
  flex-direction: column;
}

.head {
  padding: 13px 16px;
  border-bottom: 1px solid var(--border-soft);
  font-weight: 600;
  letter-spacing: 2px;
  font-size: 13.5px;
  flex-shrink: 0;
}

.orn {
  color: var(--accent);
  font-size: 9px;
  margin-right: 4px;
  vertical-align: 2px;
}

.pad {
  padding: 12px 16px;
}

.empty {
  line-height: 1.9;
}

.section-title {
  padding: 7px 16px;
  font-size: 11px;
  letter-spacing: 2px;
  color: var(--text-faint);
  background: var(--bg-soft);
  border-top: 1px solid var(--border-soft);
  border-bottom: 1px solid var(--border-soft);
}

.first-section {
  border-top: 0;
}

.activity-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.activity-row {
  display: grid;
  grid-template-columns: 10px minmax(0, 1fr);
  align-items: start;
  gap: 9px;
}

.activity-copy {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 2px;
}

.activity-copy strong {
  font-size: 12.5px;
  font-weight: 600;
}

.activity-copy span {
  color: var(--text-faint);
  font-size: 11px;
  line-height: 1.45;
}

.activity-dot {
  width: 7px;
  height: 7px;
  margin-top: 5px;
  border-radius: 50%;
  background: var(--ok);
}

.activity-dot.active {
  background: var(--accent);
  box-shadow: 0 0 0 0 color-mix(in srgb, var(--accent) 45%, transparent);
  animation: activity-pulse 1.6s ease-out infinite;
}

.activity-row.idle .activity-copy strong {
  color: var(--text-dim);
}

.warning-row {
  margin-top: 8px;
  color: var(--accent);
}

.performance-panel {
  display: flex;
  flex-direction: column;
}

.performance-head {
  min-height: 32px;
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 12px;
}

.performance-value {
  color: var(--text-main);
  font-size: 24px;
  font-weight: 650;
  line-height: 1;
}

.performance-unit {
  margin-left: 5px;
  color: var(--text-dim);
  font-size: 11px;
}

.performance-status {
  color: var(--ok);
  font-size: 11px;
}

.performance-label {
  margin-top: 4px;
  font-size: 10.5px;
}

.estimate-mark {
  margin-right: 2px;
  color: var(--text-dim);
  font-size: 14px;
  vertical-align: 4px;
}

.metric-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 1px;
  margin-top: 12px;
  background: var(--border-soft);
  border: 1px solid var(--border-soft);
}

.metric-cell {
  min-width: 0;
  min-height: 52px;
  padding: 8px 9px;
  background: var(--bg-panel);
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  gap: 4px;
  font-size: 10.5px;
}

.metric-cell strong {
  overflow-wrap: anywhere;
  font-weight: 600;
}

.performance-foot {
  margin-top: 9px;
  font-size: 11px;
}

.estimate-note {
  margin-top: 4px;
  text-align: right;
  font-size: 10px;
}

@keyframes activity-pulse {
  70% {
    box-shadow: 0 0 0 5px transparent;
  }
  100% {
    box-shadow: 0 0 0 0 transparent;
  }
}

.sm {
  font-size: 12.5px;
}

.sm + .sm {
  margin-top: 4px;
}

.mono {
  font-size: 12px;
}

.pct {
  text-align: right;
  font-size: 11px;
  margin-top: 3px;
}

@media (max-width: 768px) {
  .inspector {
    width: 100%;
    border-left: 0;
    padding-bottom: env(safe-area-inset-bottom);
  }
}
</style>
