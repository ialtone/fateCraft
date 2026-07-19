import axios from 'axios'
import type {
  Character,
  Conversation,
  ContextBuildLog,
  CreateConversationInput,
  DisplayMessage,
  EditMessageInput,
  LLMSettings,
  MemorySummary,
  Persona,
  PromptPreset,
  PromptSegment,
  Scenario,
  StateRevision,
  UpdateConversationInput,
  WorldBook,
  WorldBookEntry,
} from '@/types'

const http = axios.create({ baseURL: '/api', timeout: 30000 })

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
    http
      .get<{ conversation: Conversation; world_book_ids: string[] }>(`/conversations/${id}`)
      .then((r) => r.data),
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
  patch: (convId: string, body: { patch?: Record<string, any>; state?: Record<string, any> }) =>
    http
      .patch<{ state: Record<string, any>; skipped?: string[] }>(
        `/conversations/${convId}/state`,
        body,
      )
      .then((r) => r.data),
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

// 角色卡 / Persona / 场景
export const charactersApi = {
  list: () => http.get<{ characters: Character[] }>('/characters').then((r) => r.data.characters),
  get: (id: string) => http.get<Character>(`/characters/${id}`).then((r) => r.data),
  create: (body: Record<string, any>) =>
    http.post<Character>('/characters', body).then((r) => r.data),
  update: (id: string, body: Record<string, any>) =>
    http.patch<Character>(`/characters/${id}`, body).then((r) => r.data),
  remove: (id: string) => http.delete(`/characters/${id}`).then((r) => r.data),
}

export const personasApi = {
  list: () => http.get<{ personas: Persona[] }>('/personas').then((r) => r.data.personas),
  create: (body: Record<string, any>) => http.post<Persona>('/personas', body).then((r) => r.data),
}

export const scenariosApi = {
  list: () => http.get<{ scenarios: Scenario[] }>('/scenarios').then((r) => r.data.scenarios),
}

// Prompt 预设
export const promptsApi = {
  list: () => http.get<{ prompts: PromptPreset[] }>('/prompts').then((r) => r.data.prompts),
  get: (id: string) => http.get<PromptPreset>(`/prompts/${id}`).then((r) => r.data),
  create: (body: Record<string, any>) =>
    http.post<PromptPreset>('/prompts', body).then((r) => r.data),
  update: (id: string, body: Record<string, any>) =>
    http.patch<PromptPreset>(`/prompts/${id}`, body).then((r) => r.data),
  remove: (id: string) => http.delete(`/prompts/${id}`).then((r) => r.data),
  addSegment: (presetId: string, body: Record<string, any>) =>
    http.post<PromptSegment>(`/prompts/${presetId}/segments`, body).then((r) => r.data),
  updateSegment: (id: string, body: Record<string, any>) =>
    http.patch<PromptSegment>(`/prompt-segments/${id}`, body).then((r) => r.data),
  removeSegment: (id: string) => http.delete(`/prompt-segments/${id}`).then((r) => r.data),
}

// 世界书
export const worldbooksApi = {
  list: () => http.get<{ worldbooks: WorldBook[] }>('/worldbooks').then((r) => r.data.worldbooks),
  get: (id: string) => http.get<WorldBook>(`/worldbooks/${id}`).then((r) => r.data),
  create: (body: Record<string, any>) =>
    http.post<WorldBook>('/worldbooks', body).then((r) => r.data),
  update: (id: string, body: Record<string, any>) =>
    http.patch<WorldBook>(`/worldbooks/${id}`, body).then((r) => r.data),
  remove: (id: string) => http.delete(`/worldbooks/${id}`).then((r) => r.data),
  addEntry: (bookId: string, body: Record<string, any>) =>
    http.post<WorldBookEntry>(`/worldbooks/${bookId}/entries`, body).then((r) => r.data),
  updateEntry: (id: string, body: Record<string, any>) =>
    http.patch<WorldBookEntry>(`/worldbook-entries/${id}`, body).then((r) => r.data),
  removeEntry: (id: string) => http.delete(`/worldbook-entries/${id}`).then((r) => r.data),
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

// 账号（天道回溯）
export const accountApi = {
  get: () => http.get('/account').then((r) => r.data),
  useRewind: () =>
    http.post<{ success: boolean; account: any }>('/account/rewind/use').then((r) => r.data),
}

export default http
