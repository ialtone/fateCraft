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
  kind: RewardExchangeKind
  tutorial: string
  cost_quantity: number
  reward_quantity: number
  max_redemptions_per_user: number
  enabled: boolean
  revision: number
  sort_order: number
  created_at: string
  updated_at: string
  redeemed_units?: number
  remaining_units?: number | null
  stock_total?: number
  stock_remaining?: number
}
export type RewardExchangeKind = 'cdk' | 'reward'
export type RewardExchangeStockStatus = 'available' | 'redeemed' | 'removed' | 'voided'
export interface RewardExchangeStock {
  id: string
  offer_id: string
  code: string
  status: RewardExchangeStockStatus
  order_id: string | null
  user_id: string | null
  username?: string
  redeemed_at: string | null
  created_at: string
  updated_at: string
}
export type RewardExchangeTicketStatus = 'open' | 'rejected' | 'refunded'
export interface RewardExchangeTicket {
  id: string
  order_id: string
  user_id: string
  username?: string
  status: RewardExchangeTicketStatus
  created_at: string
  updated_at: string
  entries: {
    id: string
    kind: 'request' | 'reject' | 'refund'
    actor_id: string
    body: string
    created_at: string
  }[]
  order?: RewardExchangeOrder
}
export interface PointsCDKBatch {
  id: string
  name: string
  points: number
  quantity: number
  expires_at: string
  created_at: string
  redeemed: number
}
export interface PointsCDK {
  code: string
  batch_id: string
  redeemed_by: string | null
  redeemed_at: string | null
  username?: string
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
  username?: string
  offer_id: string
  offer_revision: number
  units: number
  cost_quantity: number
  reward_quantity: number
  cost_ledger_id: string
  reward_ledger_id: string | null
  created_at: string
  ticket_status?: RewardExchangeTicketStatus | ''
  snapshot: {
    name: string
    description: string
    kind?: RewardExchangeKind
    tutorial?: string
    codes?: string[]
    cost_per_unit: number
    reward_per_unit: number
  }
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
