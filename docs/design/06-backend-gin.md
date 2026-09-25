# 06. Go 后端设计

## 技术与入口

后端使用 Go、Gin、GORM、PostgreSQL、WebSocket 和 zap。应用入口是 `cmd/server`，负责首次内容初始化、HTTP、WebSocket、LLM 和游戏运行时。

`cmd/server` 不运行 AutoMigrate，表结构只由 `scripts/sql/init.sql` 初始化。连接数据库后，server 在事务级 advisory lock 内检查 `challenge_packs`：为空时读取 `preset_packs` 并通过正常发布流程初始化内置挑战；非空时直接跳过，不读取或修改内置内容。

## 模块划分

```text
internal/api             Gin 路由、中间件、参数和 HTTP 响应
internal/auth            邮箱认证、LinuxDo OAuth、会话生命周期
internal/challenge       挑战查询、版本、哈希、发布、入口要求与通用结算
internal/challengeimport 仓库材料解析、显式导入与空库初始化策略
internal/gameplay        玩法注册表、Runtime Contract 和通用可信规则
internal/gameplay/*      需要专用语义的题材模块，例如 cultivation
internal/requirement     奖励余额及 all/any 入口条件验证
internal/conversation    对话创建、归属、状态机和 durable generation run
internal/chat            内部消息与展示消息写入
internal/state           默认状态、白名单补丁和修订记录
internal/contextbuilder  token 预算和消息装配
internal/worldbook       纯世界书关键词扫描算法
internal/memory          异步长期记忆契约、调度、租约、Worker 与版本链
internal/inquiry         玩家可见快照与角色栏只读问答
internal/operation       不可回滚副作用检查
internal/roll            判定计算、掷骰和历史调整
internal/orchestration   主叙事 Agent 协调器、工具目录、执行与事务
internal/realtime        WebSocket 连接与事件广播
internal/storage         GORM 模型与 PostgreSQL 仓储
internal/llm             Chat/Responses 文本、Tool Call、JSON Schema 与流式适配
internal/modelcapability 用户显式触发的无副作用模型诊断
```

### LLM 能力规划与传输边界

`internal/llm` 在进入协议 Adapter 前使用服务端维护的 Provider Profile/Model Capabilities 矩阵规划请求。矩阵描述协议、流式终止事件、reasoning、工具调用、结构化输出、图片输入和可选参数能力；前端和玩法包不维护模型名称分支。明确不支持的能力会在请求前裁剪或返回可解释错误，未知能力对自定义兼容网关保持保守透传。

模型 HTTP 传输只在收到响应体之前对连接错误、429、502、503、504 和明确的临时不可用执行有限指数退避重试，并尊重 `Retry-After`。收到 2xx 流后由解析器负责一次性消费；正文、工具调用或结构化结果已经开始后不重放 Agent 步骤，避免重复领域副作用。所有 SSE JSON 解析错误、提前 EOF 和缺少协议终止事件均为失败，不得把临时文本当作最终结果。

模型边界按字节限制：完整响应/累积正文最多 32 MiB，单条 SSE 行最多 1 MiB，HTTP 错误体最多 64 KiB，详细审计请求、原始响应和正文各最多 2 MiB（超出只截断审计副本并标记 `audit_truncated`，不改变真实解析结果），工具参数最多 16 KiB。普通 HTTP JSON body 默认 4 MiB，挑战版本 PUT 明确放宽到 32 MiB；WebSocket 客户端帧最多 64 KiB、生成排队最多 64 条。PostgreSQL 连接池默认限制为 10 个打开连接、3 个空闲连接，连接生命周期默认 30 分钟、空闲回收默认 5 分钟；四项均可通过 `DB_*` 环境变量按实例配置，多实例部署必须汇总连接预算。

领域服务依赖消费端定义的小接口或专用仓储，API handler 不直接拼装 SQL。组合根在 `cmd/server/main.go` 创建仓储、服务、生成器和 Hub。

## 请求处理层次

典型受保护请求经过：

