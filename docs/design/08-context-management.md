# 08. 上下文管理设计

## 目标

在长对话中避免全量发送历史，同时让模型保留必要上下文。上下文构造必须严格绑定当前 conversation_id。

## 输入

- 当前用户输入。
- 当前对话元数据。
- 当前结构化状态。
- Prompt 预设。
- 角色卡和 Persona。
- 最新摘要。
- 世界书条目。
- 最近历史消息。

禁止输入：

- 其他对话的消息。
- 其他对话的状态。
- 其他对话的摘要。
- 其他对话的上下文构建日志。

## 输出

- 最终 Chat Completion messages。
- token 明细。
- 被加入的历史消息 ID。
- 被激活的世界书条目 ID。
- 被裁剪原因。

## PromptSegment

```go
type PromptSegment struct {
    Key       string
    Role      string
    Content   string
    Priority  int
    Required  bool
    BudgetKey string
    Tokens    int
    Metadata  map[string]any
}
```

## 构造流程

1. 读取模型上下文配置。
2. 计算 prompt 预算。
3. 校验当前 conversation_id 状态为 active。
4. 加入 required segments。
5. 加入当前对话 state。
6. 加入 user input。
7. 扫描当前对话绑定的世界书。
8. 加入当前对话摘要。
9. 从新到旧填充当前对话最近历史。
10. 加入可选风格和示例。
11. 按发送顺序组装 messages。
12. 保存当前对话 context_build_logs。

## 伪代码

```go
budget := maxContext - reservedResponse - formatReserve
builder := NewBudget(budget)

builder.MustAdd(systemMaster)
builder.MustAdd(outputSchema)
builder.MustAdd(currentState)
builder.MustAdd(userInput)

builder.TryAdd(memorySummary)
builder.TryAddWorldBook(activatedEntries, worldBudget)
builder.FillRecentHistory(recentMessages)
builder.TryAdd(styleNote)
builder.TryAdd(examples)

messages := builder.BuildMessages()
log := builder.BuildLog()
```

## Required 处理

如果 required segment 超预算，不能静默丢弃。

处理方式：

- 返回错误给前端。
- 提示降低 Prompt、状态大小或上下文预留。
- 不发起 LLM 请求。

## 历史填充规则

- 倒序遍历历史。
- 跳过 hidden 消息。
- 只读取当前 conversation_id 的消息。
- 跳过已被摘要覆盖且不在最近窗口内的消息。
- 加入前计算 token。
- 超预算停止。
- 最后恢复时间顺序。

## 摘要覆盖规则

摘要记录 `conversation_id` 和 `covered_until_message_id`。

构造上下文时：

- 摘要覆盖之前的消息不再进入最近窗口。
- 摘要之后的消息可以进入最近窗口。
- 如果摘要过旧，触发后台摘要任务。
- 摘要不能跨 conversation_id 复用。

## 状态注入格式

```text
<current_state>
{ JSON }
</current_state>
```

状态注入要短，避免把冗长 display history 放进去。

## 输出格式约束

建议固定要求模型输出：

```json
{
  "narrative": "Markdown text",
  "state_update": {},
  "notes": "optional internal note"
}
```

后端只使用 narrative 和 state_update。
