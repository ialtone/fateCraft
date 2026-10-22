import http from './index'
import type { AdminAuditLog, RewardLedgerEntry } from '@/types'
import type {
  RewardExchangeOffer,
  RewardExchangeRequest,
  RewardExchangeOrder,
  RewardExchangeRevision,
  RewardRuleCatalogEntry,
  RewardGrantRequest,
  RegistrationRewardRule,
  RewardLedgerPage,
  RewardUserBalance,
  RewardFilters,
} from '@/types/rewards'
export const rewardManagementApi = {
  rules: (offset = 0) =>
    http
      .get<{ rules: RewardRuleCatalogEntry[]; has_more: boolean }>('/admin/rewards/rules', {
        params: { offset, limit: 30 },
      })
      .then((r) => r.data),
  audits: (cursor?: string) =>
    http
      .get<{ audits: AdminAuditLog[]; next_cursor: string }>('/admin/rewards/audits', {
        params: { cursor, limit: 30 },
      })
      .then((r) => r.data),
  user: (id: string) =>
    http.get<RewardUserBalance>(`/admin/users/${id}/rewards`).then((r) => r.data),
  grant: (id: string, body: RewardGrantRequest) =>
    http.post<RewardLedgerEntry>(`/admin/users/${id}/reward-grants`, body).then((r) => r.data),
  request: (id: string) =>
    http.get<RewardLedgerEntry>(`/admin/reward-grants/by-request/${id}`).then((r) => r.data),
  reverse: (id: string, reason: string, request_id: string) =>
    http
      .post<RewardLedgerEntry>(`/admin/reward-ledger/${id}/reverse`, { reason, request_id })
      .then((r) => r.data),
  ledger: (admin = false, params: RewardFilters = {}) =>
    http
      .get<RewardLedgerPage>(admin ? '/admin/reward-ledger' : '/me/reward-ledger', {
        params: { limit: 30, ...params },
      })
      .then((r) => r.data),
  registration: () =>
    http.get<RegistrationRewardRule>('/admin/registration-reward-rule').then((r) => r.data),
  saveRegistration: (body: RegistrationRewardRule) =>
    http
      .put<RegistrationRewardRule>('/admin/registration-reward-rule', {
        id: body.id,
        points: body.points,
        rewrite_credits: body.rewrite_credits,
      })
      .then((r) => r.data),
}

export const rewardExchangeApi = {
  offers: (admin = false) =>
    http
      .get<{ offers: RewardExchangeOffer[] }>(`${admin ? '/admin' : '/me'}/reward-exchange/offers`)
      .then((r) => r.data.offers),
  save: (offer: RewardExchangeOffer) => {
    const {
      name,
      description,
      cost_quantity,
      reward_quantity,
      max_redemptions_per_user,
      enabled,
      sort_order,
      revision,
    } = offer
    const body = {
      name,
      description,
      cost_quantity,
      reward_quantity,
      max_redemptions_per_user,
      enabled,
      sort_order,
      revision,
    }
    return (
      offer.id
        ? http.put<RewardExchangeOffer>(`/admin/reward-exchange/offers/${offer.id}`, body)
        : http.post<RewardExchangeOffer>('/admin/reward-exchange/offers', body)
    ).then((r) => r.data)
  },
  exchange: (body: RewardExchangeRequest) =>
    http.post<RewardExchangeOrder>('/me/reward-exchange/orders', body).then((r) => r.data),
  request: (id: string) =>
    http.get<RewardExchangeOrder>(`/me/reward-exchange/by-request/${id}`).then((r) => r.data),
  orders: (admin = false, cursor?: string, user_id?: string) =>
    http
      .get<{ orders: RewardExchangeOrder[]; next_cursor: string }>(
        `${admin ? '/admin' : '/me'}/reward-exchange/orders`,
        { params: { cursor, user_id, limit: 30 } },
      )
      .then((r) => r.data),
  revisions: (id: string) =>
    http
      .get<{ revisions: RewardExchangeRevision[] }>(`/admin/reward-exchange/offers/${id}/revisions`)
      .then((r) => r.data.revisions),
}
