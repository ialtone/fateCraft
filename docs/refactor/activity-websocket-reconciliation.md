# WebSocket 活动推送与 /activity 对账重构

> 实现记录：2026-10-19。代码和功能测试已落地；同配置 CPU / 查询量 A/B 压测尚未完成，因此保留本文作为验收记录。

## 1. 范围与结论

WebSocket 负责状态变化推送，GET /api/conversations/:id/activity 保留为初次加载、断线和漏事件恢复的权威活动快照。健康空闲页面不再每 5 秒查询数据库。

依据 Beta 压测报告制定。报告没有 CPU 火焰图和 /activity 独立 CPU 占比，不能据此承诺容量翻倍。本次不改变业务写入、模型调用、扣费和玩家动作的重试语义。

项目为新项目，所有表、索引和函数直接定义在 backend/migrations/00001_init.sql；不新增迁移，不做历史数据回填。

## 2. 服务端实现

### 统一活动投影

storage.ActivityRepo.Snapshot 统一供 REST、WebSocket 和 runtime inspector 使用。稳定字段通过单条 SQL 读取，版本与数据处于同一 PostgreSQL 语句快照。使用 lateral 最新记录和 EXISTS 消息存在性判断，保留原有继续、重新生成和 client 路由零成本语义。00001 增加 chat_messages(conversation_id, turn_id, role) WHERE invalidated_at IS NULL 的部分索引，对应能力投影的查询条件；本轮未测量执行计划或读写成本，不承诺具体收益。

快照包含：

- activity_version、conversation_id、branch_epoch、conversation_status、is_processing；
- turn_id、attempt_id、turn_status、can_continue、can_regenerate、regenerate_cost_quantity；
- memory 和最新 memory_job 的公开 ID、状态、目标回合、执行次数及时间；
- narrative、step_no、tool_category：独立的尽力恢复 phase，不承诺属于稳定版本。

REST 必须验证玩家身份和对话所有权，内部广播才允许省略 owner。is_processing=false 时 narrative 固定 idle；生成中的 phase 额外查询，且仅拼接同一 attempt 的结果。归档正文、worker、租约和原始错误不公开。

### 版本表与提交后通知

采用独立 conversation_activity_versions 表，保存 conversation_id、version、transaction_id，而非在 conversations 上直接累加版本。原因是 memory job 与分支替换业务已有不同加锁顺序；把版本写入放在独立行并延迟至提交阶段，避免引入 job → conversation 与 conversation → job 的锁顺序冲突。

00001 中的 deferred constraint triggers 覆盖 conversations、conversation_turns、generation_attempts、chat_messages、memory_jobs。相关稳定字段变化时，版本和业务写入在同一事务提交；同一事务对同一对话最多递增一次，回滚不递增、不通知。

- 覆盖创建回合、attempt 收尾、生成锁获取/释放、分支替换、结束/恢复，以及 memory 排队、claim、重试、取消和终态；后台恢复也走相同数据库约束。
- UPDATE 使用独立触发器的 WHEN ROW(...) IS DISTINCT FROM ROW(...)，只比较指定稳定字段，不再将 OLD/NEW 整行反复转换为 JSONB。正文、续租、step_count 等无关变化不会进入延迟触发队列；INSERT/DELETE 单独注册，稳定变更仍在提交阶段递增版本。
- 关联记录的 INSERT/DELETE 以及相关稳定列变化采用保守失效；即使不是最终投影的最新记录也可能递增，版本不是内容哈希，也不是消息计数。
- 同事务改后又改回仍可能递增；不要把版本相等之外的推断用于判断业务幂等。

事务内 pg_notify 在 COMMIT 后才交付，通道按 schema 隔离。realtime.StartActivityNotifications 使用 lib/pq 的独立 LISTEN 连接，约 25ms 合并同一对话的变化，只为本实例存在订阅者的对话读取一次最新完整快照并广播。使用读取到的真实版本，不把旧提交版本套在新字段上。

