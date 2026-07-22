// ============================================================================
// 枚举类型
// ============================================================================

/** 消息角色类型 */
export type MessageRoleEnum = 'system' | 'user' | 'assistant' | 'tool' | 'internal'

/** 状态修订来源 */
export type RevisionSourceEnum = 'ai' | 'user' | 'system'

/** 判定难度模式（仅管理员全局设置） */
export type DifficultyModeEnum = 'easy' | 'normal' | 'hard'

/** 对话状态类型 */
export type ConversationStatusEnum = 'active' | 'ended' | 'archived' | 'deleted'

/** 展示消息类型（roll_result 为天命判定结果，对照 backend roll_flow.go 的 AppendSystemChatEvent） */
export type DisplayMessageTypeEnum =
  'user' | 'assistant' | 'system' | 'event' | 'state' | 'roll_result'

// ============================================================================
// 对话与运行时数据
// ============================================================================

/** 对话 - 一次完整的对话会话 */
export interface Conversation {
  /** 对话 ID */
  id: string
  /** 玩家 ID */
  player_id: string
  /** 对话标题 */
  title: string
  /** 对话状态 */
  status: ConversationStatusEnum
  /** 是否正在生成中 */
  is_processing: boolean
  /** 创建时固定的挑战版本与内容哈希 */
  challenge_version_id: string
  challenge_hash: string
  character_asset_id?: string
  persona_asset_id?: string
  scenario_asset_id?: string
  /** 本次轮回使用的天道回溯次数 */
  rewinds_used_this_trial: number
  /** 创建时间 */
  created_at: string
  /** 更新时间 */
  updated_at: string
  /** 结束时间 */
  ended_at?: string
  /** 归档时间 */
  archived_at?: string
  /** 删除时间 */
  deleted_at?: string
}

/** 聊天消息 - 内部存储的原始消息 */
export interface ChatMessage {
  /** 消息 ID */
  id: string
  /** 所属对话 ID */
  conversation_id: string
  /** 角色类型 */
  role: MessageRoleEnum
  /** 消息内容 */
  content: string
  /** 是否隐藏 */
  hidden: boolean
  /** Token 数量 */
  token_count?: number
  /** 父消息 ID */
  parent_message_id?: string
  /** 创建时间 */
  created_at: string
  /** 更新时间 */
  updated_at: string
}

/** 展示消息 - 前端显示的消息 */
export interface DisplayMessage {
  /** 消息 ID */
  id: string
  /** 所属对话 ID */
  conversation_id: string
  /** 关联的聊天消息 ID */
  chat_message_id?: string
  /** 消息类型 */
  type: DisplayMessageTypeEnum
  /** Markdown 格式的消息内容 */
  markdown: string
  /** 元数据 */
  metadata: Record<string, any>
  /** 创建时间 */
  created_at: string
}

/** 状态修订 - 状态变更的历史记录 */
export interface StateRevision {
  /** 修订 ID */
  id: string
  /** 所属对话 ID */
  conversation_id: string
  /** 来源 */
  source: RevisionSourceEnum
  /** 关联的消息 ID */
  related_message_id?: string
  /** 状态补丁 */
  patch: Record<string, any>
  /** 修改前的状态 */
  before_state: Record<string, any>
  /** 修改后的状态 */
  after_state: Record<string, any>
  /** 创建时间 */
  created_at: string
}

/** 记忆摘要 - 对话历史的摘要 */
export interface MemorySummary {
  /** 摘要 ID */
  id: string
  /** 所属对话 ID */
  conversation_id: string
  /** 版本号 */
  version: number
  /** 摘要内容 */
  content: string
  /** 摘要覆盖到的最后一条消息 ID */
  covered_until_message_id?: string
  /** Token 数量 */
  token_count?: number
  /** 创建时间 */
  created_at: string
}

/** 上下文构建日志 - 记录每次上下文构建的详情 */
export interface ContextBuildLog {
  /** 日志 ID */
  id: string
  /** 所属对话 ID */
  conversation_id: string
  /** 触发消息 ID */
  trigger_message_id?: string
  challenge_version_id: string
  challenge_hash: string
  /** 使用的模型 */
  model: string
  /** 最大上下文 Token 数 */
  max_context_tokens: number
  /** 预留的响应 Token 数 */
  reserved_response_tokens: number
  /** 实际使用的 Prompt Token 数 */
  used_prompt_tokens: number
  /** 上下文分段信息 */
  segments: any[]
  /** 包含的消息 ID 列表 */
  included_message_ids: string[]
  /** 激活的世界书条目 ID 列表 */
  activated_world_entry_ids: string[]
  /** 创建时间 */
  created_at: string
}

