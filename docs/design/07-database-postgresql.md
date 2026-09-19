# 07. PostgreSQL 数据库设计

## 定位与初始化原则

PostgreSQL 是 Fatecraft 的运行时权威数据源，保存认证、挑战固定版本、对话状态、判定证据、结算、奖励账本和管理员设置。仓库 JSON 只是内置玩法的可导入源文件；玩法导入或发布后，生成过程只能读取数据库中与对话固定版本关联的内容。

当前项目按新项目维护，不承担旧数据库兼容：

- `backend/scripts/sql/init.sql` 是空数据库的完整最终 Schema。
- 不使用 GORM `AutoMigrate` 修改生产结构。
- 不保留 `accounts`、单一灵石字段、旧回溯计数或旧结局字段。
- 测试站需要结构变化时，重建空库并完整执行 `init.sql`。
- 脚本使用一个事务；任一 DDL、约束、种子或注释失败都会整体回滚。

`CREATE EXTENSION IF NOT EXISTS pgcrypto` 和 `pg_trgm` 固定安装在 `public` schema。前者提供 `gen_random_uuid()`；后者为标题的模糊搜索提供 `public.gin_trgm_ops` GIN 索引。`IF NOT EXISTS` 让重复执行扩展声明不会报错。它不代表数据库会自动加密业务字段：密码、会话令牌、验证码、邀请码和 SMTP 密码仍由后端分别使用 Argon2id/bcrypt、SHA-256 或 AES-GCM 处理。

## 关系总览