删除旧 memory ActivitySink 回调链，避免两套活动通知。没有变更时不运行活动查询；LISTEN 的 30 秒 Ping 是连接保活。LISTEN 断开或重连产生缺口时关闭现有 WS 订阅，迫使重新 ready + REST 恢复；单个快照读取失败关闭该对话的订阅。

### 协议

连接鉴权并注册进 hub 后发送 subscription.ready。客户端等待 ready 才声明连接就绪，5 秒未确认则关闭并进入断线恢复。新项目不保留“没有 ready 也算健康”的旧服务端路径。重连按 1→2→4→8→16→30 秒退避，只有收到当前对话的 ready 才重置；TCP/WS onopen 本身不重置，避免订阅注册失败时每秒重连。

activity_updated 发送 conversation_id、activity_version、branch_epoch、reason=stable_state_changed 和完整 snapshot。根 envelope 的 branch_epoch=0 可省略，snapshot 内始终存在。完整快照允许跨版本直接应用，不因为跳号自动请求 /activity。

客户端也支持 refresh_required=true 的版本化失效通知：提高目标版本并单飞对账；当前发布器发送完整快照。没有版本的通知仅作为失效提示，不直接写状态。

agent 事件继续按 attempt_id + sequence 排序，合并 delta 使用 sequence_start。phase 不增加稳定版本，不触发 inspector。已有业务事件并非全部携带活动版本：message 使用 ID/attempt，Campaign 使用 revision/hash；无版本的 conversation_ended 触发权威对账。

## 3. 客户端实现

### 首屏、恢复和缓冲

ConversationView 管理进入、面板可见性和离开；socket store 负责统一恢复。先订阅并缓冲，ready 后并行读取 activity 和必要业务快照，随后合并缓冲事件。对话定位/鉴权读取可以提前。

业务恢复覆盖 conversation、chat、state、guidance、ending state、runtime、rewards，以及关联 Campaign。活动快照不能代替这些资源。缓冲限制为 2048 条、约 2MiB 和单次恢复 15 秒；超限关闭连接重新恢复。

消息按 ID 幂等合并。当前 attempt 的持久消息即使在终态快照之后到达也能保留；旧 attempt 事件被过滤。缓冲 state_update 没有可比较的版本，避免回滚 REST 状态，标记补读。跨 endpoint 的 REST 不构成一个数据库事务，Campaign 继续验证 revision/hash。

### 单飞与错误

context store 维护活动请求代际、在途请求和最高目标版本。REST 和 inspector 内嵌 activity 均经同一版本/branch/attempt 合并入口。读取期间的新失效目标不能被旧响应清除；更高版本 WS 完整快照可满足目标而免去补读。每轮最多两次即时读取，其余退避。

socket store 合并活动和业务恢复，业务失败即使 activity 成功仍保留重试标记。读取失败保留最后成功状态并标记 stale，不把未知状态改成 idle。断线、切换及卸载使旧请求代际失效，并清理定时器、缓冲和监听。

### 触发节奏

| 场景 | 行为 |
| --- | --- |
| 健康连接且空闲 | 不做 activity / inspector 周期请求 |
| 首屏、重连、页面重新可见或 online | 合并业务恢复 |
| 更高版本完整 activity | 直接应用；分支、回合、结束或生成解锁等变化按需补业务资源 |
| 更高版本失效通知 | 单飞 activity 对账至目标版本 |
| agent sequence 缺口 | 暂停该增量并恢复业务快照 |
| WS 不可用且页面可见/在线 | 5 → 10 → 30 秒退避，带抖动 |
| 已知生成或 memory queued/running 无进展 | 75 秒后核验，重复无进展退避至 5 分钟；完成后停止 |
| 页面隐藏或离线 | 暂停兜底请求，恢复时对账 |
| 不可恢复的 HTTP 错误（如 400/401/403/404/410） | 初次读取失败不建立 WS；恢复中遇到则停止自动恢复。仅网络错误、408、429、5xx 自动重试 |

仅接受的有效任务事件更新进展时间，重复事件与连接心跳不延后 watchdog。权威 is_processing=false 可以清除遗留 streaming placeholder 并解锁输入，恢复过程不自动重发写动作。

