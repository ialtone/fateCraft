# 08. 上下文管理设计

## 目标

上下文构建器要在模型窗口内提供完整规则、当前状态和尽可能多的有效历史，同时保证：

- 只读取当前对话及其固定挑战版本。
- 核心安全规则不能因预算不足被静默删除。
- 摘要覆盖的旧历史不重复注入。
- 世界书只注入当前文本相关内容。
- 每次实际选择都可通过日志审计。

## 预算公式

```text
prompt_budget = max_context_tokens - max_response_tokens - 256
world_budget  = prompt_budget * 0.22
```

固定 256 token 为 JSON 信封等格式开销预留。若 prompt budget 小于等于 0，构建立即失败。

当前 tokenizer 是启发式计数器，因此预算是保护性估计，不是上游模型的精确 tokenizer 结果。更换模型或计数方式时应保留同样的预算接口和日志字段。

## 输入来源

一次生成读取：

1. 对话固定 Challenge Bundle。
2. 用户 LLM 设置中的模型和 token 窗口。
3. 当前结构化状态。
4. 当前对话最新摘要。
5. 未被摘要覆盖的内部消息历史。
6. 由历史、状态和摘要激活的世界书条目。

Challenge Bundle 在返回前重新计算内容哈希。任何版本状态、版本哈希或对话哈希不一致都会中止构建。

## Prompt 分段

Prompt 资产包含多个 `PromptSegment`：

- `key`：稳定语义键。
- `role`：发送给模型的消息角色。
- `content`：正文。
- `enabled`：是否参与候选。
- `required`：是否不可省略。
- `priority`、`position`：内容维护和顺序信息。

已发布 Prompt 必须包含启用、required、system role 且非空的：

- `system_master`
- `settlement_security`
- `output_schema`

代码不再提供这些内容的内置文字兜底，缺失时发布失败。

## 候选段

构建器会生成：

- Prompt 中所有 enabled 分段。
- Character 描述、性格、场景、示例对话和覆盖提示。
- 玩家选择的 Persona 描述、说话风格和限制。
- Scenario 描述与开场。
- `<current_state>` JSON，始终 required。
- 最新 `<memory_summary>`。
- 合并后的 `<world_info>`。

世界书在进入总预算前已经受独立 22% 预算筛选，但合并后的完整标签、标题和正文仍重新计入总 prompt token。

## 预算消费顺序

### 必选段

先统计全部 required 段。如果总量超过 prompt budget，返回 `ErrRequiredOverflow`，不调用模型。不能只保留一部分核心规则继续运行。

### 可选段

按以下优先级逐段尝试：

1. Character、Persona、Scenario。
2. 最新摘要。
3. 世界书。
4. 其他可选 Prompt 段，包括 style。

整段放不下时跳过并在 `SegmentInfo.skip_reason` 记录 `budget`，当前不会截断段内容。

### 历史

可选段处理后，从最新内部消息向旧消息倒序填充。遇到第一条放不下的消息就停止，再恢复为时间正序发送。

如果存在可见历史但剩余预算连一条都放不下，构建失败，避免模型在完全缺少近期交互的情况下继续生成。

## 最终消息顺序

已选段按语义顺序发送：

1. `system_master`
2. `output_schema`
3. Character
4. Persona
5. Scenario
6. 其他普通 Prompt system 段
7. 当前状态
8. 摘要
9. 世界书
10. style
11. 时间正序历史

required 影响是否必须纳入，不改变上述最终语义顺序。

## 摘要覆盖

如果最新摘要有 `covered_until_message_id`，该消息及之前历史都从候选中删除。摘要本身作为独立 system 段注入，之后消息继续按正常历史参与预算。

若覆盖消息在历史中找不到，当前实现保留全部历史。这通常意味着摘要数据损坏或消息被删除，应通过上下文日志排查。

## 状态格式

当前状态使用紧凑 JSON 包裹：

```text
<current_state>
{"profile":{...},"attributes":{...}}
</current_state>
```

状态是 required 段。内容设计应避免无界增长；大段历史文本、物品描述或任务全文不应无限塞入状态，否则会挤压摘要、世界书与对话历史。

## 世界书扫描输入

世界书扫描文本由三部分拼接：

- 最近最多 6 条未覆盖历史消息。
- 当前状态中递归提取出的字符串值。
- 最新摘要正文。

扫描细节见 `09-worldbook-memory.md`。

## 构建日志

每次正常生成写入 `context_build_logs`：

- 对话、触发消息、挑战版本和哈希。
- 模型、最大窗口、回复预留和已用 prompt token。
- 每个候选段的 key、role、token、required、included 和 skip reason。
- 实际纳入的消息 ID。
- 激活的世界书 entry ID。

日志用于上下文检查器、预算排障和版本审计。第二阶段判定续写复用同一装配流程，但当前不会额外写一条完整构建日志。

## 失败处理

- 固定版本哈希失败：停止生成，不能降级到仓库文件或内置 Prompt。
- required 段溢出：提示调整上下文窗口、回复预留或内容大小。
- 世界书超预算：跳过低优先候选，不影响 required 规则。
- 摘要读取失败：当前按无摘要继续；消息历史仍是权威记录。
- 上下文日志写入失败：记录应用日志，但不改变已经构造出的模型请求。

## 维护检查

- 新增 required 分段时验证最小上下文窗口仍能容纳。
- 修改段顺序时补 Context Builder 测试。
- 修改摘要覆盖规则时测试边界消息是否重复。
- 修改世界书预算时同时检查独立预算与合并后总预算。
- 日志不得写入 LLM API Key 或会话令牌。
