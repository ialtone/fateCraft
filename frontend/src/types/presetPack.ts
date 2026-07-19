export interface PresetPack {
  id: string
  name: string
  version: string
  description: string
  author: string
  icon?: string
  tags: string[]
  default_config: {
    recommended_character: string
    recommended_persona: string
    recommended_scenario: string
    recommended_prompt: string
    required_worldbooks: string[]
  }
  roll_system_config?: {
    force_enabled: boolean // 强制启用判定系统
    allow_user_override: boolean // 是否允许用户修改
    default_difficulty: string // 默认难度（easy/normal/hard）
    allowed_difficulties: string[] // 允许的难度选项
  }
  quick_start_guide: string
  features: string[]
  dependencies: string[]
  created_at: string
  updated_at: string
}

// 添加 pack_id 字段到现有类型（可选，用于标识资源所属的预设包）
export interface ResourceWithPack {
  pack_id?: string
}
