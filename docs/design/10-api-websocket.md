# 10. API 与 WebSocket 契约

## 统一玩法行动

仅当前用户可以读取和执行所属对话的纯模块行动：

| 接口 | 输入/输出 |
| --- | --- |
| `GET /api/conversations/:id/actions` | `branch_epoch`、`revision`、公开 `gameplay`、`action_capabilities`；不支持纯行动或当前忙碌/已结束时返回空目录 |
| `POST /api/conversations/:id/actions` | `{request_id, branch_epoch, expected_revision, key, arguments}`；成功返回新 revision、公开 result 及按模式提供的 events/campaign/settlement |

请求 ID 是 UUID，参数与整个行动输入受 8 KiB 限制。服务端复核权限、分支、实时状态和规则；冲突/规则拒绝为 409，`code` 提供 `state.conflict` 或模块原因。同请求重试读取已提交结果，不生成第二次效果。

成功提交广播 `module_action_committed`，客户端重新读取真实状态；Challenge 和 Campaign 同次终局均广播 `challenge_completed.completion`。Campaign 的收尾以领域终态为准，模型不能再写后续行动。模块 DTO、菜单方案与可见性见[24](24-gameplay-module-architecture.md)。

Challenge 面板动作提交成功且未触发结局时，服务端自动追加一个 `module_action` 叙事回合（不产生用户消息），通过常规 `message_created` / agent 流式事件推送动作结果叙事；对话忙或模型路由不可用时静默跳过，结果由下一次叙事回合注入补齐，见[13](13-challenge-system.md)。行动方案可携带 `blocked_reason`（资源不足缺口说明），带该字段的方案仅用于展示，执行会被服务端支付复核以 409 拒绝。

Campaign 玩家动作结果不明时，客户端按存档保存原 `action_id`、参数与 revision，重试同一请求；切换/重新打开对话后，迟到响应不得覆盖新作用域。动作已提交但快照刷新失败时保留成功事实并提示刷新，不重新执行。玩家动作成功和 `agent.turn.completed` 后刷新账号余额，涵盖阶段到账与终局所得。青河通过 `cultivation.retreat` 闭关、`character.recover` 恢复，阶段奖励随合法动作自动核验和发放；`cultivation.begin_ascension` 引劫与终局任务仍须玩家行动，具体参数取动态目录，见 [16](16-cultivation-campaign-agent-reference.md) 与 [21](21-campaign-outcome-rewards.md)。

## 通用约定

- REST 前缀为 `/api`，WebSocket 前缀为 `/ws`。
- 请求和响应正文使用 JSON，OAuth 跳转除外。
- 会话通过 `fatecraft_session` HttpOnly Cookie 传递。
- 除健康检查、认证配置、首次安装初始化、登录和 OAuth 流程外，所有路由需要登录。
- `/api/admin/*` 还需要管理员角色；角色、认证策略和平台密钥等敏感写操作还需要 `owner`。
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

首次安装接口：

```text
GET  /api/setup/status
POST /api/setup/initialize
```

空库的 `setup/status` 返回 `{"initialized":false}`。首次安装页面直接提交 `username`、`password`、可选 `email` 和 `confirm=true` 到 `setup/initialize`；服务端只接受完全空库，并在一个事务中创建唯一 `owner`、初始奖励、`installation_completed_at` 和审计记录，同时签发会话。接口只能成功一次，不存在把已登录账号提升为 owner 的入口。安装完成前邮箱和 LinuxDo 登录均关闭，后续注册用户默认为普通用户。

```text
GET  /api/auth/config
GET  /api/auth/linuxdo/start?return_to=/
GET  /api/auth/linuxdo/callback?code=...&state=...
POST /api/auth/email-code
POST /api/auth/register
POST /api/auth/login
POST /api/auth/logout
GET  /api/auth/me
GET  /api/auth/identities
GET  /api/auth/linuxdo/bind/start?return_to=/settings
DELETE /api/auth/identities/:id
GET  /api/admin/auth-identities
```

`config` 返回邮箱登录、邮箱注册、邮箱验证码、邀请码要求、验证码重发间隔和 LinuxDo 是否可用，不返回 Client Secret 或 SMTP 信息。`email-code` 只在邮箱注册和验证码均启用时发信，并按邮箱执行重发冷却。`register` 请求体为 `email`、`username`、`password`、可选 `display_name`、`verification_code` 和 `invitation_code`；用户名 2-32 个字符，不能包含 `@`、空白或控制字符，按大小写不敏感唯一，冲突返回 409“用户名已被使用”；昵称留空时使用用户名。`register` 和 `login` 成功后设置会话 Cookie 并返回 `{ "user": ... }`。登录请求体为 `identifier` 和 `password`：含 `@` 的标识只按邮箱查找，否则按用户名（大小写不敏感）查找。第三方身份列表只返回脱敏后的绑定摘要；管理员审计接口同样不返回 profile 原文。