~~~text
users
  |-- auth_identities
  |-- auth_sessions
  |-- email_verification_codes（按邮箱）
  |-- invitation_codes（创建人）
  |-- character_inquiry_rate_limits
  |-- llm_settings
  |-- challenge_packs / challenge_versions（创建与发布人）
  |-- campaigns
  |     |-- campaign_events
  |     |-- campaign_action_commits
  |     `-- conversations（唯一持续对话）
  |-- conversations
  |     |-- conversation_turns
  |     |     |-- generation_attempts
  |     |     |     |-- agent_steps
  |     |     |     `-- agent_tool_invocations
  |     |     |-- chat_messages
  |     |     `-- display_messages
  |     |-- conversation_states
  |     |-- state_revisions
  |     |-- conversation_memories
  |     |-- memory_jobs
  |     |-- character_inquiry_threads
  |     |     `-- character_inquiry_messages
  |     |-- context_build_logs
  |     |-- roll_history
  |     `-- challenge_runs（每个对话至多一条）
  |             `-- challenge_run_rewards
  |-- player_reward_balances
  `-- reward_ledger

challenge_packs
  `-- challenge_versions
        |-- challenge_runtime_configs（一对一）
        |-- challenge_assets
        |-- challenge_outcome_rules
        `-- challenge_reward_rules -> reward_definitions
~~~

Schema 当前包含 57 张表，`init.sql` 为每张表提供 `COMMENT ON TABLE`。表注释用于数据库工具和后续维护，不代替本文的关系与事务说明。

## 认证与平台设置

### `users`

平台用户主记录，只保存展示资料、邮箱凭据状态和平台角色，不保存任何题材角色、局内状态或奖励余额。

重要约束：

- `email` 可空且全局唯一，LinuxDo 用户不必拥有可登录邮箱。
- `password_hash` 只存 Argon2id 哈希，不存明文或可逆密文。
- `status` 仅允许 `active/disabled`。
- `role` 仅允许 `user/admin`，首个创建用户由后端提升为管理员。

### `auth_identities`

记录用户与外部身份提供方的绑定。`provider + provider_user_id` 唯一；LinuxDo 返回的用户名、邮箱、信任等级和原始安全资料属于身份快照，不参与玩法状态。

### `auth_sessions`

登录会话。浏览器 Cookie 保存随机原令牌，数据库只保存 64 字符 SHA-256 摘要。`expires_at` 默认由后端按 14 天计算；登出删除对应摘要。

### `oauth_login_flows`

LinuxDo OAuth 授权过程的一次性 state 与 PKCE `code_verifier`。state 只保存摘要，完成或过期后不可复用。

### `auth_settings`

单行管理员配置，主键固定为 `1`。包含：

- 邮箱登录、邮箱注册、验证码和邀请码开关；
- SMTP 地址、端口、安全模式、账号和发件人；
- 验证码主题、正文模板、有效期和重发间隔；
- 经后端 AES-GCM 加密后的 SMTP 密码。

读取管理接口时不会回显 SMTP 密码原文。

### `email_verification_codes`

注册验证码。只保存 bcrypt 摘要、尝试次数、过期时间和消费时间；同一邮箱与 purpose 同时只允许一条未消费记录。

### `invitation_codes`

邀请码。数据库只保存 SHA-256 摘要和用于管理员辨识的短 hint，原文仅在创建接口成功时返回一次。`max_uses = 0` 表示不限制次数；消费必须在注册事务中锁行检查。

### `llm_settings`、`system_settings` 与 `llm_request_logs`

`llm_settings` 保存用户模式下每个用户自己的 OpenAI 兼容端点、协议、模型和生成参数；`api_key_encrypted` 只保存 AES-GCM 密文。`system_settings` 是主键固定为 1 的管理员单例，同时保存判定/诊断设置、诊断日志清理开关与保留天数，以及平台模式公共 LLM 配置；`llm_access_mode` 为 `user_managed`（默认）或 `platform_managed`，平台模式不删除用户行。新设置默认使用 128000 上下文和 16384 最大输出 Token，应用层校验窗口、回复预留和格式余量的关系；用户和平台各自的 settings revision 用于审计。读取接口只返回 `has_api_key`/`public_llm_has_api_key`。

平台不持久化模型能力评分或健康状态。用户显式点击检测时执行一次无副作用请求，结果只返回当前页面，不改变生产调用路径。

详细日志开启后，每个 Chat Completions 或 Responses 模型请求写入一条 `llm_request_logs`，记录 purpose、业务 attempt、Memory job/候选版本、Inquiry snapshot、请求 JSON、最终原始 HTTP/SSE 响应、合并正文、HTTP 状态、finish reason、终止事件、settings revision、输出模式、契约名称/版本/指纹、输出类型、传输成功、结构成功、稳定结构错误码、耗时和错误。响应体交付前发生的传输重试只保留在结构化服务端日志中，不伪装成独立业务 attempt；Agent Tool Loop 的每个模型步骤仍是独立逻辑请求，显式诊断以 `purpose=protocol_test` 形成独立记录。记录不包含 API Key、Authorization 头或 URL 查询参数，仅管理员 API 可读取；关闭开关不会删除已有记录。

自动清理默认开启并保留 30 天，可由管理员关闭或配置 1-3650 天。清理仅覆盖 `llm_request_logs` 和 `context_build_logs`，使用有界批次和 `SKIP LOCKED`；`conversation_share_audit_events` 按治理审计策略长期保留。Schema 不为清理额外增加索引，优先复用现有访问路径并由 autovacuum 回收死元组。

## 挑战内容与运行契约

### `challenge_packs`

稳定玩法入口，保存 slug、名称、说明、状态和 `current_version_id`。Pack 本身不保存可变运行规则；当前版本只指向一个已经发布的快照。

### `challenge_versions`

挑战不可变版本：

- `version` 在同一 Pack 内唯一；使用 `major.minor.patch`。
- `manifest` 保存公开元数据、入口配置和可选奖励前置条件。
- `status` 为 `draft/published/retired`。
- `content_hash` 是 Manifest、Runtime、资产、结局、奖励配置的规范化 SHA-256。
- 发布版本不可原地写入，修改必须复制新草稿。

### `challenge_runtime_configs`

与挑战版本一对一，保存题材无关运行契约：

- `engine_key/engine_version`：已安装可信玩法模块。
- `mode`：`challenge` 为单局挑战，`campaign` 为长期存档。
- `campaign_config`：长期模式的生命周期、投影、上下文策略、固定起点和世界时钟限制；单局模式为空。
- `state_schema`：状态路径、类型、默认值、可见性、更新权限和约束。
- `action_config`：当前版本允许的模型动作和玩家能力成本。
- `settlement_config`：可信结算处理器。
- `presentation`：玩家可见状态分组、控件、术语和完成界面文案。

技术验证配置只供管理员和后端读取；普通用户只获得后端编译后的公开投影。

`mode` 和完整 `campaign_config` 都进入挑战版本内容哈希。单局模式必须声明可信 settlement；长期模式不要求单局结局，但必须由安装模块明确允许全部 Campaign handler key。

### `challenge_assets`

版本拥有的 Character、Persona、Scenario、Prompt 和 WorldBook JSON。`challenge_version_id + kind + slug` 唯一，资产不能跨版本引用。普通用户没有读取完整资产的 API。

### `challenge_outcome_rules`

结局规则使用稳定 `outcome_key`，与展示标题分离。每条规则保存：

- `category`：`success/failure/neutral`；
- `tone`：`success/error/warning/info`；
- `verifier_key` 与受限配置；
- 是否启用。

模型提交 outcome 只是一项候选，后端必须用可信状态和判定流水执行 verifier。

### `challenge_reward_rules`

把一个结局绑定到一个全局奖励定义，并保存：

- `calculator_key` 与版本化配置；
- 发布时奖励公开展示快照；
- 是否启用。

同一结局不能重复绑定同一奖励定义。一次结算可以命中多条规则，因而可以同时发放货币、物品和徽章。

Campaign 的 `campaign.extract_item_stack` 规则把当前版本中的可堆叠局内物按有界比例转换为可叠加账号奖励。规则仍保存在本表，不新增题材专属奖励表；终局事务从 Campaign 领域表读取来源并将扣除、余额和账本原子提交。

## 长期存档与对话运行时

### `campaigns`

长期玩法权威存档头，固定 `player_id`、挑战版本、内容哈希及创建时选择的 Character、Persona、Scenario。`revision` 每次成功领域动作加一，`event_sequence` 按实际事件条数递增，两者不能混用。`game_time` 只能单调推进，`random_seed` 不通过普通 API 暴露。

### `campaign_events` 与 `campaign_action_commits`

`campaign_events` 是数据库触发器保护的只追加审计记录；`visibility` 区分 player、model、internal，玩家事件接口只返回 player。一次动作可产生多条连续事件。

`campaign_action_commits` 以 `campaign_id + idempotency_key` 为主键，保存动作 key、提交后 revision、事件区间和公开结果。重试先检查该表，再检查 active 状态与 expected revision；同一幂等键换用其他动作会被拒绝。

### Campaign 领域表

`campaign_characters` 分别保存境界基础资源上限与后端结算后的有效上限；气血归零或寿元耗尽后 Campaign 进入 `dead`。`campaign_techniques`、`campaign_item_stacks` 和 `campaign_item_instances` 保存已掌握功法、堆叠资源及独立装备实例，唯一索引保证同一功法槽和装备槽最多一个实例。

`campaign_known_recipes` 以 `campaign_id + recipe_key` 保存已掌握配方和学习来源快照；配方规则本体仍来自 Campaign 固定版本的 `gameplay_definitions`，摘要和模型文本不是权威来源。

`campaign_market_states` 保存每个市场的地点、最近补货游戏时间和动态价格修正，`campaign_market_stocks` 以市场、商品类型、商品 key 和品质为复合主键保存有限库存。库存有非负约束并通过复合外键绑定市场状态；购买、出售和补货只在 Campaign 行锁事务中更新。市场不会按现实离线时间自行增长。

`campaign_location_discoveries` 只保存已发现地点事实，地点的四层父子结构来自固定版本 `gameplay_definitions`。`campaign_npcs` 和 `campaign_relationships` 分离 NPC 私密状态与玩家已知关系。`campaign_faction_standings.position_key` 由贡献阈值派生，`permissions` 由声望阈值派生；二者都是服务端投影，不是模型写入入口。

`campaign_region_states` 为每个 region 保存 0..10000 的资源、危险、稳定与势力强弱，以及 0..100 的冲突阶段。普通快照只读取当前位置的祖先 region，远端区域不会逐 NPC 展开。

`campaign_scheduled_events` 保存世界事件和任务截止，待处理索引按 Campaign、到期游戏时间、优先级、类型、模板与 occurrence 排序。已处理和已取消行保留审计事实，周期事件另建下一 occurrence；每回合达到处理上限时其余行仍为 pending。

`campaign_quests` 以 `campaign_id + quest_key` 保存普通任务或 obligation 的状态、接受/截止/解决游戏时间、进度和定义快照。目标规则仍来自固定版本定义；任务提交、物品扣除、势力奖励、截止事件取消及领域事件处于同一 Campaign 事务。

### `conversations`

每个玩家挑战实例。创建时固定：

- `player_id`；
- `challenge_version_id` 与 `challenge_hash`；
- 从该版本选中的 Character、Persona 和 Scenario。

普通单局对话的 `campaign_id` 为空。长期存档对话必须填写 `campaign_id`；复合外键保证玩家、版本和哈希与 Campaign 完全一致，`campaign_id` 唯一约束保证每个 Campaign 最多绑定一个持续 Conversation。

状态只允许 `active/ended/archived/deleted`。`conversation_generation_runs` 保存每次可写生成的 durable owner，以及全局唯一的 `request_id`、`operation_type`、可选 `target_message_id`、payload hash 和 conversation branch revision。活动状态部分唯一索引保证每个对话最多一个 `running/cancel_requested` run；同一 request ID 重试只回放原 run，参数不一致返回冲突。`is_processing` 仅是兼容性忙碌投影，不是生成所有权，也不是普通用户可写设置。

生成 Worker 的写入必须在事务内以 `run_id` 重新校验 conversation active、run 状态和 conversation revision，并在已有 attempt 时继续校验 attempt、turn 的 running/未失效状态和 branch epoch。生命周期转换先锁 conversation、标记 active run 为 `cancel_requested`，待 Worker 结束后再由同一 run 清理忙碌投影；`FinishGeneration` 同样先锁 conversation 再锁 run，避免与取消/生命周期转换形成死锁。租约由 `owner_instance_id` + `lease_expires_at` 管理：持有方定期心跳，回收器只封存本实例遗留或已过期的 run（`abandoned/lease_expired`），其他实例仍在心跳的 run 不受影响。旧 run 不得清除新 run 的状态。

### `conversation_turns`、`generation_attempts`、Agent 步骤与消息

`conversation_turns` 是按 conversation 单调编号的逻辑剧情单位；`generation_attempts` 保存一次实际生成的 attempt 编号、branch epoch、固定 Memory、步骤数、已提交写工具数、工具目录版本和 Runtime hash。状态允许 `running/completed/partial/failed/cancelled`：`partial` 表示已有可信写工具提交，但最终叙事没有完成。进程异常重启时遗留 pending/running 行会按已提交副作用封口。固定 Challenge Bundle 在 attempt 首次装配并校验哈希后复用；状态、Campaign Projection 和工具快照仍按步骤读取最新值。

`agent_steps` 记录每次模型调用的步骤号、模型、协议、输出通道、完整工具快照及哈希、Campaign revision、token 与结束原因。`agent_tool_invocations` 以 `attempt_id + call_id` 唯一，保存参数哈希、`received/rejected/committed/failed/abandoned` 状态和有界公开结果；所属对话已无活动租约的残留 `received` 会由回收器标记为 `abandoned/generation_lost`，不会自动重放。

`chat_messages` 是进入 LLM 上下文的内部消息，使用数据库分配的稳定 `message_seq` 并关联 turn/attempt。`content_blocks` 是唯一的模型内容字段，按原始顺序保存 `text`、`thinking`、`refusal`、`tool_call` 以及 provider-owned `replay`；因此 Chat 与 Responses 的协议差异不会扩散到数据库。`content` 仅作为普通文本索引/投影，工具调用和工具结果仍通过 `kind`、`tool_call_id`、`tool_invocation_id` 关联。消息上的 Token 计数只有在 `tokenizer_version` 与当前版本一致、且内容仍是相同的单文本块时才可复用。`display_messages` 是玩家时间线投影；关联 assistant 消息时，接口只从 `content_blocks` 提取安全的可读 `thinking` 文本，忽略 provider 原始加密字段，前端默认折叠并在流式思考期间展开。重新生成与剧情改写事务都会锁定目标、扣除平台级剧情改写额度、递增 branch epoch、失效旧分支 turn/message/Memory 并建立新用户回合；含已提交 Campaign 副作用的历史不允许普通回滚。

### `conversation_states` 与 `state_revisions`

`conversation_states` 保存当前 JSONB 状态和 Schema 版本。后端按对话固定 Runtime Contract 初始化、校验和投影；模型不能更新未声明或非 model 权限路径。

`state_revisions` 保存每次状态更新的来源、补丁、前后快照和关联 turn/attempt，用于审计。普通用户的当前状态和修订接口都只返回 `visibility=player` 的投影，父对象不会带出显式声明的 model/internal 子字段。

### `conversation_memories`、`memory_jobs` 与 `context_build_logs`

`conversation_memories` 是只追加版本链，保存覆盖边界、branch epoch、严格结构化内容、稳定渲染、evidence、模型和生成指纹；conversation 只指向当前有效链头。`memory_jobs` 保存来源边界、任务状态、退避、租约、fencing token 和结果，每个 conversation 最多一个 pending/running 任务。

`context_build_logs` 关联 turn/attempt 和固定 Memory，记录 token 预算、分段、完整回合取舍、Campaign Projection 压缩前 Token 与省略 key、世界书命中、Memory lag、省略回合范围和规划 job，便于定位模型输入问题。

### `character_inquiry_threads` 与 `character_inquiry_messages`

每个 conversation/player 最多一个独立只读问答 thread。消息固定 snapshot ID、branch epoch 和 revision，只保存回答正文与公开来源标签，不复制玩家状态、公开世界书规则或 Campaign 快照；它们不关联主叙事 turn，也不成为 Memory 来源。公开规则在请求时从对话固定版本的 required worldbook 读取，且条目必须启用并显式设置 `metadata.inquiry_visible=true`。`character_inquiry_rate_limits` 是按玩家原子更新的固定窗口计数器，不随用户清空问答历史而删除。

### `roll_history`

服务端判定证据。`roll_key` 是稳定规则键，`title_snapshot` 是当局展示标题，`category` 用于计算器与审计。结局 verifier 只读取成功的稳定 key，不用中文标题作为安全标识。

## 通用奖励与结算

### `reward_definitions`

全局稳定奖励目录：

- `key` 使用命名空间，如 `reincarnation-trials.spirit-stone`；
- `kind` 为 `currency/item/badge/entitlement/credit`；
- 名称、说明、单位、图标和 featured 属于展示；
- `stackable/consumable` 定义长期语义。

非堆叠奖励不能同时标记为 consumable。奖励被挑战引用后，key、kind、stackable、consumable 不能原地改变；需要新语义时创建新 key。

### `player_reward_balances`

`player_id + reward_definition_id` 主键的余额投影，保存当前余额、累计授予和累计消费。它用于快速查询，不能脱离账本单独修改。

### `reward_ledger`

不可变奖励账本，操作为 `grant/consume/adjust`，每行保存数量变化、来源、可选挑战结算 ID、幂等键、元数据和时间。数据库触发器拒绝 UPDATE/DELETE；正常业务只能追加记录。

幂等键全局唯一，防止重试或并发导致重复发奖/扣费。账本与余额必须位于同一事务。

Campaign 终局授予使用 `source_type=campaign_outcome` 和 Campaign ID 作为 `source_id`。metadata 保存 outcome、固定版本、奖励展示快照及可选提取来源快照；完成态玩家快照据此恢复本次带出，不依赖聊天文本。完整语义见 `21-campaign-outcome-rewards.md`。

### `challenge_runs`

每个对话最多一条的结算主记录，保存固定版本、outcome 快照、类别、摘要、可信结算详情和结算时间。它不保存单一奖励字段。

### `challenge_run_rewards`

某次挑战实际发放的零到多项奖励快照，关联奖励账本，并保存 reward key、名称、单位、图标、数量与 `balance_after`。前端完成弹窗以这里的结果为准，不自行计算余额。

## 关键事务边界

### Agent 工具提交

完整 Tool Call 先以 `received` 持久化。成功路径在同一短事务提交领域事实、`committed` invocation、assistant tool-call 消息、tool-result 消息和 attempt 写工具计数。Handler 返回业务拒绝或失败时，内部哨兵会回滚事务中全部领域写入，再单独保存 invocation 终态和公开结果。数据库事务提交失败不得广播状态、Campaign、判定或结算事件。

### 挑战发布

1. 锁定草稿版本及其 Challenge Pack 行；同一 Pack 的并发发布因此串行化。
2. 读取 Runtime、资产、结局、奖励规则和定义。
3. 校验注册表能力、状态路径、Prompt、世界书、奖励快照与前置条件。
4. 计算规范化内容哈希。
5. 将版本置为 published，并切换 Pack 当前版本。

列表读取统一采用有界、稳定顺序：conversation 按 `updated_at DESC, id DESC`，消息按 `(created_at, id)` 复合游标，Campaign 事件按定义的时间/优先级/模板/occurrence/id，奖励账本按 `created_at DESC, id DESC`。API 层拒绝非法或超限 `limit`，仓储层再次执行上限保护；不存在的消息 anchor 不会静默退化为第一页。

奖励展示和账本查询一次性 JOIN 奖励定义；Campaign 结算在事务开始时批量预取涉及的定义、余额、物品栈和物品定义，避免逐项 N+1 查询。余额行仍按稳定顺序加锁并串行写入账本，以保持并发扣费和发奖正确性。

### 挑战结算

1. 锁定 conversation，确认仍 active 且尚无 run。
2. 读取可信状态与成功判定 key，执行固定版本 verifier。
3. 执行每条可信 calculator。
4. 写 `challenge_runs`。
5. 对每项新奖励追加 ledger、更新 balance、写 run reward 快照。
6. 将 conversation 置为 ended。

### 奖励消费型功能

检查余额、追加 consume 账本、更新 balance 和目标业务写入必须在同一事务。例如重新生成或消息改写不能先扣额度后再用补偿记录返还。

## 删除与保留策略

- 删除用户会级联认证、会话、对话和奖励投影；正式环境通常应优先禁用账号。
- 物理删除对话会级联回合、attempt、消息、状态、Memory、询问、判定、上下文和结算；产品层普通删除采用软删除状态。
- 被当前版本引用的挑战版本受外键保护；已发布历史版本不应物理删除。
- 奖励账本由触发器保护，不参与普通清理。
- Prompt、世界书和运行规则随挑战版本保留，以保证历史对话可复现。

## 维护检查

Schema 变更至少执行：

~~~powershell
rg -n "^CREATE TABLE" backend/scripts/sql/init.sql
rg -n "^COMMENT ON TABLE" backend/scripts/sql/init.sql
cd backend
go test ./...
go vet ./...
~~~

两次表计数必须一致。由于本项目当前不维护迁移，DDL 修改后应使用空测试数据库完整执行 `init.sql` 验证，而不是在旧库上零散补字段。

### 状态域与事件权威性

`conversation_states` 仅保存 Runtime 明确声明的会话叙事字段；Campaign 的角色、位置、库存、关系、任务和其他长期事实分别以领域表和 `campaign_events` 为权威。`campaign.field_ownership` 与模块 Action 元数据在导入/发布时校验，禁止同一事实在两个域声明或通过动态子路径复制。任何 Campaign 写入必须在锁定 Campaign 的事务内同时更新领域表、追加事件和记录动作提交，事务提交前不得广播或依赖前端本地修改。项目为新项目，不增加旧字段 fallback 或双写兼容。
## 对话分享表

- `conversation_shares`：保存来源归属、幂等 request、创建时 branch epoch、连续范围端点、版本化 JSONB 快照、SHA-256 哈希、展示字段、状态和过期时间。`(player_id, request_id)` 唯一，active 查询和所有者列表有独立索引。
- `conversation_share_reports`：保存匿名举报原因、可选说明和 HMAC 派生的举报者指纹；`(share_id, reporter_fingerprint)` 唯一，避免同一来源重复创建。
- `conversation_share_audit_events`：保存管理员下架、解决举报和驳回举报事件；关联对象或管理员物理删除后外键置空，审计事件本身保留。

快照是匿名读取的唯一正文来源，数据库不保存完整分享 token。`conversation_shares.player_id` 和 `conversation_id` 随账号或对话物理删除级联清理；对话日常使用软删除，因此仓储还会在删除生命周期事务中把 active 分享标记为 revoked。表结构只由 `backend/scripts/sql/init.sql` 管理，不使用 AutoMigrate。
