# 12. AI 输出、判定与动作系统

## 设计目标

模型负责提出叙事、状态变化、判定请求和动作意图；后端负责解析、白名单、随机性、持久化和奖励。模型输出不能直接等同于已经执行的业务事实。

当前使用结构化 JSON 信封而不是真正的上游 Tool Calling，原因是需要兼容多种 OpenAI 风格服务，并让流式叙事和状态更新使用一个稳定协议。

## 响应 Schema

服务启动时读取 `data/schemas/narrative_response.json`。成功加载后使用 `response_format: json_schema` 请求严格 JSON，并把 `roll_config.yaml` 中的情境词表动态注入 enum。

Schema 加载失败时服务会记录警告并退化为无 response_format 调用，但后端解析和白名单仍然生效。不能把上游声称支持 JSON Schema 当作业务授权。

## 信封结构

```json
{
  "narrative": "玩家可见剧情",
  "state_update": {
    "profile.remaining_lifespan": 110,
    "traits+": "顿悟"
  },
  "roll_request": null,
  "action": null,
  "notes": "内部备注"
}
```

字段：

- `narrative`：玩家可见的 Markdown 叙事，必需。
- `state_update`：状态补丁，必需但可为空对象。
- `roll_request`：可选，要求后端执行一次天命判定。
- `action`：可选，要求后端执行注册动作。
- `notes`：可选内部说明，写入消息 metadata，不直接展示。

## 容错解析

后端按以下顺序解析：

1. 去掉可能的 Markdown code fence。
2. 尝试把全文解析为 Envelope。
3. 尝试截取第一个 `{` 到最后一个 `}` 再解析。
4. 仍失败时把原始文本作为 narrative，state_update 为空。

解析降级只保证剧情可见，不会从自由文本中猜测状态或动作。流式模式从累积 JSON 中只提取 narrative 新增前缀，避免裸信封展示给玩家。

`FlexInt` 和 `FlexStrings` 对部分上游非标准数字、字符串数组和对象形态做尽力解析。这是协议容错，不是旧数据库兼容层。

## 状态补丁

允许的顶层命名空间：

```text
profile attributes inventory relationships location world_flags
cultivation ascension quests risks traits
```

嵌套对象使用点号路径，数组使用整体替换或 `+` 追加。`quests`、`risks`、`traits` 不能写任意嵌套字段。

非法路径跳过，合法路径在状态事务内应用并写入修订。状态补丁不能修改账号奖励、对话所有者、挑战版本、Prompt 或系统判定设置。

## 判定请求

典型请求：

```json
{
  "type": "筑基突破判定",
  "category": "cultivation",
  "relevant_attributes": ["根骨", "悟性", "气运"],
  "attribute_weights": {"根骨": 0.5, "悟性": 0.3, "气运": 0.2},
  "base_difficulty": 72,
  "sides": 100,
  "description": "突破到筑基期",
  "context": {
    "advantages": ["服用辅助丹药", "充分准备"],
    "disadvantages": ["首次尝试"],
    "environment": []
  },
  "stakes": {
    "reward_value": 50000,
    "risk_level": "high"
  }
}
```

category 支持 combat、exploration、crafting、social、cultivation。情境数组只能使用配置词表，后端精确查表，未知值不加分。

## 成功率计算

后端依次计算：

1. `base_difficulty`，缺失时使用 category 默认成功率。
2. 状态属性加成：属性 50 为中性，按 `(value - 50) * 0.4` 和权重合计。
3. 初始天赋与动态 traits 加减成。
4. 有利、不利、环境、对手境界与克制修正。
5. 根据 `stakes.reward_value` 应用高收益成功率上限。
6. 最近 10 条服务端流水产生的连败、重试或连胜调整。
7. 管理员 easy/normal/hard 难度修正。
8. 把最终成功率限制在 1%-99%，生成大成功、成功、失败、大失败区间。

模型提供的是规则输入，不提供随机结果。掷骰与成功布尔值只由后端产生。