```text
Gin middleware
  -> session authentication
  -> identity in request context
  -> optional requireAdmin
  -> API handler binding
  -> domain service
  -> storage repository
  -> PostgreSQL
```

Handler 负责 HTTP 语义，不实现业务授权。领域服务不能相信请求体中的用户 ID；当前用户通过 `identity.Provider` 获取。

## 认证

会话令牌使用加密安全随机数生成，响应只通过 HttpOnly Cookie 发送。数据库保存 SHA-256 哈希和过期时间。密码使用 bcrypt 哈希。

邮箱认证策略和 SMTP 配置来自单行 `auth_settings`，每次登录、发码或注册都读取服务端设置。验证码只保存 bcrypt 摘要并限制有效期、重发和尝试次数；邀请码只保存 SHA-256 摘要。注册事务负责消费验证码与邀请码、创建用户，并在平台定义了初始奖励时写入奖励余额和账本；任何一步失败都会整体回滚。SMTP 密码通过部署环境变量 `AUTH_DATA_ENCRYPTION_KEY` 加密，管理 API 只暴露是否已经配置。

用户角色固定为 `owner`、`admin`、`user`。空库通过 `/setup` 首次安装向导直接提交本地用户名、邮箱和密码，服务端在事务中创建唯一 `owner`，同时写入 `system_settings.installation_completed_at`、初始奖励和管理员审计记录；安装完成前邮箱和 LinuxDo 登录关闭，完成后普通注册开放且后续用户默认都是 `user`。`requireAdmin` 允许 owner/admin 访问日常运营接口，`requireOwner` 保护角色、认证策略和平台密钥等最高权限接口。认证中间件每次从数据库读取当前用户状态和角色，因此停用或降权会立即使旧会话失去对应权限。

LinuxDo 流程保存哈希后的 state、PKCE verifier、purpose、绑定用户、return path 和短期过期时间。LinuxDo 只能作为 `auth_identities` 中绑定到本地用户的外部身份；未绑定的 subject 不会自动创建站内用户。绑定回调要求发起时已有本地会话，解除绑定要求保留密码或其他身份。回调必须同时验证 state、code、provider 和过期时间，再交换 token。Client Secret、上游 token 和数据库连接串不得写入日志或 API 响应。管理员的 `/admin/auth-identities` 仅返回脱敏绑定摘要。

`requireAdmin` 必须位于 `requireAuth` 之后，并只信任已加载的 `storage.User.Role`。

## 挑战内容

`challenge.Service` 负责三种读取：

- 公开挑战：只返回公开 Manifest 字段、允许 Persona 和奖励概要。
- 管理文档：只供管理员读取完整版本、资产和奖励规则。
- 运行 Bundle：按对话固定版本加载 Character、Persona、Scenario、Prompt 和 WorldBook。

公开 API 不能复用完整管理结构，否则会泄漏技术 Prompt 和世界书。

### 玩法注册表与 Runtime

`gameplay.Registry` 在 server 组合根显式注册可用 engine、model/player actions、Campaign actions、outcome verifier、reward calculator 和 settlement handler。挑战版本只能引用已安装的稳定 key 和版本，管理员不能上传代码、SQL 或任意表达式。

每个版本的 Runtime Contract 声明 State Schema、动作、玩家能力、结算处理器和 Presentation。State Schema 决定字段类型、默认值、约束、可见性与更新权限；普通用户状态接口只返回 `player` 可见投影，`model` 和 `internal` 字段不能因返回父对象而泄漏。Presentation 只允许引用玩家可见字段。

### 草稿保存

保存草稿会锁定版本行、确认状态仍为 draft，然后在事务中替换 Manifest、资产和奖励规则。已发布版本返回冲突，不允许更新。

### 发布

发布事务执行：

1. 对版本行加 `FOR UPDATE`。
2. 读取同一事务内的 Manifest、资产和奖励规则。
3. 校验挑战包元数据、Manifest 引用、资产 ID、Runtime 模块、State Schema、三个核心 Prompt 分段、世界书、Outcome、verifier、calculator 和奖励规则。
4. 对 JSON 规范化并计算 SHA-256。
5. 把版本设为 published，并更新挑战包当前版本。

