<script setup lang="ts">
import { computed } from 'vue'
import { NProgress, NTag } from 'naive-ui'
import { useRuntimeStore } from '@/stores/runtime'
import { useStateStore } from '@/stores/state'
import { presentValue } from '@/lib/presentation'
import type { StateViewField } from '@/types'

defineProps<{ convId: string }>()
const runtime = useRuntimeStore()
const stateStore = useStateStore()

const sections = computed(() =>
  [...(runtime.presentation?.state_sections ?? [])]
    .sort((left, right) => left.order - right.order)
    .map((section) => ({
      ...section,
      fields: [...section.fields].sort((left, right) => left.order - right.order),
    })),
)

function valueAt(path: string): unknown {
  let current: unknown = stateStore.state
  for (const part of path.split('.')) {
    if (!current || typeof current !== 'object' || Array.isArray(current)) return undefined
    current = (current as Record<string, unknown>)[part]
  }
  return current
}

function hasValue(value: unknown) {
  return value !== undefined && value !== null && value !== ''
}

function format(value: unknown, field: StateViewField) {
  if (!hasValue(value)) return '未记录'
  if (field.widget === 'boolean') return value ? '是' : '否'
  if (Array.isArray(value)) {
    return (
      value
        .map((item) =>
          typeof item === 'string'
            ? presentValue(runtime.presentation, `state.${field.path}`, item)
            : String(item),
        )
        .join('、') || '无'
    )
  }
  if (typeof value === 'object') {
    return (
      Object.values(value as Record<string, unknown>)
        .filter((item) => hasValue(item))
        .map((item) =>
          typeof item === 'string'
            ? presentValue(runtime.presentation, `state.${field.path}`, item)
            : String(item),
        )
        .join('、') || '无'
    )
  }
  const displayed = presentValue(runtime.presentation, `state.${field.path}`, value)
  return `${displayed}${field.unit ? ` ${field.unit}` : ''}`
}

function collectionItems(value: unknown, fieldPath: string): string[] {
  if (!hasValue(value)) return []
  if (Array.isArray(value))
    return value.map((item) => formatCollectionItem(item, fieldPath)).filter(Boolean)
  if (typeof value !== 'object')
    return [presentValue(runtime.presentation, `state.${fieldPath}`, value)]

  const items: string[] = []
  for (const [key, entry] of Object.entries(value as Record<string, unknown>)) {
    if (key === 'items' && Array.isArray(entry)) {
      items.push(...entry.map((item) => formatCollectionItem(item, fieldPath)).filter(Boolean))
      continue
    }
    if (Array.isArray(entry)) {
      if (entry.length) {
        const label = presentValue(runtime.presentation, `state.${fieldPath}`, key, '')
        const text = entry.map((item) => formatCollectionItem(item, fieldPath)).join('、')
        items.push(label ? `${label}：${text}` : text)
      }
      continue
    }
    if (hasValue(entry)) {
      const label = presentValue(runtime.presentation, `state.${fieldPath}`, key, '')
      const text = formatCollectionItem(entry, fieldPath)
      items.push(label ? `${label}：${text}` : text)
    }
  }
  return items
}

function formatCollectionItem(value: unknown, fieldPath: string): string {
  if (!hasValue(value)) return ''
  if (typeof value !== 'object' || Array.isArray(value)) {
    return presentValue(runtime.presentation, `state.${fieldPath}`, value)
  }

  const item = value as Record<string, unknown>
  const name = item.name ?? item.title
  const description = item.description
  const quantity = item.quantity ?? item.count
  if (hasValue(name)) {
    return [
      String(name),
      hasValue(description) ? String(description) : '',
      hasValue(quantity) ? `×${String(quantity)}` : '',
    ]
      .filter(Boolean)
      .join(' · ')
  }
  return Object.entries(item)
    .filter(([, entry]) => hasValue(entry) && typeof entry !== 'object')
    .map(([key, entry]) => {
      const label = presentValue(runtime.presentation, `state.${fieldPath}.${key}`, key, '')
      const displayed = presentValue(runtime.presentation, `state.${fieldPath}`, entry)
      return label ? `${label}：${displayed}` : displayed
    })
    .join(' · ')
}

function progressValue(value: unknown) {
  const number = Number(value)
  return Number.isFinite(number) ? Math.max(0, Math.min(100, number)) : 0
}