LinuxDo 登录和绑定共用 OAuth 2.0 + PKCE，但 flow 会区分 `login` / `bind` purpose。发起授权时服务端下发只在 `/api/auth/linuxdo` 路径有效的短期 HttpOnly nonce Cookie，flow 只保存其 SHA-256；回调必须带回同一 nonce，否则拒绝，防止授权结果被其他浏览器使用（登录 CSRF）。`return_to` 只接受以单个 `/` 开头、不含反斜杠、空白和控制字符的站内路径，否则回落到 `/`。登录只允许已经绑定到站内用户的 subject，未绑定 subject 不会自动创建用户，失败时回到 `/login?error=...` 并说明需要先在设置中绑定；绑定必须由当前已登录用户发起，回调时的会话用户也必须是发起者，成功或失败都回到发起页并附带 `linuxdo_bind=ok` 或 `linuxdo_bind_error=原因`。回调只新增 `auth_identities` 记录，不替换本地用户名、密码或 owner 角色。解除绑定必须保留至少一种可用登录方式；站点关闭密码登录时，即使账号有密码也不能解除最后一个第三方身份。owner 关闭密码登录前必须已配置 LinuxDo 且自己已绑定 LinuxDo。

管理员用户、第三方身份和审计日志列表支持 `limit`（1-200，默认 50）；用户和第三方身份列表另支持 `offset`（0-100000）。超出范围返回 400。

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

创建存档只接收挑战 slug、Persona slug 和可选标题；标题为空时使用玩法名称。后端固定发布版本、内容哈希和资产。`POST /api/campaigns/:id/conversations` 不接收独立标题，只创建一次并继承存档名，重复创建返回 409。领域动作必须提交 `action_id`、稳定 `key`、`expected_revision` 和严格参数对象。成功响应包含更新后的 Campaign（含 `state_hash`）、事件、公开结果和 `narrative` ActionResult；同一 `action_id` 重试返回 `replayed=true`，并发旧 revision 返回 409。`narrative` 只用于展示和诊断，不能作为下一次请求的可信输入。

`GET /api/campaigns/:id` 返回存档头；`GET /api/campaigns/:id/snapshot` 返回玩家快照：基础角色与有效加成、功法层效果、库存和装备效果、已知配方、持有配方载体对应的可学习配方、当前位置市场与实际买卖价、当前位置祖先路径、已发现连接及可达性、公开 NPC 关系、势力状态、当前 region 聚合局势、任务与独立分组的承诺。`campaign` 头同时返回 `revision` 与 `state_hash`，客户端必须用它们判断消息归属。所有快照均包含平台保留的 `reward_points`（实际终局积分）、`outcome_rewards`、`stage_rewards`、`stage_reward_points`（实际阶段合计）和 `total_reward_points`（两者之和）。`stage_rewards` 保存阶段 key、名称、配置/实际积分、停发原因与领取时间。改写后的活动存档仍保留已经领取的账号事实；直连实际积分为 0。局内阶段可用性由 `progression` 提供，不能用历史到账代替当前分支前置。远端市场和区域、未来计划事件、无关未知配方、NPC 私密状态/目标、随机种子、未发现地点和 internal/model 事件不在响应中。

制作交易玩家动作 key 包括 `craft.learn_recipe`、`craft.execute_recipe`、`trade.buy` 和 `trade.sell`。配方动作只接收 `recipe_key`；交易动作接收 `market_key`、`item_kind`、`item_key`、`quality_key` 和 `quantity`，出售独立装备时还必须提交 `item_instance_id` 且数量为 1。服务端计算材料选择、成败、品质、补货和价格，响应 `result` 仅用于展示，不能作为下一次请求的可信输入。

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

消息写操作必须解析消息所属对话，再校验当前用户和 active 状态。不能只按 message ID 更新。内部消息没有通用 PATCH/DELETE API；edit-and-regenerate 必须经过 Runtime capability、奖励消费、分支与完整检查点检查。

`roll_result` 的 `metadata` 可包含版本化判定详情：`details_version`、`roll_key`、`roll_title`、`roll_category`、`roll_outcome` 和 `conditions`。`roll_outcome` 至少包含 `rolled`、`sides`、`success_rate`、`success`、`level`、`breakdown` 与服务端计算的 `success_range`；`conditions` 只返回公开条件投影，客户端必须忽略未知版本和非公开条件。该详情随现有消息 REST 响应及 `message_created` 一起传输，不新增接口或数据库列；旧客户端继续只渲染 `markdown`。

Campaign 助手消息额外返回 `campaign_revision`、`campaign_state_hash`、`validation_status` 和 `validation_detail`。`message_created` / `message_updated` 的接收方必须先与当前 Campaign 快照比较：同 revision/hash 才能显示；消息版本落后时丢弃，版本领先时缓冲并拉取快照。`display_message_campaign_actions` 是服务端内部追溯关系，不单独暴露为公开接口；普通消息这些字段为空或 `not_applicable`。

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

activity 返回 `narrative` 和 `memory` 两类当前状态，并投影当前有效回合的 `turn_status`、`can_continue`、`can_regenerate`。叙事状态为 `idle/preparing/model/tool/streaming`，并可附带 `step_no` 和题材无关的 `tool_category=state/roll/settlement/action`；Memory 状态为 `idle/queued/running`。它用于刷新和 WebSocket 重连后的恢复；按钮能力以服务端投影为准，不能由最后一条消息类型推断。

