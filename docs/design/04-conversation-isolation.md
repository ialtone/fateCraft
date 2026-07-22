# 04. 对话隔离与生命周期

## 隔离目标

每个对话代表一个玩家在固定挑战版本下的一次独立运行。任何消息、状态、记忆、判定或结算都不能因为 UUID 可猜、前端伪造或仓储漏过滤而跨到其他用户或其他对话。

对话固定保存：

- `player_id`：所有者。
- `challenge_version_id`：创建时的发布版本。
- `challenge_hash`：该版本内容哈希。
- `character_asset_id`、`persona_asset_id`、`scenario_asset_id`：本局固定资产。

消息、展示消息、状态、状态修订、摘要、上下文日志、判定流水和结算流水都通过 `conversation_id` 归属对话。

## 身份传播

1. `requireAuth` 从 HttpOnly Cookie 读取会话令牌。
2. 认证服务只用令牌哈希查询 `auth_sessions`。
3. 中间件将用户对象放入 Gin context，并把用户 ID 写入 request context。
4. 对话、设置和账号领域服务通过 `identity.Provider` 获取当前用户，不信任请求体中的玩家 ID。

普通用户不能通过提交 `player_id` 切换身份。结算动作中的玩家 ID也来自已认证 context，并在事务中再次和对话所有者比较。

## 读取隔离

`conversation.Service.GetOwned` 是对话侧的基础归属检查：

1. 按 UUID 读取对话。
2. 比较 `conversation.player_id` 和当前身份。
3. 不匹配时返回 `storage.ErrNotFound`。

返回 404 而不是 403，是为了避免泄漏某个 UUID 是否真实存在。消息、摘要、状态、上下文和 WebSocket 在执行自身操作前，都必须直接或间接通过同一归属检查。

列表查询必须在 SQL 中带 `player_id` 条件，不能先读取全量再由前端过滤。

## 状态机

```text
           archive             restore
active  ------------> archived ---------> active
  |
  | manual end or settlement
  v
ended

active/ended/archived -- soft delete --> deleted
```

约束：

- 只有 `active` 对话允许生成、编辑、重新生成、续写和归档。
- `archived` 只由未结束的 active 对话产生，因此可以恢复。
- `ended` 是终态，不允许归档后再恢复。
- `deleted` 默认不出现在列表中，不提供普通恢复流程。
- 结算会把状态设为 `ended`，写入 `ended_at` 并释放生成锁。

手动结束不等于挑战成功，不会写入 `challenge_run` 或发放奖励。

## 新建对话

创建流程必须由后端完成选择解析：

1. 按 `challenge_slug` 查 active 挑战包及当前发布版本。
2. 重算已发布版本内容哈希并与记录比较。
3. 校验 `persona_slug` 位于允许集合并属于该版本。
4. 从 Manifest 固定 Character 和 Scenario。
5. 写入对话的版本、哈希和资产 ID。
6. 初始化默认状态。
7. 根据 Character 开场白写入首条消息或自动生成触发消息。

前端不能提交版本 ID、哈希和技术资产 ID。数据库组合外键还会保证对话选中资产来自同一挑战版本。

## 挑战版本隔离

新对话总是使用挑战包当前发布版本。旧对话不跟随 `current_version_id`：

- 管理员发布 `1.1.0` 后，已存在的 `1.0.0` 对话仍加载 `1.0.0`。
- 运行时允许加载 published 或 retired 的固定版本。
- 每次装配 Bundle 都重算 Manifest、资产和奖励规则哈希。
- 版本状态不可用、记录哈希不匹配或对话哈希不匹配时停止生成。

这样可以避免进行中的挑战因管理员更新内容而改变规则，也能发现直接修改数据库 JSON 的行为。

## 并发控制

### 生成锁

`conversations.is_processing` 是对话级生成锁。抢锁使用带条件的原子 UPDATE，只有从 `false` 改为 `true` 的请求可以开始生成。完成、失败或取消后释放锁；服务启动时还会清理进程异常退出留下的锁。

生成锁只解决同一对话重复生成，不替代所有权与 active 状态检查。

### 状态更新

状态补丁在事务中对 `conversation_states` 加行锁，读取当前值、应用白名单补丁、写入新状态和修订记录。这样两个并发状态更新不会基于同一旧值互相覆盖。

### 结算

结算事务对 `conversations` 加 `FOR UPDATE` 锁，并依赖 `challenge_runs.conversation_id` 唯一约束。即使模型或网络重复提交结局，也只有一笔可以更新账号并完成对话。

## WebSocket 隔离

WebSocket 握手先经过会话认证。生成器开始工作前再次读取并校验对话归属和 active 状态。Hub 的广播键是对话 UUID，只有已通过该对话归属检查的连接可以加入。

客户端断线不改变数据库事实；重新进入页面时应通过 REST 重新加载对话、消息和状态，不能假设事件一定完整送达。

## 删除语义

当前删除是软删除，只修改对话状态与 `deleted_at`。关联消息和状态仍保留，用于一致性和后续审计。数据库外键的 `ON DELETE CASCADE` 只在未来执行物理删除时生效。

## 安全回归清单

- 使用 A 用户 Cookie 请求 B 用户对话、消息、摘要和上下文，均返回 404。
- 直接连接 B 用户 WebSocket 被拒绝。
- archived 对话可以恢复，ended 对话不能恢复。
- 同一 active 对话同时发送两个生成请求，只有一个获得生成锁。
- 发布新版本不会改变旧对话的 Bundle。
- 修改已发布 JSON 后，旧对话和新建对话都不能继续使用损坏版本。
- 两个并发成功结局只产生一条结算流水。