### 检查器

检查器首次实际显示时按需加载；隐藏时只标记 dirty。可见时在回合终态、分支替换、memory 归档完成等低频边界合并刷新。memory 排队/执行展示直接用 activity；agent phase 不触发检查器数据库查询。

## 4. 交付边界

PostgreSQL NOTIFY 与事务提交绑定，避免应用提交后、回调前崩溃造成的活动通知窗口，但它不是 durable outbox。监听连接断开后没有历史重放，依靠关闭订阅及 REST 快照恢复。

Hub、生成队列和其他业务事件仍在进程内；即便 PG 活动通知可跨连接交付，也不能据此宣称整个系统已支持多实例。当前按单实例 Beta 验收。

已知任务漏掉 terminal 可由 watchdog 恢复；健康空闲且无后续事件、可见性变化或重连的页面，不能保证发现静默丢失的最后一条业务事件。若要求这种场景有确定恢复时限，应另行实现应用层版本心跳及可靠事件日志。普通 ping/pong 不具备该保证。

## 5. 验证与待完成项

索引精简后，活动查询只保留消息存在性索引 `idx_chat_messages_active_turn_role` 和最新 memory job 索引 `idx_activity_latest_memory_job`。最新有效回合复用 `(conversation_id, turn_seq)` 唯一索引并过滤失效行，移除专用部分索引及多余的 id 排序。大量连续失效回合可能增加扫描量，此取舍未经性能测试；全项目另清理 9 个重复或当前查询不需要的普通索引，依据见 [数据库设计：索引精简](../design/07-database-postgresql.md#索引精简)。

已增加真实 PostgreSQL 集成测试：基线初始化/不可逆约束、事务内一次递增、回滚、phase/续租不递增、所有权、能力投影一致性、memory 终态、提交后推送及无变化不推送、ready 和订阅失效。

前端活动专项测试覆盖 20 个场景：模拟 10 分钟空闲零请求、单飞目标补读、WS 满足目标、旧 phase/响应过滤、失败保留状态、隐藏检查器、缓冲消息、漏终态解锁、phase 零 REST、根 branch=0、业务失败重试、终态交错消息、重复事件、ready 超时、重连检查器恢复、订阅失败退避及其上限、错误对话 ready 和永久读取错误停止恢复。模拟时间测试不等同真实浏览器容量测试。

本轮按用户要求不运行性能测试、EXPLAIN 性能对比或 A/B。若后续评估容量，应在 Beta 相同 2 核 / 2.5GiB、禁用 swap 环境做 A/B：保持模拟 LLM、历史和思考时间一致，覆盖 1/50/100 空闲页面与 10/50/100 持续用户、检查器可见/隐藏及重连风暴，比较请求数、SQL、CPU、P95 和新增触发器/锁成本。压测客户端必须使用新协议。未执行前不宣称 CPU 降幅或容量提升。

正式协议和运行边界已同步至 design/05、06、07、10、18、19、23、26、28。完成上述性能验收后可归档本文。

前轮验证：后端 go test -p 2 ./... -count=1 全量通过（使用独立本机 PostgreSQL 测试实例，强制启用数据库集成测试）；前端 45 项测试全部通过，vue-tsc 与 Vite 生产构建通过。未操作项目实际数据库。

本轮边界优化验证：独立 PostgreSQL 的迁移、存储与 realtime 功能测试通过，涵盖稳定字段 NULL 转换、正文更新不递增、事务递增和提交后通知；前端 49 项测试及 vue-tsc/Vite 生产构建通过。容量仍按 Beta 约 100 名同时活跃用户保守规划，不将 225 作为已验证容量。

索引精简验证：在独立本机 PostgreSQL 上强制启用数据库集成测试，`go test -p 2 ./... -count=1 -timeout=10m` 全量通过，`go vet ./...` 通过。SQL 差异检查确认仅删除 10 个普通索引，约束定义不变。本轮未运行性能测试，未操作项目实际数据库。
