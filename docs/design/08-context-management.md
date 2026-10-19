# 08. 上下文管理设计

## 模块受众与投影预算

纯模块分别生成 player/model/inquiry 投影。Challenge 模型上下文不重复塞入缓存的玩家 `gameplay`；使用实时 model 投影参与单独预算与世界书扫描。大对象优先保留可容纳的标量，再尝试完整集合，省略路径写入 `projection_control.omitted_fields`。未展示的库存或列表不能被解释为事实为空；内部种子与随机证据不参与投影。

问答使用 inquiry 投影，来自固定请求快照，不能预览或执行行动。模块自身输出上限与内容要求见[24](24-gameplay-module-architecture.md)。

## 目标

Context Builder 在模型窗口内保留平台契约、当前权威状态、当前回合、已提交长期记忆和尽可能多的近期完整历史。它只读取当前 conversation 与固定 ChallengeVersion，不把 Memory 当作状态或权限来源。

异步长期记忆、任务租约和角色问答的完整规则见 `18-async-memory-character-inquiry.md`。

## 预算

```text
prompt_budget = max_context_tokens - max_response_tokens - tool_definition_tokens - 256
world_budget  = prompt_budget * 0.22
campaign_projection_target = min(prompt_budget * 0.50, 32768, required 后的剩余预算)
```

固定 256 Token 是传输与格式安全余量。当前 tokenizer 是统一的启发式估算器；消息的 `content_blocks`（含 thinking/replay/tool call）、Memory、Campaign Projection、工具定义和世界书使用同一版本。只有消息仍是相同的单一文本块且持久化 `tokenizer_version` 与当前版本一致时，Context Builder 才复用消息上的 Token 计数；多块内容、thinking/replay/tool block 或版本不匹配都会重新计数。Campaign 的 50% 是优先保留近期回合的目标上限，最小权威核心放不下时可以使用 required 后的全部剩余预算。

新设置默认使用 `128000` 上下文和 `16384` 最大输出 Token。用户仍应按实际上游模型修改；平台不根据模型名称猜测窗口。设置接口允许上下文 4096 到 2000000、回复至少 128，且强制 `max_response_tokens + 256 < max_context_tokens`。低窗口是受支持配置，不能依赖提高默认值掩盖无界投影。

`prompt_budget <= 0` 或无法压缩的 required 总量溢出时直接返回 `ErrRequiredOverflow`，不调用模型。错误包含各 required 段和当前回合的 Token 明细。

## 固定快照

每个 `generation_attempt` 创建时保存：

- branch epoch；
- 当前已提交 `memory_id`；
- Memory 覆盖到的 turn sequence。

Agent Tool Loop 的全部模型步骤复用同一 attempt 和记忆。每个步骤会重新读取已提交状态和 Campaign Projection，但后台 Worker 即使在步骤之间完成，也只影响之后新建的 attempt。

进程级 Challenge 缓存与 attempt 缓存职责不同：前者跨用户复用已发布版本的 Manifest、Runtime、Prompt、世界书和玩法定义编译结果；后者只在当前 attempt 固定 Bundle 和首次扫描得到的世界书激活集合。State、历史、固定 Memory、Campaign Projection、revision 和工具快照不会进入版本缓存，仍按各自一致性规则读取。

## 候选来源

- 已发布 Prompt 的 enabled 分段；
- Character、Persona、Scenario；
- 当前权威 State；
- Campaign 模式的有界 Projection；
- attempt 固定的 `memory_policy` 和 `memory_summary`；
- 当前 pending 回合与未被 Memory 覆盖的有效封口回合；
- Responses 思考模型产生的协议 reasoning item（用于下一次请求回放；可读文本另投影为玩家可折叠查看的 reasoning）；
- 由固定记忆、近期原文和当前状态激活的世界书；
- 服务端 Agent 输出契约、当前步骤工具定义与可选 runtime directive。

玩家输入始终按低信任数据处理。服务端会在输出契约中固定声明该边界；当前输入命中提示注入或越权表达时，额外追加位于全部历史之后的 `security_notice` required 段。检测不改写或阻断玩家文本，也不代替工具权限和状态校验。

