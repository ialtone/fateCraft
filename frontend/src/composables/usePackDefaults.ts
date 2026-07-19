import { computed, ref, watch, type Ref } from 'vue'
import { useTemplateStore } from '@/stores/templates'
import { usePromptStore } from '@/stores/prompts'
import { usePresetPackStore } from '@/stores/presetPacks'

// 新建对话表单的形状：表单状态由弹窗组件持有，本 composable 只负责联动读写
export interface CreateConversationForm {
  title: string
  character_id: string
  persona_id: string
  scenario_id: string
  prompt_preset_id: string
  world_book_id: string // 单选
  roll_system_enabled: boolean
  roll_difficulty_mode: string
}

/**
 * 预设包联动逻辑：
 * - 根据所选世界书推断当前预设包（currentPackId / selectedPack）
 * - 切换预设包时应用推荐配置，切走时清理旧包的不兼容选项
 * - 提供按预设包筛选后的资源列表（无包时回退全量列表）
 * - 提供创建约束：必需世界书未选时禁止创建（canCreate）、必需世界书锁定选择器（isWorldBookLocked）
 */
export function usePackDefaults(form: Ref<CreateConversationForm>) {
  const templates = useTemplateStore()
  const prompts = usePromptStore()
  const presetPacks = usePresetPackStore()

  // 当前选中的预设包ID（根据世界书推断）
  const currentPackId = ref<string | null>(null)

  // 筛选后的资源列表（根据预设包筛选；无包时回退全量，供表单直接消费）
  const filteredCharacters = computed(() => {
    if (!currentPackId.value) return templates.characters
    // 使用后端返回的 pack_id 字段精确匹配
    return templates.characters.filter((c) => c.pack_id === currentPackId.value)
  })

  const filteredPersonas = computed(() => {
    if (!currentPackId.value) return templates.personas
    return templates.personas.filter((p) => p.pack_id === currentPackId.value)
  })

  const filteredScenarios = computed(() => {
    if (!currentPackId.value) return templates.scenarios
    return templates.scenarios.filter((s) => s.pack_id === currentPackId.value)
  })

  // 当前选中的预设包信息
  const selectedPack = computed(() => {
    if (!currentPackId.value) return null
    return presetPacks.packs.find((p) => p.id === currentPackId.value)
  })

  // 是否可以创建对话
  const canCreate = computed(() => {
    // 如果当前选中的预设包有必需的世界书，则必须选择世界书
    if (
      selectedPack.value?.default_config?.required_worldbooks &&
      selectedPack.value.default_config.required_worldbooks.length > 0
    ) {
      return !!form.value.world_book_id
    }
    return true // 没有预设包或预设包不要求世界书时，允许不选
  })

  // 世界书选择器是否被锁定（required_worldbooks 时锁定）
  const isWorldBookLocked = computed(() => {
    if (!selectedPack.value) return false
    return (selectedPack.value.default_config?.required_worldbooks?.length ?? 0) > 0
  })

  // 根据世界书选择推断预设包并筛选资源
  watch(
    () => form.value.world_book_id,
    (worldBookId) => {
      const oldPackId = currentPackId.value

      if (!worldBookId) {
        // 清空世界书选择，恢复显示全部资源
        currentPackId.value = null
        // 重置判定系统为全局设置
        form.value.roll_system_enabled = false
        form.value.roll_difficulty_mode = 'normal'
        return
      }

      // 根据世界书ID推断预设包
      const pack = presetPacks.getPackByWorldBookId(worldBookId)

      if (pack) {
        // 找到了对应的预设包
        currentPackId.value = pack.id

        // 应用预设包的判定系统配置
        if (pack.roll_system_config) {
          form.value.roll_system_enabled = pack.roll_system_config.force_enabled
          form.value.roll_difficulty_mode = pack.roll_system_config.default_difficulty
        }

        // 如果是切换到新的预设包，需要检查并重置不兼容的选项
        if (oldPackId !== pack.id) {
          // 检查当前选中的角色卡是否在筛选后的列表中
          if (form.value.character_id) {
            const isCharacterValid = filteredCharacters.value.some(
              (c) => c.id === form.value.character_id,
            )
            if (!isCharacterValid) {
              // 当前角色卡不在筛选列表中，应用推荐配置或清空
              form.value.character_id = pack.default_config.recommended_character || ''
            }
          } else {
            // 没有选择角色卡，应用推荐配置
            form.value.character_id = pack.default_config.recommended_character || ''
          }

          // 检查人设
          if (form.value.persona_id) {
            const isPersonaValid = filteredPersonas.value.some(
              (p) => p.id === form.value.persona_id,
            )
            if (!isPersonaValid) {
              form.value.persona_id = pack.default_config.recommended_persona || ''
            }
          } else {
            form.value.persona_id = pack.default_config.recommended_persona || ''
          }

          // 检查场景
          if (form.value.scenario_id) {
            const isScenarioValid = filteredScenarios.value.some(
              (s) => s.id === form.value.scenario_id,
            )
            if (!isScenarioValid) {
              form.value.scenario_id = pack.default_config.recommended_scenario || ''
            }
          } else {
            form.value.scenario_id = pack.default_config.recommended_scenario || ''
          }

          // 应用推荐的Prompt预设
          if (!form.value.prompt_preset_id || oldPackId) {
            form.value.prompt_preset_id = pack.default_config.recommended_prompt || ''
          }
        }
      } else {
        // 没有找到对应的预设包（可能是独立的世界书）
        currentPackId.value = null

        // 重置判定系统为全局设置
        form.value.roll_system_enabled = false
        form.value.roll_difficulty_mode = 'normal'

        // 如果之前有预设包，需要清理可能不兼容的选项
        if (oldPackId) {
          // 用 pack_id 字段精确判断资源是否属于之前的预设包
          if (form.value.character_id) {
            const belongsToOldPack =
              templates.characters.find((c) => c.id === form.value.character_id)?.pack_id ===
              oldPackId
            if (belongsToOldPack) {
              // 清空之前预设包的推荐选项
              form.value.character_id = ''
            }
          }

          if (form.value.persona_id) {
            const belongsToOldPack =
              templates.personas.find((p) => p.id === form.value.persona_id)?.pack_id === oldPackId
            if (belongsToOldPack) {
              form.value.persona_id = ''
            }
          }

          if (form.value.scenario_id) {
            const belongsToOldPack =
              templates.scenarios.find((s) => s.id === form.value.scenario_id)?.pack_id ===
              oldPackId
            if (belongsToOldPack) {
              form.value.scenario_id = ''
            }
          }

          if (form.value.prompt_preset_id) {
            // Prompt 预设无 pack_id 字段，与旧预设包的推荐 Prompt 比对
            const oldPack = presetPacks.packs.find((p) => p.id === oldPackId)
            const belongsToOldPack =
              form.value.prompt_preset_id === oldPack?.default_config?.recommended_prompt
            if (belongsToOldPack) {
              form.value.prompt_preset_id = prompts.presets[0]?.id || ''
            }
          }
        }
      }
    },
  )

  // 重置表单为默认值：如果有预设包，自动选中第一个预设包的必需世界书（打开弹窗时调用）
  function resetFormToDefaults() {
    let defaultWorldBookId = ''
    let defaultPackId: string | null = null

    if (presetPacks.packs.length > 0) {
      const firstPack = presetPacks.packs[0]
      const requiredBooks = firstPack.default_config?.required_worldbooks
      if (requiredBooks && requiredBooks.length > 0) {
        defaultWorldBookId = requiredBooks[0]
        defaultPackId = firstPack.id
      }
    }

    form.value = {
      title: '',
      character_id: '',
      persona_id: '',
      scenario_id: '',
      prompt_preset_id: prompts.presets[0]?.id || '',
      world_book_id: defaultWorldBookId, // 自动选中必需的世界书
      roll_system_enabled: false,
      roll_difficulty_mode: 'normal',
    }
    // 同步设置预设包状态：world_book_id 的 watch 异步触发时 oldPackId 已是新包，
    // 从而只应用判定系统配置、不覆盖刚重置的角色卡/人设/场景选项（与原实现一致）
    currentPackId.value = defaultPackId
  }

  return {
    currentPackId,
    selectedPack,
    filteredCharacters,
    filteredPersonas,
    filteredScenarios,
    canCreate,
    isWorldBookLocked,
    resetFormToDefaults,
  }
}
