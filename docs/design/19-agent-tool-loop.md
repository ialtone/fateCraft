# 19. Agent Tool Loop 与模型协议

## 可信模块工具边界

纯模块动作共用现有步骤快照和工具事务，model 只收到允许主体与当前方案。内核负责参数、资源与新状态复核；事务适配器保存状态、内部随机证据、事件与提交结果。玩家专属终局不会因为模型自由文本声明确认而执行。

Campaign 模型动作结束存档后，当前相同 attempt/turn/revision 的提交保留收尾叙事权限；旧尝试、旧修订和仅持有 run 的工作不具备该权限。显式改写使用完整检查点，模型传输失败和 partial 续写不回档已提交动作。架构见[24](24-gameplay-module-architecture.md)。

本文是 fateCraft 主叙事 Agent 的统一传输、协调、事务和恢复规范。设计与题材无关，适用于导入任何符合 Runtime Contract 的挑战或长期玩法。

百年飞升劫的可信数值通过模块行动更新，普通状态工具仅操作声明为 model 的叙事字段；结局按模块终态结算。青河阶段进度属于当前分支的可回档玩法状态，结算回执与账本在终局事务提交后不回档；当前分支的任务证据仍需重新满足。模型步骤失败或余额刷新失败都不能触发重复发放。规则与验收见 [14](14-ascension-trial-gameplay.md)、[21](21-campaign-outcome-rewards.md) 和 [25](25-core-gameplay-validation.md)。

## Agent 边界

平台包含三个职责隔离的模型 Agent：

| Agent | 输出 | 权限 | 执行方式 |
| --- | --- | --- | --- |
| 主叙事 Agent | 普通文本或一个/多个受控 Tool Call | 仅能调用当前步骤开放的工具 | 前台回合内执行；写工具按返回顺序提交，文本可流式 |
| Memory Agent | 原生 JSON Schema 对象 | 只写版本化叙事记忆，不写玩法事实 | 回合完成后后台异步任务 |
| 角色问答 Agent | 原生 JSON Schema 对象 | 只读固定快照，不写对话或玩法状态 | 独立请求，不触发主叙事 |

主叙事不使用整回合 JSON 信封。Memory 和角色问答仍需要结构化对象，因为它们的消费方是确定性数据服务而不是玩家时间线。

## 生成操作与分支围栏

普通发送、重新生成、续写和编辑改写共享一个生成操作协议。客户端为每次意图生成 `request_id`；服务端把操作类型、目标消息和 payload hash 写入 durable generation run。同一 request ID 的 HTTP/WS 重试只回放已有 run，不重复模型调用、奖励消费或工具副作用；改变正文、目标或操作类型会被拒绝。

重新生成和编辑改写先在 conversation 行锁内完成目标回合解析与平台策略校验，再在同一事务中消费 `message_rewrite` 平台额度、递增 `branch_epoch`、失效旧分支并创建替代 turn/attempt/触发消息。重新生成 completed 与 partial 回合都消耗额度（两者都已提交可信事实）；failed/cancelled 回合没有提交任何写工具，重试免费。替代 attempt 没有提交任何写工具就失败或取消时返还额度；一旦提交过写工具（partial）则保留扣费，避免“取消 → 重新生成”变成免费重掷判定。生成 Worker 的每次消息、状态、工具和结算写入都必须同时通过 run、attempt、turn 和 branch epoch fencing；对话状态必须为 active，唯一例外是本 attempt 自己完成挑战结算后对话变为 ended，此时该 attempt 仍可写入工具结果和收尾叙事。`branch.replaced` 只在事务提交后广播；前端在收到它或服务端快照前不得自行改动时间线。这样即使旧模型请求迟到、断线重连或进程重启，也不会把旧分支事实写入当前历史。

REST 重试若与仍在运行的原请求使用同一 `request_id`，接口返回 `202 {"status":"replayed"}`，结果继续经 WebSocket 推送；只有不同的新请求才返回 409。