Prompt 发布必须保证 `system_master`、`settlement_security` 和 `agent_tool_loop` 等核心段存在。服务端 Agent 契约不依赖玩法包，并作为历史之后的最后一条 system 消息发送；工具定义通过 Provider 原生字段传输，不伪装成 Prompt JSON。

## 消费顺序

1. 统计平台规则、Runtime 规则、State、runtime directive、Agent 契约、工具定义、Memory policy/body 等固定 required 内容。
2. 把当前 pending turn 的全部有效内部消息作为 required 历史预留。
3. Challenge 模块与 Campaign 将内部动作能力转换为工具快照，提取有界只读限制说明后移除执行目录；关键事实作为独立 required 段预留，再按剩余预算构建有界 Projection。
4. 从新到旧尝试加入封口历史；一个 turn 的所有消息必须同时加入或同时省略。
5. 用剩余预算加入 Character/Persona/Scenario、世界书、style 和其他可选 Prompt 段。
6. 按语义顺序组装 system 段、时间正序历史，再附加最新关键事实、输出契约和（如命中）安全提示。

当前回合不能因普通预算不足被裁掉。任何一组 required 内容放不下都属于配置错误，不能静默生成。旧历史遇到第一组放不下的完整 turn 后停止向前填充，确保保留的是连续近期窗口。

## Memory 覆盖与滞后

只有 `message_seq` 大于固定 Memory 覆盖边界的消息会成为原文候选。Memory 不存在时从第一条有效历史开始。

预算压力可能暂时省略尚未被 Memory 覆盖的早期 turn。此时允许继续叙事，但构建结果和 `context_build_logs` 必须记录：

- `memory_lag=true`；
- memory ID 与覆盖边界；
- `omitted_turn_from` 和 `omitted_turn_until`；
- 已存在或刚规划的 memory job ID。

Context Builder 还会在构建日志的 `segments` 中追加 `history_compaction` 元数据段，标记省略原因和 turn 范围。该段不注入模型消息、不改变领域存档，只把历史裁剪变成可审计的上下文决策。当前回合、必选玩法事实和未完成工具链仍属于 required 内容，无法容纳时直接返回 `ErrRequiredOverflow`。

省略范围不能冒充 Memory 已覆盖范围。出现 lag 时同步路径只执行有界元数据查询、创建/合并任务和非阻塞唤醒，不读取完整来源或等待 Worker。

## State 与 Campaign Projection

`<current_state>` 和 `<campaign_projection>` 是 required 段，但 Campaign Projection 不是不可拆的大 JSON。`id`、`status`、`current_location_key`、`game_time`、`revision`、`state_hash`、`narrative_facts` 和 `narrative_context` 是平台最小核心；玩法模块返回的顶层主题在此核心上分层装入。内部 `action_capabilities` 构建原生工具和只读限制说明，不作为执行目录进入该正文。

`narrative_context` 绑定本轮生成使用的 Campaign 快照：`revision_from`、`revision_to`、`state_hash` 和最多 4 个尚未被正式叙事消费的 `ActionResult`。每个 ActionResult 包含动作 ID、`goal_status`、有界 `changes`、`current_facts`、`preserved` 和断言约束。历史正文只提供语气与连续性；状态变化只能来自 `changes`，`preserved` 中的字段不得写成恢复、清除或提升，`details_partial=true` 时省略字段不能补具体数值。该段与 `state_hash` 一起属于 required 核心，压缩时不得丢弃。

模块声明的 `narrative_facts` 单独提取为 required `<narrative_facts>` 消息，放在历史之后并携带当前 revision/status 等头部；从较大的 Projection 删除同名核心，避免重复传输。每个模块最多声明 20 条投影路径，事实对象最多 4 KiB，动作反馈另可补充运行头部。值为零、false 与缺失严格区分，缺失表示 `unavailable`。最小事实放不下返回 `ErrRequiredOverflow`，不能为了保留长背包或旧正文而裁掉当前境界等核心。

