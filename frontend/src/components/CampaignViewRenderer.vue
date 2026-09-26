<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { NAlert, NProgress, NSelect, NSkeleton, NTag } from 'naive-ui'
import { useCampaignStore } from '@/stores/campaign'
import { useRuntimeStore } from '@/stores/runtime'
import { presentValue } from '@/lib/presentation'
import GuidanceRenderer from '@/components/GuidanceRenderer.vue'
import type {
  CampaignViewAction,
  CampaignViewCondition,
  CampaignViewField,
  CampaignViewItemField,
  CampaignViewSection,
  GuidanceGroup,
  GuidanceItem,
} from '@/types'

const campaign = useCampaignStore()
const runtime = useRuntimeStore()
const snapshot = computed<Record<string, unknown> | null>(() => {
  return campaign.snapshot ? (campaign.snapshot as unknown as Record<string, unknown>) : null
})
const view = computed(() => runtime.presentation?.campaign_view)
const activeSection = ref('')
// client 路由终局不发放奖励：后端快照带 rewards_disabled_reason，前端显示替代文案。
const rewardsDisabledReason = computed(() => {
  const value = snapshot.value?.['rewards_disabled_reason']
  return typeof value === 'string' && value ? value : ''
})

const sections = computed(() =>
  [...(view.value?.sections ?? [])]
    .filter((section) => conditionMatches(section.visible_when))
    .sort((left, right) => left.order - right.order)
    .map((section) => ({
      ...section,
      fields: [...section.fields]
        .filter((field) => conditionMatches(field.visible_when))
        .sort((left, right) => left.order - right.order),
    })),
)

const sectionOptions = computed(() =>
  sections.value.map((section) => ({ label: section.title, value: section.key })),
)

watch(
  sectionOptions,
  (options) => {
    if (!options.some((option) => option.value === activeSection.value)) {
      activeSection.value = options[0]?.value || ''
    }
  },
  { immediate: true },
)

function read(path: string, item?: unknown): unknown {
  if (!path) return undefined
  if (path.startsWith('item.')) return readRelative(path.slice(5), item)
  if (path.startsWith('option.') || path.startsWith('input.')) return undefined
  let current: unknown = snapshot.value
  if (path.startsWith('snapshot.')) path = path.slice(9)
  for (const part of path.split('.')) {
    if (!current || typeof current !== 'object' || Array.isArray(current)) return undefined
    current = (current as Record<string, unknown>)[part]
  }
  return current
}

function readRelative(path: string, item: unknown): unknown {
  let current = item
  for (const part of path.split('.')) {
    if (!current || typeof current !== 'object' || Array.isArray(current)) return undefined
    current = (current as Record<string, unknown>)[part]
  }
  return current
}

function conditionMatches(
  condition?: CampaignViewCondition,
  item?: unknown,
  parent?: unknown,
): boolean {
  if (!condition) return true
  const actual =
    condition.path.startsWith('item.') || condition.path.startsWith('option.')
      ? readRelative(condition.path.slice(condition.path.indexOf('.') + 1), item)
      : condition.path.startsWith('parent.')
        ? readRelative(condition.path.slice('parent.'.length), parent)
        : read(condition.path)
  if (condition.exists !== undefined)
    return condition.exists === (actual !== undefined && actual !== null)
  if (condition.equals !== undefined) return String(actual) === String(condition.equals)
  if (condition.not_equals !== undefined) return String(actual) !== String(condition.not_equals)
  if (condition.operator && condition.value !== undefined) {
    const number = Number(actual)
    const expected = condition.value
    if (!Number.isFinite(number)) return false
    if (condition.operator === 'gte') return number >= expected
    if (condition.operator === 'gt') return number > expected
    if (condition.operator === 'lte') return number <= expected
    if (condition.operator === 'lt') return number < expected
    return number === expected
  }
  return false
}

function hasValue(value: unknown): boolean {
  return value !== undefined && value !== null && value !== ''
}

function formatTime(value: unknown): string {
  const minutes = Number(value)
  if (!Number.isFinite(minutes)) return '未记录'
  const days = Math.floor(minutes / 1440)
  const hours = Math.floor((minutes % 1440) / 60)
  const remainder = minutes % 60
  if (days) return `${days} 天 ${hours} 时`
  if (hours) return remainder ? `${hours} 时 ${remainder} 分` : `${hours} 时`
  return `${remainder} 分`
}

function headerTitle(value: unknown): string {
  if (!hasValue(value)) return ''
  const text = String(value)
  return /^[a-z][a-z0-9._:-]*$/i.test(text) ? '' : text
}

