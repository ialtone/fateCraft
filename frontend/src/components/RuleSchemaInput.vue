<script setup lang="ts">
import { computed, ref } from 'vue'
import { NButton, NInput, NInputNumber, NSelect, NSwitch } from 'naive-ui'
import type { RuleSchema } from '@/types/module-actions'

const props = defineProps<{ schema: RuleSchema; readonly?: boolean; depth?: number }>()
const model = defineModel<any>({ required: true })
const expanded = ref((props.depth ?? 0) < 2)
const entryKey = ref('')
const properties = computed(() => Object.entries(props.schema.properties ?? {}))
const entries = computed(() =>
  model.value && typeof model.value === 'object' && !Array.isArray(model.value)
    ? Object.entries(model.value)
    : [],
)
const items = computed(() => (Array.isArray(model.value) ? model.value : []))
const childDepth = computed(() => (props.depth ?? 0) + 1)
function initial(schema: RuleSchema): any {
  if (schema.type === 'array') return []
  if (schema.type === 'object')
    return Object.fromEntries(
      Object.entries(schema.properties ?? {}).map(([key, child]) => [key, initial(child)]),
    )
  if (schema.type === 'integer' || schema.type === 'number') return schema.minimum ?? 0
  if (schema.type === 'boolean') return false
  return schema.enum?.[0] ?? ''
}
function set(key: string, value: unknown) {
  model.value = { ...Object.fromEntries(entries.value), [key]: value }
}
function remove(key: string) {
  const next = { ...Object.fromEntries(entries.value) }
  delete next[key]
  model.value = next
}
function addEntry() {
  const key = entryKey.value.trim()
  if (
    !key ||
    entries.value.some(([existing]) => existing === key) ||
    typeof props.schema.additionalProperties !== 'object'
  )
    return
  set(key, initial(props.schema.additionalProperties))
  entryKey.value = ''
}
function updateItem(index: number, value: any) {
  const next = [...items.value]
  next[index] = value
  model.value = next
}
function addItem() {
  if (!props.schema.items) return
  model.value = [...items.value, initial(props.schema.items)]
  expanded.value = true
}
</script>

<template>
  <div class="rule-schema-input">
    <template v-if="schema.type === 'object' && schema.properties">
      <div class="rule-property" v-for="[key, child] in properties" :key="key">
        <label>{{ child.title || key }}</label>
        <RuleSchemaInput
          :model-value="model?.[key]"
          :schema="child"
          :readonly="readonly"
          :depth="childDepth"
          @update:model-value="(value) => set(key, value)"
        />
      </div>
    </template>
    <template
      v-else-if="schema.type === 'object' && typeof schema.additionalProperties === 'object'"
    >
      <div v-for="[key, value] in entries" :key="key" class="map-entry">
        <span class="map-key">{{ key }}</span>
        <RuleSchemaInput
          :model-value="value"
          :schema="schema.additionalProperties"
          :readonly="readonly"
          :depth="childDepth"
          @update:model-value="(value) => set(key, value)"
        />
        <NButton v-if="!readonly" size="tiny" tertiary @click="remove(key)">移除</NButton>
      </div>
      <div v-if="!readonly && entries.length < (schema.maxProperties ?? 64)" class="map-entry">
        <NInput
          v-model:value="entryKey"
          size="small"
          placeholder="资源或对象的标识"
          :maxlength="128"
          @keydown.enter.prevent="addEntry"
        />
        <NButton
          size="small"
          :disabled="!entryKey.trim() || entries.some(([key]) => key === entryKey.trim())"
          @click="addEntry"
          >添加</NButton
        >
      </div>
      <span v-if="readonly && !entries.length" class="empty">未配置</span>
    </template>
    <template v-else-if="schema.type === 'array' && schema.items">
      <NButton text size="small" @click="expanded = !expanded"
        >{{ expanded ? '收起' : '展开' }} {{ items.length }} 项</NButton
      >
      <div v-if="expanded" class="array-items">
        <div class="array-item" v-for="(value, index) in items" :key="index">
          <div class="array-heading">
            <span>{{ value?.name || `第 ${index + 1} 项` }}</span
            ><NButton
              v-if="!readonly"
              size="tiny"
              tertiary
              @click="model = items.filter((_, i) => i !== index)"
              >移除</NButton
            >
          </div>
          <RuleSchemaInput
            :model-value="value"
            :schema="schema.items"
            :readonly="readonly"
            :depth="childDepth"
            @update:model-value="(value) => updateItem(index, value)"
          />
        </div>
      </div>
      <NButton
        v-if="!readonly && items.length < (schema.maxItems ?? 64)"
        size="small"
        secondary
        @click="addItem"
        >新增一项</NButton
      >
    </template>
    <NSelect
      v-else-if="schema.enum"
      :value="model"
      :options="schema.enum.map((value) => ({ label: String(value), value }))"
      :disabled="readonly"
      @update:value="(value) => (model = value)"
    />
    <NInputNumber
      v-else-if="schema.type === 'integer' || schema.type === 'number'"
      :value="typeof model === 'number' ? model : null"
      :min="schema.minimum"
      :max="schema.maximum"
      :precision="schema.type === 'integer' ? 0 : undefined"
      :disabled="readonly"
      @update:value="(value) => (model = value)"
    />
    <NSwitch
      v-else-if="schema.type === 'boolean'"
      :value="model === true"
      :disabled="readonly"
      @update:value="(value) => (model = value)"
    />
    <NInput
      v-else
      :value="typeof model === 'string' ? model : ''"
      :maxlength="schema.maxLength ?? 256"
      :readonly="readonly"
      @update:value="(value) => (model = value)"
    />
  </div>
</template>

<style scoped>
.rule-schema-input {
  width: 100%;
  min-width: 0;
}
.rule-property {
  margin: 8px 0 14px;
}
.rule-property > label {
  display: block;
  margin-bottom: 6px;
  color: var(--text-muted);
  font-size: 12px;
}
.map-entry {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}
.map-key {
  min-width: 90px;
  overflow-wrap: anywhere;
  font-size: 12px;
}
.array-items {
  display: grid;
  gap: 10px;
  margin: 8px 0;
}
.array-item {
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 12px;
  background: var(--bg-soft);
}
.array-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 12px;
}
.empty {
  color: var(--text-faint);
  font-size: 12px;
}
</style>
