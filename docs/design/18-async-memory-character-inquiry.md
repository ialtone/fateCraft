# 18. 异步长期记忆与只读角色问答

## 模块快照

Inquiry 对 Campaign 调用独立 `InquirySnapshot`；纯模块使用 inquiry 受众，通用/修仙关系表玩法显式复用其公开投影。Challenge 由 `ProjectInquiry` 投影本次已经固定的可信状态，避免二次读取实时状态产生混合修订；不调用行动预览或执行。

玩家行动纳入有效 turn 和消息日志。Campaign 改写通过完整检查点恢复当前事实，历史事件保持追加但按有效分支过滤；Memory、问答和工具均不能把失效分支当作当前证据。详见[24](24-gameplay-module-architecture.md)。

## 定位

fateCraft 是可导入任意题材玩法的通用平台。长期记忆与角色问答属于平台基础设施，不能识别修仙、库存、功法或其他玩法专属字段。

系统包含两个隔离的 Agent：

- Memory Agent：后台异步归纳已经封口且离开保护窗口的历史，不阻塞当前叙事。
- Character Inquiry Agent：用户在左侧角色栏主动提问并等待回答，只读查询当前已提交资料。

二者都没有领域工具，不能写玩法状态、执行判定、提交 Campaign 动作、发放奖励或结束玩法。它们只使用服务商原生 JSON Schema 返回各自的确定性对象，不使用 Tool Call、`json_object` 或 Prompt-only JSON 备用模式。长期记忆是叙事连续性资料，不是权威事实。

## 核心不变量

1. 叙事请求不等待 Memory Agent。同步路径最多规划持久任务并发送非阻塞唤醒信号。
2. Memory 只处理 `completed` 且未失效的回合，永远不读取当前 pending 回合。
3. 每个 `generation_attempt` 开始时固定一个已提交 `memory_id`；同一 Agent Tool Loop 的全部模型步骤不会切换记忆。
4. 历史按完整回合进入或离开上下文，不能只保留一次判定或半段回复。
5. 记忆候选只有通过租约 fencing、分支纪元、链头、固定来源和 evidence 校验后才能提交。
6. Campaign 事件和动作提交保持只追加。regenerate/rewrite 必须恢复目标完整检查点，并失效旧分支；缺少检查点时拒绝，不能仅删除消息伪造事实回滚。
7. Character Inquiry 不获取生成锁，不把未完成的流式候选暴露给客户端，不进入主消息、Context Builder 或 Memory 来源。
8. 问答之外唯一允许的写入是专用 inquiry thread/message 和可选详细 LLM 请求日志。
9. Memory 和角色问答只读取已提交事件、通过校验的叙事和当前玩家快照；不得写 Campaign 领域表、覆盖 `ActionResult` 或触发动作重放。摘要与当前快照冲突时以快照为准，并记录告警。Memory 的来源应保留事件 ID 与生成 revision，便于定位版本错位。

## 回合与生成尝试

`conversation_turns` 是一次逻辑剧情单位，使用 conversation 内单调递增的 `turn_seq`。触发类型包括 opening、user_input、continue、rewrite、regenerate 和 admin_mutation；状态包括 pending、completed、failed 和 cancelled。

`generation_attempts` 记录一次主叙事实际生成尝试：

- `turn_id + attempt_no` 唯一；
- 保存开始时的 `branch_epoch`、`memory_id` 和记忆覆盖边界；
- 状态为 running、completed、partial、failed 或 cancelled；
- partial 表示已提交至少一个写工具但最终文本未完成，失败保存阶段和稳定错误码。

消息、展示消息、状态修订、判定、Campaign 事件和动作提交都关联 turn/attempt。`chat_messages.message_seq` 由数据库原子分配；有效历史统一过滤 `invalidated_at IS NULL`。

进程启动时会清理遗留生成锁，把残留 `received` invocation 标记为 abandoned，并按是否已有写工具提交把 pending turn/running attempt 封口为 partial 或 failed。已 committed 的工具不会回滚或重放。

## Memory 版本

