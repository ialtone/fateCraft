# 07. PostgreSQL 数据库设计

## 初始化原则

权威 schema 是 `backend/scripts/sql/init.sql`，面向空数据库一次初始化。项目不维护旧表兼容、增量迁移或 GORM AutoMigrate 旁路。

初始化步骤：

1. 创建或清空 `fatecraft` 数据库。
2. 使用数据库所有者完整执行 `init.sql`。
3. 配置 `backend/.env` 并启动 server。
4. server 发现 `challenge_packs` 为空后，自动校验并发布内置挑战。

首次内容初始化由 PostgreSQL advisory lock 串行化，并包在事务中；任何内置包读取、校验或发布失败都会整体回滚并阻止服务启动。数据库已有任意挑战包时跳过初始化，不会把仓库内容同步到已有数据库。

`CREATE EXTENSION IF NOT EXISTS pgcrypto` 用于提供 `gen_random_uuid()` 等 PostgreSQL 函数，不代表自动加密整个数据库。

## 关系总览

```text
users
  |-- auth_identities
  |-- auth_sessions
  |-- llm_settings
  |-- accounts
  |-- conversations
        |-- chat_messages -- display_messages
        |-- conversation_states -- state_revisions
        |-- memory_summaries
        |-- context_build_logs
        |-- roll_history
        `-- challenge_runs

challenge_packs
  `-- challenge_versions
        |-- challenge_assets
        `-- challenge_reward_rules
