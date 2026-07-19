# 06. Gin 后端设计

## 模块划分

```text
internal/api              路由和 HTTP handler
internal/conversation     对话服务
internal/chat             消息服务
internal/state            状态服务
internal/contextbuilder   上下文构造
internal/llm              LLM 客户端
internal/prompt           Prompt 预设
internal/worldbook        世界书
internal/memory           摘要记忆
internal/storage          PostgreSQL 访问
internal/realtime         WebSocket
internal/tokenizer        token 统计
```

## 请求流程

用户发送消息：

1. Gin handler 接收请求或 WebSocket 消息。
2. `conversation` 服务校验当前对话存在且状态为 active。
3. `chat` 服务保存用户消息到当前 conversation_id。
4. `conversation` 服务设置 `is_processing=true`。
5. `contextbuilder` 只基于当前 conversation_id 构造上下文。
6. `llm` 服务请求模型。
7. 流式 delta 通过当前对话 WebSocket 返回前端。
8. 完整回复生成后解析结构化输出。
9. `state` 服务校验并应用当前对话状态更新。
10. 保存 assistant 消息和 display message。
11. 保存当前对话 context build log。
12. 推送最终状态。

## 对话状态

- active：允许生成和编辑。
- ended：不允许继续生成，只允许查看。
- archived：从默认列表隐藏。
- deleted：删除或软删除。

结束对话：

- 停止当前生成任务。
- 设置 status=ended。
- 设置 ended_at。
- 推送 `conversation_ended`。
- 前端返回列表或打开其他对话。

## LLM 输出协议

建议模型输出 JSON 包裹结构：

```json
{
  "narrative": "剧情文本，支持 Markdown",
  "state_update": {
    "profile.title": "新身份",
    "inventory.items+": "铜钥匙"
  }
}
```

MVP 先支持：

- narrative。
- state_update。

## 状态更新校验

后端需要维护允许更新路径：

- `profile.*`
- `attributes.*`
- `inventory.*`
- `relationships.*`
- `location.*`
- `quests`
- `risks`
- `world_flags.*`

禁止更新：

- conversation id。
- player id。
- timestamps。
- model settings。
- prompt settings。
- system flags。

## WebSocket 推送

消息类型：

- `generation_started`。
- `generation_delta`。
- `generation_finished`。
- `state_update`。
- `message_created`。
- `message_updated`。
- `context_built`。
- `conversation_ended`。
- `error`。

## 并发控制

- 单对话同一时间只能有一个生成任务。
- 生成任务只能读取当前 conversation_id 下的数据。
- 用户点击停止时取消当前对话请求。
- 断线后生成可以继续，前端重连后拉取当前对话状态。
