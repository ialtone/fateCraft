import type { RewardExchangeOrder, RewardExchangeRequest } from '@/types/rewards'

// An old order lookup must never resolve a different in-flight exchange.
export function confirmsPendingExchange(
  pending: RewardExchangeRequest | undefined,
  order: RewardExchangeOrder,
): boolean {
  return !!pending && pending.request_id === order.request_id
}

export function validExchangeRequestID(value: string): boolean {
  return (
    /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value.trim()) &&
    value.trim() !== '00000000-0000-0000-0000-000000000000'
  )
}
