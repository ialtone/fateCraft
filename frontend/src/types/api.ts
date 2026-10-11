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

export type CampaignStatusEnum = 'active' | 'completed' | 'dead' | 'abandoned'

/** 展示消息类型（roll_result 为天命判定结果，对照 backend roll_flow.go 的 AppendSystemChatEvent） */
export type DisplayMessageTypeEnum =
  'user' | 'assistant' | 'system' | 'event' | 'state' | 'roll_result'

// ============================================================================
// 对话与运行时数据
// ============================================================================

/** 对话 - 一次完整的对话会话 */
export interface Conversation {
  branch_epoch: number
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
  campaign_id?: string
  /** 创建时固化的 LLM 请求路由，之后不可修改 */
  llm_route: 'server' | 'client'
  character_asset_id?: string
  persona_asset_id?: string
  scenario_asset_id?: string
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

export interface Campaign {
  id: string
  player_id: string
  title: string
  challenge_version_id: string
  challenge_hash: string
  character_asset_id?: string
  persona_asset_id?: string
  scenario_asset_id?: string
  status: CampaignStatusEnum
  outcome_key: string
  /** 创建时固化的 LLM 请求路由，之后不可修改 */
  llm_route: 'server' | 'client'
  completed_at_game_time?: number
  current_location_key: string
  game_time: number
  revision: number
  event_sequence: number
  created_at: string
  updated_at: string
}

export interface CampaignEvent {
  id: string
  campaign_id: string
  sequence: number
  event_type: string
  visibility: 'player'
  actor_type: 'player' | 'model' | 'system'
  actor_id?: string
  payload: Record<string, unknown>
  source_message_id?: string
  idempotency_key: string
  game_time: number
  created_at: string
}

export interface CampaignActionResult {
  campaign: Campaign
  events: CampaignEvent[]
  result: Record<string, unknown>
  roll_message?: DisplayMessage
  replayed: boolean
}

export interface CampaignCharacterView {
  id: string
  name: string
  origin: string
  public_identity: string
  realm_key: string
  realm_name: string
  stage_key: string
  stage_name?: string
  realm_rank: number
  cultivation_progress: number
  cultivation_required: number
  foundation_quality: number
  age_days: number
  lifespan_days: number
  health: number
  base_max_health: number
  max_health: number
  mana: number
  base_max_mana: number
  max_mana: number
  spiritual_sense: number
  base_max_spiritual_sense: number
  max_spiritual_sense: number
  injuries: Record<string, unknown>
  long_term_effects: Record<string, unknown>
  active_effects: Record<string, number>
}

export interface CampaignTechniqueView {
  id: string
  key: string
  name: string
  category: string
  learned_layer: number
  mastery: number
  insight: number
  equipped_slot?: string
  status: string
  layer_effects: Record<string, number>
}

export interface CampaignItemStackView {
  item_key: string
  name: string
  quality_key: string
  quantity: number
  metadata: Record<string, unknown>
}

export interface CampaignItemInstanceView {
  id: string
  definition_key: string
  name: string
  quality_key: string
  state: string
  equipped_slot?: string
  refinement: number
  durability: number
  max_durability: number
  properties: Record<string, unknown>
  bonuses: Record<string, number>
}

export interface CampaignRecipeInputView {
  quantity: number
  options: Array<{ item_key: string; name: string; quality_key: string }>
}

export interface CampaignRecipeProductView {
  kind: 'item' | 'equipment'
  key: string
  name: string
  quantity: number
  quality_weights: Record<string, number>
}

export interface CampaignRecipeView {
  key: string
  name: string
  craft_type: 'alchemy' | 'artifact' | 'talisman'
  known: boolean
  learnable: boolean
  source_item_key?: string
  source_item_name?: string
  craft_minutes: number
  mana_cost: number
  success_chance_bps: number
  available: boolean
  missing_requirements: string[]
  inputs: CampaignRecipeInputView[]
  outputs: CampaignRecipeProductView[]
  failure_outputs: CampaignRecipeProductView[]
}

export interface CampaignMarketItemView {
  kind: 'item' | 'equipment'
  key: string
  name: string
  quality_key: string
  stock: number
  max_stock: number
  buyable: boolean
  buy_price: number
  sellable: boolean
  sell_price: number
}

export interface CampaignMarketView {
  key: string
  name: string
  location_key: string
  currency_item_key: string
  currency_name: string
  last_restock_game_time: number
  items: CampaignMarketItemView[]
}

export interface CampaignLocationView {
  key: string
  name: string
  node_type: 'realm' | 'region' | 'location' | 'scene'
  parent_key?: string
  visited: boolean
  knowledge: Record<string, unknown>
}

export interface CampaignRelationshipView {
  instance_key: string
  name: string
  faction_key: string
  realm_key: string
  stage_key: string
  location_key: string
  status: string
  public_profile: Record<string, unknown>
  relationship?: {
    trust: number
    fear: number
    affinity: number
    hostility: number
    debt: number
    public_stance: string
    known_facts: Record<string, unknown>
  }
}

export interface CampaignFactionView {
  key: string
  name: string
  reputation: number
  reputation_level: string
  position_key: string
  contribution: number
  wanted_level: number
  permissions: Record<string, unknown>
}

export interface CampaignRouteRequirementView {
  type: 'faction' | 'realm'
  faction_key?: string
  faction_name?: string
  permission_key?: string
  required_reputation?: number
  current_reputation?: number
  required_realm_rank?: number
  current_realm_rank?: number
}

export interface CampaignRouteView {
  location_key: string
  name: string
  travel_minutes: number
  accessible: boolean
  missing_requirements: CampaignRouteRequirementView[]
}

export interface CampaignGatheringResourceView {
  item_key: string
  name: string
  quality_key: string
  min_quantity: number
  max_quantity: number
  minutes: number
  risk_chance_bps: number
  risk_health_damage: number
}

export interface CampaignRegionStateView {
  key: string
  name: string
  resource_level: number
  danger_level: number
  stability: number
  conflict_stage: number
  faction_power: Record<string, number>
  latest_event?: {
    key: string
    name: string
    variant_key: string
    summary: string
    game_time: number
  }
}

export interface CampaignQuestObjectiveView {
  key: string
  type:
    | 'item'
    | 'encounter_win'
    | 'location'
    | 'faction_reputation'
    | 'npc_relationship'
    | 'region_state'
  label: string
  current: number
  target: number
  comparison?: 'at_least' | 'at_most'
  satisfied: boolean
  consume: boolean
}

export interface CampaignQuestView {
  key: string
  name: string
  quest_type: 'quest' | 'obligation'
  status: 'offered' | 'active' | 'blocked' | 'completed' | 'failed' | 'expired'
  offered_at_game_time: number
  accepted_at_game_time?: number
  due_game_time?: number
  resolved_at_game_time?: number
  objectives: CampaignQuestObjectiveView[]
  rewards: Array<{ type: string; label: string; amount: number }>
  accept_location_keys: string[]
  can_accept: boolean
  can_submit: boolean
}

export interface CampaignEncounterView {
  id: string
  key: string
  location_key: string
  opponent_key: string
  opponent_name: string
  opponent_realm_rank: number
  opponent_health: number
  opponent_max_health: number
  opponent_attack: number
  opponent_defense: number
  round: number
  max_rounds: number
  status: 'active' | 'won' | 'lost' | 'fled' | 'abandoned'
  last_move: string
  started_at_game_time: number
}

export interface CampaignAvailableEncounterView {
  key: string
  name: string
  location_key: string
  opponent_key: string
  opponent_name: string
  opponent_realm_rank: number
  opponent_max_health: number
  round_minutes: number
  max_rounds: number
  flee_chance_bps: number
}

export interface CampaignRewardSource {
  type: 'item_stack'
  key: string
  name: string
  quality_key?: string
  available_quantity: number
  consumed_quantity: number
}

export interface CampaignOutcomeReward {
  key: string
  kind: string
  name: string
  unit: string
  icon: string
  quantity: number
  balance: number
  source?: CampaignRewardSource
}

export interface CampaignSnapshot {
  campaign: Campaign
  outcome_category?: 'success' | 'failure' | 'neutral'
  outcome_title?: string
  character: CampaignCharacterView | null
  techniques: CampaignTechniqueView[]
  inventory: {
    stacks: CampaignItemStackView[]
    instances: CampaignItemInstanceView[]
  }
  recipes: CampaignRecipeView[]
  markets: CampaignMarketView[]
  world: {
    current: {
      key: string
      name: string
      content?: Record<string, unknown>
      gathering: CampaignGatheringResourceView[]
    }
    current_path: Array<{
      key: string
      name: string
      node_type: 'world' | 'realm' | 'region' | 'location' | 'scene'
    }>
    available_routes: CampaignRouteView[]
    discovered: CampaignLocationView[]
  }
  relationships: CampaignRelationshipView[]
  factions: CampaignFactionView[]
  region_state: CampaignRegionStateView | null
  quests: CampaignQuestView[]
  obligations: CampaignQuestView[]
  encounters: CampaignEncounterView[]
  available_encounters: CampaignAvailableEncounterView[]
  outcome_rewards: CampaignOutcomeReward[]
  reward_points: number
  stage_rewards: Array<{
    stage_key: string
    name: string
    configured_points: number
    granted_points: number
    disabled_reason?: string
    game_time: number
  }>
  stage_reward_points: number
  total_reward_points: number
  /** 客户端直连终局不发放奖励时后端填写，前端据此展示替代文案。 */
  rewards_disabled_reason?: string
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
  /** 模型可读思考内容（不包含协议原始 reasoning item） */
  reasoning?: string
  /** 创建时间 */
  created_at: string
}

export type ConversationShareScope = 'all' | 'range'
export type ConversationShareStatus = 'active' | 'expired' | 'revoked' | 'moderated'

export interface SharedMessage {
  position: number
  type: DisplayMessageTypeEnum
  markdown: string
  metadata?: Record<string, unknown>
  created_at: string
}

export interface ConversationShareSnapshot {
  schema_version: 1
  title: string
  attribution: string
  show_username: boolean
  avatar_url?: string
  challenge: {
    slug: string
    name: string
    version: string
  }
  /** 来源对话固化的请求路由；client 表示内容未经服务端校验 */
  llm_route: 'server' | 'client'
  created_at: string
  messages: SharedMessage[]
}

export interface ConversationShare {
  id: string
  conversation_id: string
  conversation_title?: string
  branch_epoch: number
  scope: ConversationShareScope
  source_start_message_id: string
  source_end_message_id: string
  title: string
  show_username: boolean
  public_attribution: string
  message_count: number
  status: ConversationShareStatus
  expires_at?: string
  created_at: string
  revoked_at?: string
  moderated_at?: string
  public_url: string
  replayed?: boolean
}

export interface ConversationSharePage {
  shares: ConversationShare[]
  next_cursor?: string
  has_more: boolean
  page_size: number
}

export interface PublicConversationShare {
  id: string
  snapshot_hash: string
  snapshot: ConversationShareSnapshot
}

export interface CreateConversationShareInput {
  scope: ConversationShareScope
  start_message_id: string | null
  end_message_id: string | null
  title: string
  show_username: boolean
  expires_in_days: 0 | 7 | 30
  request_id: string
}

export interface ConversationShareReport {
  id: string
  share_id: string
  reason: 'privacy' | 'abuse' | 'copyright' | 'other'
  details: string
  status: 'open' | 'resolved' | 'dismissed'
  created_at: string
  resolved_at?: string
  resolved_by?: string
}

/** 服务端冻结的天命判定详情；前端只负责展示，不重新计算。 */
export interface RollConditionSnapshot {
  key?: string
  label: string
  display_expected?: string
  display_actual?: string
  met: boolean
  public: boolean
}

export interface RollBreakdown {
  base: number
  attributes: number
  traits: number
  context: number
  reward_cap: number
  history: number
  difficulty_mode: number
  final: number
}

export interface RollOutcomeRange {
  probability?: number
  range_start: number
  range_end: number
}

export interface RollOutcomeDetail {
  rolled: number
  sides: number
  success_rate: number
  success: boolean
  level: string
  breakdown: RollBreakdown
  success_range: RollOutcomeRange
  outcomes?: {
    critical_success?: RollOutcomeRange
    success?: RollOutcomeRange
    failure?: RollOutcomeRange
    critical_failure?: RollOutcomeRange
  }
}

export interface RollResultMetadata {
  roll_key?: string
  roll_title?: string
  roll_category?: string
  roll_outcome: RollOutcomeDetail
  conditions: RollConditionSnapshot[]
  details_version: number
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

export interface CharacterInquirySourceRef {
  source_id: string
  category: string
  title: string
}

export interface CharacterInquiryMessage {
  id: string
  thread_id: string
  conversation_id: string
  player_id: string
  role: 'user' | 'assistant'
  content: string
  snapshot_id?: string
  snapshot_branch_epoch: number
  snapshot_revision: number
  source_refs: CharacterInquirySourceRef[]
  status: 'completed' | 'failed' | 'cancelled'
  error_code?: string
  created_at: string
}

export interface CharacterInquiryThread {
  id: string
  conversation_id: string
  player_id: string
  created_at: string
  updated_at: string
}

export interface CharacterInquiryHistory {
  thread: CharacterInquiryThread
  messages: CharacterInquiryMessage[]
}

export interface CharacterInquiryAnswer {
  message: CharacterInquiryMessage
  stale: boolean
  snapshot_id: string
  snapshot_branch_epoch: number
  snapshot_revision: number
}

/** 普通玩家可见的上下文构建摘要；Prompt 内部明细仅管理员可见。 */
export interface ContextBuildLog {
  /** 日志 ID */
  id: string
  /** 所属对话 ID */
  conversation_id: string
  /** 使用的模型 */
  model: string
  /** 最大上下文 Token 数 */
  max_context_tokens: number
  /** 预留的响应 Token 数 */
  reserved_response_tokens: number
  /** 实际使用的 Prompt Token 数 */
  used_prompt_tokens: number
  /** 纳入的历史消息数量 */
  included_message_count: number
  /** 激活的世界书条目数量 */
  activated_world_entry_count: number
  /** 较早回合是否等待长期记忆覆盖 */
  memory_lag: boolean
  omitted_turn_from?: number
  omitted_turn_until?: number
  /** 创建时间 */
  created_at: string
}

export type MemoryActivityStatus = 'idle' | 'queued' | 'running'
export type NarrativeActivityStatus = 'idle' | 'preparing' | 'model' | 'tool' | 'streaming'
export type ToolActivityCategory =
  'state' | 'roll' | 'settlement' | 'action' | 'guidance' | (string & {})

export interface ConversationActivity {
  narrative: NarrativeActivityStatus
  memory: MemoryActivityStatus
  memory_job?: {
    status: string
    target_until_turn: number
    execution_count: number
    max_executions: number
    created_at: string
    started_at?: string
    finished_at?: string
  }
  turn_status?: 'pending' | 'completed' | 'partial' | 'failed' | 'cancelled' | string
  can_continue: boolean
  can_regenerate: boolean
  regenerate_cost_quantity?: number
  step_no?: number
  /** Backend tool category. Kept open so a newly added tool cannot break the inspector. */
  tool_category?: ToolActivityCategory
}

export interface TurnPerformance {
  attempt_id: string
  status: 'running' | 'completed' | 'partial' | 'failed' | 'cancelled' | string
  model: string
  step_count: number
  tool_call_count: number
  prompt_tokens: number
  completion_tokens: number
  model_duration_ms: number
  generation_ms: number
  average_first_response_ms: number
  attempt_duration_ms: number
  tokens_per_second: number
  usage_estimated: boolean
  started_at: string
  finished_at?: string
}

export interface ChallengeCacheStats {
  entries: number
  estimated_bytes: number
  max_versions: number
  max_bytes: number
  hits: number
  misses: number
  hit_rate: number
  loads: number
  load_failures: number
  coalesced_loads: number
  evictions: number
  load_duration_ms: number
  compile_duration_ms: number
}

export interface RuntimeInspector {
  conversation_id: string
  updated_at: string
  activity: ConversationActivity
  request?: ContextBuildLog | null
  performance?: TurnPerformance | null
  memory_archive?: {
    covered_from_turn: number
    covered_until_turn: number
    token_count: number
    created_at: string
  }
  memory: MemoryActivityStatus
  diagnostics: Array<{ kind: string; message: string; at: string }>
}

/** LLM 设置 - 大语言模型的配置 */
export interface LLMSettings {
  /** 按已保存 base_url 识别出的端点档位（只读，供前端展示识别结果） */
  resolved_profile?: LLMResolvedProfile
  /** API 基础 URL */
  base_url: string
  /** LLM HTTP 协议 */
  protocol: 'chat_completions' | 'responses'
  /** 是否已设置 API Key */
  has_api_key: boolean
  /** 服务端可见的模型配置是否完整；客户端直连仍需检查本设备 Key */
  configuration_ready: boolean
  access_mode: 'user_managed' | 'platform_managed'
  source: 'user' | 'platform' | 'client'
  editable: boolean
  /** 请求路由：server=服务端转发（默认），client=浏览器直连 */
  llm_route: 'server' | 'client'
  /** 平台是否允许客户端直连；false 时路由选择不展示，有效路由恒为 server */
  client_route_enabled: boolean
  /** 模型名称 */
  model: string
  /** 后台长期记忆模型，空值继承主模型 */
  memory_model: string
  /** 只读角色问答模型，空值继承主模型 */
  inquiry_model: string
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
  /** 思考强度；空串表示跟随供应商默认，非空时原样透传（如 low/medium/high/ultra） */
  reasoning_effort: string
  /** 长期记忆归档请求的思考强度；与叙事互相独立 */
  memory_reasoning_effort: string
  /** 角色问答请求的思考强度；与叙事互相独立 */
  inquiry_reasoning_effort: string
  /** 影响模型调用形态的设置修订 */
  settings_revision: number
  /** 更新时间 */
  updated_at: string
}

/** 服务端按 base_url 识别出的端点档位（只读）。协议完全由用户选择，档位不表达协议信息。 */
export interface LLMResolvedProfile {
  id: string
}

export type LLMCapabilityRole = 'narrative' | 'memory' | 'character_inquiry'

export interface LLMCapabilityDiagnostic {
  role: LLMCapabilityRole
  feature: 'agent_tools' | 'json_schema'
  status: 'supported' | 'unsupported' | 'inconclusive' | 'unavailable'
  protocol: 'chat_completions' | 'responses'
  model: string
  stream: boolean
  error_code?: string
  summary?: string
  checked_at: string
}

export interface LLMCapabilityCheckResponse {
  settings_revision: number
  result: LLMCapabilityDiagnostic
}

/** 管理员维护的平台全局设置 */
export type DetailedLLMLoggingScope = 'all' | 'user' | 'conversation'

export interface AdminSystemSettings {
  id: number
  roll_system_enabled: boolean
  roll_difficulty_mode: DifficultyModeEnum
  detailed_llm_logging_enabled: boolean
  detailed_llm_logging_scope: DetailedLLMLoggingScope
  detailed_llm_logging_target_id: string | null
  diagnostic_log_cleanup_enabled: boolean
  diagnostic_log_retention_days: number
  llm_access_mode: 'user_managed' | 'platform_managed'
  /** 是否允许用户选择客户端直连（默认开启，由 owner 管理） */
  client_llm_route_enabled: boolean
  public_llm_base_url: string
  /** 公共 base_url 的端点档位识别结果（只读） */
  public_llm_resolved_profile?: LLMResolvedProfile
  public_llm_protocol: 'chat_completions' | 'responses'
  public_llm_has_api_key: boolean
  public_llm_model: string
  public_llm_memory_model: string
  public_llm_inquiry_model: string
  public_llm_max_context_tokens: number
  public_llm_max_response_tokens: number
  public_llm_temperature: number
  public_llm_top_p: number
  public_llm_stream_enabled: boolean
  public_llm_reasoning_effort: string
  public_llm_memory_reasoning_effort: string
  public_llm_inquiry_reasoning_effort: string
  public_llm_settings_revision: number
  updated_at: string
}

export interface AdminLLMRequestLogSummary {
  started_at: string
  trace_id?: string
  protocol: string
  transport_attempts: number
  input_tokens: number | null
  output_tokens: number | null
  body_status: 'disabled' | 'captured' | 'truncated' | 'invalid'
  body_metadata: Record<
    string,
    { original_bytes: number; stored_bytes: number; truncated: boolean; invalid: boolean }
  >
  error_code?: string