function scalar(
  value: unknown,
  field: CampaignViewField | CampaignViewItemField,
  fallback = '未记录',
): string {
  if (!hasValue(value)) return fallback
  if (field.value_namespace)
    return presentValue(runtime.presentation, field.value_namespace, value, fallback)
  if (typeof value === 'boolean') return value ? '是' : '否'
  if (typeof value === 'number') return `${value}${field.unit ? ` ${field.unit}` : ''}`
  if (typeof value === 'object') return fallback
  const text = String(value)
  // Technical stable keys are only rendered when the Runtime supplied a label.
  if (/^[a-z][a-z0-9._:-]*$/i.test(text)) return fallback
  return `${text}${field.unit ? ` ${field.unit}` : ''}`
}

function progress(value: unknown): number {
  const number = Number(value)
  return Number.isFinite(number) ? Math.max(0, Math.min(100, Math.round(number))) : 0
}

function collection(value: unknown, field: CampaignViewField): unknown[] {
  let items: unknown[]
  if (Array.isArray(value)) items = [...value]
  else if (value && typeof value === 'object') items = Object.values(value)
  else return []
  if (field.sort) {
    const descending = field.sort.endsWith('desc') || field.sort === 'desc'
    const byOrder = field.sort.startsWith('order')
    items.sort((left, right) => {
      const l = left && typeof left === 'object' ? (left as Record<string, unknown>) : {}
      const r = right && typeof right === 'object' ? (right as Record<string, unknown>) : {}
      const leftValue = byOrder ? l.order : (l.name ?? l.label ?? l.title ?? String(left))
      const rightValue = byOrder ? r.order : (r.name ?? r.label ?? r.title ?? String(right))
      const result = String(leftValue).localeCompare(String(rightValue), 'zh-Hans')
      return descending ? -result : result
    })
  }
  return field.limit ? items.slice(0, field.limit) : items
}

function formatItem(value: unknown, field: CampaignViewField): string {
  if (!hasValue(value)) return ''
  if (!field.item_fields?.length) {
    if (typeof value === 'object' && value !== null && !Array.isArray(value)) {
      const object = value as Record<string, unknown>
      return [
        object.name,
        object.title,
        object.label,
        object.summary,
        object.description,
        object.quantity ? `×${object.quantity}` : '',
      ]
        .filter(hasValue)
        .map((item) => {
          const text = String(item)
          return /^[a-z][a-z0-9._:-]*$/i.test(text) ? '' : text
        })
        .filter(Boolean)
        .join(' · ')
    }
    return field.value_namespace
      ? presentValue(runtime.presentation, field.value_namespace, value, '')
      : /^[a-z][a-z0-9._:-]*$/i.test(String(value))
        ? ''
        : String(value)
  }
  const object = value as Record<string, unknown>
  return field.item_fields
    .map((itemField) => {
      const itemValue = readRelative(itemField.path, object)
      if (!hasValue(itemValue)) return ''
      return `${itemField.label}：${formatItemFieldValue(itemValue, itemField)}`
    })
    .filter(Boolean)
    .join(' · ')
}

function formatItemFieldValue(value: unknown, field: CampaignViewItemField): string {
  if (Array.isArray(value)) {
    return value
      .map((item) => {
        if (!item || typeof item !== 'object') return scalar(item, field, '')
        const object = item as Record<string, unknown>
        if (field.item_fields?.length) {
          return field.item_fields
            .map((nested) => {
              const nestedValue = readRelative(nested.path, object)
              return hasValue(nestedValue)
                ? `${nested.label}：${formatItemFieldValue(nestedValue, nested)}`
                : ''
            })
            .filter(Boolean)
            .join(' · ')
        }
        return [object.name, object.title, object.label, object.summary, object.description]
          .filter(hasValue)
          .map(String)
          .filter((text) => !/^[a-z][a-z0-9._:-]*$/i.test(text))
          .join(' · ')
      })
      .filter(Boolean)
      .join('、')
  }
  if (value && typeof value === 'object') {
    const object = value as Record<string, unknown>
    if (field.item_fields?.length) {
      return field.item_fields
        .map((nested) => {
          const nestedValue = readRelative(nested.path, object)
          return hasValue(nestedValue)
            ? `${nested.label}：${formatItemFieldValue(nestedValue, nested)}`
            : ''
        })
        .filter(Boolean)
        .join(' · ')
    }
    const text = [object.name, object.title, object.label, object.summary, object.description]
      .filter(hasValue)
      .map(String)
      .filter((item) => !/^[a-z][a-z0-9._:-]*$/i.test(item))
      .join(' · ')
    return text || '未记录'
  }
  return scalar(value, field)
}