主叙事流式正文通过 `agent.text.delta` 实时推送。该正文在模型步骤通道确认前只是临时预览，不代表已写入可信历史；如果同一步随后出现工具调用，服务端发送 `agent.text.reset`（携带临时 `message_id` 和 `reason=tool_call`）清空预览，再进入工具流程。`agent.text.reset` 不清除 reasoning 或生成状态。最终正文以 `message_created` 的持久化消息为准；取消、失败、partial、断线重连时临时正文不恢复。

## 角色只读问答

```text
GET    /api/conversations/:id/inquiries
POST   /api/conversations/:id/inquiries   {"question":"..."}
POST   /api/conversations/:id/inquiries/cancel
DELETE /api/conversations/:id/inquiries
```

接口都只允许 conversation 所有者。问答可读取 ended/archived 对话的已提交玩家资料，不获取主生成锁、不执行玩法动作、不写主消息或长期记忆；cancel 只取消当前实例正在执行的侧栏模型请求，DELETE 只清空专用问答历史且会拒绝与执行中请求竞态。回答返回 snapshot ID、branch epoch、revision 和 `stale`，引用的来源只包含公开标签。长期记忆没有普通玩家生成、编辑、查询或重试 API。

## 玩家积分与兑换

~~~text
GET  /api/me/rewards
GET  /api/me/reward-ledger?limit=30&cursor=...
GET  /api/me/reward-exchange/offers
POST /api/me/reward-exchange/orders
GET  /api/me/reward-exchange/orders?limit=30&cursor=...
GET  /api/me/reward-exchange/by-request/:request_id
GET  /api/me/reward-exchange/tickets
POST /api/me/reward-exchange/orders/:id/ticket
POST /api/me/points-cdk/redeem
~~~

本人身份来自认证，不能用 user_id 选择他人。余额返回 {rewards}，固定两项 {asset:{key,name,unit,icon},balance}；收支返回 {entries,next_cursor}，商品返回 {offers}，订单返回 {orders,next_cursor}。游标按 (created_at,id) 倒序，limit 为 1–200。玩家商品额外返回当前 `max_redemptions_per_user`、本人累计 `redeemed_units` 和 `remaining_units`；不限时 `remaining_units` 为 null，有限时为 `max(0, max_redemptions_per_user - redeemed_units)`。管理端商品列表只返回商品配置，不附带任何玩家的额度统计。

收支支持 reward_key、source_type、created_from/created_to（RFC3339）筛选。玩家响应不含内部原因、操作者、管理请求 ID/指纹或 metadata。余额缺行返回 0。

兑换请求为 {request_id,offer_id,revision,units}，全部数量为正安全整数。revision 对应页面确认价格，服务端复核价格、状态和每人剩余额度，扣积分、加改写次数、两条账本与订单同事务。超额整笔拒绝并返回 409 `exchange_limit_reached`，库存不足返回 409 `exchange_stock_insufficient`，不拆单、不部分成交。商品附带 `stock_total/stock_remaining`；CDK 商品订单快照含 `codes` 与 `tutorial`。库存、工单与积分 CDK 语义见 [22](22-reward-management-and-exchange.md)。响应保存成交快照及 cost_ledger_id、reward_ledger_id。相同请求重试返回原结果（即使之后达到上限或商品下架），换参数返回 409；响应丢失应查询或原样重试。

## 奖励管理接口

以下需要 admin/owner，管理写事务再次校验权限：

~~~text
GET  /api/admin/rewards/rules?limit=30&offset=0
GET  /api/admin/rewards/audits?limit=30&cursor=...
GET  /api/admin/users/:id/rewards
GET  /api/admin/users/:id/reward-ledger
POST /api/admin/users/:id/reward-grants
GET  /api/admin/reward-grants/by-request/:request_id
GET  /api/admin/reward-ledger?user_id=...&reward_key=...&source_type=...&created_from=...&created_to=...&cursor=...
GET  /api/admin/reward-ledger/:id
POST /api/admin/reward-ledger/:id/reverse
GET  /api/admin/registration-reward-rule
PUT  /api/admin/registration-reward-rule
GET  /api/admin/reward-exchange/offers
POST /api/admin/reward-exchange/offers
PUT  /api/admin/reward-exchange/offers/:id
GET  /api/admin/reward-exchange/offers/:id/revisions
GET  /api/admin/reward-exchange/orders?user_id=...&cursor=...
GET  /api/admin/reward-exchange/orders/:id
GET  /api/admin/reward-exchange/offers/:id/stock?status=&code=&offset=
POST /api/admin/reward-exchange/offers/:id/stock
POST /api/admin/reward-exchange/offers/:id/stock/remove
GET  /api/admin/reward-exchange/tickets?status=
POST /api/admin/reward-exchange/tickets/:id/resolve
PUT  /api/admin/reward-exchange/tickets-enabled
GET  /api/admin/points-cdk/batches
POST /api/admin/points-cdk/batches
GET  /api/admin/points-cdk/batches/:id/codes
~~~

用户余额返回 {user,rewards}；规则返回 {rules,has_more}，附玩法、版本、当前标记、结局和 reward_points。审计返回 {audits,next_cursor}。管理账本扩展 user_id、operator_id、request_id、reason，并通过 reversal_id 标记原发放是否已撤回。

管理员直接发放请求：

~~~json
{"request_id":"客户端生成的 UUID","reward_key":"fatecraft.points","quantity":100,"reason":"内部运营原因","public_note":"感谢参与"}
~~~

