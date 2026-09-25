import axios from 'axios'
import type {
  AdminAuthSettings,
  InvitationCode,
  CreatedInvitation,
  AdminSystemSettings,
  AdminLLMRequestLogSummary,
  AdminLLMRequestLog,
  AdminContextBuildLogSummary,
  AdminContextBuildLog,
  AdminChallengePack,
  AdminBuiltinChallenge,
  ChallengePack,
  ChallengeVersionDocument,
  Campaign,
  CampaignActionInput,
  CampaignActionResult,
  CampaignSnapshot,
  CampaignEvent,
  CreateCampaignInput,
  Conversation,
  ContextBuildLog,
  ConversationActivity,
  CreateConversationInput,
  DisplayMessage,
  ActiveGuidance,
  LLMSettings,
  LLMProviderProfile,
  LLMCapabilityCheckResponse,
  LLMCapabilityRole,
  CharacterInquiryAnswer,
  CharacterInquiryHistory,
  PublicChallenge,
  SaveChallengeVersionInput,
  BuiltinImportResult,
  StateRevision,
  UpdateConversationInput,
  PlayerReward,
  PublicRuntime,
  RewardDefinition,
  RewardLedgerEntry,
  GameplayModuleDescriptor,
  ConversationShare,
  ConversationSharePage,
  CreateConversationShareInput,
  PublicConversationShare,
  ConversationShareReport,
  AdminUser,
  AdminExternalIdentity,
  AdminAuditLog,
} from '@/types'
import type { AuthConfig, AuthUser, ExternalIdentity } from '@/types'

const http = axios.create({ baseURL: '/api', timeout: 30000, withCredentials: true })

export const guidanceApi = {
  async get(convId: string): Promise<ActiveGuidance | null> {
    const { data } = await http.get(`/conversations/${convId}/guidance`)
    return data.guidance
  },
  async select(convId: string, id: string, key: string, requestId: string) {
    await http.post(`/conversations/${convId}/guidance/select`, {
      guidance_id: id,
      item_key: key,
      request_id: requestId,
    })
  },
}

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
  fork: (id: string, title?: string) =>
    http.post<Conversation>(`/conversations/${id}/fork`, { title }).then((r) => r.data),
  update: (id: string, body: UpdateConversationInput) =>
    http.patch<Conversation>(`/conversations/${id}`, body).then((r) => r.data),
  remove: (id: string) => http.delete(`/conversations/${id}`).then((r) => r.data),
  end: (id: string) => http.post<Conversation>(`/conversations/${id}/end`).then((r) => r.data),
  archive: (id: string) =>
    http.post<Conversation>(`/conversations/${id}/archive`).then((r) => r.data),
  restore: (id: string) =>
    http.post<Conversation>(`/conversations/${id}/restore`).then((r) => r.data),
}

export const campaignsApi = {
  list: (params?: { status?: string }) =>
    http.get<{ campaigns: Campaign[] }>('/campaigns', { params }).then((r) => r.data.campaigns),
  get: (id: string) =>
    http.get<{ campaign: Campaign }>(`/campaigns/${id}`).then((r) => r.data.campaign),
  snapshot: (id: string) =>
    http
      .get<{ snapshot: CampaignSnapshot }>(`/campaigns/${id}/snapshot`)
      .then((r) => r.data.snapshot),
  create: (body: CreateCampaignInput) =>
    http.post<Campaign>('/campaigns', body).then((r) => r.data),
  events: (id: string, params?: { after_sequence?: number; limit?: number }) =>
    http
      .get<{ events: CampaignEvent[] }>(`/campaigns/${id}/events`, { params })
      .then((r) => r.data.events),
  conversations: (id: string) =>
    http
      .get<{ conversations: Conversation[] }>(`/campaigns/${id}/conversations`)
      .then((r) => r.data.conversations),
  createConversation: (id: string) =>
    http.post<Conversation>(`/campaigns/${id}/conversations`).then((r) => r.data),
  action: (id: string, body: CampaignActionInput) =>
    http.post<CampaignActionResult>(`/campaigns/${id}/actions`, body).then((r) => r.data),
  abandon: (id: string, body: { action_id: string; expected_revision: number }) =>
    http.post<CampaignActionResult>(`/campaigns/${id}/abandon`, body).then((r) => r.data),
}

