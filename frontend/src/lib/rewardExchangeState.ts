import type { RewardExchangeOrder, RewardExchangeRequest } from '@/types/rewards'

export function maxExchangeUnits(
  offer: {
    cost_quantity: number
    reward_quantity: number
    remaining_units?: number | null
    stock_remaining?: number
  },
  balance: number,
  pending: boolean,
): number {
  const limits = [
    Math.floor(Number.MAX_SAFE_INTEGER / offer.cost_quantity),
    Math.floor(Number.MAX_SAFE_INTEGER / offer.reward_quantity),
  ]
  if (!pending && offer.remaining_units !== null && offer.remaining_units !== undefined) {
    limits.push(Math.max(0, offer.remaining_units))
  }
  if (!pending) limits.push(Math.max(0, offer.stock_remaining ?? 0))
  // A pending request must stay retryable even if its first attempt already
  // succeeded and the latest balance and quota reflect the deduction.
  if (!pending) limits.push(Math.floor(Math.max(0, balance) / offer.cost_quantity))
  return Math.min(...limits)
}

// An old order lookup must never resolve a different in-flight exchange.
export function confirmsPendingExchange(
  pending: RewardExchangeRequest | undefined,
  order: RewardExchangeOrder,
): boolean {
  return (
    !!pending &&
    pending.request_id === order.request_id &&
    pending.offer_id === order.offer_id &&
    pending.revision === order.offer_revision &&
    pending.units === order.units
  )
}

export function validExchangeRequestID(value: string): boolean {
  return (
    /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value.trim()) &&
    value.trim() !== '00000000-0000-0000-0000-000000000000'
  )
}
