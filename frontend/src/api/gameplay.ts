import http from './index'
import type { ChallengePack, ChallengeVersionDocument } from '@/types'

export interface VersionReferences {
  conversations: number
  active_conversations: number
  campaigns: number
  active_campaigns: number
  generating: number
  rewards: number
}
export interface GameplayImportRecord {
  id: string
  status: 'preview' | 'imported' | 'expired' | 'cancelled'
  file_name: string
  content_hash: string
  target_slug: string
  strategy: string
  created_at: string
  expires_at: string
  result_version_id?: string
  request_id?: string
  preview: {
    manifest: { id: string; name: string; version: string }
    engine: { key: string; version: number }
    mode: string
    current?: { version: string }
    references?: VersionReferences
    outcomes: Array<{ outcome_key: string; title: string; reward_points: number }>
    differences: Array<{ path: string; before?: unknown; after?: unknown }>
    integrity_declared: boolean
    definitions: Array<{ kind: string; content: unknown }>
    definition_schemas: Record<string, unknown>
  }
}
export const gameplayAdminApi = {
  preview: (file: File, strategy: string, target_slug: string, trusted_update: boolean) => {
    const body = new FormData()
    body.append('file', file)
    body.append('strategy', strategy)
    body.append('target_slug', target_slug)
    body.append('trusted_update', String(trusted_update))
    return http
      .post<GameplayImportRecord>('/admin/gameplay-imports/preview', body)
      .then((r) => r.data)
  },
  confirm: (id: string, request_id: string) =>
    http
      .post<GameplayImportRecord>(`/admin/gameplay-imports/${id}/confirm`, { request_id })
      .then((r) => r.data),
  record: (id: string) =>
    http.get<GameplayImportRecord>(`/admin/gameplay-imports/${id}`).then((r) => r.data),
  records: (before?: string) =>
    http
      .get<{ imports: GameplayImportRecord[]; next_cursor: string }>('/admin/gameplay-imports', {
        params: { before },
      })
      .then((r) => r.data),
  cancel: (id: string) => http.delete(`/admin/gameplay-imports/${id}`),
  references: (id: string) =>
    http.get<VersionReferences>(`/admin/challenge-versions/${id}/references`).then((r) => r.data),
  current: (id: string, version_id: string, expected_revision: number, confirm_rollback: boolean) =>
    http
      .put<ChallengePack>(`/admin/challenges/${id}/current-version`, {
        version_id,
        expected_revision,
        confirm_rollback,
      })
      .then((r) => r.data),
  policy: (id: string, policy: string, reason: string, expected_revision: number) =>
    http
      .put<ChallengeVersionDocument>(`/admin/challenge-versions/${id}/run-policy`, {
        policy,
        reason,
        expected_revision,
      })
      .then((r) => r.data),
  retire: (id: string, expected_revision: number) =>
    http.post(`/admin/challenge-versions/${id}/retire`, { expected_revision }),
  download: async (id: string, name: string, format: 'json' | 'zip') => {
    const response = await http.get(`/admin/challenge-versions/${id}/export`, {
      params: { format },
      responseType: 'blob',
    })
    const url = URL.createObjectURL(response.data)
    const a = document.createElement('a')
    a.href = url
    a.download = `${name.replace(/[^a-zA-Z0-9._-]/g, '_')}.${format === 'zip' ? 'zip' : 'fatecraft.json'}`
    a.click()
    URL.revokeObjectURL(url)
  },
}
export function gameplayError(error: unknown): string {
  const e = error as { response?: { data?: { error?: string } }; message?: string }
  return e.response?.data?.error || e.message || '操作失败，请重试'
}
