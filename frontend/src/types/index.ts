// 预设包相关
export type { PresetPack, ResourceWithPack } from './presetPack'

// 账号相关
export type { Account } from './account'

// API 相关类型
export type {
  // 枚举类型
  MessageRoleEnum,
  RevisionSourceEnum,
  DifficultyModeEnum,
  ConversationStatusEnum,
  DisplayMessageTypeEnum,
  // 实体类型
  Character,
  Persona,
  Scenario,
  PromptPreset,
  PromptSegment,
  WorldBook,
  WorldBookEntry,
  Conversation,
  ChatMessage,
  DisplayMessage,
  StateRevision,
  MemorySummary,
  ContextBuildLog,
  LLMSettings,
  SocketEvent,
  IncomingSocketEvent,
  UnknownSocketEvent,
  CreateConversationInput,
  UpdateConversationInput,
  EditMessageInput,
} from './api'

// WS 事件类型守卫（值导出，不能走 export type）
export { isKnownSocketEvent } from './api'
