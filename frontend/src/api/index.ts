import axios from 'axios'
import type {
  AdminAuthSettings,
  InvitationCode,
  CreatedInvitation,
  AdminRollSettings,
  AdminChallengePack,
  AdminBuiltinChallenge,
  ChallengePack,
  ChallengeVersionDocument,
  Conversation,
  ContextBuildLog,
  CreateConversationInput,
  DisplayMessage,
  EditMessageInput,
  LLMSettings,
  MemorySummary,
  PublicChallenge,
  SaveChallengeVersionInput,
  BuiltinImportResult,
  StateRevision,
  UpdateConversationInput,
} from '@/types'
import type { AuthConfig, AuthUser } from '@/types'

const http = axios.create({ baseURL: '/api', timeout: 30000, withCredentials: true })

// 全局响应拦截器：提取后端返回的 error 字段，统一错误消息。
http.interceptors.response.use(
  (res) => res,
  (err) => {
    const backendMsg = err.response?.data?.error
    if (backendMsg) err.message = backendMsg
    return Promise.reject(err)
  },
)

// 对话
export const conversationsApi = {
  list: (params?: { status?: string; search?: string }) =>
    http
      .get<{ conversations: Conversation[] }>('/conversations', { params })
      .then((r) => r.data.conversations),
  get: (id: string) =>
    http.get<{ conversation: Conversation }>(`/conversations/${id}`).then((r) => r.data),
  create: (body: CreateConversationInput) =>
    http.post<Conversation>('/conversations', body).then((r) => r.data),
  update: (id: string, body: UpdateConversationInput) =>
    http.patch<Conversation>(`/conversations/${id}`, body).then((r) => r.data),
  remove: (id: string) => http.delete(`/conversations/${id}`).then((r) => r.data),
  end: (id: string) => http.post<Conversation>(`/conversations/${id}/end`).then((r) => r.data),
  archive: (id: string) =>
    http.post<Conversation>(`/conversations/${id}/archive`).then((r) => r.data),
  restore: (id: string) =>
    http.post<Conversation>(`/conversations/${id}/restore`).then((r) => r.data),
}

// 消息
export const messagesApi = {
  list: (convId: string, params?: { before?: string; limit?: number }) =>
    http
      .get<{ messages: DisplayMessage[] }>(`/conversations/${convId}/messages`, { params })
      .then((r) => r.data.messages),
  send: (convId: string, content: string) =>
    http.post(`/conversations/${convId}/messages`, { content }).then((r) => r.data),
  regenerate: (convId: string) =>
    http.post(`/conversations/${convId}/regenerate`).then((r) => r.data),
  continue: (convId: string) => http.post(`/conversations/${convId}/continue`).then((r) => r.data),
  edit: (msgId: string, body: EditMessageInput) =>
    http.patch<DisplayMessage>(`/messages/${msgId}`, body).then((r) => r.data),
  editAndRegenerate: (msgId: string, content: string) =>
    http
      .post<{ status: string; message: DisplayMessage }>(`/messages/${msgId}/edit-and-regenerate`, {
        content,
      })
      .then((r) => r.data),
  remove: (msgId: string) => http.delete(`/messages/${msgId}`).then((r) => r.data),
}

// 状态
export const stateApi = {
  get: (convId: string) =>
    http
      .get<{ state: Record<string, any> }>(`/conversations/${convId}/state`)
      .then((r) => r.data.state),
  revisions: (convId: string) =>
    http
      .get<{ revisions: StateRevision[] }>(`/conversations/${convId}/state/revisions`)
      .then((r) => r.data.revisions),
}

// 上下文
export const contextApi = {
  last: (convId: string) =>
    http.get<ContextBuildLog>(`/conversations/${convId}/context/last`).then((r) => r.data),
  get: (id: string) => http.get<ContextBuildLog>(`/context/${id}`).then((r) => r.data),
}

// 摘要
export const summaryApi = {
  list: (convId: string) =>
    http
      .get<{ summaries: MemorySummary[] }>(`/conversations/${convId}/summaries`)
      .then((r) => r.data.summaries),
  generate: (convId: string) =>
    http.post<MemorySummary>(`/conversations/${convId}/summaries`).then((r) => r.data),
  edit: (id: string, conversationId: string, content: string) =>
    http
      .patch<MemorySummary>(`/summaries/${id}`, { conversation_id: conversationId, content })
      .then((r) => r.data),
}

