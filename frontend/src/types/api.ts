// ============================================================================
// 枚举类型
// ============================================================================

/** 消息角色类型 */
export type MessageRoleEnum = 'system' | 'user' | 'assistant' | 'tool' | 'internal'

/** 状态修订来源 */
export type RevisionSourceEnum = 'ai' | 'user' | 'system'

/** 判定难度模式 */
export type DifficultyModeEnum = 'easy' | 'normal' | 'hard'

/** 对话状态类型 */
export type ConversationStatusEnum = 'active' | 'ended' | 'archived' | 'deleted'

/** 展示消息类型（roll_result 为天命判定结果，对照 backend roll_flow.go 的 AppendSystemChatEvent） */
export type DisplayMessageTypeEnum =
  'user' | 'assistant' | 'system' | 'event' | 'state' | 'roll_result'

// ============================================================================
// 模板实体（文件存储）
// ============================================================================

/** 角色卡 - 定义 AI 扮演的角色 */
export interface Character {
  /** 角色 ID（slug） */
  id: string
  /** 角色名称 */
  name: string
  /** 角色描述 */
  description: string
  /** 性格特征 */
  personality: string
  /** 场景设定 */
  scenario: string
  /** 开场白 */
  first_message: string
  /** 示例对话 */
  example_dialogues: string
  /** 系统提示词覆盖 */
  system_prompt_override: string
  /** 标签 */
  tags: string[]
  /** 头像 URL */
  avatar_url?: string
  /** 所属预设包 ID */
  pack_id?: string
  /** 创建时间 */
  created_at: string
  /** 更新时间 */
  updated_at: string
}

/** 玩家人设 - 定义玩家的身份和说话风格 */
export interface Persona {
  /** 人设 ID（slug） */
  id: string
  /** 人设名称 */
  name: string
  /** 人设描述 */
  description: string
  /** 说话风格 */
  speaking_style: string
  /** 限制条件 */
  constraints: string
  /** 所属预设包 ID */
  pack_id?: string
  /** 创建时间 */
  created_at: string
  /** 更新时间 */
  updated_at: string
}

/** 场景 - 定义故事发生的背景 */
export interface Scenario {
  /** 场景 ID（slug） */
  id: string
  /** 场景名称 */
  name: string
  /** 场景描述 */
  description: string
  /** 开场文本 */
  opening: string
  /** 所属预设包 ID */
  pack_id?: string
  /** 创建时间 */
  created_at: string
  /** 更新时间 */
  updated_at: string
}

/** Prompt 预设 - 包含多个 Prompt 分段的集合 */
export interface PromptPreset {
  /** 预设 ID（slug） */
  id: string
  /** 预设名称 */
  name: string
  /** 预设描述 */
  description?: string
  /** 创建时间 */
  created_at: string
  /** 更新时间 */
  updated_at: string
  /** Prompt 分段列表 */
  segments: PromptSegment[]
}

/** Prompt 分段 - Prompt 预设中的单个分段 */
export interface PromptSegment {
  /** 分段 ID */
  id: string
  /** 所属预设 ID */
  preset_id: string
  /** 分段键名 */
  key: string
  /** 角色类型 */
  role: MessageRoleEnum
  /** 分段内容 */
  content: string
  /** 是否启用 */
  enabled: boolean
  /** 是否必需 */
  required: boolean
  /** 优先级 */
  priority: number
  /** 位置顺序 */
  position: number
}

/** 世界书 - 包含多个世界书条目的集合 */
export interface WorldBook {
  /** 世界书 ID（slug） */
  id: string
  /** 世界书名称 */
  name: string
  /** 世界书描述 */
  description?: string
  /** 创建时间 */
  created_at: string
  /** 更新时间 */
  updated_at: string
  /** 世界书条目列表 */
  entries: WorldBookEntry[]
}

/** 世界书条目 - 世界书中的单个知识条目 */
export interface WorldBookEntry {
  /** 条目 ID */
  id: string
  /** 所属世界书 ID */
  world_book_id: string
  /** 条目标题 */
  title: string
  /** 条目内容 */
  content: string
  /** 触发关键词 */
  keywords: string[]
  /** 是否始终激活 */
  constant: boolean
  /** 是否启用 */
  enabled: boolean
  /** 优先级 */
  priority: number
  /** 预算组 */
  budget_group: string
  /** Token 数量 */
  token_count?: number
  /** 备注 */
  notes: string
  /** 元数据 */
  metadata: Record<string, any>
}

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
  /** 使用的角色卡 ID */
  character_id?: string
  /** 使用的玩家人设 ID */
  persona_id?: string
  /** 使用的场景 ID */
  scenario_id?: string
  /** 使用的 Prompt 预设 ID */
  prompt_preset_id?: string
  /** 本次轮回使用的天道回溯次数 */
  rewinds_used_this_trial: number
  /** 对话级判定系统开关（null/缺失 = 跟随全局设置，对照 backend models.go） */
  roll_system_enabled?: boolean | null
  /** 对话级判定难度（缺失 = 跟随全局设置） */
  roll_difficulty_mode?: string
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
  /** 设置 ID */
  id: number
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
  /** 是否启用天命判定系统 */
  roll_system_enabled: boolean
  /** 判定难度模式 */
  roll_difficulty_mode: DifficultyModeEnum
  /** 更新时间 */
  updated_at: string
}

// ============================================================================
// 请求体类型（高频写路径）
// ============================================================================

/** 创建对话请求体（对照 backend/internal/conversation/service.go 的 CreateInput） */
export interface CreateConversationInput {
  /** 对话标题（留空时后端默认「新的对话」） */
  title?: string
  /** 角色卡 ID（文件 slug） */
  character_id?: string
  /** 玩家人设 ID */
  persona_id?: string
  /** 场景 ID */
  scenario_id?: string
  /** Prompt 预设 ID */
  prompt_preset_id?: string
  /** 绑定的世界书 ID 列表 */
  world_book_ids?: string[]
  /** 对话级判定系统开关（不传 = 跟随全局设置） */
  roll_system_enabled?: boolean
  /** 对话级判定难度（不传 = 跟随全局设置） */
  roll_difficulty_mode?: string
}

/** 更新对话请求体（对照 backend/internal/api/conversations.go 的 updateConvBody，只传需要修改的字段） */
export interface UpdateConversationInput {
  /** 对话标题 */
  title?: string
  /** 角色卡 ID */
  character_id?: string
  /** 玩家人设 ID */
  persona_id?: string
  /** Prompt 预设 ID */
  prompt_preset_id?: string
  /** 场景 ID */
  scenario_id?: string
  /** 绑定的世界书 ID 列表（整组替换） */
  world_book_ids?: string[]
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
