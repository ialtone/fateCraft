# 12. AI 动作系统

## 概述

AI 动作系统允许模型在输出中包含一个可选的 `action` 字段，用于触发特殊的系统级操作。当前支持的主要动作是 `trigger_ending`，允许 AI 根据剧情自然地结束对话。

## 设计动机

在叙事类游戏中，某些剧情条件满足时应该自动触发结局，而不是让用户手动点击"结束对话"。例如：

- 玩家连续三次拒绝茶馆的茶水
- 说书人拍下醒木第三拍
- 故事自然走向终章（晨光中茶馆消失）

传统方案（Tool Calling）需要改造 LLM client 并且依赖模型的 function calling 能力。**方案 A-lite（伪 Tool Calling）** 通过在 JSON 输出中添加 `action` 字段，既保持了语义清晰度，又避免了复杂的实现。

## 输出格式

### 基本结构

```json
{
  "narrative": "剧情文本，支持 Markdown",
  "state_update": {
    "profile.title": "新身份",
    "location.name": "茶馆门外"
  },
  "action": {
    "type": "trigger_ending",
    "params": {
      "ending_type": "good",
      "title": "命运归处",
      "summary": "你走出茶馆，回望时它已消失在晨光中。"
    }
  },
  "notes": "可选内部备注"
}
```

### 字段说明

- `action` (可选): 触发的动作对象
  - `type` (string): 动作类型，目前支持 `trigger_ending`
  - `params` (object): 动作参数，根据 type 不同而不同

### 向后兼容

`action` 字段完全可选。不包含 `action` 的输出会像之前一样正常处理：

```json
{
  "narrative": "普通的剧情推进。",
  "state_update": {},
  "notes": ""
}
```

## trigger_ending 动作

### 参数

| 参数 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `ending_type` | string | 是 | 结局类型：`good`（好结局）/ `bad`（坏结局）/ `neutral`（中立结局）/ `hidden`（隐藏结局） |
| `title` | string | 是 | 结局标题，用于前端展示 |
| `summary` | string | 是 | 结局摘要，简短描述这个结局的含义 |

### 示例

#### 好结局

```json
{
  "narrative": "说书人微笑着合上折扇，醒木轻轻一拍。茶馆在晨光中渐渐隐去，你发现自己站在清晨的街头，手中握着一枚温润的玉佩。",
  "state_update": {
    "inventory.items+": "命运玉佩",
    "world_flags.ending_reached": true
  },
  "action": {
    "type": "trigger_ending",
    "params": {
      "ending_type": "good",
      "title": "命运归处",
      "summary": "你在命运茶馆中做出了正确的选择，带着祝福回到现实世界。"
    }
  }
}
```

#### 坏结局

```json
{
  "narrative": "醒木第三次落下，茶馆剧烈震动。你的视野逐渐模糊，意识陷入无尽的轮回……",
  "state_update": {
    "world_flags.trapped_in_cycle": true
  },
  "action": {
    "type": "trigger_ending",
    "params": {
      "ending_type": "bad",
      "title": "永恒轮回",
      "summary": "你被困在了命运的循环中，再也无法回到现实。"
    }
  }
}
```

#### 中立结局

```json
{
  "narrative": "你第三次婉拒了说书人递来的茶杯。他叹息一声：「缘分未到，客官请回吧。」茶馆的门在你身后关闭，再也找不到入口。",
  "state_update": {
    "world_flags.left_peacefully": true
  },
  "action": {
    "type": "trigger_ending",
    "params": {
      "ending_type": "neutral",
      "title": "缘尽于此",
      "summary": "你拒绝了命运茶馆的邀约，平安回到了日常生活。"
    }
  }
}
```

## 系统行为

当 AI 输出包含 `action` 字段时，后端会：

1. **解析 action**：从 `Envelope` 中提取 `Action` 对象
2. **执行动作**：
   - 对于 `trigger_ending`：调用 `conversation.Service.End()` 将对话状态置为 `ended`
3. **广播事件**：通过 WebSocket 发送 `ending_triggered` 事件给所有连接的客户端
4. **正常完成**：动作执行不影响消息的保存和状态更新流程

### WebSocket 事件

```json
{
  "type": "ending_triggered",
  "conversation_id": "uuid",
  "ending_type": "good",
  "ending_title": "命运归处",
  "ending_summary": "你在命运茶馆中做出了正确的选择..."
}
```

