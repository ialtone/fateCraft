<script setup lang="ts">
import { computed } from 'vue'
import { NCollapse, NCollapseItem } from 'naive-ui'
import type { RollResultMetadata } from '@/types'

const props = defineProps<{ details: RollResultMetadata }>()

const outcome = computed(() => props.details.roll_outcome)
const breakdownRows = computed<Array<[string, number]>>(() => [
  ['基础成功率', outcome.value.breakdown.base],
  ['属性修正', outcome.value.breakdown.attributes],
  ['天赋/特质修正', outcome.value.breakdown.traits],
  ['情境修正', outcome.value.breakdown.context],
  ['收益平衡', outcome.value.breakdown.reward_cap],
  ['历史调整', outcome.value.breakdown.history],
  ['难度模式', outcome.value.breakdown.difficulty_mode],
])
const levelLabel = computed(
  () =>
    ({
      critical_success: '大成功',
      success: '成功',
      failure: '失败',
      critical_failure: '大失败',
    })[outcome.value.level] || '已完成',
)

function modifier(value: number) {
  return value === 0 ? '0%' : `${value > 0 ? '+' : ''}${value}%`
}
</script>

<template>
  <NCollapse
    v-if="details.conditions.length || details.roll_outcome.breakdown"
    class="roll-details"
  >
    <NCollapseItem name="details" title="判定详情">
      <div class="detail-body">
        <section v-if="details.conditions.length" class="detail-section">
          <div class="section-title">前置条件</div>
          <div
            v-for="condition in details.conditions"
            :key="condition.key || condition.label"
            class="condition-row"
          >
            <span class="condition-status" :class="condition.met ? 'met' : 'unmet'">{{
              condition.met ? '✓' : '×'
            }}</span>
            <span class="condition-label">{{ condition.label }}</span>
            <span class="condition-value">
              <template v-if="condition.display_actual">{{ condition.display_actual }}</template>
              <template v-if="condition.display_expected"
                >（{{ condition.display_expected }}）</template
              >
            </span>
          </div>
        </section>

        <section class="detail-section">
          <div class="section-title">成功率分解</div>
          <div
            v-for="row in breakdownRows"
            :key="row[0]"
            v-show="row[1] !== 0 || row[0] === '基础成功率'"
            class="breakdown-row"
          >
            <span>{{ row[0] }}</span>
            <span>{{ row[0] === '基础成功率' ? `${row[1]}%` : modifier(row[1]) }}</span>
          </div>
          <div class="breakdown-row final-row">
            <span>最终成功率</span>
            <span>{{ outcome.breakdown.final }}%</span>
          </div>
        </section>

        <section class="detail-section result-section">
          <div class="section-title">结果区间</div>
          <div class="result-line">
            <span>成功区间（1-100）</span>
            <span
              >{{ outcome.success_range.range_start }} - {{ outcome.success_range.range_end }}</span
            >
          </div>
          <div class="result-line">
            <span>本次投骰</span>
            <span>{{ outcome.rolled }} / {{ outcome.sides }}（{{ levelLabel }}）</span>
          </div>
        </section>
      </div>
    </NCollapseItem>
  </NCollapse>
</template>

<style scoped>
.roll-details {
  margin-top: 8px;
  text-align: left;
  color: var(--text-dim);
}

.roll-details :deep(.n-collapse-item__header) {
  min-height: 28px;
  padding: 4px 8px;
  color: var(--text-dim);
  font-size: 12px;
}

.roll-details :deep(.n-collapse-item__content-inner) {
  padding: 4px 8px 8px;
}

.detail-body {
  display: grid;
  gap: 12px;
}

.detail-section {
  display: grid;
  gap: 4px;
}

.section-title {
  color: var(--text-faint);
  font-size: 11px;
  letter-spacing: 0.5px;
}

.condition-row,
.breakdown-row,
.result-line {
  display: grid;
  grid-template-columns: minmax(92px, 1fr) minmax(0, 1.6fr);
  gap: 8px;
  align-items: baseline;
  font-size: 12px;
}

.condition-row {
  grid-template-columns: 18px minmax(76px, 1fr) minmax(0, 1.5fr);
}

.condition-status.met {
  color: var(--ok);
}

.condition-status.unmet {
  color: var(--danger);
}

.condition-label,
.breakdown-row > span:first-child,
.result-line > span:first-child {
  color: var(--text-faint);
}

.condition-value,
.breakdown-row > span:last-child,
.result-line > span:last-child {
  color: var(--text-dim);
  text-align: right;
  overflow-wrap: anywhere;
}

.breakdown-row.final-row {
  padding-top: 5px;
  border-top: 1px solid var(--border-soft);
  font-weight: 600;
}

.breakdown-row.final-row > span:last-child {
  color: var(--accent);
}

@media (max-width: 480px) {
  .condition-row,
  .breakdown-row,
  .result-line {
    grid-template-columns: minmax(80px, 1fr) minmax(0, 1.2fr);
  }

  .condition-row {
    grid-template-columns: 18px minmax(70px, 1fr) minmax(0, 1.2fr);
  }
}
</style>