保存和发布锁定同一行，防止发布读取后又被并发保存导致哈希过期。

### 已发布内容缓存

`challenge.Service` 持有进程级有界只读缓存。缓存键由 `challenge_version_id + content_hash` 组成，只接收 `published` 和 `retired`；`draft` 始终读取 PostgreSQL。首次读取会加载完整 VersionDocument，验证内容哈希，并一次性编译 Manifest、RuntimeContract、资产索引、按 `kind:key` 的玩法定义索引、排序后的 Prompt、世界书条目、角色询问公开规则和公开挑战投影。同版本并发冷读通过进程内 flight 合并，失败不写空缓存。

缓存同时受版本数与估算字节限制，默认 `16` 个版本、`256 MiB`，分别由 `CHALLENGE_CACHE_MAX_VERSIONS`（1-64）和 `CHALLENGE_CACHE_MAX_MIB`（16-1024）配置。单个超过字节上限的版本可以读取但不驻留；淘汰使用固定顺序，不影响 PostgreSQL 权威数据。每个实例维护自己的本地缓存，不需要 Redis，因为已发布版本不可原地修改。

缓存不得包含 Conversation State、消息、Memory、Campaign 可变事实、权限、密钥或模型回复。Campaign 与 Inquiry 只通过窄 Provider 复用版本定义和世界书规则；无 Provider 的仓储测试路径直接读取数据库但不再维护第二套缓存。运行检查接口只返回命中、未命中、加载失败、冷加载合并、淘汰、驻留条目、估算字节和累计加载/编译耗时，不返回缓存正文。

## 对话创建与运行

单局对话服务只接收挑战 slug、Persona slug 和标题。Challenge Service 解析并验证发布版本，Conversation Service 写入固定版本和资产，随后初始化状态与开场消息；campaign 模式会拒绝这条入口。

长期玩法先由 Campaign Service 创建权威存档，再从固定 Campaign 创建唯一的持续 Conversation。对话与初始状态在 Campaign 行锁事务中写入，数据库和仓储共同保证一个 Campaign 最多绑定一个 Conversation；该对话不会跟随挑战包后来切换的 current version。

所有会改变叙事的操作先调用 `EnsureActive`，同时检查归属、状态和当前玩法版本。旧版本普通对话即使因旧数据未及时标记，也会在写入边界被封存并拒绝生成。只读操作使用 `GetOwned`，允许查看 ended 或 archived 内容。

## 生成管线

所有普通发送、重新生成、续写和编辑改写都进入同一生成操作协议。请求携带客户端 `request_id`，服务端在 durable run 事务中记录操作类型、目标消息和 payload hash；重复请求只回放已有操作，不重复调用模型、消费奖励或替换分支。普通发送通过 `BeginUserTurn` 原子创建 turn/attempt、内部用户消息和展示消息；重新生成通过 `BeginRegenerate` 在锁内解析当前回合，并在同一事务中消费平台级剧情改写额度后原子替换分支；编辑改写通过 `BeginRewrite` 将奖励消费、分支失效、新回合和编辑后的消息绑定放在同一事务中。旧的“先删消息、再创建回合、最后认领展示消息”的多阶段流程不再使用。

普通生成流程：

