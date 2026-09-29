import http from './index'
import type { AdminAuditLog } from '@/types'
import type {
  RewardExchangeOffer,
  RewardExchangeOfferView,
  RewardExchangeRequest,
  RewardExchangeOrder,
  RewardExchangeRevision,
  RewardAssetOverview,
  RewardRuleCatalogEntry,
} from '@/types/rewards'
import type {
  RewardRecord,
  RewardGrantRequest,
  RegistrationRewardRule,
  RewardDefinitionImpact,
  RewardRecordPage,
  RewardLedgerPage,
  RewardUserBalance,
  RewardRecordDetail,
  RewardFilters,
} from '@/types/rewards'

export const rewardManagementApi = {
  overview: () =>
    http
      .get<{ assets: RewardAssetOverview[] }>('/admin/rewards/overview')
      .then((r) => r.data.assets),
  rules: (reward_definition_id?: string, offset = 0) =>
    http
      .get<{ rules: RewardRuleCatalogEntry[]; has_more: boolean }>('/admin/rewards/rules', {
        params: { reward_definition_id, offset, limit: 30 },
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
    http.post<RewardRecord>(`/admin/users/${id}/reward-grants`, body).then((r) => r.data),
  request: (id: string) =>
    http.get<RewardRecord>(`/admin/reward-grants/by-request/${id}`).then((r) => r.data),
  records: (admin: boolean, params: RewardFilters = {}) =>
    http
      .get<RewardRecordPage>(admin ? '/admin/reward-records' : '/me/reward-records', { params })
      .then((r) => r.data),
  detail: (admin: boolean, id: string) =>
    admin
      ? http.get<RewardRecordDetail>(`/admin/reward-records/${id}`).then((r) => r.data)
      : http
          .get<RewardRecord>(`/me/reward-records/${id}`)
          .then((r) => ({ record: r.data, audits: [] }) as RewardRecordDetail),
  claim: (id: string) =>
    http.post<RewardRecord>(`/me/reward-records/${id}/claim`).then((r) => r.data),
  manage: (id: string, action: 'cancel' | 'reverse' | 'note', reason: string, requestId: string) =>
    http
      .post<RewardRecord>(`/admin/reward-records/${id}/${action}`, {
        reason,
        request_id: requestId,
      })
      .then((r) => r.data),
  ledger: (userId?: string, cursor?: string) =>
    http
      .get<RewardLedgerPage>(
        userId ? `/admin/users/${userId}/reward-ledger` : '/me/reward-ledger',
        { params: { limit: 30, cursor } },
      )
      .then((r) => r.data),
  impact: (id: string) =>
    http.get<RewardDefinitionImpact>(`/admin/reward-definitions/${id}/impact`).then((r) => r.data),
  registration: () =>
    http.get<RegistrationRewardRule>('/admin/registration-reward-rule').then((r) => r.data),
  saveRegistration: (body: RegistrationRewardRule) =>
    http.put<RegistrationRewardRule>('/admin/registration-reward-rule', body).then((r) => r.data),
}

export const rewardExchangeApi = {
  offers: (admin = false) =>
    http
      .get<{ offers: RewardExchangeOfferView[] }>(
        `${admin ? '/admin' : '/me'}/reward-exchange/offers`,
      )
      .then((r) => r.data.offers),
  save: (body: RewardExchangeOffer) =>
    (body.id
      ? http.put<RewardExchangeOffer>(`/admin/reward-exchange/offers/${body.id}`, body)
      : http.post<RewardExchangeOffer>('/admin/reward-exchange/offers', body)
    ).then((r) => r.data),
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