reward_key 只允许积分和改写次数；数量为 1–9007199254740991 整数，原因必填，公开说明可选。目标来自路径、操作者来自认证。成功返回管理账本视图。相同请求换参数报冲突，原请求查询也可恢复撤回结果。

撤回请求为 {request_id,reason}，只支持原管理员发放整笔扣回，余额不足或已撤回拒绝，返回反向账本。没有通用领取、资产 CRUD、任意余额调整或兑换退款接口。

注册 PUT 使用 {id,points,rewrite_credits}，id 为当前配置修订，两项允许 0；成功返回新修订，旧 id 返回冲突。商品 POST/PUT 使用 {name,description,cost_quantity,reward_quantity,max_redemptions_per_user,enabled,sort_order,revision}；cost_quantity 固定为积分单价，reward_quantity 固定为改写次数，max_redemptions_per_user 为 0（不限）或 1–9007199254740991，缺省按 0 处理。创建 revision=1，编辑需匹配当前 revision；修订历史返回 {revisions}，其 snapshot 包含限额，系统初始化 actor_id 为空。

库存列表返回 {stock,has_more}，每行含 id、offer_id、code、status、order_id、user_id、username、redeemed_at、created_at、updated_at。已兑换或作废行带订单号与兑换时间，管理员可用 `GET /api/admin/reward-exchange/orders/:id` 追溯订单详情。

奖励写接口拒绝未知字段，旧资产 ID、purpose、delivery_mode 等不能被静默忽略。

业务错误响应为 {error,code}：

| code | HTTP | 含义 |
| --- | --- | --- |
| not_found | 404 | 目标或本人可访问的记录不存在 |
| admin_required | 403 | 缺少管理权限 |
| request_conflict | 409 | 同一请求 ID 内容不一致 |
| reward_state_conflict | 409 | 状态不允许操作 |
| user_disabled | 400 | 新请求目标账号不可用 |
| invalid_reward_key | 400 | 非固定的余额项目 |
| invalid_quantity | 400 | 数量不合法或超过安全范围 |
| insufficient_balance | 400 | 余额不足 |
| reversal_unsupported | 400 | 不支持撤回或单边返还 |
| exchange_changed | 409 | 商品修订已变，须刷新确认 |
| exchange_owned | 409 | 已拥有非叠加目标，未扣分 |
| exchange_unavailable | 400 | 商品下架或兑换配置无效 |
| exchange_limit_reached | 409 | 本次请求份数超过该商品每人剩余额度 |

无新奖励 WebSocket 事件；管理和兑换操作通过 REST 响应及重新读取余额/记录确认。完整语义与当前验证范围见 [22-reward-management-and-exchange.md](22-reward-management-and-exchange.md)。

## LLM 设置

```text
GET   /api/settings/llm
PATCH /api/settings/llm
POST  /api/settings/fetch-models
POST  /api/settings/llm/capabilities/check
```

新建对话、基于新版本创建对话、新建 Campaign 和 Campaign 对话均在写入游戏状态前，通过 `llmsettings.ReadyRoute` 校验当前有效模型配置。个人配置缺失返回 `400/llm_configuration_incomplete`；平台配置缺失返回 400。客户端密钥不上传，后端只检查其地址和模型，前端额外校验本设备 Key。

设置由管理员接入模式与直连开关共同控制。`GET /api/settings/llm` 返回用户个人配置行、`access_mode`、`source`、`editable`、`llm_route` 和 `client_route_enabled`（平台是否允许客户端直连），以及按已保存 base_url 识别的只读 `resolved_profile`（只含 `{id}`，协议完全由用户选择，档位不表达协议信息，供前端展示“已识别为 …，已自动适配请求参数”）；`platform_managed` 下返回的也是个人配置，看不到平台 base_url 与模型名，平台配置不完整时设置页仍可打开。`GET` 返回的 `llm_route` 已是有效路由（只由开关和用户保存值决定）：开关关闭时用户保存的 `client` 被忽略为 `server`。`source` 按有效路由与接入模式给出 `platform` / `user` / `client`；`editable` 表示个人配置是否可编辑（`user_managed`，或开关开启的 `platform_managed`）。GET 不返回 API Key，只返回 `has_api_key`，并通过 `configuration_ready` 表示当前有效模型配置是否齐全：平台模式检查公共配置而不公开公共地址或模型；个人服务端模式要求地址、模型和已保存 Key；客户端模式检查地址与模型，本设备 Key 由前端独立检查。`client` 路由的运行时配置不解密、不要求 Key，`source` 为 `client`。PATCH 支持 `llm_route`：切到 `client` 会忽略请求中的 `api_key` 并清空服务端 Key（`user_managed` 下切回需重填，`platform_managed` 下不涉及服务端 Key）；用户设置与公共配置都没有 provider 字段，请求体出现 `provider` / `public_llm_provider` 时被忽略，不报错；`platform_managed` + 开关开启 + `server` 只把 `llm_route` 写为 `server`（个人配置与 Key 原样保留，请求体配置字段被忽略，不要求 Key）；路由变化递增 `settings_revision`；平台未开放直连时以 `client` 保存返回 403（`code=llm_client_route_disabled`），`platform_managed` + 开关关闭时用户 PATCH 返回 403，未传 `llm_route` 时沿用当前有效路由；切离 `client` 时服务端主动关闭该玩家 Relay 连接。`platform_managed` 下 `/api/settings/fetch-models` 返回 403（客户端直连由浏览器直接请求 `/models`）；`client` 路由下 `/api/settings/fetch-models` 返回 409（`code=llm_route_client`）。管理员通过 `GET/PATCH /api/admin/system-settings` 维护模式、公共配置和 `client_llm_route_enabled`（只有 owner 可改该字段）；GET 响应附带按公共 base_url 识别的只读 `public_llm_resolved_profile`；Key 只写入不回显，并通过 `POST /api/admin/system-settings/fetch-models` 获取公共模型列表；公共 LLM 能力检测走独立的 owner 接口 `POST /api/admin/system-settings/capabilities/check`（请求体 `{role}`，直接使用平台配置经服务端直连，与检测者本人的路由无关）。关闭直连开关时关闭全部 Relay 连接；切换公共 LLM 不断开。

