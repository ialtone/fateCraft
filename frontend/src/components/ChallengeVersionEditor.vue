<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import {
  NAlert,
  NButton,
  NDynamicTags,
  NFormItem,
  NInput,
  NInputNumber,
  NSelect,
  NSwitch,
  NTabPane,
  NTabs,
  NTag,
  useMessage,
} from 'naive-ui'
import JsonValueInput from '@/components/JsonValueInput.vue'
import RuleSchemaInput from '@/components/RuleSchemaInput.vue'
import { outcomeRewardBudgets } from '@/lib/reward-budget'
import type { GameplayModuleDescriptor, SaveChallengeVersionInput } from '@/types'

const props = defineProps<{
  modules: GameplayModuleDescriptor[]
  readonly?: boolean
}>()
const model = defineModel<SaveChallengeVersionInput>({ required: true })
const message = useMessage()
const activeTab = ref('basic')
const advanced = ref('')
const advancedBaseline = ref('')
const rawDefinitions = ref(false)
const category = ref('')
const categoryOptions = computed(() => {
  const categories = new Map<string, string>()
  for (const module of props.modules)
    categories.set(module.category ?? '', module.category_name ?? '通用基础模块')
  return [...categories].map(([value, label]) => ({ value, label }))
})

const manifest = computed<any>(() => model.value.manifest)
const runtime = computed<any>(() => model.value.runtime)
const stateSchema = computed<any>(() => runtime.value.state_schema)
const actionConfig = computed<any>(() => runtime.value.action_config)
const presentation = computed<any>(() => runtime.value.presentation)
const campaignConfig = computed<any>({
  get: () => runtime.value.campaign_config,
  set: (value) => {
    runtime.value.campaign_config = value
  },
})
const fields = computed<any[]>(() => stateSchema.value.fields)
const sections = computed<any[]>(() => presentation.value.state_sections)

watch(
  () => runtime.value.mode,
  (mode) => {
    if (mode === 'campaign') {
      presentation.value.campaign_view ||= {
        renderer: 'generic',
        sections: [],
        actions: [],
        empty_states: {},
      }
    } else if (presentation.value.campaign_view) {
      delete presentation.value.campaign_view
    }
  },
  { immediate: true },
)

const moduleOptions = computed(() =>
  props.modules
    .filter((item) => (item.category ?? '') === category.value)
    .map((item) => ({
      label: `${item.name} · ${item.key}@${item.version}`,
      value: `${item.key}@${item.version}`,
    })),
)
const selectedModule = computed(() =>
  props.modules.find(
    (item) =>
      item.key === runtime.value.engine_key && item.version === runtime.value.engine_version,
  ),
)
watch(
  selectedModule,
  (module) => {
    category.value = module?.category ?? ''
  },
  { immediate: true },
)
const actionOptions = computed(() =>
  (selectedModule.value?.actions || []).map((value) => ({ label: value, value })),
)
const verifierOptions = computed(() =>
  (selectedModule.value?.verifiers || []).map((value) => ({ label: value, value })),
)
const settlementOptions = computed(() =>
  (selectedModule.value?.settlement_handlers || []).map((value) => ({ label: value, value })),
)
const modeOptions = computed(() =>
  (selectedModule.value?.modes || ['challenge']).map((value) => ({
    label: value === 'campaign' ? '长期世界' : '单局挑战',
    value,
  })),
)
const outcomeOptions = computed(() =>
  model.value.outcomes.map((item) => ({
    label: `${item.title || '未命名'} · ${item.outcome_key}`,
    value: item.id,
  })),
)
const playerStatePathOptions = computed(() =>
  fields.value
    .filter((item) => item.visibility === 'player')
    .map((item) => ({ label: `${item.path} · ${item.type}`, value: item.path })),
)

