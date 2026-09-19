# 10. API 与 WebSocket 契约

## 通用约定

- REST 前缀为 `/api`，WebSocket 前缀为 `/ws`。
- 请求和响应正文使用 JSON，OAuth 跳转除外。
- 会话通过 `fatecraft_session` HttpOnly Cookie 传递。
- 除健康检查、认证配置、登录和 OAuth 流程外，所有路由需要登录。
- `/api/admin/*` 还需要管理员角色。
- UUID 格式错误返回 400；资源不存在或不属于当前用户统一返回 404。

错误响应：

```json
{ "error": "可读错误信息" }
```

## Agent 控制消息

除 `user_input` 和 `cancel_generation` 外，WebSocket 客户端可以发送：

```json
{ "type": "steer_generation", "content": "在当前生成结束后优先处理这条指令" }
{ "type": "follow_up", "content": "当前回合完成后继续做这件事" }
{ "type": "clear_generation_queue" }
```

`steer_generation` 和 `follow_up` 都进入对话级有序队列，由服务端串行执行；它们不会重放当前已经开始的模型步骤。每个写操作都应携带客户端生成的 `request_id`，服务端按 request ID 幂等处理。每个对话最多保留 64 条待执行指令，超出上限返回错误。`cancel_generation` 取消当前任务并清理尚未执行的队列。服务端用 `agent.queue.updated` 广播等待数量；取消、排队和队列清理都不改变已提交工具事务。

## 健康检查

```text
GET /api/health
```

成功响应：`{"status":"ok"}`。空库内置挑战初始化发生在 HTTP 服务监听之前，因此成功响应表示启动初始化已经完成；该接口不验证外部 LLM 可用性。

## 认证

```text
GET  /api/auth/config
GET  /api/auth/linuxdo/start?return_to=/
GET  /api/auth/linuxdo/callback?code=...&state=...
POST /api/auth/email-code
POST /api/auth/register
POST /api/auth/login
POST /api/auth/logout
GET  /api/auth/me
```

`config` 返回邮箱登录、邮箱注册、邮箱验证码、邀请码要求、验证码重发间隔和 LinuxDo 是否可用，不返回 Client Secret 或 SMTP 信息。`email-code` 只在邮箱注册和验证码均启用时发信，并按邮箱执行重发冷却。`register` 根据当前服务端策略校验 `verification_code` 和 `invitation_code`；`register` 和 `login` 成功后设置会话 Cookie 并返回 `{ "user": ... }`。

LinuxDo start 创建 OAuth flow 后 302 跳转。callback 成功后创建本地会话并跳回已校验的 return path；失败跳转登录页并使用简化错误信息，完整上游错误只记服务端日志。

## 公开挑战投影

```text
GET /api/challenges
GET /api/challenges/:slug
```

虽然内容是“公开投影”，当前路由仍要求登录。响应只包含挑战公开说明、当前版本、允许 Persona 和奖励概要，不包含 Prompt、世界书、完整资产或奖励条件 JSON。

## 对话

```text
POST   /api/conversations
GET    /api/conversations?status=active,ended&search=关键词
GET    /api/conversations/:id
PATCH  /api/conversations/:id
DELETE /api/conversations/:id
POST   /api/conversations/:id/end
POST   /api/conversations/:id/archive
POST   /api/conversations/:id/restore
POST   /api/conversations/:id/fork
```

创建正文：

```json
{
  "challenge_slug": "reincarnation-trials",
  "persona_slug": "rogue-cultivator",
  "title": "可选标题"
}
```

PATCH 只识别 `title`。版本、哈希、资产 ID、状态和判定配置不是可写字段。

列表请求的 `limit` 必须是合法整数，超出范围直接返回 400：conversation 为 1–100，messages 为 1–500，Campaign events、state revisions、奖励流水和管理员日志为各自接口声明的 1–100/1–200。消息 `before` 是当前 conversation 内展示消息的复合游标；服务端按 `(created_at, id)` 稳定排序，anchor 不存在或不属于该对话时不会静默返回第一页。普通 JSON body 默认最多 4 MiB，挑战版本 PUT 单独最多 32 MiB。