check 是用户显式触发的同步、无副作用诊断，最长使用独立的 3 分钟请求超时。请求体只能是 `{"role":"narrative"}`、`{"role":"memory"}` 或 `{"role":"character_inquiry"}`；接口不接受 URL、密钥或模型参数，而是读取当前已保存设置。

成功响应为 `{"settings_revision":n,"result":{...}}`。Narrative 以生产请求相同的默认工具选择语义检测真实的 Tool Call -> Tool Result -> 普通文本闭环；Memory 与 Character Inquiry 检测各自模型的原生 JSON Schema。`result.status` 为 `supported/unsupported/inconclusive/unavailable`：只有上游明确拒绝目标能力才是 `unsupported`，单次模型行为或无法归因的参数错误是 `inconclusive`。结论属于已保存的 Base URL、协议与模型组合，只代表本次诊断，不持久化、不自动重试，也不改变生产请求。保存设置不会自动探测，前端不得用未保存输入静默检测旧设置。

## 管理员设置与挑战

用户与审计：

```text
GET   /api/admin/users?q=...&limit=30&offset=0
PATCH /api/admin/users/:id/role
PATCH /api/admin/users/:id/status
GET   /api/admin/audit-logs?limit=50&cursor=...&actor_id=...&action=...
```

用户列表返回 {users,has_more}。q 为 UUID 时精确匹配，包含 @ 时按完整邮箱匹配，其他按用户名/昵称字面包含搜索；大小写不敏感，转义 SQL 通配符，按 created_at DESC,id DESC 排序。limit 上限 200，offset 上限 100000，切换查询重置 offset。用户列表和审计读取需要管理员；角色和账号状态变更只允许 owner。角色只能是 `user`、`admin` 或 `owner`，owner 转交与唯一 owner 保护由后端事务执行。

```text
GET   /api/admin/system-settings
PATCH /api/admin/system-settings

GET   /api/admin/llm-request-logs?limit=50&conversation_id=...
GET   /api/admin/llm-request-logs/:id
GET   /api/admin/context-build-logs?limit=50&conversation_id=...
GET   /api/admin/context-build-logs/:id
GET   /api/admin/diagnostic-status

GET   /api/admin/auth-settings
PATCH /api/admin/auth-settings
POST  /api/admin/auth-settings/test-email

GET   /api/admin/invitation-codes
POST  /api/admin/invitation-codes
PATCH /api/admin/invitation-codes/:id

GET  /api/admin/builtin-challenges
GET  /api/admin/builtin-challenges/:slug/preview
GET  /api/admin/builtin-challenges/:slug/preview

GET   /api/admin/gameplay-modules
GET   /api/admin/reward-definitions
POST  /api/admin/reward-definitions

GET  /api/admin/challenges
POST /api/admin/challenges
POST /api/admin/challenges/:id/versions
GET  /api/admin/challenge-versions/:id
PUT  /api/admin/challenge-versions/:id
POST /api/admin/challenge-versions/:id/publish
```

认证设置响应不包含 SMTP 密码，只包含 `has_smtp_password`。`PATCH /api/admin/auth-settings` 仅更新提供的字段，未提供或为 `null` 的普通配置字段保留原值，显式 `false` 或空字符串按实际值校验；合并当前配置后执行完整校验和事务审计。后台登录、SMTP、验证码分组可以独立保存，不会覆盖其他页草稿。`smtp_password` 为空表示保留原值，`clear_smtp_password=true` 表示清除。邀请码创建响应是唯一返回完整 `code` 的时机，后续列表只返回尾号、状态、用量和有效期。

`PATCH /api/admin/system-settings` 维护判定与诊断策略等设置。模型摘要始终提交，`detailed_llm_logging_enabled` 仅控制默认关闭的正文采集；正文按字段预算脱敏、截断，不能称为完整可重放请求。诊断清理默认开启、保留 30 天，仅处理模型和上下文日志。管理员配置审计与更新原子提交；过时并发写入返回 `409/settings_conflict`。该通用接口和响应不包含 LLM 出站代理地址、用户名、CA 或密码状态。

### LLM 出站代理（owner）

```text
GET   /api/admin/system-settings/llm-proxy
PATCH /api/admin/system-settings/llm-proxy
POST  /api/admin/system-settings/llm-proxy/test
```