const stateTypeOptions = ['string', 'integer', 'number', 'boolean', 'enum', 'object', 'list'].map(
  (value) => ({ label: value, value }),
)
const visibilityOptions = [
  { label: '玩家可见', value: 'player' },
  { label: '仅模型可见', value: 'model' },
  { label: '仅后端内部', value: 'internal' },
]
const updatePolicyOptions = [
  { label: '仅可信玩法模块更新', value: 'module' },
  { label: '模型可更新', value: 'model' },
  { label: '仅系统更新', value: 'system' },
  { label: '不可变', value: 'immutable' },
]
const widgetOptions = ['text', 'number', 'progress', 'badge', 'boolean', 'list', 'item_list'].map(
  (value) => ({ label: value, value }),
)
const assetKindOptions = [
  { label: '角色卡', value: 'character' },
  { label: '玩家人设', value: 'persona' },
  { label: '场景', value: 'scenario' },
  { label: 'Prompt', value: 'prompt' },
  { label: '世界书', value: 'worldbook' },
]
const definitionLabels = Object.fromEntries(
  [
    ['境界', 'realm'],
    ['功法', 'technique'],
    ['堆叠物品', 'item'],
    ['法器装备', 'equipment'],
    ['配方', 'recipe'],
    ['市场', 'market'],
    ['地点', 'location'],
    ['势力', 'faction'],
    ['NPC 原型', 'npc_archetype'],
    ['遭遇', 'encounter'],
    ['世界事件', 'world_event'],
    ['任务', 'quest'],
    ['生存场景', 'survival.scenario'],
  ].map(([label, value]) => [value, label]),
)
const definitionKindOptions = computed(() =>
  (selectedModule.value?.definition_kinds ?? []).map((value) => ({
    label: selectedModule.value?.definition_labels?.[value] ?? definitionLabels[value] ?? value,
    value,
  })),
)
const domainActionOptions = computed(() =>
  (selectedModule.value?.runtime_actions ?? []).map((action) => ({
    label: action.label,
    value: action.key,
  })),
)
const outcomeCategoryOptions = [
  { label: '成功', value: 'success' },
  { label: '失败', value: 'failure' },
  { label: '中性', value: 'neutral' },
]
const toneOptions = ['success', 'error', 'warning', 'info'].map((value) => ({
  label: value,
  value,
}))

function changeModule(value: string) {
  const [key, versionText] = value.split('@')
  const descriptor = props.modules.find(
    (item) => item.key === key && item.version === Number(versionText),
  )
  if (!descriptor) return
  if (
    runtime.value.engine_key === descriptor.key &&
    runtime.value.engine_version === descriptor.version
  )
    return
  runtime.value.engine_key = descriptor.key
  runtime.value.engine_version = descriptor.version
  runtime.value.mode = descriptor.modes[0] || 'challenge'
  actionConfig.value.model = actionConfig.value.model.filter((item: string) =>
    descriptor.actions.includes(item),
  )
  runtime.value.settlement_config.handler = descriptor.settlement_handlers[0] || ''
  actionConfig.value.domain = (descriptor.runtime_actions ?? []).map((action) => action.key)
  if (runtime.value.mode === 'campaign') {
    runtime.value.campaign_config = {
      lifecycle_handler: descriptor.campaign_lifecycle_handlers?.[0] || '',
      projection_handler: descriptor.campaign_projection_handlers?.[0] || '',
      context_policy: descriptor.campaign_context_policies?.[0] || '',
      action_allowlist: descriptor.campaign_actions ?? [],
      initial_location_key: 'world.start',
      world_clock: { calendar_key: 'world.default', max_due_events_per_turn: 16 },
    }
  } else {
    delete runtime.value.campaign_config
  }
  if (descriptor.runtime_actions?.length) {
    actionConfig.value.model = [...descriptor.actions]
    actionConfig.value.player = []
    actionConfig.value.rolls = {}
    stateSchema.value.schema_version = 1
    stateSchema.value.fields = [
      {
        path: 'narrative.current_focus',
        type: 'string',
        default: '',
        visibility: 'player',
        update_policy: 'model',
      },
    ]
    presentation.value.state_sections = []
    delete presentation.value.guidance
    if (runtime.value.mode === 'challenge') {
      stateSchema.value.fields.unshift(
        {
          path: 'module_state',
          type: 'object',
          default: {},
          visibility: 'internal',
          update_policy: 'module',
        },
        {
          path: 'gameplay',
          type: 'object',
          default: {},
          visibility: 'player',
          update_policy: 'module',
          allow_children: true,
        },
        {
          path: 'gameplay.outcome_key',
          type: 'string',
          default: '',
          visibility: 'player',
          update_policy: 'module',
        },
      )
      presentation.value.state_sections = [
        {
          key: 'result',
          title: '进展',
          order: 0,
          fields: [{ path: 'gameplay.outcome_key', label: '结局', widget: 'text', order: 0 }],
        },
      ]
    } else {
      runtime.value.campaign_config.field_ownership = [
        {
          path: 'gameplay',
          owner: 'campaign',
          value_type: 'object',
          allow_children: true,
          visibility: 'player',
          storage_projection: 'campaign_runtime_states',
          write_capability: descriptor.runtime_actions[0]!.key,
          event_types: [...descriptor.runtime_actions[0]!.events],
        },
      ]
      presentation.value.campaign_view = {
        renderer: 'generic',
        title: descriptor.name,
        sections: [],
        actions: [],
        empty_states: {},
      }
    }
    model.value.definitions = model.value.definitions.filter((definition) =>
      descriptor.definition_kinds.includes(definition.kind),
    )
    message.info(
      '已设置新模块的状态权限与动作。请补充规则、结局和展示配置；也可从内置示例导入后修改。',
    )
  }
}