```

`conversations` 同时关联用户和固定挑战版本，是运行时数据的隔离根。`challenge_runs` 同时引用对话、玩家和挑战版本，并由组合外键保证三者与原对话一致。

## 认证与身份

### `users`

本地用户主体。关键字段：

- `email`：可空且唯一；纯 LinuxDo 用户可以没有本地邮箱。
- `password_hash`：可空，只保存 bcrypt 哈希。
- `display_name`、`avatar_url`：公开资料。
- `status`：`active` 或 `disabled`。
- `role`：`user` 或 `admin`。
- `last_login_at`：最后成功登录时间。

### `auth_identities`

外部身份映射。`(provider, provider_user_id)` 唯一，防止同一 LinuxDo 用户创建多个本地账号。`profile` 保存上游返回的扩展资料，`trust_level` 可用于未来准入规则。

### `auth_sessions`

主键是 64 位十六进制会话令牌哈希。数据库不保存浏览器持有的明文令牌。`expires_at` 决定会话是否有效，并有单独索引用于查询和清理。

### `oauth_login_flows`

保存短期 OAuth state 哈希、provider、PKCE verifier、return path 和过期时间。流程完成后应删除，过期 flow 不得复用。

## 挑战内容

### `challenge_packs`

稳定挑战入口：

- `slug` 全局唯一。
- `name`、`description`、`cover_url` 是公开元数据。
- `status` 为 `active`、`hidden` 或 `retired`。
- `current_version_id` 指向当前发布快照。
- `created_by` 记录管理员，用户删除时置空。

组合外键 `(current_version_id, id) -> challenge_versions(id, challenge_pack_id)` 保证当前版本确实属于该挑战包。

### `challenge_versions`

完整版本快照：

- `(challenge_pack_id, version)` 唯一。
- `manifest` 为 JSONB。
- draft 的哈希为空且没有 `published_at`。
- published/retired 必须具有 64 位小写 SHA-256 和发布时间。
- `created_by`、`published_by` 分别记录创建和发布者。

应用禁止已发布版本重新变为 draft 或原地编辑。

### `challenge_assets`

一个版本内的资产集合。`kind` 只能为：

- `character`
- `persona`
- `scenario`
- `prompt`
- `worldbook`

`(challenge_version_id, kind, slug)` 唯一，`content` 必须是 JSON object，`sort_order` 不得为负数。`content.id` 与 slug 一致由发布校验保证。

### `challenge_reward_rules`

版本化成功奖励：

- `reward_key` 在版本内唯一。
- `ending_type` 当前只能为 `good`。
- `ending_title` 必须与模型动作标题完全匹配。
- `conditions` 描述成功判定数和类型前缀。
- `calculation` 当前使用 `multiplier_bps`。
- `max_reward` 可选且不得为负数。
- `enabled` 控制规则是否可用于结算。

## 对话运行时

### `conversations`

保存所有者、标题、状态、生成锁、固定挑战版本、哈希和选中资产。

状态只允许 `active`、`ended`、`archived`、`deleted`。`rewinds_used_this_trial` 不得为负数。三个资产 ID 与 `challenge_version_id` 使用组合外键，不能引用其他版本资产。

`ended_at`、`archived_at`、`deleted_at` 用于表达状态发生时间，不依赖 GORM 的自动软删除机制。

### `chat_messages`

模型上下文使用的内部消息，role 限制为 system、user、assistant、tool、internal。`hidden` 控制是否进入正常历史，`parent_message_id` 支持编辑和分支关联。

### `display_messages`

玩家界面消息。它可以引用内部消息，也可以保存判定等系统展示事件。`metadata` 保存 roll request、roll outcome、模型、状态补丁和其他展示信息。

内部消息与展示消息分离，避免 UI Markdown、内部 system 文本和上下文历史互相污染。

### `conversation_states`

每个对话最多一行当前 JSONB 状态，主键就是 `conversation_id`。状态更新采用事务行锁。

### `state_revisions`

保存每次状态变化的 patch、before、after、来源和关联消息。来源只能为 `ai` 或 `system`。它是审计记录，不作为当前状态查询的主表。

### `memory_summaries`

一个对话可以有多个递增版本。`covered_until_message_id` 表示摘要覆盖到哪条内部消息，`token_count` 用于预算观察。运行时只读取最新版本。

### `context_build_logs`

记录一次上下文装配结果：挑战版本和哈希、模型、最大窗口、回复预留、已用 prompt token、段明细、纳入的消息 ID 和激活的世界书条目 ID。

日志不保存最终 LLM 密钥，也不作为对话内容权威来源。

## 判定与结算

### `system_settings`

单例表，`id` 固定为 1。保存管理员控制的判定开关和 easy/normal/hard 难度。初始化脚本创建默认行。

### `roll_history`

只保存结算和历史调整所需的最小信息：对话、判定类型、成功与否、时间。ID 默认使用 `gen_random_uuid()`。

判定流水由后端写入，没有普通用户写 API。索引支持按对话时间读取和按成功类型统计。

### `accounts`

每个用户一行账号统计：剩余回溯、累计获得、累计使用、总灵石、成功和失败次数。所有计数都不得为负数。

### `challenge_runs`

每个对话最多一条最终结算。字段包括结果、奖励键、奖励金额、计算明细和结算时间。

约束：

- result 只能为 good、bad、neutral。
- good 必须有 `reward_key`。
- bad/neutral 必须没有 `reward_key` 且奖励为 0。
- `conversation_id` 唯一。
- `(conversation_id, player_id, challenge_version_id)` 必须和原对话一致。

## 用户 LLM 设置

`llm_settings` 按用户隔离，保存 Base URL、API Key、模型和采样参数。API 响应不会返回 API Key 正文。

当前 API Key 在数据库中是可供后端调用上游模型的可逆明文，不是密码哈希。生产环境必须依赖 PostgreSQL 访问控制、备份保护和磁盘加密；未来如引入应用层主密钥，应设计独立密钥轮换方案。

## 索引策略

- 用户状态、角色、会话用户与过期时间均有索引。
- 对话按 `player_id, updated_at DESC` 支持首页列表。
- 消息、修订、摘要、上下文和判定均按 `conversation_id` 建索引。
- 挑战资产按 `challenge_version_id, kind, sort_order` 查询。
- 结算按玩家和版本支持统计。

新增索引前应以真实查询为依据，避免为低基数字段创建无效索引。

## 删除策略

- 用户删除会级联认证、对话和账号数据。
- 对话物理删除会级联消息、状态、摘要、上下文和判定。
- 已有 `challenge_run` 的对话使用 RESTRICT，避免删除奖励审计链。
- 挑战版本和资产使用 RESTRICT 或应用层不可变规则，防止已开始对话失去内容。
- 当前普通“删除对话”只是软删除，不触发外键级联。

## 事务边界

- 草稿整体替换：版本、资产、奖励规则同一事务。
- 发布：锁版本、校验、哈希、更新版本和挑战包同一事务。
- 状态更新：锁状态、写当前状态和修订同一事务。
- 结算：锁对话、更新账号、写 challenge_run、结束对话同一事务。

任何新增跨表写入都应明确事务归属，不能依赖调用方按正确顺序分别提交。