campaign 模式不能直接调用 `/api/conversations`，必须先创建长期存档，再从固定存档创建唯一的持续对话。

状态操作：

- end：active/owned 对话变为 ended，不发奖励；若有生成 run，先记录取消请求并等待 Worker 退出。
- archive：只接受 active；与 end 一样先处理正在运行的生成。
- restore：只接受 archived；仍有 active run 时返回冲突。
- delete：软删除 owned 对话，并等待其生成 Worker 退出。
- fork：读取来源对话的玩法和 Persona，在该玩法当前版本创建新对话；来源消息和状态不复制，Campaign 对话不支持该接口。

## 长期存档

```text
POST /api/campaigns
GET  /api/campaigns?status=active
GET  /api/campaigns/:id
GET  /api/campaigns/:id/snapshot
GET  /api/campaigns/:id/events?after_sequence=0&limit=50
GET  /api/campaigns/:id/conversations
POST /api/campaigns/:id/conversations
POST /api/campaigns/:id/actions
POST /api/campaigns/:id/abandon
```

创建存档只接收挑战 slug、Persona slug 和可选标题；标题为空时使用玩法名称。后端固定发布版本、内容哈希和资产。`POST /api/campaigns/:id/conversations` 不接收独立标题，只创建一次并继承存档名，重复创建返回 409。领域动作必须提交 `action_id`、稳定 `key`、`expected_revision` 和严格参数对象。成功响应包含更新后的 Campaign、事件和公开结果；同一 `action_id` 重试返回 `replayed=true`，并发旧 revision 返回 409。

`GET /api/campaigns/:id` 返回存档头；`GET /api/campaigns/:id/snapshot` 返回玩家快照：基础角色与有效加成、功法层效果、库存和装备效果、已知配方、持有配方载体对应的可学习配方、当前位置市场与实际买卖价、当前位置祖先路径、已发现连接及可达性、公开 NPC 关系、势力状态、当前 region 聚合局势、任务与独立分组的承诺。完成态还包含平台保留的 `outcome_rewards`，列出本次实际带出、来源快照和当前账号余额。远端市场和区域、未来计划事件、无关未知配方、NPC 私密状态/目标、随机种子、未发现地点和 internal/model 事件不在响应中。

阶段 3 玩家动作 key 为 `craft.learn_recipe`、`craft.execute_recipe`、`trade.buy` 和 `trade.sell`。配方动作只接收 `recipe_key`；交易动作接收 `market_key`、`item_kind`、`item_key`、`quality_key` 和 `quantity`，出售独立装备时还必须提交 `item_instance_id` 且数量为 1。服务端计算材料选择、成败、品质、补货和价格，响应 `result` 仅用于展示，不能作为下一次请求的可信输入。

阶段 4 玩家动作 key 为 `quest.accept` 与 `quest.submit`，两者只接收 `quest_key`。接受时由后端确定截止游戏时间并创建计划事件；提交时重新读取权威领域表核验目标，客户端不能提交进度、完成状态或奖励。世界事件由所有领域动作统一的事务包装器在时间推进后处理，不提供允许客户端指定模板、变体或效果的 API。

事件接口只返回 `visibility=player`；model/internal 事件不会通过该 API 泄漏。放弃存档同样走 revision 和幂等事务，不是直接 PATCH 状态。

## 消息与生成

```text
GET    /api/conversations/:id/messages?before=...&limit=...
POST   /api/conversations/:id/messages
POST   /api/conversations/:id/regenerate
POST   /api/conversations/:id/continue
POST   /api/messages/:id/edit-and-regenerate
```

