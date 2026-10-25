import type { Campaign, ChallengeRunReward, NarrativeActionResult } from './api'
import type { StageProgress, StageReward } from './endings'

export interface RuleSchema {
  type: 'object' | 'array' | 'string' | 'integer' | 'number' | 'boolean'
  title?: string
  properties?: Record<string, RuleSchema>
  additionalProperties?: boolean | RuleSchema
  items?: RuleSchema
  minimum?: number
  maximum?: number
  maxLength?: number
  maxItems?: number
  maxProperties?: number
  enum?: Array<string | number>
}

export interface ModuleParameter {
  key: string
  label: string
  type: 'string' | 'integer' | 'boolean'
  required: boolean
  minimum?: number
  maximum?: number
  options?: string[]
  option_labels?: Record<string, string>
}

export interface ModuleActionPlan {
  label: string
  arguments: Record<string, string | number | boolean>
  minutes: number
  costs?: Record<string, number>
  risk?: string
  /** 方案存在但当前资源不足时的具体缺口说明；带此字段的方案仅用于展示 */
  blocked_reason?: string
}

export interface ModuleActionCapability {
  ending?: boolean
  key: string
  description: string
  available: boolean
  revision: number
  parameters?: ModuleParameter[]
  plans?: ModuleActionPlan[]
  cost_labels?: Record<string, string>
  metadata: { label: string; minutes: number; costs?: Record<string, number>; risk?: string } | null
  violations?: Array<{ code: string; message: string }>
}

export interface ModuleActionSnapshot {
  revision: number
  branch_epoch: number
  action_capabilities: ModuleActionCapability[]
}

export interface ModuleActionInput {
  request_id: string
  branch_epoch: number
  expected_revision: number
  key: string
  arguments: Record<string, string | number | boolean>
}

export interface ModuleActionResult {
  completion?: import('./endings').CompletionView
  campaign?: Campaign
  revision: number
  result: Record<string, unknown>
  narrative?: NarrativeActionResult
  settlement?: {
    run: { summary: string }
    outcome: {
      key: string
      category: 'success' | 'failure' | 'neutral'
      title: string
      tone: string
    }
    rewards: ChallengeRunReward[]
    rewards_disabled_reason?: string
  }
  /** 当前分支已达成、尚未结算的阶段；局中不增加余额。 */
  stage_progress?: StageProgress[]
  /** 上述阶段的预期积分合计。 */
  pending_stage_points?: number
  /** 本次动作执行终局结算时返回的阶段回执。 */
  stage_rewards?: StageReward[]
  /** 本次终局结算实际新增的阶段积分。 */
  stage_reward_points?: number
}
