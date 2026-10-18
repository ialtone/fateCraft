import type { ModuleActionCapability } from './module-actions'
import type { CompletionMetric } from './api'

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
}