消息写操作必须解析消息所属对话，再校验当前用户和 active 状态。不能只按 message ID 更新。内部消息没有通用 PATCH/DELETE API；edit-and-regenerate 必须经过 Runtime capability、奖励消费、分支和不可回滚副作用检查。

`roll_result` 的 `metadata` 可包含版本化判定详情：`details_version`、`roll_key`、`roll_title`、`roll_category`、`roll_outcome` 和 `conditions`。`roll_outcome` 至少包含 `rolled`、`sides`、`success_rate`、`success`、`level`、`breakdown` 与服务端计算的 `success_range`；`conditions` 只返回公开条件投影，客户端必须忽略未知版本和非公开条件。该详情随现有消息 REST 响应及 `message_created` 一起传输，不新增接口或数据库列；旧客户端继续只渲染 `markdown`。

regenerate、continue 和 edit-and-regenerate 通过后台生成任务执行，最终消息与状态通过 WebSocket 广播。REST `202 Accepted` 只表示 durable run 已接收；前端不得据此乐观删除、截断或认定分支替换成功，必须等待操作终态或重新读取快照。

## 状态

```text
GET   /api/conversations/:id/state
GET   /api/conversations/:id/runtime
PATCH /api/conversations/:id/state
GET   /api/conversations/:id/state/revisions
```

state GET 和 revisions 对对话所有者开放；state 只返回 Runtime State Schema 中 `player` 可见的投影，不返回 `model`、`internal` 或隐藏子字段。runtime 返回当前固定挑战版本编译后的公开 Presentation（包括玩法包提供的 `value_labels`、可选 `campaign_view` 和 `guidance`）、玩法 capabilities、平台级 `platform_capabilities` 和公开 Outcome，不返回完整 Schema、内部规则或可信配置。普通挑战的 guidance 已按同一份 player state 过滤并标记 `enabled`；Campaign guidance 仍以公开 Snapshot 为上下文。稳定 key 保持用于动作、规则和事件，客户端只能通过 Presentation 转成玩家文案。`campaign_view`/`guidance` 只描述公开渲染和参数绑定，不能扩大后端权限；平台能力的授权和成本不由玩法 Runtime 决定。

PATCH 同时要求管理员，支持补丁或整份状态维护，写入来源记为 system。普通用户的游戏状态只能由后端执行当前步骤开放的可信状态工具时更新。

## 上下文

```text
GET /api/conversations/:id/context/last
GET /api/context/:cid
GET /api/conversations/:id/activity
```

第二个路由中的 `cid` 是上下文构建日志 ID，不是对话 ID。Handler 先读取日志，再反查其 conversation 并做归属检查。两个上下文路由都只返回玩家可理解的构建摘要：模型、Token 预算与占用、纳入历史/世界书数量和 Memory lag；不返回 Prompt 分段、消息 ID、世界书内部 ID、裁剪原因、会话令牌或 LLM API Key。

新对话尚未完成首次上下文构建时，`context/last` 返回 `204 No Content`，这是正常空状态而不是资源错误。对话不存在或不属于当前玩家时仍返回 404。

activity 返回 `narrative` 和 `memory` 两类当前状态，并投影当前有效回合的 `turn_status`、`can_continue`、`can_regenerate`。叙事状态为 `idle/preparing/model/tool`，并可附带 `step_no` 和题材无关的 `tool_category=state/roll/settlement/action`；Memory 状态为 `idle/queued/running`。它用于刷新和 WebSocket 重连后的恢复；按钮能力以服务端投影为准，不能由最后一条消息类型推断。

主叙事流式正文通过 `agent.text.delta` 实时推送。该正文在模型步骤通道确认前只是临时预览，不代表已写入可信历史；如果同一步随后出现工具调用，服务端发送 `agent.text.reset`（携带临时 `message_id` 和 `reason=tool_call`）清空预览，再进入工具流程。`agent.text.reset` 不清除 reasoning 或生成状态。最终正文以 `message_created` 的持久化消息为准；取消、失败、partial、断线重连时临时正文不恢复。