function assetOptions(kind: string) {
  return model.value.assets
    .filter((item) => item.kind === kind)
    .map((item) => ({ label: `${item.name} · ${item.slug}`, value: item.slug }))
}

function addAsset() {
  const now = new Date().toISOString()
  model.value.assets.push({
    id: crypto.randomUUID(),
    challenge_version_id: runtime.value.challenge_version_id,
    kind: 'character',
    slug: '',
    name: '',
    content: {},
    sort_order: model.value.assets.length,
    created_at: now,
    updated_at: now,
  })
}

function addDefinition() {
  const kind = definitionKindOptions.value[0]?.value
  if (!kind) return
  const now = new Date().toISOString()
  model.value.definitions.push({
    id: crypto.randomUUID(),
    challenge_version_id: runtime.value.challenge_version_id,
    kind,
    key: '',
    name: '',
    content: {},
    tags: [],
    search_text: '',
    visibility: 'player',
    sort_order: model.value.definitions.length,
    created_at: now,
    updated_at: now,
  })
}

function addStateField() {
  fields.value.push({
    path: `state.field_${fields.value.length + 1}`,
    type: 'string',
    default: '',
    visibility: 'player',
    update_policy: 'model',
    constraints: {},
  })
}

function onStateTypeChange(field: any, type: string) {
  field.type = type
  if (type === 'string' || type === 'enum') field.default = ''
  else if (type === 'integer' || type === 'number') field.default = 0
  else if (type === 'boolean') field.default = false
  else if (type === 'object') field.default = {}
  else if (type === 'list') field.default = []
  field.allow_children = type === 'object' ? Boolean(field.allow_children) : false
}

function removeStateField(index: number) {
  const [removed] = fields.value.splice(index, 1)
  for (const section of sections.value) {
    section.fields = section.fields.filter((item: any) => item.path !== removed.path)
  }
}

function addSection() {
  sections.value.push({
    key: `section-${sections.value.length + 1}`,
    title: '新分组',
    order: (sections.value.length + 1) * 10,
    fields: [],
  })
}

function addViewField(section: any) {
  section.fields.push({
    path: playerStatePathOptions.value[0]?.value || '',
    label: '状态',
    widget: 'text',
    unit: '',
    order: (section.fields.length + 1) * 10,
  })
}

function addOutcome() {
  model.value.outcomes.push({
    id: crypto.randomUUID(),
    challenge_version_id: runtime.value.challenge_version_id,
    reward_points: 0,
    outcome_key: `outcome_${model.value.outcomes.length + 1}`,
    category: 'success',
    title: '新结局',
    tone: 'success',
    verifier_key: verifierOptions.value[0]?.value || 'core.always',
    verifier_config: {},
    enabled: true,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  })
}

function removeOutcome(index: number) {
  const [removed] = model.value.outcomes.splice(index, 1)
}