生成写围栏在同一业务事务内先锁 conversation，再联合校验并锁定 run、attempt、turn；使用 FOR NO KEY UPDATE，保留写入互斥并兼容模型诊断外键的 KEY SHARE。诊断写入遇到繁忙父行须回滚后有限重试，避免持有部分父行锁再阻塞业务。完整锁协议与并发验证见[日志、诊断与配置审计](23-logging-and-diagnostics.md#模型诊断写入与业务行锁)。

## 玩家输入信任边界

玩家消息始终作为低信任剧情数据传入模型，不能升级为 system/developer 指令。服务端输出契约会在完整历史之后再次声明这一点；当前玩家消息命中提示注入、越权或提示词泄露表达时，平台只追加一条不含原文的短安全提示，不删除、改写或拒绝玩家内容。

这套检测是信号而不是授权系统：它不改变工具目录、不直接封禁玩家，也不授予任何状态写入权限。工具名称、参数、状态、奖励、随机结果和领域事实仍必须由服务端快照与事务校验决定。规则匹配使用 Unicode 规范化、大小写折叠和不可见分隔符消除，日志或诊断只可使用风险类别、命中数量和原文哈希，不能记录原始敏感输入。

## Provider 适配层

`internal/llm` 同时支持 OpenAI-compatible Chat Completions 和 Responses。业务层不读取 Provider 原始 JSON，而只接收：

```go
type AgentEvent struct {
    Kind AgentEventKind
    Text string
    CallID string
    Name string
    Arguments string
    FinishReason string
}
```

两种协议的文本增量、拒答、工具开始、参数增量、工具完成和步骤结束都会归一化。Chat 工具历史转换为 assistant `tool_calls` 与 role=tool；Responses 转换为 `function_call` 与 `function_call_output`。

Responses 流式工具调用按稳定 `call_id`、输出位置及 item ID 别名关联到唯一累积条目。同一输出位置的同一 `call_id` 在完成事件改变 item ID 时，仍只产生一次工具调用；早期缺少 call ID 的片段先按位置和 item ID 关联，后续补齐，不能用 item ID 或位置伪造执行 ID。不同输出位置重复使用同一 call ID、同一位置更换 call ID、名称或最终参数冲突均拒绝。参数增量累积，完成事件的完整参数只核对和补齐，不重复追加；完整 JSON 可忽略空白和对象字段顺序差异，不能忽略值变化。

Chat Completions 按 `tool_calls[].index` 汇总，各调用的参数增量独立累积。重复发送相同完整 ID/名称不会重复拼接；同一 index 的 ID 或名称改变拒绝，不覆盖早先身份。两种协议的最终批次仍校验调用 ID、工具名及重复 ID，不靠静默去重放行异常，也不因解析错误重放已执行工具。协议冲突对玩家返回具体说明，管理日志保留 `tool_call_conflict`。

思考模型的 reasoning 同时有两条用途：`content_blocks` 中的原始 replay 用于下一次请求回放，可读文本投影用于玩家查看。Responses 适配器保留完整 `type=reasoning` item（包括 DeepSeek 的 `reasoning_text`）及 function call 的原始 item ID；Chat Completions 适配器把 reasoning 增量归一化为 thinking block。下一次请求只由当前协议 Adapter 解释 replay：Responses 原样回传 reasoning item，Chat 转换为 `reasoning_content`。`chat_messages.content_blocks` 是唯一持久化来源；展示接口只从中提取可读 reasoning 文本，忽略 `encrypted_content` 等 provider 原始字段，且 thinking/replay token 会计入上下文预算。不能把 reasoning 文本拼进普通 `content` 或工具参数，也不能用吞掉 400 的方式代替回放。

每次 Agent 请求在协议序列化前都经过 `PrepareToolReplay` 纯整理器。它按历史顺序校验 assistant 工具调用与 tool 结果的 `call_id` 配对：缺少结果的调用补一个明确的失败结果，空结果补非空错误文本，无前置调用的孤立结果丢弃；同一条未完成调用链中的重复 ID、参数冲突或空 ID直接在本地阻止请求。同一模型步骤只保存一条包含完整批次的 assistant 声明，其后紧接每个工具的独立结果；不创建空 assistant 占位行。不同已完成回合可以继续使用供应商生成的同名短 ID，避免破坏兼容网关。整理器不访问数据库、不执行工具、不改变领域事实。这样即使模型步骤中断、超时、取消、进程重启或历史被异常截断，也不会把孤立 `function_call` 发送给 Responses API。Chat Completions 与 Responses 共用同一整理结果，前者投影为 `tool_calls`/`role=tool`，后者投影为 `function_call`/`function_call_output`，两端的配对 ID 始终保持一致；Chat 的 `tool_call_id` 只出现在 role=tool 的结果中，assistant 不序列化存储用的首调用关联 ID。修复数量和类型写入结构化日志；不可修复历史使用 `tool_replay_invalid_history` 错误码，不重试同一请求。

提供工具时省略可选的 `tool_choice`，使用 OpenAI 协议规定的默认 `auto` 语义，使生产调用与显式诊断保持同一种请求形态，并兼容会拒绝显式 `tool_choice` 的思考模式网关。Chat Completions 和 Responses 都设置 `parallel_tool_calls=false`，但兼容网关可能忽略该参数；服务端必须保留自己的批次校验和执行策略，不能只依赖供应商限制。

## 服务端出口快照与重试

`server` 路由的 Agent 请求使用 `internal/llmoutbound` 管理的不可变出口快照。每次逻辑模型调用在第一次传输尝试前取得一份快照，`doRaw` 的全部有限重试共用它；owner 在调用中途保存新代理设置不会让同一次调用换出口。Agent 的后续模型步骤属于新的逻辑调用，会取得最新快照，因此多步骤回合可以跨代理切换继续，但不重放已提交工具。

- 停用时 Transport 显式直连并忽略环境代理；启用时只使用保存的 HTTP/HTTPS 代理，不因 `NO_PROXY` 绕过，也不在失败后回退直连。
- 代理认证拒绝、明确 TLS 证书错误和配置错误不重试；连接失败、超时和 429/502/503/504 仍使用原有有限退避策略，不会无限重试或自动重放生成。
- 保留响应头等待、流式空闲看门狗、正文上限、调用方 ctx 与取消语义；模型列表的 10 秒专用超时不套在长叙事流上。
- `client` 路由仍由浏览器 Relay 执行，不读取服务端快照。底层出站错误统一归类为固定安全代码，日志不记录代理地址、凭据或 CONNECT 细节。

## 输出通道解析

部分兼容服务商会在同一步同时返回文本和工具。模型层按完整归一化结果决定通道，协调器则把实时展示和可信持久化分开：

```text
normalized result contains ToolCall -> tool lane
normalized result has text/refusal   -> text lane
otherwise                            -> empty/error
```

- 同一步存在任意 Tool Call 时优先进入 tool lane；伴随文本只作为中间输出，不进入玩家时间线。只有没有工具调用且存在文本/拒答时才进入 text lane。
- 流式正文增量在尚未确认通道时作为临时预览立即通过 `agent.text.delta` 发送；预览只存在客户端和本步骤内存缓冲，不进入数据库。若随后观察到工具调用，协调器发送一次 `agent.text.reset` 清空预览，再执行工具；工具步骤后续的中间正文不再发送。
- text lane 确认后，已经发送的预览直接对应最终文本；只有经过非空、UTF-8、重复文本等校验后才保存普通 assistant 文本。若流式上游没有发送正文增量，则在最终确认时补发一次；非流式请求仍只发送一次。
- 零文本、零拒答、零工具为空响应；多个工具进入受控工具批次。工具包含权威写操作与只暂存建议的 `fc_present_guidance`，按模型返回顺序逐个执行并各自经过幂等事务；未声明安全策略的工具不会被并发执行。

EOF、超时、取消、`finish_reason=length` 或 Responses `max_output_tokens` 都不会把临时文本保存为最终消息。

## 协调器

一个逻辑 attempt 最多执行 8 个模型步骤：

1. 读取固定挑战版本、Runtime、可信状态、记忆和历史；Campaign 模式还构建当前 Projection。
2. 从 Runtime 和 Projection 生成该步骤的工具快照。
3. 将工具定义 token 纳入必选上下文预算，保存 context build 日志。
4. 固定 attempt 的工具目录版本与 runtime hash，保存 `agent_steps`。
5. 调用模型一次并锁定输出通道。
6. text lane 校验并保存最终文本；tool lane 执行一个或多个工具，把整批调用声明与每个结果按稳定顺序加入历史，再回到步骤 1。

同一模型步骤不因解析、工具或结构化契约错误而内部重放，不换模型、不切协议、不降低输出约束。响应体交付前的连接错误、429 和网关临时错误由传输层按有限策略重试；一旦开始解析正文或工具调用，就不会重放整个 Agent 步骤。工具拒绝是正常业务结果，可以进入下一个模型步骤。一个 attempt 最多成功提交 6 个写工具；后续写调用在执行前返回 `mutating_tool_limit`。

### 多工具批次

供应商可能在忽略 `parallel_tool_calls=false` 时返回多个工具调用。协调器先校验每个调用的稳定 `call_id`，拒绝重复或非法 ID；通过校验后不把多个写调用交给并发事务，而是按响应顺序逐个进入现有 Claim、领域校验、短事务提交和结果消息流程。这样既兼容供应商的并行响应，也保持状态修订、Campaign revision、随机判定和结算的顺序与幂等语义。`fc_complete_challenge` 提交成功后，同一批次中排在其后的写工具不再执行；后续模型步骤不提供任何工具（包括行动提示），只写结局收尾叙事。

工具快照中的 `execution_mode` 是执行策略边界。当前内置状态、判定、结算和 Campaign 工具固定为 `serial`；未来只有明确只读、无副作用且声明稳定读集的工具才可以使用 `parallel_read`，并行结果仍须按原始调用顺序写回历史。

## 工具快照

每个 `agent_steps` 记录完整工具定义、快照哈希、Handler key/version 和 Campaign revision。工具名必须匹配稳定小写标识；状态、判定、结算和 Campaign 动作都通过同一快照鉴权。

Challenge 与 Campaign 的 `action_capabilities` 用于构建原生工具目录，只有当前内容启用且对 model 主体可用的动作进入 `fc_action_*`。移除内部能力目录前保留只读限制说明；参数 Schema 与可执行选项只通过原生工具传输，不在 Projection 重复。

Provider Tool Schema 用于提高参数正确率，Handler 始终重新验证名称、JSON、大小、权限、State Schema、revision、资源和领域前置。

## 可信事实与动作叙事反馈

每个模型步骤重新读取 model 投影。模块声明的关键事实通过必选 `<narrative_facts>` 放在历史之后、平台输出契约之前；当前境界、修为、资源身份与终局事实优先于历史正文、记忆、世界书和玩家说法，普通状态问答也使用这一优先级。

领域动作在原有事务内构建 `narrative_feedback`，与状态及工具结果一起提交。反馈包含版本、action key、执行前后 revision、`execution=committed`、当前关键事实及有界 `changes`；只有领域结果明确返回 `success` 时才写 `goal_succeeded`。提交成功不等于目标成功，失败结算的材料、时间和伤势变化仍然保留。工具结果超出 16 KiB 时省略大事件等详情，保留最多 8 KiB 的事实反馈及 revision。

纯模块根据执行前后受众投影生成反馈；Campaign 在同一事务中读取实际领域变化和动作后头部，反馈保存在原有动作提交 JSON 中供幂等重放。共享结果仅包含 player 与 model 均可见且值一致的事实，不包含种子、随机证据或任一受众独占的信息。按稳定 key/id/name 比较的集合只列变化项；省略字段不视为事实删除。`details_partial`、有界省略路径和数量明确说明反馈不完整，不能把未知解释成空或零。

不可用原因通过必选 runtime directive 中的 `<action_restrictions>` 提供，正文最多 4 KiB，近期尝试动作优先，并记录省略数量。这些说明不注册工具、不授予权限。调用可识别但规则不可用的动作返回 `action_unavailable`；动作可用却因步骤目录限制未提供返回 `action_not_provided`；真正未知的名称返回 `unknown_tool`。同批前序动作使 revision 变化时，旧原因改为 `action_snapshot_stale`，下一步重新读取状态。拒绝说明只针对本调用，不能否认本回合此前已提交的其他动作。

普通生成、继续叙事、重新生成和终局收尾共用上述装配。继续叙事不重放动作，重新生成依照既有检查点与分支协议使用恢复后的状态。Chat Completions 和 Responses 共享同一反馈与权限边界。

最终文本落库前执行确定性事实门禁：L0 检查空文本、UTF-8、长度和内部标记；L1 检查可映射的境界/阶段、气血、法力、神识、寿元、库存和关系主张。校验只使用当前 `narrative_context`，不根据历史正文反推状态。检测到矛盾时先执行一次有界确定性修复：删除包含违规主张的句子并重新校验；仍失败则写入后端模板说明，标记 `validation_status=fallback`。修复或兜底都不重放工具、不回滚已提交动作，也不额外扣改写额度。当前不调用第二个语义审核模型，无法映射的文学表达仍可能出错；门禁的目标是阻止已知权威事实被正文直接反驳。

叙事落库事务重新锁定 Campaign，比较 `revision` 与 `state_hash`。生成期间状态已推进时返回 `narrative_stale`，不写 `display_messages`、不扣改写额度；已有工具动作保留，界面提示基于最新快照继续。成功落库的叙事同时写 `display_message_campaign_actions`，绑定本回合全部来源动作。用户消息、动作回执和普通非 Campaign 消息不进入该绑定。

## 调用幂等与事务

完整 Tool Call 先写入 `agent_tool_invocations(status=received)`。唯一键是 `attempt_id + call_id`，参数哈希防止同一 ID 被重用于不同调用。

成功路径在一个短 PostgreSQL 事务中提交：

```text
领域事实
  + invocation(status=committed)
  + assistant tool_call chat message（本步骤仅第一项写完整批次）
  + tool_result chat message
  + generation_attempts.committed_mutating_tool_count
```

Handler 返回 `rejected` 或 `failed` 时使用内部哨兵错误回滚整个领域事务，再单独保存终态 invocation 和有界公开结果。事务提交本身失败会记录 `tool_transaction_failed`；不会广播领域事件或增加写工具计数。若失败结果本身也无法保存，返回 `tool_persistence_failed`，日志保留原事务与后续保存错误。

所有数据库提交完成后才广播 `state_update`、`campaign_updated`、判定消息或 `challenge_completed`。

后续工具只追加自己的结果，仓储要求同一 conversation/attempt/step 已有唯一声明，并核对 call_id、工具名和原始参数哈希。缺少声明、重复声明或参数不一致会回滚该工具；每个 invocation 和其结果仍各自唯一。数据库原有非空 tool_call 内容约束保持不变，消息仓储也在写入前拒绝空声明。

2026-10-09 已在临时本地 PostgreSQL 17.6（完整 00001）中先复现旧版第二工具触发 `ck_chat_message_tool_shape`，再验证修复。`agent_batch_postgres_test.go` 覆盖人物状态 + 行动建议、重复请求、连续写入、业务拒绝、结果保存故障回滚、中途停止和参数篡改；通过真实上下文构建及本地 HTTP 模型替身检查 Chat/Responses 回放与最终叙事保存。没有对业务库进行测试写入或调用付费模型。

## Attempt 状态与恢复

| 状态 | 含义 |
| --- | --- |
| `completed` | 最终普通文本已保存 |
| `partial` | 至少一个写工具已提交，但最终文本未完成 |
| `failed` | 没有可信副作用，模型或协调器失败 |
| `cancelled` | 没有可信副作用且请求被取消 |

租约回收器（启动时及每 30s）会封口所属对话已无活动租约的 running attempt，并把对应 `received` invocation 标记为 `abandoned/generation_lost`；新请求抢占已过期租约时，也会在同一事务里按同样规则封口该对话遗留的回合，保证抢占后立即可以开始新回合。生成 goroutine 内的 panic 由 Hub 兜住：记录堆栈、只封存本实例持有的该对话租约与回合、向客户端发送 `error`，进程本身继续运行。已经 committed 的领域事实不会回滚或重复执行。用户对 partial attempt 使用“继续叙事”时只允许文本续写，依据数据库中的可信事实收尾。

`GET /api/conversations/:id/activity` 从持久行恢复 `preparing/model/tool`；流式字符本身不持久化。用户点击“继续”会创建一条普通用户消息，因此实时和刷新后的玩家时间线一致。WebSocket 连接会在服务端按同一 attempt/step/message/type 合并相邻 reasoning/text delta（约 40ms 或 4KB flush），控制与终态事件进入有限队列；队列满时立即记录告警并关闭慢连接，不在模型回调中等待。重连时前端重新拉取消息、状态、Campaign、活动和最近上下文，丢失的临时预览不恢复，以持久化快照为准。

## Memory 与角色问答

两个只读/后台 Agent 使用 `CompleteStructured` 和原生 `json_schema`：

- Chat Completions 使用 `response_format.type=json_schema`；
- Responses 使用 `text.format.type=json_schema`；
- 只接受单个完整 JSON 对象，拒绝工具调用、对象外内容、重复 key、空响应和截断；
- 还要执行各自的业务 payload validator。

它们不降级到 `json_object`、Prompt-only JSON 或固定提交工具。Memory 的持久任务可以按任务策略处理暂时性网络失败；响应体交付前的临时传输错误可由统一传输层有限重试，但解析、结构化契约或工具结果失败不会重放该模型步骤。

## 显式模型诊断

平台不维护能力评分、漂移窗口、cooldown 或 half-open，也不让探测结果改变生产请求。设置页只在用户点击时执行一次无副作用诊断：

- `narrative`：提供 `fc_diagnostic_echo` 并用指令要求调用，回送 tool result，再验证模型能返回普通文本；诊断请求不额外强制 `tool_choice`；
- `memory`：验证当前记忆模型的原生 JSON Schema；
- `character_inquiry`：验证当前问答模型的原生 JSON Schema。

能力属于已保存的 Base URL、协议与模型组合，不能只根据模型名称推断。结果只返回当前请求，不入能力表：完整闭环成功为 `supported`；上游明确拒绝当前能力为 `unsupported`；模型单次未遵循探针或返回了无法归因的 400/422 为 `inconclusive`；网络、鉴权、超时或服务异常为 `unavailable`。这些结论不是运行期熔断器，也不会触发降级或重试。

## 审计

每次逻辑模型调用默认提交轻量摘要。详细正文默认关闭，开启后保存预算内的请求 JSON/预览、原始响应和归一化文本；字段含 trace、设置来源、契约及可用 usage。传输重试复用 llm_call_id，最终摘要保存尝试次数；业务 attempt 不变。

诊断队列最多 128 条、32 MiB，包含正在写入的条目；满载立即丢弃并累计原因，不重放模型或工具。正常关闭先停止 HTTP、WebSocket 和后台任务生产者，再限时排空 writer。

编排器的 `performance` 结构化日志额外记录每个 Agent 步骤的上下文装配、模型请求、首个事件、首段文本、首个工具和总耗时，以及工具提交耗时；首步上下文装配会并行读取互不依赖的固定包、State、历史和 Memory，Campaign/世界书仍按依赖顺序处理。这些数据用于区分模型 TTFT、工具事务和后端重复读取，不进入玩家正文。

API Key、Authorization、URL userinfo/query/fragment 不得入库。工具领域审计由 `agent_steps`、`agent_tool_invocations`、chat tool messages、状态修订、Campaign 事件和奖励账本承担。

## 实时事件

主叙事只发送 `agent.*`：

- `agent.turn.started`
- `agent.step.started`
- `agent.tool.started`
- `agent.tool.finished`
- `agent.reasoning.delta`
- `agent.text.delta`
- `agent.text.reset`
- `agent.turn.completed/partial/failed/cancelled`
- `agent.queue.updated`

模型请求遵循统一流生命周期：`started -> delta* -> completed`，异常分支为 `error`、`aborted`、`truncated` 或 `unexpected_eof`。Chat 的 `[DONE]`、Responses 的 `response.completed/response.incomplete/response.failed` 以及等价兼容终止标记必须被明确确认；提前 EOF、坏 SSE JSON 或缺少终止事件都会使该步骤失败。传输层只允许在响应体交付前对明确临时错误重试，解析开始后不自动重放。

`agent.text.delta` 的 payload 包含临时消息 ID、正文片段和 `preview=true`；同键相邻 delta 合并时还会携带被覆盖范围的 `sequence_start`，而事件公共 `sequence` 为该范围末序号。`agent.text.reset` 包含临时消息 ID 与撤销原因（当前为 `tool_call`）。reset 只清空未持久化的临时正文，不清空 reasoning，不结束回合，也不删除已持久化消息。事件带随机 `event_id`、`turn_id`、`attempt_id`、`step_no`、attempt 内递增 `sequence` 和时间；合并后的 delta 使用新的 `event_id`。前端按 event ID 去重、按序号范围防倒序；发现真实间隙时拉取持久快照。

## 验证矩阵

- Chat/Responses × stream/non-stream × final text/tool call。
- Chat 的 `reasoning_content` 与 Responses 的 reasoning item 在非流式/流式解析和 Adapter 回放中保持协议对应；数据库统一保存为 `content_blocks`，可读 reasoning 增量通过 `agent.reasoning.delta` 发送，不能把原始协议 item 直接发送到玩家层。
- 文本先于工具、工具先于文本、非流式混合和多工具。
- 空响应、EOF、超时、取消、429、5xx、400 和截断。
- 工具参数分片、大小上限、未知工具、重复 call ID 和参数哈希冲突。
- 工具回放缺失结果、空结果、孤立结果、重复 ID、参数冲突和不可修复历史；修复不得触发工具执行。
- committed 工具与领域事实同事务；rejected/failed 回滚领域写入后仍保存公开结果。
- 写工具后模型失败进入 partial；进程重启不会重放 committed 工具。
- 工具快照哈希、目录版本、Runtime hash 和 Campaign revision 保持稳定。

关键实现：

| 责任 | 代码 |
| --- | --- |
| Provider Profile 与协议选择 | `backend/internal/llm/profile.go`、`backend/internal/llm/protocol_adapter.go` |
| HTTP/SSE 与审计传输 | `backend/internal/llm/transport.go` |
| 服务端出口快照与代理 Transport | `backend/internal/llmoutbound` |
| 代理配置保存与发布 | `backend/internal/llmproxy` |
| Agent 协议归一化 | `backend/internal/llm/agent.go` |
| JSON Schema 传输 | `backend/internal/llm/structured.go` |
| 主协调器 | `backend/internal/orchestration/agent_pipeline.go` |
| 工具目录与 Handler | `backend/internal/orchestration/agent_tools.go` |
| 工具事务 | `backend/internal/orchestration/agent_transaction.go` |
| Agent 持久化 | `backend/internal/storage/repo_agent.go` |
| 显式诊断 | `backend/internal/modelcapability/service.go` |

### 动态行动提示

主叙事 Agent 通过 `fc_present_guidance` 提交可选的玩家意图提示。该工具只接受 2 到 4 个短标题和意图文本，不执行领域动作，也不允许模型提交 action key 或前端参数。它仅在玩家缺少方向、面临重要决策分叉或主动索要建议时开放；普通回合不提供该工具。服务端校验后把提示快照附着在最终 assistant display message 上，并通过 `guidance.updated` 推送。提示与消息分支、state fingerprint 和 Campaign revision 绑定，状态变化、回合开始、失败或分支替换都会使其失效。点击时客户端只提交 guidance ID 与 item key，服务端重新校验快照后把受信任的意图文本作为普通用户输入创建新回合，不能信任客户端 label 或 message。

前端动态提示不渲染在消息正文内，而由会话底部固定的 `ConversationGuidanceBar` 展示在滚动内容区与输入框之间。没有 active guidance 时组件完全移除；正文和思考仍按原流式链路显示。Runtime 中声明的长期玩法固定 guidance 仍由 Presentation 渲染，和主叙事动态提示使用同一按钮组件但拥有独立生命周期。

## 客户端直连传输切点

传输层切点在 `llm` 包 `doRaw` 的 HTTP 层（`transport.go` 的 `roundTrip`）：`Params.Relay != nil` 时把已构建的请求交给浏览器执行并返回原始响应，之后的 SSE 解析、大小上限、空闲超时、审计与重试策略全部原样生效，解析与校验逻辑零分叉。回传的必须是原始响应字节，服务端不信任浏览器做任何归一化。Relay 实现见 `internal/llmrelay`，协议与错误码见 [10-api-websocket.md](10-api-websocket.md)。等价性由 `llm` 包的直连/Relay 对照测试保证（同 fixture 下结果、事件序列与审计一致）。

### 工具快照与字段所有权

每个 Agent 工具快照都绑定 Runtime 编译出的 Ownership Registry hash。Campaign 模式只生成 `narrative.*` conversation 字段的 `fc_state_*` 工具；Campaign 权威字段不会出现在状态工具目录，必须通过对应的 `fc_action_*`。服务端执行时再次解析 Registry，未知路径或跨域路径统一返回 `state_path_forbidden`，拒绝状态、revision 和领域事件副作用。该校验不能只依赖模型收到的 JSON Schema，因为旧步骤、兼容网关或伪造调用都可能绕过目录。

性能日志的正常模型步骤/工具耗时为 Debug，模型达到 60 秒、工具达到 1 秒记 Warn，失败和回合摘要保留。脱敏、正文状态和容量细则见[日志、诊断与配置审计](23-logging-and-diagnostics.md)。


## 终局提议

`fc_propose_ending` 读取玩家终局预览，在原工具事务中校验后记录待确认提议；不执行玩家动作。`ending.proposed` 通知前端读取确认入口。当前终局目录与事实约束在历史之后；真实结束才进入既有有限收尾授权。 完整协议见 [26 玩法结局生命周期](26-gameplay-ending-lifecycle.md)。


活动稳定版本与 agent sequence 分离：纯 phase、step_count 和 delta 不递增版本，也不触发 REST/inspector 查询。turn/attempt 终态与生成锁释放若分两次事务，各自推送；终态不代表 is_processing 已释放。客户端只用通过去重和 sequence 校验的事件更新进展，已知生成 75 秒无进展后核验权威快照，漏终态时仍可关闭流式占位。REST phase 不覆盖读取期间已收到的同 attempt 新 phase。