## 角色只读问答

```text
GET    /api/conversations/:id/inquiries
POST   /api/conversations/:id/inquiries   {"question":"..."}
POST   /api/conversations/:id/inquiries/cancel
DELETE /api/conversations/:id/inquiries
```

接口都只允许 conversation 所有者。问答可读取 ended/archived 对话的已提交玩家资料，不获取主生成锁、不执行玩法动作、不写主消息或长期记忆；cancel 只取消当前实例正在执行的侧栏模型请求，DELETE 只清空专用问答历史且会拒绝与执行中请求竞态。回答返回 snapshot ID、branch epoch、revision 和 `stale`，引用的来源只包含公开标签。长期记忆没有普通玩家生成、编辑、查询或重试 API。

## 玩家奖励

```text
GET /api/me/rewards
GET /api/me/reward-ledger?limit=50
```

rewards 返回当前用户持有的奖励定义与余额；ledger 返回关联奖励定义的只读流水，`limit` 由后端限制在 1–200。接口不接受客户端提交用户 ID，余额不能直接修改。玩法能力消费和挑战结算由领域事务写入账本。

## LLM 设置

```text
GET   /api/settings/llm
GET   /api/settings/llm/providers
PATCH /api/settings/llm
POST  /api/settings/fetch-models
POST  /api/settings/llm/capabilities/check
```

设置由管理员接入模式控制。`GET /api/settings/llm` 返回当前有效配置、`access_mode`、`source` 和 `editable`；`user_managed` 时来自当前用户行，`platform_managed` 时来自 `system_settings` 公共配置。GET 不返回 API Key，只返回 `has_api_key`。平台模式下用户 PATCH 和 `/api/settings/fetch-models` 返回 403，能力检测仍使用当前有效公共配置。管理员通过 `GET/PATCH /api/admin/system-settings` 维护模式和公共配置，Key 只写入不回显，并通过 `POST /api/admin/system-settings/fetch-models` 获取公共模型列表。

check 是用户显式触发的同步、无副作用诊断，最长使用独立的 3 分钟请求超时。请求体只能是 `{"role":"narrative"}`、`{"role":"memory"}` 或 `{"role":"character_inquiry"}`；接口不接受 URL、密钥或模型参数，而是读取当前已保存设置。

成功响应为 `{"settings_revision":n,"result":{...}}`。Narrative 以生产请求相同的默认工具选择语义检测真实的 Tool Call -> Tool Result -> 普通文本闭环；Memory 与 Character Inquiry 检测各自模型的原生 JSON Schema。`result.status` 为 `supported/unsupported/inconclusive/unavailable`：只有上游明确拒绝目标能力才是 `unsupported`，单次模型行为或无法归因的参数错误是 `inconclusive`。结论属于已保存的 Base URL、协议与模型组合，只代表本次诊断，不持久化、不自动重试，也不改变生产请求。保存设置不会自动探测，前端不得用未保存输入静默检测旧设置。

## 管理员设置与挑战

```text
GET   /api/admin/system-settings
PATCH /api/admin/system-settings

GET   /api/admin/llm-request-logs?limit=50&conversation_id=...
GET   /api/admin/llm-request-logs/:id
GET   /api/admin/context-build-logs?limit=50&conversation_id=...
GET   /api/admin/context-build-logs/:id

GET   /api/admin/auth-settings
PATCH /api/admin/auth-settings
POST  /api/admin/auth-settings/test-email

GET   /api/admin/invitation-codes
POST  /api/admin/invitation-codes
PATCH /api/admin/invitation-codes/:id

GET  /api/admin/builtin-challenges
POST /api/admin/builtin-challenges/:slug/import

GET   /api/admin/gameplay-modules
GET   /api/admin/reward-definitions
POST  /api/admin/reward-definitions
PATCH /api/admin/reward-definitions/:id

GET  /api/admin/challenges
POST /api/admin/challenges
POST /api/admin/challenges/:id/versions
GET  /api/admin/challenge-versions/:id
PUT  /api/admin/challenge-versions/:id
POST /api/admin/challenge-versions/:id/publish
```

