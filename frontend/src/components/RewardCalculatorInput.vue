<script setup lang="ts">
import { computed, watch } from 'vue'
import { NAlert, NFormItem, NInput, NInputNumber, NSwitch } from 'naive-ui'
import JsonValueInput from './JsonValueInput.vue'
const props = defineProps<{ calculator: string; readonly?: boolean }>()
const model = defineModel<Record<string, unknown>>({ required: true })
type Field = { key: string; label: string; type: 'text' | 'number' | 'boolean'; optional?: boolean }
const configurations: Record<string, Field[]> = {
  'core.fixed': [{ key: 'amount', label: '固定数量', type: 'number' }],
  'core.state_multiplier': [
    { key: 'state_path', label: '可信状态路径', type: 'text' },
    { key: 'multiplier_bps', label: '倍率（10000 = 1 倍）', type: 'number' },
    { key: 'max_amount', label: '数量上限（可选）', type: 'number', optional: true },
  ],
  'campaign.extract_item_stack': [
    { key: 'item_key', label: '局内物品 Key', type: 'text' },
    { key: 'quality_key', label: '品质 Key', type: 'text' },
    { key: 'source_quantity', label: '每批来源物数量', type: 'number' },
    { key: 'reward_quantity', label: '每批奖励数量', type: 'number' },
    { key: 'maximum_reward_quantity', label: '最大奖励数量（每批奖励的整数倍）', type: 'number' },
    { key: 'consume', label: '扣除实际兑换的来源物', type: 'boolean' },
  ],
}
const fields = computed(() => configurations[props.calculator])
function set(key: string, value: unknown) {
  const next = { ...model.value }
  if (value === null || value === '') delete next[key]
  else next[key] = value
  model.value = next
}
watch(
  () => props.calculator,
  (key) => {
    if (props.readonly) return
    model.value =
      key === 'core.fixed'
        ? { amount: 1 }
        : key === 'core.state_multiplier'
          ? { state_path: '', multiplier_bps: 10000 }
          : key === 'campaign.extract_item_stack'
            ? {
                item_key: '',
                quality_key: 'common',
                source_quantity: 1,
                reward_quantity: 1,
                maximum_reward_quantity: 1,
                consume: true,
              }
            : {}
  },
)
</script>
<template>
  <div v-if="fields" class="calculator-fields">
    <NFormItem v-for="field in fields" :key="field.key" :label="field.label"
      ><NInput
        v-if="field.type === 'text'"
        :value="String(model[field.key] ?? '')"
        :disabled="readonly"
        @update:value="set(field.key, $event)" /><NSwitch
        v-else-if="field.type === 'boolean'"
        :value="!!model[field.key]"
        :disabled="readonly"
        @update:value="set(field.key, $event)" /><NInputNumber
        v-else
        :value="typeof model[field.key] === 'number' ? Number(model[field.key]) : null"
        :min="1"
        :max="Number.MAX_SAFE_INTEGER"
        :precision="0"
        :clearable="field.optional"
        :disabled="readonly"
        @update:value="set(field.key, $event)"
    /></NFormItem>
  </div>
  <template v-else
    ><NAlert type="info">此计算器尚无专用参数表单，参数仍须通过后端校验。</NAlert
    ><JsonValueInput v-model="model" :readonly="readonly"
  /></template>
</template>
<style scoped>
.calculator-fields {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 12px;
}
</style>