三个接口都要求 `owner`，普通 `admin` 和用户直接请求返回 403。GET 返回 `{enabled,url,username,has_password,ca_pem,revision}`，不返回明文密码或密文；`has_password` 只表示是否已保存密码。PATCH 接受：

```json
{
  "expected_revision": 3,
  "enabled": true,
  "url": "http://proxy.internal:3128",
  "username": "optional-user",
  "password": "optional-new-password",
  "clear_password": false,
  "ca_pem": "-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----"
}
```

省略字段保留原值；`password` 非空表示替换，空或省略表示保留；`clear_password=true` 表示清除，不能与新的非空密码同时提交。`expected_revision` 必须显式提供非负整数，省略、null 或负数返回 400；与当前 `llm_proxy_revision` 不一致返回 `409/settings_conflict`。URL 只接受 http/https、明确主机和端口，拒绝 userinfo、query、fragment、业务路径、CR/LF 和超长值；CA 必须是可解析的证书 PEM，不能包含私钥。配置无效返回 `400` 与 `code=llm_proxy_invalid`；需要使用密码时，缺少加密密钥或已保存密文无法解密返回 `503` 与 `code=llm_proxy_encryption_unavailable`。成功响应与 GET 相同，修订号加 1。仅停用代理无需解密旧密码，原密文保留；再次启用仍须能解密，或显式替换/清除密码。

测试接口接受可空 `{"url":"https://target.example"}`。URL 留空时从已保存平台公共 Base URL 提取 origin；无法提取时返回 `400` 与 `code=llm_proxy_test_url_required`。检测 URL 必须是 HTTPS，拒绝 userinfo、query、fragment 和 CR/LF。代理未启用返回 `409` 与 `code=llm_proxy_disabled`。

测试使用已保存配置和运行快照，发送一次不带供应商 API Key、Cookie、Prompt 的 `HEAD`，不跟随重定向、不自动重试，整次截止时间 10 秒，并且不改变业务 Base URL。响应为 `{ok,code,error,target_status,elapsed_ms,checked_at,revision,stale}`；目标返回 401/403/405 只说明网络通路可达，`ok=true`，仍不代表模型授权或协议通过。网络失败 `ok=false`，`code` 使用 `proxy_unreachable`、`proxy_auth_failed`、`target_unreachable`、`tls_certificate_failed`、`timeout`、`outbound_error` 等固定分类；检测期间配置被并发修改时 `stale=true`，结果属于响应中的旧 `revision`。

### 诊断策略与其他管理接口

详细正文范围由 `detailed_llm_logging_scope`（`all/user/conversation`，默认 `all`）与 `detailed_llm_logging_target_id` 控制。GET 的目标为 UUID 字符串或 `null`；PATCH 省略字段保留原值，目标传空字符串表示清空。切换范围类型会清除旧目标，指定范围须在同一请求提供有效非零 UUID；`all` 不能携带目标。新目标和重新开启时检查用户存在或对话存在且未删除，参数/目标无效返回 400。只关闭开关会保留范围，不设自动关闭时间。

例如 `{"detailed_llm_logging_enabled":true,"detailed_llm_logging_scope":"user","detailed_llm_logging_target_id":"<用户 UUID>"}` 只采集该用户模型正文。无归属或不匹配的调用继续保留摘要；Memory 按所属用户匹配，无对话归属的模型诊断不匹配对话范围。判断在调用开始时固定，规则变更不回溯在途调用，详细语义见[日志设计](23-logging-and-diagnostics.md)。

上下文构建日志不受详细模型日志开关影响：管理员列表返回最近构建的预算摘要，单条详情返回 Prompt 分段、必选/裁剪结果、历史消息 ID、世界书 entry ID 和 Memory lag。普通玩家上下文接口不会返回这些技术明细。

内置列表返回来源版本、哈希及可导入状态；预览生成统一 GameplayImport，确认使用 /admin/gameplay-imports/:id/confirm。旧内置 /import 与 preview_token 协议移除。手动导入只生成草稿，发布不封存旧局，fork 在当前入口新开局且不复制历史进度。

预览接口返回当前/拟导入结局积分规则，确认时在事务中重新计算 token；过期返回冲突，需重新预览。

gameplay-modules 返回已安装的 engine、动作、定义 Schema、verifier 与 settlement handler。结局通过 reward_points 设置固定终局积分；支持阶段预算的定义 Schema 以 `x-reward-budget` 声明阶段数组、积分、结局与总额字段，后台据此显示“阶段 + 终局 = 总预算”，发布时仍由后端复核。

挑战版本 PUT 接受：

```json
{
  "manifest": {},
  "runtime": {},
  "assets": [],
  "definitions": [],
  "outcomes": []
}
```

保存允许内容暂时不满足发布条件，但必须是合法 JSON 和基本合法资产集合。publish 执行完整业务校验并返回发布后的完整版本文档。

不存在 Character、Prompt、WorldBook 或旧预设包自由 CRUD 路由。

## WebSocket 连接

```text
GET /ws/conversations/:id
GET /ws/llm-relay
```

升级前经过 `requireAuth`，handler 还会校验对话归属。浏览器 Origin 必须位于 `ALLOWED_ORIGINS`；空白配置只用于开发。