认证设置响应不包含 SMTP 密码，只包含 `has_smtp_password`。PATCH 中 `smtp_password` 为空表示保留原值，`clear_smtp_password=true` 表示清除。邀请码创建响应是唯一返回完整 `code` 的时机，后续列表只返回尾号、状态、用量和有效期。

`PATCH /api/admin/system-settings` 维护判定配置和默认关闭的 `detailed_llm_logging_enabled`。开启后记录每个主叙事模型步骤、Memory、Inquiry 和显式诊断的完整模型请求与原始响应，并用 purpose、attempt、Memory job/version、Inquiry snapshot branch/revision 关联，但永不记录 API Key、Authorization 头或 URL 查询参数。日志列表只返回诊断摘要，单条详情接口才返回请求与响应正文；两者都仅限管理员。

上下文构建日志不受详细模型日志开关影响：管理员列表返回最近构建的预算摘要，单条详情返回 Prompt 分段、必选/裁剪结果、历史消息 ID、世界书 entry ID 和 Memory lag。普通玩家上下文接口不会返回这些技术明细。

内置内容列表只返回仓库包的公开元数据、版本、内容哈希、数据库当前版本和 `available/imported/update_available` 状态，不返回 Prompt 或世界书正文。导入接口只接受扫描结果中的 slug，以及可选请求体 `{"overwrite": true}`，不接受文件路径或上传内容。普通导入不会覆盖或降级；已使用版本应提升版本号并发布新快照。新版本发布事务会将旧版本标记为 retired，并把该版本下普通 active 对话标记为 archived；旧对话仍可通过只读接口浏览。`POST /conversations/:id/fork` 会按来源对话的玩法和 Persona，在当前版本创建全新的初始对话，不复制任意旧状态；Campaign 对话必须走独立的存档流程。

gameplay-modules 返回服务端已经安装的 engine、动作、verifier、calculator 与 settlement handler，仅用于选择可信能力。奖励定义接口维护全局稳定奖励；已被版本、余额或账本引用后，不允许破坏性改变 kind、stackable 或 consumable 语义。

挑战版本 PUT 接受：

```json
{
  "manifest": {},
  "runtime": {},
  "assets": [],
  "definitions": [],
  "outcomes": [],
  "reward_rules": []
}
```

保存允许内容暂时不满足发布条件，但必须是合法 JSON 和基本合法资产集合。publish 执行完整业务校验并返回发布后的完整版本文档。

不存在 Character、Prompt、WorldBook 或旧预设包自由 CRUD 路由。

## WebSocket 连接

```text
GET /ws/conversations/:id
```

升级前经过 `requireAuth`，handler 还会校验对话归属。浏览器 Origin 必须位于 `ALLOWED_ORIGINS`；空白配置只用于开发。

一个连接只绑定一个对话，同一对话可以有多个观察连接。Hub 对同一对话维护一个当前生成任务；普通新请求不会静默取消旧任务，而是返回冲突，只有显式取消或生命周期变更才会请求取消并等待旧任务释放 durable generation run。队列中的 follow-up/steer 由同一 Worker 串行消费。

## 客户端消息

发送用户输入（`request_id` 用于重试幂等）：

```json
{ "type": "user_input", "request_id": "客户端生成的 UUID", "content": "我选择闭关筑基" }
```

取消生成：

```json
{ "type": "cancel_generation" }
```

空白用户输入被拒绝并广播 error。未知消息类型当前忽略。

## 服务端事件