`conversation_memories` 保存不可原地编辑的版本：

- previous memory、version 和 conversation 当前链头；
- 来源 branch epoch、覆盖回合与消息边界；
- 严格结构化 `content` 和稳定 `rendered_text`；
- schema、Prompt、renderer 与 tokenizer 版本；
- source hash、生成参数指纹、模型和 Token 数；
- 分支改写后的失效时间与原因。

记忆成功不会删除原消息。`conversations.current_memory_id` 只指向当前有效链头；attempt 读取的是自己固定的 memory，不是调用时动态查询到的新链头。

## Memory 输出契约

Memory Agent 只允许返回一个 JSON 对象：

```json
{
  "synopsis": "已覆盖历史的简短梗概",
  "facts": [{ "text": "已发生事实", "evidence_refs": [{ "kind": "message", "id": "uuid" }] }],
  "entities": [{ "name": "实体", "description": "仍需记住的信息", "evidence_refs": [{ "kind": "campaign_event", "id": "uuid" }] }],
  "goals": [{ "text": "仍在推进的目标", "evidence_refs": [{ "kind": "message", "id": "uuid" }] }],
  "open_threads": [{ "text": "未解决线索", "evidence_refs": [{ "kind": "message", "id": "uuid" }] }],
  "player_choices": [{ "text": "关键选择", "evidence_refs": [{ "kind": "message", "id": "uuid" }] }]
}
```

六个字段全部必填，数组可以为空。evidence kind 仅允许 `message`、`state_revision`、`roll` 和 `campaign_event`。

服务端递归拒绝重复 JSON 字段，并拒绝对象外文字、未知或缺失字段、错误类型、超限文本/数组/总字节、未知 evidence、重复 evidence 和超 Token 渲染结果。协议错误不会触发同次模型纠错调用。

Renderer 使用固定顺序和 JSON 字符串编码自然语言，`memory_policy` 与模型生成的 `memory_summary` 是两个独立 required system 段。当前数值、位置、任务进度、动作权限与奖励继续以 State 或 Campaign Projection 为准。

## 调度、租约与提交

Planner 保留最近 4 个完整封口回合，并在未覆盖来源达到约 4000 估算 Token 时创建任务。单个任务按完整回合限制在约 4500 来源 Token；上下文已经发生未覆盖省略时会强制轻量规划一次。

`memory_jobs` 状态机：

```text
pending -> running -> succeeded
                   -> pending    暂时错误，指数退避并释放租约
                   -> failed     协议/配置确定错误或达到上限
                   -> cancelled  分支或来源失效
```

每个 conversation 最多一个 pending/running 任务。pending 可以合并更远目标，running 输入不可改变。Worker 使用 `FOR UPDATE SKIP LOCKED` 领取任务，写入 worker ID、lease token 和过期时间；长调用定期续租，旧 fencing token 不能提交。

Worker 使用进程根 context 和独立串行执行器，不继承 HTTP/WS 请求取消，也不修改 `is_processing`。新任务通过容量为 1 的 channel 低延迟唤醒，数据库和周期扫描仍是事实源。服务退出时停止领取并取消未提交模型调用；租约到期后其他实例可恢复。

最终事务锁定 job 与 conversation，并校验：

- lease token 与 worker ID；
- branch epoch 和 previous memory 链头；
- target、source hash 和 generation fingerprint；
- 来源 turn/message/revision/roll 仍有效；
- Campaign evidence 仍存在且可见。

提交成功后插入一个 Memory 版本、更新 conversation 链头、完成 job，并继续规划剩余分块。

## 失败策略

- 429、暂时性 5xx、连接失败和超时：后台指数退避，最多 4 次执行。
- 协议错误、未知 evidence、输出截断：直接失败，不做模型纠错重试。
- 配置或密钥暂时不可用：后台退避；不会复制密钥到任务。
- 来源、分支、链头或租约变化：丢弃候选，不推进覆盖边界。
- 数据库失败：不创建可见半成品，不影响当前叙事。