一个连接只绑定一个对话，同一对话可以有多个观察连接。Hub 对同一对话维护一个当前生成任务；普通新请求不会静默取消旧任务，而是返回冲突，只有显式取消或生命周期变更才会请求取消并等待旧任务释放 durable generation run。队列中的 follow-up/steer 由同一 Worker 串行消费。

浏览器端首连不计入自动重连预算；首连失败后最多重连 5 次，延迟依次为 1/2/4/8/16 秒。握手超时 10 秒，等待 `subscription.ready` 超时 5 秒。只有当前对话的 ready 才算订阅成功，`onopen`、错误对话的 ready 和旧连接回调都不重置预算。第 5 次成功且权威快照恢复、连续健康 30 秒后预算归零；ready 后立即掉线不会反复归零。隐藏/离线时暂停待执行的重连，不消耗次数，恢复可见/online 不会额外叠加正在建立的连接。ready 后业务恢复最多失败 5 轮，失败取消健康计时，后续恢复成功才重新开始计时。

首连失败时只执行一次受预算约束的 REST 定位，用于区分可重试网络错误和明确的 401/403/404。自动重连耗尽或访问被拒绝后停止自动 WebSocket、REST 兜底和资源补读，保留最后页面并提示刷新；发送、继续、重新生成、guidance 动作、模块行动、结局确认和询问写入均被禁用，不能通过 REST 降级绕过。用户刷新页面或进入新对话上下文后重新开始预算。

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
| `subscription.ready` | `conversation_id` | 鉴权且注册订阅后发送，客户端再开始 REST 对账 |
| `activity_updated` | `activity_version`, `branch_epoch`, `snapshot`, `reason` | 提交后完整活动快照；同一对话可合并到最新版本 |
| `conversation_ended` | conversation ID | 手动结束或状态变更 |
| `challenge_completed` | `run_id`, `outcome`, `reward_grants`, `metrics`, `next_actions` | 通用挑战结算成功；`client` 路由时 `outcome.rewards_disabled_reason=client_llm_route` 且无奖励 |
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

TPS 使用成功回合内各完成模型步骤的总输出 Token 除以模型请求总耗时之和（`model_duration_ms`），流式与非流式均包含首响应等待，不扣除隐藏推理可能占用的时间。该值为综合平均输出速度，包含供应商 usage 中的推理及工具输出，不代表纯正文流式速度；上下文装配、领域工具执行与回合落库耗时不计入分母。优先使用供应商 usage；缺失时只估算正文、可见思考、拒绝文本及工具名称/参数，不计存储包装字段、调用 ID 与 opaque replay，标记为估算值。缺少有效模型请求耗时时不展示 TPS。

`GET /api/conversations/:id/runtime-inspector` 返回玩家运行面板投影。当前活动实时更新；性能和上下文只取最近一次成功完成且未失效的回合尝试，失败、部分完成、取消或运行中的尝试不替换上次成功结果。性能仅统计完成的模型步骤及已提交工具，失败诊断仍完整保留于管理日志。`context_built` 不直接更新玩家统计，成功完成后或重连时读取快照。

`activity.memory_job` 返回最新归档任务的公开元数据：状态、目标回合、执行次数/上限、排队/开始/结束时间；`memory_archive` 返回当前有效归档覆盖回合和 Token 数。活动接口也返回 `memory_job`，用于按需对账和提交后推送。归档正文、Worker、租约和原始错误不公开。进程级共享内容缓存与当前用户回合成功无关，不再通过此接口返回或在回合面板展示；底层缓存与诊断日志不因生成失败而伪造回滚。该接口不暴露 Prompt、世界书、密钥、原始响应或隐藏工具参数。

## 对话分享 API

所有者接口需要登录且只操作自己的数据：

```text
POST   /api/conversations/:id/shares
GET    /api/conversation-shares
DELETE /api/conversation-shares/:id
POST   /api/conversation-shares/:id/replace
```

列表接口接受 `status=active|expired|revoked|moderated`、`search`、不透明的 `cursor` 和 `page_size`（最大 100），只返回当前用户的数据，并附带 `has_more`、可选 `next_cursor` 和来源对话标题；它不执行总数统计。创建与 replace 接受 `scope=all|range`、可选的起止消息 ID、标题、`show_username`、`expires_in_days=0|7|30` 和客户端 UUID `request_id`。`show_username=false` 时署名固定为“匿名旅人”且不公开头像；为 true 时由后端读取当前登录账号的 `display_name` 和头像，不接受客户端自定义身份字段。range 的端点必须属于同一对话当前有效分支且顺序正确，服务端自动包含中间全部消息。

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

## 客户端直连 Relay（`/ws/llm-relay`）

用户级 WebSocket，专门承载模型请求转发。升级前经过 `requireAuth`，且当前有效路由必须为 `client`，否则拒绝升级（403）。心跳沿用 Hub 参数（30s ping / 60s read），入站单帧上限 256KiB（独立于会话 WS 的 64KiB）。

服务端 → 客户端（JSON 文本帧）：

- `relay.request`：`relay_id`、`endpoint`（仅 `/chat/completions` 或 `/responses`）、`stream`、`purpose`、`conversation_id`、`body`（请求体 JSON 字符串，原样透传）。body 最大 8MiB，超限直接失败不发送。
- `relay.cancel`：`relay_id`，取消在途请求。