前端可以监听这个事件，展示结局卡片、播放特殊动画、禁用输入框等。

## 世界书集成

在世界书中定义结局规则，AI 会根据这些规则判断何时触发结局：

```json
{
  "title": "茶馆结局规则",
  "content": "当以下情况发生时，故事走向终局：\n1. 玩家连续三次拒绝命运茶馆提供的茶水，说书人会叹息并送客离开（中立结局）。\n2. 说书人拍下醒木第三拍，因果线彻底改写，玩家的命运发生根本转折（好结局或坏结局，取决于玩家的选择）。\n3. 玩家主动提出离开茶馆且说书人认可时机已到（中立结局）。\n4. 茶馆在晨光中消失，玩家被自然送出（好结局）。\n触发结局时，使用 action 字段：{\"type\": \"trigger_ending\", \"params\": {\"ending_type\": \"good/bad/neutral/hidden\", \"title\": \"结局标题\", \"summary\": \"结局摘要\"}}",
  "keywords": ["结局", "终章", "离开", "醒木三拍", "晨光"],
  "priority": 150
}
```

## 实现细节

### 数据结构

**llm/types.go**

```go
type Envelope struct {
    Narrative   string         `json:"narrative"`
    StateUpdate map[string]any `json:"state_update"`
    Action      *Action        `json:"action,omitempty"`
    Notes       string         `json:"notes"`
}

type Action struct {
    Type   string         `json:"type"`
    Params map[string]any `json:"params"`
}
```

### 处理流程

**realtime/generator.go**

```go
func (g *Generator) runGeneration(...) {
    // ... 生成并解析 ...
    env := llm.ParseEnvelope(raw)
    
    // ... 保存消息和应用状态 ...
    
    // 检测并处理动作
    if env.Action != nil {
        g.handleAction(ctx, convID, env.Action, emit)
    }
    
    // ... 推送完成事件 ...
}

func (g *Generator) handleAction(ctx context.Context, convID uuid.UUID, action *llm.Action, emit EmitFunc) {
    switch action.Type {
    case "trigger_ending":
        g.handleEndingAction(ctx, convID, action.Params, emit)
    }
}

func (g *Generator) handleEndingAction(ctx context.Context, convID uuid.UUID, params map[string]any, emit EmitFunc) {
    endingType := getStringParam(params, "ending_type", "neutral")
    title := getStringParam(params, "title", "故事终章")
    summary := getStringParam(params, "summary", "")
    
    g.convSvc.End(ctx, convID)
    
    emit(Event{
        Type:           TypeEndingTriggered,
        ConversationID: convID.String(),
        EndingType:     endingType,
        EndingTitle:    title,
        EndingSummary:  summary,
    })
}
```

## 未来扩展

动作系统设计为可扩展的，未来可以支持更多动作类型：

### trigger_event

触发特殊剧情事件：

```json
{
  "action": {
    "type": "trigger_event",
    "params": {
      "event_id": "merchant_arrives",
      "description": "神秘商人出现在茶馆门口"
    }
  }
}
```

### start_minigame

启动内嵌小游戏：

```json
{
  "action": {
    "type": "start_minigame",
    "params": {
      "game_type": "dice_roll",
      "stakes": "命运骰子"
    }
  }
}
```

### request_choice

要求玩家做出明确选择（带倒计时）：

```json
{
  "action": {
    "type": "request_choice",
    "params": {
      "question": "你要喝下这杯茶吗？",
      "options": ["喝下", "拒绝", "观察"],
      "timeout_seconds": 60
    }
  }
}
```

## 与真 Tool Calling 的对比

| 维度 | 方案 A-lite (action 字段) | 方案 A (真 Tool Calling) |
|------|--------------------------|--------------------------|
| 实现复杂度 | 低（5 行检测代码） | 高（改造 LLM client + 流式处理） |
| 模型兼容性 | 好（任何能输出 JSON 的模型） | 差（需要支持 function calling） |
| 语义清晰度 | 高 | 高 |
| 可扩展性 | 高（随意添加新 action type） | 中（每个 action 需定义 tool schema） |
| 调试友好度 | 高（action 在 JSON 输出里，可见） | 中（tool call 在单独字段） |
| 向后兼容 | 完美（action 可选） | 差（需要模型理解 tools） |

**结论**：对于当前需求（剧情驱动的自动结局），方案 A-lite 是最优选择。