function actionList(section: CampaignViewSection): CampaignViewAction[] {
  const defaultPlacement = sections.value[0]?.key
  return (view.value?.actions ?? []).filter((action) =>
    action.placement ? action.placement === section.key : section.key === defaultPlacement,
  )
}

function actionItems(action: CampaignViewAction): Array<{ item?: unknown; parent?: unknown }> {
  if (!action.source_path) return [{ item: undefined }]
  const value = read(action.source_path)
  const parents = Array.isArray(value)
    ? value
    : value && typeof value === 'object'
      ? Object.values(value)
      : []
  if (!action.item_path) return parents.map((item) => ({ item }))
  return parents.flatMap((parent) => {
    const child = readRelative(action.item_path!, parent)
    return Array.isArray(child) ? child.map((item) => ({ item, parent })) : []
  })
}

function actionLabel(action: CampaignViewAction, item?: unknown): string {
  if (!item || typeof item !== 'object') return action.label
  const name = (item as Record<string, unknown>).name
  if (!name) return action.label
  const text = String(name)
  return /^[a-z][a-z0-9._:-]*$/i.test(text) ? action.label : `${action.label} · ${text}`
}

function actionArguments(
  action: CampaignViewAction,
  context: { item?: unknown; parent?: unknown },
): Record<string, unknown> {
  const args: Record<string, unknown> = {}
  for (const [name, binding] of Object.entries(action.arguments ?? {})) {
    if (binding.from) {
      if (binding.from.startsWith('item.') || binding.from.startsWith('option.')) {
        args[name] = readRelative(binding.from.slice(binding.from.indexOf('.') + 1), context.item)
      } else if (binding.from.startsWith('parent.')) {
        args[name] = readRelative(binding.from.slice('parent.'.length), context.parent)
      } else {
        args[name] = read(binding.from)
      }
    } else if (binding.value !== undefined) {
      args[name] = binding.value
    }
  }
  return args
}

// Legacy campaign_view.actions are adapted to the same renderer used by
// ordinary conversation guidance.  Arguments are resolved against the current
// public snapshot before rendering; the server still revalidates every action.
function guidanceGroupsForSection(section: CampaignViewSection): GuidanceGroup[] {
  const items: GuidanceItem[] = []
  for (const action of actionList(section)) {
    for (const [index, context] of actionItems(action).entries()) {
      if (!conditionMatches(action.visible_when, context.item, context.parent)) continue
      const resolved = actionArguments(action, context)
      const bindings = Object.fromEntries(
        Object.entries(resolved).map(([name, value]) => [name, { value }]),
      )
      items.push({
        key: `${action.key}:${index}`,
        label: actionLabel(action, context.item),
        mode: 'action',
        action_key: action.key,
        arguments: bindings,
        enabled: conditionMatches(action.enabled_when, context.item, context.parent),
      })
    }
  }
  if (!items.length) return []
  return [
    {
      key: `campaign-actions:${section.key}`,
      title: '行动',
      placement: 'campaign_panel',
      items,
    },
  ]
}

function runGuidanceAction(item: GuidanceItem, args: Record<string, unknown>) {
  if (item.mode !== 'action' || !item.action_key) return
  campaign.execute(item.action_key, args).catch(() => {})
}

function emptyText(section: CampaignViewSection): string {
  return view.value?.empty_states?.[section.key] || '暂无可展示内容'
}
</script>

