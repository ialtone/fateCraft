<script setup lang="ts">
import { computed, ref } from 'vue'
import { NIcon, NInput } from 'naive-ui'
import { ChevronDownOutline } from '@vicons/ionicons5'

type ComboOption = string | { label: string; value: string }

const props = withDefaults(
  defineProps<{
    modelValue?: string
    options: ComboOption[]
    placeholder?: string
    disabled?: boolean
  }>(),
  {
    modelValue: '',
    placeholder: '可直接输入模型名称',
    disabled: false,
  },
)

const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()

const menuOpen = ref(false)
const showAllOptions = ref(false)

const normalizedOptions = computed(() =>
  props.options.map((option) =>
    typeof option === 'string' ? { label: option, value: option } : option,
  ),
)

const filteredOptions = computed(() => {
  const query = (props.modelValue || '').trim().toLowerCase()
  if (showAllOptions.value || !query) {
    return normalizedOptions.value
  }
  return normalizedOptions.value.filter(
    (option) =>
      option.label.toLowerCase().includes(query) || option.value.toLowerCase().includes(query),
  )
})

function onInput(value: string) {
  if (props.disabled) return
  emit('update:modelValue', value)
  if (props.options.length > 0) {
    showAllOptions.value = false
    menuOpen.value = true
  }
}

function selectOption(value: string) {
  if (props.disabled) return
  emit('update:modelValue', value)
  menuOpen.value = false
  showAllOptions.value = false
}

function toggleMenu() {
  if (props.disabled) return
  if (props.options.length > 0) {
    menuOpen.value = !menuOpen.value
    showAllOptions.value = menuOpen.value
  }
}

function onFocus() {
  if (props.disabled) return
  if (props.options.length > 0) {
    // 聚焦已有值时仍展示完整候选；只有输入事件才进入过滤模式。
    menuOpen.value = true
    showAllOptions.value = true
  }
}

function onFocusOut(event: FocusEvent) {
  const next = event.relatedTarget as Node | null
  if (!next || !(event.currentTarget as HTMLElement).contains(next)) {
    menuOpen.value = false
  }
}
</script>

<template>
  <div class="model-combo" @focusout="onFocusOut">
    <NInput
      :value="modelValue"
      :placeholder="placeholder"
      :disabled="disabled"
      autocomplete="off"
      spellcheck="false"
      @update:value="onInput"
      @focus="onFocus"
      @keydown.esc="menuOpen = false"
    >
      <template v-if="options.length > 0 && !disabled" #suffix>
        <button
          type="button"
          class="combo-toggle"
          :aria-label="menuOpen ? '收起模型列表' : '展开模型列表'"
          @mousedown.prevent.stop
          @click="toggleMenu"
        >
          <NIcon :component="ChevronDownOutline" :class="{ open: menuOpen }" />
        </button>
      </template>
    </NInput>

    <ul v-if="menuOpen && filteredOptions.length > 0" class="combo-menu" role="listbox">
      <li v-for="option in filteredOptions" :key="option.value" role="option">
        <button
          type="button"
          class="combo-item"
          :class="{ active: option.value === modelValue }"
          @mousedown.prevent="selectOption(option.value)"
        >
          {{ option.label }}
        </button>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.model-combo {
  position: relative;
  width: 100%;
  min-width: 0;
}

.model-combo :deep(.n-input) {
  width: 100%;
}

.combo-toggle {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 3px;
  border: 0;
  background: transparent;
  color: var(--text-faint);
  cursor: pointer;
  transition: color 0.15s ease;
}

.combo-toggle:hover {
  color: var(--accent);
}

.combo-toggle :deep(.n-icon) {
  transition: transform 0.15s ease;
}

.combo-toggle :deep(.n-icon.open) {
  transform: rotate(180deg);
}

.combo-menu {
  position: absolute;
  z-index: 20;
  top: calc(100% + 4px);
  right: 0;
  left: 0;
  max-height: 260px;
  margin: 0;
  padding: 4px;
  overflow-y: auto;
  list-style: none;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  box-shadow: var(--shadow-card);
}

.combo-item {
  display: block;
  width: 100%;
  padding: 7px 9px;
  overflow: hidden;
  border: 0;
  border-radius: calc(var(--radius) - 2px);
  background: transparent;
  color: var(--text);
  font: inherit;
  font-size: 13px;
  text-align: left;
  text-overflow: ellipsis;
  white-space: nowrap;
  cursor: pointer;
}

.combo-item:hover {
  background: var(--accent-soft);
}

.combo-item.active {
  background: var(--accent-soft);
  color: var(--accent);
  font-weight: 600;
}
</style>
