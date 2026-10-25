import type { ModuleActionCapability } from './module-actions'
import type { CompletionMetric } from './api'

export interface StageReward {
  stage_key: string
  name: string
  sort_order: number
  configured_points: number
  granted_points: number
  disabled_reason?: string
  granted_at: string
}

/**
 * 当前分支已达成、尚未结算的阶段。configured_points 只是预期积分，
 * 局中不会增加账号余额；终局结算后才产生 StageReward 回执。
 */
export interface StageProgress {
  stage_key: string
  name: string
  sort_order: number
  configured_points: number
  disabled_reason?: string
  reached_at?: string
}

export interface CompletionView {
  id: string
  conversation_id: string
  branch_epoch: number
  revision: number
  key: string
  category: 'success' | 'failure' | 'neutral'
  title: string
  tone: string
  summary: string
  chronicle?: string[]
  rewards: Array<{
    reward_key: string
    name: string
    unit: string
    icon: string
    quantity: number
    balance_after: number
  }>
  metrics: CompletionMetric[]
  next_actions: Array<'replay' | 'back_to_challenges'>
  rewards_disabled_reason?: string
  /** 本局已处理过的阶段事实（含直连的 0 分记录）。 */
  stage_rewards?: StageReward[]
  /** 本局实际到账的阶段积分合计。 */
  stage_reward_points?: number
  /** 终局积分；不要把 rewards 里的积分再加一遍。 */
  reward_points?: number
  /** 阶段积分 + 终局积分。 */
  total_reward_points?: number
}

export interface EndingProposal {
  id: string
  conversation_id: string
  branch_epoch: number
  revision: number
  key: string
  arguments: Record<string, string | number | boolean>
  label: string
  available: boolean
  reason?: string
  metadata: ModuleActionCapability['metadata']
}

export interface EndingSnapshot {
  revision: number
  branch_epoch: number
  has_trusted_actions: boolean
  abandon_action?: string
  ending_capabilities: ModuleActionCapability[]
  completion: CompletionView | null
  proposal: EndingProposal | null
  is_processing: boolean
  /** 当前分支已达成、尚未结算的阶段；局中不增加余额。 */
  stage_progress?: StageProgress[]
  /** 上述阶段的预期积分合计。 */
  pending_stage_points?: number
  /** 已结算的阶段回执；只有终局后才可能出现。 */
  stage_rewards?: StageReward[]
  stage_reward_points?: number
}
