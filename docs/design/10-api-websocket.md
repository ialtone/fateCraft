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

状态操作：

- end：active/owned 对话变为 ended，不发奖励。
- archive：只接受 active。
- restore：只接受 archived。
- delete：软删除 owned 对话。

## 消息与生成

```text
GET    /api/conversations/:id/messages?before=...&limit=...
POST   /api/conversations/:id/messages
POST   /api/conversations/:id/regenerate
POST   /api/conversations/:id/continue
PATCH  /api/messages/:id
POST   /api/messages/:id/edit-and-regenerate
DELETE /api/messages/:id
```

消息写操作必须解析消息所属对话，再校验当前用户和 active 状态。不能只按 message ID 更新。

regenerate、continue 和 edit-and-regenerate 通过后台生成任务执行，最终消息与状态通过 WebSocket 广播。REST 接受响应不代表模型已经完成。

## 状态

```text
GET   /api/conversations/:id/state
PATCH /api/conversations/:id/state
GET   /api/conversations/:id/state/revisions
```

GET 和 revisions 对对话所有者开放；PATCH 同时要求管理员。PATCH 支持补丁或整份状态维护，写入来源记为 system。普通用户的游戏状态只能由后端处理模型信封时更新。

## 上下文

```text
GET /api/conversations/:id/context/last
GET /api/context/:cid
```

第二个路由中的 `cid` 是上下文构建日志 ID，不是对话 ID。Handler 先读取日志，再反查其 conversation 并做归属检查。响应包含预算与段明细，不包含会话令牌或 LLM API Key。

## 摘要

```text
GET   /api/conversations/:id/summaries
POST  /api/conversations/:id/summaries
PATCH /api/summaries/:id
```

列表对 owned 对话开放。生成和编辑要求 active 对话。编辑正文必须提交 `conversation_id`，后端用对话和摘要双重条件更新。

## 账号与回溯

```text
GET  /api/account
POST /api/account/rewind/use
```

账号按当前认证用户读取。回溯使用接口负责扣减账号次数，不能接受客户端提交余额或用户 ID。

## LLM 设置

```text
GET   /api/settings/llm
PATCH /api/settings/llm
POST  /api/settings/fetch-models
```

设置按当前用户隔离。GET 不返回 API Key，只返回 `has_api_key`。fetch-models 由后端代理访问指定 OpenAI 兼容服务，用于避免浏览器跨域；错误响应不能泄漏其他用户设置。

## 管理员设置与挑战

```text
GET   /api/admin/roll-settings
PATCH /api/admin/roll-settings

GET   /api/admin/auth-settings
PATCH /api/admin/auth-settings
POST  /api/admin/auth-settings/test-email

GET   /api/admin/invitation-codes
POST  /api/admin/invitation-codes
PATCH /api/admin/invitation-codes/:id

GET  /api/admin/builtin-challenges
POST /api/admin/builtin-challenges/:slug/import

GET  /api/admin/challenges
POST /api/admin/challenges
POST /api/admin/challenges/:id/versions
GET  /api/admin/challenge-versions/:id
PUT  /api/admin/challenge-versions/:id
POST /api/admin/challenge-versions/:id/publish
```

认证设置响应不包含 SMTP 密码，只包含 `has_smtp_password`。PATCH 中 `smtp_password` 为空表示保留原值，`clear_smtp_password=true` 表示清除。邀请码创建响应是唯一返回完整 `code` 的时机，后续列表只返回尾号、状态、用量和有效期。

内置内容列表只返回仓库包的公开元数据、版本、内容哈希、数据库当前版本和 `available/imported/conflict` 状态，不返回 Prompt 或世界书正文。导入接口只接受扫描结果中的 slug，不接受文件路径或上传内容；成功后通过完整发布事务立即成为该挑战的当前版本。内置源必须使用 `major.minor.patch` 稳定版本号；同版本哈希不一致或源版本不高于数据库当前版本时返回 409。

挑战版本 PUT 接受：

```json
{
  "manifest": {},
  "assets": [],
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

一个连接只绑定一个对话，同一对话可以有多个观察连接。Hub 对同一对话维护一个当前生成任务，新任务会取消旧任务，并等待旧任务释放数据库生成锁。

## 客户端消息

发送用户输入：

```json
{ "type": "user_input", "content": "我选择闭关筑基" }
```

取消生成：

```json
{ "type": "cancel_generation" }
```

空白用户输入被拒绝并广播 error。未知消息类型当前忽略。

## 服务端事件

| type | 关键字段 | 含义 |
| --- | --- | --- |
| `generation_started` | `message_id` | 某条临时消息开始生成 |
| `generation_delta` | `message_id`, `delta` | 该消息新增的可见叙事 |
| `generation_finished` | `message_id` | 该消息流结束 |
| `generation_discarded` | `message_id` | 候选回复未通过动作预校验，删除临时流式消息且不会持久化 |
| `message_created` | `message` | 服务端已经持久化消息 |
| `message_updated` | `message` | 已有展示消息更新 |
| `state_update` | `state` | 当前状态的新完整值 |
| `context_built` | `context_build_id` | 上下文日志可读取 |
| `conversation_ended` | conversation ID | 手动结束或状态变更 |
| `ending_triggered` | 结局、奖励、回合 | 挑战结算成功 |
| `error` | `error` | 生成、权限或持久化错误 |

判定两阶段和安全纠错都可能产生多个 message ID。收到 `generation_discarded` 时，客户端必须移除该 ID 的全部临时增量并复位对应流状态；后端随后可能以新 ID 开始一次纠错生成。

## 连接存活与丢事件

服务端每 30 秒发送 ping，60 秒未收到任何帧或 pong 则关闭连接。每个连接发送队列有限，队列满时事件可能被丢弃，以避免慢客户端阻塞整个 Hub。

因此 WebSocket 不是可靠事件存储。客户端重连、刷新或发现事件缺口时，应通过 REST 重新加载对话、消息、状态和账号。

## CORS 与 Cookie

显式配置 `ALLOWED_ORIGINS` 时，HTTP CORS 允许凭证并支持 GET、POST、PUT、PATCH、DELETE、OPTIONS。未配置时使用通配来源但不允许跨域凭证，只适合本地同源代理开发。

生产推荐由同一域名反代前后端，避免跨站 Cookie 的 SameSite 限制。