客户端 → 服务端：

- `relay.response.head`：`relay_id`、`status`、`content_type`、`retry_after`。
- `relay.response.chunk`：`relay_id`、`seq`（从 1 连续递增）、`data`（原始响应文本片段）。
- `relay.response.end`：`relay_id`、`seq`（等于 chunk 总数）。
- `relay.response.error`：`relay_id`、`code`（`network`|`no_key`|`aborted`|`invalid_request`）、`message`。

语义：`head` 前断开/超时（120s）可重试；`head` 后断开、`seq` 乱序、未知 `relay_id` 的后续帧按中断处理（不可重试）；`no_key`/`network` 不可重试。同玩家多连接时请求只派发给最新存活连接，旧连接作候补；每玩家并发上限 4，超限排队（受 ctx 约束）。

错误码：`llm_route_mismatch`（对象路由 ≠ 当前有效路由，REST 409 / WS `notice`）、`llm_relay_unavailable`（无 Relay 连接，问答等同步接口 409）、`llm_relay_lost`、`llm_relay_network`、`llm_relay_no_key`、`llm_client_route_disabled`（平台未开放直连时以 `client` 保存设置，PATCH 403）。`POST /api/settings/fetch-models` 在 `client` 路由返回 409（`code=llm_route_client`）。

## 诊断分页与错误关联

HTTP 返回服务端生成的 `X-Request-ID`，未知 500 为 `{error:"服务暂时不可用",code:"internal_error",trace_id:"<UUID>"}`；业务错误保持原语义。该 trace 不作为授权或幂等键。

模型、上下文、管理员审计列表统一返回 `{logs,next_cursor,has_more}`，均支持 `from/until/limit/cursor`，按 `(created_at DESC,id DESC)` 分页并保留时间精度；游标绑定种类与筛选。模型另支持 conversation_id、attempt_id、trace_id、purpose、model、success；上下文支持 conversation_id、attempt_id；管理审计支持 actor_id、action。非法游标返回 400；详情 404 可能是已清理。

诊断状态返回 `{instance_id,writer,cleanup}`，读取进程内队列和最近清理状态，不扫描日志表。具体字段与权限边界见[日志、诊断与配置审计](23-logging-and-diagnostics.md)。


## 结局提议与恢复接口

新增 `GET /conversations/:id/endings`：返回 revision、branch_epoch、has_trusted_actions、abandon_action、ending_capabilities、proposal、completion、is_processing。提议事件为 `ending.proposed`；两种模式的真实结束都返回/广播 `CompletionView`，WS 字段为 `challenge_completed.completion`。确认仍调用现有玩家动作端点。 完整协议见 [26 玩法结局生命周期](26-gameplay-ending-lifecycle.md)。


### 玩法入口启用状态

`PUT /api/admin/challenges/:id/enabled`（管理员）请求 `{ "enabled": true }` 或 `{ "enabled": false }`，返回更新后的 ChallengePack。缺少布尔值返回 400；没有当前已发布版本的包拒绝操作。停用使玩家列表、详情和新开局不可用，已有存档可继续。`GET /api/admin/builtin-challenges` 额外返回每个已导入包的 `pack_id` 和 `enabled`，未导入项没有 `pack_id`。

## 玩法包交换与版本管理

新增上传/导出、导入记录/确认重查、引用、入口回退、运行策略和拥有者 gameplay-version 接口。保存/发布带 expected_revision，发布默认不切换入口；管理写入要求管理员，导入记录按操作者隔离。接口详情见专题。

完整协议见 [27 玩法包交换与版本生命周期](27-gameplay-package-exchange-and-versions.md)。


### 活动快照与恢复协议

/activity、activity_updated.snapshot 和 runtime inspector.activity 共用 storage.ActivityRepo 投影。新增 activity_version、conversation_id、branch_epoch、conversation_status、is_processing、turn_id、attempt_id；memory_job 包含公开 ID。稳定字段与版本来自同一 SQL 语句；phase 独立补充，is_processing=false 时为 idle。

版本由 conversation_activity_versions 保存，00001 中的延迟触发器使稳定变更与递增原子提交，同事务同对话至多一次。续租、step 和 delta 不递增。PG NOTIFY 在提交后唤醒服务端，25ms 合并后按对话读一次完整快照。根 branch_epoch=0 可省略。

完整快照可跨版本直接应用；refresh_required=true 的失效通知只提高目标版本，需取得至少该版本的快照。当前发布器发送完整快照。agent phase 仍按 attempt + sequence 排序，不用 activity_version 排序字符。无版本业务事件不能覆盖更新的权威快照。

客户端先缓冲、收到 subscription.ready 后读取活动及消息/状态/Campaign/结局/guidance/奖励等快照，再合并事件。ready 超过 5 秒、恢复超过 15 秒或缓冲超过 2048 条/约 2MiB 时关闭连接恢复。首连失败后最多自动重连 5 次；耗尽或鉴权失败后停止自动恢复和写入，提示用户刷新。健康空闲不轮询；已知任务无进展由 watchdog 核验，重连预算内的恢复请求按资源单飞合并，同一资源在途再次失效最多补读一次。LISTEN 缺口关闭 WS 订阅以强制重建快照。协议不提供 durable replay，仍按单实例部署。
