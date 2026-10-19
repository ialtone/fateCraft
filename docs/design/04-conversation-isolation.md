# 04. 对话隔离与生命周期

## 模块行动和检查点

纯玩法模块的玩家行动与模型工具共用对话归属、运行租约、回合和分支保障。请求携带 `request_id`、`branch_epoch`、`expected_revision`，锁定后复核；重复请求重用已提交结果，旧分支/旧修订拒绝。

Campaign 每回合保存完整 `campaign_checkpoints`。改写/重新生成恢复领域表和模块状态，递增 Campaign revision 与对话 branch epoch；缺少检查点拒绝。账号余额、已领取奖励、种子与追加历史不回档，旧事件按来源 turn 有效性过滤。模型失败后的“继续叙事”保持已提交事实，不执行这类显式改写。细节见[24 玩法架构](24-gameplay-module-architecture.md)。

## 隔离目标

每个对话代表一个玩家在固定挑战版本下的一次独立运行。任何消息、状态、记忆、判定或结算都不能因为 UUID 可猜、前端伪造或仓储漏过滤而跨到其他用户或其他对话。

对话固定保存：

- `player_id`：所有者。
- `challenge_version_id`：创建时的发布版本。
- `challenge_hash`：该版本内容哈希。
- `character_asset_id`、`persona_asset_id`、`scenario_asset_id`：本局固定资产。

回合、生成 attempt、消息、展示消息、状态、状态修订、长期记忆、Memory job、角色询问、上下文日志、判定流水和结算流水都通过 `conversation_id` 归属对话。

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

返回 404 而不是 403，是为了避免泄漏某个 UUID 是否真实存在。消息、状态、角色询问、上下文和 WebSocket 在执行自身操作前，都必须直接或间接通过同一归属检查。Memory 没有普通玩家 API，由内部仓储始终按 conversation 工作。

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
- 结算会把状态设为 `ended`，写入 `ended_at`；正在运行的生成由生命周期事务标记为取消请求，待拥有该运行的 Worker 退出后才释放兼容状态。

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
- 每个 attempt 首次装配时校验并缓存固定 Bundle；同一 attempt 的后续步骤复用该不可变 Bundle，状态、Campaign Projection、世界书激活和工具快照仍按步骤读取最新权威数据。
- 版本状态不可用、记录哈希不匹配或对话哈希不匹配时停止生成。

这样可以避免进行中的挑战因管理员更新内容而改变规则，也能发现直接修改数据库 JSON 的行为。

## 并发控制

### Durable generation run

`conversation_generation_runs` 是生成所有权的唯一权威：每次可写生成先在锁定的 conversation 行内创建一个 `running` run，部分唯一索引保证同一对话最多一个 `running/cancel_requested` run。所有生成写入都携带 `run_id`，并在事务边界重新确认 run 仍为当前 active owner；旧任务、取消任务或生命周期已改变的任务不能继续写入消息、状态、Campaign 事实或释放新任务。

`conversations.is_processing` 仅保留为兼容性的派生忙碌指示，不参与所有权判断。完成、失败或取消只允许结束自己持有的 run。run 记录 `owner_instance_id` 与 `lease_expires_at`：生成期间持有方每 30s 心跳续期（租约 2 分钟有效），续期失败即中止本轮；新的生成请求遇到已过期的 run 会直接抢占。回收器在启动时及每 30s 把本实例遗留的、以及任何实例上已过期的 run 标记为 `abandoned/lease_expired`，再清理该派生指示；其他实例仍在心跳的 run 不受影响。

每个生成请求还保存客户端提供的 `request_id`、操作类型、目标消息和 payload hash。`request_id` 全局唯一：同一请求重试只返回原 run 的回放状态，不能再次调用模型、扣奖励或替换分支；同一 ID 改变操作、目标或正文会返回冲突。新的 `request_id` 才是一次新的业务重试，仍须重新通过 active、回合能力和奖励校验。

重新生成与编辑改写都在 durable run 已获取后执行原子分支事务。事务在 conversation 行锁内固定目标回合，校验当前回合和平台策略，消费平台统一的改写额度，恢复可信状态，递增 `branch_epoch`，失效旧 turn/message/memory，并创建替代 turn、attempt 和触发消息；任一步失败都回滚，旧分支仍可读。所有 Worker 写入同时校验 run、attempt、turn 和 branch epoch，旧任务即使晚退出也不能污染新分支。