function onTabChange(value: string) {
  activeTab.value = value
  if (value === 'advanced' && advanced.value === advancedBaseline.value) {
    advanced.value = JSON.stringify(model.value, null, 2)
    advancedBaseline.value = advanced.value
  }
}

function applyAdvanced() {
  try {
    const parsed = JSON.parse(advanced.value)
    if (
      !parsed.manifest ||
      !parsed.runtime ||
      !Array.isArray(parsed.assets) ||
      !Array.isArray(parsed.definitions) ||
      !Array.isArray(parsed.outcomes)
    ) {
      throw new Error('缺少必需区域')
    }
    model.value = parsed
    advancedBaseline.value = advanced.value
    message.success('高级 JSON 已应用到结构化表单，仍需保存草稿')
  } catch (err: any) {
    message.error(`无法应用 JSON：${err.message || '格式无效'}`)
  }
}
</script>

<template>
  <NTabs :value="activeTab" type="line" animated @update:value="onTabChange">
    <NTabPane name="basic" tab="基本信息" display-directive="show:lazy">
      <div class="form-grid">
        <NFormItem label="玩法 ID"
          ><NInput v-model:value="manifest.id" :readonly="readonly"
        /></NFormItem>
        <NFormItem label="版本"><NInput v-model:value="manifest.version" readonly /></NFormItem>
        <NFormItem label="名称"
          ><NInput v-model:value="manifest.name" :readonly="readonly"
        /></NFormItem>
        <NFormItem label="作者"
          ><NInput v-model:value="manifest.author" :readonly="readonly"
        /></NFormItem>
        <NFormItem label="图标 Key"
          ><NInput v-model:value="manifest.icon" :readonly="readonly"
        /></NFormItem>
        <NFormItem label="说明" class="wide"
          ><NInput v-model:value="manifest.description" type="textarea" :readonly="readonly"
        /></NFormItem>
        <NFormItem label="快速上手" class="wide"
          ><NInput v-model:value="manifest.quick_start_guide" type="textarea" :readonly="readonly"
        /></NFormItem>
        <NFormItem label="标签" class="wide"
          ><NDynamicTags v-model:value="manifest.tags" :disabled="readonly"
        /></NFormItem>
        <NFormItem label="特色" class="wide"
          ><NDynamicTags v-model:value="manifest.features" :disabled="readonly"
        /></NFormItem>
      </div>
      <h4>入口资产</h4>
      <div class="form-grid">
        <NFormItem label="推荐角色卡"
          ><NSelect
            v-model:value="manifest.default_config.recommended_character"
            :options="assetOptions('character')"
            :disabled="readonly"
        /></NFormItem>
        <NFormItem label="推荐玩家人设"
          ><NSelect
            v-model:value="manifest.default_config.recommended_persona"
            :options="assetOptions('persona')"
            :disabled="readonly"
        /></NFormItem>
        <NFormItem label="推荐场景"
          ><NSelect
            v-model:value="manifest.default_config.recommended_scenario"
            :options="assetOptions('scenario')"
            :disabled="readonly"
        /></NFormItem>
        <NFormItem label="推荐 Prompt"
          ><NSelect
            v-model:value="manifest.default_config.recommended_prompt"
            :options="assetOptions('prompt')"
            :disabled="readonly"
        /></NFormItem>
        <NFormItem label="允许选择的人设" class="wide"
          ><NSelect
            v-model:value="manifest.default_config.allowed_personas"
            multiple
            :options="assetOptions('persona')"
            :disabled="readonly"
        /></NFormItem>
        <NFormItem label="必需世界书" class="wide"
          ><NSelect
            v-model:value="manifest.default_config.required_worldbooks"
            multiple
            :options="assetOptions('worldbook')"
            :disabled="readonly"
        /></NFormItem>
      </div>
      <NFormItem label="进入条件（可选）">
        <JsonValueInput v-model="manifest.entry_requirement" :readonly="readonly" :min-rows="3" />
      </NFormItem>
    </NTabPane>

    <NTabPane name="assets" tab="叙事资产" display-directive="show:lazy">
      <div class="section-toolbar">
        <p>角色卡、玩家人设、场景、Prompt 与世界书仍使用各自稳定 JSON 格式。</p>
        <NButton v-if="!readonly" size="small" @click="addAsset">新增资产</NButton>
      </div>
      <NAlert v-if="!model.assets.length" type="info"
        >发布前至少需要入口配置引用的完整资产。</NAlert
      >
      <article v-for="(asset, index) in model.assets" :key="asset.id" class="editor-card">
        <div class="card-head">
          <strong>{{ asset.name || '未命名资产' }}</strong>
          <NButton
            v-if="!readonly"
            size="tiny"
            type="error"
            secondary
            @click="model.assets.splice(index, 1)"
            >删除</NButton
          >
        </div>
        <div class="form-grid compact">
          <NFormItem label="类型"
            ><NSelect v-model:value="asset.kind" :options="assetKindOptions" :disabled="readonly"
          /></NFormItem>
          <NFormItem label="稳定 Slug"
            ><NInput v-model:value="asset.slug" :readonly="readonly"
          /></NFormItem>
          <NFormItem label="名称"
            ><NInput v-model:value="asset.name" :readonly="readonly"
          /></NFormItem>
          <NFormItem label="排序"
            ><NInputNumber v-model:value="asset.sort_order" :disabled="readonly"
          /></NFormItem>
        </div>
        <NFormItem label="资产内容"
          ><JsonValueInput
            v-model="asset.content"
            :readonly="readonly"
            :min-rows="5"
            :max-rows="16"
        /></NFormItem>
      </article>
    </NTabPane>

    <NTabPane name="definitions" tab="玩法定义" display-directive="show:lazy">
      <div class="section-toolbar">
        <p>境界、功法、物品、地点、NPC 与势力使用版本化稳定 key；正文只负责叙事。</p>
        <NButton v-if="!readonly" size="small" @click="addDefinition">新增定义</NButton>
      </div>
      <NAlert v-if="runtime.mode === 'campaign' && !model.definitions.length" type="warning">
        定义类型与必需规则由所选模块决定，发布时会检查资源来源、引用和结局条件。
      </NAlert>
      <article
        v-for="(definition, index) in model.definitions"
        :key="definition.id"
        class="editor-card"
      >
        <div class="card-head">
          <strong>{{ definition.name || '未命名玩法定义' }}</strong>
          <NButton
            v-if="!readonly"
            size="tiny"
            type="error"
            secondary
            @click="model.definitions.splice(index, 1)"
            >删除</NButton
          >
        </div>
        <div class="form-grid compact">
          <NFormItem label="类型">
            <NSelect
              v-model:value="definition.kind"
              :options="definitionKindOptions"
              :disabled="readonly"
            />
          </NFormItem>
          <NFormItem label="稳定 Key">
            <NInput v-model:value="definition.key" :readonly="readonly" />
          </NFormItem>
          <NFormItem label="名称">
            <NInput v-model:value="definition.name" :readonly="readonly" />
          </NFormItem>
          <NFormItem label="排序">
            <NInputNumber v-model:value="definition.sort_order" :disabled="readonly" />
          </NFormItem>
          <NFormItem label="可见性">
            <NSelect
              v-model:value="definition.visibility"
              :options="visibilityOptions"
              :disabled="readonly"
            />
          </NFormItem>
          <NFormItem label="检索文本" class="wide">
            <NInput v-model:value="definition.search_text" :readonly="readonly" />
          </NFormItem>
        </div>
        <NFormItem label="标签">
          <NDynamicTags v-model:value="definition.tags" :disabled="readonly" />
        </NFormItem>
        <NFormItem label="规则内容">
          <div class="rule-editor">
            <div
              v-if="selectedModule?.definition_schemas?.[definition.kind]"
              class="section-toolbar"
            >
              <span>按模块字段编辑</span
              ><NButton size="tiny" @click="rawDefinitions = !rawDefinitions">{{
                rawDefinitions ? '返回字段' : '高级 JSON'
              }}</NButton>
            </div>
            <RuleSchemaInput
              v-if="!rawDefinitions && selectedModule?.definition_schemas?.[definition.kind]"
              v-model="definition.content"
              :schema="selectedModule.definition_schemas[definition.kind]!"
              :readonly="readonly"
            />
            <JsonValueInput
              v-else
              v-model="definition.content"
              :readonly="readonly"
              :min-rows="5"
              :max-rows="18"
            />
          </div>
        </NFormItem>
      </article>
    </NTabPane>

    <NTabPane name="runtime" tab="玩法运行" display-directive="show:lazy">
      <div class="form-grid">
        <NFormItem label="玩法主类型"
          ><NSelect v-model:value="category" :options="categoryOptions" :disabled="readonly"
        /></NFormItem>
        <NFormItem label="可信玩法模块">
          <NSelect
            :value="`${runtime.engine_key}@${runtime.engine_version}`"
            :options="moduleOptions"
            :disabled="readonly"
            @update:value="changeModule"
          />
        </NFormItem>
        <NFormItem label="运行模式">
          <NSelect
            v-model:value="runtime.mode"
            :options="modeOptions"
            :disabled="readonly || modeOptions.length <= 1"
          />
        </NFormItem>
        <NFormItem v-if="runtime.mode !== 'campaign'" label="结算处理器"
          ><NSelect
            v-model:value="runtime.settlement_config.handler"
            :options="settlementOptions"
            :disabled="readonly"
        /></NFormItem>
        <NFormItem label="允许的模型动作" class="wide"
          ><NSelect
            v-model:value="actionConfig.model"
            multiple
            :options="actionOptions"
            :disabled="readonly"
        /></NFormItem>
        <NFormItem v-if="domainActionOptions.length" label="模块提供的玩法行动" class="wide">
          <NSelect
            v-model:value="actionConfig.domain"
            multiple
            :options="domainActionOptions"
            disabled
          />
          <template #feedback>行动由所选模块统一提供，可用条件随游戏进度变化。</template>
        </NFormItem>
      </div>
      <NFormItem v-if="runtime.mode === 'campaign'" label="长期世界配置">
        <JsonValueInput v-model="campaignConfig" :readonly="readonly" :min-rows="8" />
      </NFormItem>
      <NFormItem label="玩家能力配置">
        <JsonValueInput v-model="actionConfig.capabilities" :readonly="readonly" :min-rows="4" />
      </NFormItem> </NTabPane
    ><NTabPane name="state" tab="状态字段" display-directive="show:lazy">
      <div class="section-toolbar">
        <div>
          <h4>状态 Schema</h4>
          <p>每个可验证或展示的状态都应有稳定路径和明确类型。</p>
        </div>
        <NButton v-if="!readonly" size="small" @click="addStateField">新增字段</NButton>
      </div>
      <article v-for="(field, index) in fields" :key="`${index}-${field.path}`" class="editor-card">
        <div class="card-head">
          <code>{{ field.path }}</code>
          <NButton
            v-if="!readonly"
            size="tiny"
            type="error"
            secondary
            @click="removeStateField(index)"
            >删除</NButton
          >
        </div>
        <div class="form-grid compact">
          <NFormItem label="路径"
            ><NInput v-model:value="field.path" :readonly="readonly"
          /></NFormItem>
          <NFormItem label="类型"
            ><NSelect
              :value="field.type"
              :options="stateTypeOptions"
              :disabled="readonly"
              @update:value="(value) => onStateTypeChange(field, value)"
          /></NFormItem>
          <NFormItem label="可见性"
            ><NSelect
              v-model:value="field.visibility"
              :options="visibilityOptions"
              :disabled="readonly"
          /></NFormItem>
          <NFormItem label="更新权限"
            ><NSelect
              v-model:value="field.update_policy"
              :options="updatePolicyOptions"
              :disabled="readonly"
          /></NFormItem>
          <NFormItem v-if="field.type === 'object'" label="允许动态子路径"
            ><NSwitch v-model:value="field.allow_children" :disabled="readonly"
          /></NFormItem>
        </div>
        <div class="json-pair">
          <NFormItem label="默认值"
            ><JsonValueInput v-model="field.default" :readonly="readonly"
          /></NFormItem>
          <NFormItem label="约束"
            ><JsonValueInput v-model="field.constraints" :readonly="readonly"
          /></NFormItem>
        </div>
      </article>
    </NTabPane>

    <NTabPane name="presentation" tab="玩家界面" display-directive="show:lazy">
      <div class="form-grid">
        <NFormItem label="状态栏标题"
          ><NInput v-model:value="presentation.state_panel_title" :readonly="readonly"
        /></NFormItem>
        <NFormItem label="无奖励提示"
          ><NInput
            v-model:value="presentation.completion_view.empty_reward_text"
            :readonly="readonly"
        /></NFormItem>
        <NFormItem label="重玩按钮"
          ><NInput v-model:value="presentation.completion_view.replay_label" :readonly="readonly"
        /></NFormItem>
        <NFormItem label="返回按钮"
          ><NInput v-model:value="presentation.completion_view.back_label" :readonly="readonly"
        /></NFormItem>
      </div>
      <div class="section-toolbar">
        <div>
          <h4>状态展示分组</h4>
          <p>普通玩家只会看到这里引用的 player 状态。</p>
        </div>
        <NButton v-if="!readonly" size="small" @click="addSection">新增分组</NButton>
      </div>
      <article
        v-for="(section, sectionIndex) in sections"
        :key="`${sectionIndex}-${section.key}`"
        class="editor-card"
      >
        <div class="card-head">
          <strong>{{ section.title }}</strong>
          <NButton
            v-if="!readonly"
            size="tiny"
            type="error"
            secondary
            @click="sections.splice(sectionIndex, 1)"
            >删除分组</NButton
          >
        </div>
        <div class="form-grid compact">
          <NFormItem label="分组 Key"
            ><NInput v-model:value="section.key" :readonly="readonly"
          /></NFormItem>
          <NFormItem label="标题"
            ><NInput v-model:value="section.title" :readonly="readonly"
          /></NFormItem>
          <NFormItem label="排序"
            ><NInputNumber v-model:value="section.order" :disabled="readonly"
          /></NFormItem>
        </div>
        <div v-for="(item, itemIndex) in section.fields" :key="itemIndex" class="view-field-row">
          <NSelect
            v-model:value="item.path"
            :options="playerStatePathOptions"
            :disabled="readonly"
            placeholder="状态路径"
          />
          <NInput v-model:value="item.label" :readonly="readonly" placeholder="玩家标签" />
          <NSelect
            v-model:value="item.widget"
            :options="widgetOptions"
            :disabled="readonly"
            placeholder="控件"
          />
          <NInput v-model:value="item.unit" :readonly="readonly" placeholder="单位" />
          <NButton
            v-if="!readonly"
            size="tiny"
            type="error"
            quaternary
            @click="section.fields.splice(itemIndex, 1)"
            >移除</NButton
          >
        </div>
        <NButton v-if="!readonly" size="tiny" secondary @click="addViewField(section)"
          >添加展示字段</NButton
        >
      </article>
      <NFormItem label="界面术语"
        ><JsonValueInput v-model="presentation.terminology" :readonly="readonly"
      /></NFormItem>
      <NFormItem label="值标签"
        ><JsonValueInput v-model="presentation.value_labels" :readonly="readonly"
      /></NFormItem>
      <NFormItem v-if="runtime.mode === 'campaign'" label="长期世界视图">
        <JsonValueInput v-model="presentation.campaign_view" :readonly="readonly" :min-rows="8" />
      </NFormItem>
    </NTabPane>

    <NTabPane name="outcomes" tab="结局验证" display-directive="show:lazy">
      <div class="section-toolbar">
        <p>模型只提交 outcome_key，标题、类别和验证器均由这里的固定版本决定。</p>
        <NButton v-if="!readonly" size="small" @click="addOutcome">新增结局</NButton>
      </div>
      <article v-for="(outcome, index) in model.outcomes" :key="outcome.id" class="editor-card">
        <div class="card-head">
          <strong>{{ outcome.title }}</strong>
          <div class="card-tags">
            <NTag size="small">{{ outcome.category }}</NTag
            ><NButton
              v-if="!readonly"
              size="tiny"
              type="error"
              secondary
              @click="removeOutcome(index)"
              >删除</NButton
            >
          </div>
        </div>
        <div class="form-grid compact">
          <NFormItem label="稳定 Key"
            ><NInput v-model:value="outcome.outcome_key" :readonly="readonly"
          /></NFormItem>
          <NFormItem label="标题"
            ><NInput v-model:value="outcome.title" :readonly="readonly"
          /></NFormItem>
          <NFormItem label="类别"
            ><NSelect
              v-model:value="outcome.category"
              :options="outcomeCategoryOptions"
              :disabled="readonly"
          /></NFormItem>
          <NFormItem label="色调"
            ><NSelect v-model:value="outcome.tone" :options="toneOptions" :disabled="readonly"
          /></NFormItem>
          <NFormItem label="终局积分（0 表示不发放）"
            ><NInputNumber
              v-model:value="outcome.reward_points"
              :min="0"
              :max="Number.MAX_SAFE_INTEGER"
              :precision="0"
              :disabled="readonly"
          /></NFormItem>
          <NFormItem label="可信验证器"
            ><NSelect
              v-model:value="outcome.verifier_key"
              :options="verifierOptions"
              :disabled="readonly"
          /></NFormItem>
          <NFormItem label="启用"
            ><NSwitch v-model:value="outcome.enabled" :disabled="readonly"
          /></NFormItem>
        </div>
        <NAlert
          v-for="(budget, budgetIndex) in outcomeRewardBudgets(
            model.definitions,
            selectedModule?.definition_schemas ?? {},
            outcome.outcome_key,
            outcome.reward_points,
          )"
          :key="budgetIndex"
          :type="budget.valid ? 'info' : 'warning'"
          :bordered="false"
        >
          {{ budget.text }}。阶段积分在达成时发放；终局积分仅在通关时发放。
          <template v-if="!budget.valid"
            >请在“规则定义”中核对阶段与总预算，发布时会再次校验。</template
          >
        </NAlert>
        <NFormItem label="验证器配置"
          ><JsonValueInput v-model="outcome.verifier_config" :readonly="readonly" :min-rows="3"
        /></NFormItem>
      </article>
    </NTabPane>

    <NTabPane name="advanced" tab="高级 JSON" display-directive="show:lazy">
      <NAlert type="warning" :bordered="false"
        >高级模式用于导入、导出和少量特殊配置。应用后仍会经过与结构化表单相同的后端发布校验。</NAlert
      >
      <NInput
        v-model:value="advanced"
        type="textarea"
        :autosize="{ minRows: 22, maxRows: 42 }"
        :readonly="readonly"
        class="advanced-editor"
      />
      <NButton v-if="!readonly" class="apply-json" @click="applyAdvanced">应用到表单</NButton>
    </NTabPane>
  </NTabs>
