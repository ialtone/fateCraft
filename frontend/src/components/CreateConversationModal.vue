<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import {
  NButton,
  NForm,
  NFormItem,
  NIcon,
  NInput,
  NModal,
  NPopover,
  NSelect,
  NSwitch,
  useMessage,
} from 'naive-ui'
import { HelpCircleOutline } from '@vicons/ionicons5'
import { useConversationStore } from '@/stores/conversations'
import { useTemplateStore } from '@/stores/templates'
import { usePromptStore } from '@/stores/prompts'
import { useWorldBookStore } from '@/stores/worldbooks'
import { usePackDefaults, type CreateConversationForm } from '@/composables/usePackDefaults'
import type { Conversation, CreateConversationInput } from '@/types'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{
  (e: 'update:show', value: boolean): void
  (e: 'created', conv: Conversation): void
}>()

const message = useMessage()
const convStore = useConversationStore()
const templates = useTemplateStore()
const prompts = usePromptStore()
const worldbooks = useWorldBookStore()

const isCreating = ref(false)

// 表单状态由本组件持有，预设包联动交给 usePackDefaults
const form = ref<CreateConversationForm>({
  title: '',
  character_id: '',
  persona_id: '',
  scenario_id: '',
  prompt_preset_id: '',
  world_book_id: '',
  roll_system_enabled: false,
  roll_difficulty_mode: 'normal',
})

const {
  currentPackId,
  selectedPack,
  filteredCharacters,
  filteredPersonas,
  filteredScenarios,
  canCreate,
  isWorldBookLocked,
  resetFormToDefaults,
} = usePackDefaults(form)

// 每次打开弹窗时重置表单并应用预设包默认值（原 openCreate 语义）
watch(
  () => props.show,
  (show) => {
    if (show) resetFormToDefaults()
  },
)

// 世界书选项（带描述和图标）
const worldBookOptions = computed(() => {
  return [
    { label: '（无 - 自由组合）', value: '' },
    ...worldbooks.books.map((book) => ({
      label: book.name,
      value: book.id,
      // 可以在这里添加 render 函数来自定义显示
    })),
  ]
})

const toOption = (list: { id: string; name: string }[], emptyLabel: string) => [
  { label: emptyLabel, value: '' },
  ...list.map((x) => ({ label: x.name, value: x.id })),
]

// 获取描述信息的辅助函数
const getDescription = (
  type: 'character' | 'persona' | 'scenario' | 'prompt' | 'worldbook',
  id: string,
) => {
  if (!id) return ''
  switch (type) {
    case 'character':
      return templates.characters.find((c) => c.id === id)?.description || ''
    case 'persona':
      return templates.personas.find((p) => p.id === id)?.description || ''
    case 'scenario':
      return templates.scenarios.find((s) => s.id === id)?.description || ''
    case 'prompt':
      return prompts.presets.find((p) => p.id === id)?.description || ''
    case 'worldbook':
      return worldbooks.books.find((b) => b.id === id)?.description || ''
    default:
      return ''
  }
}

// 角色卡/人设/场景/Prompt 四个选择项的统一配置：
// filteredXxx 在无预设包时本就回退全量列表，因此有/无预设包共用同一段模板
type ResourceKey = 'character_id' | 'persona_id' | 'scenario_id' | 'prompt_preset_id'
interface ResourceField {
  key: ResourceKey
  label: string
  descType: 'character' | 'persona' | 'scenario' | 'prompt'
  options: { label: string; value: string }[]
}

const resourceFields = computed<ResourceField[]>(() => [
  {
    key: 'character_id',
    label: '角色卡',
    descType: 'character',
    options: toOption(filteredCharacters.value, '（无）'),
  },
  {
    key: 'persona_id',
    label: '玩家人设',
    descType: 'persona',
    options: toOption(filteredPersonas.value, '（无）'),
  },
  {
    key: 'scenario_id',
    label: '场景',
    descType: 'scenario',
    options: toOption(filteredScenarios.value, '（无）'),
  },
  {
    key: 'prompt_preset_id',
    label: 'Prompt 预设',
    descType: 'prompt',
    options: toOption(prompts.presets, '（默认）'),
  },
])

async function create() {
  isCreating.value = true
  try {
    const body: CreateConversationInput = { title: form.value.title || '新的对话' }
    if (form.value.character_id) body.character_id = form.value.character_id
    if (form.value.persona_id) body.persona_id = form.value.persona_id
    if (form.value.scenario_id) body.scenario_id = form.value.scenario_id
    if (form.value.prompt_preset_id) body.prompt_preset_id = form.value.prompt_preset_id
    if (form.value.world_book_id) body.world_book_ids = [form.value.world_book_id] // 转为数组给后端

    // 提交判定系统配置（对话级）
    body.roll_system_enabled = form.value.roll_system_enabled
    body.roll_difficulty_mode = form.value.roll_difficulty_mode

    const conv = await convStore.create(body)
    emit('update:show', false)
    // 创建成功交由父组件跳转到对话页
    emit('created', conv)
  } catch (err: any) {
    message.error(`创建对话失败：${err.message || '未知错误'}`)
  } finally {
    isCreating.value = false
  }
}
</script>