`<action_restrictions>` 使用现有 required runtime directive，正文最多 4 KiB；近期工具尝试优先保留，其他说明按稳定顺序装入，省略数量明确标记。它只解释限制，不重复工具参数、不扩大目录，也不增加模型请求。工具定义、限制说明、关键事实和当前回合工具链都计入真实 token 预算。

`ProjectionProvider.Context` 返回 `ProjectionView.Data + ProjectionView.Priority`。Priority 只列顶层主题 key，由具体玩法模块按当前决策价值排序；平台不理解修仙、推理、经营等题材字段。未声明的主题按稳定 key 顺序作为低优先级候选。

完整投影能放入目标预算时原样发送。需要压缩时：

- 玩法主题按 Provider Priority 整段加入，不截断主题内部 JSON；
- `projection_control.partial=true` 告诉模型省略表示“本轮上下文不可见”，不表示权威集合为空；模型只能调用当前步骤实际提供的工具；
- Context Build Log 在 `campaign_projection` 段记录 `original_tokens`、`compacted` 和 `omitted_keys`，不保存被省略正文。

只有平台最小核心和压缩控制信息仍无法放入 required 剩余预算时才停止生成。玩法 Provider 自身仍必须维持集合与字节上限，Token 压缩不是接收无界数据的理由。

Memory 只补充人物、关系、承诺、目标与已发生剧情。当前数值、位置、资源、任务状态、动作能力、判定和奖励始终以 State、Campaign Projection 和服务端流水为准。

## 世界书

世界书扫描使用：

- 最近最多 6 条未覆盖原文；
- 当前 State 中递归提取的字符串；
- 固定 Memory 的 rendered text。

Worker 不扫描世界书。世界书只在正常 Context Build 时按固定挑战版本激活，详细算法见 `09-worldbook-memory.md`。

## 构建日志

每次正常生成记录 conversation、trigger message、turn/attempt、branch epoch、固定 Memory、模型窗口、回复预留、实际使用 Token、候选段、纳入消息 ID、世界书 entry ID、Memory lag/省略范围和任务 ID。

同一 Agent attempt 内，已通过内容哈希校验的固定 Challenge Bundle 只装配一次；Bundle 的版本级组成来自进程级编译缓存。每一步会并行读取 State、历史和需要的固定 Memory，Campaign Projection 与工具快照仍按依赖顺序重新构建；世界书只在 attempt 首步扫描，随后固定激活集合。这样减少跨对话和工具循环中的固定内容查询与解析，又不牺牲权威状态的新鲜度。

日志不保存 API Key、认证头或会话令牌。模型摘要始终记录，只有详细 LLM 正文采集由管理员开关独立控制。上下文是短期诊断，不属于奖励等可靠业务审计；按时间/对话/attempt 游标查询，详见[日志、诊断与配置审计](23-logging-and-diagnostics.md)。

普通玩家接口只投影模型、Token 占用、纳入历史/世界书数量和 Memory lag，不返回 Prompt 分段、消息 ID、世界书内部 ID 或裁剪原因。完整构建日志仅通过管理员诊断接口读取；玩家侧上下文检查器另外展示主叙事步骤、题材无关的工具类别和 Memory 排队/运行状态，但不暴露工具参数、后台任务 ID、失败详情或重试入口。

## 失败规则

- 固定版本或内容哈希不一致：停止生成。
- 固定 required、Campaign 最小核心或当前 turn 放不下：停止生成，并返回分段 Token 明细。
- 可选段超预算：整段跳过并记录 `skip_reason=budget`。
- Context 日志写入失败：记录应用日志，不改变已构造请求。
- Memory Worker 失败：保持最后成功链头，不中断当前叙事。


## 终局事实与目录

有界 `ending_capabilities` 只提供玩家终局预览，不授予模型执行权。终局提示作为必选 RuntimeDirective 放到历史之后，和当前事实一起约束旧正文；预算不足不调用模型。提议、拒绝、真实提交必须区分，正文不被用作结局检测器。 完整协议见 [26 玩法结局生命周期](26-gameplay-ending-lifecycle.md)。