1. 校验对话归属和 active 状态，通过统一 `llmsettings.Service` 解析当前有效 LLM 设置（用户模式读取用户行，平台模式读取 `system_settings` 公共配置），在事务中获取唯一的 durable generation run；`is_processing` 只是兼容性忙碌投影。
2. 创建逻辑 turn 与 generation attempt，固定 branch epoch、设置修订和已提交 Memory 链头；一个 attempt 最多执行 8 个模型步骤。
3. 首次步骤装配并校验对话固定 Bundle，后续步骤复用该不可变 Bundle；每步重新读取可信状态和当前 Campaign Projection，按 Runtime 构建工具快照。Campaign 动态能力会转换为 `fc_action_*` 工具，并从 Projection 正文移除。
4. Context Builder 为当前回合、required Prompt、工具定义和最大回复预留预算，按完整回合边界装配历史并写上下文日志；Memory 滞后只规划后台任务，不阻塞叙事。
5. 第一步固定 attempt 的工具目录版本与 Runtime hash；每步保存工具定义、快照哈希、Handler 版本和 Campaign revision 到 `agent_steps`。
6. LLM Client 先由 Provider Profile 选择唯一协议 Adapter，在序列化前用 `PrepareToolReplay` 校验并修复历史中的工具调用/结果配对，再向 Chat Completions 或 Responses 提供工具并省略可选的 `tool_choice`，使用协议默认 `auto` 语义。缺失结果只补明确失败结果，孤立结果丢弃，重复/冲突 ID 在本地阻止请求；整理不执行工具、不改变领域事实。步骤结束后按完整统一结果判定：只要存在工具调用就进入工具 lane，伴随文本视为中间内容；否则保存普通文本。
7. 文本通道完整结束后保存最终 assistant/display message 并封口 attempt；工具通道先持久化 invocation，再由可信 Handler 执行，将公开 tool result 加入历史后进入下一模型步骤。用户点击“继续”时，触发词也按普通用户消息同时写入内部历史和展示时间线。
8. 最多允许 6 个写工具成功提交；第 7 个写调用在执行前返回 `mutating_tool_limit`。工具拒绝可以由下一模型步骤消化，不触发内部模型重试。
9. 写工具已提交但最终文本失败时，attempt/turn 标记为 `partial`，已提交事实保留；用户可以使用只提供文本通道的“继续叙事”收尾。
10. 封口后异步规划 Memory，结束当前 run 并更新对话时间；只有拥有该 run 的 Worker 可以释放兼容性忙碌投影。

流式模式会把模型步骤中的 reasoning 和正文作为临时预览通过 WebSocket 推送；在同一连接内，相邻同键增量会按时间/大小阈值合并，以避免高频帧挤满出站队列。步骤完整解析后若发现工具调用，服务端发送 `agent.text.reset` 并丢弃伴随文本；只有无工具的完整步骤才保存最终消息。工具参数和未完整结束的临时文本不进入玩家时间线，控制与事实事件不会因队列满而静默丢弃。完整协议见 [19-agent-tool-loop.md](19-agent-tool-loop.md)。

## 服务端判定工具

模型调用 `fc_request_roll` 时只提交稳定判定 key、标题和行为说明。Handler 从固定 Runtime 与可信状态读取规则、执行前置门禁、计算概率并使用服务端随机源掷骰。

判定完成后，服务端在 `display_messages.metadata` 写入版本化的 `roll_outcome` 快照（成功率分解、归一化成功区间和结果等级）、`roll_category`、`details_version` 以及按稳定 key 排序的 `conditions`。条件快照与门禁使用同一份可信状态；只有 State Schema 明确声明为 `player` 的字段才保留期望值和实际值，存在公开 Presentation 字段标签时才保留技术 key 作为稳定标识。没有公开标签的值可以使用通用“前置条件”文案，但不得把技术路径当作玩家文案。`model`/`internal` 条件最多保留满足状态，不返回原始路径或值；条件数量、文案和序列化内容有界。Campaign 领域判定复用同一 `roll_outcome` 契约，暂无 Runtime requires 时写入空条件数组。详情只供玩家展示，不进入模型上下文，也不新增数据库字段。

`roll_history`、展示消息、system 状态效果、`state_revisions`、invocation 与 tool call/result 消息在同一工具事务提交。门禁拒绝会回滚所有领域写入，只保存公开拒绝结果；下一模型步骤依据已提交工具结果继续叙事。模型可以在后续步骤再次请求当前快照允许的判定，但总步骤数与写工具数仍受统一上限约束。

管理员难度来自 `system_settings`，不接受用户或模型覆盖。