/** LLM 设置 - 大语言模型的配置 */
export interface LLMSettings {
  /** API 基础 URL */
  base_url: string
  /** 是否已设置 API Key */
  has_api_key: boolean
  /** 模型名称 */
  model: string
  /** 最大上下文 Token 数 */
  max_context_tokens: number
  /** 最大响应 Token 数 */
  max_response_tokens: number
  /** 温度参数（0-2） */
  temperature: number
  /** Top-p 参数（0-1） */
  top_p: number
  /** 是否启用流式输出 */
  stream_enabled: boolean
  /** 更新时间 */
  updated_at: string
}

/** 管理员维护的平台判定设置 */
export interface AdminRollSettings {
  id: number
  roll_system_enabled: boolean
  roll_difficulty_mode: DifficultyModeEnum
  updated_at: string
}

export type SMTPSecurity = 'none' | 'starttls' | 'tls'

export interface AdminAuthSettings {
  id: number
  email_auth_enabled: boolean
  email_registration_enabled: boolean
  email_verification_enabled: boolean
  invitation_required: boolean
  smtp_host: string
  smtp_port: number
  smtp_security: SMTPSecurity
  smtp_username: string
  has_smtp_password: boolean
  sender_email: string
  sender_name: string
  verification_subject: string
  verification_template: string
  verification_code_ttl_minutes: number
  verification_resend_seconds: number
  updated_at: string
}

export interface InvitationCode {
  id: string
  code_hint: string
  label: string
  enabled: boolean
  max_uses: number
  used_count: number
  expires_at?: string
  created_by?: string
  created_at: string
  updated_at: string
}

export interface CreatedInvitation {
  invitation: InvitationCode
  code: string
}

export interface ChallengePersonaOption {
  id: string
  slug: string
  name: string
  description?: string
}

export interface ChallengeRewardOverview {
  reward_key: string
  ending_title: string
  multiplier: string
}

export interface PublicChallenge {
  id: string
  slug: string
  name: string
  description: string
  version: string
  icon?: string
  tags: string[]
  features: string[]
  quick_start_guide: string
  personas: ChallengePersonaOption[]
  reward_overview: ChallengeRewardOverview[]
}

export interface ChallengePack {
  id: string
  slug: string
  name: string
  description: string
  cover_url?: string
  status: 'active' | 'hidden' | 'retired'
  current_version_id?: string
  created_by?: string
  created_at: string
  updated_at: string
}

export interface ChallengeVersion {
  id: string
  challenge_pack_id: string
  version: string
  status: 'draft' | 'published' | 'retired'
  manifest: Record<string, any>
  content_hash: string
  created_by?: string
  published_by?: string
  published_at?: string
  created_at: string
  updated_at: string
}

export interface ChallengeAsset {
  id: string
  challenge_version_id: string
  kind: 'character' | 'persona' | 'scenario' | 'prompt' | 'worldbook'
  slug: string
  name: string
  content: Record<string, any>
  sort_order: number
  created_at: string
  updated_at: string
}