<template>
  <aside class="campaign-panel">
    <header class="head">
      <div>
        <span class="eyebrow">{{
          view?.title || runtime.presentation?.state_panel_title || '视图'
        }}</span>
        <strong>{{
          headerTitle(view?.header?.title_path ? read(view.header.title_path) : '')
        }}</strong>
      </div>
      <span v-if="snapshot && view?.header?.time_path" class="clock">
        {{
          view.header.time_format === 'game_time'
            ? formatTime(read(view.header.time_path))
            : scalar(read(view.header.time_path), { path: '', label: '', widget: 'text', order: 0 })
        }}
      </span>
    </header>
    <div class="body scroll">
      <NAlert v-if="campaign.error" type="error" :bordered="false">{{ campaign.error }}</NAlert>
      <NAlert v-if="rewardsDisabledReason" type="warning" :bordered="false">
        客户端直连模式不发放奖励
      </NAlert>
      <template v-if="campaign.loading && !snapshot">
        <NSkeleton v-for="index in 5" :key="index" text :repeat="2" />
      </template>
      <div v-else-if="!view" class="empty">暂无可展示视图</div>
      <template v-else-if="snapshot">
        <div v-if="sectionOptions.length > 1" class="section-navigation">
          <NSelect
            v-model:value="activeSection"
            :options="sectionOptions"
            size="small"
            aria-label="视图信息"
          />
        </div>
        <div v-if="!sections.length" class="empty">暂无可展示内容</div>
        <section
          v-for="section in sections"
          v-show="activeSection === section.key"
          :key="section.key"
          class="section"
        >
          <h3>{{ section.title }}</h3>
          <template v-for="field in section.fields" :key="field.path">
            <div
              v-if="field.widget === 'list' || field.widget === 'item_list'"
              class="collection-field"
            >
              <span class="field-label">{{ field.label }}</span>
              <div v-if="collection(read(field.path), field).length" class="field-list">
                <span
                  v-for="(item, index) in collection(read(field.path), field)"
                  :key="`${field.path}-${index}`"
                  class="collection-item"
                >
                  {{ formatItem(item, field) }}
                </span>
              </div>
              <span v-else class="field-empty">{{ emptyText(section) }}</span>
            </div>
            <div v-else class="field-row">
              <span class="field-label">{{ field.label }}</span>
              <NProgress
                v-if="field.widget === 'progress'"
                type="line"
                :percentage="progress(read(field.path))"
                :show-indicator="true"
              />
              <NTag v-else-if="field.widget === 'badge'" size="small" :bordered="false">{{
                scalar(read(field.path), field)
              }}</NTag>
              <span v-else class="field-value">{{ scalar(read(field.path), field) }}</span>
            </div>
          </template>
          <GuidanceRenderer
            :groups="guidanceGroupsForSection(section)"
            :snapshot="snapshot"
            placement="campaign_panel"
            :disabled="campaign.acting || campaign.loading"
            :loading="campaign.acting"
            @action="runGuidanceAction"
          />
          <div
            v-if="
              !section.fields.some(
                (field) => hasValue(read(field.path)) || collection(read(field.path), field).length,
              ) && !actionList(section).length
            "
            class="empty"
          >
            {{ emptyText(section) }}
          </div>
        </section>
      </template>
    </div>
  </aside>
</template>

<style scoped>
.campaign-panel {
  display: flex;
  width: 100%;
  min-width: 260px;
  max-width: 320px;
  height: 100%;
  min-height: 0;
  flex-direction: column;
  background: var(--bg-panel);
  border-right: 1px solid var(--border);
}
.head {
  display: flex;
  min-height: 58px;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 10px 16px;
  background: var(--bg-soft);
  border-bottom: 1px solid var(--border);
}
.head > div {
  display: grid;
  gap: 2px;
  min-width: 0;
}
.head strong {
  overflow: hidden;
  font-size: 14px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.eyebrow,
.clock {
  color: var(--text-faint);
  font-size: 11px;
}
.body {
  flex: 1;
  min-height: 0;
  padding: 0 14px 18px;
  overflow: auto;
}
.section-navigation {
  position: sticky;
  z-index: 3;
  top: 0;
  margin: 0 -14px;
  padding: 10px 14px 8px;
  background: color-mix(in srgb, var(--bg-panel) 94%, transparent);
  border-bottom: 1px solid var(--border-soft);
  backdrop-filter: blur(8px);
}
.section-navigation :deep(.n-base-selection) {
  background: var(--bg-soft);
}
.section {
  padding: 13px 0;
  border-bottom: 1px solid var(--border-soft);
}
.section h3 {
  margin: 0 0 9px;
  color: var(--text-faint);
  font-size: 11px;
  font-weight: 600;
}
.field-row {
  display: grid;
  grid-template-columns: minmax(72px, 0.8fr) minmax(0, 1.2fr);
  min-height: 30px;
  align-items: center;
  gap: 10px;
}
.field-label {
  color: var(--text-dim);
  font-size: 12px;
}
.field-value {
  min-width: 0;
  overflow-wrap: anywhere;
  color: var(--text);
  font-size: 13px;
  text-align: right;
}
.field-row :deep(.n-progress) {
  min-width: 0;
}
.collection-field {
  display: grid;
  gap: 5px;
  padding: 5px 0;
}
.field-list {
  display: grid;
  gap: 5px;
}
.collection-item {
  padding-left: 9px;
  border-left: 2px solid var(--border-strong);
  overflow-wrap: anywhere;
  color: var(--text);
  font-size: 12px;
  line-height: 1.5;
}
.field-empty,
.empty {
  color: var(--text-faint);
  font-size: 12px;
}
.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 10px;
}
.empty {
  display: grid;
  min-height: 160px;
  place-items: center;
}
@media (max-width: 768px) {
  .campaign-panel {
    min-width: 0;
    max-width: none;
    border-right: 0;
  }
}
</style>