  id: string
  conversation_id?: string
  message_id?: string
  purpose: 'narrative' | 'memory' | 'character_inquiry' | 'protocol_test' | string
  attempt_id?: string
  memory_job_id?: string
  memory_id?: string
  inquiry_id?: string
  inquiry_snapshot_id?: string
  inquiry_snapshot_branch_epoch: number
  inquiry_snapshot_revision: number
  model: string
  stream: boolean
  response_status?: number
  finish_reason?: string
  received_done: boolean
  success: boolean
  audit_truncated: boolean
  error?: string
  duration_ms: number
  settings_revision: number
  settings_source?: 'user' | 'platform' | string
  output_mode?: 'native_json_schema' | string
  contract_name?: string
  contract_version?: string
  contract_fingerprint?: string
  transport_success: boolean
  structured_success: boolean
  structured_error_code?: string
  output_types: string[]
  created_at: string
}

export interface AdminLLMRequestLog extends AdminLLMRequestLogSummary {
  base_url: string
  request_body: Record<string, any>
  raw_response: string
  response_text: string
}

export interface ContextBuildSegment {
  key: string
  role: string
  tokens: number
  required: boolean
  included: boolean
  budget_key: string
  skip_reason?: string
  original_tokens?: number
  compacted?: boolean
  omitted_keys?: string[]
}

export interface AdminContextBuildLogSummary {
  id: string
  conversation_id: string
  trigger_message_id?: string
  model: string
  max_context_tokens: number
  reserved_response_tokens: number
  used_prompt_tokens: number
  included_message_count: number
  activated_world_entry_count: number
  turn_id?: string
  attempt_id?: string
  branch_epoch: number
  memory_id?: string
  memory_lag: boolean
  memory_job_id?: string
  created_at: string
}

export interface AdminContextBuildLog {
  id: string
  conversation_id: string
  trigger_message_id?: string
  challenge_version_id: string
  challenge_hash: string
  model: string
  max_context_tokens: number
  reserved_response_tokens: number
  used_prompt_tokens: number
  segments: ContextBuildSegment[]
  included_message_ids: string[]
  activated_world_entry_ids: string[]
  turn_id?: string
  attempt_id?: string
  branch_epoch: number
  memory_id?: string
  memory_covered_until_turn_seq: number
  memory_lag: boolean
  omitted_turn_from?: number
  omitted_turn_until?: number
  memory_job_id?: string
  created_at: string
}

export type SMTPSecurity = 'none' | 'starttls' | 'tls'

export interface AdminAuthSettings {
  id: number
  email_auth_enabled: boolean
  linuxdo_login_enabled: boolean
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

export interface AdminUser {
  id: string
  username: string
  email?: string
  display_name: string
  avatar_url?: string
  status: 'active' | 'disabled' | string
  role: 'owner' | 'admin' | 'user' | string
  last_login_at?: string
  created_at: string
  updated_at: string
}

export interface AdminExternalIdentity {
  id: string
  user_id: string
  provider: string
  provider_user_id: string
  username: string
  provider_email?: string
  user_name: string
  user_email?: string
  created_at: string
}

export interface AdminAuditLog {
  trace_id?: string
  id: string
  request_id?: string
  actor_user_id?: string
  target_user_id?: string
  action: string
  old_role?: string
  new_role?: string
  result: 'success' | 'rejected' | 'failed' | string
  metadata: Record<string, unknown>
  created_at: string
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
  points: number
  outcome_key: string
  outcome_title: string
}

export interface ChallengeOutcome {
  reward_points: number
  key: string
  category: 'success' | 'failure' | 'neutral'
  title: string
  tone: string
}

export type StateWidget =
  'text' | 'number' | 'progress' | 'badge' | 'boolean' | 'list' | 'item_list'

export interface StateViewField {
  path: string
  label: string
  widget: StateWidget
  unit?: string
  order: number
}

export interface StateSection {
  key: string
  title: string
  order: number
  fields: StateViewField[]
}

export type CampaignViewWidget =
  'title' | 'text' | 'badge' | 'number' | 'progress' | 'list' | 'item_list'

export interface CampaignViewCondition {
  path: string
  exists?: boolean
  equals?: string | number | boolean
  not_equals?: string | number | boolean
  operator?: 'gte' | 'gt' | 'lte' | 'lt' | 'eq'
  value?: number
}

export interface CampaignViewItemField {
  path: string
  label: string
  widget?: CampaignViewWidget
  unit?: string
  value_namespace?: string
  item_fields?: CampaignViewItemField[]
}

export interface CampaignViewField {
  path: string
  label: string
  widget: CampaignViewWidget
  unit?: string
  order: number
  value_namespace?: string
  visible_when?: CampaignViewCondition
  item_fields?: CampaignViewItemField[]
  sort?: string
  limit?: number
}

export interface CampaignViewSection {
  key: string
  title: string
  order: number
  visible_when?: CampaignViewCondition
  fields: CampaignViewField[]
}

export interface CampaignViewBinding {
  from?: string
  value?: unknown
}

export type GuidanceMode = 'message' | 'action'

export interface GuidanceItem {
  key: string
  label: string
  description?: string
  mode: GuidanceMode
  message?: string
  action_key?: string
  visible_when?: CampaignViewCondition
  enabled_when?: CampaignViewCondition
  arguments?: Record<string, CampaignViewBinding>
  /** Local adapter flag used when legacy campaign actions are projected. */
  enabled?: boolean
}

export interface GuidanceGroup {
  key: string
  title: string
  placement?: string
  max_items?: number
  items: GuidanceItem[]
}

export interface Guidance {
  groups: GuidanceGroup[]
}

export interface ActiveGuidance {
  id: string
  title: string
  items: { key: string; label: string }[]
}

export interface CampaignViewAction {
  key: string
  label: string
  icon?: string
  placement?: string
  source_path?: string
  item_path?: string
  visible_when?: CampaignViewCondition
  enabled_when?: CampaignViewCondition
  arguments?: Record<string, CampaignViewBinding>
}

export interface CampaignView {
  renderer: 'generic'
  title?: string
  header?: {
    title_path?: string
    time_path?: string
    time_format?: 'game_time'
  }
  sections: CampaignViewSection[]
  actions?: CampaignViewAction[]
  empty_states?: Record<string, string>
}

export interface GameplayCapability {
  enabled: boolean
  label: string
  description?: string
  cost_reward_key?: string
  cost_quantity?: number
}

// Platform capabilities are owned by Fatecraft itself and are independent of
// the selected challenge package. GameplayCapability remains reserved for
// actions declared by a Runtime Contract.
export interface PlatformCapability {
  enabled: boolean
  label: string
  description?: string
  cost_reward_key?: string
  cost_quantity?: number
}

export interface RuntimePresentation {
  state_panel_title: string
  state_sections: StateSection[]
  completion_view: {
    replay_label?: string
    back_label?: string
    empty_reward_text?: string
  }
  terminology?: Record<string, string>
  value_labels?: Record<string, Record<string, string>>
  campaign_view?: CampaignView
  guidance?: Guidance
}

export interface PublicRuntime {
  has_trusted_actions: boolean
  engine_key: string
  mode: 'challenge' | 'campaign'
  presentation: RuntimePresentation
  capabilities: Record<string, GameplayCapability>
  platform_capabilities: Record<string, PlatformCapability>
  outcomes?: ChallengeOutcome[]
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
  outcomes: ChallengeOutcome[]
  reward_overview: ChallengeRewardOverview[]
  runtime: PublicRuntime
  available: boolean
  unavailable_reason?: string
}

export interface RewardAsset {
  key: 'fatecraft.points' | 'fatecraft.message-rewrite-credit'
  name: string
  unit: string
  icon: string
}

export interface PlayerReward {
  asset: RewardAsset
  balance: number
}

export interface RewardLedgerEntry {
  id: string
  reward_key: 'fatecraft.points' | 'fatecraft.message-rewrite-credit'
  operation: 'grant' | 'consume' | 'refund' | 'reverse'
  related_entry_id?: string
  reversal_id?: string
  quantity_delta: number
  balance_after: number
  source_type: string
  source_id?: string
  challenge_run_id?: string
  public_note: string
  created_at: string
  user_id?: string
  operator_id?: string
  request_id?: string
  reason?: string
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

export interface GameplayDefinition {
  id: string
  challenge_version_id: string
  kind: string
  key: string
  name: string
  content: Record<string, any>
  tags: string[]
  search_text: string
  visibility: 'player' | 'model' | 'internal'
  sort_order: number
  created_at: string
  updated_at: string
}

export interface ChallengeOutcomeRule {
  reward_points: number
  id: string
  challenge_version_id: string
  outcome_key: string
  category: 'success' | 'failure' | 'neutral'
  title: string
  tone: string
  verifier_key: string
  verifier_config: Record<string, any>
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface ChallengeRuntimeConfig {
  challenge_version_id: string
  engine_key: string
  engine_version: number
  mode: 'challenge' | 'campaign'
  campaign_config?: Record<string, any>
  state_schema: Record<string, any>
  action_config: Record<string, any>
  settlement_config: Record<string, any>
  presentation: RuntimePresentation
}

export interface AdminChallengePack {
  pack: ChallengePack
  versions: ChallengeVersion[]
}

export interface ChallengeVersionDocument {
  version: ChallengeVersion
  runtime: ChallengeRuntimeConfig
  assets: ChallengeAsset[]
  definitions: GameplayDefinition[]
  outcomes: ChallengeOutcomeRule[]
}

export interface SaveChallengeVersionInput {
  manifest: Record<string, any>
  runtime: ChallengeRuntimeConfig
  assets: ChallengeAsset[]
  definitions: GameplayDefinition[]
  outcomes: ChallengeOutcomeRule[]
}

export type BuiltinChallengeStatus = 'available' | 'imported' | 'update_available'

export interface AdminBuiltinChallenge {
  pack_id?: string
  enabled: boolean
  slug: string
  name: string
  description: string
  version: string
  content_hash: string
  current_version?: string
  status: BuiltinChallengeStatus
  importable: boolean
  overwritable: boolean
}

export interface BuiltinImportResult {
  slug: string
  version: string
  content_hash: string
  created: boolean
  overwritten: boolean
}

export interface GameplayModuleDescriptor {
  definition_schemas?: Record<string, import('./module-actions').RuleSchema>
  definition_labels?: Record<string, string>
  category?: string
  category_name?: string
  category_order: number
  definition_kinds: string[]
  campaign_actions?: string[]
  runtime_actions?: Array<{ key: string; label: string; description: string; events: string[] }>
  abandon_action?: string
  key: string
  version: number
  name: string
  description: string
  modes: Array<'challenge' | 'campaign'>
  actions: string[]
  verifiers: string[]
  settlement_handlers: string[]
  campaign_lifecycle_handlers?: string[]
  campaign_projection_handlers?: string[]
  campaign_context_policies?: string[]
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

export interface CreateCampaignInput {
  title?: string
  challenge_slug: string
  persona_slug?: string
}

export interface CampaignActionInput {
  action_id: string
  key: string
  expected_revision: number
  arguments: Record<string, unknown>
}

/** 更新对话请求体（对照 backend/internal/api/conversations.go 的 updateConvBody，只传需要修改的字段） */
export interface UpdateConversationInput {
  /** 对话标题 */
  title?: string
}

// ============================================================================
// WebSocket 事件（对照 backend/internal/realtime/events.go 按事件类型逐一定义）
//
// 后端 Event 结构体所有字段均为 omitempty，零值字段在线上会整个缺失
// （如 delta 为空串），因此各成员字段一律标记为可选，
// 消费方需自行做空值防御。
// ============================================================================

/** 所有服务端推送事件的公共字段 */
interface SocketEventBase {
  /** 关联的对话 ID（个别事件如 error 不携带） */
  conversation_id?: string
  /** 生成操作身份；带有该字段的事件属于同一 durable run。 */
  operation_id?: string
  /** 回合/attempt 身份；用于丢弃被分支替换后的迟到事实事件。 */
  turn_id?: string
  attempt_id?: string
}

/** 生成开始：后续流式增量将挂到 message_id 对应的展示消息上 */
export interface AgentRuntimePayload {
  message_id?: string
  delta?: string
  /** 首个被合并增量的 attempt 序号；sequence 是该合并范围的末序号。 */
  sequence_start?: number
  preview?: boolean
  reason?: 'tool_call'
  name?: string
  category?: 'state' | 'roll' | 'settlement' | 'action'
  status?: 'received' | 'rejected' | 'committed' | 'failed' | 'abandoned'
  code?: string
  message?: string
  tool_count?: number
}

interface AgentRuntimeEventBase extends SocketEventBase {
  event_id?: string
  operation_id?: string
  turn_id?: string
  attempt_id?: string
  step_no?: number
  sequence?: number
  occurred_at?: string
  payload?: AgentRuntimePayload
}

export interface AgentTurnStartedEvent extends AgentRuntimeEventBase {
  type: 'agent.turn.started'
}

export interface AgentStepStartedEvent extends AgentRuntimeEventBase {
  type: 'agent.step.started'
}

export interface AgentToolStartedEvent extends AgentRuntimeEventBase {
  type: 'agent.tool.started'
}

export interface AgentToolFinishedEvent extends AgentRuntimeEventBase {
  type: 'agent.tool.finished'
}

export interface AgentReasoningDeltaEvent extends AgentRuntimeEventBase {
  type: 'agent.reasoning.delta'
}

export interface AgentTextDeltaEvent extends AgentRuntimeEventBase {
  type: 'agent.text.delta'
}

/** 撤销同一步中在工具调用确认前发送的临时正文预览。 */
export interface AgentTextResetEvent extends AgentRuntimeEventBase {
  type: 'agent.text.reset'
}

export interface AgentTurnCompletedEvent extends AgentRuntimeEventBase {
  type: 'agent.turn.completed'
}

export interface AgentTurnPartialEvent extends AgentRuntimeEventBase {
  type: 'agent.turn.partial'
}

export interface AgentTurnFailedEvent extends AgentRuntimeEventBase {
  type: 'agent.turn.failed'
}

export interface AgentTurnCancelledEvent extends AgentRuntimeEventBase {
  type: 'agent.turn.cancelled'
}

export interface AgentQueueUpdatedEvent extends SocketEventBase {
  type: 'agent.queue.updated'
  payload?: { count?: number }
}

/** 服务端持久化了一条新消息 */
export interface MessageCreatedEvent extends SocketEventBase {
  type: 'message_created'
  /** 消息 ID（部分发送点不填，以 message.id 为准） */
  message_id?: string
  /** 完整消息对象 */
  message?: DisplayMessage
}

export interface GuidanceUpdatedEvent extends SocketEventBase {
  type: 'guidance.updated'
  payload: { guidance: ActiveGuidance | null }
}

/** 服务端更新了一条已有消息 */
export interface MessageUpdatedEvent extends SocketEventBase {
  type: 'message_updated'
  /** 消息 ID */
  message_id?: string
  /** 更新后的完整消息对象 */
  message?: DisplayMessage
}

/** 服务端已原子替换当前消息分支。 */
export interface BranchReplacedEvent extends SocketEventBase {
  type: 'branch.replaced'
  payload?: { invalidated_message_ids?: string[] }
  message?: DisplayMessage
}

export interface GenerationReplayedEvent extends SocketEventBase {
  type: 'generation.replayed'
  operation_id?: string
}

export interface ModuleActionCommittedEvent extends SocketEventBase {
  type: 'module.action_committed'
}

/** 玩家状态更新（携带更新后的完整状态） */
export interface StateUpdateEvent extends SocketEventBase {
  type: 'state_update'
  /** 更新后的完整状态对象 */
  state?: Record<string, any>
}

export interface CampaignUpdatedEvent extends SocketEventBase {
  type: 'campaign_updated'
  campaign?: Campaign
  domain_events?: CampaignEvent[]
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

export interface ChallengeRunReward {
  challenge_run_id: string
  reward_key: string
  name: string
  unit: string
  icon: string
  quantity: number
  balance_after: number
}

export interface CompletionMetric {
  key: string
  label: string
  value: string | number | boolean
  unit?: string
}

/** 挑战完成，所有展示与最终余额均来自服务端结算快照。 */
export interface ChallengeCompletedEvent extends SocketEventBase {
  type: 'challenge_completed'
  completion?: import('./endings').CompletionView
  run_id?: string
  outcome?: {
    key: string
    category: 'success' | 'failure' | 'neutral'
    title: string
    summary: string
    tone: string
    rewards_disabled_reason?: string
  }
  reward_grants?: ChallengeRunReward[]
  metrics?: CompletionMetric[]
  next_actions?: Array<'replay' | 'back_to_challenges'>
}

/** 生成终止：本轮生成已经结束，前端复位流式状态 */
export interface ErrorEvent extends SocketEventBase {
  type: 'error'
  /** 错误信息 */
  error?: string
}

/**
 * 非终止提示：请求被拒绝（对话忙、参数非法、队列满）、对话未激活、已取消等。
 * 它不代表正在进行的生成已经结束，只展示横幅；只在本端没有任何流式占位时
 * 才复位本端乐观标记的"生成中"。
 */
export interface NoticeEvent extends SocketEventBase {
  type: 'notice'
  error?: string
}

/** 后台活动状态变化。 */
export interface ActivityUpdatedEvent extends SocketEventBase {
  type: 'activity_updated'
  activity?: 'memory'
  status?: MemoryActivityStatus
}

/** 已知的服务端事件联合体（switch ev.type 可按字面量收窄成员） */
export type SocketEvent =
  | GuidanceUpdatedEvent
  | AgentTurnStartedEvent
  | AgentStepStartedEvent
  | AgentToolStartedEvent
  | AgentToolFinishedEvent
  | AgentReasoningDeltaEvent
  | AgentTextDeltaEvent
  | AgentTextResetEvent
  | AgentTurnCompletedEvent
  | AgentTurnPartialEvent
  | AgentTurnFailedEvent
  | AgentTurnCancelledEvent
  | AgentQueueUpdatedEvent
  | MessageCreatedEvent
  | MessageUpdatedEvent
  | BranchReplacedEvent
  | GenerationReplayedEvent
  | ModuleActionCommittedEvent
  | StateUpdateEvent
  | CampaignUpdatedEvent
  | ContextBuiltEvent
  | ConversationEndedEvent
  | (SocketEventBase & { type: 'ending.proposed' })
  | ChallengeCompletedEvent
  | ActivityUpdatedEvent
  | ErrorEvent
  | NoticeEvent

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
  'guidance.updated': true,
  'agent.turn.started': true,
  'agent.step.started': true,
  'agent.tool.started': true,
  'agent.tool.finished': true,
  'agent.reasoning.delta': true,
  'agent.text.delta': true,
  'agent.text.reset': true,
  'agent.turn.completed': true,
  'agent.turn.partial': true,
  'agent.turn.failed': true,
  'agent.turn.cancelled': true,
  'agent.queue.updated': true,
  message_created: true,
  message_updated: true,
  'branch.replaced': true,
  'generation.replayed': true,
  'module.action_committed': true,
  state_update: true,
  campaign_updated: true,
  context_built: true,
  conversation_ended: true,
  challenge_completed: true,
  'ending.proposed': true,
  activity_updated: true,
  error: true,
  notice: true,
}

/** 类型守卫：过滤掉后端新增而前端不认识的事件，让 switch 收窄安全生效 */
export function isKnownSocketEvent(ev: IncomingSocketEvent): ev is SocketEvent {
  return ev.type in KNOWN_SOCKET_EVENT_TYPES
}
