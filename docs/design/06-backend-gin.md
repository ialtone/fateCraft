# 06. Go 后端设计

## 技术与入口

后端使用 Go、Gin、GORM、PostgreSQL、WebSocket 和 zap。应用入口是 `cmd/server`，负责首次内容初始化、HTTP、WebSocket、LLM 和游戏运行时。

`cmd/server` 不运行 AutoMigrate，表结构只由 `scripts/sql/init.sql` 初始化。连接数据库后，server 在事务级 advisory lock 内检查 `challenge_packs`：为空时读取 `preset_packs` 并通过正常发布流程初始化内置挑战；非空时直接跳过，不读取或修改内置内容。

## 模块划分

```text
internal/api             Gin 路由、中间件、参数和 HTTP 响应
internal/auth            邮箱认证、LinuxDo OAuth、会话生命周期
internal/challenge       挑战查询、版本、哈希、发布与结算
internal/challengeimport 仓库材料解析、显式导入与空库初始化策略
internal/conversation    对话创建、归属、状态机和生成锁
internal/chat            内部消息与展示消息写入
internal/state           默认状态、白名单补丁和修订记录
internal/contextbuilder  token 预算和消息装配
internal/worldbook       纯世界书关键词扫描算法
internal/memory          摘要读取、版本与 token 统计
internal/roll            判定计算、掷骰和历史调整
internal/orchestration   LLM 调用、两阶段判定和动作分发
internal/realtime        WebSocket 连接与事件广播
internal/storage         GORM 模型与 PostgreSQL 仓储
internal/llm             OpenAI 兼容 Chat Completions 客户端
internal/protocol        模型结构化信封解析
```

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

邮箱认证策略和 SMTP 配置来自单行 `auth_settings`，每次登录、发码或注册都读取服务端设置。验证码只保存 bcrypt 摘要并限制有效期、重发和尝试次数；邀请码只保存 SHA-256 摘要。注册事务负责消费验证码与邀请码、创建用户和初始账号，任何一步失败都会整体回滚。SMTP 密码通过部署环境变量 `AUTH_DATA_ENCRYPTION_KEY` 加密，管理 API 只暴露是否已经配置。

LinuxDo 流程保存哈希后的 state、PKCE verifier、return path 和短期过期时间。回调必须同时验证 state、code、provider 和过期时间，再交换 token。Client Secret、上游 token 和数据库连接串不得写入日志或 API 响应。

`requireAdmin` 必须位于 `requireAuth` 之后，并只信任已加载的 `storage.User.Role`。

## 挑战内容

`challenge.Service` 负责三种读取：

- 公开挑战：只返回公开 Manifest 字段、允许 Persona 和奖励概要。
- 管理文档：只供管理员读取完整版本、资产和奖励规则。
- 运行 Bundle：按对话固定版本加载 Character、Persona、Scenario、Prompt 和 WorldBook。

公开 API 不能复用完整管理结构，否则会泄漏技术 Prompt 和世界书。

### 草稿保存

保存草稿会锁定版本行、确认状态仍为 draft，然后在事务中替换 Manifest、资产和奖励规则。已发布版本返回冲突，不允许更新。

### 发布

发布事务执行：

1. 对版本行加 `FOR UPDATE`。
2. 读取同一事务内的 Manifest、资产和奖励规则。
3. 校验挑战包元数据、Manifest 引用、资产 ID、三个核心 Prompt 分段、世界书条目和奖励规则。
4. 对 JSON 规范化并计算 SHA-256。
5. 把版本设为 published，并更新挑战包当前版本。

保存和发布锁定同一行，防止发布读取后又被并发保存导致哈希过期。

## 对话创建与运行

对话服务只接收挑战 slug、Persona slug 和标题。Challenge Service 解析并验证发布版本，Conversation Service 写入固定版本和资产，随后初始化状态与开场消息。