// 消息
export const messagesApi = {
  list: (convId: string, params?: { before?: string; limit?: number }) =>
    http
      .get<{ messages: DisplayMessage[] }>(`/conversations/${convId}/messages`, { params })
      .then((r) => r.data.messages),
  send: (convId: string, content: string, requestId?: string) =>
    http
      .post(`/conversations/${convId}/messages`, { content, request_id: requestId })
      .then((r) => r.data),
  regenerate: (convId: string, requestId?: string) =>
    http.post(`/conversations/${convId}/regenerate`, { request_id: requestId }).then((r) => r.data),
  continue: (convId: string, requestId?: string) =>
    http.post(`/conversations/${convId}/continue`, { request_id: requestId }).then((r) => r.data),
  editAndRegenerate: (msgId: string, content: string, requestId?: string) =>
    http
      .post<{ status: string }>(`/messages/${msgId}/edit-and-regenerate`, {
        content,
        request_id: requestId,
      })
      .then((r) => r.data),
}

export const sharesApi = {
  create: (conversationId: string, body: CreateConversationShareInput) =>
    http
      .post<ConversationShare>(`/conversations/${conversationId}/shares`, body)
      .then((r) => r.data),
  listOwned: (params?: { status?: string; search?: string; cursor?: string; page_size?: number }) =>
    http.get<ConversationSharePage>('/conversation-shares', { params }).then((r) => r.data),
  revoke: (id: string) => http.delete(`/conversation-shares/${id}`).then((r) => r.data),
  replace: (id: string, body: CreateConversationShareInput) =>
    http.post<ConversationShare>(`/conversation-shares/${id}/replace`, body).then((r) => r.data),
  public: (token: string) =>
    http
      .get<PublicConversationShare>(`/public/conversation-shares/${encodeURIComponent(token)}`)
      .then((r) => r.data),
  report: (token: string, body: { reason: string; details: string }) =>
    http
      .post(`/public/conversation-shares/${encodeURIComponent(token)}/reports`, body)
      .then((r) => r.data),
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

export const runtimeApi = {
  get: (convId: string) =>
    http
      .get<{ runtime: PublicRuntime }>(`/conversations/${convId}/runtime`)
      .then((response) => response.data.runtime),
}

export const rewardsApi = {
  list: () =>
    http.get<{ rewards: PlayerReward[] }>('/me/rewards').then((response) => response.data.rewards),
  ledger: (limit = 50) =>
    http
      .get<{ entries: RewardLedgerEntry[] }>('/me/reward-ledger', { params: { limit } })
      .then((response) => response.data.entries),
}

// 上下文
export const contextApi = {
  last: (convId: string) =>
    http
      .get<ContextBuildLog>(`/conversations/${convId}/context/last`)
      .then((r) => (r.status === 204 ? null : r.data)),
  get: (id: string) => http.get<ContextBuildLog>(`/context/${id}`).then((r) => r.data),
}

export const activityApi = {
  get: (convId: string) =>
    http.get<ConversationActivity>(`/conversations/${convId}/activity`).then((r) => r.data),
}
export const runtimeInspectorApi = {
  get: (convId: string) =>
    http
      .get<import('@/types').RuntimeInspector>(`/conversations/${convId}/runtime-inspector`)
      .then((r) => r.data),
}

export const inquiriesApi = {
  history: (convId: string) =>
    http
      .get<CharacterInquiryHistory>(`/conversations/${convId}/inquiries`)
      .then((response) => response.data),
  ask: (convId: string, question: string) =>
    http
      .post<CharacterInquiryAnswer>(
        `/conversations/${convId}/inquiries`,
        { question },
        { timeout: 130000 },
      )
      .then((response) => response.data),
  cancel: (convId: string) =>
    http.post(`/conversations/${convId}/inquiries/cancel`).then((response) => response.data),
  clear: (convId: string) =>
    http.delete(`/conversations/${convId}/inquiries`).then((response) => response.data),
}

export const challengesApi = {
  list: () =>
    http.get<{ challenges: PublicChallenge[] }>('/challenges').then((r) => r.data.challenges),
  get: (slug: string) => http.get<PublicChallenge>(`/challenges/${slug}`).then((r) => r.data),
}

// 设置
export const settingsApi = {
  get: () => http.get<LLMSettings>('/settings/llm').then((r) => r.data),
  providers: () =>
    http
      .get<{ providers: LLMProviderProfile[] }>('/settings/llm/providers')
      .then((r) => r.data.providers),
  update: (body: Record<string, any>) =>
    http.patch<LLMSettings>('/settings/llm', body).then((r) => r.data),
  // 通过后端代理拉取上游可用模型列表，避免跨域
  fetchModels: (body: { base_url: string; api_key?: string }) =>
    http.post<{ models: string[] }>('/settings/fetch-models', body).then((r) => r.data),
  checkCapabilities: (role: LLMCapabilityRole) =>
    http
      .post<LLMCapabilityCheckResponse>(
        '/settings/llm/capabilities/check',
        { role },
        { timeout: 180000 },
      )
      .then((r) => r.data),
}

// 管理员全局玩法设置
export const adminApi = {
  // 用户与第三方身份列表由后端限制为每页最多 200 条。
  listUsers: (params: { limit?: number; offset?: number } = { limit: 200 }) =>
    http.get<{ users: AdminUser[] }>('/admin/users', { params }).then((r) => r.data.users),
  listAuthIdentities: (params: { limit?: number; offset?: number } = { limit: 200 }) =>
    http
      .get<{ identities: AdminExternalIdentity[] }>('/admin/auth-identities', { params })
      .then((r) => r.data.identities),
  updateUserRole: (id: string, role: AdminUser['role']) =>
    http.patch<AdminUser>(`/admin/users/${id}/role`, { role }).then((r) => r.data),
  updateUserStatus: (id: string, status: AdminUser['status']) =>
    http.patch<AdminUser>(`/admin/users/${id}/status`, { status }).then((r) => r.data),
  listAuditLogs: (limit = 50) =>
    http
      .get<{ logs: AdminAuditLog[] }>('/admin/audit-logs', { params: { limit } })
      .then((r) => r.data.logs),
  getSystemSettings: () =>
    http.get<AdminSystemSettings>('/admin/system-settings').then((r) => r.data),
  updateSystemSettings: (body: {
    roll_system_enabled: boolean
    roll_difficulty_mode: string
    detailed_llm_logging_enabled?: boolean
    diagnostic_log_cleanup_enabled?: boolean
    diagnostic_log_retention_days?: number
    llm_access_mode?: 'user_managed' | 'platform_managed'
    public_llm_base_url?: string
    public_llm_provider?: string
    public_llm_protocol?: 'chat_completions' | 'responses'
    public_llm_api_key?: string
    clear_public_llm_api_key?: boolean
    public_llm_model?: string
    public_llm_memory_model?: string
    public_llm_inquiry_model?: string
    public_llm_max_context_tokens?: number
    public_llm_max_response_tokens?: number
    public_llm_temperature?: number
    public_llm_top_p?: number
    public_llm_stream_enabled?: boolean
    public_llm_reasoning_effort?: string
    public_llm_memory_reasoning_effort?: string
    public_llm_inquiry_reasoning_effort?: string
  }) => http.patch<AdminSystemSettings>('/admin/system-settings', body).then((r) => r.data),
  fetchPublicModels: (body: { base_url: string; api_key?: string }) =>
    http
      .post<{ models: string[] }>('/admin/system-settings/fetch-models', body)
      .then((r) => r.data),
  listLLMRequestLogs: (params?: { limit?: number; conversation_id?: string }) =>
    http
      .get<{ logs: AdminLLMRequestLogSummary[] }>('/admin/llm-request-logs', { params })
      .then((r) => r.data.logs),
  getLLMRequestLog: (id: string) =>
    http.get<AdminLLMRequestLog>(`/admin/llm-request-logs/${id}`).then((r) => r.data),
  listContextBuildLogs: (params?: { limit?: number; conversation_id?: string }) =>
    http
      .get<{ logs: AdminContextBuildLogSummary[] }>('/admin/context-build-logs', { params })
      .then((r) => r.data.logs),
  getContextBuildLog: (id: string) =>
    http.get<AdminContextBuildLog>(`/admin/context-build-logs/${id}`).then((r) => r.data),
  listConversationShares: (params?: { status?: string; limit?: number }) =>
    http
      .get<{ shares: ConversationShare[] }>('/admin/conversation-shares', { params })
      .then((r) => r.data.shares),
  moderateConversationShare: (id: string, reason: string) =>
    http.post(`/admin/conversation-shares/${id}/moderate`, { reason }).then((r) => r.data),
  listShareReports: (params?: { status?: string; limit?: number }) =>
    http
      .get<{ reports: ConversationShareReport[] }>('/admin/share-reports', { params })
      .then((r) => r.data.reports),
  resolveShareReport: (id: string, status: 'resolved' | 'dismissed') =>
    http.post(`/admin/share-reports/${id}/resolve`, { status }).then((r) => r.data),
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
  importBuiltin: (slug: string, overwrite = false) =>
    http
      .post<BuiltinImportResult>(`/admin/builtin-challenges/${encodeURIComponent(slug)}/import`, {
        overwrite,
      })
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
  listGameplayModules: () =>
    http
      .get<{ modules: GameplayModuleDescriptor[] }>('/admin/gameplay-modules')
      .then((response) => response.data.modules),
  listRewardDefinitions: () =>
    http
      .get<{ reward_definitions: RewardDefinition[] }>('/admin/reward-definitions')
      .then((response) => response.data.reward_definitions),
  createRewardDefinition: (body: Partial<RewardDefinition>) =>
    http
      .post<RewardDefinition>('/admin/reward-definitions', body)
      .then((response) => response.data),
  updateRewardDefinition: (id: string, body: Partial<RewardDefinition>) =>
    http
      .patch<RewardDefinition>(`/admin/reward-definitions/${id}`, body)
      .then((response) => response.data),
}

// 认证
export const authApi = {
  config: () => http.get<AuthConfig>('/auth/config').then((r) => r.data),
  me: () => http.get<{ user: AuthUser }>('/auth/me').then((r) => r.data.user),
  login: (body: { identifier: string; password: string }) =>
    http.post<{ user: AuthUser }>('/auth/login', body).then((r) => r.data.user),
  sendRegistrationCode: (email: string) =>
    http.post('/auth/email-code', { email }).then((r) => r.data),
  register: (body: {
    email: string
    username: string
    password: string
    display_name: string
    verification_code?: string
    invitation_code?: string
  }) => http.post<{ user: AuthUser }>('/auth/register', body).then((r) => r.data.user),
  logout: () => http.post('/auth/logout').then((r) => r.data),
}

export const setupApi = {
  status: () => http.get<{ initialized: boolean }>('/setup/status').then((r) => r.data),
  initialize: (body: { email: string; password: string; username: string; confirm: boolean }) =>
    http
      .post<{ initialized: boolean; user: AuthUser }>('/setup/initialize', body)
      .then((r) => r.data),
}

export const identityApi = {
  list: () =>
    http.get<{ identities: ExternalIdentity[] }>('/auth/identities').then((r) => r.data.identities),
  bindLinuxDOURL: (returnTo = '/settings') =>
    `/api/auth/linuxdo/bind/start?${new URLSearchParams({ return_to: returnTo }).toString()}`,
  unbind: (id: string) => http.delete(`/auth/identities/${id}`).then((r) => r.data),
}

export default http