`system_settings` 是单例热读数据，仓储提供 5 秒进程内缓存；当前实例保存后立即刷新，多实例最多在 TTL 内收敛。缓存返回值始终复制，调用方不能修改共享对象，从而避免每次模型调用为了日志开关和接入模式重复查询 PostgreSQL。

模型请求详细日志同样由 `system_settings` 全局控制且默认关闭。开启后，LLM Client 为每个逻辑模型请求保存最终去认证请求 JSON、原始 HTTP/SSE 响应、合并正文、结束原因、`[DONE]` 状态、输出类型、设置修订、`settings_source`、协议、模型、契约指纹、传输/结构成功状态、稳定错误码、耗时与错误，并按 purpose 关联 attempt、Memory job/预分配候选版本或 Inquiry snapshot。响应体交付前的传输重试次数只写入结构化服务端日志，不创建独立业务 attempt。主叙事一个 attempt 可以因真实工具循环产生多条 `purpose=narrative` 调用；显式模型诊断使用 `purpose=protocol_test` 单独记录。日志写入失败只记录服务端告警，不改变模型调用结果。

诊断日志自动清理由独立的 `diagnostic_log_cleanup_enabled` 控制，默认保留 30 天。Worker 每小时读取最新设置，以每批 500 条、单轮最多 20 批的短事务清理 `llm_request_logs` 与 `context_build_logs`；多实例通过 `SKIP LOCKED` 避免争抢同一批记录。分享治理审计不受该短期诊断保留策略影响。

Memory Worker 由进程根 context 驱动，不继承 HTTP/WS 取消，也不占用 durable generation run。它通过 PostgreSQL `SKIP LOCKED`、带 fencing token 的租约和事务 CAS 提交版本；暂时性上游错误可以按持久任务策略后台退避，单次调用不会因解析或契约错误重放，但响应体交付前的临时传输错误仍遵循统一的有限重试边界。Character Inquiry 使用独立 thread 和非流式低温 JSON Schema 调用，读取玩家状态、显式标记为公开的当前版本规则，并在 Campaign 中加入玩家 Snapshot；它不获取主生成 run，不进入主消息、Context Builder、Memory 来源或任何玩法动作 Handler。

列表接口在 handler 层拒绝非法或超限 `limit`，仓储层仍保留安全上限作为纵深防御：conversation 1–100、messages 1–500、state/reward/admin logs 1–200、Campaign events 1–100。消息游标按 `(created_at, id)` 排序与比较，不存在或不属于当前 conversation 的 anchor 明确返回错误；“我的分享”同样使用 `(created_at, id)` 游标与 `page_size + 1` 判断 `has_more`，不执行每页 `COUNT` 或深 `OFFSET`。标题搜索会转义 `%`、`_` 和反斜杠，并使用 trigram 索引。

### 显式模型诊断

平台不保存模型能力评分，不维护漂移窗口、冷却或自动恢复状态机，也不根据诊断结果改变生产请求。保存设置不会自动探测。

用户保存当前设置后可点击“检测”，后端同步执行一次无副作用诊断：主叙事使用与生产相同的默认工具选择语义，验证真实 Tool Call -> Tool Result -> 普通文本闭环；Memory 和 Inquiry 分别验证其所选模型的原生 JSON Schema。能力结论属于 Base URL、协议与实际模型的组合。只有上游明确拒绝当前能力才返回 `unsupported`；模型本次未调用工具、nonce 不正确或无法归因的参数错误返回 `inconclusive`；网络、鉴权、超时与服务异常返回 `unavailable`。结果不入能力表，也不成为运行期门禁。

## 状态更新

状态工具按值类型提交有界 `updates` 数组。每项只允许当前工具快照给出的 path、operation 和 value 组合，同一路径不能重复。系统字段、未知路径、危险父对象覆盖、错误类型、范围、枚举或追加方式会拒绝整个工具调用，不逐键跳过。

状态读改写使用事务和行锁，成功后写入 `state_revisions`。普通用户没有状态 PATCH 路由；管理员修改使用 `SourceSystem`，模型修改使用 `SourceAI`。

## 动作与结算