export const challengesApi = {
  list: () =>
    http.get<{ challenges: PublicChallenge[] }>('/challenges').then((r) => r.data.challenges),
  get: (slug: string) => http.get<PublicChallenge>(`/challenges/${slug}`).then((r) => r.data),
}

// 设置
export const settingsApi = {
  get: () => http.get<LLMSettings>('/settings/llm').then((r) => r.data),
  update: (body: Record<string, any>) =>
    http.patch<LLMSettings>('/settings/llm', body).then((r) => r.data),
  // 通过后端代理拉取上游可用模型列表，避免跨域
  fetchModels: (body: { base_url: string; api_key?: string }) =>
    http.post<{ models: string[] }>('/settings/fetch-models', body).then((r) => r.data),
}

// 管理员全局玩法设置
export const adminApi = {
  getRollSettings: () => http.get<AdminRollSettings>('/admin/roll-settings').then((r) => r.data),
  updateRollSettings: (body: { roll_system_enabled: boolean; roll_difficulty_mode: string }) =>
    http.patch<AdminRollSettings>('/admin/roll-settings', body).then((r) => r.data),
  getAuthSettings: () => http.get<AdminAuthSettings>('/admin/auth-settings').then((r) => r.data),
  updateAuthSettings: (body: Record<string, unknown>) =>
    http.patch<AdminAuthSettings>('/admin/auth-settings', body).then((r) => r.data),
  testEmail: (email: string) =>
    http.post('/admin/auth-settings/test-email', { email }).then((r) => r.data),
  listInvitationCodes: () =>
    http
      .get<{ invitation_codes: InvitationCode[] }>('/admin/invitation-codes')
      .then((r) => r.data.invitation_codes),
  createInvitationCode: (body: { label: string; max_uses: number; expires_at?: string }) =>
    http.post<CreatedInvitation>('/admin/invitation-codes', body).then((r) => r.data),
  updateInvitationCode: (id: string, body: Record<string, unknown>) =>
    http.patch<InvitationCode>(`/admin/invitation-codes/${id}`, body).then((r) => r.data),
  listBuiltins: () =>
    http
      .get<{ builtins: AdminBuiltinChallenge[] }>('/admin/builtin-challenges')
      .then((r) => r.data.builtins),
  importBuiltin: (slug: string) =>
    http
      .post<BuiltinImportResult>(`/admin/builtin-challenges/${encodeURIComponent(slug)}/import`)
      .then((r) => r.data),
  listChallenges: () =>
    http
      .get<{ challenges: AdminChallengePack[] }>('/admin/challenges')
      .then((r) => r.data.challenges),
  createChallenge: (body: { slug: string; name: string; description: string }) =>
    http.post<ChallengePack>('/admin/challenges', body).then((r) => r.data),
  getChallengeVersion: (id: string) =>
    http.get<ChallengeVersionDocument>(`/admin/challenge-versions/${id}`).then((r) => r.data),
  createChallengeVersion: (packId: string, version: string) =>
    http
      .post<ChallengeVersionDocument>(`/admin/challenges/${packId}/versions`, { version })
      .then((r) => r.data),
  saveChallengeVersion: (id: string, body: SaveChallengeVersionInput) =>
    http.put<ChallengeVersionDocument>(`/admin/challenge-versions/${id}`, body).then((r) => r.data),
  publishChallengeVersion: (id: string) =>
    http
      .post<ChallengeVersionDocument>(`/admin/challenge-versions/${id}/publish`)
      .then((r) => r.data),
}

// 账号（天道回溯）
export const accountApi = {
  get: () => http.get('/account').then((r) => r.data),
  useRewind: () =>
    http.post<{ success: boolean; account: any }>('/account/rewind/use').then((r) => r.data),
}

// 认证
export const authApi = {
  config: () => http.get<AuthConfig>('/auth/config').then((r) => r.data),
  me: () => http.get<{ user: AuthUser }>('/auth/me').then((r) => r.data.user),
  login: (body: { email: string; password: string }) =>
    http.post<{ user: AuthUser }>('/auth/login', body).then((r) => r.data.user),
  sendRegistrationCode: (email: string) =>
    http.post('/auth/email-code', { email }).then((r) => r.data),
  register: (body: {
    email: string
    password: string
    display_name: string
    verification_code?: string
    invitation_code?: string
  }) => http.post<{ user: AuthUser }>('/auth/register', body).then((r) => r.data.user),
  logout: () => http.post('/auth/logout').then((r) => r.data),
}

export default http
