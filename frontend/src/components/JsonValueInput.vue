<script setup lang="ts">
import { ref, watch } from 'vue'
import { NInput } from 'naive-ui'

const props = withDefaults(
  defineProps<{
    modelValue: unknown
    readonly?: boolean
    minRows?: number
    maxRows?: number
  }>(),
  { readonly: false, minRows: 2, maxRows: 8 },
)
const emit = defineEmits<{ 'update:modelValue': [value: unknown] }>()

const text = ref(format(props.modelValue))
const error = ref('')
const focused = ref(false)

watch(
  () => props.modelValue,
  (value) => {
    if (!focused.value) text.value = format(value)
  },
  { deep: true },
)

function format(value: unknown) {
  return JSON.stringify(value ?? {}, null, 2)
}

function commit() {
  focused.value = false
  try {
    const parsed = JSON.parse(text.value)
    error.value = ''
    emit('update:modelValue', parsed)
    text.value = format(parsed)
  } catch {
    error.value = 'JSON 格式无效'
  }
}
</script>

<template>
  <div class="json-value-input">
    <NInput
      v-model:value="text"
      type="textarea"
      :autosize="{ minRows, maxRows }"
      :readonly="readonly"
      class="json-textarea"
      @focus="focused = true"
      @blur="commit"
    />
    <span v-if="error" class="json-error">{{ error }}</span>
  </div>
</template>

<style scoped>
.json-value-input {
  min-width: 0;
}

.json-textarea :deep(textarea) {
  font-size: 12px;
}

.json-error {
  display: block;
  margin-top: 4px;
  color: var(--error, #d03050);
  font-size: 12px;
}
</style>
