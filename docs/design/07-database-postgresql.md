# 07. PostgreSQL 数据库设计
## 玩法模块基线

本次直接修改 `00001_init.sql`：`campaign_runtime_states` 保存纯模块的引擎、种子与状态；`campaign_checkpoints` 按回合保存完整 Campaign 可变事实；`module_action_commits` 以 `(conversation_id, branch_epoch, request_id)` 唯一记录玩家行动与结果。`gameplay_definitions.kind` 使用稳定标识约束，由对应已安装模块验证语义，不再逐题材扩张数据库枚举。

恢复在一个事务内按外键顺序重建领域表，校验固定内容与存档归属，推进 revision；不恢复账号余额、账本、奖励事实、种子或追加历史。模块领域校验由投影/行动边界继续执行。已执行旧基线的库不会自动更新；使用显式 Goose 和隔离 schema 验证。实现与未验项见[24](24-gameplay-module-architecture.md)及[验收记录](gameplay-modules/validation.md)。

## 定位与初始化原则

PostgreSQL 是 fateCraft 的运行时权威数据源，保存认证、挑战固定版本、对话状态、判定证据、结算、奖励账本和管理员设置。仓库 JSON 只是内置玩法的可导入源文件；玩法导入或发布后，生成过程只能读取数据库中与对话固定版本关联的内容。

当前项目从第一版 schema 开始使用 Goose 管理结构：

