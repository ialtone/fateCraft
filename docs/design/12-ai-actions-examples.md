# 12A. Agent 工具调用示例

本文使用协议无关的“模型步骤 / 工具结果”表示法。Chat Completions 和 Responses 的线上 JSON 由 LLM 适配层转换，业务层只接收统一 `ToolCall`。规则以 [12-ai-actions.md](12-ai-actions.md) 和 [19-agent-tool-loop.md](19-agent-tool-loop.md) 为准。

## 1. 状态更新后叙事

模型步骤 1：

```json
{
  "tool": "fc_state_set_number",
  "call_id": "call-1",
  "arguments": {
    "updates": [
      {"path": "investigation.clue_integrity", "value": 60}
    ]
  }
}
```

工具结果：

```json
{"ok":true,"code":"state_committed","message":"状态已更新","data":{"paths":["investigation.clue_integrity"]}}
```

模型步骤 2 直接返回普通文本：

```text
你将钟表记录与女仆证词并排，停摆时间相差的一刻钟终于显出意义。
```

若路径未知、越权或越界，步骤 1 返回 `state_rejected`，不会保存半份状态。模型步骤 2 应依据真实旧状态和拒绝原因继续。

## 2. 服务端判定

模型步骤 1：

```json
{
  "tool": "fc_request_roll",
  "call_id": "call-2",
  "arguments": {
    "key": "investigation.final-accusation",
    "title": "最终指控判定",
    "description": "根据已经收集的证据指出真凶"
  }
}
```

工具成功后，判定流水、展示消息和效果已经由服务端提交。模型步骤 2 只能依据工具结果叙述成败；它不能自行改写概率、骰点或奖励。

## 3. 多步工具循环

修仙突破可能需要多个顺序步骤：

```text
step 1  -> fc_request_roll(cultivation.breakthrough.core)
tool    -> 成功，系统境界与寿元效果已提交
step 2  -> fc_state_set_number(修为进度、剩余寿元等模型字段)
tool    -> 状态已提交
step 3  -> 普通文本叙述突破后的可感知结果
```

每个模型步骤只能调用一个工具。第一个工具没有成功前，不得在最终叙事中宣布后续事实。

## 4. Campaign 领域动作

假设当前工具快照提供 `fc_action_world_gather`：

```json
{
  "tool": "fc_action_world_gather",
  "call_id": "call-3",
  "arguments": {
    "node_key": "spirit-herb-patch",
    "quantity": 1
  }
}
```

revision、幂等键和合法候选已经固定在服务端快照中，不由模型提交。Handler 在行锁事务中计算实际产量、品质、库存和时间。如果采集点已耗尽，工具返回 `action_rejected`，模型不能沿用调用前设想的收获叙事。

## 5. 完成挑战

```json
{
  "tool": "fc_complete_challenge",
  "call_id": "call-4",
  "arguments": {
    "outcome_key": "case_solved",
    "summary": "依据完整时间线完成最终指控。"
  }
}
```

模型不能增加 `reward_key/quantity/multiplier` 等字段。Schema 和 Handler 都会拒绝额外参数。工具成功后前端以 `challenge_completed` 的服务端快照展示结局和奖励。

## 6. JSON 状态值

整体替换列表：

```json
{
  "tool": "fc_state_replace_json",
  "arguments": {
    "updates": [
      {"path":"quests","value_json":"[{\"key\":\"find-witness\",\"status\":\"active\"}]"}
    ]
  }
}
```

向列表追加对象：

```json
{
  "tool": "fc_state_append_json",
  "arguments": {
    "updates": [
      {"path":"inventory.items","value_json":"{\"key\":\"iron-token\",\"name\":\"玄铁令\"}"}
    ]
  }
}
```

动态子路径：

```json
{
  "tool": "fc_state_set_dynamic_json",
  "arguments": {
    "updates": [
      {"path":"relationships","child_path":"steward.trust","value_json":"25"}
    ]
  }
}
```

`value_json` 是 JSON 编码字符串，解码后的值仍要通过 State Schema。

以上动态示例仅适用于非 Campaign Runtime。长期玩法的会话状态由 `narrative.*` 固定字段组成；角色、地点、库存等权威事实不接受 `fc_state_set_dynamic_json`，必须调用当前工具快照提供的 `fc_action_*`。

## 7. 输出通道

流式步骤在完整归一化结果确认后选择通道：

- 无工具调用且存在文本/拒答：进入 text lane，完整文本可以保存；
- 只要存在工具调用：进入 tool lane，伴随文本不进入玩家时间线；
- 非流式 Chat 同时返回 content 和 tool calls：选择 tool lane，按返回顺序执行；
- 多个工具调用：通过批次校验后按返回顺序逐个执行，仍受幂等和写工具上限约束。

模型调用 EOF、超时或截断时，临时文本不会写入最终消息。此前没有写工具则 attempt 为 `failed/cancelled`；已有工具提交则为 `partial`，用户可使用“继续叙事”从可信事实继续。

## 8. 前端事件顺序

工具后完成文本：

```text
agent.turn.started
agent.step.started
agent.tool.started
agent.tool.finished
agent.step.started
agent.reasoning.delta * N
agent.text.delta * N
message_created
agent.turn.completed
```

写工具已提交但后续模型失败：

```text
agent.tool.finished(status=committed)
agent.step.started
agent.turn.partial
error
```

每个 `agent.*` 事件带 `event_id`、`attempt_id` 和递增 `sequence`。前端去重；发现 sequence 间隙时重新获取消息、状态、存档、活动和最近上下文快照。

## 9. 回归检查

- Chat/Responses × stream/non-stream 都能归一化文本和工具调用。
- 工具参数按 call ID 正确重组，空响应、EOF、截断和 provider error 不会伪装成成功。
- 单步骤多工具不会执行第一个后忽略其余工具。
- 拒绝工具不会留下领域写入；成功工具和审计消息同事务提交。
- 第 7 个写工具在执行前返回 `mutating_tool_limit`。
- 最终 assistant 只保存普通文本，不保存内部工具参数或中间工作内容。