普通玩家只在上下文检查器看到“等待记忆归档”或“正在执行记忆归档”，不展示内部失败原因；公开接口包含 job ID 和执行次数供同步与检查器使用，也不提供失败 Toast 或重试按钮。状态由 WebSocket 即时通知，并以只读 activity 接口在刷新和重连后恢复。Worker 在调用模型前预分配候选 Memory ID；管理员开启详细模型日志后，可用 `purpose=memory`、memory job ID、候选 memory ID 和原始上游响应排查。成功提交沿用该 ID，失败候选不会生成 Memory 版本。

## Context Builder

必选优先级为平台规则、Runtime 规则、当前权威状态/Projection、当前 pending 回合、固定记忆策略与正文。之后从新到旧按完整回合加入近期历史，再消费 Character、Persona、Scenario、世界书和 style 等可选段预算。

若 required 总量溢出，构建直接失败且不调用模型。若更早未覆盖回合因预算省略，`context_build_logs` 写入：

- `memory_lag=true`；
- attempt 固定的 memory ID 与覆盖边界；
- `omitted_turn_from` / `omitted_turn_until`；
- 已存在或新建的 memory job ID。

省略不等于覆盖。Memory 完成只影响之后新建的 attempt。

## Character Inquiry 快照

每次问题创建新的 snapshot ID，并固定 conversation、player、branch epoch 和当前已提交 revision。普通 Challenge 使用 Runtime 投影后的玩家状态、当前固定版本中显式公开的玩法规则与最近已封口叙事；Campaign 再加入玩家 Snapshot 和玩家可见事件。

资料被规范成 `character`、`rules`、`objectives`、`activities`、`events`、`location`、`relationships` 和 `resources` 等通用段，使用请求内 `source-N` 标识。世界书不会整体暴露：只有 Manifest 当前引用、条目启用且设置 `metadata.inquiry_visible=true` 的内容才能成为 `rules` 来源。权威状态和公开规则先消费预算；Campaign Projection 内再按规则、角色、目标、活动、地点、关系和资源的顺序加入，近期叙事与事件最后加入。单段和总 Token 都有上限。模型看不到其他世界书条目、internal/model-only 状态或事件、随机种子、管理员配置、技术 Prompt、动作 Handler 或其他玩家数据。Inquiry 不直接读取 Memory 正文，因为 Memory 可以合法引用 model-only 资料，不能把叙事侧可见性提升为玩家可见性。

Inquiry Snapshot 的 conversation、State、已封口消息、Campaign 事件和 revision 仍在只读事务内实时读取。事务完成且归属校验通过后，显式公开规则通过 `challenge.Service` 读取同一固定版本的编译世界书缓存；规则不可变，因此不需要进入动态快照事务。仓储层不再维护独立的 Inquiry 规则缓存，避免两套容量、淘汰和解析行为漂移。

如果 Campaign Projection 构建期间 branch epoch 或 revision 改变，该投影会被丢弃，避免混合两个版本；响应标记 `stale=true`。回答结束时再次读取二者，期间发生变化也只标记过时，不自动再次调用模型。

## Inquiry 输出契约

```json
{
  "answer": "基于当前已提交资料的回答",
  "source_ids": ["source-1"],
  "cannot_answer": false
}
```

三个字段全部必填。服务端拒绝重复/未知字段、对象外文字、超长回答、未知或重复 source ID；`cannot_answer=false` 时至少引用一个本次快照来源。动作型自然语言请求只能解释，不能转发给领域服务。

`character_inquiry_threads` 按 conversation/player 唯一，`character_inquiry_messages` 只追加成功、取消和脱敏失败记录。assistant 只保存通用类别与玩家可读来源标签，不保存内部 key，也不复制快照 payload。模型只读取最近固定条数且不超过 2000 估算 Token 的独立历史；清空记录不改变 branch epoch、状态或记忆。

问答使用独立的 4 槽非排队并发额度，同一用户和同一 conversation 同时最多一条，并限制每个用户每分钟 12 个问题。超限只返回 409 或 429，不占用主叙事生成锁。前端左栏使用“状态 / 询问”切换，桌面与移动端共享独立历史、发送/取消状态、来源标签、过时提示和清空操作；问答失败不影响主叙事输入。

