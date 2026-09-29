import type {
  AdminUser,
  PlayerReward,
  RewardDefinition,
  RewardLedgerEntry,
  AdminAuditLog,
  ChallengeRewardRule,
} from './api'

export interface RewardRecord {
  id: string
  reward_definition_id: string
  source_type: string
  source_id: string
  snapshot: Pick<
    RewardDefinition,
    'key' | 'name' | 'kind' | 'unit' | 'icon' | 'description' | 'stackable' | 'consumable'
  >
  metadata: Record<string, unknown>
  quantity: number
  delivery_mode: 'direct' | 'claim'
  status: 'pending' | 'granted' | 'skipped' | 'cancelled'
  skip_reason: string
  ledger_id?: string
  reversal_ledger_id?: string
  balance_after: number
  purpose: string
  public_note: string
  cancelled_reason: string
  created_at: string
  completed_at?: string
  user_id?: string
  operator_id?: string
  request_id?: string
  reason?: string
}
export interface RewardGrantRequest {
  request_id: string
  reward_definition_id: string
  quantity: number
  purpose: 'gift' | 'compensation'
  delivery_mode: 'direct' | 'claim'
  reason: string
  public_note: string
}
export interface RegistrationRewardRule {
  id: number
  reward_definition_id: string
  quantity: number
  enabled: boolean
  actor_id?: string
  created_at: string
}
export interface RewardDefinitionImpact {
  exchange_offers: RewardExchangeOffer[]
  rules: ChallengeRewardRule[]
  pending: number
  registration?: RegistrationRewardRule
}
export type RewardAssetSnapshot = RewardRecord['snapshot']
export interface RewardExchangeOffer {
  id: string
  name: string
  description: string
  cost_definition_id: string
  cost_quantity: number
  reward_definition_id: string
  reward_quantity: number
  enabled: boolean
  revision: number
  sort_order: number
  created_at: string
  updated_at: string
}
export interface RewardExchangeOfferView extends RewardExchangeOffer {
  cost_asset: RewardAssetSnapshot
  reward_asset: RewardAssetSnapshot
  available: boolean
}
export interface RewardExchangeRequest {
  request_id: string
  offer_id: string
  revision: number
  units: number
}
export interface RewardExchangeOrder {
  id: string
  request_id: string
  user_id: string
  offer_id: string
  offer_revision: number
  units: number
  cost_quantity: number
  reward_quantity: number
  cost_ledger_id: string
  reward_record_id: string
  created_at: string
  snapshot: {
    name: string
    description: string
    cost_asset: RewardAssetSnapshot
    reward_asset: RewardAssetSnapshot
    cost_per_unit: number
    reward_per_unit: number
  }
}
export interface RewardExchangeRevision {
  offer_id: string
  revision: number
  snapshot: RewardExchangeOffer
  actor_id: string
  created_at: string
}
export interface RewardAssetOverview {
  id: string
  key: string
  name: string
  unit: string
  disabled: boolean
  balance: string
  granted: string
  consumed: string
  refunded: string
  reversed: string
  pending: string
  exchange_cost: string
  exchange_reward: string
}
export interface RewardRuleCatalogEntry extends ChallengeRewardRule {
  pack_id: string
  pack_name: string
  pack_slug: string
  version: string
  version_status: string
  current: boolean
  reward_name: string
  reward_key: string
}
export interface RewardRecordPage {
  records: RewardRecord[]
  next_cursor: string
}
export interface RewardLedgerPage {
  entries: RewardLedgerEntry[]
  next_cursor: string
}
export interface RewardUserBalance {
  user: AdminUser
  rewards: PlayerReward[]
}
export interface RewardRecordDetail {
  record: RewardRecord
  audits: AdminAuditLog[]
}
export interface RewardFilters {
  created_from?: string
  created_to?: string
  cursor?: string
  limit?: number
  status?: string
  source_type?: string
  source_id?: string
  user_id?: string
  reward_definition_id?: string
  purpose?: string
  actor_id?: string
}
export interface BuiltinRewardPreview {
  preview_token: string
  info: { name: string; version: string; content_hash: string }
  current_version?: { version: string; content_hash: string }
  current_rules: ChallengeRewardRule[]
  proposed_rules: {
    outcome: string
    reward: string
    calculator: string
    config: Record<string, unknown>
    enabled: boolean
  }[]
  rewards: {
    key: string
    before: Record<string, unknown> | null
    after: Record<string, unknown>
    conflict: boolean
    impact?: RewardDefinitionImpact
  }[]
}