模型动作先由当前步骤工具快照约束，再通过注册表分发。通用 `fc_complete_challenge` 只接收稳定 `outcome_key` 和摘要；Handler 采集认证玩家、持久化状态、服务端判定及回合证据，再调用 Challenge Service。题材模块只提供注册后的可信规则，不把题材字段写回通用动作。

Campaign 生产与经济动作使用同一可信入口。`craft.learn_recipe`、`craft.execute_recipe`、`trade.buy` 和 `trade.sell` 可由玩家或模型提交，`market.adjust_price_modifier` 仅允许 system；所有参数使用稳定定义 key，客户端和模型不能提交成功结果、品质、价格或补货量。制作在写入前完成配方知识、地点、境界、法力、技艺、器具和全部材料校验；交易在写入前完成当前位置、商品、库存、收购容量、货币或装备实例校验。动作 handler 产生公开结果和领域事件，Campaign Repository 负责行锁、revision、幂等、时间与事务提交。

市场补货只依赖 Campaign 游戏时间。读取投影可以预测已跨过的完整补货周期，实际买卖会在同一事务中先持久化补货，再结算库存和价格；不启动现实时间后台任务。买卖价格统一复用 `marketrules`，依次应用版本市场修正、动态市场修正、商品修正、库存稀缺和势力声望，避免展示价与结算价使用两套算法。

结算工具在事务内锁定对话并重建证据，执行固定版本的 outcome verifier 和全部 reward calculator。`challenge_runs`、`challenge_run_rewards`、`player_reward_balances`、只增不改的 `reward_ledger`、对话状态、invocation 与 tool 消息原子更新；动作 Handler 不直接写余额。条件不满足是可观察的业务拒绝，数据库、事务或持久化失败才是系统错误。

消息改写与重新生成共享平台级能力，公开描述通过 Runtime API 的 `platform_capabilities` 返回，但授权策略不由 Runtime Contract 决定。其奖励扣除、新分支创建、旧 turn/message/Memory 失效和新用户消息必须在一个仓储事务中完成；存在已提交 Campaign 动作或事件的回合不能通过普通 regenerate/rewrite 回滚。

## 错误映射

- 资源不存在或挑战未发布：404。
- 内容、选择或结算参数无效：400。
- 对话状态冲突、重复结算或修改已发布版本：409。
- 未登录：401。
- 非管理员调用管理接口：403。
- 未分类内部错误：500，并写结构化日志。

归属不符必须映射为 404。不要把数据库错误、密钥或上游完整响应直接返回客户端。

## 日志与可观测性

Gin 请求、panic、GORM 慢查询和业务异常统一进入 zap。重要日志应带 `conversation_id`、`challenge_version_id` 或用户 ID，但不得包含密码、会话明文、LinuxDo Secret、LLM API Key 和完整数据库 URL。

生成和结算日志按语义分级：

- Info：用户取消、对话已结束、资源不存在或同一对话已有生成任务等正常分支。
- Warn：模型工具参数或输出形态异常、奖励申请被拒绝、参数或数据库约束未通过等需要观察但未破坏系统的行为。
- Error：数据库执行故障、事务失败、模型服务失败、配置缺失或无法持久化等系统异常。

WebSocket 的 `error` 是“本次操作无法继续”的客户端事件类型，不等同于服务端 Error 级别。服务端必须根据原始 error 类型分级，不能根据客户端事件名称统一记 Error。

Runtime Presentation 支持题材无关的 `guidance`。发布校验限制分组、项目、文案、条件深度和参数数量；`message` 项仅作为低信任玩家输入，`action` 项必须属于当前 Campaign `action_allowlist`。普通挑战的 guidance 在 runtime API 中按玩家可见 state 投影，投影只影响展示，动作处理仍需在事务内重新校验能力、revision、幂等和权限。

GORM 使用自定义 zap logger：绑定参数通过 `ParamsFilter` 保持为占位符；普通 SQL 与未命中只在 Debug 展示，SQLSTATE `23` 完整性约束和超过 200ms 的慢查询记 Warn，其他 SQL 执行故障才记 Error。