</template>

<style scoped>
.rule-editor {
  width: 100%;
  min-width: 0;
}
.form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 16px;
}

.form-grid.compact {
  grid-template-columns: repeat(4, minmax(0, 1fr));
}

.wide {
  grid-column: 1 / -1;
}

h4 {
  margin: 10px 0 12px;
}

.section-toolbar,
.card-head,
.card-tags {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.section-toolbar {
  margin: 6px 0 14px;
}

.section-toolbar h4,
.section-toolbar p {
  margin: 0;
}

.section-toolbar p {
  color: var(--text-dim);
  font-size: 13px;
}

.editor-card {
  margin-bottom: 12px;
  padding: 14px;
  border: 1px solid var(--border-soft);
  border-radius: 8px;
  background: color-mix(in srgb, var(--bg) 78%, transparent);
}

.card-head {
  margin-bottom: 12px;
}

.card-head code {
  overflow-wrap: anywhere;
  color: var(--accent);
}

.json-pair {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}

.view-field-row {
  display: grid;
  grid-template-columns: 1.5fr 1fr 0.8fr 0.6fr auto;
  gap: 8px;
  margin-bottom: 8px;
}

.advanced-editor {
  margin-top: 12px;
}

.advanced-editor :deep(textarea) {
  font-size: 12px;
}

.apply-json {
  margin-top: 10px;
}

@media (max-width: 900px) {
  .form-grid.compact {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .view-field-row {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 640px) {
  .form-grid,
  .form-grid.compact,
  .json-pair,
  .view-field-row {
    grid-template-columns: 1fr;
  }

  .section-toolbar,
  .card-head {
    align-items: flex-start;
  }
}
</style>