| type | 关键字段 | 含义 |
| --- | --- | --- |
| `agent.turn.started` | 公共元数据；payload `message_id` | 主叙事 attempt 开始 |
| `agent.step.started` | 公共元数据；payload `tool_count` | 一个模型步骤开始 |
| `agent.tool.started` | 公共元数据；payload `name`, `category` | 模型已选择一个工具，服务端开始处理 |
| `agent.tool.finished` | 公共元数据；payload `name`, `status`, `code` | 工具已 committed/rejected/failed，公开结果已进入工具历史 |
| `agent.reasoning.delta` | 公共元数据；payload `message_id`, `delta` | 模型思考内容新增一段；前端默认展开，正文开始后收起 |
| `agent.text.delta` | 公共元数据；payload `message_id`, `delta` | 最终文本通道新增可见叙事 |
| `agent.turn.completed` | 公共元数据；payload `message_id` | 最终文本已持久化，attempt 完成 |
| `agent.turn.partial` | 公共元数据；payload `message_id`, `code`, `message` | 至少一个写工具已提交，但最终叙事未完成 |
| `agent.turn.failed` | 公共元数据；payload `message_id`, `code`, `message` | 没有可信副作用，本次 attempt 失败 |
| `agent.turn.cancelled` | 公共元数据；payload `message_id`, `code`, `message` | 没有可信副作用且本次 attempt 被取消 |
| `message_created` | `message` | 服务端已经持久化消息 |
| `message_updated` | `message` | 已有展示消息更新 |
| `branch.replaced` | `operation_id`, `attempt_id`, payload `invalidated_message_ids`, 可选 `message` | 原子分支替换已提交，前端据此更新时间线 |
| `generation.replayed` | `operation_id` | request ID 已对应已有操作，本次没有再次调用模型 |
| `state_update` | `state` | 当前状态的新完整值 |
| `campaign_updated` | `campaign`, `domain_events` | 长期存档领域动作已提交；客户端应重新读取权威快照 |
| `context_built` | `context_build_id` | 上下文日志可读取 |
| `activity_updated` | `activity`, `status` | 后台活动变化；当前仅 Memory 使用 `queued/running/idle` |
| `conversation_ended` | conversation ID | 手动结束或状态变更 |
| `challenge_completed` | `run_id`, `outcome`, `reward_grants`, `metrics`, `next_actions` | 通用挑战结算成功 |
| `error` | `error` | 生成、权限或持久化错误 |

每个 `agent.*` 事件都带 `event_id`、`conversation_id`、`operation_id`、`turn_id`、`attempt_id`、`step_no`、attempt 内递增的 `sequence`、`occurred_at` 和按类型变化的 `payload`。`message_created`、`message_updated`、`branch.replaced`、`state_update`、`campaign_updated`、`context_built` 和 `challenge_completed` 若由生成操作产生，也带同一 `operation_id/attempt_id`。客户端按 event ID 去重并拒绝旧 operation/attempt；检测到间隙时重新读取消息、状态、Campaign、activity 和最近上下文。

临时文本只有在 `message_created` 与 `agent.turn.completed` 后成为持久事实。失败、取消或截断时前端清除尚未持久化的增量；若收到 `agent.turn.partial`，已提交工具产生的状态和领域事实必须保留，并提示用户使用“继续叙事”收尾。

## 连接存活与丢事件

服务端每 30 秒发送 ping，60 秒未收到任何帧或 pong 则关闭连接。每个连接维护容量有限的出站队列（当前为 256），但队列不是可靠事件存储：