上下文构建日志是业务级审计数据，记录模型、预算、段、消息 ID、世界书命中和挑战哈希，不等同于应用日志。

## 并发与事务原则

- 同一对话生成：conversation 行锁 + `conversation_generation_runs` 部分唯一索引；`is_processing` 仅为兼容性投影。
- Agent 工具：`attempt_id + call_id` 唯一，成功领域写入、invocation、tool 消息和 attempt 计数在同一事务。
- 状态读改写：状态行锁。
- 草稿保存与发布：版本行锁。
- 结算：对话行锁、奖励余额行锁、账本幂等键与唯一约束。
- 消息改写：奖励余额、分支纪元、回合/消息/记忆失效与新消息的单事务。
- 多表业务写入必须由仓储事务封装，不能在 handler 中分散提交。
- 制作与交易：Campaign 行锁内原子更新角色、材料、产物、局内货币、市场库存、事件和幂等记录。

## 后端回归清单

- `go test ./...` 与 `go vet ./...` 通过。
- 普通用户不能进入管理员路由。
- 所有对话子资源都有归属检查。
- 发布校验拒绝缺少核心分段、无效 Runtime、隐藏状态展示或无效奖励引用。
- Campaign 发布校验拒绝无效配方/市场结构和悬空材料、产物、器具、技艺、地点、货币、势力或商品引用。
- 已发布内容哈希在运行时重新验证。
- 判定工具只记录服务端计算的结果，拒绝调用不留下领域写入。
- 写工具提交后最终文本失败会进入 `partial`，不会回滚或重放已提交事实。
- 普通用户状态响应不包含 model/internal 字段及其隐藏子字段。
- 状态修订和角色询问快照也不包含 model/internal 字段，Inquiry 不直接读取可能含 model evidence 的 Memory 正文。
- 重复结算不会重复增加奖励余额或账本。
- 消息改写失败不会单独扣除奖励或留下上下文分叉。
- 空库启动会自动发布内置挑战，失败时阻止服务带着不可用内容继续启动。
- 已有任意挑战包时，server 启动不会读取、升级或覆盖内置挑战。
- 资源边界测试覆盖完整响应、SSE 长行、审计截断、请求 body、分页超限、慢 WebSocket 和 durable generation run 竞态。
## Share 服务

`internal/share` 负责输入规范化、公开字段投影、快照大小限制、HMAC token、公开读取和进程内限流；`storage.ShareRepository` 负责所有权、当前分支、已提交回合、连续范围和生命周期事务。创建使用客户端 UUID `request_id` 幂等，replace 在一个 Repeatable Read 事务中创建新分享并撤销旧分享。客户端只提交 `show_username`，实际公开名称和头像由服务端从已认证用户生成，防止客户端伪造他人身份；快照只接受受信任的 LinuxDo HTTPS 头像域名。未知展示消息类型默认拒绝，`roll_result.metadata` 只复制显式允许字段。

公共读取只查询单条不可变 `conversation_shares` 记录，使用 `snapshot_hash` 作为 ETag，不重新装配原对话。响应设置 `noindex`、`no-referrer` 和 `must-revalidate`，首版不启用代理共享缓存，以保证撤销和下架立即生效。公共读取和举报分别受进程内滑动窗口限制；多实例部署时每个实例独立计数，若以后需要全局配额再接入共享限流存储。

`GET /s/:token` 输出服务端 HTML 外壳，只在 Open Graph 中放经过转义的标题与最多 160 个字符的公开摘要，不写入消息全文；浏览器随后进入 `/shared/:token` 的 Vue 只读页。结构化访问日志会把 `/s/*` 和公共 API 中的 token 段整体替换为 `[REDACTED]`。

2026-09-18 在 Windows amd64、Intel i5-14600KF 上以 25 次串行迭代执行公开投影加 JSON 编码基准：50 条约 60 us/op，200 条约 238 us/op，500 条约 550 us/op。该数字只用于识别线性增长和回归，不代表生产吞吐量，也没有执行并发压测。