## 两阶段流程

### 第一阶段

- 保存带判定请求的 narrative。
- 应用该阶段真实发生的资源消耗，例如用掉丹药或雷符。
- 读取服务端状态并计算、执行判定。
- 将结果写入 `roll_history`、内部 system 消息和展示消息。

### 第二阶段

- 重新装配包含判定结果的上下文。
- 再次调用模型完成结果叙事和状态后果。
- 第二阶段再次提交 roll_request 时忽略，防止无限递归。
- 第二阶段 action 可以进入正常动作分发。

第一阶段和第二阶段使用不同 message ID，WebSocket 各自闭合 started/delta/finished 生命周期。

## 动作注册表

编排器持有 `map[string]ActionHandler`。未知动作类型忽略或记录，不允许通过反射执行任意函数。当前只注册 `trigger_ending`。

动作结构：

```json
{
  "type": "trigger_ending",
  "params": {}
}
```

## `trigger_ending`

参数：

| 字段 | 必需性 | 含义 |
| --- | --- | --- |
| `ending_type` | 必需 | good、bad、neutral |
| `title` | 必需 | 结局标题；成功时必须匹配奖励规则 |
| `summary` | 可选 | 前端结局展示摘要 |
| `reward_key` | good 必需 | 只用于申请版本化奖励 |

成功示例：

```json
{
  "type": "trigger_ending",
  "params": {
    "ending_type": "good",
    "reward_key": "void_ascension",
    "title": "破碎虚空",
    "summary": "你穿过虚空裂缝，成功超脱此界。"
  }
}
```

neutral/bad 不能携带 reward_key。模型不能提交最终灵石、账号累计、成功次数或结算 ID。

## 结算执行

动作 handler 使用 `Prepare` / `Commit` 两阶段协议。`Prepare` 在候选回复落库前从服务端读取并冻结：

- 当前认证玩家。
- 当前对话结构化状态中的角色、境界和灵石。
- 当前对话用户消息数作为存活回合。

Challenge Service 先非锁定预校验对话、奖励规则、最低境界和精确 `roll_history`。候选被拒绝时，编排器发送 `generation_discarded` 删除前端流式占位，携带服务端原因重试一次；第二次仍无效则保存后端兜底叙事。通过后才保存模型叙事和状态，随后 `Commit` 使用冻结输入在事务锁内重做相同校验、计算奖励并广播 `ending_triggered`。

这个顺序保证同一回复的 `state_update` 不能给自己的结局补境界、物品或灵石。事务复核则负责处理预校验和提交之间的并发变化。

具体奖励规则和当前仍属于叙事约束的条件见 `14-ascension-trial-gameplay.md`。

## 安全要求

- Prompt 必须含 required 的 `settlement_security`，但 Prompt 不是唯一安全边界。
- 玩家输入只能表达当前意图；不能建立过去事件、状态、物品、判定或奖励事实。
- reward_key 必须来自对话固定版本，不能从当前挑战包新版本读取。
- ending title 与 type 必须精确匹配规则。
- 判定条件只查询当前 conversation 的服务端流水。
- 成功路线使用精确且去重的判定类型集合，不接受前缀相似项或重复项代替。
- 同一对话依靠行锁和唯一 challenge_run 防止重复结算。
- state_update 和 summary 都不能直接成为账号记账指令。
- 未知 action、字段或情境词条不能被当作已授权能力。

## 完整示例

更多普通回复、状态更新、判定和结局信封见 `12-ai-actions-examples.md`。示例用于协议理解；题材规则以已发布挑战版本和 `14-ascension-trial-gameplay.md` 为准。

## 未来动作

可能增加 `request_choice`、`trigger_event` 或小游戏动作，但每种动作必须：

1. 定义严格参数结构。
2. 注册明确 handler。
3. 从服务端读取身份和业务事实。
4. 设计幂等、事务和重放保护。
5. 增加 API/领域测试和文档。

不得增加一个可以让模型指定任意数据库操作的通用 action。