## API 与模型配置

```text
GET    /api/conversations/:id/inquiries
POST   /api/conversations/:id/inquiries   {"question":"..."}
POST   /api/conversations/:id/inquiries/cancel
DELETE /api/conversations/:id/inquiries
```

旧的普通摘要生成/编辑 API 已删除。管理员也不能直接 PATCH/DELETE 内部消息；历史改写必须走受 Runtime capability、奖励消费和 Campaign 副作用检查约束的分支服务。

LLM 调用统一经过 `llmsettings.Service` 解析。管理员可选择 `user_managed` 或 `platform_managed`；平台模式下 Memory 与 Inquiry 也使用 `system_settings` 公共配置，用户原配置只读保留。有效设置包含统一的 `protocol`、叙事 `model`、可空的 `memory_model` 与 `inquiry_model`，后两者为空时继承叙事模型。Memory 始终使用低温非流式严格调用；Inquiry 的上游传输模式跟随 `stream_enabled`，但无论是否流式都必须在服务端收齐并校验完整 JSON 后才落库和返回。API Key 仅在服务端单次调用内存中解密，日志带来源和修订号但不含密钥。

Memory 使用 `memory_archive_v1`，Inquiry 使用 `character_inquiry_v1`，两者各自传递原生 JSON Schema 并执行 payload validator。生产请求不会依赖预先能力探测，也不会在失败后切换协议或输出模式。

设置页允许用户分别点击 narrative、memory 或 character_inquiry 的“检测”。Memory/Inquiry 检测只对当前保存的对应模型发出一次小型 JSON Schema 请求，结果同步返回且不入库；失败只说明本次诊断结论，不改变生产请求。通用模型协议见 [19-agent-tool-loop.md](19-agent-tool-loop.md)。

## 客户端直连下的执行条件

- 路由闸门：问答 `Ask` 在对象路由 ≠ 有效路由时返回 409（不消耗限流额度、不写消息）；Memory Worker 遇到不匹配时把任务放回 pending（不计失败）。
- `client` 路由的 Memory 任务只在有 Relay 连接时领取执行（`HasActive`）；领取后执行中连接丢失按暂时性网络失败重试，不计入契约失败。全部关闭页面后任务保持 pending，重新打开（Relay 重连）后继续执行。
- 问答在 `client` 路由且无 Relay 连接时同步返回 `llm_relay_unavailable`（409）。

## 维护入口

| 责任 | 代码 |
| --- | --- |
| Memory 契约、来源、Worker 与调度 | `backend/internal/memory` |
| Turn/attempt、Memory job 与 CAS | `backend/internal/storage/repo_turn.go`、`repo_memory.go` |
| 完整回合预算与 lag | `backend/internal/contextbuilder`、`orchestration` |
| Inquiry 快照、契约与服务 | `backend/internal/inquiry`、`storage/repo_inquiry.go` |
| Inquiry HTTP | `backend/internal/api/inquiries.go` |
| 左栏 UI | `frontend/src/components/CharacterSidebar.vue`、`InquiryPanel.vue` |
| 权威表结构 | `backend/migrations/*.sql`，由 `cmd/migrate` 执行 |

修改这些边界时至少运行 `go test ./...`、`go vet ./...` 和 `vue-tsc --noEmit`；发布前再执行项目约定的生产构建。PostgreSQL 集成测试使用随机隔离 schema，设置 `FATECRAFT_TEST_DATABASE_URL` 后执行；测试结束会删除该 schema。


活动通知统一由 memory_jobs 稳定字段的提交触发，覆盖 schedule、目标合并、claim、release、retry、cancel 与终态。旧 ActivitySink 回调已移除。租约续期与 updated_at 不递增活动版本；完整 activity 推送携带公开任务元数据，客户端不为排队/执行 phase 查询 inspector，归档完成且检查器可见时才刷新归档信息。