- `backend/migrations/00001_init.sql` 是新项目当前完整基线；奖励与兑换变更直接并入 00001，不新建 00002。基线冻结后才使用增量 migration。执行过旧基线的库不会自动补齐变化；验证使用空 schema，不自动清库。
- 不使用 GORM `AutoMigrate` 修改生产结构。
- 不保留 `accounts`、单一灵石字段、旧回溯计数或旧结局字段。
- 开发、测试、Docker 和生产环境都通过 `cmd/migrate up` 执行相同的内嵌 migration。
- migration 默认使用事务；任一 DDL、约束、种子或注释失败都会整体回滚。确需禁用事务的 migration 必须显式标注并说明原因。
- `cmd/migrate` 只接受 `up/status/version`，在连接数据库前拒绝 `down`，不再提供环境变量绕过。00001 的 `Down` 段主动抛错，即使用外部 Goose 工具回退也不会撤销版本记录或删除业务表。
- 迁移通过 PostgreSQL session advisory lock 串行执行。`MIGRATION_CONNECT_TIMEOUT` 默认 `10s`，限制首次连接；`MIGRATION_TIMEOUT` 默认 `10m`，限制包含连接、等锁、DDL 在内的命令执行。二者必须为正的 Go duration，错误配置直接报错；Ctrl+C / SIGTERM 取消操作并回滚尚未提交的事务。

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
  |     |-- campaign_runtime_states / campaign_checkpoints
  |     |-- campaign_stage_rewards（不随检查点恢复）
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
  |-- reward_ledger
  `-- reward_exchange_orders -> reward_exchange_revisions -> reward_exchange_offers

challenge_packs
  `-- challenge_versions
        |-- challenge_runtime_configs（一对一）
        |-- challenge_assets
        `-- challenge_outcome_rules（含 reward_points）
~~~

基线 migration 当前包含 67 张业务表，并为每张表提供 `COMMENT ON TABLE`。Goose 另行维护 `goose_db_version`。表注释用于数据库工具和后续维护，不代替本文的关系与事务说明。

## 认证与平台设置

### `users`

平台用户主记录，只保存展示资料、邮箱凭据状态和平台角色，不保存任何题材角色、局内状态或奖励余额。

重要约束：

- `username` 是必填的站内登录标识（2-32 个字符，不含 `@`、空白和控制字符），由唯一索引 `idx_users_username_lower` 保证按 `lower(username)` 大小写不敏感唯一；`email` 可空且全局唯一；第三方身份的 provider_email 只属于身份快照，不等同于站内登录邮箱。
- `password_hash` 只存 Argon2id 哈希，不存明文或可逆密文。
- `status` 仅允许 `active/disabled`。
- `role` 仅允许 `user/admin/owner`，默认值为 `user`；普通创建流程不会自动提权。
- 初始化完成后只允许一个 `owner`，角色转交、停用和降级由后端事务保护。
- `users` 使用仅匹配 `role = 'owner'` 的唯一部分索引强制最多一个 owner；不建立用于普通角色筛选的低基数索引。

### `auth_identities`

记录用户与外部身份提供方的绑定。`provider + provider_user_id` 唯一；LinuxDo 返回的用户名、邮箱、信任等级和原始安全资料属于身份快照，不参与玩法状态。外部身份不能脱离本地用户单独创建账号。

### `auth_sessions`

登录会话。浏览器 Cookie 保存随机原令牌，数据库只保存 64 字符 SHA-256 摘要。`expires_at` 默认由后端按 14 天计算；登出删除对应摘要。

### `oauth_login_flows`

LinuxDo OAuth 授权过程的一次性 state、purpose（登录/绑定）、绑定用户、PKCE `code_verifier` 和 `browser_binding_hash`（发起浏览器 nonce Cookie 的 SHA-256）。state 只保存摘要，完成、失败或过期后都不可复用；回调必须带回匹配的 nonce。

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

`llm_settings` 保存用户模式下每个用户自己的 OpenAI 兼容端点、协议、模型和生成参数；`api_key_encrypted` 只保存 AES-GCM 密文。该表没有 provider 列，端点能力档位只由服务端按 `base_url` 识别。`system_settings` 是主键固定为 1 的管理员单例，同时保存 `installation_completed_at`（首次安装向导完成时间）、判定/诊断设置、诊断日志清理开关与保留天数，以及平台模式公共 LLM 配置（公共配置没有 `public_llm_provider` 列）；`llm_access_mode` 为 `user_managed`（默认）或 `platform_managed`，平台模式不删除用户行。`client_llm_route_enabled`（默认 TRUE）决定是否在服务端选项之外额外给用户“客户端直连”选项，与 `llm_access_mode` 相互独立，由 owner 管理。修改初始默认值不会覆盖已有实例保存的开关状态。新设置默认使用 128000 上下文和 16384 最大输出 Token，应用层校验窗口、回复预留和格式余量的关系；用户和平台各自的 settings revision 用于审计。读取接口只返回 `has_api_key`/`public_llm_has_api_key`。

同一单例还保存服务端 LLM 出站代理组：`llm_proxy_enabled`（默认 FALSE，即显式直连）、`llm_proxy_url`、`llm_proxy_username`、`llm_proxy_password_encrypted`、`llm_proxy_ca_pem` 和独立的 `llm_proxy_revision`。代理密码只保存 AES-GCM 密文；URL 只允许 http/https 明确主机和端口，不含 userinfo、query、fragment 或业务路径。`llm_proxy_revision` 从 0 开始，每次代理分组保存递增，不跟随用户/公共模型 settings revision。普通系统设置保存不更新这些列，代理 PATCH 也不覆盖其他设置列；两者通过同一仓储和事务边界避免分组互相覆盖。通用 `GET /api/admin/system-settings` 不返回代理地址、用户名、CA 或密码状态，owner 专属投影另见 [10 API](10-api-websocket.md)。

`admin_audit_logs` 保存首次安装、角色变更、账号状态变更、全局/平台/认证配置更新等操作的脱敏记录；trace_id 独立于业务 request_id，配置审计与更新在同一事务，密钥/私有端点仅记录 set/replace/clear，不保存密码、Cookie、Authorization、初始化凭证或 LLM Key。LLM 出站代理保存写入 `llm_proxy_change`：记录修订、启用、地址、用户名、密码设置/替换/清除，以及 CA 是否配置和指纹；不记录密码密文或完整 CA PEM。奖励操作也在此保存审计，使用请求唯一部分索引、(created_at,id) 奖励分页索引和 record_id 表达式索引。

平台不持久化模型能力评分或健康状态。用户显式点击检测时执行一次无副作用请求，结果只返回当前页面，不改变生产调用路径。

每个逻辑模型调用都向 `llm_request_logs` 提交最终摘要，增加 trace_id、protocol、started_at、transport_attempts、可空 input/output_tokens、error_code、body_status 与 body_metadata。正文默认关闭：request_body 为合法空对象，响应为空；开启后保存有界正文/JSON 预览。网络重试复用日志 ID，正常工具循环的不同模型步骤分别记录。详情仅管理员可读。

`system_settings` 的详细正文范围使用 `detailed_llm_logging_scope`（默认 `all`，枚举 `all/user/conversation`）与可空 UUID `detailed_llm_logging_target_id`。CHECK 约束要求 `all` 无目标、指定范围有非零目标。目标按范围指向用户或对话，由设置仓储在新增目标或重新启用时校验存在性；不设置多态外键，删除目标不会扩大范围且仍可关闭开关。范围及目标 ID 随配置事务进入脱敏审计，范围外调用的正文状态为 `disabled`。字段仍在 `00001_init.sql` 定义。

自动清理默认开启并保留 30 天，可由管理员关闭或配置 1-3650 天。清理仅覆盖 `llm_request_logs` 和 `context_build_logs`，使用有界批次和 `SKIP LOCKED`；`conversation_share_audit_events` 按治理审计策略长期保留。模型表复用全局时间索引并增加 trace/attempt 索引；上下文增加全局 `(created_at,id)` 与 attempt 索引；管理员审计增加全局时间和 actor 索引。普通删除由 autovacuum 回收死元组，不保证文件立即缩小。

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

结局规则直接保存 `reward_points`，数据库 CHECK 限制为非负安全整数。积分属于版本快照，结局、可信条件和积分共同进入内容哈希；不再单独建立账号奖励规则表。

## 长期存档与对话运行时

### `campaigns`

长期玩法权威存档头，固定 `player_id`、挑战版本、内容哈希及创建时选择的 Character、Persona、Scenario。`revision` 每次成功领域动作加一，`event_sequence` 按实际事件条数递增，两者不能混用。`game_time` 只能单调推进，`random_seed` 不通过普通 API 暴露。

`state_hash` 是当前权威状态的规范化 SHA-256：覆盖存档头中影响叙事的字段以及全部 Campaign 领域表，按键排序并排除 `created_at`、`updated_at` 等非语义时间戳。服务创建存档时写入首个真实哈希，每次成功动作在同一事务内重算；检查点恢复后也必须重算。该哈希与 `revision` 一起用于拒绝过期叙事，不能单独作为并发版本使用。

### `campaign_events` 与 `campaign_action_commits`

`campaign_events` 是数据库触发器保护的只追加审计记录；`visibility` 区分 player、model、internal，玩家事件接口只返回 player。一次动作可产生多条连续事件。

`campaign_action_commits` 以 `campaign_id + idempotency_key` 为主键，保存动作 key、提交后 revision、事件区间、公开结果和 `narrative`。`narrative` 是稳定的 `ActionResult`：动作 ID、revision、`state_hash`、`goal_status`、有界 `changes`、`current_facts`、`preserved` 与断言约束；重放直接返回同一结果，不重新执行 Handler。表上另有 `state_hash` 与 `schema_version`，便于一致性审计。

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

`chat_messages` 是进入 LLM 上下文的内部消息，使用数据库分配的稳定 `message_seq` 并关联 turn/attempt。`content_blocks` 是唯一的模型内容字段，按原始顺序保存 `text`、`thinking`、`refusal`、`tool_call` 以及 provider-owned `replay`；因此 Chat 与 Responses 的协议差异不会扩散到数据库。`content` 仅作为普通文本索引/投影，工具调用和工具结果仍通过 `kind`、`tool_call_id`、`tool_invocation_id` 关联。同一模型步骤的完整调用批次只保存一条 assistant 声明，行级关联指向首个 invocation；其余调用由同一声明内的 call_id 对应各自 invocation 和 tool_result，不能为每个调用再插入空声明。消息上的 Token 计数只有在 `tokenizer_version` 与当前版本一致、且内容仍是相同的单文本块时才可复用。`display_messages` 是玩家时间线投影；关联 assistant 消息时，接口只从 `content_blocks` 提取安全的可读 `thinking` 文本，忽略 provider 原始加密字段，前端默认折叠并在流式思考期间展开。重新生成与剧情改写事务都会锁定目标、扣除平台级剧情改写额度、递增 branch epoch、失效旧分支 turn/message/Memory 并建立新用户回合；Campaign 事实按目标回合完整检查点恢复，奖励账本不回档；缺少检查点则拒绝。

Campaign 助手叙事在 `display_messages` 上额外保存 `campaign_revision`、`campaign_state_hash`、`validation_status` 和 `validation_detail`。`validation_status` 取值是 `not_applicable`、`passed`、`repaired`、`fallback`、`stale`、`failed`；只有通过校验或修复后的正文会进入玩家时间线。`display_message_campaign_actions` 以 `(display_message_id, action_id)` 关联同回合的全部来源动作，并用 `(campaign_id, action_id)` 复合外键指回 `campaign_action_commits`。该关联表用于追溯“这段叙事由哪些动作产生”，以及查询尚未被正式叙事消费的动作；系统动作回执不参与消费判定。

### `conversation_states` 与 `state_revisions`

`conversation_states` 保存当前 JSONB 状态和 Schema 版本。后端按对话固定 Runtime Contract 初始化、校验和投影；模型不能更新未声明或非 model 权限路径。

`state_revisions` 保存每次状态更新的来源、补丁、前后快照和关联 turn/attempt，用于审计。普通用户的当前状态和修订接口都只返回 `visibility=player` 的投影，父对象不会带出显式声明的 model/internal 子字段。

### `conversation_memories`、`memory_jobs` 与 `context_build_logs`

`conversation_memories` 是只追加版本链，保存覆盖边界、branch epoch、严格结构化内容、稳定渲染、evidence、模型和生成指纹；conversation 只指向当前有效链头。`memory_jobs` 保存来源边界、任务状态、退避、租约、fencing token 和结果，每个 conversation 最多一个 pending/running 任务。

`context_build_logs` 关联 turn/attempt 和固定 Memory，记录 token 预算、分段、完整回合取舍、Campaign Projection 压缩前 Token 与省略 key、世界书命中、Memory lag、省略回合范围和规划 job，便于定位模型输入问题。

### `character_inquiry_threads` 与 `character_inquiry_messages`

每个 conversation/player 最多一个独立只读问答 thread。消息固定 snapshot ID、branch epoch 和 revision，只保存回答正文与公开来源标签，不复制玩家状态、公开世界书规则或 Campaign 快照；它们不关联主叙事 turn，也不成为 Memory 来源。公开规则在请求时从对话固定版本的 required worldbook 读取，且条目必须启用并显式设置 `metadata.inquiry_visible=true`。`character_inquiry_rate_limits` 是按玩家原子更新的固定窗口计数器，不随用户清空问答历史而删除。

### `roll_history`

服务端判定证据。`roll_key` 是稳定规则键，`title_snapshot` 是当局展示标题，`category` 用于判定与审计。结局 verifier 只读取成功的稳定 key，不用中文标题作为安全标识。

## 积分、改写次数与结算

### `player_reward_balances`

`player_id + reward_key` 主键的余额投影。数据库与服务端只允许 `fatecraft.points`、`fatecraft.message-rewrite-credit`；不存在通用账号资产定义表。保存当前余额、授予/消费/返还/撤回累计值，全部为非负安全整数，余额等于累计净额。不能脱离账本修改。

### `reward_ledger`

只追加的实际收支事实，操作为 `grant/consume/refund/reverse`。保存 reward_key、quantity_delta、balance_after、来源、可选结算 ID、原流水关联、请求 ID/指纹、操作者、内部原因与公开说明。普通用户响应不包含管理字段和内部 metadata。

幂等键、管理员请求 ID、`(player_id,reward_key,operation,source_type,source_id)` 分别唯一。返还关联改写消费，撤回只关联原管理员发放；每笔各最多处理一次，触发器验证用户、项目、金额、原操作与来源并拒绝账本 UPDATE/DELETE。余额、账本、原业务事实和必要管理审计共同提交。

Campaign 来源为 `campaign_outcome`，source_id 为 Campaign ID，metadata 保存结局与固定版本。完成态据账本恢复积分；0 分保存于完成事实，不写零金额账本。参见 [21 终局积分](21-campaign-outcome-rewards.md)。

### `campaign_stage_rewards`

阶段领取事实在 `00001_init.sql` 中定义，以 `(campaign_id, stage_key)` 唯一，记录名称、配置积分、实际积分、停发原因和游戏时间。UPDATE/DELETE 由触发器拒绝，Campaign 外键为 RESTRICT。实际正积分与配置相同；0 分和客户端直连也保存事实，分别记录 `zero_points` / `client_llm_route`，不写零金额账本。

领取与 Campaign 事件、revision、余额、账本及动作提交处于同一事务。正积分账本来源 `campaign_stage`，source_id 为 `CampaignID:stageKey`，与终局的 `campaign_outcome` / CampaignID 分开。事实不进入检查点；改写后重新满足当前分支前置也不会再次发放，历史领取不替代新分支的任务和胜利事实。规则与完整事务边界见 [21 阶段与终局积分](21-campaign-outcome-rewards.md)。

### 注册与兑换

`registration_reward_rules` 只追加两项赠送数量的修订，最新 ID 生效，初始为 0 积分/10 次。`users.registration_reward_revision` 引用注册实际采用的规则，即使两项为 0 也保存，用于防止重试套用新规则。

`reward_exchange_offers` 固定积分支付、改写次数所得，保存当前配置和 revision。`reward_exchange_revisions` 保存不可变配置与操作者，初始商品为 100 积分兑换 1 次，系统初始化 actor_id 为空。`reward_exchange_orders` 引用商品修订、支付和所得两条 ledger，保存单价、份数和合计快照，request_id 唯一。注册修订、商品修订、订单都拒绝 UPDATE/DELETE。扣积分、加次数与订单同事务；没有兑换退款接口。

账本、订单和管理审计按 (created_at,id) 游标分页。账本可筛选用户、项目、来源和时间；用户搜索保持字面匹配与分页索引。数字业务上限为 9007199254740991。详见 [22 奖励管理](22-reward-management-and-exchange.md)。

### `challenge_runs` 与 `challenge_run_rewards`

每个对话最多一条结算，保存固定版本、outcome、摘要和可信结算详情，detail.reward_points 为实际所得；直连实际为 0 并记录原因。实际正积分生成至多一条 `challenge_run_rewards` 投影，引用账本并保存积分名称、数量和入账后余额。0 分不写该投影。

## 关键事务边界

### Agent 工具提交

完整 Tool Call 先以 `received` 持久化。成功路径在同一短事务提交领域事实、`committed` invocation、assistant tool-call 消息（同一步骤仅首项写完整批次）、各自 tool-result 消息和 attempt 写工具计数。后续结果必须匹配同一步骤已有声明中的调用 ID、名称与参数哈希。Handler 返回业务拒绝或失败时，内部哨兵会回滚事务中全部领域写入，再单独保存 invocation 终态和公开结果。数据库事务提交失败不得广播状态、Campaign、判定或结算事件。

### 挑战发布

1. 锁定草稿版本及其 Challenge Pack 行；同一 Pack 的并发发布因此串行化。
2. 读取 Runtime、资产、结局、奖励规则和定义。
3. 校验注册表能力、状态路径、Prompt、世界书、奖励快照与前置条件。
4. 计算规范化内容哈希。
5. 将版本置为 published，并切换 Pack 当前版本。

列表读取统一采用有界、稳定顺序：conversation 按 `updated_at DESC, id DESC`，消息按 `(created_at, id)` 复合游标，Campaign 事件按定义的时间/优先级/模板/occurrence/id，奖励账本按 `created_at DESC, id DESC`。API 层拒绝非法或超限 `limit`，仓储层再次执行上限保护；不存在的消息 anchor 不会静默退化为第一页。

奖励展示使用两项平台常量；收支从账本读取并关联原发放的撤回记录。跨余额操作按固定 reward key 顺序加锁，以保持并发扣费与发放正确性。

### 挑战结算

1. 锁定 conversation，确认仍 active 且尚无 run。
2. 读取可信状态与成功判定 key，执行固定版本 verifier。
3. 读取固定结局积分，直连实际所得为 0。
4. 写 `challenge_runs`。
5. 正积分追加 ledger、更新 balance、写 run reward 快照；0 分仅记录于结算详情。
6. 将 conversation 置为 ended。

### 奖励消费型功能

检查余额、追加 consume 账本、更新 balance 和目标业务写入必须在同一事务。例如重新生成或消息改写不能先扣额度后再用补偿记录返还。

## 删除与保留策略

- 部分用户数据有级联关系，但奖励账本、获奖、修订和订单的外键限制物理删除相关用户；后台采用停用，不提供清除历史奖励的入口。
- 物理删除对话会级联回合、attempt、消息、状态、Memory、询问、判定、上下文和结算；产品层普通删除采用软删除状态。
- 被当前版本引用的挑战版本受外键保护；已发布历史版本不应物理删除。
- 奖励账本由触发器保护，不参与普通清理。
- Prompt、世界书和运行规则随挑战版本保留，以保证历史对话可复现。

## 维护检查

Schema 变更至少执行：

~~~powershell
rg -n "^CREATE TABLE" backend/migrations
rg -n "^COMMENT ON TABLE" backend/migrations
cd backend
go run ./cmd/migrate status
go vet ./...
# 必须在进程环境配置专用的 FATECRAFT_TEST_DATABASE_URL，不从 .env 自动加载。
$env:FATECRAFT_REQUIRE_TEST_DATABASE = 'true'
go test -count=1 -timeout=10m ./...
~~~

两次业务表计数必须一致。仓库根目录的 `task backend:test:postgres` 提供同样的强制验证流程：没有 `FATECRAFT_TEST_DATABASE_URL` 会失败，不会以跳过集成测试视为通过；CI 使用相同规则。集成测试在随机 PostgreSQL schema 中执行同一套 migration，覆盖首次初始化、重复与并发 `up`、拒绝基线 `down` 后版本和表仍保留，以及失败 migration 的事务回滚。

当前基线未冻结，结构修改仍集中在 00001；Goose 不校验已执行文件的内容，旧开发库即使显示版本 1，也可能缺少后来加入的表或约束。每次修改应在空数据库或上述隔离 schema 中验证，不自动删除、重置或改写已有库的版本记录。开始保留正式数据后冻结基线，此后已执行 migration 不得修改，DDL 修正改用新增 migration，并同时在空数据库和已迁移数据库验证 `up`。

### 状态域与事件权威性

`conversation_states` 仅保存 Runtime 明确声明的会话叙事字段；Campaign 的角色、位置、库存、关系、任务和其他长期事实分别以领域表和 `campaign_events` 为权威。`campaign.field_ownership` 与模块 Action 元数据在导入/发布时校验，禁止同一事实在两个域声明或通过动态子路径复制。任何 Campaign 写入必须在锁定 Campaign 的事务内同时更新领域表、追加事件和记录动作提交，事务提交前不得广播或依赖前端本地修改。项目为新项目，不增加旧字段 fallback 或双写兼容。
## 对话分享表

- `conversation_shares`：保存来源归属、幂等 request、创建时 branch epoch、连续范围端点、版本化 JSONB 快照、SHA-256 哈希、展示字段、状态和过期时间。`(player_id, request_id)` 唯一，active 查询和所有者列表有独立索引。
- `conversation_share_reports`：保存匿名举报原因、可选说明和 HMAC 派生的举报者指纹；`(share_id, reporter_fingerprint)` 唯一，避免同一来源重复创建。
- `conversation_share_audit_events`：保存管理员下架、解决举报和驳回举报事件；关联对象或管理员物理删除后外键置空，审计事件本身保留。

快照是匿名读取的唯一正文来源，数据库不保存完整分享 token。`conversation_shares.player_id` 和 `conversation_id` 随账号或对话物理删除级联清理；对话日常使用软删除，因此仓储还会在删除生命周期事务中把 active 分享标记为 revoked。表结构只由 `backend/migrations` 中按顺序执行的 SQL 管理，不使用 AutoMigrate。

## LLM 请求路由列

- `llm_settings.llm_route`（`server`/`client`，默认 `server`）+ 约束 `chk_llm_settings_client_no_key`（`client` 时 `api_key_encrypted` 必须为空串或 NULL；Go 空串按 `''` 存库，与 `HasAPIKey = APIKeyEncrypted != ""` 约定一致）。
- `campaigns.llm_route`（无默认值，创建显式写入）+ UNIQUE 扩展为 `(id, player_id, challenge_version_id, challenge_hash, llm_route)`。
- `conversations.llm_route`（无默认值）+ `fk_conversations_campaign` 扩展为含 `llm_route` 的复合外键，保证对话路由等于所属 Campaign 路由。
- `trg_conversations_llm_route_immutable` / `trg_campaigns_llm_route_immutable`：`BEFORE UPDATE` 触发器，路由变化直接报错。
- `llm_request_logs.settings_source` 扩展为 `('user', 'platform', 'client')`，`client` 表示原始响应由浏览器回传。

本次日志字段与索引仅修改 00001 基线，不新增实际 00002；已初始化的旧库不会自动同步。完整预算、配置并发冲突及隔离测试方法见[日志、诊断与配置审计](23-logging-and-diagnostics.md)。

## 玩法包交换与版本管理

00001 新增 gameplay_imports、Pack 来源/管理修订、Version 内容修订/运行策略/来源摘要及账本索引。写栅栏持有版本共享锁，策略变更按包→版本锁顺序持有排他锁。开局在事务内检查当前入口，版本绑定不可改。新项目只更新 00001，不新增 00002、不自动迁移或清库。

完整协议见 [27 玩法包交换与版本生命周期](27-gameplay-package-exchange-and-versions.md)。


### 活动稳定版本

新项目直接在 00001_init.sql 定义 conversation_activity_versions(conversation_id, version, transaction_id)、最新 memory job 索引和 record_activity_change 延迟约束触发器，无历史回填或增量迁移。最新有效 turn 查询复用 `(conversation_id, turn_seq)` 唯一索引，按 turn_seq 倒序读取并过滤失效行。独立版本行在业务写入后更新，避免新增 memory job 与 conversation 的反向锁依赖。

触发器覆盖 conversations、conversation_turns、generation_attempts、chat_messages、memory_jobs 的稳定字段。同事务同对话最多递增一次，回滚同时撤销版本和 NOTIFY；续租、step_count、纯 phase 不递增。相关 INSERT/DELETE 保守失效，版本不表示精确内容变化次数。按 schema 隔离的 pg_notify 在提交后交付；它不是可重放事件日志。

活动 UPDATE 触发器使用显式稳定列的 WHEN ROW(...) IS DISTINCT FROM ROW(...)，在入延迟队列前过滤无变化、正文与心跳更新，不序列化整行 JSONB。INSERT/DELETE 与 UPDATE 分开定义，版本仍延迟至事务提交。00001 增加 idx_chat_messages_active_turn_role：chat_messages(conversation_id, turn_id, role) WHERE invalidated_at IS NULL，用于活动能力的消息存在性查询。本轮仅验证结构与功能，未做查询计划或性能对比。

### 索引精简

按当前仓储查询清理 10 个普通索引，00001 中显式 CREATE INDEX（含 UNIQUE）由 96 个减为 86 个，其中显式 UNIQUE 仍为 14 个；此计数不含主键和 UNIQUE 约束自动创建的索引。所有主键、唯一约束及组合外键保持不变。

| 移除索引 | 依据 / 现有访问路径 |
| --- | --- |
| idx_agent_steps_attempt | 与 `(attempt_id, step_no)` 唯一约束重复 |
| idx_conversation_memories_chain | 固定 conversation_id 后，`(conversation_id, version)` 唯一索引可倒序扫描 |
| idx_email_verification_lookup | 当前只查未消费验证码，`idx_email_verification_active` 部分唯一索引保证最多一条，无须按创建时间排序 |
| idx_challenge_outcomes_version | 列表和单条查询使用 `(challenge_version_id, outcome_key)` 唯一索引；单条查询再检查 enabled |
| idx_activity_latest_turn | 复用 `(conversation_id, turn_seq)` 唯一索引；同对话 turn_seq 唯一，无须 id 排序 |
| idx_invitation_codes_status | 当前无 enabled / expires_at 筛选列表查询 |
| idx_gameplay_definitions_tags | 当前按版本加载，标签在应用层处理，无 SQL 标签检索 |
| idx_player_reward_balances_reward | 当前按玩家及奖励键读取，无跨玩家奖励余额排行 |
| idx_campaign_action_commits_hash | 按幂等键或 revision 读取后校验哈希，无哈希检索 |
| idx_campaign_events_idempotency | 事件按 sequence 读取，幂等检查由 action_commits 主键承担 |

活动查询保留 `idx_chat_messages_active_turn_role` 和 `idx_activity_latest_memory_job` 两个专用索引。最新回合复用唯一索引会过滤失效回合，若将来出现大量连续失效回合，需根据实际查询表现重新评估部分索引。本次仅做结构与功能回归，不做性能测试，不据此宣称 CPU 降幅或容量提升。后续新增索引应对应具体筛选、排序或约束需求，不为尚未实现的查询预建索引。
