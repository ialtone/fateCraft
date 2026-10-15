import type { AdminUser, PlayerReward, RewardLedgerEntry, ChallengeOutcomeRule } from './api'
export type RewardKey = 'fatecraft.points' | 'fatecraft.message-rewrite-credit'
export interface RewardGrantRequest {
  request_id: string
  reward_key: RewardKey
  quantity: number
  reason: string
  public_note: string
}
export interface RegistrationRewardRule {
  id: number
  points: number
  rewrite_credits: number
  actor_id?: string
  created_at: string
}
export interface RewardExchangeOffer {
  id: string
  name: string
  description: string
  cost_quantity: number
  reward_quantity: number
  enabled: boolean
  revision: number
  sort_order: number
  created_at: string
  updated_at: string
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
  reward_ledger_id: string
  created_at: string
  snapshot: { name: string; description: string; cost_per_unit: number; reward_per_unit: number }
}
export interface RewardExchangeRevision {
  offer_id: string
  revision: number
  snapshot: RewardExchangeOffer
  actor_id: string | null
  created_at: string
}
export interface RewardRuleCatalogEntry extends ChallengeOutcomeRule {
  pack_id: string
  pack_name: string
  pack_slug: string
  version: string
  version_status: string
  current: boolean
}
export interface RewardLedgerPage {
  entries: RewardLedgerEntry[]
  next_cursor: string
}
export interface RewardUserBalance {
  user: AdminUser
  rewards: PlayerReward[]
}
export interface RewardFilters {
  pack_id?: string
  version_id?: string
  created_from?: string
  created_to?: string
  cursor?: string
  limit?: number
  source_type?: string
  user_id?: string
  reward_key?: RewardKey
}