export interface ChallengeRewardRule {
  id: string
  challenge_version_id: string
  reward_key: string
  ending_type: string
  ending_title: string
  conditions: Record<string, any>
  calculation: Record<string, any>
  max_reward?: number
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface AdminChallengePack {
  pack: ChallengePack
  versions: ChallengeVersion[]
}

export interface ChallengeVersionDocument {
  version: ChallengeVersion
  assets: ChallengeAsset[]
  reward_rules: ChallengeRewardRule[]
}

export interface SaveChallengeVersionInput {
  manifest: Record<string, any>
  assets: ChallengeAsset[]
  reward_rules: ChallengeRewardRule[]
}

export type BuiltinChallengeStatus = 'available' | 'imported' | 'conflict'

export interface AdminBuiltinChallenge {
  slug: string
  name: string
  description: string
  version: string
  content_hash: string
  current_version?: string
  status: BuiltinChallengeStatus
  importable: boolean
}

export interface BuiltinImportResult {
  slug: string
  version: string
  content_hash: string
  created: boolean
}

// ============================================================================
// 请求体类型（高频写路径）
// ============================================================================

/** 创建对话请求体（对照 backend/internal/conversation/service.go 的 CreateInput） */
export interface CreateConversationInput {
  /** 对话标题（留空时后端默认「新的对话」） */
  title?: string
  /** 已发布挑战 slug */
  challenge_slug: string
  /** 该挑战允许的玩家人设 slug */
  persona_slug?: string
}

/** 更新对话请求体（对照 backend/internal/api/conversations.go 的 updateConvBody，只传需要修改的字段） */
export interface UpdateConversationInput {
  /** 对话标题 */
  title?: string
}

/** 编辑消息请求体（对照 backend/internal/chat/service.go 的 EditInput） */
export interface EditMessageInput {
  /** 新的消息内容 */
  content?: string
  /** 是否隐藏该消息 */
  hidden?: boolean
}

// ============================================================================
// WebSocket 事件（对照 backend/internal/realtime/events.go 按事件类型逐一定义）
//
// 后端 Event 结构体所有字段均为 omitempty，零值字段在线上会整个缺失
// （如 delta 为空串、spirit_stones 为 0），因此各成员字段一律标记为可选，
// 消费方需自行做空值防御。
// ============================================================================

/** 所有服务端推送事件的公共字段 */
interface SocketEventBase {
  /** 关联的对话 ID（个别事件如 error 不携带） */
  conversation_id?: string
}

/** 生成开始：后续流式增量将挂到 message_id 对应的展示消息上 */
export interface GenerationStartedEvent extends SocketEventBase {
  type: 'generation_started'
  /** 本轮流式回复的展示消息 ID */
  message_id?: string
}

/** 流式增量 */
export interface GenerationDeltaEvent extends SocketEventBase {
  type: 'generation_delta'
  /** 流式回复的展示消息 ID */
  message_id?: string
  /** 本次增量的可见文本 */
  delta?: string
}

/** 生成结束（正常完成或被取消） */
export interface GenerationFinishedEvent extends SocketEventBase {
  type: 'generation_finished'
  /** 流式回复的展示消息 ID */
  message_id?: string
}

/** 候选回复未通过服务端动作预校验，已撤销且不会持久化 */
export interface GenerationDiscardedEvent extends SocketEventBase {
  type: 'generation_discarded'
  /** 要从时间线移除的临时流式消息 ID */
  message_id?: string
}

/** 服务端持久化了一条新消息 */
export interface MessageCreatedEvent extends SocketEventBase {
  type: 'message_created'
  /** 消息 ID（部分发送点不填，以 message.id 为准） */
  message_id?: string
  /** 完整消息对象 */
  message?: DisplayMessage
}

/** 服务端更新了一条已有消息 */
export interface MessageUpdatedEvent extends SocketEventBase {
  type: 'message_updated'
  /** 消息 ID */
  message_id?: string
  /** 更新后的完整消息对象 */
  message?: DisplayMessage
}

/** 玩家状态更新（携带更新后的完整状态） */
export interface StateUpdateEvent extends SocketEventBase {
  type: 'state_update'
  /** 更新后的完整状态对象 */
  state?: Record<string, any>
}

/** 上下文构建完成：只推 id，详情由前端按需拉取 */
export interface ContextBuiltEvent extends SocketEventBase {
  type: 'context_built'
  /** 上下文构建日志 ID */
  context_build_id?: string
}

/** 对话已结束 */
export interface ConversationEndedEvent extends SocketEventBase {
  type: 'conversation_ended'
}

/** 结局触发（附带结局摘要与结算信息） */
export interface EndingTriggeredEvent extends SocketEventBase {
  type: 'ending_triggered'
  /** 结局类型（good / bad / neutral） */
  ending_type?: string
  /** 结局标题 */
  ending_title?: string
  /** 结局摘要 */
  ending_summary?: string
  /** 本次带出的灵石数量（为 0 时字段缺失） */
  spirit_stones?: number
  /** 本次存活回合数 */
  survival_turns?: number
}

/** 生成或处理出错 */
export interface ErrorEvent extends SocketEventBase {
  type: 'error'
  /** 错误信息 */
  error?: string
}

/** 已知的服务端事件联合体（switch ev.type 可按字面量收窄成员） */
export type SocketEvent =
  | GenerationStartedEvent
  | GenerationDeltaEvent
  | GenerationFinishedEvent
  | GenerationDiscardedEvent
  | MessageCreatedEvent
  | MessageUpdatedEvent
  | StateUpdateEvent
  | ContextBuiltEvent
  | ConversationEndedEvent
  | EndingTriggeredEvent
  | ErrorEvent

/** 兜底成员：后端新增而前端尚未定义的事件类型，防止运行时炸掉 */
export interface UnknownSocketEvent {
  /** 未知的事件类型 */
  type: string
  /** 未知事件的其余字段一律视为不可信 */
  [key: string]: unknown
}

/** WS 线上实际收到的事件：已知事件或未知新事件 */
export type IncomingSocketEvent = SocketEvent | UnknownSocketEvent

// 已知事件类型清单。用 Record<SocketEvent['type'], true> 保证与联合体
// 一一对应：新增事件成员却漏登记时编译器会报错。
const KNOWN_SOCKET_EVENT_TYPES: Record<SocketEvent['type'], true> = {
  generation_started: true,
  generation_delta: true,
  generation_finished: true,
  generation_discarded: true,
  message_created: true,
  message_updated: true,
  state_update: true,
  context_built: true,
  conversation_ended: true,
  ending_triggered: true,
  error: true,
}

/** 类型守卫：过滤掉后端新增而前端不认识的事件，让 switch 收窄安全生效 */
export function isKnownSocketEvent(ev: IncomingSocketEvent): ev is SocketEvent {
  return ev.type in KNOWN_SOCKET_EVENT_TYPES
}
