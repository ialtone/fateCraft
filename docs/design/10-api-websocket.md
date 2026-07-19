# 10. API 与 WebSocket 草案

## REST API

> 本章为主要路由的概览，可能滞后于实现；**完整、权威的路由列表以 `backend/internal/api/server.go` 的注册为准**。

### Health

- `GET /api/health`

### Conversations

- `POST /api/conversations`
- `GET /api/conversations`
- `GET /api/conversations/:id`
- `PATCH /api/conversations/:id`
- `DELETE /api/conversations/:id`
- `POST /api/conversations/:id/end`
- `POST /api/conversations/:id/archive`
- `POST /api/conversations/:id/restore`

### Messages

- `GET /api/conversations/:id/messages?before=&limit=`
- `POST /api/conversations/:id/messages`
- `PATCH /api/messages/:id`
- `DELETE /api/messages/:id`
- `POST /api/conversations/:id/regenerate`
- `POST /api/conversations/:id/continue`

### State

- `GET /api/conversations/:id/state`
- `PATCH /api/conversations/:id/state`
- `GET /api/conversations/:id/state/revisions`

### Context

- `GET /api/conversations/:id/context/last`
- `GET /api/context/:context_build_id`

### Characters

- `GET /api/characters`
- `POST /api/characters`
- `GET /api/characters/:id`
- `PATCH /api/characters/:id`
- `DELETE /api/characters/:id`

### Prompts

- `GET /api/prompts`
- `POST /api/prompts`
- `GET /api/prompts/:id`
- `PATCH /api/prompts/:id`
- `POST /api/prompts/:id/segments`
- `PATCH /api/prompt-segments/:id`

### World Books

- `GET /api/worldbooks`
- `POST /api/worldbooks`
- `GET /api/worldbooks/:id`
- `PATCH /api/worldbooks/:id`
- `POST /api/worldbooks/:id/entries`
- `PATCH /api/worldbook-entries/:id`
- `DELETE /api/worldbook-entries/:id`
- `POST /api/conversations/:id/worldbooks/:world_book_id`
- `DELETE /api/conversations/:id/worldbooks/:world_book_id`

### Settings

- `GET /api/settings/llm`
- `PATCH /api/settings/llm`

## WebSocket

Endpoint：

- `/ws/conversations/:id`

一个 WebSocket 连接只绑定一个 conversation_id。切换对话时关闭旧连接，打开新连接。

客户端发送：

```json
{
  "type": "user_input",
  "content": "我走向窗边，听外面的动静。"
}
```

```json
{
  "type": "cancel_generation"
}
```

服务端发送：

```json
{
  "type": "generation_started",
  "message_id": "uuid"
}
```

```json
{
  "type": "generation_delta",
  "message_id": "uuid",
  "delta": "雨声渐近"
}
```

```json
{
  "type": "generation_finished",
  "message_id": "uuid"
}
```

```json
{
  "type": "state_update",
  "conversation_id": "uuid",
  "state": {}
}
```

```json
{
  "type": "message_created",
  "conversation_id": "uuid",
  "message_id": "uuid",
  "message": {}
}
```

```json
{
  "type": "message_updated",
  "conversation_id": "uuid",
  "message_id": "uuid",
  "message": {}
}
```

```json
{
  "type": "conversation_ended",
  "conversation_id": "uuid"
}
```

```json
{
  "type": "ending_triggered",
  "conversation_id": "uuid",
  "ending_type": "good",
  "ending_title": "渡劫飞升",
  "ending_summary": "九九天劫尽数渡过，功德圆满。",
  "spirit_stones": 12000,
  "survival_turns": 42
}
```

```json
{
  "type": "context_built",
  "context_build_id": "uuid"
}
```

```json
{
  "type": "error",
  "error": "error text"
}
```