所有会改变叙事的操作先调用 `EnsureActive`，同时检查归属和状态。只读操作使用 `GetOwned`，允许查看 ended 或 archived 内容。

## 生成管线

普通生成流程：

1. 校验对话归属和 active 状态。
2. 原子获取 `is_processing` 生成锁。
3. 读取用户 LLM 设置和管理员系统设置。
4. Challenge Service 加载并校验固定 Bundle。
5. 读取状态、摘要、消息并扫描世界书。
6. Context Builder 按预算构造消息并写上下文日志。
7. 调用模型，解析结构化信封。
8. 落库叙事、应用状态补丁、分发动作。
9. 通过 WebSocket 推送消息和状态事件。
10. 释放生成锁并更新对话时间。

流式模式只向前端发送从累积 JSON 信封中提取出的 `narrative` 增量，不能把裸 JSON、notes 或内部动作参数展示给玩家。

## 两阶段判定

第一阶段模型提交 `roll_request` 后：

1. 保存第一阶段叙事和必要资源消耗。
2. 服务端读取状态并计算成功率。
3. 服务端掷骰并写入 `roll_history`。
4. 判定结果同时写入内部 system 消息和展示消息。
5. 第二次调用模型，让它依据判定结果继续。
6. 第二阶段若再次请求判定则忽略，防止递归判定循环。

管理员难度来自 `system_settings`，不接受用户或模型覆盖。

## 状态更新

状态补丁只允许规定顶层命名空间，点号表示嵌套路径，`+` 表示数组追加。非法路径被跳过并记录在展示消息 metadata 中。

状态读改写使用事务和行锁，成功后写入 `state_revisions`。普通用户没有状态 PATCH 路由；管理员修改使用 `SourceSystem`，模型修改使用 `SourceAI`。

## 动作与结算

模型动作通过注册表按 `type` 分发。当前 `trigger_ending` 由修仙结局处理器采集服务端状态、消息回合和认证玩家，再调用 Challenge Service。

结算服务锁定对话，验证固定版本奖励规则和判定流水，计算奖励并原子更新 `accounts`、`challenge_runs` 与对话状态。动作 handler 不直接写账号。

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
- Warn：模型动作未满足版本规则、奖励申请被拒绝、参数或数据库约束未通过等需要观察但未破坏系统的行为。
- Error：数据库执行故障、事务失败、模型服务失败、配置缺失或无法持久化等系统异常。

WebSocket 的 `error` 是“本次操作无法继续”的客户端事件类型，不等同于服务端 Error 级别。服务端必须根据原始 error 类型分级，不能根据客户端事件名称统一记 Error。

GORM 使用自定义 zap logger：绑定参数通过 `ParamsFilter` 保持为占位符；普通 SQL 与未命中只在 Debug 展示，SQLSTATE `23` 完整性约束和超过 200ms 的慢查询记 Warn，其他 SQL 执行故障才记 Error。

上下文构建日志是业务级审计数据，记录模型、预算、段、消息 ID、世界书命中和挑战哈希，不等同于应用日志。

## 并发与事务原则

- 同一对话生成：条件 UPDATE 生成锁。
- 状态读改写：状态行锁。
- 草稿保存与发布：版本行锁。
- 结算：对话行锁与唯一约束。
- 多表业务写入必须由仓储事务封装，不能在 handler 中分散提交。

## 后端回归清单

- `go test ./...` 与 `go vet ./...` 通过。
- 普通用户不能进入管理员路由。
- 所有对话子资源都有归属检查。
- 发布校验拒绝缺少核心分段或无效奖励引用。
- 已发布内容哈希在运行时重新验证。
- 两阶段判定只记录服务端结果。
- 重复结算不会重复增加账号奖励。
- 空库启动会自动发布内置挑战，失败时阻止服务带着不可用内容继续启动。
- 已有任意挑战包时，server 启动不会读取、升级或覆盖内置挑战。