<template>
  <NModal
    :show="props.show"
    preset="card"
    title="新建对话"
    style="width: 520px; max-width: 92vw"
    @update:show="(v: boolean) => emit('update:show', v)"
  >
    <NForm label-placement="top" size="small">
      <NFormItem label="标题">
        <NInput v-model:value="form.title" placeholder="新的对话" />
      </NFormItem>

      <!-- 第一步：选择世界观（预设包） -->
      <NFormItem label="世界观预设">
        <NSelect
          v-model:value="form.world_book_id"
          :options="worldBookOptions"
          :disabled="isWorldBookLocked"
          placeholder="选择世界观或自由组合"
        >
          <template #empty>
            <div style="padding: 12px; text-align: center; color: var(--text-faint)">
              暂无世界书
            </div>
          </template>
        </NSelect>
        <template #feedback>
          <div
            v-if="currentPackId && selectedPack"
            style="margin-top: 8px; font-size: 12px; color: var(--text-faint)"
          >
            {{ selectedPack.description }}
            <span v-if="isWorldBookLocked" style="color: var(--warning)">（必需）</span>
          </div>
          <div v-else style="margin-top: 8px; font-size: 12px; color: var(--text-faint)">
            选择世界观后，系统将自动推荐相关的角色卡、人设等配置。选择"无"则可自由组合各种资源。
          </div>
        </template>
      </NFormItem>

      <div v-if="currentPackId" class="divider">
        <span>预设包配置</span>
      </div>

      <!-- 角色卡/人设/场景/Prompt 预设（有预设包时显示筛选后的资源，无预设包时显示全部） -->
      <NFormItem v-for="field in resourceFields" :key="field.key">
        <template #label>
          <div class="form-label-with-help">
            <span>{{ field.label }}</span>
            <NPopover
              v-if="form[field.key] && getDescription(field.descType, form[field.key])"
              trigger="hover"
              placement="right"
            >
              <template #trigger>
                <NIcon :component="HelpCircleOutline" class="help-icon" />
              </template>
              <div class="help-popover">{{ getDescription(field.descType, form[field.key]) }}</div>
            </NPopover>
          </div>
        </template>
        <NSelect v-model:value="form[field.key]" :options="field.options" />
      </NFormItem>

      <!-- 判定系统配置（仅预设包下显示；预设包强制配置时禁用） -->
      <template v-if="currentPackId">
        <NFormItem>
          <template #label>
            <div class="form-label-with-help">
              <span>天命判定系统</span>
              <NPopover trigger="hover" placement="right">
                <template #trigger>
                  <NIcon :component="HelpCircleOutline" class="help-icon" />
                </template>
                <div class="help-popover">
                  启用后，AI
                  在遇到不确定事件（战斗、探索、炼丹等）时会使用掷骰判定，让冒险更有紧张感。
                  <span v-if="selectedPack?.roll_system_config?.force_enabled">
                    <br /><br />
                    ⚠️ 此预设包强制启用判定系统，无法修改。
                  </span>
                </div>
              </NPopover>
            </div>
          </template>
          <NSwitch
            v-model:value="form.roll_system_enabled"
            :disabled="selectedPack?.roll_system_config?.allow_user_override === false"
          >
            <template #checked>开启</template>
            <template #unchecked>关闭</template>
          </NSwitch>
        </NFormItem>

        <NFormItem v-if="form.roll_system_enabled" label="判定难度">
          <NSelect
            v-model:value="form.roll_difficulty_mode"
            :disabled="selectedPack?.roll_system_config?.allow_user_override === false"
            :options="
              selectedPack?.roll_system_config?.allowed_difficulties
                ? selectedPack.roll_system_config.allowed_difficulties.map((d) => ({
                    label: { easy: '简单 +15%', normal: '普通', hard: '困难 -15%' }[d] || d,
                    value: d,
                  }))
                : [
                    { label: '简单 +15%', value: 'easy' },
                    { label: '普通', value: 'normal' },
                    { label: '困难 -15%', value: 'hard' },
                  ]
            "
          />
          <template
            v-if="selectedPack?.roll_system_config?.allow_user_override === false"
            #feedback
          >
            <div style="font-size: 12px; color: var(--text-faint); margin-top: 4px">
              ⚠️ 此预设包已锁定难度设置
            </div>
          </template>
        </NFormItem>
      </template>
    </NForm>
    <template #footer>
      <div class="row" style="justify-content: flex-end; gap: 8px">
        <NButton quaternary :disabled="isCreating" @click="emit('update:show', false)"
          >取消</NButton
        >
        <NButton type="primary" :loading="isCreating" :disabled="!canCreate" @click="create"
          >创建并进入</NButton
        >
      </div>
    </template>
  </NModal>
</template>

<style scoped>
.form-label-with-help {
  display: flex;
  align-items: center;
  gap: 6px;
}

.help-icon {
  font-size: 16px;
  color: var(--text-faint);
  cursor: help;
  transition: color 0.2s ease;
}

.help-icon:hover {
  color: var(--accent);
}

.help-popover {
  max-width: 320px;
  line-height: 1.6;
  font-size: 13px;
  color: var(--text);
}

.divider {
  margin: 20px 0 16px;
  text-align: center;
  position: relative;
}

.divider::before {
  content: '';
  position: absolute;
  left: 0;
  right: 0;
  top: 50%;
  height: 1px;
  background: var(--border-soft);
}

.divider span {
  position: relative;
  padding: 0 12px;
  background: var(--bg-panel);
  color: var(--text-faint);
  font-size: 12px;
  letter-spacing: 0.5px;
}
</style>