- 相邻、同一对话/回合/attempt/step/消息且同一类型的 `agent.reasoning.delta` 和 `agent.text.delta` 会在连接级合并，约每 40ms 或累计 4KB flush 一次。合并不跨越 reasoning/正文切换、`text.reset`、工具事件、step 切换或其他控制事件；合并后的事件仍保留原类型和公共元数据，使用新的 `event_id`，`sequence` 取所覆盖增量的最后一个序号，范围跨越多个源事件时在 payload 附带 `sequence_start`。客户端按该范围判断 sequence 缺口，不会把正常合并误判为丢事件。
- 增量是临时预览。出站队列短暂拥塞时，尚未 flush 的增量可以被丢弃，最终正文仍以 `message_created` 和 REST 快照为准。
- `agent.*` 控制/终态事件、`message_created`、`message_updated`、`state_update`、`campaign_updated`、`context_built`、`activity_updated`、`conversation_ended`、`challenge_completed` 和 `error` 不再走静默丢弃分支。若有限队列已满，服务端记录告警并立即关闭该慢连接，让客户端自动重连并同步快照；模型回调不会为慢连接阻塞等待。
- 广播只在 Hub 锁内复制连接快照，实际合并和入队在锁外逐连接执行；一个慢连接不会阻塞其他对话或连接。连接注销、写协程退出和入队通过连接级生命周期锁协调，不会向已关闭 channel 写入。

因此 WebSocket 仍不是可靠事件存储。客户端重连、刷新或发现 attempt 内 `sequence` 缺口时，应通过 REST 重新加载对话、消息、状态、Runtime、Campaign 事件、奖励、上下文摘要和当前活动；断线期间丢失的临时 delta 不恢复。

## CORS 与 Cookie

显式配置 `ALLOWED_ORIGINS` 时，HTTP CORS 允许凭证并支持 GET、POST、PUT、PATCH、DELETE、OPTIONS。未配置时使用通配来源但不允许跨域凭证，只适合本地同源代理开发。

生产推荐由同一域名反代前后端，避免跨站 Cookie 的 SameSite 限制。
### 回合运行面板

`GET /api/conversations/:id/runtime-inspector` 返回玩家可见的本回合运行投影，聚合当前活动、最近上下文请求、性能、记忆状态、安全诊断和进程级只读内容缓存统计。`cache` 包含驻留条目与上限、估算字节与上限、命中/未命中、加载/失败、冷加载合并、淘汰及累计加载/编译耗时；不包含缓存键或正文。该接口不暴露 Prompt、世界书、密钥、原始响应或隐藏工具参数；前端可在重连/刷新时用快照恢复，实时状态仍由现有 WebSocket 活动事件驱动。

## 对话分享 API

所有者接口需要登录且只操作自己的数据：

```text
POST   /api/conversations/:id/shares
GET    /api/conversation-shares
DELETE /api/conversation-shares/:id
POST   /api/conversation-shares/:id/replace
```

列表接口接受 `status=active|expired|revoked|moderated`、`search`、`page` 和 `page_size`（最大 100），只返回当前用户的数据，并附带总数和来源对话标题。创建与 replace 接受 `scope=all|range`、可选的起止消息 ID、标题、`show_username`、`expires_in_days=0|7|30` 和客户端 UUID `request_id`。`show_username=false` 时署名固定为“匿名旅人”且不公开头像；为 true 时由后端读取当前登录账号的 `display_name` 和头像，不接受客户端自定义身份字段。range 的端点必须属于同一对话当前有效分支且顺序正确，服务端自动包含中间全部消息。

匿名接口不读取 Cookie，也不创建 WebSocket：

```text
GET  /s/:token
GET  /api/public/conversation-shares/:token
POST /api/public/conversation-shares/:token/reports
```

JSON 读取返回公开快照和 `ETag`，匹配 `If-None-Match` 时返回 304。非匿名快照包含冻结的受信任 LinuxDo 头像地址，前端直接加载该地址。无效、过期、撤销和下架 token 对匿名方统一返回 404。HTML 入口只输出安全社交摘要，再进入 SPA 阅读页。

管理员接口：

```text
GET  /api/admin/conversation-shares
POST /api/admin/conversation-shares/:id/moderate
GET  /api/admin/share-reports
POST /api/admin/share-reports/:id/resolve
```

管理员列表返回治理摘要而非未发布私有内容；下架与举报处理写入 `conversation_share_audit_events`。