// 长文本（外貌/出身等描述段落）右对齐排在标签旁边时无法分辨归属，
// 超过一行宽度就切换为「标签在上、正文在下」的堆叠布局。
function isLongText(field: StateViewField): boolean {
  if (field.widget !== 'text') return false
  const text = format(valueAt(field.path), field)
  return text.length > 16 || text.includes('\n')
}
</script>

<template>
  <aside class="state-panel">
    <div class="head">
      <span class="title">{{ runtime.presentation?.state_panel_title || '状态' }}</span>
    </div>
    <div class="body scroll">
      <div v-if="!sections.length" class="empty-state">
        <div class="empty-text">暂无可展示状态</div>
      </div>
      <section v-for="section in sections" v-else :key="section.key" class="state-section">
        <h3>{{ section.title }}</h3>
        <div
          v-for="field in section.fields"
          :key="field.path"
          class="state-row"
          :class="{
            'state-row-collection': field.widget === 'list' || field.widget === 'item_list',
            'state-row-stacked': isLongText(field),
          }"
        >
          <span class="field-label">{{ field.label }}</span>
          <div v-if="field.widget === 'list' || field.widget === 'item_list'" class="field-list">
            <template v-if="collectionItems(valueAt(field.path), field.path).length">
              <span
                v-for="(item, index) in collectionItems(valueAt(field.path), field.path)"
                :key="`${field.path}-${index}`"
                class="collection-item"
                >{{ item }}</span
              >
            </template>
            <span v-else class="field-empty">无</span>
          </div>
          <NProgress
            v-else-if="field.widget === 'progress'"
            class="field-progress"
            type="line"
            :height="7"
            :percentage="progressValue(valueAt(field.path))"
            :show-indicator="true"
          />
          <NTag v-else-if="field.widget === 'badge'" size="small" :bordered="false">
            {{ format(valueAt(field.path), field) }}
          </NTag>
          <span v-else class="field-value" :class="{ 'field-value-long': isLongText(field) }">{{
            format(valueAt(field.path), field)
          }}</span>
        </div>
      </section>
    </div>
  </aside>
</template>

<style scoped>
.state-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  min-width: 260px;
  max-width: 320px;
  background: var(--bg-panel);
  border-right: 1px solid var(--border);
}

.head {
  flex-shrink: 0;
  padding: 16px 18px;
  background: var(--bg-soft);
  border-bottom: 1px solid var(--border);
}

.title {
  color: var(--text);
  font-size: 14px;
  font-weight: 600;
}

.body {
  flex: 1;
  min-height: 0;
  padding: 8px 16px 20px;
  overflow: auto;
}

.state-section {
  padding: 14px 0;
  border-bottom: 1px solid var(--border-soft);
}

.state-section:last-child {
  border-bottom: 0;
}

.state-section h3 {
  margin: 0 0 10px;
  color: var(--text-faint);
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
}

.state-row {
  display: grid;
  grid-template-columns: minmax(72px, 0.8fr) minmax(0, 1.2fr);
  min-height: 30px;
  align-items: center;
  gap: 10px;
}

.state-row-collection {
  grid-template-columns: 1fr;
  gap: 5px;
  padding: 5px 0;
}

.state-row-stacked {
  grid-template-columns: 1fr;
  gap: 5px;
  padding: 5px 0;
  align-items: start;
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

/* 长文本改为左对齐正文块，左侧竖线与列表项保持同一视觉语言。 */
.field-value-long {
  padding-left: 9px;
  border-left: 2px solid var(--border-strong);
  font-size: 12px;
  line-height: 1.6;
  text-align: left;
  white-space: pre-wrap;
}

.field-progress {
  min-width: 0;
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

.field-empty {
  color: var(--text-faint);
  font-size: 12px;
}

.empty-state {
  display: grid;
  min-height: 180px;
  place-items: center;
  color: var(--text-faint);
  font-size: 13px;
}

@media (max-width: 768px) {
  .state-panel {
    width: 100%;
    min-width: 0;
    max-width: none;
    border-right: 0;
  }

  .head {
    padding: 12px 16px;
  }

  .body {
    padding-bottom: calc(18px + env(safe-area-inset-bottom));
  }
}
</style>