结束、归档和软删除先在数据库事务中切换生命周期并标记运行取消，再由内存协调器取消 context、等待 Worker 退出。恢复 active 时如果仍有 active run 会返回冲突。

### 状态更新

状态补丁在事务中对 `conversation_states` 加行锁，读取当前值、应用白名单补丁、写入新状态和修订记录。这样两个并发状态更新不会基于同一旧值互相覆盖。

### 结算

结算事务对 `conversations` 加 `FOR UPDATE` 锁，并依赖 `challenge_runs.conversation_id` 唯一约束。即使模型或网络重复提交结局，也只有一笔可以更新账号并完成对话。

## WebSocket 隔离

WebSocket 握手先经过会话认证。生成器开始工作前再次读取并校验对话归属和 active 状态。Hub 的广播键是对话 UUID，只有已通过该对话归属检查的连接可以加入。

客户端断线不改变数据库事实；重新进入页面时应通过 REST 重新加载对话、消息和状态，不能假设事件一定完整送达。

## 删除语义

当前删除是软删除，只修改对话状态与 `deleted_at`。关联回合、消息、状态、Memory 和询问仍保留，用于一致性和后续审计。分支改写同样通过 `invalidated_at` 让旧分支退出有效历史，不物理删除证据。数据库外键的 `ON DELETE CASCADE` 只在运维明确物理删除根记录时生效。

## 安全回归清单

- 使用 A 用户 Cookie 请求 B 用户对话、消息、角色询问和上下文，均返回 404。
- 直接连接 B 用户 WebSocket 被拒绝。
- archived 对话可以恢复，ended 对话不能恢复。
- 同一 active 对话同时发送两个生成请求，只有一个获得 durable generation run；旧 run 结束不能清除新 run 的忙碌状态。
- 发布新版本不会改变旧对话的 Bundle。
- 修改已发布 JSON 后，旧对话和新建对话都不能继续使用损坏版本。
- 两个并发成功结局只产生一条结算流水。
## 公开分享隔离

`conversation_shares` 是私有对话之外唯一允许匿名读取的数据边界。创建分享时，后端在同一事务内锁定来源对话，验证所有权、空闲状态、当前有效分支和连续范围，再通过字段允许列表生成版本化 JSON 快照。公共接口只按签名 token 读取这一行，不 join 用户、对话、消息、状态、Memory 或 Campaign 表。

分享 token 是 `v1.<share_id>.<HMAC-SHA256>`，数据库只保存 share ID 和快照，不保存可直接访问的 bearer token。快照不包含来源对话 ID、用户 ID或原消息 ID；所有者列表中的内部定位字段仅由已认证接口返回。后续分支替换不会修改旧快照，重新发布会在同一事务中创建新快照并撤销旧链接。

对话进入 `deleted` 状态时，生命周期事务同步撤销其全部 active 分享，避免出现对话已删除但公开链接仍可读取的中间状态。账号物理删除由外键级联删除分享。ended/archived 不自动撤销已经主动发布的快照。

## LLM 路由固化与不匹配

对话与 Campaign 在创建时按当前有效路由（`platform_managed` 时恒为 `server`，否则为用户设置的 `llm_route`）固化 `llm_route`，不接受请求体传入；`conversations` 与 `campaigns` 各有 `BEFORE UPDATE` 触发器禁止修改，对话路由还必须等于所属 Campaign 路由（复合外键兜底）。

对象路由与当前有效路由不一致时禁止任何模型调用：REST 返回 409（`code=llm_route_mismatch`），WS 返回 `notice`，不创建 run/turn/attempt、不扣额度；生成中途改设置时当前回合按 `llm_route_mismatch` 失败。Memory Worker 遇到不匹配或 `client` 路由无 Relay 连接时把任务放回 pending（不计失败）；角色问答返回 409。


## 结局确认隔离

结局提议及结果绑定 conversation、branch_epoch 和 revision。玩家确认提交原修订，动作结果不明按原 request_id 重查。切换会话清除作用域和旧弹窗；恢复只读取当前有效分支，历史正文不制造终态。 完整协议见 [26 玩法结局生命周期](26-gameplay-ending-lifecycle.md)。

## 玩法包交换与版本管理

普通对话及 Campaign 固定原版本继续；恢复归档检查原哈希、运行策略和精确模块，不要求当前入口。发布不会自动归档，暂停只限制新事实，已提交结果可读。fork 为当前入口重新开局，不迁移正文或进度。

完整协议见 [27 玩法包交换与版本生命周期](27-gameplay-package-exchange-and-versions.md)。
